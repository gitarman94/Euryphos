package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestSystemInventoryPreservesLargeIntegerFacts(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "numbers.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	if _, err := db.Exec("INSERT INTO devices(id) VALUES('device-numeric')"); err != nil { t.Fatal(err) }
	payload, err := json.Marshal(map[string]any{
		"hostname": "numeric-host", "architecture": "amd64", "uptime_seconds": 12345.125,
		"hardware": map[string]any{"physical_cores": 12, "logical_processors": 24},
		"memory": map[string]any{"total_bytes": int64(17179869185)},
		"root_filesystem": map[string]any{"total_bytes": int64(1099511627777), "free_bytes": int64(549755813889), "available_bytes": int64(549755813888)},
	})
	if err != nil { t.Fatal(err) }
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { t.Fatal(err) }
	if err := (&Server{}).applySystemInventory(ctx, tx, "device-numeric", payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		t.Fatalf("normalize system inventory: %v", err)
	}
	if err := tx.Commit(); err != nil { t.Fatal(err) }
	var memory, total, free, available int64
	var uptime float64
	var cores, threads int
	if err := db.QueryRow("SELECT memory_total_bytes,root_total_bytes,root_free_bytes,root_available_bytes,uptime_seconds,cpu_physical_cores,cpu_logical_processors FROM devices WHERE id='device-numeric'").Scan(&memory, &total, &free, &available, &uptime, &cores, &threads); err != nil { t.Fatal(err) }
	if memory != 17179869185 || total != 1099511627777 || free != 549755813889 || available != 549755813888 {
		t.Fatalf("integer byte facts lost precision: memory=%d total=%d free=%d available=%d", memory, total, free, available)
	}
	if uptime != 12345.125 || cores != 12 || threads != 24 {
		t.Fatalf("numeric system facts normalized incorrectly: uptime=%v cores=%d threads=%d", uptime, cores, threads)
	}
}

func TestInventoryHistoryFingerprintIgnoresLiveMetrics(t *testing.T) {
	before := []byte(`{"hostname":"host-1","uptime_seconds":1000,"last_boot":"2026-11-25T01:00:00Z","hardware":{"serial_number":"S-1"},"memory":{"total_bytes":17179869184,"available_bytes":8000000000,"used_bytes":9000000000},"root_filesystem":{"total_bytes":1000000000000,"free_bytes":500000000000,"available_bytes":450000000000},"batteries":[{"status":"Discharging","capacity_percent":80,"health_percent":95}]}`)
	after := []byte(`{"hostname":"host-1","uptime_seconds":1900,"last_boot":"2026-11-25T01:00:01Z","hardware":{"serial_number":"S-1"},"memory":{"total_bytes":17179869184,"available_bytes":7000000000,"used_bytes":10000000000},"root_filesystem":{"total_bytes":1000000000000,"free_bytes":490000000000,"available_bytes":440000000000},"batteries":[{"status":"Charging","capacity_percent":82,"health_percent":95}]}`)
	if !bytes.Equal(inventoryHistoryFingerprint("system", before), inventoryHistoryFingerprint("system", after)) {
		t.Fatal("live uptime, capacity, and charge changes should not create inventory history snapshots")
	}
	identityChange := []byte(`{"hostname":"host-1","hardware":{"serial_number":"S-2"}}`)
	if bytes.Equal(inventoryHistoryFingerprint("system", before), inventoryHistoryFingerprint("system", identityChange)) {
		t.Fatal("identity changes must create inventory history snapshots")
	}
	if !bytes.Equal(inventoryHistoryFingerprint("network", before), before) {
		t.Fatal("non-system categories must retain their full payload for exact change detection")
	}
	if bytes.Equal(inventoryHistoryFingerprint("network", before), inventoryHistoryFingerprint("network", after)) {
		t.Fatal("different non-system payloads must remain different history fingerprints")
	}
}
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
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/inventory", enrollment.DeviceToken,
		`{"category":"system","payload":{"cpu":"test-cpu-v2","memory_bytes":16384},"observed_at":"2026-11-24T12:10:00Z"}`, http.StatusAccepted)
	var historyRows int
	if err := db.QueryRow(`SELECT count(*) FROM device_inventory_history WHERE device_id=? AND category='system'`, enrollment.DeviceID).Scan(&historyRows); err != nil {
		t.Fatalf("count system history snapshots: %v", err)
	}
	if historyRows != 2 {
		t.Fatalf("unchanged inventory should not add another history snapshot; got %d rows", historyRows)
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
	var claimed struct {
		Job *struct {
			ID string `json:"id"`
			Action string `json:"action"`
			ClaimID string `json:"claim_id"`
			Attempt int `json:"attempt"`
		} `json:"job"`
	}
	if err := json.Unmarshal(nextJob, &claimed); err != nil {
		t.Fatalf("decode claimed job: %v", err)
	}
	if claimed.Job == nil || claimed.Job.ID != jobCreated.JobID || claimed.Job.Action != "inventory.refresh" ||
		len(claimed.Job.ClaimID) != 36 || claimed.Job.Attempt != 1 {
		t.Fatalf("agent did not receive a unique first-attempt claim: %s", nextJob)
	}
	resultBody, err := json.Marshal(map[string]any{
		"claim_id": claimed.Job.ClaimID,
		"status": "succeeded",
		"result": map[string]any{"message": "refreshed"},
	})
	if err != nil {
		t.Fatalf("encode job result: %v", err)
	}
	resultPath := "/api/v1/devices/"+enrollment.DeviceID+"/jobs/"+jobCreated.JobID+"/result"
	performRequest(t, handler, http.MethodPost, resultPath, enrollment.DeviceToken, string(resultBody), http.StatusOK)
	duplicateResult := performRequest(t, handler, http.MethodPost, resultPath, enrollment.DeviceToken, string(resultBody), http.StatusOK)
	if !strings.Contains(string(duplicateResult), `"status":"already_recorded"`) {
		t.Fatalf("duplicate result should be idempotently acknowledged: %s", duplicateResult)
	}
	jobStatus := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+jobCreated.JobID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobStatus), `"status":"completed"`) {
		t.Fatalf("job did not reach completed state: %s", jobStatus)
	}
	var jobAuditRows int
	if err := db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='job.execution.result' AND target_id=?", jobCreated.JobID).Scan(&jobAuditRows); err != nil || jobAuditRows != 1 {
		t.Fatalf("job result audit event missing: rows=%d error=%v", jobAuditRows, err)
	}
}


func seedOverdueJobAlertFixture(t *testing.T, db *sql.DB, jobID, deviceID, action, payload, claimID, deviceToken string) {
	t.Helper()
	if _, err := db.Exec("INSERT INTO devices(id,status,token_hash,hostname) VALUES(?,'approved',?,?)",
		deviceID, hash(deviceToken), deviceID+"-host"); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err := db.Exec("INSERT INTO jobs(id,action,payload,status) VALUES(?,?,?,'running')",
		jobID, action, payload); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := db.Exec("INSERT INTO job_targets(job_id,device_id,status,claim_id,attempt_count,updated_at) VALUES(?,?,'running',?,1,datetime('now','-5 minutes'))",
		jobID, deviceID, claimID); err != nil {
		t.Fatalf("insert overdue target: %v", err)
	}
}

