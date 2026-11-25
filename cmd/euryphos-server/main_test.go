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
	if !strings.Contains(string(nextJob), jobCreated.JobID) || !strings.Contains(string(nextJob), "inventory.refresh") {
		t.Fatalf("agent did not receive queued job: %s", nextJob)
	}
	performRequest(t, handler, http.MethodPost, "/api/v1/devices/"+enrollment.DeviceID+"/jobs/"+jobCreated.JobID+"/result", enrollment.DeviceToken,
		`{"status":"succeeded","result":{"message":"refreshed"}}`, http.StatusOK)
	jobStatus := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+jobCreated.JobID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobStatus), `"status":"completed"`) {
		t.Fatalf("job did not reach completed state: %s", jobStatus)
	}
	var jobAuditRows int
	if err := db.QueryRow("SELECT count(*) FROM audit_log WHERE event_type='job.execution.result' AND target_id=?", jobCreated.JobID).Scan(&jobAuditRows); err != nil || jobAuditRows != 1 {
		t.Fatalf("job result audit event missing: rows=%d error=%v", jobAuditRows, err)
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
}

func TestDuplicateDetectionFallsBackToSerialManufacturerModel(t *testing.T) {
	db, err := openDatabase(filepath.Join(t.TempDir(), "serial-duplicates.db"))
	if err != nil { t.Fatal(err) }
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrate(ctx, db); err != nil { t.Fatal(err) }
	_, err = db.Exec("INSERT INTO devices(id,manufacturer,model,serial_number,smbios_uuid) VALUES(?,?,?,?,?)",
		"serial-canonical", "Acme", "Widget", "ser-123", "")
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
	var targetID string
	if err := db.QueryRow("SELECT device_id FROM job_targets WHERE job_id=?", job.ID).Scan(&targetID); err != nil || targetID != "windows-1" {
		t.Fatalf("group job selected wrong target: %q (%v)", targetID, err)
	}
	if _, err := db.Exec("UPDATE machine_groups SET name='Renamed Group' WHERE id=?", created.ID); err != nil { t.Fatal(err) }
	performRequest(t, handler, http.MethodDelete, groupPath, "admin-test-token", "", http.StatusOK)

	jobDetail := performRequest(t, handler, http.MethodGet, "/api/v1/jobs/"+job.ID, "admin-test-token", "", http.StatusOK)
	if !strings.Contains(string(jobDetail), `"name":"Windows Servers"`) || !strings.Contains(string(jobDetail), "windows-1") {
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

	loginPage := httptest.NewRecorder()
	handler.ServeHTTP(loginPage, httptest.NewRequest(http.MethodGet, "/login", nil))
	if loginPage.Code != http.StatusOK || !strings.Contains(loginPage.Body.String(), "Sign in to Euryphos") {
		t.Fatalf("login page did not render: status=%d", loginPage.Code)
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
