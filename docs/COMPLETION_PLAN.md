# Euryphos — Detailed Completion Plan and Engineering Work Breakdown
## From Current Prototype to Enterprise Systems Management Platform

**Document status:** Proposed implementation plan  
**Purpose:** Break the target Euryphos product into actionable engineering work, dependencies, deliverables, and acceptance criteria.  
**Important:** This plan distinguishes existing repository foundations from work that is not yet implemented. It is not a statement that any phase is complete.

---

## 0. How to use this plan

This is the master execution checklist. Each work item should become an issue or task with an owner, estimate, dependency, test evidence, and completion status.

Recommended status values:

- `NOT STARTED`
- `IN PROGRESS`
- `BLOCKED`
- `IN REVIEW`
- `DONE`
- `DEFERRED`

Do not mark a capability complete because a route, button, struct, or placeholder exists. Completion requires implementation, security review where relevant, automated tests, integration evidence, operator documentation, and acceptance criteria passing.

### 0.1 Completion gates

A workstream is complete only when:

1. Its design and API/data contracts are documented.
2. Implementation is complete for all supported platforms.
3. Failure and recovery paths are implemented.
4. Unit, integration, and relevant end-to-end tests pass.
5. Security and authorization behavior is tested.
6. Metrics, logs, and audit evidence exist.
7. Documentation and operational runbooks are updated.
8. Acceptance criteria are demonstrated in a repeatable environment.


## Device inventory and management UI scope (required specification)

