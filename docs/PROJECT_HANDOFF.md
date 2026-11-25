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
- `audit_log`: initial operational events.
- `admin_sessions`: hashed server-side browser sessions and CSRF material.
- `schema_migrations`: startup schema marker table. The schema is defined only in Go in `migrate()`; there is no standalone SQL migration file or second schema copy. The current startup routine creates missing tables and indexes and additively adds missing columns, with markers through version **13**. This is intentionally an in-code, idempotent schema initializer for the ground-up product. Future upgrades must preserve existing device rows, identifiers, credentials, inventory, groups, memberships, jobs, job outcomes, audit records, and sessions. Do not drop, truncate, recreate, or reset populated tables during an ordinary application upgrade. Any non-additive data transformation requires an explicit tested conversion and a verified backup/restore path before it can ship.

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

Initial audit coverage includes administrator login success/failure, enrollment, approval/revocation, group mutation, job creation/results, and duplicate merges. This is an operational audit foundation only. It is not immutable, tamper-evident, retention-governed, or compliance-complete.

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

**Stale-heartbeat visibility:** `EURYPHOS_JOB_STALE_SECONDS` defaults to 180 seconds and accepts 60–86400. The API returns `stale_running` in the job list and `heartbeat_state=current|overdue|not_applicable` per execution in job details. The browser shows the overdue count and heartbeat state beside the real execution status. This is a calculated read-only flag; it does not alter `job_targets.status`, terminate a process, cancel work, or requeue a target. An overdue heartbeat means "needs operator review", not proof that the action stopped.

**Remaining limitation:** no notification, persistent `stalled` state, or administrator resolution/retry workflow exists yet. A running target is never automatically requeued based only on an old timestamp, because the original action may still be executing on an unreachable endpoint. If an agent is permanently lost or its local config is deleted, the target can remain `running` indefinitely; the operator can now identify old heartbeats but must resolve them through a future explicit workflow.

This is an incremental attempt identity, journal, heartbeat and result-deduplication safeguard, **not** exactly-once execution or a complete retry system. The agent does not prove whether a partially executed non-idempotent operation took effect before an outage. Bounded retry/backoff, per-action retry classification, explicit job expiry/cancellation, stale-running administration, and robust restart/network-partition reconciliation remain open. The 10-minute timeout applies only to unacknowledged `received` claims, not long-running `running` actions.

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
- `GET /api/v1/jobs` — paginated jobs with target counts, `last_updated_at`, and calculated `stale_running`; `POST /api/v1/jobs`; `GET /api/v1/jobs/{id}` — per-target `updated_at` and `heartbeat_state` (`current`, `overdue`, or `not_applicable`) alongside real execution status/error.
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

