# Implementation status

**Architecture baseline:** Go management server + separately distributed Go endpoint agent + SQLite embedded database.

## Present in this repository

- Go server entry point only; the endpoint agent is distributed from the separate Euryphos-Agent project.
- SQLite storage implementation using the pure-Go `modernc.org/sqlite` driver; PostgreSQL driver/configuration and Compose database service removed.
- SQLite WAL, foreign keys, a 10-second busy timeout, bounded connection pool, local database path, and restrictive Unix file permissions.
- Initial schema for devices, categorized inventory, jobs, job targets, and schema version tracking.
- Enrollment, administrator approval/revocation, device authentication, heartbeat, categorized inventory upload, device listing, job creation/claiming, and job-result aggregation endpoints.
- Added a server integration test for enrollment → approval → heartbeat → inventory → job dispatch → result against a SQLite file and HTTP handlers. It has not been executed here because the module dependency cannot be downloaded.
- Docker Compose deployment with a persistent local volume instead of a PostgreSQL container.
- Updated product, architecture, distribution support matrix, installer audit, and completion-plan documentation.

## Verified limitations / incomplete work

- The server repository has no Linux server installer or Windows server installer. Client installers belong in the separate Euryphos-Agent repository and are not yet included there. The old prototype scripts target Rust/Python and are not valid for the current Go implementation.
- Formal OS support is not yet certified. Arch, Debian, Fedora, and listed derivatives are planned targets, not current compatibility claims.
- The agent has basic host and installed-software inventory, but distribution identification, Arch/pacman collection, Debian/apt nuances, Fedora/dnf support, and Bazzite/image-aware update management remain incomplete.
- Most privileged job executors are not implemented. Patch installation, software deployment/removal, configuration enforcement, service control, reboot/shutdown, and agent self-update remain incomplete.
- No native macOS client exists yet; it is explicitly in the roadmap.
- **Web UI:** no browser-based dashboard, Devices list, or per-device details page exists yet. No frontend assets/templates, browser authentication/session flow, or working navigation is present.
- **Device APIs:** `GET /api/v1/devices` returns only the current summary fields. `GET /api/v1/devices/{id}` and `GET /api/v1/devices/{id}/inventory/history` now provide initial detail and snapshot retrieval. Full field coverage, activity endpoints, advanced fleet filtering, and chart-ready metric aggregation remain incomplete; the Devices list now supports basic server-side search, status/OS filters, sorting, and bounded pagination. the required field catalog is specified in `docs/DEVICE_MANAGEMENT_UI_SPEC.md`.
- **Endpoint telemetry:** the current agent does not collect most detailed identity, domain/OU, hardware, boot, storage, network, certificate, battery, GPS, or user-session facts. The field and privacy contract is specified in the Euryphos-Agent repository's `docs/TELEMETRY_COLLECTION_SPEC.md`.
- There is no SSO/MFA/RBAC system, durable push channel, message broker, worker pool, comprehensive audit system, or package repository.
- SQLite is a single-node embedded database with one writer at a time. Fleet-scale throughput, retention, backup/restore, integrity check, and recovery behavior still require measurement and tests.
- `go.sum` still needs to be populated by `go mod tidy`/`go mod download` in a network-enabled environment. This execution environment cannot resolve `proxy.golang.org`, so the SQLite driver dependency cannot be downloaded and a real driver-backed Go build/test cannot be completed here.

## Installer audit findings

- Current Go archive: zero installer scripts.
- Original Linux client script: uses `apt-get` only, installs Rust, clones/builds the legacy Rust client, and contains ordering/configuration defects. It does not install this Go agent and cannot support Arch/Fedora without redesign.
- Original Linux server script: installs Python/Flask/PostgreSQL, edits a PostgreSQL authentication file, and configures a Python virtual environment. It is incompatible with the Go/SQLite server.
- Original Windows client PowerShell script: downloads old `windows-client` branch executables/scripts and sets up scheduled tasks; it does not install the current Go service agent and does not verify signed Go release artifacts.
- There was no Windows server installer in the original archive.

## Validation status for this revision

- Go source formatting: `gofmt` completed.
- Historical agent build evidence applied to the extracted endpoint source; the standalone Euryphos-Agent archive is validated separately. Cross-compilation is not OS product certification.
- Go server source: build and `go vet` passed using a temporary compile-only replacement for the unavailable SQLite driver module. This verifies Go syntax and type usage but does not test SQLite connectivity or SQL through the real Go driver.
- Server non-database unit tests (`TestUUIDFormat`, `TestCredentialHash`, `TestBearerComparison`): passed against the compile-only replacement.
- SQLite schema and representative SQL paths: passed using Python's SQLite 3.46.1 runtime, including schema creation, inventory UPSERT, job creation, atomic `UPDATE ... RETURNING` job claim, and completion aggregation. This validates the SQL syntax/logic against SQLite, not `modernc.org/sqlite` integration.
- Real-driver `go test ./...` is blocked: `modernc.org/sqlite` is not cached and this environment cannot resolve `proxy.golang.org`; `go.sum` must be populated with `go mod tidy` or `go mod download` in a network-enabled Go environment.
- Docker CLI/daemon was unavailable here, so the container image was not built or run.
- Linux installer, Windows installer, and macOS client do not exist yet; no OS VM installation tests have been completed.

## Separate endpoint-agent repository

- The current standalone endpoint repository is `https://github.com/gitarman94/Euryphos-Agent`. It contains the independently maintained Go agent and has no server or SQLite runtime dependency.
- The former `CommandPilot-Agent` project is historical. Its source, dependencies, installer scripts, commit history, copyright notices, and prior GPL-3.0 grant require file-level provenance review before reuse or relicensing.
- The current agent repository is not yet production-installable. Its Linux/Windows installers, signed versioned releases, and end-to-end validation against the live Euryphos server remain outstanding. See `docs/REPOSITORY_LAYOUT.md`.


## Device management UI and complete inventory requirements

Device list and full device-detail requirements are specified in `docs/DEVICE_MANAGEMENT_UI_SPEC.md`. The current repository still has no browser UI. Initial administrator device-detail and inventory-history APIs are now implemented, but they do not yet cover the complete field catalog, activity timeline, or trend aggregation. The endpoint telemetry requirements, including client-local GPS opt-in with a default-off hard disable, are specified in the Euryphos-Agent repository. These are requirements only; no listed field or UI page should be considered implemented until source code, persistence, and end-to-end tests demonstrate it.


## Inventory history and device detail implementation

Implemented in the server source:

- Accepted inventory uploads are written atomically to both the latest-category table and an append-only snapshot history table.
- `GET /api/v1/devices/{id}` returns device identity fields and the latest payload for each inventory category; credentials and token hashes are excluded.
- `GET /api/v1/devices/{id}/inventory/history` returns bounded, cursor-paginated snapshots with optional category filtering.
- SQLite indexes support device/category timeline reads. Existing databases receive the table through the server's startup migration routine.

Integration assertions now cover latest-value replacement, historical paging, and safe detail responses. They have not yet been executed in a build environment.


## First device inventory foundation milestone

The server now writes each accepted inventory submission transactionally to an append-only `device_inventory_history` table while updating the existing latest-value `device_inventory` row. New administrator APIs return device detail and bounded, cursor-paginated inventory history. An SQLite migration creates the history table and indexes on existing installations.

Server integration tests were extended to cover latest-payload replacement, history paging, and exclusion of credential hashes from the detail API. The test suite has not been executed in this environment.


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
