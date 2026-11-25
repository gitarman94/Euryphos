package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Server struct {
	db                      *sql.DB
	adminToken, enrollToken string
	onlineGrace             time.Duration
}

type response map[string]any

func main() {
	admin := os.Getenv("EURYPHOS_ADMIN_TOKEN")
	enroll := os.Getenv("EURYPHOS_ENROLLMENT_TOKEN")
	if admin == "" || enroll == "" {
		log.Fatal("EURYPHOS_ADMIN_TOKEN and EURYPHOS_ENROLLMENT_TOKEN are required")
	}

	db, err := openDatabase(env("EURYPHOS_DB_PATH", "./data/euryphos.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
	if env("EURYPHOS_AUTO_MIGRATE", "true") == "true" {
		if err := migrate(ctx, db); err != nil {
			log.Fatal("SQLite migration failed: ", err)
		}
	}

	graceSeconds := 90
	if raw := os.Getenv("EURYPHOS_ONLINE_GRACE_SECONDS"); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 15 || parsed > 3600 {
			log.Fatal("EURYPHOS_ONLINE_GRACE_SECONDS must be between 15 and 3600")
		}
		graceSeconds = parsed
	}
	s := &Server{db: db, adminToken: admin, enrollToken: enroll, onlineGrace: time.Duration(graceSeconds) * time.Second}
	addr := env("EURYPHOS_LISTEN", ":8080")
	log.Printf("Euryphos Go server listening on %s (SQLite: %s)", addr, env("EURYPHOS_DB_PATH", "./data/euryphos.db"))
	server := &http.Server{
		Addr:              addr,
		Handler:           securityHeaders(routes(s)),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Fatal(server.ListenAndServe())
}

func routes(s *Server) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("POST /api/v1/enrollment", s.enroll)
	mux.HandleFunc("POST /api/v1/devices/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/v1/devices/{id}/revoke", s.revoke)
	mux.HandleFunc("POST /api/v1/devices/{id}/heartbeat", s.deviceAuth(s.heartbeat))
	mux.HandleFunc("POST /api/v1/devices/{id}/inventory", s.deviceAuth(s.inventory))
	mux.HandleFunc("GET /api/v1/devices", s.admin(s.devices))
	mux.HandleFunc("GET /api/v1/devices/{id}", s.admin(s.deviceDetail))
	mux.HandleFunc("GET /api/v1/devices/{id}/inventory/history", s.admin(s.inventoryHistory))
	mux.HandleFunc("POST /api/v1/jobs", s.admin(s.createJob))
	mux.HandleFunc("GET /api/v1/devices/{id}/jobs/next", s.deviceAuth(s.nextJob))
	mux.HandleFunc("POST /api/v1/devices/{id}/jobs/{job}/result", s.deviceAuth(s.jobResult))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.admin(s.getJob))
	return mux
}

