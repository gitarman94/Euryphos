# Implementation status

**Architecture baseline:** Go management server + separately distributed Go endpoint agent + SQLite embedded database.

## Present in this repository

- Go standard-library HTTP server and embedded browser-console handlers/assets; the endpoint agent remains separately distributed from the Euryphos-Agent project.
- SQLite storage implementation using the pure-Go `modernc.org/sqlite` driver; PostgreSQL driver/configuration and Compose database service removed.
- SQLite WAL, foreign keys, a 10-second busy timeout, bounded connection pool, local database path, and restrictive Unix file permissions.
- Initial schema for devices, categorized inventory, jobs, job targets, and schema version tracking.
- Enrollment, administrator approval/revocation, device authentication, heartbeat, categorized inventory upload, device listing/detail/history, normalized interface, disk, volume, and service details, duplicate review/merge, groups, paginated job history, job creation/claiming, and job-result aggregation endpoints.
- Server integration tests now run against the real `modernc.org/sqlite` driver in GitHub Actions and cover enrollment, approval, heartbeat, inventory, job dispatch/results, schema upgrades, typed inventory, group membership, duplicate detection/merge, audit, and history behavior.
- Docker Compose deployment with a persistent local volume instead of a PostgreSQL container.
- Updated product, architecture, distribution support matrix, installer audit, and completion-plan documentation.

## Verified limitations / incomplete work

- The server repository has no Linux server installer or Windows server installer. Client installers belong in the separate Euryphos-Agent repository and are not yet included there. The old prototype scripts target Rust/Python and are not valid for the current Go implementation.
- Formal OS support is not yet certified. Arch, Debian, Fedora, and listed derivatives are planned targets, not current compatibility claims.
- The agent now has an initial Linux inventory provider for DMI identity, CPU/RAM, uptime/boot time, root filesystem, batteries where exposed, network interfaces, DNS/default gateway, and DPKG/RPM/pacman software inventory. Distribution-aware compatibility, Debian/apt detail, Fedora/dnf/dnf5 integration, Bazzite/rpm-ostree support, and all platform validation remain incomplete; no Linux distribution is certified.
- Most privileged job executors are not implemented. Patch installation, software deployment/removal, configuration enforcement, service control, reboot/shutdown, and agent self-update remain incomplete.
- No native macOS client exists yet; it is explicitly in the roadmap.
- **Web UI:** an initial Go-served browser console is implemented at `/login` and `/console`. It includes fleet overview counts, searchable/paginated Devices, device approval/revocation, device detail with current inventory and change history, duplicate candidate review and explicit merge, group listing/creation/edit/deletion, static membership add/remove, dynamic-rule JSON editing and reevaluation, group inventory-refresh jobs, paginated Jobs and per-device outcome views, and an audit log view. The console uses an eight-hour server-side session; the session cookie is HttpOnly/SameSite Strict, and unsafe cookie-authenticated API requests require a CSRF token. The configured admin bearer token is exchanged only during login and is not stored by the frontend. A visual multi-condition rule builder, bulk member selection, charts, live job monitoring, full responsive/accessibility review, SSO/MFA/RBAC, and multi-user identity remain outstanding.
- **Device and job APIs:** the Devices list supports search, status/OS/directory/connection filters, allowlisted sorting, and bounded pagination. Jobs support bounded pagination with per-target state counters; job detail retains selected group snapshots and per-device results. Device detail returns typed columns, latest inventory categories, normalized network interfaces, physical disks, mounted volumes, and service records; history is change-only, cursor-paginated, and retains observations when a category payload changes. The initial browser console consumes these APIs. Activity aggregation, category freshness states, complete field presentation, and fleet metrics remain incomplete. The full field catalog is in `docs/DEVICE_MANAGEMENT_UI_SPEC.md`.
- **Endpoint telemetry:** the agent now uploads system, network, storage, services, and software categories. Initial normalized Linux/Windows records include physical disks, mounted/logical volumes, and system service state/start configuration, alongside hardware/CPU/memory/boot/network and directory facts. The server stores disk and volume facts in indexed `device_storage_disks` and `device_storage_volumes` tables and services in `device_services`; the console exposes these on device detail pages. Unavailable storage/service collection preserves last-known records; unchanged inventory refreshes observation timestamps without rebuilding child rows. Full inventory is still incomplete: SMART/disk health, RAID/device-mapper relationships, controller metadata, certificates, scheduled tasks, service configuration history, security posture, sessions, metrics, NetworkManager/dhclient and other Linux DHCP-provider coverage, and full platform-specific coverage remain outstanding. Windows CIM/Storage/Win32_Service and Linux sysfs/systemd facts are collected; Linux DHCP expiry is flagged estimated. GPS collection remains disabled/unimplemented. See the agent repository's `docs/TELEMETRY_COLLECTION_SPEC.md`.
- There is no SSO/MFA/RBAC system, durable push channel, message broker, worker pool, complete compliance-grade audit system (retention, tamper evidence, export controls), or package repository. The browser currently uses a single configured administrator token exchanged for a bounded server-side session; this is not multi-user authentication or role-based authorization. An initial operational audit table and event coverage are implemented.
- SQLite is a single-node embedded database with one writer at a time. Fleet-scale throughput, retention, backup/restore, integrity check, and recovery behavior still require measurement and tests.
- SQLite module checksums are committed in `go.sum`; CI runs `go mod tidy` and validates the real driver-backed server.