func requestJobAlertQueue(t *testing.T, handler http.Handler) (int64, string, string) {
	t.Helper()
	raw := performRequest(t, handler, http.MethodGet, "/api/v1/job-alerts?page=1&page_size=20",
		"admin-test-token", "", http.StatusOK)
	var queue struct {
		Total int `json:"total"`
		OpenCount int `json:"open_count"`
		Items []struct {
			ID int64 `json:"id"`
			ClaimID string `json:"claim_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &queue); err != nil {
		t.Fatalf("decode job alert queue: %v", err)
	}
	if queue.Total < 1 || queue.OpenCount < 1 || len(queue.Items) < 1 || queue.Items[0].ID < 1 {
		t.Fatalf("expected an open overdue alert in queue: %s", raw)
	}
	return queue.Items[0].ID, queue.Items[0].ClaimID, string(raw)
}

func TestJobClaimResolutionOnlyRetiresReclaimedUnstartedClaims(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-claim-resolution.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }

	const jobID = "job-claim-resolution"
	const deviceID = "device-claim-resolution"
	const claimID = "66666666-6666-4666-8666-666666666666"
	const deviceToken = "claim-resolution-device-token"
	seedOverdueJobAlertFixture(t, db, jobID, deviceID, "inventory.refresh", "{}", claimID, deviceToken)
	if _, err := db.ExecContext(ctx, "UPDATE job_targets SET status='queued',claim_id=NULL,updated_at=CURRENT_TIMESTAMP WHERE job_id=? AND device_id=?", jobID, deviceID); err != nil {
		t.Fatal(err)
	}
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	path := "/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/claims/"+claimID
	unauthorized := performRequest(t, handler, http.MethodGet, path, "", "", http.StatusUnauthorized)
	if strings.Contains(string(unauthorized), `"journal_action"`) {
		t.Fatalf("claim-resolution route exposed device state without authentication: %s", unauthorized)
	}
	body := performRequest(t, handler, http.MethodGet, path, deviceToken, "", http.StatusOK)
	if !strings.Contains(string(body), `"journal_action":"retire_if_not_started"`) {
		t.Fatalf("reclaimed unstarted claim did not return restricted retirement instruction: %s", body)
	}
	if !strings.Contains(string(body), `"current_claim_id":""`) {
		t.Fatalf("reclaimed claim response did not report the empty active claim: %s", body)
	}
}

func TestJobAlertQueueAcknowledgementAndHeartbeatRecovery(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-alert-ack.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }

	const jobID = "job-alert-ack"
	const deviceID = "device-alert-ack"
	const claimID = "22222222-2222-4222-8222-222222222222"
	seedOverdueJobAlertFixture(t, db, jobID, deviceID, "inventory.refresh", "{}", claimID, "alert-token")
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",jobStaleAfter:time.Minute}))
	alertID, gotClaimID, queue := requestJobAlertQueue(t, handler)
	if gotClaimID != claimID || !strings.Contains(queue, "\"retry_allowed\":true") {
		t.Fatalf("queue did not expose the active claim and safe retry classification: %s", queue)
	}
	ackBody := "{\"reason\":\"Reviewed endpoint heartbeat and contacted the operator.\"}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/job-alerts/"+strconv.FormatInt(alertID,10)+"/acknowledge",
		"admin-test-token", ackBody, http.StatusOK)
	var targetStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&targetStatus); err != nil {
		t.Fatal(err)
	}
	if targetStatus != "running" {
		t.Fatalf("acknowledging an alert must not change target execution state: %q", targetStatus)
	}
	var ackAudit int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE event_type='job.alert.acknowledged' AND target_id=?", jobID+"/"+deviceID).Scan(&ackAudit); err != nil || ackAudit != 1 {
		t.Fatalf("acknowledgement audit missing: rows=%d err=%v", ackAudit, err)
	}
	if _, err := db.ExecContext(ctx, "UPDATE job_targets SET updated_at=CURRENT_TIMESTAMP WHERE job_id=? AND device_id=?", jobID, deviceID); err != nil {
		t.Fatal(err)
	}
	// A new handler instance simulates a server process restart while SQLite
	// retains the outstanding alert record.
	restartedHandler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",jobStaleAfter:time.Minute}))
	raw := performRequest(t, restartedHandler, http.MethodGet, "/api/v1/job-alerts?page=1&page_size=20",
		"admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(raw), "\"total\":0") {
		t.Fatalf("recovered alert should leave the active review queue: %s", raw)
	}
	var alertStatus, resolution string
	if err := db.QueryRowContext(ctx, "SELECT status,resolution FROM job_alerts WHERE id=?", alertID).Scan(&alertStatus,&resolution); err != nil {
		t.Fatal(err)
	}
	if alertStatus != "recovered" || resolution != "heartbeat_resumed" {
		t.Fatalf("heartbeat recovery was not persisted: status=%q resolution=%q", alertStatus, resolution)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE event_type='job.alert.recovered' AND target_id=?", jobID+"/"+deviceID).Scan(&ackAudit); err != nil || ackAudit != 1 {
		t.Fatalf("heartbeat recovery audit missing: rows=%d err=%v", ackAudit, err)
	}
}

func TestJobAlertResolutionMarksOutcomeIndeterminateAndRejectsLateResult(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-alert-indeterminate.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }

	const jobID = "job-alert-indeterminate"
	const deviceID = "device-alert-indeterminate"
	const claimID = "33333333-3333-4333-8333-333333333333"
	const deviceToken = "indeterminate-device-token"
	seedOverdueJobAlertFixture(t, db, jobID, deviceID, "inventory.refresh", "{}", claimID, deviceToken)
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",jobStaleAfter:time.Minute}))
	alertID, _, _ := requestJobAlertQueue(t, handler)
	body := "{\"decision\":\"mark_indeterminate\",\"expected_claim_id\":\"33333333-3333-4333-8333-333333333333\",\"reason\":\"Endpoint could not be reached; execution outcome cannot be verified.\",\"confirm\":true}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/job-alerts/"+strconv.FormatInt(alertID,10)+"/resolve",
		"admin-test-token", body, http.StatusOK)
	var targetStatus, jobStatus, alertStatus string
	if err := db.QueryRowContext(ctx, "SELECT status FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&targetStatus); err != nil { t.Fatal(err) }
	if err := db.QueryRowContext(ctx, "SELECT status FROM jobs WHERE id=?", jobID).Scan(&jobStatus); err != nil { t.Fatal(err) }
	if err := db.QueryRowContext(ctx, "SELECT status FROM job_alerts WHERE id=?", alertID).Scan(&alertStatus); err != nil { t.Fatal(err) }
	if targetStatus != "indeterminate" || jobStatus != "completed_with_errors" || alertStatus != "resolved" {
		t.Fatalf("unexpected indeterminate resolution: target=%q job=%q alert=%q", targetStatus, jobStatus, alertStatus)
	}
	lateResult := "{\"claim_id\":\"33333333-3333-4333-8333-333333333333\",\"status\":\"succeeded\",\"result\":{\"message\":\"late success\"}}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/result", deviceToken, lateResult, http.StatusConflict)
	resolution := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/claims/"+claimID, deviceToken, "", http.StatusOK)
	if !strings.Contains(string(resolution), "\"journal_action\":\"retire\"") {
		t.Fatalf("explicit indeterminate decision did not authorize journal retirement: %s", resolution)
	}
	var eventCount int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE event_type='job.target.marked_indeterminate' AND target_id=?", jobID+"/"+deviceID).Scan(&eventCount); err != nil || eventCount != 1 {
		t.Fatalf("indeterminate resolution audit missing: rows=%d err=%v", eventCount, err)
	}
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE event_type='job.execution.result' AND target_id=?", jobID).Scan(&eventCount); err != nil || eventCount != 0 {
		t.Fatalf("late agent result must not be audited as accepted: rows=%d err=%v", eventCount, err)
	}
}

func TestJobAlertRetryRequiresSafeActionAndStoppedConfirmation(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-alert-retry.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }

	const jobID = "job-alert-retry"
	const deviceID = "device-alert-retry"
	const claimID = "44444444-4444-4444-8444-444444444444"
	const deviceToken = "retry-device-token"
	seedOverdueJobAlertFixture(t, db, jobID, deviceID, "inventory.refresh", "{}", claimID, deviceToken)
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",jobStaleAfter:time.Minute}))
	alertID, _, _ := requestJobAlertQueue(t, handler)
	base := "{\"decision\":\"retry\",\"expected_claim_id\":\"44444444-4444-4444-8444-444444444444\",\"reason\":\"Confirmed the old worker process is no longer running.\"}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/job-alerts/"+strconv.FormatInt(alertID,10)+"/resolve",
		"admin-test-token", base, http.StatusBadRequest)
	valid := "{\"decision\":\"retry\",\"expected_claim_id\":\"44444444-4444-4444-8444-444444444444\",\"reason\":\"Confirmed the old worker process is no longer running.\",\"confirm\":true,\"confirm_previous_execution_stopped\":true}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/job-alerts/"+strconv.FormatInt(alertID,10)+"/resolve",
		"admin-test-token", valid, http.StatusOK)
	var status string
	var activeClaim sql.NullString
	var attempts int
	if err := db.QueryRowContext(ctx, "SELECT status,claim_id,attempt_count FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&status,&activeClaim,&attempts); err != nil {
		t.Fatal(err)
	}
	if status != "queued" || activeClaim.Valid || attempts != 1 {
		t.Fatalf("safe retry did not create a fresh queued delivery: status=%q claim=%#v attempts=%d", status, activeClaim, attempts)
	}
	lateResult := "{\"claim_id\":\"44444444-4444-4444-8444-444444444444\",\"status\":\"succeeded\",\"result\":{\"message\":\"late old result\"}}"
	performRequest(t, handler, http.MethodPost,
		"/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/result", deviceToken, lateResult, http.StatusConflict)
	resolution := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/claims/"+claimID, deviceToken, "", http.StatusOK)
	if !strings.Contains(string(resolution), "\"journal_action\":\"retire\"") {
		t.Fatalf("explicit retry decision did not authorize old journal retirement: %s", resolution)
	}
	next := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/"+deviceID+"/jobs/next", deviceToken, "", http.StatusOK)
	var delivery struct {
		Job *struct {
			ClaimID string `json:"claim_id"`
			Attempt int `json:"attempt"`
		} `json:"job"`
	}
	if err := json.Unmarshal(next,&delivery); err != nil { t.Fatal(err) }
	if delivery.Job == nil || delivery.Job.ClaimID == claimID || delivery.Job.Attempt != 2 {
		t.Fatalf("retry did not issue a new claim generation: %s", next)
	}
	var retryAudit int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM audit_log WHERE event_type='job.target.retry_authorized' AND target_id=?", jobID+"/"+deviceID).Scan(&retryAudit); err != nil || retryAudit != 1 {
		t.Fatalf("retry authorization audit missing: rows=%d err=%v", retryAudit, err)
	}
}

func TestJobAlertRetryRejectsNonIdempotentActions(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-alert-no-retry.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }

	const jobID = "job-alert-no-retry"
	const deviceID = "device-alert-no-retry"
	const claimID = "55555555-5555-4555-8555-555555555555"
	seedOverdueJobAlertFixture(t, db, jobID, deviceID, "device.reboot", "{}", claimID, "no-retry-device-token")
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",jobStaleAfter:time.Minute}))
	alertID, _, queue := requestJobAlertQueue(t, handler)
	if !strings.Contains(queue, "\"retry_allowed\":false") {
		t.Fatalf("non-idempotent action was exposed as retryable: %s", queue)
	}
	body := "{\"decision\":\"retry\",\"expected_claim_id\":\"55555555-5555-4555-8555-555555555555\",\"reason\":\"Confirmed previous execution stopped before retry request.\",\"confirm\":true,\"confirm_previous_execution_stopped\":true}"
	raw := performRequest(t, handler, http.MethodPost,
		"/api/v1/job-alerts/"+strconv.FormatInt(alertID,10)+"/resolve",
		"admin-test-token", body, http.StatusConflict)
	if !strings.Contains(string(raw), "\"retry_not_safe\"") {
		t.Fatalf("non-idempotent retry was not explicitly blocked: %s", raw)
	}
	var status string
	if err := db.QueryRowContext(ctx, "SELECT status FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&status); err != nil || status != "running" {
		t.Fatalf("blocked retry changed target state: status=%q err=%v", status, err)
	}
}

func TestSafeJobRetryClassification(t *testing.T) {
	cases := []struct {
		action, payload string
		want bool
	}{
		{"inventory.refresh", "{}", true},
		{"service.control", "{\"service_name\":\"spooler\",\"operation\":\"start\"}", true},
		{"service.control", "{\"service_name\":\"spooler\",\"operation\":\"disable\"}", true},
		{"service.control", "{\"service_name\":\"spooler\",\"operation\":\"restart\"}", false},
		{"patch.install", "{}", false},
		{"device.reboot", "{}", false},
		{"agent.update", "{}", false},
	}
	for _, item := range cases {
		got, _ := safeJobRetry(item.action, []byte(item.payload))
		if got != item.want {
			t.Errorf("safeJobRetry(%q,%s)=%v, want %v", item.action, item.payload, got, item.want)
		}
	}
}

func TestJobAPIReportsOverdueRunningTargetsWithoutChangingExecutionState(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "stale-jobs.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	for _, deviceID := range []string{"device-stale", "device-current"} {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO devices(id,status) VALUES(?, 'approved')", deviceID); err != nil {
			t.Fatalf("insert %s: %v", deviceID, err)
		}
	}
	const jobID = "job-stale-heartbeat"
	if _, err := db.ExecContext(ctx,
		"INSERT INTO jobs(id,action,payload,status) VALUES(?, 'service.control', '{}', 'running')", jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_targets(job_id,device_id,status,updated_at)
		VALUES(?, 'device-stale', 'running', datetime('now','-5 minutes')),
		      (?, 'device-current', 'running', CURRENT_TIMESTAMP)`, jobID, jobID); err != nil {
		t.Fatalf("insert job targets: %v", err)
	}

	handler := securityHeaders(routes(&Server{db: db, adminToken: "admin-test-token", jobStaleAfter: time.Minute}))
	list := performRequest(t, handler, http.MethodGet, "/api/v1/jobs?page=1&page_size=10", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(list), `"stale_running":1`) {
		t.Fatalf("job list did not report exactly one overdue running target: %s", list)
	}

	detail := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+jobID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(detail), `"heartbeat_state":"overdue"`) ||
		!strings.Contains(string(detail), `"heartbeat_state":"current"`) {
		t.Fatalf("job detail did not distinguish overdue and current heartbeats: %s", detail)
	}
	var running int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM job_targets WHERE job_id=? AND status='running'", jobID).Scan(&running); err != nil {
		t.Fatalf("query execution state: %v", err)
	}
	if running != 2 {
		t.Fatalf("stale heartbeat indicator changed real execution state; running targets=%d, want 2", running)
	}
}

