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
- There is no finished operator dashboard, SSO/MFA/RBAC system, durable push channel, message broker, worker pool, comprehensive audit system, or package repository.
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