// openDatabase uses the pure-Go modernc SQLite driver. SQLite WAL permits concurrent
// readers while SQLite itself serializes writers. The busy timeout smooths short write
// bursts; it does not turn SQLite into a multi-writer database server.
func openDatabase(path string) (*sql.DB, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve SQLite database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		return nil, fmt.Errorf("create SQLite data directory: %w", err)
	}
	file, err := os.OpenFile(abs, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create SQLite database file: %w", err)
	}
	_ = file.Close()
	if runtime.GOOS != "windows" {
		if err := os.Chmod(abs, 0600); err != nil {
			return nil, fmt.Errorf("secure SQLite database permissions: %w", err)
		}
	}

	uriPath := filepath.ToSlash(abs)
	// Windows drive-letter paths need a leading slash in a file: URI (file:///C:/...).
	if runtime.GOOS == "windows" && len(uriPath) >= 2 && uriPath[1] == ':' {
		uriPath = "/" + uriPath
	}
	uri := (&url.URL{Scheme: "file", Path: uriPath}).String()
	query := url.Values{}
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_pragma", "busy_timeout(10000)")
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "synchronous(NORMAL)")
	db, err := sql.Open("sqlite", uri+"?"+query.Encode())
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}
	// Bound the connection pool. WAL allows concurrent readers, while writes remain
	// serialized by SQLite; a bounded pool prevents unbounded connection creation.
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	db.SetConnMaxLifetime(0)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect to SQLite database: %w", err)
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			hostname TEXT NOT NULL DEFAULT '',
			os_family TEXT NOT NULL DEFAULT '',
			os_version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'pending',
			token_hash TEXT,
			last_seen TEXT,
			first_connected_at TEXT,
			last_connected_at TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS device_inventory (
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			category TEXT NOT NULL,
			payload TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(device_id, category)
		)`,
		`CREATE TABLE IF NOT EXISTS device_inventory_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			category TEXT NOT NULL,
			payload TEXT NOT NULL,
			observed_at TEXT NOT NULL,
			received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_inventory_history_device_category_id
			ON device_inventory_history(device_id, category, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_inventory_history_device_received
			ON device_inventory_history(device_id, received_at DESC)`,
		`CREATE TABLE IF NOT EXISTS jobs (
			id TEXT PRIMARY KEY,
			action TEXT NOT NULL,
			payload TEXT NOT NULL DEFAULT '{}',
			status TEXT NOT NULL DEFAULT 'queued',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS job_targets (
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			status TEXT NOT NULL DEFAULT 'queued',
			result TEXT,
			error TEXT,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(job_id, device_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname)`,
		`CREATE INDEX IF NOT EXISTS idx_inventory_category ON device_inventory(category)`,
		`CREATE INDEX IF NOT EXISTS idx_job_targets_device_status ON job_targets(device_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (2)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (3)`,
	}
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		definition string
	}{
		{name: "first_connected_at", definition: "TEXT"},
		{name: "last_connected_at", definition: "TEXT"},
	} {
		if err := ensureDeviceColumn(ctx, tx, column.name, column.definition); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureDeviceColumn(ctx context.Context, tx *sql.Tx, columnName, definition string) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info(devices)")
	if err != nil { return err }
	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil { rows.Close(); return err }
		if name == columnName { found = true }
	}
	if err := rows.Err(); err != nil { rows.Close(); return err }
	if err := rows.Close(); err != nil { return err }
	if found { return nil }
	_, err = tx.ExecContext(ctx, "ALTER TABLE devices ADD COLUMN "+columnName+" "+definition)
	return err
}

func (s *Server) connectionState(lastSeen sql.NullString) string {
	if !lastSeen.Valid || lastSeen.String == "" { return "never_connected" }
	observed, err := time.Parse(time.RFC3339Nano, lastSeen.String)
	if err != nil { return "unknown" }
	grace := s.onlineGrace
	if grace <= 0 { grace = 90 * time.Second }
	age := time.Since(observed)
	if age < -30*time.Second { return "unknown" }
	if age <= grace { return "online" }
	return "offline"
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.db.PingContext(ctx); err != nil {
		write(w, http.StatusServiceUnavailable, response{"status": "degraded"})
		return
	}
	write(w, http.StatusOK, response{"status": "ok", "service": "Euryphos", "runtime": "go", "database": "sqlite"})
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r, s.adminToken) {
			write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) deviceAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var storedHash, status string
		err := s.db.QueryRowContext(r.Context(), "SELECT COALESCE(token_hash, ''), status FROM devices WHERE id = ?", r.PathValue("id")).Scan(&storedHash, &status)
		if err != nil || status != "approved" || !constantHashMatch(storedHash, token(r)) {
			write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	if !constantString(token(r), s.enrollToken) {
		write(w, http.StatusUnauthorized, response{"error": "invalid_enrollment_token"})
		return
	}
	var in struct {
		Hostname  string `json:"hostname"`
		OSFamily  string `json:"os_family"`
		OSVersion string `json:"os_version"`
	}
	if !decode(w, r, &in) {
		return
	}
	id := uuid()
	secret := randomToken()
	_, err := s.db.ExecContext(r.Context(),
		"INSERT INTO devices(id, hostname, os_family, os_version, token_hash) VALUES (?, ?, ?, ?, ?)",
		id, in.Hostname, in.OSFamily, in.OSVersion, hash(secret))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "enrollment_failed"})
		return
	}
	write(w, http.StatusCreated, response{"device_id": id, "device_token": secret, "status": "pending_approval"})
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	result, err := s.db.ExecContext(r.Context(), "UPDATE devices SET status = 'approved' WHERE id = ? AND status = 'pending'", r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		write(w, http.StatusNotFound, response{"error": "pending_device_not_found"})
		return
	}
	write(w, http.StatusOK, response{"status": "approved"})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	result, err := s.db.ExecContext(r.Context(), "UPDATE devices SET status = 'revoked', token_hash = NULL WHERE id = ?", r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		write(w, http.StatusNotFound, response{"error": "device_not_found"})
		return
	}
	write(w, http.StatusOK, response{"status": "revoked"})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if !bearer(r, s.adminToken) {
		write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
		return false
	}
	return true
}

func (s *Server) heartbeat(w http.ResponseWriter, r *http.Request) {
	var in map[string]any
	if !decode(w, r, &in) {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(r.Context(), `UPDATE devices
		SET last_seen = ?,
		    first_connected_at = COALESCE(first_connected_at, ?),
		    last_connected_at = ?,
		    hostname = COALESCE(NULLIF(?, ''), hostname),
		    os_family = COALESCE(NULLIF(?, ''), os_family),
		    os_version = COALESCE(NULLIF(?, ''), os_version)
		WHERE id = ?`,
		now, now, now, str(in["hostname"]), str(in["os_family"]), str(in["os_version"]), r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	write(w, http.StatusOK, response{"status": "ok", "server_time": time.Now().UTC()})
}

func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Category   string          `json:"category"`
		Payload    json.RawMessage `json:"payload"`
		ObservedAt time.Time       `json:"observed_at"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Category == "" || len(in.Category) > 80 || len(in.Payload) == 0 || !json.Valid(in.Payload) {
		write(w, http.StatusBadRequest, response{"error": "invalid_inventory"})
		return
	}
	if in.ObservedAt.IsZero() {
		in.ObservedAt = time.Now().UTC()
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer tx.Rollback()
	observedAt := in.ObservedAt.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO device_inventory_history(device_id, category, payload, observed_at)
		VALUES (?, ?, ?, ?)`, r.PathValue("id"), in.Category, string(in.Payload), observedAt); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_history_write_failed"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), `INSERT INTO device_inventory(device_id, category, payload, observed_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(device_id, category) DO UPDATE SET
			payload = excluded.payload,
			observed_at = excluded.observed_at,
			received_at = CURRENT_TIMESTAMP`,
		r.PathValue("id"), in.Category, string(in.Payload), observedAt); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_current_write_failed"})
		return
	}
	receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `UPDATE devices
		SET last_seen = ?,
		    first_connected_at = COALESCE(first_connected_at, ?),
		    last_connected_at = ?
		WHERE id = ?`, receivedAt, receivedAt, receivedAt, r.PathValue("id")); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "device_last_seen_update_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_commit_failed"})
		return
	}
	write(w, http.StatusAccepted, response{"status": "accepted"})
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	page := 1
	pageSize := 50
	if raw := r.URL.Query().Get("page"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 1000000 {
			write(w, http.StatusBadRequest, response{"error": "invalid_page"})
			return
		}
		page = parsed
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			write(w, http.StatusBadRequest, response{"error": "invalid_page_size", "max": 200})
			return
		}
		pageSize = parsed
	}
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(search) > 200 {
		write(w, http.StatusBadRequest, response{"error": "search_too_long", "max": 200})
		return
	}
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	if status != "" && status != "pending" && status != "approved" && status != "revoked" {
		write(w, http.StatusBadRequest, response{"error": "invalid_status_filter"})
		return
	}
	osFamily := strings.TrimSpace(r.URL.Query().Get("os_family"))
	if len(osFamily) > 40 {
		write(w, http.StatusBadRequest, response{"error": "invalid_os_family_filter"})
		return
	}
	sortField := r.URL.Query().Get("sort")
	sortColumns := map[string]string{
		"hostname": "hostname",
		"os_family": "os_family",
		"os_version": "os_version",
		"status": "status",
		"last_seen": "last_seen",
		"created_at": "created_at",
	}
	if sortField == "" {
		sortField = "hostname"
	}
	sortColumn, ok := sortColumns[sortField]
	if !ok {
		write(w, http.StatusBadRequest, response{"error": "invalid_sort_field"})
		return
	}
	sortDirection := strings.ToUpper(r.URL.Query().Get("direction"))
	if sortDirection == "" {
		sortDirection = "ASC"
	}
	if sortDirection != "ASC" && sortDirection != "DESC" {
		write(w, http.StatusBadRequest, response{"error": "invalid_sort_direction"})
		return
	}

	where := []string{"1 = 1"}
	args := []any{}
	if search != "" {
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search)
		pattern := "%" + escaped + "%"
		where = append(where, "(hostname LIKE ? ESCAPE '\\' OR os_family LIKE ? ESCAPE '\\' OR os_version LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if osFamily != "" {
		where = append(where, "os_family = ?")
		args = append(args, osFamily)
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT count(*) FROM devices WHERE "+whereSQL, args...).Scan(&total); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	offset := (page - 1) * pageSize
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	query := "SELECT id, hostname, os_family, os_version, status, last_seen, first_connected_at, last_connected_at FROM devices WHERE " +
		whereSQL + " ORDER BY " + sortColumn + " " + sortDirection + ", id ASC LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(r.Context(), query, queryArgs...)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, hostname, osFamilyValue, osVersion, deviceStatus string
		var lastSeen, firstConnected, lastConnected sql.NullString
		if err := rows.Scan(&id, &hostname, &osFamilyValue, &osVersion, &deviceStatus, &lastSeen, &firstConnected, &lastConnected); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "database_error"})
			return
		}
		items = append(items, map[string]any{
			"id": id, "hostname": hostname, "os_family": osFamilyValue, "os_version": osVersion,
			"status": deviceStatus, "last_seen": lastSeen.String,
			"first_connected_at": firstConnected.String, "last_connected_at": lastConnected.String,
			"connection_state": s.connectionState(lastSeen),
		})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	write(w, http.StatusOK, response{
		"items": items, "count": len(items), "total": total, "page": page, "page_size": pageSize,
		"has_more": offset+len(items) < total,
	})
}

// deviceDetail returns the endpoint's current identity and the latest payload for
// every inventory category. It deliberately omits the device credential hash.
func (s *Server) deviceDetail(w http.ResponseWriter, r *http.Request) {
	var id, hostname, osFamily, osVersion, status, createdAt string
	var lastSeen, firstConnected, lastConnected sql.NullString
	if err := s.db.QueryRowContext(r.Context(), `SELECT id, hostname, os_family, os_version,
		status, last_seen, first_connected_at, last_connected_at, created_at
		FROM devices WHERE id = ?`, r.PathValue("id")).
		Scan(&id, &hostname, &osFamily, &osVersion, &status, &lastSeen, &firstConnected, &lastConnected, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			write(w, http.StatusNotFound, response{"error": "device_not_found"})
			return
		}
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}

	rows, err := s.db.QueryContext(r.Context(), `SELECT category, payload, observed_at, received_at
		FROM device_inventory WHERE device_id = ? ORDER BY category`, id)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_read_failed"})
		return
	}
	defer rows.Close()
	inventory := []map[string]any{}
	for rows.Next() {
		var category, observedAt, receivedAt string
		var payload []byte
		if err := rows.Scan(&category, &payload, &observedAt, &receivedAt); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "inventory_read_failed"})
			return
		}
		inventory = append(inventory, map[string]any{
			"category": category, "payload": json.RawMessage(payload),
			"observed_at": observedAt, "received_at": receivedAt,
		})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_read_failed"})
		return
	}
	write(w, http.StatusOK, response{
		"device": map[string]any{
			"id": id, "hostname": hostname, "os_family": osFamily, "os_version": osVersion,
			"status": status, "last_seen": lastSeen.String, "created_at": createdAt,
			"first_connected_at": firstConnected.String, "last_connected_at": lastConnected.String,
			"connection_state": s.connectionState(lastSeen),
		},
		"inventory": inventory,
	})
}

// inventoryHistory returns append-only inventory observations. The cursor is the
// previous page's oldest snapshot ID; callers can filter the timeline by category.
func (s *Server) inventoryHistory(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 500 {
			write(w, http.StatusBadRequest, response{"error": "invalid_limit", "max": 500})
			return
		}
		limit = parsed
	}
	beforeID := int64(0)
	if raw := r.URL.Query().Get("before_id"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed < 1 {
			write(w, http.StatusBadRequest, response{"error": "invalid_before_id"})
			return
		}
		beforeID = parsed
	}
	query := `SELECT id, category, payload, observed_at, received_at
		FROM device_inventory_history WHERE device_id = ?`
	args := []any{r.PathValue("id")}
	if category := strings.TrimSpace(r.URL.Query().Get("category")); category != "" {
		if len(category) > 80 {
			write(w, http.StatusBadRequest, response{"error": "invalid_category"})
			return
		}
		query += ` AND category = ?`
		args = append(args, category)
	}
	if beforeID > 0 {
		query += ` AND id < ?`
		args = append(args, beforeID)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := s.db.QueryContext(r.Context(), query, args...)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_history_read_failed"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var snapshotID int64
		var category, observedAt, receivedAt string
		var payload []byte
		if err := rows.Scan(&snapshotID, &category, &payload, &observedAt, &receivedAt); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "inventory_history_read_failed"})
			return
		}
		items = append(items, map[string]any{
			"id": snapshotID, "category": category, "payload": json.RawMessage(payload),
			"observed_at": observedAt, "received_at": receivedAt,
		})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_history_read_failed"})
		return
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}
	var nextBeforeID int64
	if hasMore && len(items) > 0 {
		nextBeforeID = items[len(items)-1]["id"].(int64)
	}
	write(w, http.StatusOK, response{
		"items": items, "count": len(items), "has_more": hasMore, "next_before_id": nextBeforeID,
	})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action    string          `json:"action"`
		DeviceIDs []string        `json:"device_ids"`
		Payload   json.RawMessage `json:"payload"`
	}
	if !decode(w, r, &in) {
		return
	}
	allowed := map[string]bool{
		"inventory.refresh": true, "patch.scan": true, "patch.install": true,
		"software.install": true, "software.uninstall": true, "configuration.apply": true,
		"service.control": true, "device.reboot": true, "device.shutdown": true, "agent.update": true,
	}
	if !allowed[in.Action] || len(in.DeviceIDs) == 0 || len(in.DeviceIDs) > 10000 {
		write(w, http.StatusBadRequest, response{"error": "invalid_action_or_targets"})
		return
	}
	payload := in.Payload
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		write(w, http.StatusBadRequest, response{"error": "invalid_payload"})
		return
	}
	id := uuid()
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(r.Context(), "INSERT INTO jobs(id, action, payload) VALUES (?, ?, ?)", id, in.Action, string(payload)); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_create_failed"})
		return
	}
	for _, deviceID := range in.DeviceIDs {
		if _, err = tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO job_targets(job_id, device_id)
			SELECT ?, id FROM devices WHERE id = ? AND status = 'approved'`, id, deviceID); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_target_failed"})
			return
		}
	}
	var count int
	if err = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM job_targets WHERE job_id = ?", id).Scan(&count); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_target_count_failed"})
		return
	}
	if count == 0 {
		write(w, http.StatusBadRequest, response{"error": "no_approved_targets"})
		return
	}
	if err = tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_commit_failed"})
		return
	}
	write(w, http.StatusAccepted, response{"job_id": id, "status": "queued", "target_count": count})
}