func TestJobClaimsRejectStaleResultsAndAcceptDuplicateTerminalResults(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-claims.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	const deviceID = "device-claim-test"
	const jobID = "job-claim-test"
	const token = "claim-test-token"
	const oldClaimID = "11111111-1111-4111-8111-111111111111"
	if _, err := db.ExecContext(ctx,
		"INSERT INTO devices(id,status,token_hash) VALUES(?, 'approved', ?)", deviceID, hash(token)); err != nil {
		t.Fatalf("insert device: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		"INSERT INTO jobs(id,action,payload,status) VALUES(?, 'inventory.refresh', '{}', 'queued')", jobID); err != nil {
		t.Fatalf("insert job: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO job_targets(
		job_id,device_id,status,claim_id,attempt_count,updated_at)
		VALUES(?,?,'received',?,1,datetime('now','-16 minutes'))`,
		jobID, deviceID, oldClaimID); err != nil {
		t.Fatalf("insert expired delivery claim: %v", err)
	}

	handler := securityHeaders(routes(&Server{db: db}))
	claimedBody := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/"+deviceID+"/jobs/next", token, "", http.StatusOK)
	var claimed struct {
		Job *struct {
			ID string `json:"id"`
			ClaimID string `json:"claim_id"`
			Attempt int `json:"attempt"`
		} `json:"job"`
	}
	if err := json.Unmarshal(claimedBody, &claimed); err != nil {
		t.Fatalf("decode reclaimed job: %v", err)
	}
	if claimed.Job == nil || claimed.Job.ID != jobID || claimed.Job.ClaimID == "" ||
		claimed.Job.ClaimID == oldClaimID || claimed.Job.Attempt != 2 {
		t.Fatalf("reclaimed job did not receive a new claim and incremented attempt: %s", claimedBody)
	}

	resultPath := "/api/v1/devices/"+deviceID+"/jobs/"+jobID+"/result"
	oldResult := `{"claim_id":"`+oldClaimID+`","status":"succeeded","result":{"message":"stale"}}`
	performRequest(t, handler, http.MethodPost, resultPath, token, oldResult, http.StatusConflict)

	var status, activeClaim string
	var attempts int
	if err := db.QueryRowContext(ctx,
		"SELECT status,claim_id,attempt_count FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).
		Scan(&status, &activeClaim, &attempts); err != nil {
		t.Fatalf("read active delivery attempt: %v", err)
	}
	if status != "received" || activeClaim != claimed.Job.ClaimID || attempts != 2 {
		t.Fatalf("stale result changed active attempt: status=%q claim=%q attempts=%d", status, activeClaim, attempts)
	}

	startedBody := `{"claim_id":"`+claimed.Job.ClaimID+`","status":"started","result":{"message":"started"}}`
	performRequest(t, handler, http.MethodPost, resultPath, token, startedBody, http.StatusOK)
	if _, err := db.ExecContext(ctx,
		"UPDATE job_targets SET updated_at=datetime('now','-3 minutes') WHERE job_id=? AND device_id=?", jobID, deviceID); err != nil {
		t.Fatalf("age running job timestamp: %v", err)
	}
	var beforeHeartbeat string
	if err := db.QueryRowContext(ctx,
		"SELECT updated_at FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&beforeHeartbeat); err != nil {
		t.Fatalf("read aged running timestamp: %v", err)
	}
	duplicateStarted := performRequest(t, handler, http.MethodPost, resultPath, token, startedBody, http.StatusOK)
	if !strings.Contains(string(duplicateStarted), `"status":"heartbeat_recorded"`) {
		t.Fatalf("duplicate started acknowledgement did not renew running timestamp: %s", duplicateStarted)
	}
	var renewedAt string
	if err := db.QueryRowContext(ctx,
		"SELECT updated_at FROM job_targets WHERE job_id=? AND device_id=?", jobID, deviceID).Scan(&renewedAt); err != nil {
		t.Fatalf("read renewed running timestamp: %v", err)
	}
	if renewedAt <= beforeHeartbeat {
		t.Fatalf("running timestamp was not renewed: before=%q after=%q", beforeHeartbeat, renewedAt)
	}
	succeededBody := `{"claim_id":"`+claimed.Job.ClaimID+`","status":"succeeded","result":{"message":"done"}}`
	performRequest(t, handler, http.MethodPost, resultPath, token, succeededBody, http.StatusOK)
	duplicate := performRequest(t, handler, http.MethodPost, resultPath, token, succeededBody, http.StatusOK)
	if !strings.Contains(string(duplicate), `"status":"already_recorded"`) {
		t.Fatalf("duplicate terminal result was not idempotently acknowledged: %s", duplicate)
	}
	conflictingTerminal := `{"claim_id":"`+claimed.Job.ClaimID+`","status":"failed","error":"late-conflict"}`
	performRequest(t, handler, http.MethodPost, resultPath, token, conflictingTerminal, http.StatusConflict)

	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM audit_log WHERE event_type='job.execution.result' AND target_id=?", jobID).Scan(&attempts); err != nil {
		t.Fatalf("count accepted result audit events: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected only started and succeeded audit events, got %d", attempts)
	}
}

func TestMigrationAddsJobClaimColumnsToExistingDatabase(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-claim-migration.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, `CREATE TABLE job_targets (
		job_id TEXT NOT NULL,
		device_id TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'queued',
		result TEXT,
		error TEXT,
		updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY(job_id,device_id)
	)`); err != nil {
		t.Fatalf("create previous-version job_targets table: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("upgrade previous-version database in place: %v", err)
	}
	var claimID sql.NullString
	var attemptCount int
	if err := db.QueryRowContext(ctx,
		"SELECT claim_id,attempt_count FROM job_targets LIMIT 1").Scan(&claimID, &attemptCount); err != nil && err != sql.ErrNoRows {
		t.Fatalf("query additive job claim columns: %v", err)
	}
	var columns int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM pragma_table_info('job_targets') WHERE name IN ('claim_id','attempt_count')").Scan(&columns); err != nil {
		t.Fatalf("inspect job_targets schema: %v", err)
	}
	if columns != 2 {
		t.Fatalf("expected additive claim columns on existing table, got %d", columns)
	}
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM schema_migrations WHERE version=13").Scan(&columns); err != nil || columns != 1 {
		t.Fatalf("schema version 13 marker missing: count=%d error=%v", columns, err)
	}
}

func TestNextJobReclaimsExpiredReceiveAndDispatchesRunningJobs(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "job-leases.db"))
	if err != nil {
		t.Fatalf("open SQLite database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	devices := []struct {
		id    string
		token string
	}{
		{"device-expired", "expired-token"},
		{"device-active", "active-token"},
		{"device-running", "running-token"},
		{"device-running-queued", "running-queued-token"},
	}
	for _, device := range devices {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO devices(id,status,token_hash) VALUES(?, 'approved', ?)",
			device.id, hash(device.token)); err != nil {
			t.Fatalf("insert %s: %v", device.id, err)
		}
	}
	jobs := []struct {
		id     string
		status string
	}{
		{"job-expired", "queued"},
		{"job-active", "queued"},
		{"job-running", "running"},
	}
	for _, job := range jobs {
		if _, err := db.ExecContext(ctx,
			"INSERT INTO jobs(id,action,payload,status) VALUES(?, 'inventory.refresh', '{}', ?)",
			job.id, job.status); err != nil {
			t.Fatalf("insert %s: %v", job.id, err)
		}
	}
	for _, target := range []struct {
		jobID     string
		deviceID  string
		status    string
		updatedAt string
	}{
		{"job-expired", "device-expired", "received", "datetime('now','-16 minutes')"},
		{"job-active", "device-active", "received", "CURRENT_TIMESTAMP"},
		{"job-running", "device-running", "running", "CURRENT_TIMESTAMP"},
		{"job-running", "device-running-queued", "queued", "CURRENT_TIMESTAMP"},
	} {
		query := "INSERT INTO job_targets(job_id,device_id,status,updated_at) VALUES(?,?,?, " + target.updatedAt + ")"
		if _, err := db.ExecContext(ctx, query, target.jobID, target.deviceID, target.status); err != nil {
			t.Fatalf("insert target %s/%s: %v", target.jobID, target.deviceID, err)
		}
	}

	handler := securityHeaders(routes(&Server{db: db}))
	var expiredResponse struct {
		Job *struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	expiredBody := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/device-expired/jobs/next", "expired-token", "", http.StatusOK)
	if err := json.Unmarshal(expiredBody, &expiredResponse); err != nil {
		t.Fatalf("decode recovered job response: %v", err)
	}
	if expiredResponse.Job == nil || expiredResponse.Job.ID != "job-expired" {
		t.Fatalf("expired received job was not reclaimed: %s", expiredBody)
	}

	var activeResponse struct {
		Job *json.RawMessage `json:"job"`
	}
	activeBody := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/device-active/jobs/next", "active-token", "", http.StatusOK)
	if err := json.Unmarshal(activeBody, &activeResponse); err != nil {
		t.Fatalf("decode active lease response: %v", err)
	}
	if activeResponse.Job != nil {
		t.Fatalf("unexpired received job was delivered twice: %s", activeBody)
	}

	var runningResponse struct {
		Job *struct {
			ID string `json:"id"`
		} `json:"job"`
	}
	runningBody := performRequest(t, handler, http.MethodGet,
		"/api/v1/devices/device-running-queued/jobs/next", "running-queued-token", "", http.StatusOK)
	if err := json.Unmarshal(runningBody, &runningResponse); err != nil {
		t.Fatalf("decode running job response: %v", err)
	}
	if runningResponse.Job == nil || runningResponse.Job.ID != "job-running" {
		t.Fatalf("queued target was not dispatched while another target was running: %s", runningBody)
	}

	var recoveryAuditCount int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM audit_log WHERE event_type='job.target.requeued' AND target_id='device-expired'").Scan(&recoveryAuditCount); err != nil {
		t.Fatalf("count job recovery audit events: %v", err)
	}
	if recoveryAuditCount != 1 {
		t.Fatalf("expected one recovery audit event, got %d", recoveryAuditCount)
	}
}

func TestGroupsUseDirectoryAndLinuxCompatibleAttributes(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "groups.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,hostname,os_family,os_version,os_distribution,directory_type,directory_domain,organizational_unit) VALUES(?,?,?,?,?,?,?,?)",
		"group-device","linux-workstation","linux","Debian 13","debian","active_directory","corp.example","Engineering/Linux")
	if err != nil { t.Fatal(err) }
	server := &Server{db:db,adminToken:"admin-test-token",onlineGrace:time.Minute}
	handler := securityHeaders(routes(server))
	raw := performRequest(t,handler,http.MethodPost,"/api/v1/groups","admin-test-token",
		"{\"name\":\"Linux Directory Fleet\",\"group_type\":\"dynamic\",\"match_operator\":\"all\",\"rules\":[{\"field\":\"os_family\",\"operator\":\"eq\",\"value\":\"linux\"},{\"field\":\"directory_domain\",\"operator\":\"eq\",\"value\":\"corp.example\"},{\"field\":\"organizational_unit\",\"operator\":\"contains\",\"value\":\"Engineering\"}]}",
		http.StatusCreated)
	var group map[string]any
	if err:=json.Unmarshal(raw,&group);err!=nil { t.Fatal(err) }
	id:=int64(group["id"].(float64))
	members:=performRequest(t,handler,http.MethodGet,"/api/v1/groups/"+strconv.FormatInt(id,10)+"/members","admin-test-token","",http.StatusOK)
	if !strings.Contains(string(members),"group-device") {
		t.Fatalf("dynamic group did not evaluate cross-platform attributes: %s",members)
	}
	performRequest(t,handler,http.MethodPost,"/api/v1/groups","admin-test-token",
		"{\"name\":\"Bad Rule\",\"group_type\":\"dynamic\",\"rules\":[{\"field\":\"token_hash\",\"operator\":\"eq\",\"value\":\"secret\"}]}",
		http.StatusBadRequest)
	var auditCount int
	if err:=db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='group.created'").Scan(&auditCount);err!=nil || auditCount!=1 {
		t.Fatalf("group creation audit missing: count=%d error=%v",auditCount,err)
	}
}

func TestDuplicateMergePreservesAgentCredential(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "merge.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatal(err) }
	canonical, source := "canonical-device", "duplicate-device"
	_,err=db.Exec("INSERT INTO devices(id,hostname,manufacturer,model,serial_number,smbios_uuid,status,token_hash) VALUES(?,?,?,?,?,?,?,?)",
		canonical,"workstation-a","Contoso","Model X","SERIAL-77","11111111-2222-3333-4444-555555555555","approved",hash("canonical-token"))
	if err!=nil { t.Fatal(err) }
	_,err=db.Exec(`INSERT INTO devices(id,hostname,manufacturer,model,serial_number,smbios_uuid,
		directory_domain,organizational_unit,os_distribution,cpu_model,memory_total_bytes,status,token_hash)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		source,"workstation-b","Contoso","Model X","SERIAL-77","11111111-2222-3333-4444-555555555555",
		"corp.example","Servers/Finance","windows","Example CPU",17179869184,"approved",hash("source-token"))
	if err!=nil { t.Fatal(err) }
	_,err=db.Exec(`INSERT INTO device_storage_disks(device_id,disk_key,name,model,capacity_bytes,observed_at)
		VALUES(?,?,?,?,?,?)`,source,"disk0","nvme0n1","Merge Test NVMe",1099511627777,time.Now().UTC().Format(time.RFC3339Nano))
	if err!=nil { t.Fatal(err) }
	_,err=db.Exec(`INSERT INTO device_storage_volumes(device_id,volume_key,name,filesystem,mount_point,capacity_bytes,free_bytes,observed_at)
		VALUES(?,?,?,?,?,?,?,?)`,source,"rootfs","root","ext4","/",1099511627777,549755813889,time.Now().UTC().Format(time.RFC3339Nano))
	if err!=nil { t.Fatal(err) }
	_,err=db.Exec(`INSERT INTO device_services(device_id,service_key,name,manager,active_state,start_type,status,observed_at)
		VALUES(?,?,?,?,?,?,?,?)`,source,"sshd.service","sshd.service","systemd","active","enabled","active",time.Now().UTC().Format(time.RFC3339Nano))
	if err!=nil { t.Fatal(err) }
	tx,err:=db.BeginTx(ctx,nil)
	if err!=nil { t.Fatal(err) }
	if err:=(&Server{db:db}).recordDuplicateCandidates(ctx,tx,source);err!=nil { tx.Rollback();t.Fatal(err) }
	if err:=tx.Commit();err!=nil { t.Fatal(err) }
	server:=&Server{db:db,adminToken:"admin-test-token",onlineGrace:time.Minute}
	handler:=securityHeaders(routes(server))
	candidates:=performRequest(t,handler,http.MethodGet,"/api/v1/devices/"+source+"/duplicates","admin-test-token","",http.StatusOK)
	if !strings.Contains(string(candidates),canonical) { t.Fatalf("duplicate candidate not returned: %s",candidates) }
	performRequest(t,handler,http.MethodPost,"/api/v1/devices/"+source+"/merge","admin-test-token",
		"{\"canonical_device_id\":\""+canonical+"\"}",http.StatusOK)
	performRequest(t,handler,http.MethodPost,"/api/v1/devices/"+source+"/heartbeat","source-token",
		"{\"hostname\":\"merged-host\",\"os_family\":\"linux\"}",http.StatusOK)
	var hostname,status,mergedInto string
	if err:=db.QueryRow("SELECT hostname,status,COALESCE(merged_into,'') FROM devices WHERE id=?",canonical).Scan(&hostname,&status,&mergedInto);err!=nil {
		t.Fatal(err)
	}
	if hostname!="merged-host" || status!="approved" || mergedInto!="" {
		t.Fatalf("old credential did not route to canonical device: hostname=%q status=%q merged_into=%q",hostname,status,mergedInto)
	}
	if err:=db.QueryRow("SELECT status,merged_into FROM devices WHERE id=?",source).Scan(&status,&mergedInto);err!=nil {
		t.Fatal(err)
	}
	if status!="merged" || mergedInto!=canonical { t.Fatalf("source alias not retained: status=%q merged_into=%q",status,mergedInto) }
	var directoryDomain,ou,distribution,cpuModel string
	var memoryTotal int64
	if err:=db.QueryRow("SELECT directory_domain,organizational_unit,os_distribution,cpu_model,memory_total_bytes FROM devices WHERE id=?",canonical).
		Scan(&directoryDomain,&ou,&distribution,&cpuModel,&memoryTotal);err!=nil { t.Fatal(err) }
	if directoryDomain!="corp.example" || ou!="Servers/Finance" || distribution!="windows" ||
		cpuModel!="Example CPU" || memoryTotal!=17179869184 {
		t.Fatalf("merge did not fill canonical typed fields: domain=%q ou=%q distro=%q cpu=%q memory=%d",
			directoryDomain,ou,distribution,cpuModel,memoryTotal)
	}
	var diskCount,volumeCount int
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_disks WHERE device_id=?",canonical).Scan(&diskCount);err!=nil { t.Fatal(err) }
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_volumes WHERE device_id=?",canonical).Scan(&volumeCount);err!=nil { t.Fatal(err) }
	if diskCount!=1 || volumeCount!=1 { t.Fatalf("merge did not preserve storage rows: disks=%d volumes=%d",diskCount,volumeCount) }
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_disks WHERE device_id=?",source).Scan(&diskCount);err!=nil { t.Fatal(err) }
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_volumes WHERE device_id=?",source).Scan(&volumeCount);err!=nil { t.Fatal(err) }
	if diskCount!=0 || volumeCount!=0 { t.Fatalf("merged source still owns storage rows: disks=%d volumes=%d",diskCount,volumeCount) }
	var serviceCount int
	if err:=db.QueryRow("SELECT count(*) FROM device_services WHERE device_id=?",canonical).Scan(&serviceCount);err!=nil { t.Fatal(err) }
	if serviceCount!=1 { t.Fatalf("merge did not preserve service rows: %d",serviceCount) }
	if err:=db.QueryRow("SELECT count(*) FROM device_services WHERE device_id=?",source).Scan(&serviceCount);err!=nil { t.Fatal(err) }
	if serviceCount!=0 { t.Fatalf("merged source still owns service rows: %d",serviceCount) }
}

func TestDuplicateDetectionFallsBackToSerialManufacturerModel(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "serial-duplicates.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid,status) VALUES(?,?,?,?,?,?)",
		"serial-canonical", "Acme", "Widget", "ser-123", "", "approved")
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"serial-duplicate", "ACME", "Widget", "SER-123", "")
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"placeholder-one", "OEM", "Default String", "To be filled by O.E.M.", "00000000-0000-0000-0000-000000000000")
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"placeholder-two", "OEM", "Default String", "to be filled by o.e.m.", "00000000-0000-0000-0000-000000000000")
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"unknown-uuid-one", "OEM A", "Model A", "", "unknown")
	if err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"unknown-uuid-two", "OEM B", "Model B", "", "UNKNOWN")
	if err != nil { t.Fatal(err) }
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { t.Fatal(err) }
	server := &Server{db: db}
	if err := server.recordDuplicateCandidates(ctx, tx, "serial-duplicate"); err != nil { tx.Rollback(); t.Fatal(err) }
	if err := server.recordDuplicateCandidates(ctx, tx, "placeholder-two"); err != nil { tx.Rollback(); t.Fatal(err) }
	if err := server.recordDuplicateCandidates(ctx, tx, "unknown-uuid-two"); err != nil { tx.Rollback(); t.Fatal(err) }
	if err := tx.Commit(); err != nil { t.Fatal(err) }
	var matchedOn string
	if err := db.QueryRow("SELECT matched_on FROM device_duplicate_candidates WHERE device_id='serial-duplicate' AND candidate_device_id='serial-canonical'").Scan(&matchedOn); err != nil {
		t.Fatalf("serial fallback candidate not found: %v", err)
	}
	if matchedOn != "serial_manufacturer_model" { t.Fatalf("unexpected duplicate match reason: %q", matchedOn) }
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	sourceView := performRequest(t,handler,http.MethodGet,"/api/v1/devices/serial-duplicate/duplicates","admin-test-token","",http.StatusOK)
	if !strings.Contains(string(sourceView), `"merge_source_id":"serial-duplicate"`) ||
		!strings.Contains(string(sourceView), `"merge_canonical_id":"serial-canonical"`) ||
		!strings.Contains(string(sourceView), `"merge_allowed":true`) {
		t.Fatalf("duplicate source view did not expose the safe merge direction: %s",sourceView)
	}
	canonicalView := performRequest(t,handler,http.MethodGet,"/api/v1/devices/serial-canonical/duplicates","admin-test-token","",http.StatusOK)
	if !strings.Contains(string(canonicalView), `"device_id":"serial-duplicate"`) ||
		!strings.Contains(string(canonicalView), `"merge_source_id":"serial-duplicate"`) ||
		!strings.Contains(string(canonicalView), `"merge_canonical_id":"serial-canonical"`) {
		t.Fatalf("canonical-side duplicate view failed: %s",canonicalView)
	}
	var placeholderMatches int
	if err := db.QueryRow("SELECT count(*) FROM device_duplicate_candidates WHERE device_id='placeholder-two'").Scan(&placeholderMatches); err != nil { t.Fatal(err) }
	if placeholderMatches != 0 { t.Fatalf("OEM placeholder identifiers must not create duplicate candidates, got %d", placeholderMatches) }
	if err := db.QueryRow("SELECT count(*) FROM device_duplicate_candidates WHERE device_id='unknown-uuid-two'").Scan(&placeholderMatches); err != nil { t.Fatal(err) }
	if placeholderMatches != 0 { t.Fatalf("placeholder SMBIOS UUIDs must not create duplicate candidates, got %d", placeholderMatches) }
}
func TestAdminSessionAuditSuccessAndFailure(t *testing.T) {
	db,err:=openDatabase(filepath.Join(t.TempDir(),"audit.db"))
	if err!=nil { t.Fatal(err) }
	defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatal(err) }
	handler:=securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	performRequest(t,handler,http.MethodPost,"/api/v1/auth/session","bad-token","{}",http.StatusUnauthorized)
	performRequest(t,handler,http.MethodGet,"/api/v1/groups","bad-token","",http.StatusUnauthorized)
	performRequest(t,handler,http.MethodPost,"/api/v1/auth/session","admin-test-token","{}",http.StatusOK)
	var failures,successes int
	if err:=db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='auth.admin.failure'").Scan(&failures);err!=nil { t.Fatal(err) }
	if err:=db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='auth.admin.success'").Scan(&successes);err!=nil { t.Fatal(err) }
	if failures!=2 || successes!=1 { t.Fatalf("unexpected auth audit rows: failures=%d successes=%d",failures,successes) }
}

