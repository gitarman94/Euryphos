# Euryphos — Project Context and Continuation Handoff

**Canonical continuation document. Read this before changing code whenever chat history is missing.**
**Snapshot date:** 2026-11-25
**Repository:** `gitarman94/Euryphos` — central server only
**Companion repository:** [Euryphos-Agent](https://github.com/gitarman94/Euryphos-Agent) — separately installed endpoint client
**Project status:** Early development. Not production-ready.

This document deliberately consolidates the product definition, architecture decisions, implementation status, requirements, roadmap, test state, and AI continuation instructions. Keep the documentation set small: this file is the detailed handoff; the repository README is the short public-facing overview. Do not recreate separate roadmap, status, architecture, support, or installer Markdown documents.

---

## 1. Mission and product direction

Euryphos is an enterprise systems-management platform. It is intended to give authorized operators a detailed, searchable, current view of an organization's managed computers and servers, and to let them perform controlled changes across one device, selected devices, groups, or the wider fleet.

It is not just a patch dashboard or a remote shell. Its long-term responsibilities are:

- Inventory and visibility: hardware, operating system, software, services, network, configuration, security posture, certificates, sessions, and operational health.
- Fleet search: find devices using normalized hardware, OS, software/version, service, directory, configuration, patch, and security facts.
- Fleet operations: create authorized jobs against one endpoint, a static group, a dynamic group, or all eligible endpoints.
- Patch and software lifecycle: discover, approve, stage, deploy, verify, report, and where supported roll back updates and approved software.
- Configuration management: assign versioned desired-state policies, detect drift, remediate, and verify.
- Operational evidence: record who requested an action, which targets were resolved, when each endpoint received and executed it, the outcome, and verification evidence.

The governing product loop is:

**Discover → Normalize → Search → Target → Approve → Dispatch → Execute → Verify → Audit → Report**

A command being accepted, queued, dispatched, received, started, completed, and verified are different states. The UI must not claim success merely because a request was queued or a process returned exit code zero. Remote execution cannot be guaranteed to finish in milliseconds; the goal is immediate server acknowledgement, low-latency dispatch to connected clients, durable delivery to offline clients, and truthful per-device results.

### Product principles

1. Fleet-first: capabilities must work on one endpoint and scale to groups and the estate.
2. Inventory before automation: management decisions use timestamped and queryable facts.
3. Secure by default: administrator, device, command, package, and update identities are authenticated and authorized.
4. Safe operations: large changes require target previews, approval/maintenance controls, concurrency limits, and recovery policies.
5. Honest status: never invent, fabricate, or silently treat missing/stale/unsupported data as current.
6. Resilience: endpoints disconnect, machines sleep, server processes restart, and partial failures are expected.
7. Cross-platform consistency: expose common management concepts while using native platform mechanisms.
8. No unrestricted shell executor by default. Use typed actions with validation, authorization, audit, timeouts, and bounded output.

## 2. Non-negotiable project decisions

These decisions come from the project owner and must not be silently changed:

- **Implementation language:** Go. Do not introduce Python or Rust dependencies, helper services, installer runtimes, or build requirements.
- **Database:** SQLite, using the pure-Go `modernc.org/sqlite` driver. Do not reintroduce PostgreSQL.
- **Repository split:** server and endpoint agent remain separate repositories. The server repository must not contain the agent source or binary; the agent repository must not contain the server, admin UI, migrations, or a SQLite runtime.
- **Server deployment target:** first production deployment is intended for a Debian 13 Proxmox LXC container.
- **Endpoint targets:** Linux distributions in the Arch, Debian, and Fedora families, Windows client/server, and a native macOS client in a later phase.
- **Keep the project compact:** avoid unnecessary files, duplicate documentation, speculative abstractions, and unrelated refactors. Add a new source file only when it creates a useful platform or responsibility boundary.
- **Performance matters:** bound request sizes, work concurrency, memory, database connections, inventory collection, and UI result sets. Do not load the whole fleet or repository into memory/context.
- **Preserve data and identity:** database-generated internal device IDs and the agent-facing UUID must remain distinct. Duplicate merges must preserve history and old agent-token continuity.
- **GPS:** must stay disabled by default and be locally controlled. A server policy must never override a local disabled state. Timezone collection is separate from GPS.
- **Licensing intent:** free individual personal/home-lab use; corporate/business use requires a separate written commercial license only for material the rights holder can license. Previously granted GPL-3.0 and third-party rights remain intact. Do not claim the entire agent repository is exclusively EPUL-licensed before file-level provenance is audited.

## 3. Repository ownership and source map

### Server repository: this project

- `cmd/euryphos-server/main.go` — current Go HTTP API, SQLite migration/queries, device, group, job, audit, and authentication logic. It is currently a large consolidated entry point; do not split it into many packages without a clear testability or ownership benefit.
- `cmd/euryphos-server/ui.go` — Go-served browser console and UI assets.
- `cmd/euryphos-server/main_test.go` — SQLite-backed server integration and regression tests.
- SQLite schema creation and compatibility upgrades are defined in `migrate()` and its helpers in `cmd/euryphos-server/main.go`. This is the single canonical schema source; there is no separate SQL migration file.
- `scripts/install.sh` — root-run Debian 13 Proxmox CT installer/updater; downloads and verifies GitHub release assets, preserves configuration, backs up SQLite, and rolls back a failed upgrade.
- `deploy/euryphos.service` — native systemd unit for the unprivileged `euryphos` account.
- `.github/workflows/release.yml` — validates tagged source and publishes Linux amd64/arm64 release archives with SHA-256 checksums.
- There is no Dockerfile, Docker Compose deployment, standalone SQL migration file, or separate database service.
- `go.mod`, `go.sum` — Go module and dependency lock.
- `README.md` — concise repository landing page.
- `docs/PROJECT_HANDOFF.md` — this detailed context/roadmap file.

### Agent repository: companion project

- Repository: https://github.com/gitarman94/Euryphos-Agent
- `cmd/euryphos-agent/main.go` — agent lifecycle, configuration, enrollment, heartbeat, job polling, result reporting, and current service executor.
- `cmd/euryphos-agent/inventory_common.go` — shared inventory types/helpers.
- `cmd/euryphos-agent/inventory_linux.go` — Linux providers.
- `cmd/euryphos-agent/inventory_windows.go` — Windows providers.
- `cmd/euryphos-agent/inventory_other.go` — unsupported-platform behavior.
- Files named `*_test.go` — agent and provider tests.
- Its only detailed Markdown handoff is `docs/PROJECT_HANDOFF.md`.

The historical `CommandPilot-Agent` repository is not the current maintained agent. Its source, releases, scripts, commit history, and GPL-3.0 provenance must be audited before reusing code. Old scripts are not valid Euryphos installers.

## 4. Architecture baseline

### Server runtime

- Go standard-library HTTP server.
- SQLite embedded in the server process; no separate database server.
- Pure-Go SQLite driver; no CGO requirement from the selected driver.
- HTTP server has explicit read-header, read, write, and idle timeouts.
- Browser console is served by the Go server; no separate frontend runtime is required.
- Agent communication is outbound from endpoints to the server.

### SQLite behavior and constraints

The server configures WAL, foreign keys, a 10-second busy timeout, `synchronous=NORMAL`, a bounded eight-connection pool, and restrictive Unix database/data-directory permissions.

SQLite supports concurrent readers in WAL mode but serializes writes. This is a single-node/single-host design: **do not run multiple server replicas against the same SQLite file**. Do not describe SQLite as universally faster than a server database. Benchmark the real workload and define a supported fleet scale before making performance claims.

Backups must use SQLite's online backup API or a tested quiesced procedure. A live copy of only the main database file may omit WAL data. Production requires restore drills, integrity checks, migration/rollback protection, retention, and recovery procedures.

### Core data model

- `devices`: one row per computer. `database_id` is an internal SQLite-generated integer; `id` is the stable agent-facing UUID used in routes and credentials. Frequently queried scalar identity, OS, directory, hardware, CPU, memory, boot, root storage, and agent facts belong in typed columns.
- `device_inventory`: latest accepted payload for each device/category.
- `device_inventory_history`: change-history snapshots, with server receive and endpoint observation times. It is not a high-frequency metrics time-series table.
- `device_network_interfaces`: normalized repeating NIC/interface records.
- `device_storage_disks`, `device_storage_volumes`: normalized physical disk and volume records.
- `device_services`: normalized installed service records.
- `installed_software`: normalized package/application records.
- `device_duplicate_candidates`: reviewable possible duplicate pairs.
- `machine_groups`, `device_group_memberships`: static/dynamic machine groups and resolved membership.
- `jobs`, `job_targets`, `job_group_targets`: parent jobs, per-device execution state, and immutable group-selection snapshots. Each target stores a unique current `claim_id` plus an incrementing `attempt_count`; this identifies delivery generations and prevents a delayed result from an older claim from overwriting a newer attempt.
- `job_alerts`: durable, claim-specific overdue-heartbeat review records. Each record moves through `open`, `acknowledged`, `recovered`, or `resolved`; a later delivery claim creates a separate alert identity.
- `audit_log`: operational events, including alert creation/recovery/closure and administrator acknowledgement/resolution decisions.
- `admin_sessions`: hashed server-side browser sessions and CSRF material.
- `schema_migrations`: startup schema marker table. The schema is defined only in Go in `migrate()`; there is no standalone SQL migration file or second schema copy. The current startup routine creates missing tables and indexes and additively adds missing columns, with markers through version **14**. This is intentionally an in-code, idempotent schema initializer for the ground-up product. Future upgrades must preserve existing device rows, identifiers, credentials, inventory, groups, memberships, jobs, job outcomes, audit records, and sessions. Do not drop, truncate, recreate, or reset populated tables during an ordinary application upgrade. Any non-additive data transformation requires an explicit tested conversion and a verified backup/restore path before it can ship.

JSON is retained for extensible provider payloads, not as a substitute for typed columns on frequently queried facts. Never create columns from arbitrary client-supplied field names. Timestamps are stored in UTC. Child-table replacement must respect collection status: unsupported, unavailable, failed, or permission-denied collection must not erase last-known good records.

### Device identity and duplicates

Duplicate detection creates candidates; it never merges automatically. Strong match: normalized SMBIOS UUID. Fallback: serial number + manufacturer + model. Reject placeholder values such as unknown/OEM defaults. An administrator reviews candidates and explicitly chooses the canonical record.

A merge transfers relevant history, normalized child rows, memberships, and job associations; fills missing canonical scalar facts where appropriate; leaves a merged alias; and preserves old agent-token continuity by routing authenticated alias requests to the approved canonical device. Any change to merge semantics requires regression tests.

### Groups

Static groups have explicit membership. Dynamic groups use a bounded allowlist of typed fields and operators such as `eq`, `ne`, `contains`, `starts_with`, and `ends_with`, with `all` or `any` match semantics. Compile rules to parameterized SQL, never concatenate user-supplied SQL. Directory facts are normalized across Windows AD and Linux SSSD/realmd where available.

### Communication topology

The current agent checks in over authenticated HTTP(S) requests and polls jobs; it does not yet use a persistent bidirectional message channel or broker. The protocol is an early contract, not a stable public API. TLS must be provided by a trusted deployment edge; clients must validate certificates. Enterprise CA/proxy support, retry/backoff, version negotiation, token rotation, and replay protection remain open.

## 5. Implemented server capabilities (source baseline)

Treat this section as a snapshot, not a substitute for checking current source and tests.

### Enrollment, identity, and API security

- Shared enrollment secret is used to request a new endpoint identity.
- Server creates a device UUID and random per-device bearer token; only a hash is stored.
- New endpoints remain pending until administrator approval.
- Approved-device authentication is checked on heartbeat, inventory, job polling, and result routes.
- Revocation clears the stored token hash.
- Merged aliases can continue through the canonical device identity path.
- Browser login exchanges the configured admin bearer secret for an eight-hour server-side session. Session cookies are HttpOnly and SameSite Strict; state-changing browser requests use CSRF protection. The browser does not persist the admin bearer token.
- Security headers and HTTP server timeouts are configured.
- This is **not** multi-user identity, RBAC, MFA, or SSO.

### Device inventory and management API

Current agent inventory categories are system, network, storage, services, and software. The server normalizes a significant subset into typed device/child records and keeps latest payloads plus change history. It exposes device detail and cursor-paginated inventory history. The Devices API has bounded pagination, basic search, allowlisted sorting, OS/status/directory/connection filters, and server-derived connectivity state.

Current server-derived presence values include online, offline, never-connected, and unknown. The default online grace period is 90 seconds and is configurable with `EURYPHOS_ONLINE_GRACE_SECONDS` (15–3600 seconds). First/last connection timestamps are server-observed; connecting IP through trusted proxies is not complete.

### Audit

Initial audit coverage includes administrator login success/failure, enrollment, approval/revocation, group mutation, job creation/results, duplicate merges, and the job-alert lifecycle (`job.alert.overdue`, `job.alert.acknowledged`, `job.alert.recovered`, `job.alert.closed`, `job.target.marked_indeterminate`, `job.target.retry_authorized`, and `job.target.retry_rejected` (denied non-idempotent retry attempts)). This is an operational audit foundation only. It is not immutable, tamper-evident, retention-governed, or compliance-complete.

### Groups and fleet targeting

The server can create/edit/delete static or dynamic groups, manage static membership, reevaluate dynamic rules, and target approved devices and/or groups with jobs. Group membership is expanded transactionally at job creation; overlapping targets are deduplicated; selected group ID/name snapshots remain in job history even if the live group changes or is deleted.

### Jobs and service control

Job action strings currently accepted by the server include:

- `inventory.refresh`
- `patch.scan`
- `patch.install`
- `software.install`
- `software.uninstall`
- `configuration.apply`
- `service.control`
- `device.reboot`
- `device.shutdown`
- `agent.update`

The allowlist is not proof that an agent executor exists. Server-side validation for `service.control` accepts only a bounded service name and start/stop/restart/enable/disable operations; reserved command name `systemctl` is rejected. The console has initial Start/Stop and Enable/Disable controls, with confirmation for disruptive operations. The agent implements an initial Linux/systemd and Windows service executor. Current agent validation is recorded in Section 7; its builds are cross-build evidence, not runtime certification.

### Current job delivery behavior and known limitation

The server atomically marks a queued device target as `received` when the agent polls. The response includes a random per-delivery `claim_id` and a per-target `attempt` counter. A new claim ID is generated every time a queued target is claimed; the attempt counter increments. A **10-minute receive lease** returns an unacknowledged `received` target to `queued`, clears its previous claim ID, records `job.target.requeued`, and permits a new claim. Recovery is indexed by device/status/time. A job already in `running` may still dispatch its other queued device targets.

The agent includes the returned `claim_id` in every result update. The server rejects results from stale claims with HTTP 409, so a delayed result from an expired/reclaimed delivery cannot overwrite a newer attempt. Result processing accepts only an active `received`/`running` target. The agent persists its job locally and sends an idempotent `started` acknowledgement for the active claim **before** running the action; this moves the target from `received` to `running` and removes it from the 10-minute receive-lease reclamation path. Repeating `started` with the same active claim renews `job_targets.updated_at` and returns `heartbeat_recorded` without creating another audit event. A repeated terminal result with the same claim and same terminal status is acknowledged as `already_recorded`; a conflicting terminal status is rejected. A delayed result from a superseded claim remains rejected with HTTP 409.

The agent persists `pending_job` in its existing configuration file with phases `received`, `starting`, `executing`, and `reporting`. It retries start/result messages with the same claim and does not replay an unfinished action after restart. During execution it sends device heartbeats and same-claim `started` heartbeats approximately every 30 seconds. The agent has a bounded job deadline: default four hours, configurable by `job_timeout_seconds` between 60 seconds and 24 hours. A deadline reached before a confirmed result is reported as `agent_interrupted_outcome_unknown`, preserved in the journal, and retried for server acknowledgement on the next cycle.

The administrator API now exposes the most recent target activity timestamp as `updated_at` in each job detail execution and `last_updated_at` in the paginated job list. The browser Jobs list and per-device job detail display these values, allowing an operator to see the last server-confirmed heartbeat/result time.

**Stale-heartbeat visibility:** `EURYPHOS_JOB_STALE_SECONDS` defaults to 180 seconds and accepts 60–86400. The Jobs API returns `stale_running` and `last_updated_at`; job detail returns `heartbeat_state=current|overdue|not_applicable` and the target `updated_at`. An overdue heartbeat means "needs operator review", not proof that the action stopped. This derived state never terminates a process, cancels work, or automatically requeues a running target.

**Administrator overdue-job workflow (schema version 14):** `job_alerts` stores one durable alert per `(job_id, device_id, claim_id)`. The authenticated `GET /api/v1/job-alerts` endpoint materializes up to 500 newly detected overdue alerts per request, reconciles active alert records in batches of 500, and returns a paginated queue with open/acknowledged counts and the retry classification. The console's Jobs navigation displays an active-alert badge and the Jobs view has an overdue review queue. The console refreshes the badge periodically; the queue is also loaded when Jobs is opened. This is an in-console, polling-based notification only: there is no email, push, webhook, or independent background alert scheduler.

An administrator can acknowledge an alert with a required reason. Acknowledgement records review and audit evidence but deliberately leaves the target's execution status as `running`. When a same-claim heartbeat resumes, the alert is marked `recovered`; when the claim or target state changes, the alert is closed. Each lifecycle transition is recorded in `audit_log`.

Resolution requires the alert ID, the exact expected claim ID, an explicit confirmation flag, and a reason. `mark_indeterminate` changes the target to `indeterminate` and records an audit event. It does **not** stop the endpoint process or prove whether the action took effect. A late result from that claim is rejected with HTTP 409. After the agent receives that conflict, it queries the device-authenticated `GET /api/v1/devices/{id}/jobs/{job}/claims/{claim}` endpoint. The server returns `journal_action=retire` only when that exact claim has an explicit administrator resolution (`mark_indeterminate` or `retry`); the agent then persists retirement of only that local journal and never replays the action. Without this server authorization, the local journal remains intact.

Manual retry is limited to `inventory.refresh` and `service.control` operations `start`, `stop`, `enable`, and `disable`. It additionally requires an explicit administrator confirmation that the previous execution has stopped and a recorded reason. `service.control restart`, patch/software installation or removal, configuration changes, reboot/shutdown, agent update, and every unclassified action are not retryable. Retry clears the old claim and queues a fresh attempt; the next claim receives a new ID and increments the attempt number. Delayed results from the old claim remain rejected. The agent uses the claim-resolution endpoint after the expected HTTP 409 and retires the superseded journal only after the server confirms this explicit retry decision. A receive-lease reclaim without an administrator alert returns `retire_if_not_started`, which the agent honors only while its local phase is `received` or `starting`; an executing/reporting journal cannot use that response. This classification is intentionally conservative and must be expanded only with action-specific idempotency evidence.

The job lifecycle is still **not** exactly-once execution or a complete retry system. The server cannot prove whether an endpoint that lost connectivity completed a side effect before the last heartbeat. Explicit operator acknowledgement is not execution recovery. The 10-minute timeout applies only to unacknowledged `received` claims, not long-running `running` actions.

### Browser console

The initial console is served at `/login` and `/console`. Current views include fleet overview, Devices, device detail, duplicate review/merge, Groups, Jobs/per-device outcomes, and Audit. Device detail includes system/category payloads and normalized network interfaces, disks, volumes, and services. The UI is an initial operator foundation, not a completed enterprise UX.

## 6. Current server API surface

Endpoint/device-authenticated routes:

- `POST /api/v1/enrollment` — enrollment-token bearer.
- `POST /api/v1/devices/{id}/heartbeat` — approved device bearer.
- `POST /api/v1/devices/{id}/inventory` — approved device bearer.
- `GET /api/v1/devices/{id}/jobs/next` — atomically claim one queued device target; a returned job includes `claim_id` and incrementing `attempt`.
- `POST /api/v1/devices/{id}/jobs/{job}/result` — report started/succeeded/failed/cancelled outcome and the required `claim_id`; stale claims are rejected and duplicate matching terminal results are idempotently acknowledged.

Administrator-authenticated routes:

- `POST /api/v1/devices/{id}/approve`, `POST /api/v1/devices/{id}/revoke`.
- `GET /api/v1/devices`, `GET /api/v1/devices/{id}`, `GET /api/v1/devices/{id}/inventory/history`.
- `GET /api/v1/devices/{id}/duplicates`, `POST /api/v1/devices/{id}/merge`.
- `GET /api/v1/jobs` — paginated jobs with target counts, `last_updated_at`, calculated `stale_running`, and indeterminate counts; `POST /api/v1/jobs`; `GET /api/v1/jobs/{id}` — per-target `updated_at` and `heartbeat_state` (`current`, `overdue`, or `not_applicable`) alongside real execution status/error.
- `GET /api/v1/job-alerts` — paginated overdue review queue; `POST /api/v1/job-alerts/{id}/acknowledge` — record administrator review; `POST /api/v1/job-alerts/{id}/resolve` — mark the outcome indeterminate or request a permitted manual retry.
- `GET /api/v1/devices/{id}/jobs/{job}/claims/{claim}` — device-authenticated claim resolution query. Returns `journal_action=retire` only after an explicit administrator resolution, or `retire_if_not_started` when the server confirms a received claim was superseded before execution. It never tells an agent to replay work.
- `GET /api/v1/groups`, `POST /api/v1/groups`, `PUT /api/v1/groups/{id}`, `DELETE /api/v1/groups/{id}`.
- `GET /api/v1/groups/{id}/members`, `POST /api/v1/groups/{id}/members`, `DELETE /api/v1/groups/{id}/members/{deviceID}`.
- `POST /api/v1/auth/session`, `POST /api/v1/auth/logout`, `GET /api/v1/audit`.

Also `GET /api/health`, `GET /login`, `GET /console`, and Go-served assets.

Keep `Euryphos-Agent/docs/PROJECT_HANDOFF.md` synchronized with actual handler behavior if a protocol contract changes. Do not create a separate API Markdown file; this route summary and the agent handoff are the documentation source.

## 7. Validation status — do not overstate it

### Server CI

The following server CI runs completed successfully on 2026-11-25. Each passed `go mod tidy`, `go test ./...`, `go vet ./...`, `bash -n scripts/install.sh`, and `go build -trimpath ./cmd/euryphos-server`:

- **[45631229526](https://github.com/gitarman94/Euryphos/actions/runs/45631229526)** — stale-running heartbeat classification and API output on commit `752bb1e08643686d197ab62a3fdd40a1c19027d6`.
- **[45631313260](https://github.com/gitarman94/Euryphos/actions/runs/45631313260)** — regression test proving overdue is distinct from execution status and does not mutate a running target, commit `9eb2f91de43099c75a6425fee0571f16ad3969ac`.
- **[45631318715](https://github.com/gitarman94/Euryphos/actions/runs/45631318715)** — console overdue heartbeat display, commit `0a9d32fb40a9ac13cba06214c8bbd441b7febc0a`.
- **[45631324054](https://github.com/gitarman94/Euryphos/actions/runs/45631324054)** — Debian installer default for `EURYPHOS_JOB_STALE_SECONDS=180`, commit `4caea1f413ca9e88c59030c9d551bd9cd2e689f4`.

The API activity timestamp and earlier handoff synchronization runs **[45630523155](https://github.com/gitarman94/Euryphos/actions/runs/45630523155)** and **[45631440620](https://github.com/gitarman94/Euryphos/actions/runs/45631440620)** also passed.

The source-build test installer change on commit `c53a6540eeb6abb9fbb766180938f35e5bcb0347` passed [run 45644152313](https://github.com/gitarman94/Euryphos/actions/runs/45644152313), including module normalization, tests, vet, installer shell syntax validation, and server build. The following README update passed [run 45644160200](https://github.com/gitarman94/Euryphos/actions/runs/45644160200).

The systemd unit-state fix on commit `83d38ff492c85d77d4ed611358c765168c2c0b9c` passed [run 45650543257](https://github.com/gitarman94/Euryphos/actions/runs/45650543257). It passed module normalization, tests, vet, installer shell syntax validation, and server build. The installer now runs `systemctl daemon-reload`, checks `LoadState` and `ActiveState`, stops only an active loaded unit, and preserves the pre-update active/inactive state if rollback is needed.

These checks do not by themselves qualify a live server/agent deployment. A fresh Debian 13 Proxmox CT installation initially failed with systemd `status=203/EXEC`, `Permission denied`. Diagnosis confirmed the release version directory was mode 0700 because the installer sets `umask 077` before using `mkdir -m 0755`; the unprivileged `euryphos` service account could not traverse it. The binary itself was mode 0755 and `/opt` was on the root ext4 mount without `noexec`. Installer revision `2026-11-25.4` fixed release-directory traversal permissions, and a subsequent test installation completed successfully: the service reached active/running state, `NRestarts=0`, and `GET /api/health` returned `status=ok`. Revision `2026-11-25.5` adds the installer terminal UI and resource pre-flight; its shell syntax and live CT behavior still require validation after that UI change.

### Agent CI

The current agent main commit `4963c7a96cb79e986de2acf20f5edce641a0db41` passed [GitHub Actions run 45638390318](https://github.com/gitarman94/Euryphos-Agent/actions/runs/45638390318) on 2026-11-25. The validate job passed `go test ./...`, `go vet ./...`, Linux build, Windows amd64 cross-build, and macOS arm64 cross-build, including the claim-resolution handshake and regression tests. Later documentation-only commits `731ee3a2ce95fdc65dfa81e6fedf7b24730954c2` and `8b1aa6d8ea89aeb659733baba6c5968525c37869` also passed runs `45643719942` and `45643725887`. Cross-builds do not certify runtime support on those systems.

### Overdue-job workflow CI validation

The overdue-job workflow, administrator resolution paths, restricted retry logic, claim-resolution handshake, audit behavior, and associated server regression tests are on the server `main` branch. The source/test commits include:

- `620281bc78477e342b96bbee7c5fcec1cb1e3bbe` — durable alert table, queue API, acknowledgement, indeterminate resolution, and restricted retry.
- `4b7cf2370612777e0321cdcac2afae12ceeb752e` — queue, audit, recovery, late-result, retry-confirmation, and retry-allowlist tests.
- `dfb14bbb7547dcc92482af9295c0d5acd611f018`, `bbc026463d73f50a0ac4d6da09811838b89d1a8b`, `5129dd8c667953fc13c84d626935d3f73222c068` — administrator console queue, pagination, and embedded JavaScript string fix.
- `e2621396c22b3b4d8e927348c6cac6a7d0318f98` — bounded alert reconciliation batches and throttled last-seen updates.
- `bffde59e20b7264b6ab13b7a44e40a6f4e0ab114` — device-authenticated claim-resolution response for agent journal retirement.
- `993af998837690ee87a36161422f1cc81b344ef6`, `2754ab7ba856a7b2e15823090fd5776e088b71d2` — regression coverage for administrator-authorized journal retirement and receive-lease reclaim.
- `22f6d0593e1944f04cff8a38d8ff0e9c19ea434f`, `dc9217d41bfee8a1d41b97c03cabb660b6213109` — audit and tests for denied non-idempotent retry attempts.

The corresponding agent commits are `f8122ac58801f8e286ac7e30c2a1989e4d2aa5b3`, `f786a4faad149f0a6f3ae4d04f1f20fb0f8d6898`, `602a41f5c76c7bac95dc2c06aaea22b59249a2bc`, and `cebed6eb687530ada40c4c5385bd02e20bafcacc`.

**Current verified server result:** [GitHub Actions run 45641090250](https://github.com/gitarman94/Euryphos/actions/runs/45641090250) completed successfully on 2026-11-25 against current main commit `dfca0912ab8dfdf10df092cb858e7ff76254c3b5`. The `validate` job passed module normalization, `go test ./...`, `go vet ./...`, installer shell syntax validation, and server build. Earlier source validation [run 45638559694](https://github.com/gitarman94/Euryphos/actions/runs/45638559694) also passed on the denied-retry test commit.

These are CI results, not evidence of a live Debian 13 installation or a real endpoint operation.
### What has not been demonstrated

No real running server + installed agent end-to-end test has been completed. No actual Linux or Windows service operation has been runtime-tested. Cross-compilation does not certify runtime compatibility. No first tagged GitHub server release has been published, and no fresh install, in-place upgrade, failed-upgrade rollback, reboot, or database restore has been tested in a Debian 13 Proxmox CT. Cryptographic release signing/provenance, fleet load benchmarks, penetration testing, and supported-OS certification remain incomplete.

When writing status, distinguish:
- source code exists;
- unit/integration tests pass;
- CI passes;
- cross-build passes;
- runtime test passes on a named OS;
- end-to-end server/agent test passes;
- production/release qualification passes.

These are separate evidence levels.

## 8. Target product requirements to preserve

### Fleet/device record

One computer per device row. The Devices view must be server-paginated and searchable. Required default columns: hostname, local IP, server-observed connecting IP, OS/version, agent version, connectivity, last connected, and last boot. Administrative state (pending/approved/revoked) is separate from connection state (online/offline/never connected).

Device detail should group and expose:

- Identity/directory: hostname, FQDN, domain/join state, OU, site, device ID, serial, SMBIOS UUID, device/chassis type, current user and active sessions when permitted.
- Location/time: timezone and offset; optional GPS only under strict local opt-in; assigned site/building/room is distinct from sensor-derived location.
- Hardware: manufacturer/model, CPU, cores/threads, memory/modules, GPU, DPU as a separate category, BIOS/UEFI, boot mode, battery, disks/volumes, encryption and health where available.
- OS: family, distribution/product, edition, normalized/human version, build/revision, kernel, architecture, reliable install date, last boot/uptime, support lifecycle where a source exists.
- Network: multiple NICs and addresses, IPv4/IPv6 prefixes, MAC, link type/state/speed, DHCP server and leases, gateways, DNS/search domains, optional Wi-Fi SSID only with a separate local privacy policy, connectivity events, bytes/latency where measured.
- Agent/security: agent version/build/channel/service status, update time, certificate public metadata only once certificate identity exists; never display or upload private keys or bearer secrets.
- Activity: job history, agent events, reconnects, failure categories, and inventory freshness.

Every fact should carry observation/receipt time, source/provider, schema version, and collection status. Distinguish unsupported, unavailable, failed, permission denied, stale, not reported, and privacy-disabled. Never replace unknown values with zeroes or fabricated values.

### Dashboard, search, and charts

Fleet overview should include total/approved/pending/revoked, online/offline, OS/version, hardware/software summaries, patch/compliance, vulnerable/unsupported devices, agent version drift, job activity/failures, configuration drift, and inventory freshness. Every count drills down to real matching devices.

Search/filter should eventually include hostname/FQDN/domain, serial/asset tag/UUID, local and connecting IP, MAC, OS/build, hardware, software name/publisher/version, services/processes/configuration, patch/CVE state, agent version, groups/tags/site/owner/environment, compliance, and activity. Always bound pagination server-side; never load an entire fleet into browser memory.

Time-series charts must use real persisted samples and show units, source, interval, time range, and gaps. Missing data is not zero. Required future trends include CPU, memory, per-volume storage, network throughput/latency, battery, connectivity, job/patch activity, inventory changes, fleet enrollment/online/agent version, and outcomes. This requires a separate bounded metrics model; current inventory history is not a metrics store.

Comparison view: compare a bounded number of devices side by side, allow an explicitly selected baseline, highlight differences, and preserve unavailable/stale/unsupported states. GPS must be excluded from default compare/export and require explicit sensitive-data permission plus confirmation.

### Inventory completeness

Beyond the current initial categories, the target catalog includes:

- SMART/disk health, RAID/device-mapper/controllers, encryption, GPU/DPU, memory modules, firmware and Secure Boot/TPM.
- Broader Linux network manager/DHCP providers (NetworkManager, systemd-networkd, dhclient and derivatives), Wi-Fi, traffic/latency.
- Certificates/thumbprints/expiry (public metadata only), local users, interactive sessions, processes only under explicit policy, scheduled tasks/cron/systemd timers, drivers/modules, listening ports where enabled.
- Firewall/encryption/AV/EDR/security posture and selected configuration values.
- Software metadata beyond package names/versions, machine/user install scope, app bundles and trusted source where available.
- Category freshness, collection errors, source confidence, full/delta collection and retention.
- Native macOS providers in a later phase.

Sensitive process command lines, user/session data, GPS, network identifiers, and unique hardware identifiers require explicit access control, minimization, read/export auditing, retention, and redaction. Never collect passwords, tokens, private keys, or unrelated personal files.

### GPS privacy acceptance

Fresh install/migration defaults to disabled. When false, the agent must not call location APIs, retain coordinates, or upload GPS data. Server policies cannot enable it. Explicit enablement requires a local administrator action and OS permission. No continuous tracking; only configured inventory cadence. Upgrades, repair, reset, and reconnect preserve the disabled state. Timezone is collected separately.

## 9. Prioritized remaining work

Use this order unless fresh test evidence reveals a blocking issue. Do not jump to advanced inventory or UI work while the execution path is unreliable.

### P0 — CI and source validation completed

The current server main branch has passing CI for tests, vet, installer shell syntax, and server build. The current agent main branch has passing tests, vet, Linux build, and Windows/macOS cross-builds. Exact run IDs and commit SHAs are recorded in Section 7. This does not certify a running server/agent deployment on any endpoint OS.

### P1 — Run the complete server-agent workflow on real Linux and Windows machines

This is the next engineering priority after the alert workflow is verified in CI. Use disposable VMs and a test server/database. For each OS, prove:

- Enrollment, approval, device heartbeat, and all five current inventory categories.
- A safe typed action reaches the endpoint, is started, executes, and reports its real result.
- Long-running heartbeat renewal; endpoint/network interruption; agent restart with each persisted journal phase; server restart; lost result response and retry; late/stale claim rejection.
- Overdue alert detection, acknowledgement, heartbeat recovery, indeterminate resolution, and a manual retry only after independently verifying that the prior process stopped.
- No duplicate action execution when a result response is lost or the endpoint restarts. Capture logs and audit rows for each test.

Record exact OS edition/version, agent/server commits, test sequence, and evidence. Cross-builds and unit tests are not substitutes for runtime tests.

### P2 — Complete job lifecycle safety before patch deployment

- Explicit expiry/deadline and cancellation rules; documented state transitions and operator expectations.
- Bounded retry/backoff/jitter and action-specific idempotency classification beyond the narrow current manual retry allowlist.
- Protocol compatibility/version negotiation and recovery tests for concurrent fleet jobs.
- Operational handling for unreachable endpoints, stale local journals, and administrator decisions without silently discarding uncertain outcomes.
- Keep audit evidence for each delivery attempt, recovery, cancellation, manual retry, and final result.
- Avoid whole-fleet scans; retain indexes and bounded query/processing batches.

Do not claim exactly-once delivery. Design honestly around at-least-once delivery with idempotency/deduplication.

### P2 — Real end-to-end vertical slice and security

Before implementing mass patch execution, prove one real Linux endpoint and one Windows endpoint can:

1. Enroll and receive its device token.
2. Wait for administrator approval.
3. Heartbeat and upload all five inventory categories.
4. Show normalized device fields, disks, volumes, services, and installed software in SQLite and the console.
5. Receive a typed job, execute a safe test action, and report success/failure.
6. Lose connectivity/restart and recover without losing or silently duplicating state.
7. Be revoked; old credentials must fail.
8. Preserve duplicate merge continuity where applicable.

Add protocol schema/version negotiation and compatibility tests. Then address RBAC/multi-admin, MFA/SSO, device certificate issuance/rotation/revocation, signed commands/artifacts, action approvals, privilege separation, least-privilege identities, proxy/custom-CA configuration, request replay resistance, secret rotation, sensitive inventory permissions, and tamper-evident audit/retention. Current admin bearer plus browser session is not enterprise RBAC.

### P3 — Patch discovery, then deployment

Implement **read-only discovery first** and verify real results before any installation action.

Linux discovery providers:
- Debian/Ubuntu/Mint/LMDE: dpkg/apt package and update metadata.
- Arch/CachyOS: pacman metadata, rolling-release synchronization, mirror/keyring/conflict conditions.
- Fedora/Nobara: RPM plus dnf/dnf5 update metadata.
- Bazzite/Atomic: rpm-ostree/image deployments, pending deployment, reboot and rollback status. Never treat it as mutable Fedora with ordinary host dnf.

Windows: native Windows Update provider, applicable updates, classifications/severity, KB/CVE metadata where available, prerequisites, reboot requirement, install state and errors.

Store updates per device with source, version, applicability, severity, timestamps, reboot requirement, and status. Test stale metadata, provider unavailable, offline, and permission cases. Then add selected update installation, verification, reboot policies, campaign targeting, approvals, rings (pilot → early → broad), maintenance/blackout windows, concurrency/rate limits, retry/exclusion, rollback/compensating actions, emergency/zero-day workflows, and post-action fleet re-query. Never mark a patch successful until post-state verifies it.

### P4 — Software, configuration, and endpoint actions

- Approved software catalog and package metadata; install/uninstall/upgrade with signature/hash/provenance verification.
- Desired-state configuration policies with versioning, assignments, precedence/conflict handling, drift detection, remediation and verification.
- Typed catalog for service actions (initial executor exists but remains CI/runtime gated), reboot/shutdown, diagnostics, and later approved automation packages.
- Do not add arbitrary shell as an unrestricted job type.

### P5 — Inventory, console, and reporting

- Complete missing inventory domains listed in Section 8, prioritizing high-value fleet queries and safe provider behavior.
- Finish Devices columns/search/filter/sort, detail sections, freshness/error states, bulk-target preview, visual dynamic-rule builder, comparison/baseline, live job console, and audit/activity.
- Add actual metrics sampling and bounded time-series retention before charts.
- Add reports, sensitive exports with RBAC/audit, compliance evidence, policy/drift reports.
- Keep all counts and views backed by real server queries.

### P6 — Installers, releases, and operations

**Server first target — implementation started:** the repository now contains a Debian 13-only Proxmox CT installer/updater at `scripts/install.sh`, a systemd unit at `deploy/euryphos.service`, and a tag-triggered release workflow at `.github/workflows/release.yml`. GitHub releases contain static Go server archives for Linux amd64/arm64 and a SHA-256 manifest. Installation creates the `euryphos` unprivileged account, keeps configuration at `/etc/euryphos/euryphos.env`, stores SQLite at `/var/lib/euryphos/euryphos.db`, and runs directly under systemd. Upgrade stages a versioned binary, stops the service, creates and integrity-checks a SQLite backup, atomically switches the binary, and health-checks startup. A failed upgrade restores the previous binary and the pre-upgrade database. The server now handles SIGTERM/SIGINT with graceful HTTP shutdown and closes SQLite before systemd completes stopping it.

**Still required before calling this deployment production-ready:** publish the first tagged release and execute the tag-triggered release workflow (including archive and checksum verification); complete repeat install, in-place upgrade with populated device/inventory/group/job/audit data, a deliberately failed upgrade and rollback, reboot/startup, manual backup/restore, and disk-full/backup-failure cases in disposable Debian 13 Proxmox CTs. One initial source-build installation has passed the local health check after the release-directory permission correction. The newer `2026-11-25.5` terminal UI and resource pre-flight have not yet been exercised in the CT. Confirm credentials and device tokens remain unchanged. Add cryptographic release signing/provenance verification; the installer currently validates SHA-256 checksums against the release manifest but does not establish a separate signing trust root. Do not add Docker or Docker Compose deployment files.

**Linux agent:** signed immutable Go releases and checksum manifest; client-only installers/service lifecycle for Arch/CachyOS, Debian/Ubuntu/Mint/LMDE, Fedora/Nobara, and a separate image-aware Bazzite/rpm-ostree approach. Preserve config/credentials on upgrade; support repair/uninstall/rollback.

**Windows agent:** signed MSI or chosen installer, native service, restrictive ACL/DPAPI secret handling, install/repair/upgrade/uninstall/rollback, reboot recovery and EDR/AV compatibility.

**macOS later:** Intel/Apple Silicon binaries, launchd, Keychain, signed/notarized PKG, MDM deployment, native inventory/update.

### P7 — Scale and enterprise release gate

- Benchmark SQLite concurrent heartbeat/inventory writes, job dispatch, group expansion, device search, history queries, and UI workloads with representative fleet sizes.
- Define target fleet size, request throughput, p50/p95/p99 latency, queue latency, inventory freshness, and storage growth before claiming scale.
- Define retention, pruning, online backup, restore objectives, integrity checks, migration backup/rollback, WAL management, disk-full behavior, and disaster recovery.
- Add server observability: structured logs, metrics, health, queue lag, database health, agent connectivity and job failure rates.
- Penetration test, dependency/license review, threat model review, release signing, reproducible build/provenance, support matrix and runbooks.
- No operating system is certified simply because a binary cross-compiles.

## 10. Platform support target matrix

All platforms remain **planned/not certified** until installation, service lifecycle, inventory, update behavior, communication, recovery, and security acceptance tests pass on each named release/architecture.

| Family | Target | Native management direction |
|---|---|---|
| Arch | Arch Linux, CachyOS; later EndeavourOS/Manjaro | pacman; rolling-release safeguards and derivative validation |
| Debian | Debian 13 and supported stable releases | dpkg/apt |
| Ubuntu | Current and previous supported LTS | dpkg/apt |
| Mint | Ubuntu-based Linux Mint and LMDE as separate tests | dpkg/apt; LMDE uses Debian base |
| Fedora | Fedora Workstation/Server | RPM, dnf/dnf5 |
| Fedora derivative | Nobara | RPM, dnf/dnf5 plus derivative-specific differences |
| Atomic | Bazzite; later Silverblue/Kinoite | rpm-ostree/image deployment, reboot, rollback; no ordinary mutable-DNF assumptions |
| Windows | Workstations and Server; version floor to be chosen | native CIM/WMI, service APIs, Windows Update, signed MSI |
| macOS later | Intel and Apple Silicon | launchd, Keychain, Apple APIs, signed/notarized PKG |

Each OS/release/architecture is independently qualified. Shared package-manager ancestry does not automatically certify a derivative.

## 11. Server deployment configuration reference

### Debian 13 Proxmox CT layout

- Binary releases: `/opt/euryphos/releases/<version>-<checksum>/euryphos-server`.
- Active binary: `/opt/euryphos/current/euryphos-server` (atomic symlink).
- systemd unit: `/etc/systemd/system/euryphos.service`; use systemd drop-ins for local overrides rather than editing the release-managed unit.
- Root-only configuration: `/etc/euryphos/euryphos.env` (mode 0600). Upgrades must never regenerate or overwrite this file.
- Persistent state: `/var/lib/euryphos/euryphos.db`; SQLite WAL sidecars live beside it.
- Pre-upgrade backups: `/var/lib/euryphos/backups/`, root-only and integrity-checked.
- Service identity: unprivileged `euryphos:euryphos`.
- Logs: systemd journal; inspect with `journalctl -u euryphos`.
- No Docker, Compose, PostgreSQL, or separate database host.

### Install and upgrade

The server repository is public. No GitHub token is needed to retrieve the installer or public source. The separate `Euryphos-Agent` repository is currently private and has no supported endpoint installer/release; endpoint installation remains manual source-build and integration testing work.

**Current test install (run as root inside a Debian 13 systemd CT):**

```bash
apt-get update && apt-get install -y ca-certificates curl && f="$(mktemp)" && curl --retry 3 --retry-all-errors -fsSL "https://raw.githubusercontent.com/gitarman94/Euryphos/main/scripts/install.sh" -o "$f" && grep -Fq 'INSTALLER_REVISION="2026-11-25.5"' "$f" && bash -n "$f" && EURYPHOS_SOURCE_BUILD=1 bash "$f" install; result=$?; rm -f "${f:-}"; exit "$result"
```

With `EURYPHOS_SOURCE_BUILD=1`, the installer installs Debian's `golang-go` package, resolves and pins the current public `main` commit by SHA, downloads that source archive, builds the server for the CT's Linux architecture, and packages the binary, systemd unit, and license locally. It then applies the same SHA-256 package check, atomic versioned installation, secret/config preservation, SQLite backup, service startup, and `/api/health` check as the release path. The Go build toolchain remains installed in the CT. This path is for pre-release testing; it is not an immutable prebuilt release.

The Bash terminal UI runs before apt installs or Go compilation. It checks effective CPU capacity (using `nproc` and cgroup CPU quota when exposed), container RAM (cgroup v2/v1 limit, falling back to `/proc/meminfo` while explicitly warning when the LXC limit is not visible), and total/free bytes on the filesystem containing `/var/lib/euryphos`. It prints `[OK]` and `[WARN]` findings and, when attached to a terminal, asks before continuing with warnings. No Proxmox resource setting is changed automatically. Current initial sizing guidance is 2 vCPU and 2 GiB RAM as a baseline, 4 GiB RAM recommended; disk baseline 10 GiB total/5 GiB free, recommended 20 GiB total/10 GiB free. These are planning recommendations, not a fleet-capacity guarantee.

After a successful health check, the UI prints CT IP address login URLs, systemd status/log commands, and the path to the root-only token file. The default `EURYPHOS_COOKIE_SECURE=true` means authenticated browser sessions require HTTPS even though the login page can be viewed over the direct HTTP listener. Configure a trusted HTTPS reverse proxy and access `https://<your-hostname>/login`; the installer does not configure DNS, TLS, firewall rules, or the proxy.

The normal installer path (without `EURYPHOS_SOURCE_BUILD=1`) downloads the latest published GitHub release, requires the matching Linux amd64/arm64 archive and `SHA256SUMS`, and verifies the release checksum before changing the active installation. **No first versioned release exists yet.** The release workflow builds those assets after a `v*` tag is pushed. After a release is published, the standard command can omit the source-build variable.

For an upgrade, rerun the same source-build command with final argument `upgrade`; this builds the current `main` commit. For a pinned released version, set `EURYPHOS_VERSION=vX.Y.Z` and omit the source-build variable.

The installer requires root, Debian 13, and systemd. It installs `curl`, `jq`, `sqlite3`, `openssl`, CA certificates, `su` (for service-identity access checks; supplied by Debian's base util-linux package), and (only in source-build mode) `golang-go`. Before stopping anything, it reloads systemd's unit catalogue and checks the service's actual load/active states; an absent, unloaded, inactive, failed, or dead unit is not stopped. After staging, it explicitly sets the version directory and binary permissions and verifies that the `euryphos` account can read and execute the binary before changing the active symlink. This compensates for the installer's restrictive `umask 077`, which otherwise can create a 0700 directory even when `mkdir -m 0755` is requested. Startup uses a non-blocking systemd start, a visible 120-second health-check countdown, and service journal diagnostics on failure. Rollback restarts the previous server only when it was running before the attempted update. The application itself remains one Go server process with embedded SQLite; no database daemon, Docker, or Compose is installed.

### Non-data-destructive upgrade contract

1. Download and checksum-verify the release before stopping the active service.
2. Stop systemd service and wait for process exit; the server handles SIGTERM/SIGINT, finishes active HTTP requests within its graceful timeout, and closes SQLite.
3. Use the SQLite backup API via `sqlite3 .backup` to create a consistent pre-upgrade snapshot; validate it with `PRAGMA integrity_check`.
4. Never overwrite the live DB, config file, generated tokens, or agent device credentials as part of binary installation.
5. Install the release into a new immutable version directory and switch `current` using an atomic symlink rename.
6. Start the service and verify `GET /api/health` locally.
7. If startup/health fails, stop the new process, restore the prior symlink and service unit, remove stale WAL/SHM sidecars, restore the SQLite backup, and restart the old version.
8. Keep the pre-upgrade backup for manual recovery even after a successful upgrade.

A version's Go schema initializer may only make compatible additive changes automatically. Never use an application upgrade to wipe/reseed populated data. Before shipping any schema change, test it against a copy of a populated prior-version DB and prove devices, credentials, history, groups, jobs, audit, and sessions are preserved.

### Environment configuration

- `EURYPHOS_LISTEN` — HTTP listen address; default `:8080`.
- `EURYPHOS_DB_PATH` — SQLite file; installer default `/var/lib/euryphos/euryphos.db`.
- `EURYPHOS_ADMIN_TOKEN` — generated first-install admin bearer secret; never rotate automatically.
- `EURYPHOS_ENROLLMENT_TOKEN` — generated first-install enrollment secret; never rotate automatically.
- `EURYPHOS_AUTO_MIGRATE` — Go schema initialization/compatibility upgrade toggle; default true.
- `EURYPHOS_ONLINE_GRACE_SECONDS` — server-derived device online grace, default 90, allowed 15–3600 seconds.
- `EURYPHOS_JOB_STALE_SECONDS` — running-job heartbeat visibility grace, default 180, allowed 60–86400 seconds. Overdue is observational only and never causes automatic requeue.
- `EURYPHOS_COOKIE_SECURE` — secure cookie behavior; installed default true.

Never expose unencrypted HTTP directly to untrusted networks. Terminate TLS at a correctly configured trusted proxy. Forwarded client-IP headers must be honored only from explicitly configured trusted proxies.

## 12. AI continuation and change-control instructions

When continuing this work after chat history loss:

1. Read this document first. Then inspect the current GitHub main heads and Actions status; this handoff may lag commits.
2. Read only the source files and tests relevant to the next task. Do not dump the whole repository or load every documentation/source file into context.
3. Treat source and executable tests as truth. This handoff describes intended/current state but may need correction after inspection.
4. Continue the highest-priority incomplete task in Section 9. Prefer small vertical slices with API, persistence, agent behavior, failure handling, tests, and docs together.
5. Preserve the Go-only, SQLite, two-repository split, compact-file, and performance constraints.
6. Do not implement patch installation before read-only discovery and reliable job delivery. Do not implement a broad action before proving its authorization, validation, executor, result reporting, audit, timeout, and recovery.
7. Never claim completion from a placeholder, route, UI button, cross-build, or an old green CI run. Record exact commit, run, steps, and platform evidence.
8. After verified changes, update this file's implementation snapshot, CI evidence, and next priority. Keep README concise and public-facing. Do not create separate status/roadmap/API/support/architecture/installer Markdown files.
9. Keep code changes scoped. Avoid unrelated refactors and large new file trees. Preserve user changes and historical data.
10. If blocked, record the exact failing test/log and the smallest next action; do not hide the blocker or claim an unverified pass.

### Immediate next task

1. Run the source-build installer in a disposable Debian 13 Proxmox CT; verify fresh install, health check, reboot persistence, in-place upgrade, forced failed-upgrade rollback, SQLite backup, and restore.
2. Run the complete server-agent workflow on disposable Linux and Windows VMs, including enrollment, inventory, typed service-control execution, heartbeat loss, agent/server restart, result retry, stale claims, overdue alerts, and manual resolution.
3. Finish public/reproducible agent distribution and installer work, including deciding when the private agent repository is ready to become public.
4. Publish the first tagged server release only after the CT install/upgrade/rollback path is qualified.
5. Keep patch discovery and installation deferred until the real Linux/Windows job lifecycle and recovery behavior is verified.
---

## 13. Product completion definition

Euryphos is not enterprise-ready until an operator can securely enroll a declared-supported endpoint, see accurate fresh inventory, find every endpoint matching a fleet query, preview and approve a target set, dispatch work promptly, durably queue eligible offline work, observe receipt/progress/failure/retry, verify the actual endpoint state, and export authorized evidence/audit records. This must work across the published platform matrix at a measured and documented fleet scale with tested backup, restore, upgrade, rollback, and security controls.
