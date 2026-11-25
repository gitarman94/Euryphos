package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Server struct {
	db                      *sql.DB
	adminToken, enrollToken string
	onlineGrace             time.Duration
	cookieSecure            bool
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
	s := &Server{db: db, adminToken: admin, enrollToken: enroll, onlineGrace: time.Duration(graceSeconds) * time.Second,
		cookieSecure: !strings.EqualFold(env("EURYPHOS_COOKIE_SECURE", "true"), "false")}
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
	mux.HandleFunc("GET /{$}", s.root)
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("GET /console", s.admin(s.consolePage))
	mux.HandleFunc("GET /console/", s.admin(s.consolePage))
	mux.HandleFunc("GET /assets/login.js", s.loginJS)
	mux.HandleFunc("GET /assets/app.js", s.consoleJS)
	mux.HandleFunc("GET /assets/app.css", s.consoleCSS)
	mux.HandleFunc("POST /api/v1/auth/session", s.authSession)
	mux.HandleFunc("POST /api/v1/auth/logout", s.admin(s.logoutSession))
	mux.HandleFunc("GET /api/v1/audit", s.admin(s.auditList))
	mux.HandleFunc("POST /api/v1/enrollment", s.enroll)
	mux.HandleFunc("POST /api/v1/devices/{id}/approve", s.approve)
	mux.HandleFunc("POST /api/v1/devices/{id}/revoke", s.revoke)
	mux.HandleFunc("POST /api/v1/devices/{id}/merge", s.admin(s.mergeDevice))
	mux.HandleFunc("GET /api/v1/devices/{id}/duplicates", s.admin(s.deviceDuplicates))
	mux.HandleFunc("POST /api/v1/devices/{id}/heartbeat", s.deviceAuth(s.heartbeat))
	mux.HandleFunc("POST /api/v1/devices/{id}/inventory", s.deviceAuth(s.inventory))
	mux.HandleFunc("GET /api/v1/devices", s.admin(s.devices))
	mux.HandleFunc("GET /api/v1/devices/{id}", s.admin(s.deviceDetail))
	mux.HandleFunc("GET /api/v1/devices/{id}/inventory/history", s.admin(s.inventoryHistory))
	mux.HandleFunc("GET /api/v1/jobs", s.admin(s.listJobs))
	mux.HandleFunc("POST /api/v1/jobs", s.admin(s.createJob))
	mux.HandleFunc("GET /api/v1/devices/{id}/jobs/next", s.deviceAuth(s.nextJob))
	mux.HandleFunc("POST /api/v1/devices/{id}/jobs/{job}/result", s.deviceAuth(s.jobResult))
	mux.HandleFunc("GET /api/v1/jobs/{id}", s.admin(s.getJob))
	mux.HandleFunc("GET /api/v1/groups", s.admin(s.listGroups))
	mux.HandleFunc("POST /api/v1/groups", s.admin(s.createGroup))
	mux.HandleFunc("PUT /api/v1/groups/{id}", s.admin(s.updateGroup))
	mux.HandleFunc("DELETE /api/v1/groups/{id}", s.admin(s.deleteGroup))
	mux.HandleFunc("GET /api/v1/groups/{id}/members", s.admin(s.groupMembers))
	mux.HandleFunc("POST /api/v1/groups/{id}/members", s.admin(s.addGroupMembers))
	mux.HandleFunc("DELETE /api/v1/groups/{id}/members/{deviceID}", s.admin(s.removeGroupMember))
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
			database_id INTEGER,
			hostname TEXT NOT NULL DEFAULT '',
			fqdn TEXT NOT NULL DEFAULT '',
			os_family TEXT NOT NULL DEFAULT '',
			os_version TEXT NOT NULL DEFAULT '',
			os_architecture TEXT NOT NULL DEFAULT '',
			os_build TEXT NOT NULL DEFAULT '',
			os_install_date TEXT NOT NULL DEFAULT '',
			directory_type TEXT NOT NULL DEFAULT '',
			directory_domain TEXT NOT NULL DEFAULT '',
			organizational_unit TEXT NOT NULL DEFAULT '',
			directory_site TEXT NOT NULL DEFAULT '',
			manufacturer TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			serial_number TEXT NOT NULL DEFAULT '',
			smbios_uuid TEXT NOT NULL DEFAULT '',
			chassis_type TEXT NOT NULL DEFAULT '',
			cpu_model TEXT NOT NULL DEFAULT '',
			cpu_vendor TEXT NOT NULL DEFAULT '',
			cpu_architecture TEXT NOT NULL DEFAULT '',
			cpu_physical_cores INTEGER,
			cpu_logical_processors INTEGER,
			memory_total_bytes INTEGER,
			bios_version TEXT NOT NULL DEFAULT '',
			bios_release_date TEXT NOT NULL DEFAULT '',
			root_total_bytes INTEGER,
			root_free_bytes INTEGER,
			root_available_bytes INTEGER,
			last_boot TEXT NOT NULL DEFAULT '',
			uptime_seconds REAL,
			timezone TEXT NOT NULL DEFAULT '',
			agent_version TEXT NOT NULL DEFAULT '',
			local_ip TEXT NOT NULL DEFAULT '',
			mac_address TEXT NOT NULL DEFAULT '',
			default_gateway TEXT NOT NULL DEFAULT '',
			dns_servers_json TEXT NOT NULL DEFAULT '[]',
			connection_type TEXT NOT NULL DEFAULT '',
			duplicate_of TEXT,
			merged_into TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			token_hash TEXT,
			last_seen TEXT,
			first_connected_at TEXT,
			last_connected_at TEXT,
			last_inventory_at TEXT,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS device_duplicate_candidates (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id),
			candidate_device_id TEXT NOT NULL REFERENCES devices(id),
			matched_on TEXT NOT NULL,
			confidence TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(device_id, candidate_device_id)
		)`,
		`CREATE TABLE IF NOT EXISTS audit_log (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			occurred_at TEXT NOT NULL,
			actor_type TEXT NOT NULL,
			actor_id TEXT NOT NULL DEFAULT '',
			event_type TEXT NOT NULL,
			outcome TEXT NOT NULL,
			target_type TEXT NOT NULL DEFAULT '',
			target_id TEXT NOT NULL DEFAULT '',
			source_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			details_json TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_target ON audit_log(target_type, target_id, occurred_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_log_event ON audit_log(event_type, occurred_at DESC)`,
		`CREATE TABLE IF NOT EXISTS machine_groups (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL COLLATE NOCASE UNIQUE,
			description TEXT NOT NULL DEFAULT '',
			group_type TEXT NOT NULL CHECK(group_type IN ('static','dynamic')),
			match_operator TEXT NOT NULL DEFAULT 'all' CHECK(match_operator IN ('all','any')),
			rules_json TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS device_group_memberships (
			group_id INTEGER NOT NULL REFERENCES machine_groups(id) ON DELETE CASCADE,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			added_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			added_by TEXT NOT NULL DEFAULT '',
			PRIMARY KEY(group_id, device_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_group_memberships_device ON device_group_memberships(device_id)`,
		`CREATE TABLE IF NOT EXISTS device_network_interfaces (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			interface_key TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			mac_address TEXT NOT NULL DEFAULT '',
			connection_type TEXT NOT NULL DEFAULT '',
			state TEXT NOT NULL DEFAULT '',
			addresses_json TEXT NOT NULL DEFAULT '[]',
			dhcp_enabled INTEGER NOT NULL DEFAULT 0,
			dhcp_server TEXT NOT NULL DEFAULT '',
			dhcp_lease_expires TEXT NOT NULL DEFAULT '',
			dhcp_lease_expiry_estimated INTEGER NOT NULL DEFAULT 0,
			mtu INTEGER,
			observed_at TEXT NOT NULL,
			UNIQUE(device_id, interface_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_network_interfaces_device ON device_network_interfaces(device_id)`,
		`CREATE TABLE IF NOT EXISTS device_storage_disks (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			disk_key TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			manufacturer TEXT NOT NULL DEFAULT '',
			serial_number TEXT NOT NULL DEFAULT '',
			interface_type TEXT NOT NULL DEFAULT '',
			capacity_bytes INTEGER,
			logical_block_size INTEGER,
			is_removable INTEGER,
			is_rotational INTEGER,
			status TEXT NOT NULL DEFAULT '',
			observed_at TEXT NOT NULL,
			UNIQUE(device_id,disk_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_disks_device ON device_storage_disks(device_id)`,
		`CREATE TABLE IF NOT EXISTS device_storage_volumes (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			volume_key TEXT NOT NULL,
			name TEXT NOT NULL DEFAULT '',
			parent_key TEXT NOT NULL DEFAULT '',
			device TEXT NOT NULL DEFAULT '',
			filesystem TEXT NOT NULL DEFAULT '',
			mount_point TEXT NOT NULL DEFAULT '',
			volume_label TEXT NOT NULL DEFAULT '',
			capacity_bytes INTEGER,
			free_bytes INTEGER,
			available_bytes INTEGER,
			used_bytes INTEGER,
			block_size INTEGER,
			is_read_only INTEGER,
			status TEXT NOT NULL DEFAULT '',
			observed_at TEXT NOT NULL,
			UNIQUE(device_id,volume_key)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_volumes_device ON device_storage_volumes(device_id)`,
		`CREATE INDEX IF NOT EXISTS idx_storage_volumes_mount ON device_storage_volumes(mount_point)`,
		`CREATE TABLE IF NOT EXISTS installed_software (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			name_normalized TEXT NOT NULL,
			version TEXT NOT NULL DEFAULT '',
			publisher TEXT NOT NULL DEFAULT '',
			package_manager TEXT NOT NULL DEFAULT '',
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			UNIQUE(device_id, name_normalized, version, package_manager)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_installed_software_name_version ON installed_software(name_normalized, version)`,
		`CREATE INDEX IF NOT EXISTS idx_installed_software_device ON installed_software(device_id)`,
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
		`CREATE TABLE IF NOT EXISTS admin_sessions (
			token_hash TEXT PRIMARY KEY,
			csrf_hash TEXT NOT NULL,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
			source_ip TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_admin_sessions_expiry ON admin_sessions(expires_at)`,
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
		`CREATE TABLE IF NOT EXISTS job_group_targets (
			job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			group_id INTEGER NOT NULL,
			group_name TEXT NOT NULL,
			PRIMARY KEY(job_id, group_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_job_group_targets_group ON job_group_targets(group_id)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname)`,
		`CREATE INDEX IF NOT EXISTS idx_inventory_category ON device_inventory(category)`,
		`CREATE INDEX IF NOT EXISTS idx_job_targets_device_status ON job_targets(device_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (1)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (2)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (3)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (4)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (5)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (6)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (7)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (8)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (9)`,
		`INSERT OR IGNORE INTO schema_migrations(version) VALUES (10)`,
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
		{name: "database_id", definition: "INTEGER"},
		{name: "fqdn", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "os_distribution", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "os_distribution_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "kernel_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "os_architecture", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "os_build", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "os_install_date", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "directory_type", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "directory_domain", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "organizational_unit", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "directory_site", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "manufacturer", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "model", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "serial_number", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "smbios_uuid", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "chassis_type", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "cpu_model", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "cpu_vendor", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "cpu_architecture", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "cpu_physical_cores", definition: "INTEGER"},
		{name: "cpu_logical_processors", definition: "INTEGER"},
		{name: "memory_total_bytes", definition: "INTEGER"},
		{name: "bios_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "bios_release_date", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "root_total_bytes", definition: "INTEGER"},
		{name: "root_free_bytes", definition: "INTEGER"},
		{name: "root_available_bytes", definition: "INTEGER"},
		{name: "last_boot", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "uptime_seconds", definition: "REAL"},
		{name: "timezone", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "agent_version", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "local_ip", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "mac_address", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "default_gateway", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "dns_servers_json", definition: "TEXT NOT NULL DEFAULT '[]'"},
		{name: "connection_type", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "duplicate_of", definition: "TEXT"},
		{name: "merged_into", definition: "TEXT"},
		{name: "last_inventory_at", definition: "TEXT"},
	} {
		if err := ensureDeviceColumn(ctx, tx, column.name, column.definition); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		definition string
	}{
		{name: "dhcp_enabled", definition: "INTEGER NOT NULL DEFAULT 0"},
		{name: "dhcp_server", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "dhcp_lease_expires", definition: "TEXT NOT NULL DEFAULT ''"},
		{name: "dhcp_lease_expiry_estimated", definition: "INTEGER NOT NULL DEFAULT 0"},
	} {
		if err := ensureTableColumn(ctx, tx, "device_network_interfaces", column.name, column.definition); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "UPDATE devices SET database_id = rowid WHERE database_id IS NULL"); err != nil {
		return err
	}
	for _, statement := range []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_database_id ON devices(database_id)`,
		`CREATE TRIGGER IF NOT EXISTS trg_devices_database_id_insert
			AFTER INSERT ON devices WHEN NEW.database_id IS NULL
			BEGIN UPDATE devices SET database_id = NEW.rowid WHERE rowid = NEW.rowid; END`,
		`CREATE INDEX IF NOT EXISTS idx_devices_serial_number ON devices(serial_number)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_smbios_uuid ON devices(smbios_uuid)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_smbios_uuid_normalized
			ON devices(lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ','')))`,
		`CREATE INDEX IF NOT EXISTS idx_devices_hardware_identity
			ON devices(lower(trim(serial_number)),lower(trim(manufacturer)),lower(trim(model)))`,
		`CREATE INDEX IF NOT EXISTS idx_devices_directory_domain_ou ON devices(directory_domain, organizational_unit)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_smbios_uuid_normalized ON devices(lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ','')))`,
		`CREATE INDEX IF NOT EXISTS idx_devices_hardware_identity ON devices(serial_number COLLATE NOCASE, manufacturer COLLATE NOCASE, model COLLATE NOCASE)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_os_family_distribution ON devices(os_family, os_distribution, os_distribution_version)`,
		`CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen)`,
		`CREATE INDEX IF NOT EXISTS idx_network_interfaces_mac ON device_network_interfaces(mac_address)`,
		`CREATE INDEX IF NOT EXISTS idx_machine_groups_type ON machine_groups(group_type)`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func ensureDeviceColumn(ctx context.Context, tx *sql.Tx, columnName, definition string) error {
	return ensureTableColumn(ctx, tx, "devices", columnName, definition)
}

func ensureTableColumn(ctx context.Context, tx *sql.Tx, tableName, columnName, definition string) error {
	if tableName != "devices" && tableName != "device_network_interfaces" {
		return errors.New("unsupported migration table")
	}
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+tableName+")")
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
	_, err = tx.ExecContext(ctx, "ALTER TABLE "+tableName+" ADD COLUMN "+columnName+" "+definition)
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

type auditExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func auditEvent(ctx context.Context, exec auditExecutor, r *http.Request, actorType, actorID, eventType, outcome, targetType, targetID string, details any) error {
	payload := "{}"
	if details != nil {
		raw, err := json.Marshal(details)
		if err != nil {
			return err
		}
		if len(raw) > 16384 {
			return errors.New("audit details exceed 16 KiB")
		}
		payload = string(raw)
	}
	sourceIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		sourceIP = host
	}
	userAgent := r.UserAgent()
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO audit_log(
		occurred_at, actor_type, actor_id, event_type, outcome, target_type, target_id,
		source_ip, user_agent, details_json
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339Nano), actorType, actorID, eventType,
		outcome, targetType, targetID, sourceIP, userAgent, payload)
	return err
}

const adminSessionLifetime = 8 * time.Hour

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	if !bearer(r, s.adminToken) {
		_ = auditEvent(r.Context(), s.db, r, "admin", "", "auth.admin.failure", "denied", "session", "", map[string]any{"reason": "invalid_credential"})
		write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
		return
	}
	sessionToken := randomToken()
	csrfToken := randomToken()
	now := time.Now().UTC()
	expires := now.Add(adminSessionLifetime)
	expiresText := expires.Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "session_create_failed"})
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM admin_sessions WHERE expires_at<=?", now.Format(time.RFC3339Nano)); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "session_cleanup_failed"})
		return
	}
	sourceIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		sourceIP = host
	}
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO admin_sessions(token_hash,csrf_hash,expires_at,source_ip) VALUES(?,?,?,?)",
		hash(sessionToken), hash(csrfToken), expiresText, sourceIP); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "session_create_failed"})
		return
	}
	if err := auditEvent(r.Context(), tx, r, "admin", "configured-admin", "auth.admin.success", "success", "session", "", nil); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "session_commit_failed"})
		return
	}
	s.setSessionCookies(w, sessionToken, csrfToken, expires)
	write(w, http.StatusOK, response{"status": "authenticated", "expires_at": expiresText})
}