func TestDynamicGroupUpdateAndGroupTargetedJobs(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "group-jobs.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	_, err = db.Exec(`INSERT INTO devices(id,hostname,os_family,status) VALUES
		('linux-1','linux-one','linux','approved'),
		('windows-1','windows-one','windows','approved')`)
	if err != nil { t.Fatal(err) }
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token",onlineGrace:time.Minute}))

	raw := performRequest(t, handler, http.MethodPost, "/api/v1/groups", "admin-test-token",
		`{"name":"Linux Servers","group_type":"dynamic","match_operator":"all","rules":[{"field":"os_family","operator":"eq","value":"linux"}]}`,
		http.StatusCreated)
	var created struct { ID int64 `json:"id"` }
	if err := json.Unmarshal(raw, &created); err != nil || created.ID < 1 {
		t.Fatalf("invalid group creation response: %s (%v)", raw, err)
	}
	groupPath := "/api/v1/groups/" + strconv.FormatInt(created.ID, 10)
	members := performRequest(t, handler, http.MethodGet, groupPath+"/members", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(members), "linux-1") || strings.Contains(string(members), "windows-1") {
		t.Fatalf("initial dynamic membership is incorrect: %s", members)
	}

	performRequest(t, handler, http.MethodPut, groupPath, "admin-test-token",
		`{"name":"Windows Servers","description":"Updated rule","group_type":"dynamic","match_operator":"all","rules":[{"field":"os_family","operator":"eq","value":"windows"}]}`,
		http.StatusOK)
	members = performRequest(t, handler, http.MethodGet, groupPath+"/members", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(members), "windows-1") || strings.Contains(string(members), "linux-1") {
		t.Fatalf("updated dynamic membership is incorrect: %s", members)
	}

	jobRaw := performRequest(t, handler, http.MethodPost, "/api/v1/jobs", "admin-test-token",
		`{"action":"inventory.refresh","group_ids":[`+strconv.FormatInt(created.ID,10)+`]}`, http.StatusAccepted)
	var job struct { ID string `json:"job_id"`; TargetCount int `json:"target_count"` }
	if err := json.Unmarshal(jobRaw, &job); err != nil || job.ID == "" || job.TargetCount != 1 {
		t.Fatalf("group job did not expand to exactly one approved device: %s (%v)", jobRaw, err)
	}
	jobList := performRequest(t, handler, http.MethodGet, "/api/v1/jobs?page=1&page_size=10", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobList), job.ID) || !strings.Contains(string(jobList), `"target_count":1`) || !strings.Contains(string(jobList), `"pending":1`) || !strings.Contains(string(jobList), `"last_updated_at":`) {
		t.Fatalf("paginated job listing did not expose target state: %s", jobList)
	}
	performRequest(t, handler, http.MethodGet, "/api/v1/jobs?page_size=201", "admin-test-token", "", http.StatusBadRequest)

	var targetID string
	if err := db.QueryRow("SELECT device_id FROM job_targets WHERE job_id=?", job.ID).Scan(&targetID); err != nil || targetID != "windows-1" {
		t.Fatalf("group job selected wrong target: %q (%v)", targetID, err)
	}
	if _, err := db.Exec("UPDATE machine_groups SET name='Renamed Group' WHERE id=?", created.ID); err != nil { t.Fatal(err) }
	performRequest(t, handler, http.MethodDelete, groupPath, "admin-test-token", "", http.StatusOK)

	jobDetail := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+job.ID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobDetail), `"name":"Windows Servers"`) || !strings.Contains(string(jobDetail), "windows-1") || !strings.Contains(string(jobDetail), `"updated_at":`) {
		t.Fatalf("job history did not retain group name and expanded target after deletion: %s", jobDetail)
	}
	var groupAudit, jobGroupSnapshot int
	if err := db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='group.updated' AND target_id=?", strconv.FormatInt(created.ID,10)).Scan(&groupAudit); err != nil { t.Fatal(err) }
	if err := db.QueryRow("SELECT count(*) FROM job_group_targets WHERE job_id=?", job.ID).Scan(&jobGroupSnapshot); err != nil { t.Fatal(err) }
	if groupAudit != 1 || jobGroupSnapshot != 1 {
		t.Fatalf("expected group update audit and durable job-group snapshot; audit=%d snapshot=%d",groupAudit,jobGroupSnapshot)
	}
}

func TestBrowserSessionRequiresCSRFAndSupportsConsole(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "browser-session.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	server := &Server{db:db,adminToken:"admin-test-token",cookieSecure:false}
	handler := securityHeaders(routes(server))

	rootResponse := httptest.NewRecorder()
	handler.ServeHTTP(rootResponse, httptest.NewRequest(http.MethodGet, "/", nil))
	if rootResponse.Code != http.StatusSeeOther || rootResponse.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated root did not redirect to login: status=%d location=%q", rootResponse.Code, rootResponse.Header().Get("Location"))
	}

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), "Sign in to Euryphos") {
		t.Fatalf("login page did not render: status=%d", loginPage.Code)
	}
	loginScript := httptest.NewRecorder()
	handler.ServeHTTP(loginScript, httptest.NewRequest(http.MethodGet, "/assets/login.js", nil))
	if loginScript.Code != http.StatusOK || !strings.Contains(loginScript.Body.String(), "Authorization") {
		t.Fatalf("login script was not served: status=%d",loginScript.Code)
	}
	consoleScript := httptest.NewRecorder()
	handler.ServeHTTP(consoleScript, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if consoleScript.Code != http.StatusOK || !strings.Contains(consoleScript.Body.String(), "function devicesView") {
		t.Fatalf("console script was not served: status=%d",consoleScript.Code)
	}
	consoleStyle := httptest.NewRecorder()
	handler.ServeHTTP(consoleStyle, httptest.NewRequest(http.MethodGet, "/assets/app.css", nil))
	if consoleStyle.Code != http.StatusOK || !strings.Contains(consoleStyle.Header().Get("Content-Type"), "text/css") {
		t.Fatalf("console stylesheet was not served: status=%d",consoleStyle.Code)
	}

	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/session", strings.NewReader("{}"))
	loginRequest.Header.Set("Authorization", "Bearer admin-test-token")
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, loginRequest)
	if loginResponse.Code != http.StatusOK { t.Fatalf("login failed: %d %s",loginResponse.Code,loginResponse.Body.String()) }
	var sessionCookie, csrfCookie *http.Cookie
	for _, cookie := range loginResponse.Result().Cookies() {
		switch cookie.Name {
		case "euryphos_admin_session": sessionCookie = cookie
		case "euryphos_csrf": csrfCookie = cookie
		}
	}
	if sessionCookie == nil || csrfCookie == nil || !sessionCookie.HttpOnly ||
		sessionCookie.SameSite != http.SameSiteStrictMode || csrfCookie.HttpOnly {
		t.Fatalf("session cookies missing or insecure: session=%#v csrf=%#v",sessionCookie,csrfCookie)
	}

	authenticatedRoot := httptest.NewRequest(http.MethodGet, "/", nil)
	authenticatedRoot.AddCookie(sessionCookie)
	authenticatedRootResponse := httptest.NewRecorder()
	handler.ServeHTTP(authenticatedRootResponse, authenticatedRoot)
	if authenticatedRootResponse.Code != http.StatusSeeOther || authenticatedRootResponse.Header().Get("Location") != "/console" {
		t.Fatalf("authenticated root did not redirect to console: status=%d location=%q", authenticatedRootResponse.Code, authenticatedRootResponse.Header().Get("Location"))
	}

	consoleRequest := httptest.NewRequest(http.MethodGet, "/console", nil)
	consoleRequest.AddCookie(sessionCookie)
	consoleResponse := httptest.NewRecorder()
	handler.ServeHTTP(consoleResponse, consoleRequest)
	if consoleResponse.Code != http.StatusOK || !strings.Contains(consoleResponse.Body.String(), "Euryphos Console") {
		t.Fatalf("authenticated browser console failed: status=%d body=%s",consoleResponse.Code,consoleResponse.Body.String())
	}

	readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	readRequest.AddCookie(sessionCookie)
	readResponse := httptest.NewRecorder()
	handler.ServeHTTP(readResponse, readRequest)
	if readResponse.Code != http.StatusOK {
		t.Fatalf("session cookie did not authorize safe API request: %d %s",readResponse.Code,readResponse.Body.String())
	}

	unsafeWithoutCSRF := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(`{"name":"No CSRF","group_type":"static"}`))
	unsafeWithoutCSRF.AddCookie(sessionCookie)
	unsafeWithoutCSRF.Header.Set("Content-Type","application/json")
	unsafeResponse := httptest.NewRecorder()
	handler.ServeHTTP(unsafeResponse,unsafeWithoutCSRF)
	if unsafeResponse.Code != http.StatusForbidden {
		t.Fatalf("session mutation without CSRF token returned %d, expected 403: %s",unsafeResponse.Code,unsafeResponse.Body.String())
	}

	createRequest := httptest.NewRequest(http.MethodPost, "/api/v1/groups", strings.NewReader(`{"name":"Browser Group","group_type":"static"}`))
	createRequest.AddCookie(sessionCookie)
	createRequest.AddCookie(csrfCookie)
	createRequest.Header.Set("X-Euryphos-CSRF",csrfCookie.Value)
	createRequest.Header.Set("Content-Type","application/json")
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse,createRequest)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("valid CSRF protected mutation failed: %d %s",createResponse.Code,createResponse.Body.String())
	}

	logoutRequest := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", strings.NewReader("{}"))
	logoutRequest.AddCookie(sessionCookie)
	logoutRequest.AddCookie(csrfCookie)
	logoutRequest.Header.Set("X-Euryphos-CSRF",csrfCookie.Value)
	logoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(logoutResponse,logoutRequest)
	if logoutResponse.Code != http.StatusOK { t.Fatalf("logout failed: %d %s",logoutResponse.Code,logoutResponse.Body.String()) }

	afterLogout := httptest.NewRequest(http.MethodGet, "/api/v1/groups", nil)
	afterLogout.AddCookie(sessionCookie)
	afterLogoutResponse := httptest.NewRecorder()
	handler.ServeHTTP(afterLogoutResponse,afterLogout)
	if afterLogoutResponse.Code != http.StatusUnauthorized {
		t.Fatalf("logged-out session remained authorized: %d",afterLogoutResponse.Code)
	}
}

func TestStaticGroupMembershipAddAndRemove(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "static-groups.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	_, err = db.Exec(`INSERT INTO devices(id,hostname,os_family,status) VALUES
		('device-a','workstation-a','windows','approved'),
		('device-b','workstation-b','linux','approved')`)
	if err != nil { t.Fatal(err) }
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	raw := performRequest(t, handler, http.MethodPost, "/api/v1/groups", "admin-test-token",
		`{"name":"Manually Selected","group_type":"static"}`, http.StatusCreated)
	var group struct { ID int64 `json:"id"` }
	if err := json.Unmarshal(raw, &group); err != nil || group.ID < 1 { t.Fatalf("invalid group response: %s (%v)",raw,err) }
	path := "/api/v1/groups/"+strconv.FormatInt(group.ID,10)
	performRequest(t, handler, http.MethodPost, path+"/members", "admin-test-token",
		`{"device_ids":["device-a","device-b","device-a"]}`, http.StatusOK)
	members := performRequest(t, handler, http.MethodGet, path+"/members", "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(members),"device-a") || !strings.Contains(string(members),"device-b") || strings.Count(string(members),`"id":`) != 2 {
		t.Fatalf("static group membership add/dedup failed: %s",members)
	}
	performRequest(t, handler, http.MethodDelete, path+"/members/device-a", "admin-test-token", "", http.StatusOK)
	members = performRequest(t, handler, http.MethodGet, path+"/members", "admin-test-token", "", http.StatusOK)
	if strings.Contains(string(members),"device-a") || !strings.Contains(string(members),"device-b") {
		t.Fatalf("static group member removal failed: %s",members)
	}
	var added, removed int
	if err := db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='group.members.added'").Scan(&added); err != nil { t.Fatal(err) }
	if err := db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='group.member.removed'").Scan(&removed); err != nil { t.Fatal(err) }
	if added != 1 || removed != 1 { t.Fatalf("static membership audit missing: added=%d removed=%d",added,removed) }
}

func TestDuplicateIdentityQueriesUseIndexes(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "identity-indexes.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	checkPlan := func(query string, args ...any) string {
		t.Helper()
		rows, err := db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+query, args...)
		if err != nil { t.Fatal(err) }
		defer rows.Close()
		var details []string
		for rows.Next() {
			var id, parent, notUsed int
			var detail string
			if err := rows.Scan(&id,&parent,&notUsed,&detail); err != nil { t.Fatal(err) }
			details = append(details, detail)
		}
		if err := rows.Err(); err != nil { t.Fatal(err) }
		return strings.Join(details," ")
	}
	uuidPlan := checkPlan(`SELECT id FROM devices
		WHERE lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ',''))=?`, "11112222333344445555666677778888")
	if !strings.Contains(uuidPlan,"idx_devices_smbios_uuid_normalized") {
		t.Fatalf("SMBIOS duplicate lookup is not using its expression index: %s",uuidPlan)
	}
	serialPlan := checkPlan(`SELECT id FROM devices
		WHERE lower(trim(serial_number))=? AND lower(trim(manufacturer))=? AND lower(trim(model))=?`,
		"serial-1","vendor","model")
	if !strings.Contains(serialPlan,"idx_devices_hardware_identity") {
		t.Fatalf("serial/manufacturer/model duplicate lookup is not using its composite expression index: %s",serialPlan)
	}
}

