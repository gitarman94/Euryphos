# Euryphos — Enterprise Systems Management Platform
## Product Vision, Scope, Capabilities, and Target Operating Model

**Document status:** Target-state product definition  
**Applies to:** Euryphos server, management console, endpoint agents, deployment services, integrations, and operational tooling  
**Purpose:** Define what Euryphos is intended to become. This is a target-state specification, not a claim that the current repository already implements these capabilities.

---

## 1. Product definition

Euryphos is an enterprise systems management (ESM) platform for discovering, inventorying, monitoring, configuring, securing, patching, and operating an organization's endpoint and server estate from one central management plane.

The platform must provide administrators with a current, searchable, fleet-wide view of managed systems. It must collect and present detailed hardware, operating-system, software, configuration, service, process, security, patch, and operational information, subject to platform permissions, collection policy, privacy requirements, and technical availability.

Euryphos must support actions against one device, selected devices, dynamic device groups, static groups, or the entire managed estate. Administrators must be able to initiate urgent actions—such as deploying an emergency security fix, changing a configuration, stopping a vulnerable service, or triggering an inventory refresh—without manually connecting to each endpoint.

Euryphos is not merely a patch dashboard or a script launcher. It is a controlled, auditable, policy-driven management system with reliable endpoint execution, fleet-scale orchestration, and verifiable outcomes.

### 1.1 Mission

Give authorized IT and security operators a trustworthy, near-real-time operational picture of their managed estate and the ability to execute approved changes at scale, safely and measurably.

### 1.2 Product principles

1. **Fleet first:** Every capability must work for a single endpoint and scale to groups and the whole estate.
2. **Inventory is foundational:** Management decisions must be based on detailed, timestamped, queryable endpoint facts.
3. **Fast dispatch, honest completion:** Commands should be dispatched promptly, but the product must distinguish dispatch, receipt, execution, and verified completion. No system can guarantee a physical endpoint action completes in milliseconds.
4. **Secure by default:** Every identity, administrator, agent, command, package, and update must be authenticated and authorized.
5. **Observable outcomes:** Every operation has a lifecycle, result, timestamps, logs, and audit record.
6. **Resilient by design:** Offline endpoints, partial failures, retries, duplicate delivery, server restarts, and network partitions are normal operating conditions.
7. **Safe fleet operations:** Broad changes require targeting previews, impact estimates, deployment controls, and recovery strategies.
8. **Cross-platform consistency:** Windows and Linux should expose consistent management concepts while using native platform mechanisms.
9. **API-driven:** The web console, automation, and integrations use the same documented service APIs.
10. **Extensible:** New endpoint platforms, inventory providers, action types, and integrations should not require redesigning the core.

---

## 2. Target users and operating environments

### 2.1 User roles

- **Platform administrator:** Configures the Euryphos service, integrations, identity, retention, and global policies.
- **Endpoint administrator:** Manages devices, inventory, software, configurations, and routine actions.
- **Patch operator:** Plans, approves, deploys, and monitors patch campaigns.
- **Security operator:** Responds to vulnerabilities and incidents, creates emergency campaigns, and verifies remediation.
- **Read-only auditor:** Reviews inventory, compliance, deployment history, and audit evidence.
- **Approver / change manager:** Reviews high-impact changes and authorizes deployments according to policy.
- **Automation identity:** Executes narrowly scoped, non-interactive API operations.

The platform must support role-based access control (RBAC), least privilege, separation of duties, and auditable privileged actions.

### 2.2 Managed estate

The initial implementation should prioritize:

- Windows workstations and servers.
- Linux servers and workstations, with distribution-specific capabilities.
- Virtual machines and cloud-hosted instances where an agent can be installed.
- Physical servers and endpoints.
- A native macOS endpoint agent is a planned later deliverable, alongside network appliances and other managed asset types.

The exact supported Windows versions, Linux distributions, architectures, and package managers must be published in a support matrix and tested before being declared supported.

---

## 3. Fleet-wide inventory and system intelligence

A central requirement is the ability to inspect the estate without opening each device individually.