func (s *Server) nextJob(w http.ResponseWriter, r *http.Request) {
	var jobID string
	// A single UPDATE ... RETURNING atomically claims a queued job target. SQLite
	// has one writer at a time; this avoids PostgreSQL-only SKIP LOCKED semantics.
	err := s.db.QueryRowContext(r.Context(), `UPDATE job_targets
		SET status = 'received', updated_at = CURRENT_TIMESTAMP
		WHERE rowid = (
			SELECT t.rowid FROM job_targets t
			JOIN jobs j ON j.id = t.job_id
			WHERE t.device_id = ? AND t.status = 'queued' AND j.status = 'queued'
			ORDER BY j.created_at LIMIT 1
		)
		AND status = 'queued'
		RETURNING job_id`, r.PathValue("id")).Scan(&jobID)
	if errors.Is(err, sql.ErrNoRows) {
		write(w, http.StatusOK, response{"job": nil})
		return
	}
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_claim_failed"})
		return
	}
	var action string
	var payload []byte
	if err := s.db.QueryRowContext(r.Context(), "SELECT action, payload FROM jobs WHERE id = ?", jobID).Scan(&action, &payload); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_load_failed"})
		return
	}
	write(w, http.StatusOK, response{"job": map[string]any{"id": jobID, "action": action, "payload": json.RawMessage(payload)}})
}