func TestEmbeddedBrowserScriptsHaveValidSyntax(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is not installed; browser script syntax check is optional locally")
	}
	for name, script := range map[string]string{"console": consoleJS, "login": loginJS} {
		path := filepath.Join(t.TempDir(), name+".js")
		if err := os.WriteFile(path, []byte(script), 0600); err != nil {
			t.Fatalf("write %s browser script: %v", name, err)
		}
		output, err := exec.Command(node, "--check", path).CombinedOutput()
		if err != nil {
			t.Fatalf("%s browser script has invalid JavaScript syntax: %v\n%s", name, err, output)
		}
	}
}

func TestNetworkInventoryNormalizesDHCPAndDeviceDetailReturnsIt(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "dhcp.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	if _, err := db.Exec("INSERT INTO devices(id,hostname,status) VALUES('dhcp-device','windows-host','approved')"); err != nil { t.Fatal(err) }
	payload := []byte(`{
		"default_gateway":"192.168.50.1",
		"dns_servers":["192.168.50.1","1.1.1.1"],
		"interfaces":[{"name":"Ethernet","index":12,"mac_address":"00-11-22-33-44-55","connection_type":"ethernet","status":"up","addresses":["192.168.50.22/24"]}],
		"dhcp_interfaces":[{"name":"Ethernet","interface_index":12,"dhcp_enabled":true,"dhcp_server":"192.168.50.1","dhcp_lease_expires":"2026-11-25T12:00:00Z","dhcp_lease_expiry_estimated":true}]
	}`)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil { t.Fatal(err) }
	server := &Server{db:db}
	if err := server.applyNetworkInventory(ctx, tx, "dhcp-device", payload, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		tx.Rollback()
		t.Fatalf("apply network inventory: %v",err)
	}
	if err := tx.Commit(); err != nil { t.Fatal(err) }
	var dhcpEnabled, leaseEstimated int
	var dhcpServer, lease, gateway, dns string
	if err := db.QueryRow(`SELECT n.dhcp_enabled,n.dhcp_server,n.dhcp_lease_expires,n.dhcp_lease_expiry_estimated,d.default_gateway,d.dns_servers_json
		FROM device_network_interfaces n JOIN devices d ON d.id=n.device_id WHERE n.device_id='dhcp-device'`).
		Scan(&dhcpEnabled,&dhcpServer,&lease,&leaseEstimated,&gateway,&dns); err != nil {
		t.Fatal(err)
	}
	if dhcpEnabled != 1 || leaseEstimated != 1 || dhcpServer != "192.168.50.1" || lease != "2026-11-25T12:00:00Z" ||
		gateway != "192.168.50.1" || !strings.Contains(dns,"1.1.1.1") {
		t.Fatalf("normalized network data mismatch: dhcp=%d server=%q lease=%q gateway=%q dns=%q",
			dhcpEnabled,dhcpServer,lease,gateway,dns)
	}
	handler := securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	detail := performRequest(t,handler,http.MethodGet,"/api/v1/devices/dhcp-device","admin-test-token","",http.StatusOK)
	for _, expected := range []string{`"network_interfaces"`,`"dhcp_enabled":true`,`"dhcp_server":"192.168.50.1"`,`"dhcp_lease_expires":"2026-11-25T12:00:00Z"`,`"dhcp_lease_expiry_estimated":true`} {
		if !strings.Contains(string(detail),expected) { t.Fatalf("device detail missing %s: %s",expected,detail) }
	}
}

