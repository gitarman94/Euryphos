package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestConnectionState(t *testing.T) {
	s := &Server{onlineGrace: time.Minute}
	if got := s.connectionState(sql.NullString{}); got != "never_connected" {
		t.Fatalf("empty last-seen should be never_connected, got %q", got)
	}
	recent := sql.NullString{String: time.Now().UTC().Format(time.RFC3339Nano), Valid: true}
	if got := s.connectionState(recent); got != "online" {
		t.Fatalf("recent heartbeat should be online, got %q", got)
	}
	old := sql.NullString{String: time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano), Valid: true}
	if got := s.connectionState(old); got != "offline" {
		t.Fatalf("stale heartbeat should be offline, got %q", got)
	}
}

func TestMigrationAddsConnectionColumnsToExistingDatabase(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "legacy.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE devices (
		id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL DEFAULT '',
		os_family TEXT NOT NULL DEFAULT '',
		os_version TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'pending',
		token_hash TEXT,
		last_seen TEXT,
		created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatalf("create legacy devices table: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("upgrade legacy database: %v", err)
	}
	columns := map[string]bool{}
	rows, err := db.Query("PRAGMA table_info(devices)")
	if err != nil {
		t.Fatalf("inspect migrated devices table: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan table metadata: %v", err)
		}
		columns[name] = true
	}
	for _, column := range []string{
		"first_connected_at", "last_connected_at", "database_id", "serial_number",
		"smbios_uuid", "directory_domain", "organizational_unit", "os_distribution",
		"cpu_model", "memory_total_bytes", "local_ip",
	} {
		if !columns[column] {
			t.Fatalf("migration did not add device column %q: %#v", column, columns)
		}
	}
	if _, err := db.Exec("INSERT INTO devices(id,hostname) VALUES('legacy-device','legacy-host')"); err != nil {
		t.Fatalf("insert migrated device: %v", err)
	}
	var databaseID int64
	if err := db.QueryRow("SELECT database_id FROM devices WHERE id='legacy-device'").Scan(&databaseID); err != nil || databaseID < 1 {
		t.Fatalf("database-generated device ID missing: id=%d error=%v", databaseID, err)
	}
}

func TestUUIDFormat(t *testing.T) {
	id := uuid()
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		t.Fatalf("invalid UUID format: %q", id)
	}
}

func TestCredentialHash(t *testing.T) {
	secret := "test-device-secret"
	if !constantHashMatch(hash(secret), secret) {
		t.Fatal("expected matching credential hash")
	}
	if constantHashMatch(hash(secret), "different-secret") {
		t.Fatal("different credential unexpectedly matched")
	}
}

func TestBearerComparison(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer admin-token")
	if !bearer(req, "admin-token") {
		t.Fatal("matching bearer token rejected")
	}
	if bearer(req, "other-token") {
		t.Fatal("different bearer token accepted")
	}
}