func (s *Server) jobResult(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Status string          `json:"status"`
		Result json.RawMessage `json:"result"`
		Error  string          `json:"error"`
	}
	if !decode(w, r, &in) {
		return
	}
	allowed := map[string]bool{"started": true, "succeeded": true, "failed": true, "cancelled": true}
	if !allowed[in.Status] {
		write(w, http.StatusBadRequest, response{"error": "invalid_status"})
		return
	}
	if len(in.Result) == 0 {
		in.Result = json.RawMessage(`{}`)
	}
	if !json.Valid(in.Result) {
		write(w, http.StatusBadRequest, response{"error": "invalid_result"})
		return
	}
	state := in.Status
	if state == "started" {
		state = "running"
	}
	result, err := s.db.ExecContext(r.Context(), `UPDATE job_targets
		SET status = ?, result = ?, error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE job_id = ? AND device_id = ?`, state, string(in.Result), in.Error, r.PathValue("job"), r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "result_update_failed"})
		return
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		write(w, http.StatusNotFound, response{"error": "job_target_not_found"})
		return
	}
	_, err = s.db.ExecContext(r.Context(), `UPDATE jobs SET status = CASE
		WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status IN ('queued', 'received', 'running')) THEN
			CASE WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status = 'running') THEN 'running' ELSE 'queued' END
		WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status IN ('failed', 'cancelled')) THEN 'completed_with_errors'
		ELSE 'completed' END WHERE id = ?`,
		r.PathValue("job"), r.PathValue("job"), r.PathValue("job"), r.PathValue("job"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_aggregation_failed"})
		return
	}
	write(w, http.StatusOK, response{"status": "recorded"})
}