func TestMigrationAddsDHCPColumnsToExistingNetworkTable(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "old-network.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE devices (
		id TEXT PRIMARY KEY, hostname TEXT NOT NULL DEFAULT '', os_family TEXT NOT NULL DEFAULT '',
		os_version TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'pending', token_hash TEXT,
		last_seen TEXT, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil { t.Fatal(err) }
	_, err = db.Exec(`CREATE TABLE device_network_interfaces (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		interface_key TEXT NOT NULL,name TEXT NOT NULL DEFAULT '',mac_address TEXT NOT NULL DEFAULT '',
		connection_type TEXT NOT NULL DEFAULT '',state TEXT NOT NULL DEFAULT '',
		addresses_json TEXT NOT NULL DEFAULT '[]',mtu INTEGER,observed_at TEXT NOT NULL,
		UNIQUE(device_id,interface_key)
	)`)
	if err != nil { t.Fatal(err) }
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatalf("upgrade old network table: %v",err) }
	columns:=map[string]bool{}
	rows,err:=db.Query("PRAGMA table_info(device_network_interfaces)")
	if err!=nil { t.Fatal(err) }
	defer rows.Close()
	for rows.Next() {
		var cid,notNull,primaryKey int
		var name,dataType string
		var defaultValue sql.NullString
		if err:=rows.Scan(&cid,&name,&dataType,&notNull,&defaultValue,&primaryKey);err!=nil { t.Fatal(err) }
		columns[name]=true
	}
	for _,name:=range []string{"dhcp_enabled","dhcp_server","dhcp_lease_expires","dhcp_lease_expiry_estimated"} {
		if !columns[name] { t.Fatalf("migration did not add %s: %#v",name,columns) }
	}
}