### 3.1 Hardware inventory

Collect, where supported:

- Manufacturer, model, product name, serial number, asset tag, UUID, BIOS/UEFI vendor and version.
- CPU model, architecture, sockets, cores, logical processors, virtualization capabilities.
- Installed memory, available memory, memory modules and capacities where exposed.
- Physical and logical storage devices, capacity, free space, filesystem, mount points, health indicators where available.
- Network interfaces, MAC addresses, addresses, link state, speed, DNS configuration, routes, and adapter type.
- Graphics and other relevant hardware devices.
- TPM presence and version, Secure Boot state, and firmware security posture where available.

Hardware facts must include collection time, source/provider, and an indication when a field is unavailable or permission-restricted. Sensitive identifiers must be access-controlled and retention-limited where appropriate.

### 3.2 Operating-system and platform inventory

Collect:

- OS family, edition, version, build, kernel, architecture, install date, hostname, domain/workgroup or directory identity where permitted.
- Boot time and uptime.
- Time zone and clock synchronization status where available.
- System roles and virtualization/container indicators.
- Localized platform details required to interpret update and package state.
- Agent version, agent health, last successful inventory, last check-in, and communication status.

### 3.3 Installed software inventory

Installed software is a first-class data domain, not a text field.

For each discovered application/package, capture as available:

- Product/package name and normalized identity.
- Publisher/vendor.
- Installed version and architecture.
- Install date and install location.
- Package identifier, product code, package manager, repository/source.
- Install scope (machine/user) where discoverable.
- Detection source and confidence.
- First observed and last observed timestamps.
- Uninstall metadata where safely available.
- Whether the software is managed, prohibited, approved, vulnerable, unsupported, or end-of-life according to configured policy and intelligence sources.

Windows inventory should account for machine-wide and relevant per-user software registrations, MSI and non-MSI applications, provisioned applications, and Windows package ecosystems as supported. Linux inventory should use native package databases and may separately discover manually installed software, containers, language runtimes, and application bundles where configured.

The product must normalize software identities across versions and platforms while preserving the original platform-specific identifiers.

### 3.4 Services, processes, and scheduled execution

Where authorized and technically supported, collect:

- Installed services/daemons, startup mode, current state, account identity, executable path, and dependencies.
- Running processes, PID, executable, owner, start time, command-line visibility according to policy, and resource usage snapshots.
- Scheduled tasks, cron entries, systemd timers, and other platform schedulers.
- Listening ports and network sockets as a controlled, configurable inventory feature.
- Drivers, kernel modules, and relevant system extensions.

Process and command-line collection can expose credentials or personal information. It must be configurable, access-controlled, minimized, and disabled by default unless the organization explicitly enables it.

### 3.5 Configuration and security posture

Collect configurable posture data such as:

- Local users and groups, account state, and privileged group membership.
- Firewall state and selected policy.
- Disk encryption status.
- Secure Boot and TPM status.
- Antivirus/endpoint protection product and status where exposed.
- OS security settings and policy values.
- Remote access configuration.
- Local policy and relevant registry/configuration values.
- Certificate inventory metadata and expiry, without exporting private keys.
- Vulnerability exposure and remediation state when supported by trusted data sources.

The platform should support customer-defined configuration baselines and compliance rules. A baseline result must show expected value, observed value, evaluation time, and evidence.

### 3.6 Inventory collection model

Inventory must support:

- Full inventory scans.
- Incremental/delta scans.
- Event-triggered updates where native events are reliable.
- Scheduled scans with configurable frequency and jitter.
- On-demand refresh for a device or target group.
- Collection profiles controlling data categories and detail.
- Versioned schemas and backward-compatible ingestion.
- Per-category collection status and error reporting.
- Data freshness indicators in every relevant dashboard view.

A dashboard must never present stale information as if it were live. Every inventory record and summary must expose its last observed time.

---

## 4. Patch, vulnerability, and software lifecycle management

### 4.1 Patch discovery

The agent must discover applicable updates through supported native platform mechanisms and package managers.