func (s *Server) setSessionCookies(w http.ResponseWriter, sessionToken, csrfToken string, expires time.Time) {
	maxAge := int(time.Until(expires).Seconds())
	if maxAge < 0 { maxAge = 0 }
	for _, cookie := range []*http.Cookie{
		{Name: "euryphos_admin_session", Value: sessionToken, Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge},
		{Name: "euryphos_csrf", Value: csrfToken, Path: "/", HttpOnly: false, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, Expires: expires, MaxAge: maxAge},
	} {
		http.SetCookie(w, cookie)
	}
}

func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	for _, cookie := range []*http.Cookie{
		{Name: "euryphos_admin_session", Value: "", Path: "/", HttpOnly: true, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1},
		{Name: "euryphos_csrf", Value: "", Path: "/", HttpOnly: false, Secure: s.cookieSecure, SameSite: http.SameSiteStrictMode, MaxAge: -1},
	} {
		http.SetCookie(w, cookie)
	}
}

func (s *Server) adminAuthorization(r *http.Request) (bool, bool) {
	if bearer(r, s.adminToken) {
		return true, false
	}
	sessionCookie, err := r.Cookie("euryphos_admin_session")
	if err != nil || sessionCookie.Value == "" {
		return false, false
	}
	var csrfHash, expiresText string
	if err := s.db.QueryRowContext(r.Context(), "SELECT csrf_hash,expires_at FROM admin_sessions WHERE token_hash=?",
		hash(sessionCookie.Value)).Scan(&csrfHash, &expiresText); err != nil {
		return false, false
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresText)
	if err != nil || !time.Now().Before(expires) {
		_, _ = s.db.ExecContext(r.Context(), "DELETE FROM admin_sessions WHERE token_hash=?", hash(sessionCookie.Value))
		return false, false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true, false
	}
	csrfCookie, cookieErr := r.Cookie("euryphos_csrf")
	headerToken := r.Header.Get("X-Euryphos-CSRF")
	if cookieErr != nil || headerToken == "" || !constantString(csrfCookie.Value, headerToken) ||
		!constantHashMatch(csrfHash, headerToken) {
		return true, true
	}
	return true, false
}

func (s *Server) logoutSession(w http.ResponseWriter, r *http.Request) {
	if sessionCookie, err := r.Cookie("euryphos_admin_session"); err == nil && sessionCookie.Value != "" {
		tx, err := s.db.BeginTx(r.Context(), nil)
		if err != nil {
			write(w, http.StatusInternalServerError, response{"error": "logout_failed"})
			return
		}
		defer tx.Rollback()
		if _, err := tx.ExecContext(r.Context(), "DELETE FROM admin_sessions WHERE token_hash=?", hash(sessionCookie.Value)); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "logout_failed"})
			return
		}
		if err := auditEvent(r.Context(), tx, r, "admin", "configured-admin", "auth.admin.logout", "success", "session", "", nil); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
			return
		}
		if err := tx.Commit(); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "logout_commit_failed"})
			return
		}
	}
	s.clearSessionCookies(w)
	write(w, http.StatusOK, response{"status": "logged_out"})
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authorized, csrfFailure := s.adminAuthorization(r)
		if !authorized {
			_ = auditEvent(r.Context(), s.db, r, "admin", "", "auth.admin.failure", "denied", "api", r.URL.Path, map[string]any{"method": r.Method})
			write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
			return
		}
		if csrfFailure {
			_ = auditEvent(r.Context(), s.db, r, "admin", "configured-admin", "auth.csrf.failure", "denied", "api", r.URL.Path, map[string]any{"method": r.Method})
			write(w, http.StatusForbidden, response{"error": "csrf_validation_failed"})
			return
		}
		next(w, r)
	}
}

func (s *Server) deviceAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var storedHash, status, mergedInto string
		err := s.db.QueryRowContext(r.Context(), "SELECT COALESCE(token_hash, ''), status, COALESCE(merged_into, '') FROM devices WHERE id = ?", r.PathValue("id")).Scan(&storedHash, &status, &mergedInto)
		validStatus := status == "approved" || (status == "merged" && mergedInto != "")
		if err != nil || !validStatus || !constantHashMatch(storedHash, token(r)) {
			_ = auditEvent(r.Context(), s.db, r, "device", r.PathValue("id"), "auth.device.failure", "denied", "device", r.PathValue("id"), nil)
			write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
			return
		}
		if mergedInto != "" {
			var canonicalStatus string
			if err := s.db.QueryRowContext(r.Context(), "SELECT status FROM devices WHERE id = ?", mergedInto).Scan(&canonicalStatus); err != nil || canonicalStatus != "approved" {
				write(w, http.StatusUnauthorized, response{"error": "device_merged_target_unavailable"})
				return
			}
			r.SetPathValue("id", mergedInto)
		}
		next(w, r)
	}
}

func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	if !constantString(token(r), s.enrollToken) {
		_ = auditEvent(r.Context(), s.db, r, "enrollment", "", "auth.enrollment.failure", "denied", "enrollment", "", nil)
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
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(),
		"INSERT INTO devices(id, hostname, os_family, os_version, token_hash) VALUES (?, ?, ?, ?, ?)",
		id, in.Hostname, in.OSFamily, in.OSVersion, hash(secret)); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "enrollment_failed"})
		return
	}
	if err := auditEvent(r.Context(), tx, r, "enrollment", "enrollment-token", "device.enrolled", "pending_approval", "device", id, map[string]any{"hostname": in.Hostname, "os_family": in.OSFamily}); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "enrollment_commit_failed"})
		return
	}
	write(w, http.StatusCreated, response{"device_id": id, "device_token": secret, "status": "pending_approval"})
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil { write(w, http.StatusInternalServerError, response{"error":"database_error"}); return }
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE devices SET status = 'approved' WHERE id = ? AND status = 'pending'", r.PathValue("id"))
	if err != nil { write(w, http.StatusInternalServerError, response{"error":"database_error"}); return }
	count, _ := result.RowsAffected()
	if count == 0 { write(w,http.StatusNotFound,response{"error":"pending_device_not_found"});return }
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","device.approved","success","device",r.PathValue("id"),nil);err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"device_approval_commit_failed"});return }
	write(w,http.StatusOK, response{"status": "approved"})
}

func (s *Server) revoke(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE devices SET status = 'revoked', token_hash = NULL WHERE id = ? AND merged_into IS NULL", r.PathValue("id"))
	if err != nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	count, _ := result.RowsAffected()
	if count == 0 { write(w,http.StatusNotFound,response{"error":"device_not_found"});return }
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","device.revoked","success","device",r.PathValue("id"),nil);err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"device_revocation_commit_failed"});return }
	write(w, http.StatusOK, response{"status": "revoked"})
}