func TestStorageInventoryNormalizationDetailAndUnavailablePreservation(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "storage.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatal(err) }
	if _,err:=db.Exec("INSERT INTO devices(id,hostname,status) VALUES('storage-device','storage-host','approved')");err!=nil { t.Fatal(err) }
	observedAt:=time.Now().UTC().Format(time.RFC3339Nano)
	payload:=[]byte(`{
		"schema_version":1,"collection_status":"collected",
		"disks":[
			{"key":"disk0","name":"nvme0n1","model":"Example NVMe","manufacturer":"Example","serial_number":"NVME-123","interface_type":"nvme","capacity_bytes":18014398509481985,"logical_block_size":4096,"is_removable":false,"is_rotational":false,"status":"present"}
		],
		"volumes":[
			{"key":"rootfs","name":"root","parent_key":"disk0","device":"/dev/nvme0n1p2","filesystem":"ext4","mount_point":"/","capacity_bytes":1099511627777,"free_bytes":549755813889,"available_bytes":549755800000,"used_bytes":549755813888,"block_size":4096,"is_read_only":false,"status":"mounted"},
			{"key":"data-volume","name":"data","parent_key":"disk0","device":"/dev/nvme0n1p3","filesystem":"xfs","mount_point":"/data","capacity_bytes":107374182400,"free_bytes":53687091200,"available_bytes":53687000000,"used_bytes":53687091200,"block_size":4096,"is_read_only":true,"status":"mounted"}
		]
	}`)
	tx,err:=db.BeginTx(ctx,nil)
	if err!=nil { t.Fatal(err) }
	if err:=(&Server{db:db}).applyStorageInventory(ctx,tx,"storage-device",payload,observedAt);err!=nil {
		tx.Rollback()
		t.Fatalf("apply storage inventory: %v",err)
	}
	if err:=tx.Commit();err!=nil { t.Fatal(err) }

	var diskCapacity,volumeCapacity,volumeFree int64
	if err:=db.QueryRow("SELECT capacity_bytes FROM device_storage_disks WHERE device_id='storage-device' AND disk_key='disk0'").Scan(&diskCapacity);err!=nil { t.Fatal(err) }
	if err:=db.QueryRow("SELECT capacity_bytes,free_bytes FROM device_storage_volumes WHERE device_id='storage-device' AND volume_key='rootfs'").Scan(&volumeCapacity,&volumeFree);err!=nil { t.Fatal(err) }
	if diskCapacity!=18014398509481985 || volumeCapacity!=1099511627777 || volumeFree!=549755813889 {
		t.Fatalf("storage byte values lost precision: disk=%d volume=%d free=%d",diskCapacity,volumeCapacity,volumeFree)
	}

	unavailable:=[]byte(`{"schema_version":1,"collection_status":"unavailable","disks":[],"volumes":[]}`)
	tx,err=db.BeginTx(ctx,nil)
	if err!=nil { t.Fatal(err) }
	if err:=(&Server{db:db}).applyStorageInventory(ctx,tx,"storage-device",unavailable,time.Now().UTC().Format(time.RFC3339Nano));err!=nil {
		tx.Rollback()
		t.Fatalf("unavailable storage should be accepted without clearing known data: %v",err)
	}
	if err:=tx.Commit();err!=nil { t.Fatal(err) }
	var diskCount,volumeCount int
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_disks WHERE device_id='storage-device'").Scan(&diskCount);err!=nil { t.Fatal(err) }
	if err:=db.QueryRow("SELECT count(*) FROM device_storage_volumes WHERE device_id='storage-device'").Scan(&volumeCount);err!=nil { t.Fatal(err) }
	if diskCount!=1 || volumeCount!=2 { t.Fatalf("unavailable collection erased known storage: disks=%d volumes=%d",diskCount,volumeCount) }

	handler:=securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	detail:=performRequest(t,handler,http.MethodGet,"/api/v1/devices/storage-device","admin-test-token","",http.StatusOK)
	for _,expected:=range []string{`"storage_disks"`,`"storage_volumes"`,`"model":"Example NVMe"`,`"mount_point":"/data"`,`"is_read_only":true`} {
		if !strings.Contains(string(detail),expected) { t.Fatalf("device detail missing %s: %s",expected,detail) }
	}
}