func TestSQLiteServerAgentProtocol(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "euryphos.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	server := &Server{db: db, adminToken: "admin-test-token", enrollToken: "enroll-test-token"}
	handler := securityHeaders(routes(server))

	// Agent enrolls with the enrollment secret and receives a per-device secret.
	enrollResponse := performRequest(t, handler, http.MethodPost, "/api/v1/enrollment", "enroll-test-token", `{"hostname":"test-linux","os_family":"linux","os_version":"Debian 13"}`, http.StatusCreated)
	var enrollment struct {
		DeviceID    string `json:"device_id"`
		DeviceToken string `json:"device_token"`
		Status      string `json:"status"`
	}
	if err := json.Unmarshal(enrollResponse, &enrollment); err != nil {
		t.Fatalf("decode enrollment response: %v", err)
	}
	if enrollment.DeviceID == "" || enrollment.DeviceToken == "" || enrollment.Status != "pending_approval" {
		t.Fatalf("unexpected enrollment response: %s", enrollResponse)
	}

	// Administrator approves the new endpoint; the device secret then authorizes it.
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/approve", "admin-test-token", `{}`, http.StatusOK)
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/heartbeat", enrollment.DeviceToken,
		`{"hostname":"test-linux","os_family":"linux","os_version":"Debian 13"}`, http.StatusOK)
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/inventory", enrollment.DeviceToken,
		`{"category":"system","payload":{"cpu":"test-cpu","memory_bytes":8192},"observed_at":"2026-11-24T12:00:00Z"}`, http.StatusAccepted)
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/inventory", enrollment.DeviceToken,
		`{"category":"software","payload":[{"name":"Example","version":"1.2.3"}]}`, http.StatusAccepted)

	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/inventory", enrollment.DeviceToken,
		`{"category":"system","payload":{"cpu":"test-cpu-v2","memory_bytes":16384},"observed_at":"2026-11-24T12:05:00Z"}`, http.StatusAccepted)

	var categories int
	if err := db.QueryRow(`SELECT count(*) FROM device_inventory WHERE device_id = ?`, enrollment.DeviceID).Scan(&categories); err != nil {
		t.Fatalf("query received inventory: %v", err)
	}
	if categories != 2 {
		t.Fatalf("expected two inventory categories, received %d", categories)
	}

	detail := performRequest(t, handler, http.MethodGet, "/api/v1/devices/"+enrollment.DeviceID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(detail), `"cpu":"test-cpu-v2"`) || strings.Contains(string(detail), "token_hash") {
		t.Fatalf("device detail did not return latest inventory safely: %s", detail)
	}
	history := performRequest(t, handler, http.MethodGet, "/api/v1/devices/"+enrollment.DeviceID+"/inventory/history?category=system&limit=1", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(history), `"has_more":true`) || !strings.Contains(string(history), `"cpu":"test-cpu-v2"`) {
		t.Fatalf("inventory history pagination failed: %s", history)
	}
	var historyPage struct {
		NextBeforeID int64 `json:"next_before_id"`
	}
	if err := json.Unmarshal(history, &historyPage); err != nil || historyPage.NextBeforeID < 1 {
		t.Fatalf("invalid inventory history cursor: %s (error: %v)", history, err)
	}
	pageTwo := performRequest(t, handler, http.MethodGet, "/api/v1/devices/"+enrollment.DeviceID+"/inventory/history?category=system&limit=1&before_id="+strconv.FormatInt(historyPage.NextBeforeID, 10), "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(pageTwo), `"cpu":"test-cpu"`) {
		t.Fatalf("inventory history cursor did not return older snapshot: %s", pageTwo)
	}
	performRequest(t, handler, http.MethodGet, "/api/v1/devices/"+enrollment.DeviceID, "", "", http.StatusUnauthorized)

	devices := performRequest(t, handler, http.MethodGet, "/api/v1/devices", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(devices), `"hostname":"test-linux"`) || !strings.Contains(string(devices), `"status":"approved"`) {
		t.Fatalf("device inventory not visible through admin API: %s", devices)
	}
	if !strings.Contains(string(devices), `"connection_state":"online"`) || !strings.Contains(string(devices), `"first_connected_at":`) || !strings.Contains(string(devices), `"last_connected_at":`) {
		t.Fatalf("server-derived endpoint connection state is missing: %s", devices)
	}
	filtered := performRequest(t, handler, http.MethodGet, "/api/v1/devices?q=test&status=approved&os_family=linux&page=1&page_size=1&connection_state=online&sort=hostname&direction=desc", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(filtered), `"total":1`) || !strings.Contains(string(filtered), `"page_size":1`) || !strings.Contains(string(filtered), `"hostname":"test-linux"`) {
		t.Fatalf("device search/filter/pagination failed: %s", filtered)
	}
	performRequest(t, handler, http.MethodGet, "/api/v1/devices?page_size=201", "admin-test-token", "", http.StatusBadRequest)
	stale := time.Now().UTC().Add(-10 * time.Minute).Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, "UPDATE devices SET last_seen = ?, last_connected_at = ? WHERE id = ?", stale, stale, enrollment.DeviceID); err != nil {
		t.Fatalf("set stale device contact for presence filter test: %v", err)
	}
	offline := performRequest(t, handler, http.MethodGet, "/api/v1/devices?status=approved&connection_state=offline", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(offline), `"total":1`) || !strings.Contains(string(offline), `"connection_state":"offline"`) {
		t.Fatalf("offline connection-state filter failed: %s", offline)
	}
	performRequest(t, handler, http.MethodGet, "/api/v1/devices?connection_state=invalid", "admin-test-token", "", http.StatusBadRequest)

	// The server queues work, the agent claims it, then returns the execution result.
	jobResponse := performRequest(t, handler, http.MethodPost, "/api/v1/jobs", "admin-test-token",
		`{"action":"inventory.refresh","device_ids":["`+enrollment.DeviceID+`"]}`, http.StatusAccepted)
	var jobCreated struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(jobResponse, &jobCreated); err != nil || jobCreated.JobID == "" {
		t.Fatalf("invalid job creation response: %s (error: %v)", jobResponse, err)
	}

	nextJob := performRequest(t, handler, http.MethodGet, "/api/v1/devices/"+enrollment.DeviceID+"/jobs/next", enrollment.DeviceToken, "", http.StatusOK)
	if !strings.Contains(string(nextJob), jobCreated.JobID) || !strings.Contains(string(nextJob), "inventory.refresh") {
		t.Fatalf("agent did not receive queued job: %s", nextJob)
	}
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/jobs/"+jobCreated.JobID+"/result", enrollment.DeviceToken,
		`{"status":"succeeded","result":{"message":"refreshed"}}`, http.StatusOK)
	jobStatus := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+jobCreated.JobID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobStatus), `"status":"completed"`) {
		t.Fatalf("job did not reach completed state: %s", jobStatus)
	}
}

func performRequest(t *testing.T, handler http.Handler, method, path, token, body string, expectedStatus int) []byte {
	t.Helper()
	var requestBody *bytes.Reader
	requestBody = bytes.NewReader([]byte(body))
	req := httptest.NewRequest(method, path, requestBody)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	if response.Code != expectedStatus {
		t.Fatalf("%s %s returned HTTP %d, expected %d: %s", method, path, response.Code, expectedStatus, response.Body.String())
	}
	return response.Body.Bytes()
}