For each update, capture as available:

- Stable update/package identity.
- Title, description, vendor, version, release date.
- Platform and architecture applicability.
- Security classification and severity.
- CVE identifiers and vendor advisory references.
- Download/source metadata and package dependencies.
- Reboot requirement.
- Supersedence and dependency relationships.
- Current status: applicable, downloaded, staged, installing, installed, failed, superseded, blocked, or not applicable.
- Detection timestamp and evidence source.

Euryphos must distinguish OS updates, third-party application updates, drivers/firmware, and internally distributed software. These may require different providers and deployment policies.

### 4.2 Emergency and zero-day response

The platform must support emergency remediation workflows:

1. Identify affected assets using inventory, OS/software versions, vulnerability data, and dynamic queries.
2. Preview the exact target set and show online/offline counts, exclusions, and confidence.
3. Create an emergency campaign with priority, owner, reason, approval policy, and rollback/mitigation plan.
4. Select the remediation: vendor patch, package update, configuration mitigation, service disablement, isolation action, or approved script/package.
5. Dispatch to eligible endpoints through the command/orchestration system.
6. Track per-device receipt, start, progress, result, and verification.
7. Retry transient failures under bounded policy.
8. Re-evaluate exposure and report remaining vulnerable devices.
9. Preserve evidence and a complete audit trail.

A zero-day workflow must not imply that Euryphos can create a vendor patch where none exists. It must support available mitigations and approved compensating controls while tracking the true remediation state.

### 4.3 Patch deployment policies

Support:

- Single update, selected updates, all approved updates, and policy-based updates.
- Device, group, dynamic query, tag, site, environment, OS, and business-unit targeting.
- Deployment rings (pilot, early adopters, broad production, critical systems).
- Maintenance windows and blackout windows.
- Bandwidth-aware and peer/cache-aware distribution as a later scalable capability.
- Pre-deployment checks and dependency resolution.
- Concurrency limits, rate limits, and staged rollout percentages.
- Reboot policies: never automatically reboot, prompt, deadline, maintenance-window reboot, or immediate emergency reboot subject to explicit authorization.
- Deferral, exclusion, hold, and exception workflows.
- Retry policies and failure thresholds.
- Rollback or compensating action where the package/platform supports it.
- Compliance deadlines and grace periods.

### 4.4 Third-party software lifecycle

The longer-term product should manage approved third-party application catalogs, software deployment, upgrades, uninstallations, and vulnerable-version remediation. This requires package acquisition, provenance, signature/hash verification, licensing considerations, and platform-specific deployment providers.

---

## 5. Configuration and endpoint operations

Euryphos must be able to manage more than patches.

### 5.1 Configuration management

Administrators should be able to define versioned configuration policies and apply them to targets. Examples include:

- Registry and policy values on Windows.
- Configuration files and system settings on Linux.
- Service state and startup configuration.
- Firewall rules.
- Local group membership.
- Scheduled task definitions.
- Package presence and version.
- Security baseline settings.

Each configuration action must declare its desired state, preconditions, validation method, idempotency behavior, and remediation/rollback strategy where possible.

### 5.2 Remote actions

A governed action catalog should include, as appropriate:

- Refresh inventory.
- Run patch scan.
- Install approved updates.
- Install or remove approved software.
- Start, stop, restart, or reconfigure a service.
- Apply or remove a configuration policy.
- Run a signed, approved automation package.
- Collect diagnostic bundle.
- Restart or shut down an endpoint.
- Trigger agent repair or upgrade.
- Request a vulnerability reassessment.

Arbitrary unrestricted remote shell access is not the default management model. If a script or command execution feature is introduced, it must use strong authorization, signed content, scoped permissions, execution limits, output controls, and immutable auditing.

### 5.3 Desired-state management

The platform should evolve from one-time commands toward desired-state policies. An endpoint periodically evaluates assigned policies, detects drift, applies permitted corrections, and reports compliance. Policies need versioning, precedence, conflict detection, assignment scopes, and exceptions.

---

## 6. Real-time and near-real-time fleet operations