func TestStorageMigrationCreatesNormalizedTables(t *testing.T) {
	db,err:=openDatabase(filepath.Join(t.TempDir(),"storage-migration.db"))
	if err!=nil { t.Fatal(err) }
	defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatal(err) }
	for _,table:=range []string{"device_storage_disks","device_storage_volumes"} {
		var count int
		if err:=db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?",table).Scan(&count);err!=nil { t.Fatal(err) }
		if count!=1 { t.Fatalf("storage migration did not create table %s",table) }
	}
}

func TestServicesInventoryNormalizationAndUnavailablePreservation(t *testing.T) {
	db,err:=openDatabase(filepath.Join(t.TempDir(),"services.db"))
	if err!=nil { t.Fatal(err) }
	defer db.Close()
	ctx,cancel:=context.WithTimeout(context.Background(),10*time.Second)
	defer cancel()
	if err:=migrate(ctx,db);err!=nil { t.Fatal(err) }
	if _,err:=db.Exec("INSERT INTO devices(id,hostname,status) VALUES('service-device','service-host','approved')");err!=nil { t.Fatal(err) }
	payload:=[]byte(`{
		"schema_version":1,"collection_status":"collected","manager":"systemd",
		"services":[
			{"key":"ssh.service","name":"ssh.service","display_name":"OpenSSH server","manager":"systemd","load_state":"loaded","active_state":"active","sub_state":"running","start_type":"enabled","description":"Secure shell daemon","status":"active"},
			{"key":"disabled.service","name":"disabled.service","manager":"systemd","active_state":"","start_type":"disabled","status":""}
		]
	}`)
	server:=&Server{db:db}
	tx,err:=db.BeginTx(ctx,nil)
	if err!=nil { t.Fatal(err) }
	if err:=server.applyServicesInventory(ctx,tx,"service-device",payload,time.Now().UTC().Format(time.RFC3339Nano));err!=nil {
		tx.Rollback()
		t.Fatalf("apply services inventory: %v",err)
	}
	if err:=tx.Commit();err!=nil { t.Fatal(err) }
	var active,startType,description string
	if err:=db.QueryRow("SELECT active_state,start_type,description FROM device_services WHERE device_id='service-device' AND service_key='ssh.service'").Scan(&active,&startType,&description);err!=nil { t.Fatal(err) }
	if active!="active" || startType!="enabled" || description!="Secure shell daemon" {
		t.Fatalf("normalized service values incorrect: active=%q start=%q desc=%q",active,startType,description)
	}
	unavailable:=[]byte(`{"schema_version":1,"collection_status":"unavailable","manager":"systemd","services":[]}`)
	tx,err=db.BeginTx(ctx,nil)
	if err!=nil { t.Fatal(err) }
	if err:=server.applyServicesInventory(ctx,tx,"service-device",unavailable,time.Now().UTC().Format(time.RFC3339Nano));err!=nil {
		tx.Rollback()
		t.Fatalf("unavailable service collection should not clear known rows: %v",err)
	}
	if err:=tx.Commit();err!=nil { t.Fatal(err) }
	var count int
	if err:=db.QueryRow("SELECT count(*) FROM device_services WHERE device_id='service-device'").Scan(&count);err!=nil { t.Fatal(err) }
	if count!=2 { t.Fatalf("unavailable collection erased known services: count=%d",count) }
	handler:=securityHeaders(routes(&Server{db:db,adminToken:"admin-test-token"}))
	detail:=performRequest(t,handler,http.MethodGet,"/api/v1/devices/service-device","admin-test-token","",http.StatusOK)
	for _,expected:=range []string{`"services"`,`"ssh.service"`,`"OpenSSH server"`,`"start_type":"disabled"`} {
		if !strings.Contains(string(detail),expected) { t.Fatalf("device detail missing %s: %s",expected,detail) }
	}
}

func TestServiceControlJobPayloadIsStrictlyTyped(t *testing.T) {
	valid:=[]byte(`{"service_name":"ssh.service","operation":"restart"}`)
	if !validServiceControlPayload(valid) { t.Fatal("valid service-control payload was rejected") }
	invalid:=[][]byte{
		[]byte(`{"service_name":"../sshd","operation":"start"}`),
		[]byte(`{"service_name":"--help","operation":"start"}`),
		[]byte(`{"service_name":"systemctl","operation":"start"}`),
		[]byte(`{"service_name":"SYSTEMCTL","operation":"start"}`),
		[]byte(`{"service_name":"sshd;touch /tmp/x","operation":"start"}`),
		[]byte(`{"service_name":"sshd","operation":"exec"}`),
		[]byte(`{"service_name":"","operation":"stop"}`),
		[]byte(`{"service_name":"sshd","operation":"start;stop"}`),
	}
	for _,payload:=range invalid {
		if validServiceControlPayload(payload) { t.Errorf("unsafe service-control payload accepted: %s",payload) }
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