The earlier API activity timestamp run **[45630523155](https://github.com/gitarman94/Euryphos/actions/runs/45630523155)** also passed, including the job/target activity timestamp assertions. Handoff synchronization run **[45631440620](https://github.com/gitarman94/Euryphos/actions/runs/45631440620)** passed after these changes. These checks do not execute the installer inside Debian 13 or run a live server/agent deployment.

### Agent CI

GitHub Actions run **[45619870545](https://github.com/gitarman94/Euryphos-Agent/actions/runs/45619870545)** on agent commit `56f4271bfca4136ca464e8c7d0c706ed1b42293c` completed successfully on 2026-11-25. It passed agent tests, vet, Linux build, Windows cross-build, and macOS cross-build. These cross-builds do not certify runtime support on those systems.

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

### P0 — Verify the newest changes

1. Check current main branch heads and Actions for both repositories.
2. Confirm server CI after job lease recovery, the regression tests, schema version 12, and server-side reserved service-name rejection.
3. Confirm agent CI after the case-insensitive `systemctl` rejection fix.
4. Fix only demonstrated failures. Re-run Go formatting, `go test ./...`, `go vet ./...`, and builds.
5. Update both handoff documents with exact run IDs, commits, and results. Do not write “passes” until CI evidence exists.

### P1 — Make job delivery/recovery production-grade

The 10-minute receive lease is a narrow first repair, not the final job lifecycle. Implement and test:

- Explicit target lifecycle and timestamps for queued, dispatched/received, running, progress, succeeded, failed, cancelled, expired, and verification-failed states.
- Attempt number, delivery/lease ID, lease expiry, start/heartbeat/progress, and result idempotency key.
- Running-job heartbeat is now renewed through the same-claim `started` result; the API and console expose the last server-confirmed target activity time.
- Operator notifications and a visible `stalled/indeterminate` presentation for targets whose read-only `heartbeat_state` is `overdue`, plus an administrator resolution/retry workflow. The current calculated flag is not persisted as job state and must not automatically requeue a running non-idempotent action.
- Recovery after endpoint restart, network loss, server restart, and a job that was received but not reported.
- Explicit expiry/deadline and cancellation rules.
- Bounded retry/backoff/jitter, per-action retry classification, dead-letter/manual review.
- Duplicate delivery handling. Define which actions are idempotent and make non-idempotent actions safe or non-retriable.
- Keep audit evidence for each attempt, recovery, cancellation, and final result.
- Avoid a whole-fleet scan on each endpoint poll; use device/time indexes and bounded batches.
- Add a real server+agent protocol test for disconnect during execution, process restart, duplicate result, stale lease, and partial fleet job completion.

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

**Still required before calling this deployment production-ready:** publish the first tagged release and execute the tag-triggered release workflow (including archive and checksum verification); then execute fresh install, repeat install, in-place upgrade with populated device/inventory/group/job/audit data, a deliberately failed upgrade and rollback, reboot/startup, manual backup/restore, and disk-full/backup-failure cases in disposable Debian 13 Proxmox CTs. Confirm credentials and device tokens remain unchanged. The installer script syntax passes CI, but the installer has not been run in a Debian 13 CT. Add cryptographic release signing/provenance verification; the installer currently validates SHA-256 checksums against the release manifest but does not establish a separate signing trust root. Do not add Docker or Docker Compose deployment files.

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

The repository is private, so the bootstrap command must retrieve `scripts/install.sh` with a fine-grained GitHub token that has Contents: read for this repository. The installer then uses the same token to download release metadata and assets. Do not save the GitHub token in the Euryphos environment file.

Run as root inside Debian 13 CT:

```bash
set -e
apt-get update && apt-get install -y ca-certificates curl
read -rsp "GitHub read-only token: " GITHUB_TOKEN
printf '\n'
export GITHUB_TOKEN
INSTALLER="$(mktemp)"
trap 'rm -f "$INSTALLER"; unset GITHUB_TOKEN' EXIT
curl -fsSL \
  -H "Authorization: Bearer ${GITHUB_TOKEN}" \
  -H "Accept: application/vnd.github.raw+json" \
  "https://api.github.com/repos/gitarman94/Euryphos/contents/scripts/install.sh?ref=main" \
  -o "$INSTALLER"
bash -n "$INSTALLER"
GITHUB_TOKEN="$GITHUB_TOKEN" bash "$INSTALLER" install
```

For an upgrade, rerun the same command with final argument `upgrade`. Set `EURYPHOS_VERSION=vX.Y.Z` to pin a release. Push a `v*` tag only after CI passes; the release workflow checks source, cross-builds Linux amd64/arm64, archives the binary + systemd unit + license, and uploads `SHA256SUMS`. A published release is required for the installer.

The installer requires root, Debian 13, and systemd. It installs deployment utilities `curl`, `jq`, `sqlite3`, `openssl`, and CA certificates. The application itself remains a single Go server process using embedded SQLite; no database daemon is installed.

On first installation the script generates `EURYPHOS_ADMIN_TOKEN` and `EURYPHOS_ENROLLMENT_TOKEN` and writes them once to the root-only environment file. It sets:
- `EURYPHOS_LISTEN=:8080`.
- `EURYPHOS_DB_PATH=/var/lib/euryphos/euryphos.db`.
- `EURYPHOS_AUTO_MIGRATE=true`.
- `EURYPHOS_COOKIE_SECURE=true`.
- `EURYPHOS_ONLINE_GRACE_SECONDS=90`.

Existing values remain unchanged during upgrades. `EURYPHOS_LISTEN` must end in a TCP port for the installer health check. The standard DB path must remain under `/var/lib/euryphos` so the systemd service can write to it and automatic backup/rollback can protect it.

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

1. Add an operator notification and explicit `stalled/indeterminate` view for running targets whose heartbeat is `overdue`. Preserve the current read-only semantics: the real status remains `running` and no automatic requeue occurs.
2. Implement administrator resolution for overdue/stale claims with confirmation, warnings, audit events, and retry restrictions based on action idempotency. A stale local agent journal returning HTTP 409 must not be silently cleared.
3. Add protocol tests for heartbeat loss, job deadline, agent crash during execution, server restart, stale claim, and resolution/recovery.
4. Execute real server + agent tests on disposable Linux and Windows VMs: enrollment, full inventory, safe service control, interruption, lost result response, reconnect, and stale-claim behavior.
5. Complete first server release and Debian 13 Proxmox fresh install, upgrade, failed-upgrade rollback, reboot, backup, and restore qualification.
6. Keep patch discovery and installation behind verified running-job recovery and per-action retry safety.

---

## 13. Product completion definition

Euryphos is not enterprise-ready until an operator can securely enroll a declared-supported endpoint, see accurate fresh inventory, find every endpoint matching a fleet query, preview and approve a target set, dispatch work promptly, durably queue eligible offline work, observe receipt/progress/failure/retry, verify the actual endpoint state, and export authorized evidence/audit records. This must work across the published platform matrix at a measured and documented fleet scale with tested backup, restore, upgrade, rollback, and security controls.
