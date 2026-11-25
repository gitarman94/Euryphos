-- SQLite schema reference. Runtime migrations are applied by cmd/euryphos-server/migrate.
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS devices (
    id TEXT PRIMARY KEY,
    database_id INTEGER,
    hostname TEXT NOT NULL DEFAULT '',
    fqdn TEXT NOT NULL DEFAULT '',
    os_family TEXT NOT NULL DEFAULT '',
    os_version TEXT NOT NULL DEFAULT '',
    os_distribution TEXT NOT NULL DEFAULT '',
    os_distribution_version TEXT NOT NULL DEFAULT '',
    os_architecture TEXT NOT NULL DEFAULT '',
    os_build TEXT NOT NULL DEFAULT '',
    os_install_date TEXT NOT NULL DEFAULT '',
    kernel_version TEXT NOT NULL DEFAULT '',
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
);

-- database_id is a database-generated integer, separate from the agent-facing UUID.
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_database_id ON devices(database_id);
CREATE TRIGGER IF NOT EXISTS trg_devices_database_id_insert
AFTER INSERT ON devices WHEN NEW.database_id IS NULL
BEGIN
    UPDATE devices SET database_id = NEW.rowid WHERE rowid = NEW.rowid;
END;

CREATE INDEX IF NOT EXISTS idx_devices_status ON devices(status);
CREATE INDEX IF NOT EXISTS idx_devices_hostname ON devices(hostname);
CREATE INDEX IF NOT EXISTS idx_devices_distribution ON devices(os_family, os_distribution, os_distribution_version);
CREATE INDEX IF NOT EXISTS idx_devices_directory_domain_ou ON devices(directory_domain, organizational_unit);
CREATE INDEX IF NOT EXISTS idx_devices_serial_number ON devices(serial_number);
CREATE INDEX IF NOT EXISTS idx_devices_smbios_uuid ON devices(smbios_uuid);
CREATE INDEX IF NOT EXISTS idx_devices_smbios_uuid_normalized ON devices(lower(replace(replace(replace(replace(smbios_uuid,'-',''),'{',''),'}',''),' ','')));
CREATE INDEX IF NOT EXISTS idx_devices_hardware_identity ON devices(serial_number COLLATE NOCASE, manufacturer COLLATE NOCASE, model COLLATE NOCASE);
CREATE INDEX IF NOT EXISTS idx_devices_os_family_distribution ON devices(os_family, os_distribution, os_distribution_version);
CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);

CREATE TABLE IF NOT EXISTS device_duplicate_candidates (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id),
    candidate_device_id TEXT NOT NULL REFERENCES devices(id),
    matched_on TEXT NOT NULL,
    confidence TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(device_id, candidate_device_id)
);

CREATE TABLE IF NOT EXISTS audit_log (
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
);
CREATE INDEX IF NOT EXISTS idx_audit_log_time ON audit_log(occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_target ON audit_log(target_type, target_id, occurred_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_log_event ON audit_log(event_type, occurred_at DESC);

CREATE TABLE IF NOT EXISTS machine_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    group_type TEXT NOT NULL CHECK(group_type IN ('static','dynamic')),
    match_operator TEXT NOT NULL DEFAULT 'all' CHECK(match_operator IN ('all','any')),
    rules_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS device_group_memberships (
    group_id INTEGER NOT NULL REFERENCES machine_groups(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    added_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    added_by TEXT NOT NULL DEFAULT '',
    PRIMARY KEY(group_id, device_id)
);
CREATE INDEX IF NOT EXISTS idx_machine_groups_type ON machine_groups(group_type);

CREATE INDEX IF NOT EXISTS idx_group_memberships_device ON device_group_memberships(device_id);

-- Repeating facts belong in child tables; scalar facts stay on devices.
CREATE TABLE IF NOT EXISTS device_network_interfaces (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    interface_key TEXT NOT NULL,
    name TEXT NOT NULL DEFAULT '',
    mac_address TEXT NOT NULL DEFAULT '',
    connection_type TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT '',
    addresses_json TEXT NOT NULL DEFAULT '[]',
    mtu INTEGER,
    observed_at TEXT NOT NULL,
    UNIQUE(device_id, interface_key)
);
CREATE INDEX IF NOT EXISTS idx_network_interfaces_device ON device_network_interfaces(device_id);
CREATE INDEX IF NOT EXISTS idx_network_interfaces_mac ON device_network_interfaces(mac_address);

CREATE TABLE IF NOT EXISTS installed_software (
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
);
CREATE INDEX IF NOT EXISTS idx_installed_software_name_version ON installed_software(name_normalized, version);
CREATE INDEX IF NOT EXISTS idx_installed_software_device ON installed_software(device_id);

CREATE TABLE IF NOT EXISTS device_inventory (
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    payload TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(device_id, category)
);
CREATE INDEX IF NOT EXISTS idx_inventory_category ON device_inventory(category);

CREATE TABLE IF NOT EXISTS device_inventory_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    category TEXT NOT NULL,
    payload TEXT NOT NULL,
    observed_at TEXT NOT NULL,
    received_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_inventory_history_device_category_id
    ON device_inventory_history(device_id, category, id DESC);
CREATE INDEX IF NOT EXISTS idx_inventory_history_device_received
    ON device_inventory_history(device_id, received_at DESC);

CREATE TABLE IF NOT EXISTS jobs (
    id TEXT PRIMARY KEY,
    action TEXT NOT NULL,
    payload TEXT NOT NULL DEFAULT '{}',
    status TEXT NOT NULL DEFAULT 'queued',
    created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS job_targets (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    device_id TEXT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'queued',
    result TEXT,
    error TEXT,
    updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY(job_id, device_id)
);
-- Keep the selected group name and ID with the job, independent of later group edits/deletion.
-- This intentionally has no machine_groups foreign key: job history must remain readable.
CREATE TABLE IF NOT EXISTS job_group_targets (
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL,
    group_name TEXT NOT NULL,
    PRIMARY KEY(job_id, group_id)
);
CREATE INDEX IF NOT EXISTS idx_job_group_targets_group ON job_group_targets(group_id);

CREATE INDEX IF NOT EXISTS idx_job_targets_device_status ON job_targets(device_id, status);
CREATE INDEX IF NOT EXISTS idx_jobs_status_created ON jobs(status, created_at);

INSERT OR IGNORE INTO schema_migrations(version) VALUES (1);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (2);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (3);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (4);
INSERT OR IGNORE INTO schema_migrations(version) VALUES (5);
