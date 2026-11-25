# Euryphos

**Centralized fleet visibility and endpoint management, built in Go.**

Euryphos is an enterprise systems-management platform in early development. This repository contains the **central management server**, HTTP API, SQLite persistence layer, schema migrations, and server deployment configuration. It is intended to run once in a central environment and manage separately installed endpoint agents.

> [!WARNING]
> **Development status: early development — not production-ready.** This repository is an architectural and API foundation, not a complete endpoint-management product. Do not rely on it for production patching, configuration enforcement, or security-critical fleet operations.

## What Euryphos is being built to do

- **Fleet visibility:** continuously collect hardware, operating-system, installed-application, service, security, and configuration inventory.
- **Fleet search:** find managed devices, installed software, and configuration state across an environment.
- **Controlled operations:** target one endpoint, a selected group, or a larger fleet with authenticated and auditable jobs.
- **Patch orchestration:** discover and coordinate operating-system and approved third-party software updates.
- **Configuration management:** apply desired settings, detect drift, and verify remediation.
- **Operational evidence:** track endpoint check-ins, job outcomes, failures, pending restarts, and compliance.

These are product goals. Only the subset described under [Current implementation](#current-implementation) exists in the code today.

## Two-repository architecture

Euryphos is distributed as two independent projects:

| Repository | Responsibility |
|---|---|
| **[Euryphos](https://github.com/gitarman94/Euryphos)** (this repository) | Central Go server, HTTP API, SQLite database, migrations, server configuration, and server deployment |
| **[Euryphos-Agent](https://github.com/gitarman94/Euryphos-Agent)** | Separately installed Go endpoint agent, OS inventory providers, endpoint service integration, and Linux/Windows/macOS client installers as they are implemented |

Install the server once on a central host. Install only **Euryphos-Agent** on each managed workstation or server. The agent initiates outbound authenticated requests to the server; it does not require a local copy of the server or its SQLite database.

The previous `CommandPilot-Agent` repository is the historical client project. Its old identity, API compatibility, and installer scripts must be audited and migrated before they can be treated as supported Euryphos installation paths. The current standalone agent deliverable is maintained separately from this server repository.

## Architecture

```mermaid
flowchart LR
    Admin[IT administrator] --> UI[Operator console - planned]
    UI --> API[Euryphos server / API - Go]
    Agent[Euryphos-Agent - separate project] <-->|Authenticated HTTPS/API traffic| API
    API --> DB[(SQLite database)]
    Agent --- Host[Managed Windows, Linux, or future macOS host]
```

The server uses embedded SQLite. No separate PostgreSQL or database server is required. WAL journaling allows concurrent readers, while SQLite still serializes writers. This is a single-server/single-host design; multi-node database clustering is not part of the current implementation.

## Current implementation

This repository currently includes:

- Go HTTP server using the standard library.
- SQLite storage through the pure-Go `modernc.org/sqlite` driver.
- Startup schema creation/migration path and an initial SQL migration reference.
- Endpoint enrollment using a shared enrollment secret and per-device bearer credentials.
- Administrator approval and revocation endpoints.
- Device authentication, heartbeat, and categorized inventory ingestion.
- Administrator device listing with bounded search/filter/sort/pagination, typed device detail, inventory history, job creation, paginated job history, and job status retrieval.
- Device lifecycle controls in the console for pending enrollment approval and approved-device revocation; device details show duplicate candidates from either side of a match and allow a confirmed merge into an approved canonical record.
- A SQLite `devices` row per computer with a database-generated integer `database_id` plus the stable agent-facing UUID `id`; frequently queried identity, OS, directory, hardware, CPU, memory, storage, network summary, and agent fields are stored in typed columns.
- Normalized child rows for network interfaces (including typed DHCP enabled/server/lease expiry and an explicit estimated-expiry flag) and installed software; current category payloads plus change history for relevant facts. System history ignores volatile readings (uptime, free/available memory and disk space, battery charge/status); the latest device row still refreshes those values. This is not a metrics time-series store.
- Strong duplicate candidate detection (SMBIOS UUID, or serial + manufacturer + model), reviewable candidates, explicit merge, and continued authentication using the merged endpoint's existing token.
- Initial `audit_log` events for authentication outcomes, enrollment/approval/revocation, group changes, job actions/results, and duplicate merges.
- Static and rule-based dynamic machine groups using normalized Windows AD and Linux directory attributes, with group create/edit/delete and explicit membership management.
- Jobs can target explicit devices and/or groups. Group membership is expanded into a fixed approved-device target list at job creation; group IDs/names are retained in job history even if groups are later edited or deleted.
- Initial browser console at `/login` and `/console`: overview, searchable/paginated Devices list, device approval/revocation, device details and inventory history, duplicate review/merge, Groups with static membership controls, Jobs with per-device outcomes, and Audit log. The console is served by the Go server; no separate frontend runtime is required.
- Browser sign-in exchanges the configured administrator bearer token for an eight-hour server-side session. The session cookie is HttpOnly and SameSite Strict; session-authenticated state-changing requests require a CSRF token. `EURYPHOS_COOKIE_SECURE` defaults to `true`; set it to `false` only for trusted local HTTP testing. Use HTTPS for remote access.
- Agent job polling and result reporting endpoints.
- Docker Compose deployment with a persistent SQLite volume.

### Not implemented or not production-ready

- **Browser console is an initial implementation, not a finished enterprise UI.** It covers the main overview, Devices, device details, Groups, and Audit log views. Responsive polish, complete device field presentation, advanced group editing/member administration, fleet metrics/charts, live job monitoring, and richer navigation remain outstanding.
- Administrator APIs provide `GET /api/v1/devices/{id}` for current device identity/latest inventory and `GET /api/v1/devices/{id}/inventory/history` for bounded, cursor-paginated inventory snapshots. The Devices-list API also supports basic search across hostname/FQDN/OS/directory fields, status/OS/directory filters, allowlisted sorting, and bounded pagination. Connection presence is derived server-side from the most recent authenticated heartbeat, with a 90-second default online grace period configurable through `EURYPHOS_ONLINE_GRACE_SECONDS` (allowed range 15–3600 seconds). First/last connection timestamps are returned separately from enrollment status, and the Devices list can filter by `connection_state=online|offline|never_connected`. Full telemetry field coverage, activity types, freshness indicators, fleet-wide trend aggregation, and advanced browser workflows remain incomplete.
- Full hardware, service, security, and configuration inventory providers; current normalized columns represent the initial subset and are not the complete telemetry specification.
- Functional patch installation, software deployment/removal, configuration enforcement, and most privileged job executors.
- Production server installers, signed release artifacts, and tested upgrade/backup/restore procedures.
- A tested one-command Debian 13 / Proxmox LXC deployment.
- Certified support for any Linux distribution or Windows release; see the [support matrix](docs/SUPPORT_MATRIX.md).
- End-to-end verification of this server against the separately distributed Euryphos-Agent binary.
- Enterprise SSO/MFA/RBAC, multi-tenancy, audit retention/tamper controls and complete audit coverage, and production-hardened agent communications. The current audit log is an initial operational record, not a full compliance-grade audit system.
- Fleet-scale throughput testing, data retention, backup/restore drills, and disaster recovery guarantees.

A route or placeholder is not evidence that an end-user capability is complete. Review [implementation status](docs/IMPLEMENTATION_STATUS.md) and the [completion plan](docs/COMPLETION_PLAN.md).

## Platform direction

The product roadmap includes:

- **Arch family:** Arch Linux and CachyOS, with EndeavourOS and Manjaro as additional candidates.
- **Debian family:** Debian, Ubuntu LTS, Linux Mint (Ubuntu-based editions), and Linux Mint Debian Edition (LMDE).
- **Fedora family:** Fedora Workstation/Server and Nobara.
- **Image-based Linux:** Bazzite and other Fedora Atomic variants through image-aware update providers.
- **Windows:** workstation and server editions, with supported versions/builds to be defined.
- **macOS:** a future Euryphos-Agent implementation for Intel and Apple Silicon.

These are **planned targets, not certified compatibility claims**. Installation, inventory, patching, upgrades, rollback, and server communication must be tested separately for every target before support is claimed.

## Getting started — development build

**Requirements:** Go 1.23 or later. The server embeds SQLite; a separate database service is not required.

Clone this server repository:

```bash
git clone https://github.com/gitarman94/Euryphos.git
cd Euryphos
```

Download dependencies and run tests:

```bash
go mod download
go test ./...
go vet ./...
```

Set independent high-entropy administrator and enrollment secrets, then start the development server:

```bash
export EURYPHOS_ADMIN_TOKEN="$(openssl rand -hex 32)"
export EURYPHOS_ENROLLMENT_TOKEN="$(openssl rand -hex 32)"
export EURYPHOS_DB_PATH="./data/euryphos.db"
export EURYPHOS_LISTEN=":8080"
go run ./cmd/euryphos-server
```

Check server health:

```bash
curl --fail http://127.0.0.1:8080/api/health
```

This is a development startup procedure, not a production deployment recipe. Do not expose the development listener directly to the Internet. Production deployment needs TLS, secret management, restrictive network access, service hardening, safe database backups, and tested upgrade/recovery procedures.

### Docker Compose

Create `.env` from the example, replace both placeholder values with unique random secrets, and start the server:

```bash
cp .env.example .env
# Edit .env and set unique EURYPHOS_ADMIN_TOKEN and EURYPHOS_ENROLLMENT_TOKEN values.
docker compose up -d --build
```

The database is stored in the `euryphos-data` named volume mounted at `/var/lib/euryphos`. Back up this persistent data using SQLite's online backup API or a tested quiesced backup procedure. Do not assume that copying only the `.db` file during active WAL writes is sufficient.

## Configuration

| Variable | Purpose | Default |
|---|---|---|
| `EURYPHOS_LISTEN` | HTTP listen address | `:8080` |
| `EURYPHOS_DB_PATH` | SQLite database file | `./data/euryphos.db` |
| `EURYPHOS_ADMIN_TOKEN` | Administrator API bearer secret | Required |
| `EURYPHOS_ENROLLMENT_TOKEN` | Endpoint enrollment bearer secret | Required |
| `EURYPHOS_AUTO_MIGRATE` | Apply initial schema on startup | `true` |

Never reuse development secrets in production. Do not expose an unencrypted HTTP listener to untrusted networks; terminate TLS at a trusted reverse proxy or configure a hardened TLS deployment before production use.

## Current API surface

| Method | Endpoint | Purpose |
|---|---|---|
| `GET` | `/api/health` | Health check |
| `POST` | `/api/v1/enrollment` | Request endpoint enrollment |
| `POST` | `/api/v1/devices/{id}/approve` | Approve a pending endpoint (administrator) |
| `POST` | `/api/v1/devices/{id}/revoke` | Revoke an endpoint (administrator) |
| `POST` | `/api/v1/devices/{id}/heartbeat` | Endpoint heartbeat (device credential) |
| `POST` | `/api/v1/devices/{id}/inventory` | Submit inventory category (device credential) |
| `GET` | `/api/v1/devices` | List endpoints (administrator) |
| `GET` | `/api/v1/jobs` | List recent jobs with bounded pagination and per-target counters (administrator) |
| `POST` | `/api/v1/jobs` | Create a job for approved endpoints and/or machine groups (administrator) |
| `GET` | `/api/v1/devices/{id}/jobs/next` | Claim next queued endpoint job (device credential) |
| `POST` | `/api/v1/devices/{id}/jobs/{job}/result` | Report job result (device credential) |
| `GET` | `/api/v1/jobs/{id}` | Retrieve job status, selected group snapshots, and per-device outcomes (administrator) |
| `GET` | `/api/v1/groups` | List machine groups (administrator) |
| `POST` | `/api/v1/groups` | Create a static or dynamic group (administrator) |
| `PUT` | `/api/v1/groups/{id}` | Update group definition and reevaluate dynamic membership (administrator) |
| `DELETE` | `/api/v1/groups/{id}` | Delete a group without deleting past job snapshots (administrator) |

The API is an early implementation contract and may change before a stable release. Administrative API calls accept the configured administrator bearer token; browser users sign in at `/login` and use a short-lived server-side session with CSRF protection. Endpoint requests require an approved device token. See [Go architecture](docs/GO_ARCHITECTURE.md).

## Security and operational notes

- Configure unique, high-entropy administrator and enrollment secrets; never use example values in production.
- Put the service behind TLS and restrict access to administrative endpoints.
- Keep the SQLite database and configuration files restricted to the server service account.
- Use SQLite's online backup facility or a tested quiesced procedure; include WAL behavior in backup/restore testing.
- Do not enable arbitrary shell execution as a remote action. Job executors must be typed, narrowly allowlisted, authorized, and auditable.
- Review implementation status and complete the threat model before production deployment.

## Documentation

- [`docs/PRODUCT_OVERVIEW.md`](docs/PRODUCT_OVERVIEW.md) — product goals and requirements.
- [`docs/DEVICE_MANAGEMENT_UI_SPEC.md`](docs/DEVICE_MANAGEMENT_UI_SPEC.md) — Devices table, complete device details, telemetry fields, API/storage design, and privacy acceptance criteria.
- [`docs/GO_ARCHITECTURE.md`](docs/GO_ARCHITECTURE.md) — server/API and SQLite architecture.
- [`docs/REPOSITORY_LAYOUT.md`](docs/REPOSITORY_LAYOUT.md) — server and independently distributed agent responsibilities.
- [`docs/SUPPORT_MATRIX.md`](docs/SUPPORT_MATRIX.md) — planned platform and distribution support.
- [`docs/INSTALLER_STATUS.md`](docs/INSTALLER_STATUS.md) — server installation and deployment readiness.
- [`docs/IMPLEMENTATION_STATUS.md`](docs/IMPLEMENTATION_STATUS.md) — implemented code and verified limitations.
- [`docs/COMPLETION_PLAN.md`](docs/COMPLETION_PLAN.md) — engineering roadmap and release acceptance checklist.
- [`docs/BRAND_IDENTITY.md`](docs/BRAND_IDENTITY.md) — product name and pronunciation.

## License and commercial use

Euryphos is source-available under the **Euryphos Personal Use License (EPUL) 1.0**. Individuals may use and modify it free of charge for their own personal devices, home lab, hobby, and self-directed learning. Use by or on behalf of a company, corporation, or other business—including managing its devices or infrastructure—requires a separate written commercial license. Other institutional situations are addressed in [`LICENSE`](LICENSE).

This is **not an OSI-approved open-source license**. Read [`LICENSE`](LICENSE) and [`COMMERCIAL-LICENSING.md`](COMMERCIAL-LICENSING.md) before use. Third-party components and previously granted rights remain subject to their applicable licenses; this license does not withdraw rights already granted under another license.


## SQLite device records, groups, duplicates, and audit

The server keeps one row per computer in `devices`. Its integer `database_id` is generated from SQLite's rowid and is unique within the database; the text `id` remains the stable UUID used by the separately installed agent and existing API routes. Scalar facts that are commonly filtered or displayed are typed columns. Repeating facts are stored as child rows: `device_network_interfaces` and `installed_software`. The JSON category store remains for extensible provider payloads; it is not the authoritative source for the typed columns used by the fleet directory.

Duplicate detection records candidates rather than merging automatically. A matching SMBIOS UUID is the strongest signal; the fallback is serial number + manufacturer + model. An administrator reviews candidates and explicitly selects a canonical computer. The merge moves history, normalized interface/software data, memberships, and jobs; missing canonical scalar fields are filled from the source. The source record remains as a merged alias and its existing agent credential redirects authenticated requests to the canonical record.

Groups can be static or dynamic. Dynamic groups use a bounded, allowlisted rule set over normalized fields such as OS family/distribution, directory type/domain/OU/site, manufacturer/model, and agent version. Rules are evaluated in SQLite with bound parameters. Windows AD and Linux SSSD/realmd directory facts share the same fields.

The initial `audit_log` stores authentication outcomes, enrollment and approval/revocation, group changes, job creation/results, and duplicate merges. It is not yet a substitute for user accounts, RBAC, immutable/tamper-evident retention, or a compliance audit pipeline.