### 6.1 What “milliseconds” means operationally

Euryphos should make administrator actions feel immediate. The server should accept an action, validate it, persist it, and begin dispatching it with low latency. The interface should show the operation immediately and update its state as endpoint acknowledgements arrive.

However, a remote endpoint cannot be guaranteed to receive or complete an action in milliseconds. Network latency, endpoint sleep/offline state, agent polling or connection state, OS update engines, package downloads, installation duration, and required reboots all affect completion.

The engineering target is therefore:

- **Immediate UI acknowledgement:** show that the request was accepted or rejected as soon as server validation completes.
- **Low-latency dispatch:** push to connected agents using persistent bidirectional connections or an equivalent event transport; do not rely solely on long polling intervals.
- **Fast acknowledgement:** agents acknowledge receipt promptly when connected.
- **Asynchronous execution:** actions execute under endpoint-specific safety and concurrency controls.
- **Live progress:** stream status changes to the console.
- **Honest completion:** show verified success only after endpoint evidence confirms the desired outcome.
- **Offline continuity:** queue work durably for disconnected agents, with expiry and policy-aware delivery when they reconnect.

The console must clearly distinguish `accepted`, `queued`, `dispatched`, `received`, `running`, `succeeded`, `failed`, `cancelled`, `expired`, and `verification_failed`.

### 6.2 Fleet-wide command orchestration

A command must be represented as a durable job with:

- Unique job and operation IDs.
- Requesting administrator or automation identity.
- Action type and versioned payload.
- Target selector and resolved target snapshot.
- Creation, approval, dispatch, receipt, start, completion, and verification timestamps.
- Priority, deadline, expiry, and retry policy.
- Concurrency and rate limits.
- Idempotency key and deduplication behavior.
- Per-device execution records.
- Structured output, error classification, and evidence.
- Cancellation and pause controls where safe.
- Audit and change references.

A fleet-wide action must not be implemented as a single unbounded loop that blocks an HTTP request. It must be processed by durable workers and a scalable messaging/dispatch layer.

---

## 7. Management console and operator experience

The console is the primary operational surface and must be designed for large estates.

### 7.1 Fleet overview

Show:

- Total discovered, enrolled, approved, active, inactive, and offline endpoints.
- OS and version distribution.
- Hardware and software inventory summaries.
- Patch compliance and outstanding security updates.
- Vulnerable and unsupported systems.
- Agent health and version drift.
- Deployment activity and failures.
- Configuration compliance and drift.
- Inventory freshness and collection failures.
- Geographic/site/business-unit distribution where configured.

All dashboard counts must be filterable and drill down to the underlying endpoint set.

### 7.2 Universal device search

Operators must be able to search and filter across the estate by:

- Hostname, device ID, serial, asset tag, UUID, IP, MAC.
- OS family/version/build.
- Hardware manufacturer/model/CPU/RAM/storage.
- Installed application, publisher, and version.
- Service/process/configuration facts where collected.
- Patch/CVE/advisory state.
- Agent version and last-seen state.
- Tags, groups, location, owner, environment, and custom attributes.
- Compliance status and deployment history.

Queries should support boolean conditions, saved searches, dynamic groups, and export under permission controls.

### 7.3 Device detail

A device page should provide a complete, navigable record:

- Summary and live agent status.
- Hardware and OS.
- Software inventory.
- Updates and vulnerabilities.
- Services and scheduled tasks.
- Processes and resource snapshots where enabled.
- Network configuration.
- Security posture and baselines.
- Configuration policies and drift.
- Active and historical jobs.
- Events, logs, and diagnostic evidence.
- Agent configuration and version.
- Audit history.

### 7.4 Bulk operations

From any filtered device list, authorized operators should be able to select all or a subset and initiate supported actions. Before execution, the UI must show the resolved target count, excluded devices, expected impact, approval requirements, and rollout controls.

### 7.5 Live operation view

Provide a live job page with per-device state, progress, failure reason, retry state, timestamps, filters, and export. Fleet summaries must not hide partial failure.

---