func (s *Server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	authorized, csrfFailure := s.adminAuthorization(r)
	if !authorized {
		_ = auditEvent(r.Context(), s.db, r, "admin", "", "auth.admin.failure", "denied", "api", r.URL.Path, map[string]any{"method": r.Method})
		write(w, http.StatusUnauthorized, response{"error": "unauthorized"})
		return false
	}
	if csrfFailure {
		_ = auditEvent(r.Context(), s.db, r, "admin", "configured-admin", "auth.csrf.failure", "denied", "api", r.URL.Path, map[string]any{"method": r.Method})
		write(w, http.StatusForbidden, response{"error": "csrf_validation_failed"})
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
	var previousPayload string
	previousErr := tx.QueryRowContext(r.Context(), "SELECT payload FROM device_inventory WHERE device_id=? AND category=?",
		r.PathValue("id"), in.Category).Scan(&previousPayload)
	unchangedPayload := previousErr == nil && previousPayload == string(in.Payload)
	if previousErr != nil && !errors.Is(previousErr, sql.ErrNoRows) {
		write(w, http.StatusInternalServerError, response{"error":"inventory_current_read_failed"})
		return
	}
	historyChanged := previousErr != nil ||
		!bytes.Equal(inventoryHistoryFingerprint(in.Category, []byte(previousPayload)),
			inventoryHistoryFingerprint(in.Category, in.Payload))
	if historyChanged {
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO device_inventory_history(device_id, category, payload, observed_at)
			VALUES (?, ?, ?, ?)`, r.PathValue("id"), in.Category, string(in.Payload), observedAt); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "inventory_history_write_failed"})
			return
		}
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
	deviceID := r.PathValue("id")
	var normalizeErr error
	if unchangedPayload && (in.Category=="network" || in.Category=="software") {
		if in.Category=="software" {
			_,normalizeErr=tx.ExecContext(r.Context(),"UPDATE installed_software SET last_seen_at=? WHERE device_id=?",observedAt,deviceID)
		}
	} else {
		switch in.Category {
		case "system":
			normalizeErr = s.applySystemInventory(r.Context(), tx, deviceID, in.Payload, observedAt)
		case "network":
			normalizeErr = s.applyNetworkInventory(r.Context(), tx, deviceID, in.Payload, observedAt)
		case "software":
			normalizeErr = s.applySoftwareInventory(r.Context(), tx, deviceID, in.Payload, observedAt)
		case "storage":
			normalizeErr = s.applyStorageInventory(r.Context(), tx, deviceID, in.Payload, observedAt)
		}
	}
	if normalizeErr != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_normalization_failed"})
		return
	}
	if in.Category == "system" && historyChanged {
		if err := s.refreshDynamicMemberships(r.Context(), tx, deviceID); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "group_membership_refresh_failed"})
			return
		}
	}
	receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(r.Context(), `UPDATE devices
		SET last_seen = ?,
		    first_connected_at = COALESCE(first_connected_at, ?),
		    last_connected_at = ?,
		    last_inventory_at = ?
		WHERE id = ?`, receivedAt, receivedAt, receivedAt, receivedAt, r.PathValue("id")); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "device_last_seen_update_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "inventory_commit_failed"})
		return
	}
	write(w, http.StatusAccepted, response{"status": "accepted"})
}

func inventoryHistoryFingerprint(category string, payload []byte) []byte {
	if category != "system" {
		return payload
	}
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		return payload
	}
	delete(data, "observed_at")
	delete(data, "uptime_seconds")
	delete(data, "last_boot")
	if memory, ok := data["memory"].(map[string]any); ok {
		delete(memory, "available_bytes")
		delete(memory, "used_bytes")
	}
	if root, ok := data["root_filesystem"].(map[string]any); ok {
		delete(root, "free_bytes")
		delete(root, "available_bytes")
		delete(root, "used_bytes")
	}
	if batteries, ok := data["batteries"].([]any); ok {
		for _, raw := range batteries {
			if battery, ok := raw.(map[string]any); ok {
				delete(battery, "status")
				delete(battery, "capacity_percent")
			}
		}
	}
	canonical, err := json.Marshal(data)
	if err != nil {
		return payload
	}
	return canonical
}

type groupRule struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil { return map[string]any{} }
	return result
}

func textValue(value any) string {
	if value == nil { return "" }
	return strings.TrimSpace(fmt.Sprint(value))
}

func integerValue(value any) any {
	switch number := value.(type) {
	case float64:
		if number < 0 || number > 9.22e18 { return nil }
		return int64(number)
	case int:
		return int64(number)
	case int64:
		return number
	case json.Number:
		result, err := number.Int64()
		if err == nil { return result }
	}
	return nil
}

func floatValue(value any) any {
	switch number := value.(type) {
	case float64:
		return number
	case int:
		return float64(number)
	case int64:
		return float64(number)
	case json.Number:
		result, err := strconv.ParseFloat(number.String(), 64)
		if err == nil { return result }
	}
	return nil
}

func (s *Server) applySystemInventory(ctx context.Context, tx *sql.Tx, deviceID string, payload []byte, observedAt string) error {
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil { return err }
	hardware := mapValue(data["hardware"])
	memory := mapValue(data["memory"])
	root := mapValue(data["root_filesystem"])
	var oldSerial, oldUUID, oldManufacturer, oldModel string
	if err := tx.QueryRowContext(ctx, "SELECT serial_number,smbios_uuid,manufacturer,model FROM devices WHERE id=?", deviceID).
		Scan(&oldSerial,&oldUUID,&oldManufacturer,&oldModel); err != nil { return err }
	newSerial := usableIdentity(textValue(hardware["serial_number"]))
	newUUID := normalizeIdentity(usableIdentity(textValue(hardware["smbios_uuid"])))
	if strings.Trim(newUUID,"0")=="" || strings.Trim(newUUID,"f")=="" { newUUID="" }
	newManufacturer := usableIdentity(textValue(hardware["manufacturer"]))
	newModel := usableIdentity(textValue(hardware["model"]))
	oldNormalizedUUID := normalizeIdentity(oldUUID)
	if strings.Trim(oldNormalizedUUID,"0")=="" || strings.Trim(oldNormalizedUUID,"f")=="" { oldNormalizedUUID="" }
	identityChanged :=
		(newSerial!="" && newSerial!=usableIdentity(oldSerial)) ||
		(newUUID!="" && newUUID!=oldNormalizedUUID) ||
		(newManufacturer!="" && newManufacturer!=usableIdentity(oldManufacturer)) ||
		(newModel!="" && newModel!=usableIdentity(oldModel))
	fields := []any{
		textValue(data["hostname"]), textValue(data["fqdn"]),
		textValue(data["os_family"]), textValue(data["os_version"]),
		textValue(data["architecture"]), textValue(data["os_build"]),
		textValue(data["os_install_date"]), textValue(data["os_distribution"]),
		textValue(data["os_distribution_version"]), textValue(data["kernel_version"]),
		textValue(data["directory_type"]), textValue(data["directory_domain"]),
		textValue(data["organizational_unit"]), textValue(data["directory_site"]),
		textValue(hardware["manufacturer"]), textValue(hardware["model"]),
		textValue(hardware["serial_number"]), strings.ToLower(textValue(hardware["smbios_uuid"])),
		textValue(hardware["chassis_type"]), textValue(hardware["cpu_model"]),
		textValue(hardware["cpu_vendor"]), textValue(data["architecture"]),
		integerValue(hardware["physical_cores"]), integerValue(hardware["logical_processors"]),
		integerValue(memory["total_bytes"]), textValue(hardware["bios_version"]),
		textValue(hardware["bios_release_date"]), integerValue(root["total_bytes"]),
		integerValue(root["free_bytes"]), integerValue(root["available_bytes"]),
		textValue(data["last_boot"]), floatValue(data["uptime_seconds"]), textValue(data["timezone"]),
		textValue(data["agent_version"]), observedAt,
	}
	_, err := tx.ExecContext(ctx, `UPDATE devices SET
		hostname=COALESCE(NULLIF(?,''),hostname), fqdn=COALESCE(NULLIF(?,''),fqdn),
		os_family=COALESCE(NULLIF(?,''),os_family), os_version=COALESCE(NULLIF(?,''),os_version),
		os_architecture=COALESCE(NULLIF(?,''),os_architecture), os_build=COALESCE(NULLIF(?,''),os_build),
		os_install_date=COALESCE(NULLIF(?,''),os_install_date),
		os_distribution=COALESCE(NULLIF(?,''),os_distribution),
		os_distribution_version=COALESCE(NULLIF(?,''),os_distribution_version),
		kernel_version=COALESCE(NULLIF(?,''),kernel_version),
		directory_type=COALESCE(NULLIF(?,''),directory_type),
		directory_domain=COALESCE(NULLIF(?,''),directory_domain),
		organizational_unit=COALESCE(NULLIF(?,''),organizational_unit),
		directory_site=COALESCE(NULLIF(?,''),directory_site),
		manufacturer=COALESCE(NULLIF(?,''),manufacturer), model=COALESCE(NULLIF(?,''),model),
		serial_number=COALESCE(NULLIF(?,''),serial_number),
		smbios_uuid=COALESCE(NULLIF(?,''),smbios_uuid),
		chassis_type=COALESCE(NULLIF(?,''),chassis_type),
		cpu_model=COALESCE(NULLIF(?,''),cpu_model), cpu_vendor=COALESCE(NULLIF(?,''),cpu_vendor),
		cpu_architecture=COALESCE(NULLIF(?,''),cpu_architecture),
		cpu_physical_cores=COALESCE(?,cpu_physical_cores),
		cpu_logical_processors=COALESCE(?,cpu_logical_processors),
		memory_total_bytes=COALESCE(?,memory_total_bytes),
		bios_version=COALESCE(NULLIF(?,''),bios_version),
		bios_release_date=COALESCE(NULLIF(?,''),bios_release_date),
		root_total_bytes=COALESCE(?,root_total_bytes), root_free_bytes=COALESCE(?,root_free_bytes),
		root_available_bytes=COALESCE(?,root_available_bytes),
		last_boot=COALESCE(NULLIF(?,''),last_boot), uptime_seconds=COALESCE(?,uptime_seconds),
		timezone=COALESCE(NULLIF(?,''),timezone), agent_version=COALESCE(NULLIF(?,''),agent_version),
		last_inventory_at=? WHERE id=?`, append(fields, deviceID)...)
	if err != nil { return err }
	if identityChanged {
		return s.recordDuplicateCandidates(ctx, tx, deviceID)
	}
	return nil
}

func nullableInt(value sql.NullInt64) any {
	if !value.Valid { return nil }
	return value.Int64
}

func nullableBool(value sql.NullInt64) any {
	if !value.Valid { return nil }
	return value.Int64 != 0
}

func (s *Server) applyStorageInventory(ctx context.Context, tx *sql.Tx, deviceID string, payload []byte, observedAt string) error {
	var data map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil { return err }
	status := strings.ToLower(textValue(data["collection_status"]))
	if status != "collected" {
		// Unavailable/unsupported collection must not erase the last known inventory.
		return nil
	}
	disks, ok := data["disks"].([]any)
	if !ok { return errors.New("storage disks must be an array") }
	volumes, ok := data["volumes"].([]any)
	if !ok { return errors.New("storage volumes must be an array") }
	if len(disks) > 256 || len(volumes) > 10000 {
		return errors.New("storage inventory exceeds configured record limits")
	}
	if _,err:=tx.ExecContext(ctx,"DELETE FROM device_storage_disks WHERE device_id=?",deviceID);err!=nil{return err}
	if _,err:=tx.ExecContext(ctx,"DELETE FROM device_storage_volumes WHERE device_id=?",deviceID);err!=nil{return err}
	diskStmt,err:=tx.PrepareContext(ctx,`INSERT INTO device_storage_disks(
		device_id,disk_key,name,model,manufacturer,serial_number,interface_type,capacity_bytes,
		logical_block_size,is_removable,is_rotational,status,observed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err!=nil{return err}
	defer diskStmt.Close()
	seenDisks:=make(map[string]bool,len(disks))
	for _,raw:=range disks{
		item:=mapValue(raw)
		key:=textValue(item["key"])
		if key==""{return errors.New("storage disk key is required")}
		if seenDisks[key]{return errors.New("duplicate storage disk key")}
		seenDisks[key]=true
		if _,err:=diskStmt.ExecContext(ctx,deviceID,key,textValue(item["name"]),textValue(item["model"]),
			textValue(item["manufacturer"]),textValue(item["serial_number"]),textValue(item["interface_type"]),
			integerValue(item["capacity_bytes"]),integerValue(item["logical_block_size"]),
			nullableBoolArgument(item["is_removable"]),nullableBoolArgument(item["is_rotational"]),
			textValue(item["status"]),observedAt);err!=nil{return err}
	}
	volumeStmt,err:=tx.PrepareContext(ctx,`INSERT INTO device_storage_volumes(
		device_id,volume_key,name,parent_key,device,filesystem,mount_point,volume_label,
		capacity_bytes,free_bytes,available_bytes,used_bytes,block_size,is_read_only,status,observed_at
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err!=nil{return err}
	defer volumeStmt.Close()
	seenVolumes:=make(map[string]bool,len(volumes))
	for _,raw:=range volumes{
		item:=mapValue(raw)
		key:=textValue(item["key"])
		if key==""{return errors.New("storage volume key is required")}
		if seenVolumes[key]{return errors.New("duplicate storage volume key")}
		seenVolumes[key]=true
		if _,err:=volumeStmt.ExecContext(ctx,deviceID,key,textValue(item["name"]),textValue(item["parent_key"]),
			textValue(item["device"]),textValue(item["filesystem"]),textValue(item["mount_point"]),
			textValue(item["volume_label"]),integerValue(item["capacity_bytes"]),integerValue(item["free_bytes"]),
			integerValue(item["available_bytes"]),integerValue(item["used_bytes"]),integerValue(item["block_size"]),
			nullableBoolArgument(item["is_read_only"]),textValue(item["status"]),observedAt);err!=nil{return err}
	}
	_,err=tx.ExecContext(ctx,"UPDATE devices SET last_inventory_at=? WHERE id=?",observedAt,deviceID)
	return err
}

func nullableBoolArgument(value any) any {
	if value==nil{return nil}
	return boolInt(value)
}

func boolInt(value any) int {
	switch v := value.(type) {
	case bool:
		if v { return 1 }
	case string:
		if strings.EqualFold(strings.TrimSpace(v), "true") || strings.EqualFold(strings.TrimSpace(v), "enabled") { return 1 }
	case float64:
		if v != 0 { return 1 }
	case int:
		if v != 0 { return 1 }
	}
	return 0
}

func (s *Server) applyNetworkInventory(ctx context.Context, tx *sql.Tx, deviceID string, payload []byte, observedAt string) error {
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil { return err }
	dnsRaw, _ := json.Marshal(data["dns_servers"])
	dns := string(dnsRaw)
	if data["dns_servers"] == nil { dns = "[]" }
	if _, err := tx.ExecContext(ctx, "DELETE FROM device_network_interfaces WHERE device_id=?", deviceID); err != nil { return err }
	dhcpByName := map[string]map[string]any{}
	dhcpByIndex := map[string]map[string]any{}
	if dhcpRows, ok := data["dhcp_interfaces"].([]any); ok {
		for _, raw := range dhcpRows {
			item := mapValue(raw)
			if name := textValue(item["name"]); name != "" { dhcpByName[name] = item }
			if index := textValue(item["interface_index"]); index != "" { dhcpByIndex[index] = item }
		}
	}
	interfaces, _ := data["interfaces"].([]any)
	localIP, macAddress, connectionType := "", "", ""
	for index, raw := range interfaces {
		item := mapValue(raw)
		name := textValue(item["name"])
		key := textValue(item["index"])
		if key == "" { key = name }
		if key == "" { key = strconv.Itoa(index) }
		dhcp := dhcpByName[name]
		if dhcp == nil { dhcp = dhcpByIndex[key] }
		dhcpEnabled, dhcpServer, dhcpLease, dhcpExpiryEstimated := 0, "", "", 0
		if dhcp != nil {
			dhcpEnabled = boolInt(dhcp["dhcp_enabled"])
			dhcpServer = textValue(dhcp["dhcp_server"])
			dhcpLease = textValue(dhcp["dhcp_lease_expires"])
			dhcpExpiryEstimated = boolInt(dhcp["dhcp_lease_expiry_estimated"])
		}
		addresses := item["addresses"]
		addressJSON, _ := json.Marshal(addresses)
		mtu := integerValue(item["mtu"])
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_network_interfaces(
			device_id,interface_key,name,mac_address,connection_type,state,addresses_json,
			dhcp_enabled,dhcp_server,dhcp_lease_expires,dhcp_lease_expiry_estimated,mtu,observed_at
		) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`, deviceID,key,name,textValue(item["mac_address"]),
			textValue(item["connection_type"]),textValue(item["status"]),string(addressJSON),
			dhcpEnabled,dhcpServer,dhcpLease,dhcpExpiryEstimated,mtu,observedAt); err != nil { return err }
		if macAddress == "" && textValue(item["mac_address"]) != "" { macAddress = textValue(item["mac_address"]) }
		if connectionType == "" && textValue(item["status"]) == "up" { connectionType = textValue(item["connection_type"]) }
		if localIP == "" {
			if addrList, ok := addresses.([]any); ok {
				for _, candidate := range addrList {
					value := strings.Split(textValue(candidate), "/")[0]
					ip := net.ParseIP(value)
					if ip != nil && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
						localIP = value
						break
					}
				}
			}
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE devices SET local_ip=COALESCE(NULLIF(?,''),local_ip),
		mac_address=COALESCE(NULLIF(?,''),mac_address),
		default_gateway=COALESCE(NULLIF(?,''),default_gateway),
		dns_servers_json=?, connection_type=COALESCE(NULLIF(?,''),connection_type),
		last_inventory_at=? WHERE id=?`,
		localIP, macAddress, textValue(data["default_gateway"]), dns, connectionType, observedAt, deviceID)
	return err
}

func (s *Server) applySoftwareInventory(ctx context.Context, tx *sql.Tx, deviceID string, payload []byte, observedAt string) error {
	var items []map[string]any
	if err := json.Unmarshal(payload, &items); err != nil { return err }
	if len(items) > 100000 { return errors.New("software inventory exceeds 100000 records") }
	statement, err := tx.PrepareContext(ctx, `INSERT INTO installed_software(
		device_id,name,name_normalized,version,publisher,package_manager,first_seen_at,last_seen_at
	) VALUES (?,?,?,?,?,?,?,?)
	ON CONFLICT(device_id,name_normalized,version,package_manager) DO UPDATE SET
		publisher=excluded.publisher,last_seen_at=excluded.last_seen_at`)
	if err != nil { return err }
	defer statement.Close()
	for _, item := range items {
		name := textValue(item["name"])
		if name == "" { continue }
		version := textValue(item["version"])
		if _, err := statement.ExecContext(ctx, deviceID, name, strings.ToLower(name), version,
			textValue(item["publisher"]), textValue(item["package_manager"]), observedAt, observedAt); err != nil { return err }
	}
	if _,err:=tx.ExecContext(ctx,"DELETE FROM installed_software WHERE device_id=? AND last_seen_at<?",deviceID,observedAt);err!=nil { return err }
	_, err = tx.ExecContext(ctx, "UPDATE devices SET last_inventory_at=? WHERE id=?", observedAt, deviceID)
	return err
}

func normalizeIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.NewReplacer("-", "", "{", "", "}", "", " ", "").Replace(value)
}

func usableIdentity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "", "unknown", "none", "not specified", "default string",
		"to be filled by o.e.m.", "to be filled by oem", "system serial number",
		"serial number", "n/a", "na", "0":
		return ""
	default:
		return value
	}
}

func (s *Server) recordDuplicateCandidates(ctx context.Context, tx *sql.Tx, deviceID string) error {
	var serial, uuidValue, manufacturer, model string
	if err := tx.QueryRowContext(ctx, "SELECT serial_number,smbios_uuid,manufacturer,model FROM devices WHERE id=?", deviceID).
		Scan(&serial,&uuidValue,&manufacturer,&model); err != nil { return err }
	serial = usableIdentity(serial)
	uuidValue = normalizeIdentity(usableIdentity(uuidValue))
	if strings.Trim(uuidValue,"0") == "" || strings.Trim(uuidValue,"f") == "" { uuidValue = "" }
	manufacturer, model = usableIdentity(manufacturer), usableIdentity(model)
	if uuidValue == "" && (serial == "" || manufacturer == "" || model == "") { return nil }
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO device_duplicate_candidates(device_id,candidate_device_id,matched_on,confidence)
		SELECT ?,id,
		CASE WHEN ?<>'' AND lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ',''))=? THEN 'smbios_uuid'
		ELSE 'serial_manufacturer_model' END,
		'high'
		FROM devices
		WHERE id<>? AND merged_into IS NULL AND status<>'merged'
		AND ((?<>'' AND lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ',''))=?)
		OR (?<>'' AND ?<>'' AND ?<>'' AND serial_number=? COLLATE NOCASE AND manufacturer=? COLLATE NOCASE AND model=? COLLATE NOCASE))`,
		deviceID,uuidValue,uuidValue,deviceID,uuidValue,uuidValue,serial,manufacturer,model,serial,manufacturer,model)
	return err
}

var groupFields = map[string]bool{
	"hostname":true,"fqdn":true,"os_family":true,"os_version":true,"os_distribution":true,
	"os_distribution_version":true,"os_architecture":true,"directory_type":true,
	"directory_domain":true,"organizational_unit":true,"directory_site":true,
	"manufacturer":true,"model":true,"serial_number":true,"chassis_type":true,
	"agent_version":true,"timezone":true,"cpu_model":true,"cpu_vendor":true,
}

func validGroupRules(rules []groupRule) bool {
	if len(rules)==0 || len(rules)>20 { return false }
	for _, rule := range rules {
		if !groupFields[rule.Field] || rule.Value=="" || len(rule.Value)>512 { return false }
		switch rule.Operator { case "eq","ne","contains","starts_with","ends_with": default: return false }
	}
	return true
}

func compileGroupRules(operator string, rules []groupRule) (string, []any, error) {
	if (operator!="all" && operator!="any") || !validGroupRules(rules) {
		return "",nil,errors.New("invalid dynamic group rules")
	}
	joiner:=" AND "
	if operator=="any" { joiner=" OR " }
	parts:=make([]string,0,len(rules))
	args:=[]any{}
	for _,rule:=range rules {
		column:=rule.Field
		switch rule.Operator {
		case "eq": parts=append(parts,"lower("+column+")=lower(?)");args=append(args,rule.Value)
		case "ne": parts=append(parts,"lower("+column+")<>lower(?)");args=append(args,rule.Value)
		case "contains": parts=append(parts,"instr(lower("+column+"),lower(?))>0");args=append(args,rule.Value)
		case "starts_with": parts=append(parts,"substr(lower("+column+"),1,length(?))=lower(?)");args=append(args,rule.Value,rule.Value)
		case "ends_with": parts=append(parts,"substr(lower("+column+"),-length(?))=lower(?)");args=append(args,rule.Value,rule.Value)
		default: return "",nil,errors.New("unsupported group rule operator")
		}
	}
	return "("+strings.Join(parts,joiner)+")",args,nil
}

func (s *Server) refreshOneDynamicMembership(ctx context.Context, tx *sql.Tx, groupID int64, operator string, rules []groupRule, deviceID string) error {
	where,args,err:=compileGroupRules(operator,rules)
	if err!=nil { return err }
	insertArgs:=append([]any{groupID,deviceID},args...)
	_,err=tx.ExecContext(ctx,"INSERT OR IGNORE INTO device_group_memberships(group_id,device_id,added_by) "+
		"SELECT ?,id,'dynamic' FROM devices WHERE merged_into IS NULL AND id=? AND "+where,insertArgs...)
	if err!=nil { return err }
	deleteArgs:=append([]any{groupID,deviceID},args...)
	_,err=tx.ExecContext(ctx,"DELETE FROM device_group_memberships WHERE group_id=? AND device_id=? AND NOT ("+where+")",deleteArgs...)
	return err
}

func (s *Server) refreshAllDynamicMemberships(ctx context.Context, tx *sql.Tx, groupID int64, operator string, rules []groupRule) error {
	where,args,err:=compileGroupRules(operator,rules)
	if err!=nil { return err }
	insertArgs:=append([]any{groupID},args...)
	_,err=tx.ExecContext(ctx,"INSERT OR IGNORE INTO device_group_memberships(group_id,device_id,added_by) "+
		"SELECT ?,id,'dynamic' FROM devices WHERE merged_into IS NULL AND "+where,insertArgs...)
	return err
}

func (s *Server) refreshDynamicMemberships(ctx context.Context, tx *sql.Tx, deviceID string) error {
	rows,err:=tx.QueryContext(ctx,"SELECT id,match_operator,rules_json FROM machine_groups WHERE group_type='dynamic'")
	if err!=nil { return err }
	type group struct { id int64; operator string; rules []groupRule }
	groups:=[]group{}
	for rows.Next() {
		var item group
		var raw string
		if err:=rows.Scan(&item.id,&item.operator,&raw);err!=nil { rows.Close();return err }
		if err:=json.Unmarshal([]byte(raw),&item.rules);err!=nil { rows.Close();return err }
		groups=append(groups,item)
	}
	if err:=rows.Err();err!=nil { rows.Close();return err }
	if err:=rows.Close();err!=nil { return err }
	for _,item:=range groups {
		if err:=s.refreshOneDynamicMembership(ctx,tx,item.id,item.operator,item.rules,deviceID);err!=nil { return err }
	}
	return nil
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
	connectionFilter := strings.TrimSpace(r.URL.Query().Get("connection_state"))
	if connectionFilter != "" && connectionFilter != "online" && connectionFilter != "offline" && connectionFilter != "never_connected" {
		write(w, http.StatusBadRequest, response{"error": "invalid_connection_state_filter"})
		return
	}
	sortField := r.URL.Query().Get("sort")
	sortColumns := map[string]string{
		"database_id": "database_id",
		"hostname": "hostname",
		"os_family": "os_family",
		"os_version": "os_version",
		"os_distribution": "os_distribution",
		"directory_domain": "directory_domain",
		"organizational_unit": "organizational_unit",
		"manufacturer": "manufacturer",
		"model": "model",
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

	where := []string{"merged_into IS NULL"}
	args := []any{}
	if search != "" {
		escaped := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(search)
		pattern := "%" + escaped + "%"
		where = append(where, "(hostname LIKE ? ESCAPE '\\' OR fqdn LIKE ? ESCAPE '\\' OR os_family LIKE ? ESCAPE '\\' OR os_version LIKE ? ESCAPE '\\' OR os_distribution LIKE ? ESCAPE '\\' OR directory_domain LIKE ? ESCAPE '\\' OR organizational_unit LIKE ? ESCAPE '\\')")
		args = append(args, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	if status != "" {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if osFamily != "" {
		where = append(where, "os_family = ?")
		args = append(args, osFamily)
	}
	for _, filter := range []struct{ query, column string; max int }{
		{"os_distribution","os_distribution",80},
		{"directory_domain","directory_domain",255},
		{"organizational_unit","organizational_unit",512},
	} {
		value := strings.TrimSpace(r.URL.Query().Get(filter.query))
		if len(value) > filter.max {
			write(w, http.StatusBadRequest, response{"error": "invalid_" + filter.query + "_filter"})
			return
		}
		if value != "" {
			where = append(where, filter.column + " = ?")
			args = append(args, value)
		}
	}
	switch connectionFilter {
	case "online":
		cutoff := time.Now().UTC().Add(-s.onlineGrace)
		futureLimit := time.Now().UTC().Add(30 * time.Second)
		if s.onlineGrace <= 0 {
			cutoff = time.Now().UTC().Add(-90 * time.Second)
		}
		where = append(where, "last_seen IS NOT NULL AND julianday(last_seen) >= julianday(?) AND julianday(last_seen) <= julianday(?)")
		args = append(args, cutoff.Format(time.RFC3339Nano), futureLimit.Format(time.RFC3339Nano))
	case "offline":
		grace := s.onlineGrace
		if grace <= 0 {
			grace = 90 * time.Second
		}
		where = append(where, "last_seen IS NOT NULL AND julianday(last_seen) < julianday(?)")
		args = append(args, time.Now().UTC().Add(-grace).Format(time.RFC3339Nano))
	case "never_connected":
		where = append(where, "(last_seen IS NULL OR last_seen = '')")
	}
	whereSQL := strings.Join(where, " AND ")
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT count(*) FROM devices WHERE "+whereSQL, args...).Scan(&total); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	offset := (page - 1) * pageSize
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	query := "SELECT database_id,id,hostname,os_family,os_version,os_distribution,directory_type,directory_domain,organizational_unit,manufacturer,model,status,last_seen,first_connected_at,last_connected_at FROM devices WHERE " +
		whereSQL + " ORDER BY " + sortColumn + " " + sortDirection + ", database_id ASC LIMIT ? OFFSET ?"
	rows, err := s.db.QueryContext(r.Context(), query, queryArgs...)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var databaseID sql.NullInt64
		var id, hostname, osFamilyValue, osVersion, distribution, directoryType, directoryDomain, organizationalUnit, manufacturer, model, deviceStatus string
		var lastSeen, firstConnected, lastConnected sql.NullString
		if err := rows.Scan(&databaseID,&id,&hostname,&osFamilyValue,&osVersion,&distribution,&directoryType,&directoryDomain,&organizationalUnit,&manufacturer,&model,&deviceStatus,&lastSeen,&firstConnected,&lastConnected); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "database_error"})
			return
		}
		items = append(items, map[string]any{
			"database_id": databaseID.Int64, "id": id, "hostname": hostname, "os_family": osFamilyValue,
			"os_version": osVersion, "os_distribution": distribution, "directory_type": directoryType,
			"directory_domain": directoryDomain, "organizational_unit": organizationalUnit,
			"manufacturer": manufacturer, "model": model, "status": deviceStatus, "last_seen": lastSeen.String,
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

// deviceDetail returns typed device columns and the current categorized inventory.
// The internal API key hashes are never exposed.
func (s *Server) deviceDetail(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(), "SELECT * FROM devices WHERE id = ?", r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	columns, err := rows.Columns()
	if err != nil { rows.Close(); write(w,http.StatusInternalServerError,response{"error":"database_error"}); return }
	if !rows.Next() {
		rows.Close()
		write(w, http.StatusNotFound, response{"error": "device_not_found"})
		return
	}
	raw := make([]any,len(columns))
	scan := make([]any,len(columns))
	for i := range raw { scan[i]=&raw[i] }
	if err := rows.Scan(scan...); err != nil {
		rows.Close()
		write(w,http.StatusInternalServerError,response{"error":"database_error"})
		return
	}
	if err := rows.Close(); err != nil { write(w,http.StatusInternalServerError,response{"error":"database_error"}); return }
	device := map[string]any{}
	lastSeen := sql.NullString{}
	for i,name := range columns {
		if name=="token_hash" { continue }
		value := raw[i]
		switch typed := value.(type) {
		case []byte:
			value = string(typed)
		}
		if name=="last_seen" {
			if value!=nil { lastSeen=sql.NullString{String:fmt.Sprint(value),Valid:true} }
		}
		device[name]=value
	}
	device["connection_state"]=s.connectionState(lastSeen)
	inventoryRows, err := s.db.QueryContext(r.Context(), `SELECT category,payload,observed_at,received_at
		FROM device_inventory WHERE device_id=? ORDER BY category`,r.PathValue("id"))
	if err != nil { write(w,http.StatusInternalServerError,response{"error":"inventory_read_failed"}); return }
	defer inventoryRows.Close()
	inventory := []map[string]any{}
	for inventoryRows.Next() {
		var category,observedAt,receivedAt string
		var payload []byte
		if err:=inventoryRows.Scan(&category,&payload,&observedAt,&receivedAt);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"inventory_read_failed"});return
		}
		inventory=append(inventory,map[string]any{
			"category":category,"payload":json.RawMessage(payload),
			"observed_at":observedAt,"received_at":receivedAt,
		})
	}
	if err:=inventoryRows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"inventory_read_failed"});return }
	interfaceRows, err := s.db.QueryContext(r.Context(), `SELECT interface_key,name,mac_address,connection_type,state,addresses_json,
		dhcp_enabled,dhcp_server,dhcp_lease_expires,dhcp_lease_expiry_estimated,mtu,observed_at
		FROM device_network_interfaces WHERE device_id=? ORDER BY name COLLATE NOCASE`, r.PathValue("id"))
	if err != nil {
		write(w,http.StatusInternalServerError,response{"error":"network_interfaces_read_failed"})
		return
	}
	defer interfaceRows.Close()
	networkInterfaces := []map[string]any{}
	for interfaceRows.Next() {
		var key,name,mac,connectionType,state,addresses,dhcpServer,leaseExpires,observedAt string
		var dhcpEnabled, dhcpExpiryEstimated int
		var mtu sql.NullInt64
		if err := interfaceRows.Scan(&key,&name,&mac,&connectionType,&state,&addresses,&dhcpEnabled,&dhcpServer,&leaseExpires,&dhcpExpiryEstimated,&mtu,&observedAt); err != nil {
			write(w,http.StatusInternalServerError,response{"error":"network_interfaces_read_failed"})
			return
		}
		var mtuValue any
		if mtu.Valid { mtuValue = mtu.Int64 }
		networkInterfaces = append(networkInterfaces,map[string]any{
			"interface_key":key,"name":name,"mac_address":mac,"connection_type":connectionType,
			"state":state,"addresses":json.RawMessage(addresses),"dhcp_enabled":dhcpEnabled!=0,
			"dhcp_server":dhcpServer,"dhcp_lease_expires":leaseExpires,
			"dhcp_lease_expiry_estimated":dhcpExpiryEstimated!=0,"mtu":mtuValue,
			"observed_at":observedAt,
		})
	}
	if err := interfaceRows.Err(); err != nil {
		write(w,http.StatusInternalServerError,response{"error":"network_interfaces_read_failed"})
		return
	}
	diskRows, err := s.db.QueryContext(r.Context(), `SELECT disk_key,name,model,manufacturer,serial_number,interface_type,
		capacity_bytes,logical_block_size,is_removable,is_rotational,status,observed_at
		FROM device_storage_disks WHERE device_id=? ORDER BY name COLLATE NOCASE,disk_key`, r.PathValue("id"))
	if err != nil {
		write(w,http.StatusInternalServerError,response{"error":"storage_disks_read_failed"})
		return
	}
	defer diskRows.Close()
	disks := []map[string]any{}
	for diskRows.Next() {
		var key,name,model,manufacturer,serial,interfaceType,status,observedAt string
		var capacity,blockSize,removable,rotational sql.NullInt64
		if err := diskRows.Scan(&key,&name,&model,&manufacturer,&serial,&interfaceType,&capacity,&blockSize,&removable,&rotational,&status,&observedAt); err != nil {
			write(w,http.StatusInternalServerError,response{"error":"storage_disks_read_failed"})
			return
		}
		disks=append(disks,map[string]any{
			"key":key,"name":name,"model":model,"manufacturer":manufacturer,"serial_number":serial,
			"interface_type":interfaceType,"capacity_bytes":nullableInt(capacity),"logical_block_size":nullableInt(blockSize),
			"is_removable":nullableBool(removable),"is_rotational":nullableBool(rotational),
			"status":status,"observed_at":observedAt,
		})
	}
	if err:=diskRows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"storage_disks_read_failed"});return }

	volumeRows, err := s.db.QueryContext(r.Context(), `SELECT volume_key,name,parent_key,device,filesystem,mount_point,volume_label,
		capacity_bytes,free_bytes,available_bytes,used_bytes,block_size,is_read_only,status,observed_at
		FROM device_storage_volumes WHERE device_id=? ORDER BY mount_point COLLATE NOCASE,volume_key`, r.PathValue("id"))
	if err != nil {
		write(w,http.StatusInternalServerError,response{"error":"storage_volumes_read_failed"})
		return
	}
	defer volumeRows.Close()
	volumes := []map[string]any{}
	for volumeRows.Next() {
		var key,name,parentKey,device,filesystem,mountPoint,label,status,observedAt string
		var capacity,free,available,used,blockSize,readOnly sql.NullInt64
		if err:=volumeRows.Scan(&key,&name,&parentKey,&device,&filesystem,&mountPoint,&label,
			&capacity,&free,&available,&used,&blockSize,&readOnly,&status,&observedAt);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"storage_volumes_read_failed"})
			return
		}
		volumes=append(volumes,map[string]any{
			"key":key,"name":name,"parent_key":parentKey,"device":device,"filesystem":filesystem,
			"mount_point":mountPoint,"volume_label":label,"capacity_bytes":nullableInt(capacity),
			"free_bytes":nullableInt(free),"available_bytes":nullableInt(available),"used_bytes":nullableInt(used),
			"block_size":nullableInt(blockSize),"is_read_only":nullableBool(readOnly),"status":status,"observed_at":observedAt,
		})
	}
	if err:=volumeRows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"storage_volumes_read_failed"});return }
	write(w, http.StatusOK, response{"device":device,"inventory":inventory,"network_interfaces":networkInterfaces,
		"storage_disks":disks,"storage_volumes":volumes})
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