The full field catalog, privacy model, API requirements, and acceptance criteria are maintained in [`docs/DEVICE_MANAGEMENT_UI_SPEC.md`](DEVICE_MANAGEMENT_UI_SPEC.md). The separate endpoint collection contract is in the Euryphos-Agent repository at [`docs/TELEMETRY_COLLECTION_SPEC.md`](https://github.com/gitarman94/Euryphos-Agent/blob/main/docs/TELEMETRY_COLLECTION_SPEC.md).

Implementation tasks (status as of 2026-11-25; all remain open until their full acceptance criteria pass):
- [ ] Add paginated/searchable/sortable Devices page with clickable hostname links and presence/lifecycle states separated. **Partial API foundation:** server list supports basic search, status/OS/connection-state filters, allowlisted sorting, and bounded paging; browser UI and complete lifecycle controls remain unimplemented.
- [ ] Add full device detail page and normalized data APIs for identity, directory, hardware, OS, storage, network, agent, certificates, user sessions, activity, and inventory freshness. **Partial API foundation:** an administrator detail API returns core identity and latest category payloads, plus a paginated inventory-history endpoint; the browser page, normalized field APIs, and activity/freshness views remain outstanding.
- [ ] Add server-derived online/offline, first-connected, last-connected, and connecting-IP values with trusted-proxy handling. **Partial:** server derives online/offline/never-connected and persists first/last contact timestamps; trusted-proxy connecting-IP capture and its security controls remain outstanding.
- [ ] Add versioned normalized inventory storage, snapshots/history, migrations, and category freshness. **Partial:** current category payloads are retained alongside append-only timestamped history; versioned typed/normalized records, category freshness states, retention, and backup/restore proof remain outstanding.
- [ ] Implement endpoint providers and cross-platform telemetry in Euryphos-Agent according to its collection specification. **Partial:** initial Linux DMI, CPU/RAM, boot/uptime, root filesystem, battery-where-present, network, and DPKG/RPM/pacman collection exists; full Linux coverage, Windows detail, macOS, and distribution validation remain outstanding.
- [ ] Implement client-local GPS opt-in setting, default disabled, locally authoritative, tested through upgrades and remote-policy attempts. **Partial privacy guard:** the local config field exists and defaults to false; GPS collection, a user-facing local opt-in workflow, and upgrade/remote-policy tests remain outstanding.
- [ ] Add role permissions, audit for sensitive inventory access/export, retention limits, and redaction before exposing GPS, serial/SMBIOS IDs, network identifiers, and logged-in users.
- [ ] Complete real server/agent integration tests proving collection, persistence, and device-detail rendering with actual data.

Do not mark the UI complete while it is only a static mockup or while displayed fields have no implemented data source.

---
---

# PART I — REPOSITORY AUDIT AND PROJECT CONTROL

## 1. Establish the actual baseline

### 1.1 Inventory the repository

Current server repository contains:

- `cmd/euryphos-server/` — Go management API server.
- `Euryphos-Agent` (separate repository) — Go endpoint agent and client-only installers.
- `migrations/` — versioned SQLite schema migrations.
- `go.mod` — Go module and runtime dependencies.
- `Dockerfile` and `docker-compose.yml` — container deployment.
- `docs/` — product, architecture, implementation status, and completion plan.

Tasks:

- [ ] Maintain a canonical repository tree document.
- [ ] Inspect every Go package and record its purpose and ownership.
- [ ] Identify dead, duplicate, obsolete, experimental, and unused code.
- [ ] Confirm server entry point and supported build targets; validate the independent agent project separately.
- [ ] Trace every server route to its agent or administrator caller.
- [ ] Identify all configuration variables, paths, ports, and credentials.
- [ ] Record every Go module dependency and version.
- [ ] Run `go test ./...`, `go vet ./...`, and cross-platform builds.
- [ ] Run migration tests against disposable SQLite files, including upgrade, rollback/backup, integrity, and restore cases.
- [ ] Test installers and service lifecycle in disposable Windows VMs and each declared Linux distro/architecture VM.
- [ ] Test the macOS installer in Intel and Apple Silicon VMs or physical test devices when that later phase begins.
- [ ] Record baseline failures without silently changing behavior.

**Deliverable:** `docs/BASELINE_AUDIT.md`

**Acceptance:** Every shipped component has an owner/purpose, all entry points and network calls are mapped, and build/test/install results are recorded.

### 1.3 Establish the two-repository distribution model

The target end-user model is a central Euryphos server repository and a separately installable endpoint-agent repository. The current endpoint repository is `https://github.com/gitarman94/Euryphos-Agent`; it is source-separated and independently buildable, but not yet production-installable or verified end-to-end. The former `CommandPilot-Agent` repository is historical and remains a source, installer, dependency, and license-provenance audit item. See `docs/REPOSITORY_LAYOUT.md`.

Tasks:

- [ ] Audit the full historical standalone agent repository, its source, commit history, dependencies, license notices, and Linux/Windows scripts.
- [ ] Publish and version the Euryphos API/protocol contract independently of either repository's implementation.
- [ ] Define one canonical ownership location for Go agent source and remove duplicate maintained implementations after compatibility coverage exists.
- [x] Create the separately maintained `Euryphos-Agent` repository and make its Go module independently buildable without server or SQLite dependencies.
- [ ] Complete the historical agent source/history/license audit and determine which files, if any, retain prior GPL-3.0 grants.
- [ ] Make the agent production-installable without the server repository or SQLite runtime; this requires tested Linux and Windows installers and versioned releases.
- [ ] Keep the main repository focused on the server, API, SQLite migrations, server installer/upgrade, and operator console.
- [ ] Publish client-only Linux and Windows installers that download immutable, versioned, checksum- and signature-verified release artifacts.
- [ ] Add macOS agent/package/service work in the future macOS phase, without claiming macOS support before signing and runtime tests.
- [ ] Add server-to-agent contract CI and a full enrollment, approval, heartbeat, inventory, job, result, disconnect, and reconnect test.
- [ ] Update cross-repository READMEs, release links, security policy, support matrix, and issue templates together.

**Deliverable:** `Euryphos` server repository plus independently installable `Euryphos-Agent` repository and a tested versioned release workflow.

**Acceptance:** A user can deploy the server once and install only the agent on a client host from a versioned release, complete secure enrollment/approval, and verify inventory and job results against the deployed server.

### 1.2 Keep documentation aligned with the Go architecture

The active implementation standard is Go for both server and endpoint agent, with embedded SQLite as the persistent data store. Historical Python/Rust source and installers are reference-only and must not reintroduce another first-party runtime. SQLite WAL supports concurrent readers but serializes writes; expected fleet load, backup/restore, and single-server topology must be validated.

Tasks:

- [ ] Treat checked-in Go source and versioned migrations as implementation evidence.
- [ ] Keep API routes, authentication, payload schemas, and lifecycle states synchronized across the server repository, Euryphos-Agent repository, and docs.
- [ ] Keep the Go module dependency list and deployment image reproducible.
- [ ] Remove stale instructions and references when a capability is replaced.
- [ ] Maintain one canonical architecture and operations guide.

**Deliverable:** Updated architecture and operational documentation.

---

# PART II — PRODUCT, ARCHITECTURE, AND CONTRACTS

## 2. Freeze the first supported product scope

Tasks:

- [ ] Define initial Windows versions/editions and architectures, including workstation and server SKUs.
- [ ] Publish `docs/SUPPORT_MATRIX.md` with explicit Arch, Debian, and Fedora family targets and release/architecture criteria.
- [ ] Include Arch Linux, CachyOS, Debian, Ubuntu, Linux Mint (Ubuntu-based editions and LMDE), Fedora Workstation/Server, Nobara, and Bazzite as named candidate distributions.
- [ ] Add other high-value derivatives (for example EndeavourOS, Manjaro, Pop!_OS, Zorin OS, elementary OS, Fedora Atomic variants, Rocky Linux, and AlmaLinux) as tiered candidates, not assumed-compatible products.
- [ ] Define package-manager adapters for pacman, apt/dpkg, dnf/dnf5/rpm, and rpm-ostree/image-based systems.
- [ ] Distinguish agent installation compatibility from inventory, patching, and configuration-provider support.
- [ ] Add a future native macOS client to the product roadmap, with supported macOS versions and Intel/Apple Silicon architecture targets to be decided.
- [ ] Define endpoint classes: workstation, server, VM, cloud VM.
- [ ] Define expected fleet size for first production release and 12–24 month target.
- [ ] Define expected check-in interval and inventory refresh expectations.
- [ ] Define expected concurrent command/deployment volume.
- [ ] Define data retention periods for current inventory, history, events, job output, and audit.
- [ ] Define whether multi-tenant operation is required now or later.
- [ ] Define high availability and disaster recovery requirements.
- [ ] Define which capabilities are MVP, production baseline, and later roadmap.
- [ ] Define explicit product exclusions for initial release.

**Deliverables:** `docs/PRODUCT_SCOPE.md`, `docs/SUPPORT_MATRIX.md`, `docs/NFRS_AND_SLOS.md`

**Acceptance:** Scope and measurable non-functional targets are approved before detailed architecture implementation.

## 3. Architecture design

### 3.1 Define service boundaries

- [ ] Define web console responsibility.
- [ ] Define API responsibility.
- [ ] Define identity and authorization responsibility.
- [ ] Define device registry responsibility.
- [ ] Define inventory ingestion and normalization responsibility.
- [ ] Define software catalog responsibility.
- [ ] Define patch intelligence/provider responsibility.
- [ ] Define policy evaluation responsibility.
- [ ] Define job orchestration responsibility.
- [ ] Define message transport responsibility.
- [ ] Define artifact/package distribution responsibility.
- [ ] Define audit/event responsibility.
- [ ] Define reporting/search responsibility.
- [ ] Define agent responsibilities and local state.

### 3.2 Define communication topology

- [ ] Decide agent transport: persistent mutually authenticated connection, broker-backed channel, or another explicitly designed mechanism.
- [ ] Define whether agents initiate outbound-only connections.
- [ ] Define reconnect, heartbeat, keepalive, backoff, and jitter behavior.
- [ ] Define message acknowledgement semantics.
- [ ] Define durable offline delivery and expiry.
- [ ] Define maximum payload size and chunking.
- [ ] Define event ordering guarantees and where ordering is not guaranteed.
- [ ] Define server-to-agent and agent-to-server protocol version negotiation.
- [ ] Define proxy support and certificate validation behavior.
- [ ] Define connection capacity and load test targets.

### 3.3 Design decisions

- [ ] Write architecture decision records (ADRs) for database, broker, worker model, agent transport, identity, search, package storage, and deployment topology.
- [ ] Keep the management server and agent implemented in Go; do not reintroduce Flask/Python or Rust as runtime dependencies.
- [ ] Define modular boundaries so components can evolve without a premature microservice split.
- [ ] Define local development, integration, staging, and production environments.
- [ ] Create architecture diagrams: context, containers/services, agent sequence, inventory flow, deployment flow, trust boundaries.

**Deliverables:** `docs/ARCHITECTURE.md`, `docs/ADRS/`, architecture diagrams.

**Acceptance:** Every major component, trust boundary, data flow, failure mode, and protocol is documented.

## 4. API and message contracts

### 4.1 API standards

- [ ] Establish `/api/v1/` versioned routes.
- [ ] Standardize JSON naming, timestamps (UTC RFC 3339), identifiers, pagination, filtering, sorting, and error format.
- [ ] Define request correlation IDs and idempotency keys.
- [ ] Define authentication and authorization requirements per endpoint.
- [ ] Define rate limits and request size limits.
- [ ] Define compatibility and deprecation policy.
- [ ] Publish OpenAPI specification.
- [ ] Add schema validation at all API boundaries.

### 4.2 Required API domains

- [ ] Health/readiness/liveness.
- [ ] Enrollment and device identity.
- [ ] Device approval, revocation, re-enrollment, and decommission.
- [ ] Device heartbeat and agent status.
- [ ] Inventory full and delta upload.
- [ ] Inventory query and device detail.
- [ ] Software catalog and per-device installed software.
- [ ] Patch discovery and update state.
- [ ] Dynamic groups and saved queries.
- [ ] Policies and assignments.
- [ ] Jobs, job targets, per-device executions, logs, and results.
- [ ] Approvals and change records.
- [ ] Artifact metadata and secure download.
- [ ] Agent version and update channel.
- [ ] Audit events and reporting.
- [ ] Administrator, role, and service identity management.

### 4.3 Agent message contracts

Define versioned schemas for:

- [ ] `agent.hello`
- [ ] `agent.heartbeat`
- [ ] `inventory.full`
- [ ] `inventory.delta`
- [ ] `inventory.ack`
- [ ] `job.dispatch`
- [ ] `job.received`
- [ ] `job.started`
- [ ] `job.progress`
- [ ] `job.completed`
- [ ] `job.failed`
- [ ] `job.cancel`
- [ ] `policy.assignment`
- [ ] `policy.ack`
- [ ] `agent.update`
- [ ] `agent.diagnostic`

Every message must include schema version, message ID, device ID, creation time, correlation/job ID where applicable, and integrity/authentication protection.

**Deliverables:** `docs/API_SPECIFICATION.md`, OpenAPI file, JSON schemas, protocol compatibility tests.

**Acceptance:** Server and both agent platforms pass shared contract tests using the same canonical schemas.

---

# PART III — SECURITY AND IDENTITY

## 5. Threat model and security architecture

Tasks:

- [ ] Perform a threat model covering management server compromise, administrator account compromise, malicious/rogue agent, stolen device credential, replayed command, malicious package, compromised build pipeline, insider misuse, and tenant boundary failure.
- [ ] Define security zones and trust boundaries.
- [ ] Define privileged operations and required approvals.
- [ ] Define data classification and sensitive inventory categories.
- [ ] Define incident response and credential compromise procedures.
- [ ] Perform security architecture review before enabling fleet-wide execution.

**Deliverable:** `docs/SECURITY_THREAT_MODEL.md`

## 6. Administrator identity and authorization

- [ ] Implement administrator authentication; do not use a shared static admin token embedded in templates or browser requests.
- [ ] Support enterprise SSO integration using a standard protocol where selected.
- [ ] Require MFA according to organizational policy.
- [ ] Implement secure sessions, cookie flags, CSRF protection, session expiry, and logout.
- [ ] Implement RBAC with roles and permission scopes.
- [ ] Add scope restrictions by site, group, environment, and device class.
- [ ] Separate read, inventory export, patch approval, deployment, configuration, script execution, reboot, and platform administration permissions.
- [ ] Implement approval workflows for high-risk operations.
- [ ] Support break-glass access with strong audit and expiry.
- [ ] Test privilege escalation and horizontal access-control failures.

## 7. Agent identity and enrollment

- [ ] Design secure enrollment bootstrap.
- [ ] Ensure device identity is not based solely on a mutable hostname or unverified serial number.
- [ ] Issue unique per-agent credentials or certificates.
- [ ] Protect credentials at rest using platform-native secure storage where available.
- [ ] Support credential rotation and revocation.
- [ ] Support lost/reinstalled/reimaged devices and identity reconciliation.
- [ ] Prevent unauthorized re-enrollment from taking over an existing asset.
- [ ] Bind agent identity to device identity and enrollment state.
- [ ] Enforce TLS and certificate validation.
- [ ] Add replay protection and message authentication.
- [ ] Test revoked, expired, duplicated, and stolen credentials.

## 8. Command trust and artifact security

- [ ] Sign command envelopes or protect them with authenticated transport and explicit message integrity.
- [ ] Agent validates target device, action type, schema version, expiry, authorization claims, and idempotency key.
- [ ] Define a strict action allowlist.
- [ ] Require signed packages and approved automation content.
- [ ] Verify hashes and signatures before execution.
- [ ] Store signing keys outside the application database and source repository.
- [ ] Define signing key rotation and revocation.
- [ ] Prevent path traversal, archive extraction attacks, unsafe temporary file handling, and command injection.
- [ ] Bound execution time, output, disk usage, and resource consumption.
- [ ] Redact secrets from command output and logs.
- [ ] Add security tests for malicious payloads and invalid signatures.

**Deliverable:** `docs/SECURITY.md`, credential lifecycle, signed artifact workflow, security test suite.

---

# PART IV — DATA MODEL AND INVENTORY

## 9. Database foundation

The current SQLAlchemy model is centered on a `Client` table and a `ClientUpdate` table. That is insufficient for enterprise inventory, historical state, policies, and large-scale job execution.

### 9.1 Define normalized data domains

Design and implement tables/entities for:

- [ ] Organizations/tenants (if required).
- [ ] Sites, groups, tags, and device memberships.
- [ ] Devices and stable device identity.
- [ ] Agent installations and agent versions.
- [ ] Enrollment and credential metadata (never raw secrets).
- [ ] Hardware components.
- [ ] OS/platform facts.
- [ ] Installed software and normalized software catalog.
- [ ] Services and scheduled tasks.
- [ ] Network interfaces and configuration.
- [ ] Configuration facts and baseline results.
- [ ] Vulnerabilities and affected software/device relationships.
- [ ] Updates/patch catalog and applicability.
- [ ] Inventory snapshots and per-category freshness.
- [ ] Policies and policy versions.
- [ ] Policy assignments and evaluation results.
- [ ] Jobs/campaigns and job target snapshots.
- [ ] Per-device executions, attempts, progress, and results.
- [ ] Artifacts and signatures.
- [ ] Administrator identities, roles, permissions, approvals.
- [ ] Audit events and operational events.

### 9.2 Data design requirements

- [ ] Use stable primary keys and explicit foreign keys.
- [ ] Define unique constraints and deduplication rules.
- [ ] Add indexes for fleet search and common joins.
- [ ] Define time-series/history strategy.
- [ ] Define retention and archival.
- [ ] Define JSON/document fields only where flexible provider-specific data is genuinely needed.
- [ ] Preserve raw provider facts when needed for traceability, alongside normalized values.
- [ ] Track `observed_at`, `received_at`, source, schema version, and collection status.
- [ ] Define soft deletion and device decommission behavior.
- [ ] Add migration tooling and repeatable migration tests.
- [ ] Remove production reliance on `db.create_all()` as a schema migration method.
- [ ] Test restore from backup and migration from the current prototype schema.

**Deliverable:** `docs/DATABASE_SCHEMA.md`, ERD, migration set.

### 9.3 SQLite deployment, performance, and recovery requirements

- [ ] Use one local SQLite database file per active server instance; do not run multiple server replicas against the same file.
- [ ] Enable WAL, foreign keys, busy timeout, and an explicitly bounded connection pool on every new connection.
- [ ] Keep the live database on a local filesystem with reliable locking; do not use NFS/SMB or cloud-synchronized folders for the active file.
- [ ] Add versioned migrations with checksums and pre-migration backup/restore gates.
- [ ] Implement online backup or a tested quiesced backup procedure that correctly handles WAL state.
- [ ] Add scheduled `quick_check`/`integrity_check`, restore drills, retention, compaction, and disk-space monitoring.
- [ ] Benchmark concurrent heartbeat/inventory writes, job dispatch, dashboard reads, and retention at target fleet sizes.
- [ ] Document writer serialization, queue/backpressure behavior, operational capacity limits, and supported topology.
- [ ] Do not claim SQLite is categorically faster than other databases; record comparative results for Euryphos's actual workload.

**Acceptance:** Restore from backup onto a clean host, validate integrity, and bring the service online with all devices/jobs intact.

## 10. Inventory provider framework

### 10.1 Agent inventory architecture

- [ ] Define provider interface per inventory category.
- [ ] Providers return typed, versioned records and structured errors.
- [ ] Implement timeouts and resource limits for collection.
- [ ] Separate privileged collectors from the unprivileged agent control process where necessary.
- [ ] Support collection profiles and policy-based enablement.
- [ ] Support full scan, delta scan, and on-demand scan.
- [ ] Persist local cursor/state for incremental collection.
- [ ] Deduplicate records and detect removals.
- [ ] Report collection completeness and freshness.
- [ ] Test permission denied, unsupported OS, missing command, malformed output, and partial collection.

### 10.2 Windows inventory providers

- [ ] OS edition/version/build/architecture and boot/uptime.
- [ ] Manufacturer/model/serial/UUID/BIOS.
- [ ] CPU, memory, storage, filesystems, and volumes.
- [ ] Network adapters, addresses, DNS, and routes.
- [ ] Installed machine-wide applications.
- [ ] Relevant per-user application inventory according to collection policy.
- [ ] MSI and non-MSI software sources.
- [ ] Windows Store/AppX inventory where in scope.
- [ ] Windows features and roles where in scope.
- [ ] Services and startup configuration.
- [ ] Scheduled tasks.
- [ ] Drivers and update history where available.
- [ ] Local users/groups and privileged group membership under policy.
- [ ] Firewall, Defender/endpoint protection, encryption, TPM, Secure Boot posture where supported.
- [ ] Certificate metadata without private key export.
- [ ] Optional process/socket snapshot with explicit privacy controls.

### 10.3 Linux inventory providers

- [ ] Distribution, release, kernel, architecture, boot/uptime.
- [ ] DMI manufacturer/model/serial/UUID/BIOS where exposed.
- [ ] CPU, memory, storage, filesystems, mounts.
- [ ] Network interfaces, addresses, DNS, routes.
- [ ] Installed packages using supported native package databases.
- [ ] Package origin/repository and architecture.
- [ ] Services and systemd unit state.
- [ ] Timers, cron, and scheduled tasks.
- [ ] Kernel modules and drivers where relevant.
- [ ] Local users/groups and privileged membership under policy.
- [ ] Firewall state using supported providers.
- [ ] Disk encryption and Secure Boot/TPM posture where available.
- [ ] Certificate metadata without private key export.
- [ ] Optional process/socket snapshot with explicit privacy controls.

### 10.4 Software normalization

- [ ] Create normalized vendor/product identity rules.
- [ ] Preserve raw package/product identifiers.
- [ ] Normalize version strings without assuming all version schemes are semver.
- [ ] Track architecture and install scope.
- [ ] Detect software additions, upgrades, and removals.
- [ ] Handle duplicate registrations and multiple versions.
- [ ] Support software lifecycle state: approved, prohibited, vulnerable, unsupported, unknown.
- [ ] Build tests from representative Windows and Linux inventory fixtures.

### 10.5 Server ingestion

- [ ] Authenticate agent before accepting inventory.
- [ ] Validate schema and payload limits.
- [ ] Make ingestion idempotent.
- [ ] Handle duplicate, out-of-order, and delayed events.
- [ ] Separate raw ingestion from normalization/indexing where needed.
- [ ] Record per-category freshness and collection errors.
- [ ] Protect against a single large upload starving other agents.
- [ ] Add ingestion metrics and dead-letter handling.
- [ ] Test high-volume and malformed inventory uploads.

**Deliverable:** Inventory providers for supported OS matrix, ingestion pipeline, normalized inventory API.

**Acceptance:** A test fleet can be searched centrally by hardware, OS, installed product and version, service, and configured posture facts, with freshness visible.

---

# PART V — FLEET SEARCH AND DASHBOARD

## 11. Fleet query engine

- [ ] Define query language or structured filter model.
- [ ] Support AND/OR/NOT and nested conditions.
- [ ] Support equality, inequality, contains, prefix, version comparisons, ranges, and set membership where meaningful.
- [ ] Query across device, OS, hardware, software, patch, configuration, and group data.
- [ ] Add saved searches.
- [ ] Add dynamic groups backed by saved queries.
- [ ] Preview matching device count and sample results.
- [ ] Version saved queries and track their use in jobs.
- [ ] Enforce authorization and data visibility in every query.
- [ ] Add pagination, sorting, export limits, and query timeouts.
- [ ] Add query audit for sensitive searches/exports.
- [ ] Load test against the agreed estate size and inventory volume.

## 12. Dashboard and operator console

### 12.1 Global navigation and layout

- [ ] Define information architecture and navigation.
- [ ] Add role-aware navigation and actions.
- [ ] Add global search.
- [ ] Add site/group/environment context.
- [ ] Add notifications and job activity.
- [ ] Ensure accessibility, keyboard navigation, responsive layout, and supported browsers.

### 12.2 Fleet overview

- [ ] Device totals by lifecycle and connectivity state.
- [ ] OS/version distribution.
- [ ] Hardware distribution.
- [ ] Installed software and version distribution.
- [ ] Patch compliance and missing security updates.
- [ ] Vulnerability exposure.
- [ ] Agent health/version drift.
- [ ] Configuration compliance/drift.
- [ ] Deployment success/failure and recent activity.
- [ ] Inventory freshness and collector failure indicators.
- [ ] Click-through from each metric to the exact underlying target set.
- [ ] Clearly label stale, estimated, or partial data.

### 12.3 Device explorer

- [ ] Search and filter across the whole estate.
- [ ] Column chooser and saved views.
- [ ] Bulk selection and safe “select all matching query” behavior.
- [ ] Device lifecycle controls.
- [ ] Export with authorization and audit.
- [ ] Show last seen, agent state, and inventory freshness.

### 12.4 Device detail

- [ ] Overview and status.
- [ ] Hardware.
- [ ] OS and boot information.
- [ ] Installed software.
- [ ] Services and scheduled tasks.
- [ ] Network configuration.
- [ ] Security posture and baselines.
- [ ] Updates and vulnerabilities.
- [ ] Policies and drift.
- [ ] Jobs and command history.
- [ ] Events, logs, and diagnostics.
- [ ] Agent version/configuration.
- [ ] Audit history.

### 12.5 Live job console

- [ ] Show job accepted/queued/dispatched/received/running/completed states.
- [ ] Show per-device progress and timestamps.
- [ ] Filter success, failure, offline, pending, excluded, and expired.
- [ ] Show structured failure details and remediation hints.
- [ ] Pause/cancel/retry where safe.
- [ ] Export execution evidence.
- [ ] Distinguish dispatch from verified completion.

**Deliverable:** Operational console backed by real APIs, not hardcoded or placeholder dashboard values.

---

# PART VI — AGENT COMMUNICATION AND ORCHESTRATION

## 13. Agent lifecycle and service reliability

### 13.1 Common agent behavior

- [ ] Define a single configuration schema.
- [ ] Reconcile existing use of `config.json`, `client_id.txt`, and `server_url.txt`.
- [ ] Use one authoritative persistent identity/configuration store.
- [ ] Implement first-start enrollment.
- [ ] Implement approval and denied/pending states.
- [ ] Implement secure credential storage.
- [ ] Implement heartbeat and connection health.
- [ ] Implement reconnect with exponential backoff and jitter.
- [ ] Implement graceful shutdown and service restart behavior.
- [ ] Implement local durable queue/state for in-flight work.
- [ ] Implement agent health diagnostics.
- [ ] Implement structured logs with rotation and privacy controls.
- [ ] Report agent version, platform, capabilities, and protocol version.
- [ ] Add watchdog and recovery behavior.
- [ ] Test server unavailable, DNS failure, TLS failure, proxy failure, sleep/resume, reboot, and credential revocation.

### 13.2 Low-latency command transport

- [ ] Select persistent transport and document rationale.
- [ ] Implement connection registration and keepalive.
- [ ] Implement server-side connection registry.
- [ ] Implement durable broker/queue handoff.
- [ ] Implement delivery acknowledgement.
- [ ] Implement reconnect and pending-message replay.
- [ ] Implement message expiry and cancellation.
- [ ] Implement bounded per-agent and global concurrency.
- [ ] Implement backpressure and slow-agent handling.
- [ ] Implement metrics for enqueue-to-dispatch, dispatch-to-receipt, and receipt-to-start latency.
- [ ] Test thousands of simulated agents and reconnect storms.
- [ ] Define SLOs for connected-agent command delivery; do not promise millisecond execution completion.

### 13.3 Durable job orchestration

- [ ] Create job/campaign model.
- [ ] Create immutable target snapshot or explicitly versioned dynamic targeting behavior.
- [ ] Create per-device execution record.
- [ ] Create attempt and result records.
- [ ] Implement idempotency keys.
- [ ] Implement duplicate message suppression.
- [ ] Implement state transition validation.
- [ ] Implement retry policy and exponential backoff.
- [ ] Implement dead-letter state.
- [ ] Implement job expiry and cancellation.
- [ ] Implement pause/resume where action semantics allow.
- [ ] Implement partial-success aggregation.
- [ ] Implement offline target handling.
- [ ] Implement operator-visible failure classification.
- [ ] Ensure HTTP requests enqueue work rather than execute long-running operations inline.

**Deliverable:** A durable, observable, fleet-scale orchestration subsystem.

---

# PART VII — PATCH AND SOFTWARE DEPLOYMENT

## 14. Patch provider architecture

### 14.1 Shared patch model

- [ ] Define platform-neutral update schema.
- [ ] Store platform-specific identifiers and provider metadata.
- [ ] Track applicability, dependencies, supersedence, severity, CVEs, reboot needs, and source.
- [ ] Track per-device detected/installed/failed state.
- [ ] Distinguish patch metadata from deployment execution state.
- [ ] Prevent update records for one device from being incorrectly conflated with another device's state.
- [ ] Define scan freshness and stale update behavior.

### 14.2 Windows update provider

- [ ] Select and document native Windows Update interface/provider.
- [ ] Implement scan and applicability detection.
- [ ] Capture KB/update identity and metadata.
- [ ] Handle optional, driver, feature, and quality updates according to policy.
- [ ] Implement download/staging where supported.
- [ ] Implement selected update installation.
- [ ] Implement reboot requirement detection.
- [ ] Capture per-update result and native error codes.
- [ ] Handle pending reboot, servicing stack constraints, and update engine contention.
- [ ] Implement cancellation behavior where supported.
- [ ] Test on every supported Windows version.
- [ ] Test offline, metered, proxy, WSUS/enterprise update policy, and reboot scenarios.

### 14.3 Linux package provider

- [ ] Define supported package managers/distributions.
- [ ] Implement package metadata refresh safely.
- [ ] Detect available updates without blindly upgrading the whole system.
- [ ] Identify security updates where repository metadata supports it.
- [ ] Capture package name, installed version, candidate version, architecture, repository, dependencies.
- [ ] Implement selected package install/upgrade.
- [ ] Handle package locks and concurrent package manager operations.
- [ ] Handle held packages, pinned versions, dependency conflicts, and broken package states.
- [ ] Capture stdout/stderr safely and classify exit codes.
- [ ] Detect reboot-required conditions where platform supports it.
- [ ] Test Debian/Ubuntu first if that is the initial support commitment; add other distributions only with provider-specific tests.

### 14.4 Third-party software deployment

- [ ] Define package catalog and supported package formats.
- [ ] Define package intake and malware scanning.
- [ ] Verify publisher signatures and cryptographic hashes.
- [ ] Define silent install/uninstall metadata.
- [ ] Define detection rules and post-install verification.
- [ ] Define licensing and redistribution restrictions.
- [ ] Support pilot ring and staged rollout.
- [ ] Record installed version and deployment evidence.
- [ ] Define rollback/repair support per package.

## 15. Deployment campaigns

- [ ] Build campaign authoring workflow.
- [ ] Allow one or more approved updates/packages.
- [ ] Select explicit devices, static groups, or dynamic query.
- [ ] Resolve and snapshot target set.
- [ ] Exclude unsupported, inactive, held, or noncompliant targets according to policy.
- [ ] Preview online/offline and eligible/ineligible counts.
- [ ] Configure priority, deadline, maintenance window, and blackout windows.
- [ ] Configure rollout rings and percentage.
- [ ] Configure max concurrent endpoints and rate limits.
- [ ] Configure download cache/bandwidth policy.
- [ ] Configure reboot policy and user notification.
- [ ] Require approvals based on risk and scope.
- [ ] Dispatch through job orchestration.
- [ ] Monitor and pause/stop safely.
- [ ] Verify final installed state, not merely command exit code.
- [ ] Re-scan and calculate compliance.
- [ ] Produce campaign report and audit evidence.

## 16. Zero-day / emergency response

- [ ] Create emergency campaign type with elevated priority and explicit reason.
- [ ] Ingest or manually register vulnerability/advisory data.
- [ ] Map affected software/version/platform to inventory.
- [ ] Show affected, potentially affected, unknown, and not affected populations separately.
- [ ] Support vendor patch deployment when available.
- [ ] Support approved mitigation actions when patch is unavailable.
- [ ] Support service stop/disable, configuration change, package removal, firewall control, or other typed mitigations as approved.
- [ ] Require emergency approval policy and optionally break-glass flow.
- [ ] Support expedited dispatch while retaining safety limits.
- [ ] Track offline devices and re-contact them when available.
- [ ] Verify mitigation/remediation independently.
- [ ] Provide live fleet exposure reduction view.
- [ ] Preserve incident/change reference, operator, approval, commands, results, and exceptions.

**Acceptance:** An operator can query affected assets, create a controlled emergency campaign, dispatch quickly to connected agents, and prove per-device remediation or outstanding status.

---

# PART VIII — CONFIGURATION AND DESIRED STATE

## 17. Configuration management

- [ ] Define versioned configuration policy schema.
- [ ] Define supported resource types and platform providers.
- [ ] Define desired state, detection, remediation, and verification.
- [ ] Define policy assignment and precedence.
- [ ] Define conflict detection and exception handling.
- [ ] Define idempotency and safe repeated application.
- [ ] Define rollback or compensating action.
- [ ] Define configuration drift reporting.
- [ ] Define maintenance windows and enforcement mode.
- [ ] Implement Windows registry/policy resources where supported.
- [ ] Implement Linux file/service/package resources where supported.
- [ ] Protect file paths, registry paths, ownership, and permissions against unsafe inputs.
- [ ] Test partial failure and recovery.
- [ ] Add policy simulation/dry-run mode.
- [ ] Add policy change approval and version history.

## 18. Action catalog and script execution

- [ ] Define typed built-in action catalog.
- [ ] Define action schemas, permissions, preconditions, and postconditions.
- [ ] Add safe service control actions.
- [ ] Add reboot/shutdown actions with explicit policy and countdown/maintenance rules.
- [ ] Add diagnostic collection.
- [ ] Add agent repair/update.
- [ ] If arbitrary scripts are permitted, define signed script packages, approval, interpreter restrictions, execution timeout, output cap, environment handling, secret injection rules, and audit.
- [ ] Do not expose unrestricted anonymous shell or command execution.
- [ ] Test command injection, privilege abuse, and output leakage.

---

# PART IX — AGENT SELF-UPDATE AND INSTALLATION

## 19. Consolidate updater implementation

The Go agent does not yet include a self-update implementation. Add one cohesive Go updater after signed artifact manifests, trust roots, rollback behavior, and service lifecycle integration are specified.

Tasks:

- [ ] Define authoritative updater module and entry point.
- [ ] Define release manifest schema.
- [ ] Define stable, beta, and pinned update channels if required.
- [ ] Define OS/architecture-specific artifact naming.
- [ ] Verify cryptographic signature and hash before install.
- [ ] Use safe temporary paths and atomic replacement where supported.
- [ ] Preserve configuration and identity across upgrades.
- [ ] Coordinate update with service manager.
- [ ] Implement rollback on failed health check.
- [ ] Report update attempt and result to server.
- [ ] Prevent downgrade unless explicitly authorized.
- [ ] Prevent update loops and partial update states.
- [ ] Test interrupted download, invalid signature, disk full, permission failure, restart failure, and rollback.

## 20. Linux installer and service

- [ ] Build a Go-native installer that installs a signed prebuilt agent; never clone/build Rust or require a Go toolchain on production endpoints.
- [ ] Detect distribution using `/etc/os-release`, `ID`, `ID_LIKE`, version, architecture, and init system; do not branch only on `apt-get`.
- [ ] Support the Debian branch through apt/dpkg: Debian, Ubuntu, Linux Mint (Ubuntu-based editions), and LMDE (Debian-based) as separately tested targets.
- [ ] Support the Arch branch through pacman: Arch Linux and named derivative targets including CachyOS; add EndeavourOS/Manjaro as separately certified candidates.
- [ ] Support the Fedora/RPM branch through rpm plus dnf/dnf5: Fedora Workstation/Server and Nobara, with derivative-specific tests.
- [ ] Design a separate immutable/image-based provider for Bazzite and Fedora Atomic variants; do not assume ordinary host `dnf` updates are safe. Handle rpm-ostree/image deployment, reboot coordination, and rollback semantics.
- [ ] Maintain a distribution test matrix with install, upgrade, inventory, patch scan, patch execution, rollback, uninstall, and service recovery results.
- [ ] Create a dedicated service account with least privilege and a hardened systemd unit.
- [ ] Separate the unprivileged agent from narrowly scoped privileged helpers; define/audit polkit or other privilege delegation rather than running every component as root.
- [ ] Define directories, ownership, permissions, configuration, credential storage, proxy, and custom CA support.
- [ ] Add install, upgrade, repair, uninstall, and rollback modes; preserve device identity and settings across upgrades.
- [ ] Add unattended/non-interactive flags, idempotency, logs, exit codes, preflight checks, and clear unsupported-OS behavior.
- [ ] Test clean install and in-place upgrade in disposable VMs for every declared supported distro and architecture.

## 21. Windows installer and service

- [ ] Define supported Windows client/server releases, x64 and ARM64 architectures, and minimum OS build policy.
- [ ] Produce signed Go agent artifacts and a signed MSI (or other explicitly selected enterprise installer format).
- [ ] Install a native Windows service with correct service account, restricted ACLs, automatic start, and recovery actions.
- [ ] Store device identity and credentials using Windows-protected storage (DPAPI or an approved equivalent); protect configuration ACLs.
- [ ] Define required privileges and separate routine agent operations from privileged helper operations where feasible.
- [ ] Add install, upgrade, repair, uninstall, and rollback; preserve identity/configuration and support silent deployment.
- [ ] Add proxy and enterprise certificate support.
- [ ] Handle service recovery, in-use binary replacement, reboot-required updates, and upgrade rollback safely.
- [ ] Add signing and release provenance verification; never download unverified executables/scripts from a moving branch.
- [ ] Test UAC, locked files, reboot pending, AV/EDR interference, GPO restrictions, offline execution, and service recovery in Windows VMs.


### 21.1 Future macOS endpoint client and installer (planned later phase)

- [ ] Implement a native Go macOS agent for Intel (`amd64`) and Apple Silicon (`arm64`).
- [ ] Decide the supported macOS release floor and support current/previous releases according to Apple's support lifecycle.
- [ ] Implement a hardened `launchd` LaunchDaemon with correct ownership, lifecycle, restart, and logging.
- [ ] Create a signed and notarized PKG installer with install, upgrade, repair, uninstall, and rollback flows.
- [ ] Store secrets in Keychain and define secure enrollment/re-enrollment behavior.
- [ ] Implement inventory providers using native macOS APIs and supported command-line interfaces (hardware, OS/build, applications, packages, services/launch agents, profiles, security posture).
- [ ] Implement Apple-native update discovery/install workflows with reboot/defer controls and explicit support boundaries.
- [ ] Document and respect SIP, TCC/privacy permissions, FileVault, Gatekeeper, and MDM deployment behavior.
- [ ] Add Apple Business Manager/MDM-friendly unattended deployment and custom CA/proxy handling.
- [ ] Test Intel and Apple Silicon clean installs, upgrades, uninstall, OS upgrades, service recovery, signature/notarization, and inventory correctness.

**Acceptance:** The macOS agent enrolls, reports complete inventory, receives authenticated jobs, applies only supported approved actions, and upgrades/rolls back through a signed notarized package.

## 22. Server installation and upgrade

- [ ] Define supported server OS and deployment model.
- [ ] Define secrets injection and rotation.
- [ ] Provision the SQLite database file and protected data directory; no external database server is installed.
- [ ] Configure WAL, foreign keys, busy timeout, bounded pool, and database path.
- [ ] Add secure online backup, restore, integrity validation, retention, disk monitoring, and pre-upgrade backups.
- [ ] Run application under dedicated non-root identity.
- [ ] Configure reverse proxy and TLS.
- [ ] Configure systemd/container orchestration and health checks.
- [ ] Configure worker and broker services.
- [ ] Configure persistent artifact storage.
- [ ] Add backup and restore drills for the SQLite file and WAL state.
- [ ] Add upgrade/rollback procedure.
- [ ] Add installation verification and post-upgrade smoke tests.
- [ ] Remove unsafe defaults and fail closed when secrets are absent.

---

# PART X — REPORTING, AUDIT, AND INTEGRATIONS

## 23. Reporting and compliance

- [ ] Define patch compliance calculation and grace periods.
- [ ] Define vulnerability exposure calculation and confidence.
- [ ] Define software inventory reports.
- [ ] Define hardware and OS lifecycle reports.
- [ ] Define unsupported OS/software reports.
- [ ] Define configuration baseline compliance.
- [ ] Define agent health and stale inventory reports.
- [ ] Define deployment campaign reports.
- [ ] Support saved reports and scheduled delivery.
- [ ] Support CSV/JSON exports with permissions and audit.
- [ ] Include data freshness and coverage caveats in all reports.

## 24. Audit and event history

- [ ] Record administrator login and security events.
- [ ] Record device enrollment, approval, credential rotation, revocation, and deletion.
- [ ] Record inventory exports and sensitive data access.
- [ ] Record policy creation, edits, assignments, and approvals.
- [ ] Record job creation, target snapshot, dispatch, receipt, execution, retries, cancellation, and result.
- [ ] Record artifact creation, signature, publication, and download.
- [ ] Include actor, timestamp, source, target, correlation ID, before/after where applicable, and outcome.
- [ ] Protect audit data from ordinary administrator modification.
- [ ] Define retention, export, and SIEM forwarding.

## 25. Integration framework

- [ ] Define API tokens/service identities with scopes and expiry.
- [ ] Define webhook/event schema and retry behavior.
- [ ] Add identity provider integration.
- [ ] Add SIEM forwarding.
- [ ] Add ITSM/change management integration.
- [ ] Add vulnerability/advisory intelligence integration.
- [ ] Add package/artifact repository integration.
- [ ] Add alerting integrations.
- [ ] Document integration setup and failure modes.

---

# PART XI — TESTING, PERFORMANCE, AND OPERATIONS

## 26. Test strategy

### 26.1 Backend tests

- [ ] Unit tests for models, services, validation, policy evaluation, authorization, state transitions.
- [ ] API tests for every route and error path.
- [ ] Migration tests from empty database and prior schema.
- [ ] Database concurrency and transaction tests.
- [ ] Job orchestration and retry tests.
- [ ] Inventory ingestion idempotency tests.
- [ ] Query authorization and injection tests.

### 26.2 Agent tests

- [ ] Go unit, integration, API-contract, and platform-provider tests.
- [ ] Shared protocol contract tests.
- [ ] Inventory fixture tests per OS.
- [ ] Command validation and idempotency tests.
- [ ] Service lifecycle tests.
- [ ] Network reconnect/offline tests.
- [ ] Updater signature/rollback tests.
- [ ] Platform integration tests in Windows and Linux VMs.

### 26.3 End-to-end scenarios

- [ ] Fresh agent enrollment and approval.
- [ ] Agent sends full inventory.
- [ ] Fleet query finds a software version.
- [ ] Dynamic group membership updates correctly.
- [ ] Operator dispatches an inventory refresh.
- [ ] Operator deploys one selected patch.
- [ ] Operator deploys to a staged group.
- [ ] Endpoint disconnects after receiving a job.
- [ ] Endpoint reconnects and resumes safely.
- [ ] Duplicate command delivery does not duplicate effects.
- [ ] Failed update is reported accurately.
- [ ] Reboot-required deployment follows configured policy.
- [ ] Emergency mitigation is dispatched and verified.
- [ ] Agent update succeeds and rolls back on failure.
- [ ] Administrator without permission is denied.
- [ ] Revoked device credential is rejected.

## 27. Scale and performance testing

Before production, agree numeric targets for fleet size and SLOs. Then test:

- [ ] Concurrent connected agent count.
- [ ] Heartbeat/event rate.
- [ ] Full inventory upload burst.
- [ ] Incremental inventory throughput.
- [ ] Fleet query latency by common filters.
- [ ] Dynamic group evaluation time.
- [ ] Job creation and target expansion time.
- [ ] Enqueue-to-dispatch latency.
- [ ] Dispatch-to-agent-ack latency.
- [ ] Worker throughput and queue lag.
- [ ] Live dashboard update latency.
- [ ] Database storage growth and index performance.
- [ ] Reconnect storm after network/server outage.
- [ ] Large campaign with controlled concurrency.
- [ ] Partial broker/database outage behavior.

Do not equate a low dispatch latency with endpoint installation completion. Record these as separate metrics.

## 28. Observability and operations

- [ ] Structured JSON logs across server, workers, and agents.
- [ ] Correlation IDs across UI/API/job/agent events.
- [ ] Metrics for API latency/errors, database health, queue depth, worker lag, connected agents, inventory freshness, job states, and updater health.
- [ ] Distributed tracing for key workflows.
- [ ] Alerts for queue backlog, database failure, certificate expiry, agent disconnect spikes, failed campaigns, and inventory staleness.
- [ ] Health/readiness endpoints for all services.
- [ ] Runbooks for common failures.
- [ ] Backup/restore and disaster recovery drills.
- [ ] Capacity planning and storage lifecycle.
- [ ] Security incident response and audit review.
- [ ] Document maintenance and upgrade windows.

**Deliverables:** `docs/TESTING.md`, `docs/OPERATIONS.md`, `docs/RUNBOOKS/`, dashboards and alert definitions.

---

# PART XII — RELEASE AND ACCEPTANCE

## 29. Release engineering

- [ ] Define versioning for server, API, agent, protocol, and inventory schema.
- [ ] Build reproducible release artifacts.
- [ ] Sign binaries and packages.
- [ ] Generate SBOMs.
- [ ] Scan dependencies and artifacts.
- [ ] Protect release pipeline credentials.
- [ ] Require review and test gates before release.
- [ ] Publish release manifests and checksums.
- [ ] Define staged agent rollout and rollback.
- [ ] Maintain supported/unsupported version policy.
- [ ] Publish release notes and known issues.

## 30. Documentation set

Create and maintain:

- [ ] `README.md` — concise product introduction and quick links.
- [ ] `docs/PRODUCT_OVERVIEW.md` — product definition and capabilities.
- [ ] `docs/PRODUCT_SCOPE.md` — supported scope and non-goals.
- [ ] `docs/ARCHITECTURE.md` — components, trust boundaries, data flows.
- [ ] `docs/BASELINE_AUDIT.md` — actual repository implementation audit.
- [ ] `docs/ROADMAP.md` — phases, dependencies, and status.
- [ ] `docs/API_SPECIFICATION.md` — REST API.
- [ ] `docs/AGENT_PROTOCOL.md` — agent message protocol.
- [ ] `docs/DATABASE_SCHEMA.md` — entities and relationships.
- [ ] `docs/INVENTORY_SPECIFICATION.md` — inventory fields and providers.
- [ ] `docs/PATCH_MANAGEMENT.md` — patch lifecycle and providers.
- [ ] `docs/JOB_ORCHESTRATION.md` — commands, delivery, retries, results.
- [ ] `docs/CONFIGURATION_MANAGEMENT.md` — desired state and policy model.
- [ ] `docs/SECURITY.md` — controls and threat model.
- [ ] `docs/INSTALLATION.md` — server and endpoint installation.
- [ ] `docs/CONFIGURATION.md` — settings and secrets.
- [ ] `docs/TESTING.md` — test matrix and commands.
- [ ] `docs/OPERATIONS.md` — backup, restore, monitoring, upgrades.
- [ ] `docs/RUNBOOKS/` — incident and maintenance procedures.
- [ ] `docs/CHANGELOG.md` — release history.
- [ ] `CONTRIBUTING.md` — development workflow.
- [x] `LICENSE` — initial Euryphos Personal Use License (EPUL) 1.0 published for material the copyright holder is authorized to license; legal review, file-level provenance, and prior third-party license obligations remain open.

---

# PART XIII — RECOMMENDED IMPLEMENTATION ORDER

The order below is dependency-driven. Avoid implementing broad dashboard features before the underlying data and APIs exist.

## Phase 0 — Baseline and project controls

**Work:** Sections 1–2.  
**Exit criteria:**

- Repository baseline documented.
- Current build/test status known.
- Supported initial scope agreed.
- Target scale and SLOs defined.
- Documentation conflicts reconciled.

## Phase 1 — Secure foundations

**Work:** Sections 3–8.  
**Exit criteria:**

- Architecture and trust boundaries approved.
- Versioned API and protocol contracts published.
- Administrator authentication and RBAC implemented.
- Device identity/enrollment lifecycle implemented.
- TLS, credential rotation/revocation, and audit foundations implemented.
- No default administrative credentials or unauthenticated management routes remain.

## Phase 2 — Data platform and inventory

**Work:** Sections 9–10.  
**Exit criteria:**

- Migrations and normalized data model implemented.
- Windows and Linux baseline inventory providers implemented for declared support matrix.
- Full and delta ingestion works.
- Inventory freshness and collection errors visible.
- Fleet search can locate devices by software name/version and key hardware/OS facts.

## Phase 3 — Agent connectivity and job control plane

**Work:** Sections 13 and 4.  
**Exit criteria:**

- Agent identity and reconnect behavior stable.
- Low-latency connected-agent dispatch implemented.
- Durable offline queue implemented.
- Job lifecycle, idempotency, retries, cancellation, and per-device results implemented.
- Load tests meet agreed connected-agent dispatch SLO.

## Phase 4 — Patch discovery and deployment

**Work:** Sections 14–16.  
**Exit criteria:**

- Windows and initial Linux patch providers detect applicable updates.
- Selected update installation works.
- Per-device outcomes and reboot requirements are reported.
- Campaign targeting, approvals, rollout controls, and verification work.
- Emergency response workflow demonstrated end-to-end.

## Phase 5 — Configuration and fleet operations

**Work:** Sections 17–18.  
**Exit criteria:**

- Versioned policies and assignments work.
- At least one useful Windows and Linux configuration provider works.
- Drift detection and remediation verification work.
- Typed remote action catalog is available with role and audit controls.

## Phase 6 — Enterprise console and reporting

**Work:** Sections 11–12 and 23–25.  
**Exit criteria:**

- Fleet overview is data-driven.
- Universal search and dynamic groups work.
- Device details expose supported inventory domains.
- Bulk operations preview targets and show live execution status.
- Reports, audit views, and authorized exports work.

Some console work can proceed in parallel, but must consume stable contracts and real backend data.

## Phase 7 — Packaging, updater, and production operations

**Work:** Sections 19–22 and 26–30.  
**Exit criteria:**

- Signed installers and safe upgrades work on supported platforms.
- Agent self-update supports verification and rollback.
- Server deployment, migrations, backup, restore, and rollback are documented and tested.
- Observability and runbooks are in place.
- Security review, penetration test, and performance tests pass.
- Release pipeline produces signed, traceable artifacts.

---

# PART XIV — FIRST CONCRETE SPRINTS

## Sprint 1: Audit and contracts

- [ ] Run and record Go formatting, tests, vetting, cross-compilation, shell linting, PowerShell parsing, installer VM tests, and release artifact signature verification.
- [ ] Map current routes and payloads.
- [ ] Map current agent request behavior.
- [ ] Identify broken or dead code.
- [ ] Create baseline architecture diagram.
- [ ] Agree first supported OS matrix and fleet scale.
- [ ] Create API v1 and agent protocol draft.
- [ ] Create initial issue backlog from this document.

**Sprint outcome:** We know what exists, what builds, and what protocol the system will implement.

## Sprint 2: Secure server and database foundation

- [ ] Introduce migrations.
- [ ] Remove insecure default admin token behavior.
- [ ] Add administrator authentication and authorization foundation.
- [ ] Add device credential lifecycle model.
- [ ] Add audit event model.
- [ ] Add API request validation and standardized errors.
- [ ] Add server integration test environment.
- [ ] Add secrets/configuration validation and fail-closed startup.

**Sprint outcome:** The server has a secure, migratable foundation.

## Sprint 3: Agent enrollment and heartbeat

- [ ] Define one agent configuration schema.
- [ ] Implement enrollment and approval flow.
- [ ] Implement credential storage and rotation path.
- [ ] Implement heartbeat and agent health.
- [ ] Add protocol compatibility tests.
- [ ] Test Windows and Linux service startup and reconnect.

**Sprint outcome:** Both agent platforms can securely enroll and remain connected.

## Sprint 4: Inventory vertical slice

- [ ] Implement OS/hardware inventory schema.
- [ ] Implement first Windows provider set.
- [ ] Implement first Linux provider set.
- [ ] Implement full inventory ingestion.
- [ ] Implement inventory freshness.
- [ ] Display device overview and hardware/OS detail.
- [ ] Add tests with representative VMs.

**Sprint outcome:** Administrators can see real hardware and OS inventory across both platforms.

## Sprint 5: Installed software and fleet search

- [ ] Implement Windows software inventory.
- [ ] Implement Linux package inventory.
- [ ] Normalize software identity/version.
- [ ] Build installed software view.
- [ ] Implement fleet query for product/version.
- [ ] Implement saved query and dynamic group foundation.

**Sprint outcome:** An operator can answer “which machines have product X version Y?” across the estate.

## Sprint 6: Job control plane

- [ ] Implement job/campaign tables.
- [ ] Implement durable queue/worker.
- [ ] Implement agent command receive/ack/result.
- [ ] Implement idempotency and retry.
- [ ] Implement live job status.
- [ ] Test online/offline and duplicate delivery.

**Sprint outcome:** A safe, observable command can be dispatched to a selected fleet and its outcome tracked.

## Sprint 7: Patch discovery

- [ ] Implement Windows update scan provider.
- [ ] Implement initial Linux package update provider.
- [ ] Store update applicability per device.
- [ ] Display fleet patch status.
- [ ] Add tests for stale metadata and provider failures.

**Sprint outcome:** Operators can see missing updates across the fleet.

## Sprint 8: Patch deployment

- [ ] Implement selected update installation.
- [ ] Implement per-device execution and verification.
- [ ] Implement reboot policy.
- [ ] Implement deployment rings and concurrency controls.
- [ ] Implement failure/retry/reporting.
- [ ] Demonstrate emergency campaign flow.

**Sprint outcome:** An operator can safely patch a targeted fleet and prove the result.

These sprint boundaries are illustrative. Actual sprint length and sequencing should be set after the baseline audit and capacity assessment.

---

# PART XV — RELEASE ACCEPTANCE CHECKLIST

Euryphos must not be called enterprise-ready until the following are demonstrated in a repeatable test environment.

## Inventory and visibility

- [ ] Supported endpoints enroll securely.
- [ ] Hardware and OS inventory is complete for the published support matrix.
- [ ] Installed software and versions are collected and normalized.
- [ ] Services and other enabled inventory domains are collected.
- [ ] Inventory freshness and missing data are visible.
- [ ] Fleet search can identify every device matching supported inventory queries.
- [ ] Access to sensitive inventory is permission-controlled and audited.

## Fleet operations

- [ ] A command can target one device, a static group, a dynamic group, or all eligible devices.
- [ ] Target set is previewed and recorded.
- [ ] Connected agents receive commands within the agreed dispatch SLO.
- [ ] Offline agents receive eligible unexpired jobs after reconnect.
- [ ] Duplicate delivery does not cause duplicate unsafe execution.
- [ ] Every target has an independently visible final state.
- [ ] Partial success is represented honestly.

## Patching and configuration

- [ ] Patch discovery works on all declared supported platforms.
- [ ] Selected updates can be installed.
- [ ] Installation is verified by post-action state.
- [ ] Reboot policy is enforced.
- [ ] Emergency campaign can be created and expedited under policy.
- [ ] Configuration changes are versioned, scoped, and verified.
- [ ] Failures and exceptions are visible and reportable.

## Security and governance

- [ ] No default credentials or public unauthenticated management routes.
- [ ] Administrator MFA/SSO and RBAC operate as designed.
- [ ] Device credentials can be rotated and revoked.
- [ ] Commands and artifacts are authenticated and verified.
- [ ] High-impact operations require configured approval.
- [ ] Audit records cover all privileged operations.
- [ ] Security testing has no unresolved critical/high findings.

## Reliability and operations

- [ ] Agreed scale tests pass.
- [ ] Queue backlog and agent connectivity are observable.
- [ ] Backup and restore have been demonstrated.
- [ ] Server and agent upgrades have rollback paths.
- [ ] Supported installers work from clean machines.
- [ ] Operational runbooks exist.
- [ ] API and agent compatibility policy is published.
- [ ] Release artifacts are signed and traceable.

---

# Final implementation rule

Build Euryphos as an enterprise control plane, not as a collection of dashboard buttons that set flags.

The core loop must be reliable and measurable:

**Discover → Normalize → Search → Target → Approve → Dispatch → Execute → Verify → Audit → Report**

Every major feature should fit this loop, use the shared identity and policy model, and expose its outcome. The project is complete only when this loop works end-to-end across the supported fleet at the agreed scale and security level.