## 8. Core architecture

The target architecture should separate the web application from the asynchronous control plane.

### 8.1 Logical components

1. **Web console:** Operator UI, dashboards, search, reporting, policy authoring, and approvals.
2. **API gateway / application API:** Versioned authenticated API for UI, agents, and integrations.
3. **Identity and authorization:** Administrator SSO/MFA integration, RBAC, service identities, and authorization policy.
4. **Device registry:** Enrollment, identity, metadata, lifecycle, and trust state.
5. **Inventory ingestion:** Schema validation, normalization, deduplication, history, and query indexing.
6. **Policy engine:** Desired state, targeting, evaluation, and compliance.
7. **Job orchestration service:** Durable command creation, targeting, approvals, scheduling, retries, cancellation, and result aggregation.
8. **Message transport:** Low-latency agent communication and durable offline delivery.
9. **Artifact/package service:** Trusted package/script storage, metadata, signing, hash verification, and distribution.
10. **Patch intelligence connectors:** Vendor/package metadata, advisories, CVEs, and update applicability.
11. **Event and audit service:** Security events, operational events, and immutable audit records.
12. **Reporting/analytics:** Fleet metrics, compliance, historical trends, and exports.
13. **Endpoint agent:** Enrollment, secure communication, inventory providers, policy evaluation, action execution, local state, and self-update.
14. **Worker services:** Background ingestion, dispatch, inventory processing, reporting, and scheduled jobs.

### 8.2 Existing repository baseline

The original prototype archive contained a Flask/Python server, SQLAlchemy models, server templates, shell/PowerShell setup scripts, and a Rust client. The active implementation baseline has since been converted to Go for both server and agent. The old installers and dashboard templates were not yet migrated and must not be treated as working Go deliverables.

The current implementation is a prototype foundation, not the target architecture. The present data model is much narrower than enterprise inventory and orchestration require. The current API has basic health, registration, check-in, approval, update, and command-related routes; it does not yet provide the full control plane described here.

The current Go baseline uses SQLite in a local file. Legacy Python/Rust files and installers are historical reference material only; they are not runtime dependencies. The support matrix and installer acceptance requirements are maintained in separate documentation.

### 8.3 Recommended technology direction

The active implementation direction is:

- Use Go for the management server and endpoint agents; do not reintroduce Python or Rust as first-party runtime requirements.
- Use embedded SQLite as the initial persistent store to simplify installation and backup. Apply WAL, indexes, bounded connection pools, migration controls, and measured load testing. SQLite serializes writes, so publish realistic capacity limits and do not claim it is universally faster than client/server database engines.
- Introduce a durable message broker/stream and worker system only when measured orchestration requirements justify them.
- Use native Windows and Linux management/update interfaces and implement a native macOS agent as a later roadmap item.
- Use versioned, repeatable schema migrations and tested backup/restore rather than relying solely on implicit runtime schema changes.
- Use a supported identity provider for enterprise SSO where practical.
- Add a search/indexing component only when query scale and latency measurements justify it.

Technology choices must be documented in architecture decision records and validated with load tests.

---

## 9. Security, trust, and governance

Euryphos has privileged control over endpoints. Compromise of the management plane could compromise the entire estate. Security is therefore a primary product capability.

Required controls include:

- TLS for all network communication; modern cipher and certificate lifecycle management.
- Unique per-device identity and revocable credentials.
- Secure enrollment, approval, re-enrollment, rotation, and decommissioning.
- Short-lived credentials or certificate-based agent identity where feasible.
- Administrator SSO, MFA, RBAC, session protection, and least privilege.
- Separation of duties and approvals for high-impact operations.
- Authorization evaluated for every API and every action.
- Signed and hash-verified agents, packages, policies, and executable automation.
- Secrets stored using an appropriate secret-management mechanism; never in source control or templates.
- CSRF protection for browser sessions, restrictive CORS, secure cookies, and input validation.
- Rate limiting, abuse protection, replay protection, and request size limits.
- Immutable or tamper-evident audit records.
- Secure logging that avoids secrets and unnecessary personal data.
- Backup, restore, disaster recovery, and key recovery procedures.
- Dependency, container, build pipeline, and release artifact security.
- Threat modeling, penetration testing, and vulnerability disclosure process.