func (s *Server) auditList(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 500 {
			write(w, http.StatusBadRequest, response{"error":"invalid_limit","max":500})
			return
		}
		limit = value
	}
	query := `SELECT id,occurred_at,actor_type,actor_id,event_type,outcome,target_type,target_id,
		source_ip,user_agent,details_json FROM audit_log WHERE 1=1`
	args := []any{}
	for _, filter := range []struct{ key,column string; max int }{
		{"event_type","event_type",100},{"actor_type","actor_type",40},
		{"target_type","target_type",40},{"target_id","target_id",200},{"outcome","outcome",40},
	} {
		value := strings.TrimSpace(r.URL.Query().Get(filter.key))
		if len(value)>filter.max {
			write(w,http.StatusBadRequest,response{"error":"invalid_"+filter.key+"_filter"})
			return
		}
		if value!="" { query+=" AND "+filter.column+"=?"; args=append(args,value) }
	}
	query += " ORDER BY id DESC LIMIT ?"
	args=append(args,limit)
	rows,err:=s.db.QueryContext(r.Context(),query,args...)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"audit_read_failed"});return }
	defer rows.Close()
	items:=[]map[string]any{}
	for rows.Next() {
		var id int64
		var occurredAt,actorType,actorID,eventType,outcome,targetType,targetID,sourceIP,userAgent,details string
		if err:=rows.Scan(&id,&occurredAt,&actorType,&actorID,&eventType,&outcome,&targetType,&targetID,&sourceIP,&userAgent,&details);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"audit_read_failed"});return
		}
		items=append(items,map[string]any{"id":id,"occurred_at":occurredAt,"actor_type":actorType,
			"actor_id":actorID,"event_type":eventType,"outcome":outcome,"target_type":targetType,
			"target_id":targetID,"source_ip":sourceIP,"user_agent":userAgent,"details":json.RawMessage(details)})
	}
	if err:=rows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"audit_read_failed"});return }
	write(w,http.StatusOK,response{"items":items,"count":len(items)})
}

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	rows,err:=s.db.QueryContext(r.Context(),`SELECT g.id,g.name,g.description,g.group_type,g.match_operator,g.rules_json,
		g.created_at,g.updated_at,count(m.device_id)
		FROM machine_groups g LEFT JOIN device_group_memberships m ON m.group_id=g.id
		GROUP BY g.id ORDER BY g.name COLLATE NOCASE`)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_read_failed"});return }
	defer rows.Close()
	items:=[]map[string]any{}
	for rows.Next() {
		var id,count int64
		var name,description,kind,operator,rules,createdAt,updatedAt string
		if err:=rows.Scan(&id,&name,&description,&kind,&operator,&rules,&createdAt,&updatedAt,&count);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"group_read_failed"});return
		}
		items=append(items,map[string]any{"id":id,"name":name,"description":description,"group_type":kind,
			"match_operator":operator,"rules":json.RawMessage(rules),"member_count":count,
			"created_at":createdAt,"updated_at":updatedAt})
	}
	if err:=rows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_read_failed"});return }
	write(w,http.StatusOK,response{"items":items,"count":len(items)})
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Description string `json:"description"`
		Type string `json:"group_type"`
		Operator string `json:"match_operator"`
		Rules []groupRule `json:"rules"`
	}
	if !decode(w,r,&in) { return }
	in.Name=strings.TrimSpace(in.Name)
	in.Description=strings.TrimSpace(in.Description)
	if in.Name=="" || len(in.Name)>100 || len(in.Description)>500 ||
		(in.Type!="static" && in.Type!="dynamic") {
		write(w,http.StatusBadRequest,response{"error":"invalid_group"})
		return
	}
	if in.Operator=="" { in.Operator="all" }
	if in.Operator!="all" && in.Operator!="any" {
		write(w,http.StatusBadRequest,response{"error":"invalid_match_operator"})
		return
	}
	if in.Type=="dynamic" && !validGroupRules(in.Rules) {
		write(w,http.StatusBadRequest,response{"error":"invalid_group_rules","allowed_fields":sortedGroupFields()})
		return
	}
	if in.Type=="static" && len(in.Rules)>0 {
		write(w,http.StatusBadRequest,response{"error":"static_group_cannot_have_rules"})
		return
	}
	rules,_:=json.Marshal(in.Rules)
	tx,err:=s.db.BeginTx(r.Context(),nil)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	defer tx.Rollback()
	result,err:=tx.ExecContext(r.Context(),`INSERT INTO machine_groups(name,description,group_type,match_operator,rules_json)
		VALUES (?,?,?,?,?)`,in.Name,in.Description,in.Type,in.Operator,string(rules))
	if err!=nil { write(w,http.StatusConflict,response{"error":"group_name_conflict_or_create_failed"});return }
	id,err:=result.LastInsertId()
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_create_failed"});return }
	if in.Type=="dynamic" {
		if err:=s.refreshAllDynamicMemberships(r.Context(),tx,id,in.Operator,in.Rules);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"group_member_refresh_failed"});return
		}
	}
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","group.created","success","group",strconv.FormatInt(id,10),map[string]any{"name":in.Name,"group_type":in.Type,"rules":in.Rules});err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_commit_failed"});return }
	write(w,http.StatusCreated,response{"id":id,"name":in.Name,"group_type":in.Type})
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		write(w, http.StatusBadRequest, response{"error": "invalid_group_id"})
		return
	}
	var in struct {
		Name        string      `json:"name"`
		Description string      `json:"description"`
		Type        string      `json:"group_type"`
		Operator    string      `json:"match_operator"`
		Rules       []groupRule `json:"rules"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Description = strings.TrimSpace(in.Description)
	if in.Name == "" || len(in.Name) > 100 || len(in.Description) > 500 ||
		(in.Type != "static" && in.Type != "dynamic") {
		write(w, http.StatusBadRequest, response{"error": "invalid_group"})
		return
	}
	if in.Operator == "" {
		in.Operator = "all"
	}
	if in.Operator != "all" && in.Operator != "any" {
		write(w, http.StatusBadRequest, response{"error": "invalid_match_operator"})
		return
	}
	if in.Type == "dynamic" && !validGroupRules(in.Rules) {
		write(w, http.StatusBadRequest, response{"error": "invalid_group_rules", "allowed_fields": sortedGroupFields()})
		return
	}
	if in.Type == "static" && len(in.Rules) > 0 {
		write(w, http.StatusBadRequest, response{"error": "static_group_cannot_have_rules"})
		return
	}

	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer tx.Rollback()
	var currentType string
	if err := tx.QueryRowContext(r.Context(), "SELECT group_type FROM machine_groups WHERE id=?", id).Scan(&currentType); err != nil {
		write(w, http.StatusNotFound, response{"error": "group_not_found"})
		return
	}
	if currentType != in.Type {
		write(w, http.StatusConflict, response{"error": "group_type_is_immutable"})
		return
	}
	rules, _ := json.Marshal(in.Rules)
	if _, err := tx.ExecContext(r.Context(), `UPDATE machine_groups
		SET name=?, description=?, match_operator=?, rules_json=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?`, in.Name, in.Description, in.Operator, string(rules), id); err != nil {
		write(w, http.StatusConflict, response{"error": "group_name_conflict_or_update_failed"})
		return
	}
	if in.Type == "dynamic" {
		if _, err := tx.ExecContext(r.Context(), "DELETE FROM device_group_memberships WHERE group_id=?", id); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "group_membership_reset_failed"})
			return
		}
		if err := s.refreshAllDynamicMemberships(r.Context(), tx, id, in.Operator, in.Rules); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "group_member_refresh_failed"})
			return
		}
	}
	if err := auditEvent(r.Context(), tx, r, "admin", "configured-admin", "group.updated", "success", "group", strconv.FormatInt(id, 10),
		map[string]any{"name": in.Name, "group_type": in.Type, "rules": in.Rules}); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "group_update_commit_failed"})
		return
	}
	write(w, http.StatusOK, response{"id": id, "name": in.Name, "group_type": in.Type, "status": "updated"})
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil {
		write(w, http.StatusBadRequest, response{"error": "invalid_group_id"})
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "database_error"})
		return
	}
	defer tx.Rollback()
	var name string
	if err := tx.QueryRowContext(r.Context(), "SELECT name FROM machine_groups WHERE id=?", id).Scan(&name); err != nil {
		write(w, http.StatusNotFound, response{"error": "group_not_found"})
		return
	}
	if _, err := tx.ExecContext(r.Context(), "DELETE FROM machine_groups WHERE id=?", id); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "group_delete_failed"})
		return
	}
	if err := auditEvent(r.Context(), tx, r, "admin", "configured-admin", "group.deleted", "success", "group", strconv.FormatInt(id, 10),
		map[string]any{"name": name}); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "group_delete_commit_failed"})
		return
	}
	write(w, http.StatusOK, response{"id": id, "status": "deleted"})
}

func sortedGroupFields() []string {
	fields:=make([]string,0,len(groupFields))
	for field:=range groupFields { fields=append(fields,field) }
	sort.Strings(fields)
	return fields
}

func groupID(r *http.Request) (int64,error) {
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil || id<1 { return 0,errors.New("invalid group id") }
	return id,nil
}

func (s *Server) groupMembers(w http.ResponseWriter, r *http.Request) {
	id,err:=groupID(r)
	if err!=nil { write(w,http.StatusBadRequest,response{"error":"invalid_group_id"});return }
	rows,err:=s.db.QueryContext(r.Context(),`SELECT d.database_id,d.id,d.hostname,d.os_family,d.os_version,d.os_distribution,
		d.directory_domain,d.organizational_unit,d.status
		FROM device_group_memberships m JOIN devices d ON d.id=m.device_id
		WHERE m.group_id=? AND d.merged_into IS NULL ORDER BY d.hostname COLLATE NOCASE`,id)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_members_read_failed"});return }
	defer rows.Close()
	items:=[]map[string]any{}
	for rows.Next() {
		var databaseID sql.NullInt64
		var deviceID,hostname,osFamily,osVersion,distribution,domain,ou,status string
		if err:=rows.Scan(&databaseID,&deviceID,&hostname,&osFamily,&osVersion,&distribution,&domain,&ou,&status);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"group_members_read_failed"});return
		}
		items=append(items,map[string]any{"database_id":databaseID.Int64,"id":deviceID,"hostname":hostname,
			"os_family":osFamily,"os_version":osVersion,"os_distribution":distribution,
			"directory_domain":domain,"organizational_unit":ou,"status":status})
	}
	if err:=rows.Err();err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_members_read_failed"});return }
	write(w,http.StatusOK,response{"items":items,"count":len(items)})
}

func (s *Server) addGroupMembers(w http.ResponseWriter, r *http.Request) {
	id,err:=groupID(r)
	if err!=nil { write(w,http.StatusBadRequest,response{"error":"invalid_group_id"});return }
	var kind string
	if err:=s.db.QueryRowContext(r.Context(),"SELECT group_type FROM machine_groups WHERE id=?",id).Scan(&kind);err!=nil {
		write(w,http.StatusNotFound,response{"error":"group_not_found"});return
	}
	if kind!="static" { write(w,http.StatusBadRequest,response{"error":"dynamic_group_members_are_rule_managed"});return }
	var in struct { DeviceIDs []string `json:"device_ids"` }
	if !decode(w,r,&in) { return }
	if len(in.DeviceIDs)==0 || len(in.DeviceIDs)>1000 {
		write(w,http.StatusBadRequest,response{"error":"invalid_device_ids","max":1000});return
	}
	tx,err:=s.db.BeginTx(r.Context(),nil)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	defer tx.Rollback()
	added:=0
	for _,deviceID:=range in.DeviceIDs {
		result,err:=tx.ExecContext(r.Context(),`INSERT OR IGNORE INTO device_group_memberships(group_id,device_id,added_by)
			SELECT ?,id,'configured-admin' FROM devices WHERE id=? AND merged_into IS NULL`,id,deviceID)
		if err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_membership_write_failed"});return }
		count,_:=result.RowsAffected();added+=int(count)
	}
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","group.members.added","success","group",strconv.FormatInt(id,10),map[string]any{"requested":len(in.DeviceIDs),"added":added});err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_membership_commit_failed"});return }
	write(w,http.StatusOK,response{"added":added})
}

func (s *Server) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	id,err:=groupID(r)
	if err!=nil { write(w,http.StatusBadRequest,response{"error":"invalid_group_id"});return }
	deviceID:=r.PathValue("deviceID")
	var kind string
	if err:=s.db.QueryRowContext(r.Context(),"SELECT group_type FROM machine_groups WHERE id=?",id).Scan(&kind);err!=nil {
		write(w,http.StatusNotFound,response{"error":"group_not_found"});return
	}
	if kind!="static" { write(w,http.StatusBadRequest,response{"error":"dynamic_group_members_are_rule_managed"});return }
	tx,err:=s.db.BeginTx(r.Context(),nil)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	defer tx.Rollback()
	result,err:=tx.ExecContext(r.Context(),"DELETE FROM device_group_memberships WHERE group_id=? AND device_id=?",id,deviceID)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_membership_write_failed"});return }
	count,_:=result.RowsAffected()
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","group.member.removed","success","device",deviceID,map[string]any{"group_id":id});err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"group_membership_commit_failed"});return }
	write(w,http.StatusOK,response{"removed":count>0})
}

func (s *Server) deviceDuplicates(w http.ResponseWriter, r *http.Request) {
	currentID := r.PathValue("id")
	rows, err := s.db.QueryContext(r.Context(), `SELECT c.id,c.matched_on,c.confidence,c.status,
		c.device_id,c.candidate_device_id,
		source.database_id,source.hostname,source.serial_number,source.smbios_uuid,source.status,
		candidate.database_id,candidate.hostname,candidate.serial_number,candidate.smbios_uuid,candidate.status
		FROM device_duplicate_candidates c
		JOIN devices source ON source.id=c.device_id
		JOIN devices candidate ON candidate.id=c.candidate_device_id
		WHERE c.device_id=? OR c.candidate_device_id=?
		ORDER BY c.id DESC`, currentID, currentID)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "duplicate_read_failed"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var candidateID, sourceID, matchedOn, confidence, pairStatus string
		var sourceHostname, sourceSerial, sourceUUID, sourceStatus string
		var targetHostname, targetSerial, targetUUID, targetStatus string
		var id, sourceDatabaseID, targetDatabaseID int64
		if err := rows.Scan(&id,&matchedOn,&confidence,&pairStatus,&sourceID,&candidateID,
			&sourceDatabaseID,&sourceHostname,&sourceSerial,&sourceUUID,&sourceStatus,
			&targetDatabaseID,&targetHostname,&targetSerial,&targetUUID,&targetStatus); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "duplicate_read_failed"})
			return
		}
		otherID, otherDatabaseID := sourceID, sourceDatabaseID
		otherHostname, otherSerial, otherUUID := sourceHostname, sourceSerial, sourceUUID
		mergeSource, mergeCanonical, canonicalStatus := sourceID, currentID, targetStatus
		if currentID == sourceID {
			otherID, otherDatabaseID = candidateID, targetDatabaseID
			otherHostname, otherSerial, otherUUID = targetHostname, targetSerial, targetUUID
			mergeSource, mergeCanonical, canonicalStatus = currentID, candidateID, targetStatus
		}
		mergeAllowed := canonicalStatus == "approved" && sourceStatus != "merged" && targetStatus != "merged"
		items = append(items, map[string]any{
			"id":id,"device_id":otherID,"database_id":otherDatabaseID,
			"hostname":otherHostname,"serial_number":otherSerial,"smbios_uuid":otherUUID,
			"matched_on":matchedOn,"confidence":confidence,"status":pairStatus,
			"source_device_id":sourceID,"candidate_device_id":candidateID,
			"merge_source_id":mergeSource,"merge_canonical_id":mergeCanonical,"merge_allowed":mergeAllowed,
		})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "duplicate_read_failed"})
		return
	}
	write(w, http.StatusOK, response{"items": items, "count": len(items)})
}

func mergeTypedDeviceFields(ctx context.Context, tx *sql.Tx, sourceID, canonicalID string) error {
	textColumns := []string{
		"hostname","fqdn","os_family","os_version","os_distribution","os_distribution_version",
		"os_architecture","os_build","os_install_date","kernel_version",
		"directory_type","directory_domain","organizational_unit","directory_site",
		"manufacturer","model","serial_number","smbios_uuid","chassis_type",
		"cpu_model","cpu_vendor","cpu_architecture","bios_version","bios_release_date",
		"last_boot","timezone","agent_version","local_ip","mac_address","default_gateway",
		"dns_servers_json","connection_type",
	}
	numericColumns := []string{
		"cpu_physical_cores","cpu_logical_processors","memory_total_bytes",
		"root_total_bytes","root_free_bytes","root_available_bytes","uptime_seconds",
	}
	sets := make([]string,0,len(textColumns)+len(numericColumns)+4)
	args := make([]any,0,len(textColumns)+len(numericColumns)+4)
	for _,column:=range textColumns {
		sets=append(sets,column+"=COALESCE(NULLIF("+column+",''),(SELECT "+column+" FROM devices WHERE id=?))")
		args=append(args,sourceID)
	}
	for _,column:=range numericColumns {
		sets=append(sets,column+"=COALESCE("+column+",(SELECT "+column+" FROM devices WHERE id=?))")
		args=append(args,sourceID)
	}
	sets=append(sets,
		"first_connected_at=COALESCE(first_connected_at,(SELECT first_connected_at FROM devices WHERE id=?))",
		"last_seen=MAX(COALESCE(last_seen,''),(SELECT COALESCE(last_seen,'') FROM devices WHERE id=?))",
		"last_connected_at=MAX(COALESCE(last_connected_at,''),(SELECT COALESCE(last_connected_at,'') FROM devices WHERE id=?))",
		"last_inventory_at=MAX(COALESCE(last_inventory_at,''),(SELECT COALESCE(last_inventory_at,'') FROM devices WHERE id=?))",
	)
	args=append(args,sourceID,sourceID,sourceID,sourceID,canonicalID)
	_,err:=tx.ExecContext(ctx,"UPDATE devices SET "+strings.Join(sets,",")+" WHERE id=?",args...)
	return err
}

func (s *Server) mergeDevice(w http.ResponseWriter, r *http.Request) {
	sourceID:=r.PathValue("id")
	var in struct { CanonicalDeviceID string `json:"canonical_device_id"` }
	if !decode(w,r,&in) { return }
	in.CanonicalDeviceID=strings.TrimSpace(in.CanonicalDeviceID)
	if in.CanonicalDeviceID=="" || in.CanonicalDeviceID==sourceID {
		write(w,http.StatusBadRequest,response{"error":"invalid_canonical_device_id"});return
	}
	tx,err:=s.db.BeginTx(r.Context(),nil)
	if err!=nil { write(w,http.StatusInternalServerError,response{"error":"database_error"});return }
	defer tx.Rollback()
	var sourceStatus,sourceMerged,canonicalStatus,canonicalMerged string
	if err:=tx.QueryRowContext(r.Context(),"SELECT status,COALESCE(merged_into,'') FROM devices WHERE id=?",sourceID).Scan(&sourceStatus,&sourceMerged);err!=nil {
		write(w,http.StatusNotFound,response{"error":"source_device_not_found"});return
	}
	if err:=tx.QueryRowContext(r.Context(),"SELECT status,COALESCE(merged_into,'') FROM devices WHERE id=?",in.CanonicalDeviceID).Scan(&canonicalStatus,&canonicalMerged);err!=nil {
		write(w,http.StatusNotFound,response{"error":"canonical_device_not_found"});return
	}
	if sourceMerged!="" || canonicalMerged!="" || sourceStatus=="merged" || canonicalStatus!="approved" {
		write(w,http.StatusConflict,response{"error":"invalid_merge_state"});return
	}
	mergeStatements:=[]string{
		`INSERT INTO device_inventory(device_id,category,payload,observed_at,received_at)
			SELECT ?,category,payload,observed_at,received_at FROM device_inventory WHERE device_id=?
			ON CONFLICT(device_id,category) DO UPDATE SET payload=excluded.payload,observed_at=excluded.observed_at,received_at=excluded.received_at
			WHERE julianday(excluded.observed_at)>julianday(device_inventory.observed_at)`,
		`INSERT INTO device_inventory_history(device_id,category,payload,observed_at,received_at)
			SELECT ?,category,payload,observed_at,received_at FROM device_inventory_history WHERE device_id=?`,
		`INSERT OR IGNORE INTO device_network_interfaces(device_id,interface_key,name,mac_address,connection_type,state,addresses_json,mtu,observed_at)
			SELECT ?,interface_key,name,mac_address,connection_type,state,addresses_json,mtu,observed_at FROM device_network_interfaces WHERE device_id=?`,
		`INSERT OR IGNORE INTO installed_software(device_id,name,name_normalized,version,publisher,package_manager,first_seen_at,last_seen_at)
			SELECT ?,name,name_normalized,version,publisher,package_manager,first_seen_at,last_seen_at FROM installed_software WHERE device_id=?`,
		`INSERT OR IGNORE INTO device_group_memberships(group_id,device_id,added_by)
			SELECT group_id,?,added_by FROM device_group_memberships WHERE device_id=?`,
		`INSERT OR IGNORE INTO job_targets(job_id,device_id,status,result,error,updated_at)
			SELECT job_id,?,status,result,error,updated_at FROM job_targets WHERE device_id=?`,
		"__MERGE_TYPED_DEVICE_FIELDS__",
		"DELETE FROM device_inventory WHERE device_id=?",
		"DELETE FROM device_inventory_history WHERE device_id=?",
		"DELETE FROM device_network_interfaces WHERE device_id=?",
		"DELETE FROM installed_software WHERE device_id=?",
		"DELETE FROM device_group_memberships WHERE device_id=?",
		"DELETE FROM job_targets WHERE device_id=?",
		"UPDATE devices SET status='merged',merged_into=?,duplicate_of=? WHERE id=?",
		"UPDATE device_duplicate_candidates SET status='merged' WHERE device_id=? AND candidate_device_id=?",
	}
	for index,statement:=range mergeStatements {
		if statement=="__MERGE_TYPED_DEVICE_FIELDS__" {
			if err:=mergeTypedDeviceFields(r.Context(),tx,sourceID,in.CanonicalDeviceID);err!=nil {
				write(w,http.StatusInternalServerError,response{"error":"device_merge_fields_failed"});return
			}
			continue
		}
		var args []any
		switch index {
		case 0,1,2,3: args=[]any{in.CanonicalDeviceID,sourceID}
		case 4,5: args=[]any{in.CanonicalDeviceID,sourceID}
		case 6: args=[]any{sourceID,sourceID,sourceID,sourceID,sourceID,sourceID,sourceID,sourceID,sourceID,in.CanonicalDeviceID}
		case 7,8,9,10,11,12: args=[]any{sourceID}
		case 13: args=[]any{in.CanonicalDeviceID,in.CanonicalDeviceID,sourceID}
		case 14: args=[]any{sourceID,in.CanonicalDeviceID}
		}
		if _,err:=tx.ExecContext(r.Context(),statement,args...);err!=nil {
			write(w,http.StatusInternalServerError,response{"error":"device_merge_failed","step":index});return
		}
	}
	if _,err:=tx.ExecContext(r.Context(),`INSERT OR IGNORE INTO device_storage_disks(
		device_id,disk_key,name,model,manufacturer,serial_number,interface_type,capacity_bytes,
		logical_block_size,is_removable,is_rotational,status,observed_at)
		SELECT ?,disk_key,name,model,manufacturer,serial_number,interface_type,capacity_bytes,
		logical_block_size,is_removable,is_rotational,status,observed_at
		FROM device_storage_disks WHERE device_id=?`,in.CanonicalDeviceID,sourceID);err!=nil{
		write(w,http.StatusInternalServerError,response{"error":"device_merge_storage_disks_failed"});return
	}
	if _,err:=tx.ExecContext(r.Context(),`INSERT OR IGNORE INTO device_storage_volumes(
		device_id,volume_key,name,parent_key,device,filesystem,mount_point,volume_label,
		capacity_bytes,free_bytes,available_bytes,used_bytes,block_size,is_read_only,status,observed_at)
		SELECT ?,volume_key,name,parent_key,device,filesystem,mount_point,volume_label,
		capacity_bytes,free_bytes,available_bytes,used_bytes,block_size,is_read_only,status,observed_at
		FROM device_storage_volumes WHERE device_id=?`,in.CanonicalDeviceID,sourceID);err!=nil{
		write(w,http.StatusInternalServerError,response{"error":"device_merge_storage_volumes_failed"});return
	}
	if _,err:=tx.ExecContext(r.Context(),"DELETE FROM device_storage_disks WHERE device_id=?",sourceID);err!=nil{
		write(w,http.StatusInternalServerError,response{"error":"device_merge_storage_disks_delete_failed"});return
	}
	if _,err:=tx.ExecContext(r.Context(),"DELETE FROM device_storage_volumes WHERE device_id=?",sourceID);err!=nil{
		write(w,http.StatusInternalServerError,response{"error":"device_merge_storage_volumes_delete_failed"});return
	}
	if err:=s.refreshDynamicMemberships(r.Context(),tx,in.CanonicalDeviceID);err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"device_merge_group_refresh_failed"});return
	}
	if err:=auditEvent(r.Context(),tx,r,"admin","configured-admin","device.merged","success","device",sourceID,map[string]any{"canonical_device_id":in.CanonicalDeviceID});err!=nil {
		write(w,http.StatusInternalServerError,response{"error":"audit_write_failed"});return
	}
	if err:=tx.Commit();err!=nil { write(w,http.StatusInternalServerError,response{"error":"device_merge_commit_failed"});return }
	write(w,http.StatusOK,response{"status":"merged","device_id":sourceID,"canonical_device_id":in.CanonicalDeviceID})
}

func (s *Server) createJob(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Action    string          `json:"action"`
		DeviceIDs []string        `json:"device_ids"`
		GroupIDs  []int64         `json:"group_ids"`
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
	if !allowed[in.Action] || len(in.DeviceIDs) > 10000 || len(in.GroupIDs) > 500 ||
		(len(in.DeviceIDs) == 0 && len(in.GroupIDs) == 0) {
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
			SELECT ?, id FROM devices WHERE id = ? AND status = 'approved' AND merged_into IS NULL`, id, deviceID); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_target_failed"})
			return
		}
	}
	seenGroups := make(map[int64]bool, len(in.GroupIDs))
	for _, groupID := range in.GroupIDs {
		if groupID < 1 || seenGroups[groupID] {
			write(w, http.StatusBadRequest, response{"error": "invalid_group_ids"})
			return
		}
		seenGroups[groupID] = true
		var groupName string
		if err := tx.QueryRowContext(r.Context(), "SELECT name FROM machine_groups WHERE id=?", groupID).Scan(&groupName); err != nil {
			write(w, http.StatusBadRequest, response{"error": "group_not_found", "group_id": groupID})
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT INTO job_group_targets(job_id,group_id,group_name) VALUES(?,?,?)`, id, groupID, groupName); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_group_target_failed"})
			return
		}
		if _, err := tx.ExecContext(r.Context(), `INSERT OR IGNORE INTO job_targets(job_id,device_id)
			SELECT ?, d.id FROM device_group_memberships m
			JOIN devices d ON d.id=m.device_id
			WHERE m.group_id=? AND d.status='approved' AND d.merged_into IS NULL`, id, groupID); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_group_expansion_failed"})
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
	if err := auditEvent(r.Context(), tx, r, "admin", "configured-admin", "job.created", "success", "job", id,
		map[string]any{"action": in.Action, "target_count": count, "device_target_count": len(in.DeviceIDs), "group_ids": in.GroupIDs}); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err = tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_commit_failed"})
		return
	}
	write(w, http.StatusAccepted, response{"job_id": id, "status": "queued", "target_count": count, "group_count": len(seenGroups)})
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
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error":"database_error"})
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `UPDATE job_targets
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
	_, err = tx.ExecContext(r.Context(), `UPDATE jobs SET status = CASE
		WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status IN ('queued', 'received', 'running')) THEN
			CASE WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status = 'running') THEN 'running' ELSE 'queued' END
		WHEN EXISTS (SELECT 1 FROM job_targets WHERE job_id = ? AND status IN ('failed', 'cancelled')) THEN 'completed_with_errors'
		ELSE 'completed' END WHERE id = ?`,
		r.PathValue("job"), r.PathValue("job"), r.PathValue("job"), r.PathValue("job"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_aggregation_failed"})
		return
	}
	if err := auditEvent(r.Context(), tx, r, "device", r.PathValue("id"), "job.execution.result", in.Status, "job", r.PathValue("job"), map[string]any{"error": in.Error}); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "audit_write_failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		write(w, http.StatusInternalServerError, response{"error":"job_result_commit_failed"})
		return
	}
	write(w, http.StatusOK, response{"status": "recorded"})
}

func (s *Server) listJobs(w http.ResponseWriter, r *http.Request) {
	page, pageSize := 1, 50
	if raw := r.URL.Query().Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1000000 {
			write(w, http.StatusBadRequest, response{"error": "invalid_page"})
			return
		}
		page = value
	}
	if raw := r.URL.Query().Get("page_size"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			write(w, http.StatusBadRequest, response{"error": "invalid_page_size", "max": 200})
			return
		}
		pageSize = value
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT count(*) FROM jobs").Scan(&total); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_list_count_failed"})
		return
	}
	rows, err := s.db.QueryContext(r.Context(), `SELECT j.id,j.action,j.status,j.created_at,
		count(t.device_id),
		COALESCE(sum(CASE WHEN t.status='succeeded' THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN t.status='failed' THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN t.status='running' THEN 1 ELSE 0 END),0),
		COALESCE(sum(CASE WHEN t.status='queued' OR t.status='received' THEN 1 ELSE 0 END),0)
		FROM jobs j LEFT JOIN job_targets t ON t.job_id=j.id
		GROUP BY j.id ORDER BY j.created_at DESC,j.id DESC LIMIT ? OFFSET ?`,
		pageSize, (page-1)*pageSize)
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_list_read_failed"})
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, action, status, createdAt string
		var targetCount, succeeded, failed, running, pending int
		if err := rows.Scan(&id,&action,&status,&createdAt,&targetCount,&succeeded,&failed,&running,&pending); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_list_read_failed"})
			return
		}
		items = append(items, map[string]any{
			"id":id,"action":action,"status":status,"created_at":createdAt,
			"target_count":targetCount,"succeeded":succeeded,"failed":failed,"running":running,"pending":pending,
		})
	}
	if err := rows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_list_read_failed"})
		return
	}
	offset := (page-1)*pageSize
	write(w, http.StatusOK, response{
		"items":items,"count":len(items),"total":total,"page":page,"page_size":pageSize,
		"has_more":offset+len(items)<total,
	})
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
	groupRows, err := s.db.QueryContext(r.Context(), "SELECT group_id,group_name FROM job_group_targets WHERE job_id=? ORDER BY group_name COLLATE NOCASE", r.PathValue("id"))
	if err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_group_read_failed"})
		return
	}
	defer groupRows.Close()
	groups := []map[string]any{}
	for groupRows.Next() {
		var groupID int64
		var groupName string
		if err := groupRows.Scan(&groupID, &groupName); err != nil {
			write(w, http.StatusInternalServerError, response{"error": "job_group_read_failed"})
			return
		}
		groups = append(groups, map[string]any{"id": groupID, "name": groupName})
	}
	if err := groupRows.Err(); err != nil {
		write(w, http.StatusInternalServerError, response{"error": "job_group_read_failed"})
		return
	}
	write(w, http.StatusOK, response{"job_id": r.PathValue("id"), "action": action, "status": status, "groups": groups, "executions": items})
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
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; connect-src 'self'; img-src 'self' data:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}