func (s *Server) getJob(w http.ResponseWriter, r *http.Request) {
	var action, status string
	if err := s.db.QueryRowContext(r.Context(), "SELECT action, status FROM jobs WHERE id = ?", r.PathValue("id")).Scan(&action, &status); err != nil {
		write(w, http.StatusNotFound, response{"error": "job_not_found"})
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT device_id, status, error FROM job_targets WHERE job_id = ?", r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var deviceID, state string
		var message sql.NullString
		if err := rows.Scan(&deviceID, &state, &message); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "database_error"})
			return
		}
		items = append(items, map[string]any{"device_id": deviceID, "status": state, "error": message.String})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	write(w, http.StatusOK, response{"job_id": r.PathValue("id"), "action": action, "status": status, "executions": items})
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8<<20)
	defer r.Body.Close()
	if err := json.NewDecoder(r.Body).Decode(target); err != nil {
		write(w, http.StatusBadRequest, response{"error": "invalid_json"})
		return false
	}
	return true
}

func write(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func bearer(r *http.Request, expected string) bool {
	header := r.Header.Get("Authorization")
	return strings.HasPrefix(header, "Bearer ") && constantString(strings.TrimPrefix(header, "Bearer "), expected)
}

func token(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func constantString(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var mismatch byte
	for i := range a {
		mismatch |= a[i] ^ b[i]
	}
	return mismatch == 0
}

func hash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func constantHashMatch(expected, provided string) bool {
	return constantString(expected, hash(provided))
}

func randomToken() string {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	return hex.EncodeToString(buffer)
}

func uuid() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}
	buffer[6] = (buffer[6] & 0x0f) | 0x40
	buffer[8] = (buffer[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(buffer)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:])
}

func str(value any) string {
	text, _ := value.(string)
	return text
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