## Installer audit findings

- Current Go archive: zero installer scripts.
- Original Linux client script: uses `apt-get` only, installs Rust, clones/builds the legacy Rust client, and contains ordering/configuration defects. It does not install this Go agent and cannot support Arch/Fedora without redesign.
- Original Linux server script: installs Python/Flask/PostgreSQL, edits a PostgreSQL authentication file, and configures a Python virtual environment. It is incompatible with the Go/SQLite server.
- Original Windows client PowerShell script: downloads old `windows-client` branch executables/scripts and sets up scheduled tasks; it does not install the current Go service agent and does not verify signed Go release artifacts.
- There was no Windows server installer in the original archive.

## Validation status

GitHub Actions run [45610216033](https://github.com/gitarman94/Euryphos/actions/runs/45610216033), on commit `f5db2e22176aac95d0b77c673c55cb4f1d889026`, completed successfully on 2026-11-25. It passed:

- `go mod tidy`
- `go test ./...`
- `go vet ./...`
- `go build -trimpath ./cmd/euryphos-server`

The passing tests cover legacy SQLite migration, DHCP interface-column upgrades, normalized disk/volume/service table creation, exact 64-bit storage byte values, storage/service detail API responses, preserving last-known storage/services when collection is unavailable, and transferring storage/service rows during duplicate merges; generated device IDs, exact 64-bit system facts, device detail/history, DHCP normalization/detail response, search and presence filters, AD/Linux-compatible dynamic groups, group update/delete and static membership auditing, group-targeted job expansion/history snapshots, paginated job listing, duplicate review from either side and indexed identity query plans, rejection of OEM/unknown identity placeholders, explicit merge and old-token continuity, audit events, browser session/CSRF and root redirects, served browser assets, JavaScript syntax, and change-only inventory-history behavior.

Duplicate candidates are checked only when a device first supplies or changes hardware identity. Indexed SMBIOS UUID and serial/manufacturer/model lookups avoid repeating a fleet scan on ordinary telemetry refreshes.

This validates the tested Go/SQLite code paths. It does not certify production deployment, fleet-scale throughput, backup/restore, trusted-proxy configuration, full browser accessibility/security review, or end-to-end operation with installed endpoint agents.

## Separate endpoint-agent repository

- The current standalone endpoint repository is `https://github.com/gitarman94/Euryphos-Agent`. It contains the independently maintained Go agent and has no server or SQLite runtime dependency.
- The former `CommandPilot-Agent` project is historical. Its source, dependencies, installer scripts, commit history, copyright notices, and prior GPL-3.0 grant require file-level provenance review before reuse or relicensing.
- The current agent repository is not yet production-installable. Its Linux/Windows installers, signed versioned releases, and end-to-end validation against the live Euryphos server remain outstanding. See `docs/REPOSITORY_LAYOUT.md`.


## Device management UI and complete inventory requirements

Device list and full device-detail requirements are specified in `docs/DEVICE_MANAGEMENT_UI_SPEC.md`. An initial browser console now presents the searchable Devices list, device details, inventory history, Groups, and Audit log using the administrator APIs. It does not yet cover the complete field catalog, activity timeline, category freshness, fleet trend aggregation, or all specified administrative workflows. The endpoint telemetry requirements, including client-local GPS opt-in with a default-off hard disable, are specified in the Euryphos-Agent repository. These are requirements only; no listed field or UI page should be considered implemented until source code, persistence, and end-to-end tests demonstrate it.


## Inventory history and device detail implementation

Implemented in the server source:

- Accepted inventory uploads are written atomically to both the latest-category table and an append-only snapshot history table.
- `GET /api/v1/devices/{id}` returns device identity fields and the latest payload for each inventory category; credentials and token hashes are excluded.
- `GET /api/v1/devices/{id}/inventory/history` returns bounded, cursor-paginated snapshots with optional category filtering.
- SQLite indexes support device/category timeline reads. Existing databases receive the table through the server's startup migration routine.

Integration assertions for latest-value replacement, historical paging, and safe detail responses run in the current GitHub Actions validation workflow.


## First device inventory foundation milestone

The server now writes each accepted inventory submission transactionally to an append-only `device_inventory_history` table while updating the existing latest-value `device_inventory` row. New administrator APIs return device detail and bounded, cursor-paginated inventory history. An SQLite migration creates the history table and indexes on existing installations.

Server integration tests cover latest-payload replacement, history paging, and exclusion of credential hashes from the detail API; the current server CI runs these tests against the real SQLite driver.


## Device list API implementation

`GET /api/v1/devices` now supports:

- `q`: case-insensitive substring search across hostname, OS family, and OS version (maximum 200 characters).
- `status`: exact `pending`, `approved`, or `revoked` filter.
- `os_family`: exact OS-family filter (maximum 40 characters).
- `sort`: `hostname`, `os_family`, `os_version`, `status`, `last_seen`, or `created_at`.
- `direction`: `asc` or `desc`.
- `page`: 1-based page number (default 1).
- `page_size`: 1–200 (default 50).

The response includes `items`, current-page `count`, filtered `total`, `page`, `page_size`, and `has_more`. This is an initial device directory API, not a complete fleet search implementation across all inventory fields.


## Server-derived connection presence

Device records now retain server-derived `first_connected_at` and `last_connected_at` timestamps. Authenticated heartbeats set the first timestamp once and update the last timestamp; inventory ingestion also refreshes the last-contact timestamp. The Devices-list and device-detail responses include `connection_state` with `online`, `offline`, `never_connected`, or `unknown` values, while the existing `status` remains the separate enrollment/approval state.

The server uses a 90-second default online grace period. Set `EURYPHOS_ONLINE_GRACE_SECONDS` to a value from 15 to 3600 to tune it for the deployment's heartbeat cadence and network conditions. Existing SQLite databases are upgraded by adding the two timestamp columns if they are absent.


The Devices-list API also accepts `connection_state=online`, `offline`, or `never_connected`. Presence is calculated from server timestamps and the same online-grace threshold, not from a client-reported status string.


## Automated validation

GitHub Actions run [45610991148](https://github.com/gitarman94/Euryphos/actions/runs/45610991148), on commit `28abf424cf2124ad57a3cebf8db67491088b8774`, completed successfully on 2026-11-25. `go mod tidy`, `go test ./...`, `go vet ./...`, and `go build -trimpath ./cmd/euryphos-server` all passed.


## Normalized device database and management operations

Implemented in the server's existing Go entry point and SQLite migration path:

- One `devices` row per computer. `database_id` is a unique SQLite-generated integer; `id` remains the agent-facing UUID to preserve existing endpoint credentials and API compatibility.
- Typed scalar columns for hostname/FQDN, OS family/version/distribution/build/architecture, directory type/domain/OU/site, manufacturer/model/serial/SMBIOS UUID/chassis, CPU, memory, BIOS, boot/uptime, root filesystem, timezone, agent version, and network summary.
- Repeating network interfaces and installed software are stored in indexed child tables, not serialized as a single device column. Network interfaces now include typed DHCP enabled/server/lease-expiry fields; Windows CIM lease expiry is reported as collected, while systemd-networkd Linux expiry is explicitly marked estimated from the lease file timestamp. Current category JSON remains for flexible provider data and is separately indexed by device/category.
- Inventory history stores change-relevant snapshots. System history fingerprints ignore volatile uptime, free/available memory and disk readings, and battery charge/status; current typed values still refresh on each scan. Unchanged network/software scans refresh freshness without deleting and recreating normalized child rows. This change history is not a metrics time-series store.
- Duplicate candidates are detected using SMBIOS UUID or serial + manufacturer + model. No automatic merge occurs. Administrators review candidates and explicitly merge; the source record remains as a merged alias so its current agent token can continue operating against the canonical record.
- Static groups support explicit membership. Dynamic groups support `all`/`any` matching with `eq`, `ne`, `contains`, `starts_with`, and `ends_with` against allowlisted typed fields. Rules compile to parameterized SQLite predicates; membership changes are materialized in `device_group_memberships`. Administrators can edit and delete groups; group type is immutable to avoid silently converting manual membership into rule-managed membership.
- Jobs accept explicit device IDs and/or group IDs. The server expands group membership into approved, non-merged device targets in the same SQLite transaction, deduplicates overlaps, and records each selected group ID/name in `job_group_targets`. That snapshot remains available in job detail after group rename or deletion. This is target selection only; executor coverage and durable campaign/retry orchestration remain incomplete.
- Directory fields are normalized across platforms: Windows provider reports AD domain and OU where domain services are available; Linux provider reads SSSD/realmd configuration and reports AD/LDAP domain and OU context where exposed.
- `audit_log` records initial authentication, enrollment, approval/revocation, group mutation, job, and merge events.

Not complete: automatic identity resolution across ambiguous identifiers, advanced duplicate-review workflows and bulk reconciliation, full AD/LDAP discovery/OU resolution across every distro, full RBAC, audit retention/tamper-proofing/export governance, and production load/restore validation.