No endpoint action should be trusted merely because it came from the server. The agent must validate message authenticity, schema/version, target identity, expiry, replay/idempotency state, local policy, and action permissions before execution.

---

## 10. Reliability, scale, and performance

The product must be designed and tested against an explicit scale target. Before selecting final infrastructure, define expected endpoint counts, check-in rates, inventory payload sizes, concurrent deployments, event volume, and retention periods.

Required engineering properties:

- Durable job and message persistence.
- At-least-once delivery with idempotent execution and deduplication; do not assume exactly-once network delivery.
- Backpressure and bounded concurrency.
- Retry with exponential backoff and jitter.
- Dead-letter handling and operator-visible poison messages.
- Agent reconnect and resume behavior.
- Offline queues with expiry and policy validation.
- Database indexing, partitioning/retention strategy, and query performance testing.
- Horizontal scaling of stateless API and worker components where needed.
- Health checks, metrics, tracing, structured logs, and alerting.
- Tested backup/restore and disaster recovery.
- Graceful server and agent upgrades.
- Compatibility policy for agent/API versions.

Performance targets must be set in an SLO document. Example targets should be treated as goals to validate, not promises: UI request acknowledgement latency, connected-agent dispatch latency, agent acknowledgement latency, inventory query latency, and fleet job completion rates. Installation completion time is workload- and endpoint-dependent.

---

## 11. Data governance and privacy

“Every bit of system information” must be interpreted as comprehensive enterprise asset telemetry, not indiscriminate collection of personal content.

Euryphos should collect the maximum operationally useful information that is lawful, authorized, technically available, and configured by the organization. It should not collect user document contents, passwords, private keys, browser contents, personal communications, or other personal content as ordinary inventory.

The platform needs:

- Data classification and collection profiles.
- Role-based access to sensitive fields.
- Configurable retention and deletion.
- Encryption in transit and at rest.
- Data provenance and freshness.
- Audit of sensitive inventory access and exports.
- Tenant/organization boundaries if multi-tenancy is supported.
- Export controls and privacy-aware defaults.

---

## 12. Integrations and extensibility

Plan for integrations with:

- Enterprise identity providers and directory services.
- SIEM and security analytics platforms.
- IT service management/change management systems.
- Vulnerability scanners and threat intelligence sources.
- Email, chat, and incident alerting systems.
- Software/package repositories and artifact registries.
- Cloud inventory and asset systems.
- Backup and endpoint security tools.

Integrations should use documented APIs, scoped credentials, event/webhook delivery, and versioned schemas.

---

## 13. Product boundaries and non-goals

Euryphos should not claim to replace every specialized enterprise platform on day one. Initial focus is endpoint inventory, patching, configuration, and controlled remote operations.

Potential later capabilities—remote desktop, full MDM, network configuration management, software license optimization, vulnerability scanning, EDR, and service desk—must be separately scoped. Euryphos can integrate with specialist systems rather than reimplement them prematurely.

---

## 14. Definition of a successful product

Euryphos is successful when an authorized operator can:

1. Enroll and govern a supported endpoint securely.
2. See a detailed, fresh, searchable inventory of the endpoint.
3. Query the entire fleet for systems matching a condition, including software/version and configuration facts.
4. Create a targeted patch or configuration campaign from that query.
5. Preview targets and apply approvals, rings, concurrency limits, and maintenance policies.
6. Dispatch work promptly to connected endpoints and durably queue it for offline endpoints.
7. Observe receipt, execution, progress, failures, retries, and verified results per device.
8. Re-query the fleet to prove the desired remediation or configuration state.
9. Export evidence and audit history.
10. Operate at the agreed fleet scale with documented reliability, security, and performance SLOs.

The key product outcome is not simply “a command was sent.” It is **the organization can see what exists, decide what must change, safely apply that change across the estate, and prove what happened.**
