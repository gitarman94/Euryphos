# Device management UI and complete device record specification

**Status:** Required product specification; not yet implemented.  
**Scope:** Euryphos Server UI, management API, persistence, and endpoint-agent data contract.  
**Related project:** [Euryphos-Agent](https://github.com/gitarman94/Euryphos-Agent) collects endpoint-side facts and submits them to the server.

This document is the acceptance specification for the Devices page and each individual device's detail page. A field must not be shown as populated unless a trusted source supplied it. Unsupported, unavailable, unreported, stale, and intentionally disabled values must be distinguishable.

## 1. Devices page

Provide a first-class **Devices** navigation item. It opens a searchable, sortable, paginated table with one row per enrolled device. The hostname is a link to that device's full detail page, for example `/devices/{device-id}`.

### Required default columns

| Column | Meaning |
|---|---|
| Hostname | Clickable hostname opening the device details page |
| Local IP | Most recently observed primary/local address; show multiple addresses in expanded row or details |
| Connecting IP | Source address observed by the Euryphos server for the latest authenticated connection |
| Operating system | Type/name and version |
| Agent version | Installed Euryphos-Agent version |
| Connectivity | Online or offline, calculated by the server |
| Last connected | Most recent successful authenticated agent contact |
| Last boot | Last OS-reported boot time; optionally show derived uptime |

The list must separately identify enrollment/administrative state (pending, approved, revoked) and connectivity state (online, offline). A revoked device must not appear online merely because an old heartbeat exists.

### List behavior

- Search by hostname, FQDN, domain, serial number, local IP, connecting IP, MAC address, OS, agent version, and installed software.
- Filters: online/offline, pending/approved/revoked, OS family/distribution/version, device type/chassis type, agent version, domain, OU path, site/location when available, and inventory freshness.
- Sort by every displayed column, including last connected and last boot.
- Paginate on the server; do not load the entire fleet into browser memory.
- Allow users to select one or more rows for approved fleet operations, subject to authorization and confirmation policies.
- Make offline devices, stale inventory, pending enrollment, and revoked status visually distinguishable without conflating them.
- Show last inventory observation time and per-category freshness.
- Support user-configurable columns and saved filters after role-based UI authorization is implemented.
- Do not rely on browser local time for canonical timestamps; store UTC and render in the operator's selected timezone.

## 2. Device details page

Selecting a hostname opens a dedicated page for that device. The page should use an overview header plus grouped tabs/sections. It must show when each inventory category was observed, when the server received it, and whether the information is current, stale, unsupported, unavailable, or privacy-disabled.

### 2.1 Identity and directory

- Hostname and FQDN.
- DNS domain / directory domain, where applicable.
- Active Directory domain-join state, where applicable.
- Organizational Unit (OU) distinguished path, when domain joined and available to the agent's permissions.
- Device ID and enrollment state.
- Serial number.
- SMBIOS system UUID / GUID.
- Device type (for example workstation, laptop, desktop, server, virtual machine, tablet) and chassis type, with the source and normalized value.
- Current logged-in user and, where available, active interactive sessions. Do not assume one logged-in user on multi-session systems.

### 2.2 Location and time

- Operating-system timezone identifier and UTC offset.
- Optional GPS latitude, longitude, observation timestamp, and accuracy radius when the platform provides it.
- Manually assigned site/building/room metadata may be shown separately from sensor-derived location.

**GPS privacy is a mandatory client-side control:**

1. GPS collection is disabled by default on every new agent installation.
2. The client configuration exposes a clear local setting, e.g. `privacy.gps_collection_enabled: false`, controlled by a local administrator.
3. When the local setting is false, the agent must not call location APIs, collect coordinates, or upload GPS data.
4. A server-side policy or remote command must never override a local disabled setting. Local disable is authoritative.
5. If locally enabled, collection still requires the operating system's location permission and platform support.
6. No continuous tracking. Read coordinates only as part of an explicitly configured inventory observation cadence.
7. The UI must show “Disabled on client”, “Permission denied”, “Unsupported”, or “Not reported” distinctly; never display a fabricated zero coordinate.
8. Record the GPS observation timestamp and accuracy when returned. Do not infer an address or city unless a separately approved geocoding feature is added.
9. The client must not silently re-enable collection after upgrade, repair, config migration, or server reconnect.

Timezone collection is a separate non-GPS system fact. Disabling GPS must not suppress timezone reporting.

### 2.3 Hardware

- Manufacturer.
- Model.
- Device type and chassis type.
- CPU model, vendor, core/thread counts, and architecture.
- Total physical RAM, available/free RAM when reliable, and memory type/speed/capacity per module when supported.
- GPU: vendor, model/type, architecture/generation, driver version, dedicated/shared memory where available.
- DPU (Data Processing Unit): vendor, model/type, architecture, firmware/driver version when present. DPU is separate from GPU; do not substitute one field for the other.
- BIOS/UEFI vendor and version, release date where available, and boot mode.
- SMBIOS UUID/GUID.
- Battery presence and battery health/capacity/cycle count where supported.
- Physical disks and volumes, capacity, used space, free space, filesystem, mount point/drive letter, and encryption state where available.
- Hardware collection source and observation timestamp.

### 2.4 Operating system

- OS type/family (Windows, Linux, macOS).
- Distribution or product name and edition.
- Human-readable version and normalized version number.
- Build number/revision where applicable.
- Kernel version on Linux/macOS.
- OS architecture (x86_64/amd64, arm64, etc.).
- OS installation date/time when the platform exposes a reliable value; label inferred/unavailable values rather than inventing a timestamp.
- Last boot timestamp and derived uptime.
- Current OS lifecycle/support state when a reliable product lifecycle source is integrated.

### 2.5 Storage

Show all physical disks and mounted volumes, including:
- Device/volume identifier, model where available, filesystem, mount point or drive letter.
- Total capacity, used capacity, and free capacity in bytes and human-readable units.
- Encryption status where supported.
- Observation time and collection errors.

The overview may show aggregate free space, but the details page must preserve per-volume values.

### 2.6 Network and connectivity

Support multiple interfaces and addresses. For each interface, record:

- Interface name, description, type (Ethernet, Wi-Fi, cellular, VPN, loopback, virtual, other), operational/link state, and speed when available.
- Local IPv4/IPv6 addresses and prefix lengths.
- MAC address.
- DHCP enabled/disabled state and DHCP server where available.
- DHCP lease start/expiry timestamps when available.
- Default gateway(s).
- DNS server list and search domains.
- Connection type; Wi-Fi SSID may be an optional separately controlled field because it can reveal a person's location or network affiliation.
- Interface-specific observation time.

Connection statistics should include:
- Last successful connection time and first successful connection time.
- Last heartbeat/check-in and last inventory submission.
- Connection attempts, successful reconnects, failures, and last error category.
- Bytes sent/received and latency/round-trip sample when measured.
- Agent-reported network state and server-observed connection state, kept separate.

**Connecting IP** means the remote source address observed by the Euryphos server on an authenticated request. If deployed behind a reverse proxy, only honor forwarded client-IP headers from explicitly configured trusted proxies. Never trust an arbitrary agent-supplied `connecting_ip` value as authoritative.

### 2.7 Presence, status, and activity

- Connectivity status: online/offline, calculated from server-side last-contact time and a configurable presence threshold.
- Administrative lifecycle state: pending/approved/revoked, displayed separately.
- First connected: first successful authenticated connection; keep enrollment-created time as a separate value.
- Last connected: latest successful authenticated agent request.
- Last boot and derived uptime.
- Current logged-in user/session information where the platform exposes it.
- Last agent version and last agent update time.
- Last job result and recent activity timeline, once job/audit APIs are implemented.

The online threshold must be configurable and take heartbeat interval, expected network sleep, and missed check-ins into account. It must not be hard-coded into the UI. A device that sleeps or is disconnected should become offline without being marked revoked.

### 2.8 Agent and certificate identity

- Euryphos-Agent/client version, build identifier, and update channel.
- Agent service state and last start time.
- Device identity certificate type (when certificate-based agent identity is implemented), subject, issuer, serial/thumbprint, not-before, expiration time, renewal state, and revocation state.
- Server HTTPS/TLS certificate information may be shown separately as server connection diagnostics; do not conflate it with the device identity certificate.
- Never collect or display private keys. Store only public certificate metadata and references to protected key material.

The current agent uses a bearer-token enrollment/device credential. Certificate fields must display “Not configured” until certificate-based identity actually exists; do not label the bearer token as a certificate.

## 3. Data quality, normalization, and permissions

- Every fact should have `observed_at`, server `received_at`, source/provider, and optional error/status metadata.
- Represent unsupported/unavailable/not collected/permission denied/privacy disabled as explicit states, not empty strings that look like collection failures.
- Preserve raw provider data only where it is necessary for diagnostics; normalize commonly queried facts into indexed columns/records.
- Keep multiple NICs, IPs, disks, GPUs, batteries, and user sessions as collections rather than single overwritten fields.
- Retain historical snapshots for fields where change history is useful (identity, network, OS, hardware, software, certificates, and location if enabled).
- Define per-role permissions for viewing serial numbers, SMBIOS GUIDs, IP/MAC information, logged-in users, GPS coordinates, and inventory exports.
- GPS coordinates and current-user data are sensitive. Apply access control, audit reads/exports, retention limits, and deletion rules.
- Do not collect passwords, private keys, authentication tokens, or unrelated personal files.

## 4. Server API and storage requirements

The existing `GET /api/v1/devices` endpoint currently returns only a small device summary. The following are required extensions and must be versioned/tested:

- `GET /api/v1/devices`: paginated list with filter/sort parameters and the required row summary.
- `GET /api/v1/devices/{id}`: full normalized device record and category freshness metadata.
- `GET /api/v1/devices/{id}/inventory`: inventory categories, current snapshot, and optional history pagination.
- `GET /api/v1/devices/{id}/activity`: recent heartbeat, connection, agent, enrollment, and job events.
- A versioned inventory payload schema shared between Euryphos and Euryphos-Agent.

Persist frequently filtered facts as normalized indexed columns/tables. Keep extensible provider-specific facts in versioned JSON inventory records with schema validation. Use SQLite migrations; do not rely on creating ad-hoc columns from arbitrary client input.

For every ingestion request, the server records its own receive timestamp and remote peer IP. Behind a reverse proxy, the peer-IP resolver must use forwarded headers only when the immediate proxy is trusted.

## 5. Acceptance criteria

The feature is not complete until all of the following pass:

- A Devices page lists all authorized enrolled devices with the required default columns and clickable hostnames.
- Search, filters, sorting, pagination, and online/offline calculation work using server-side queries.
- Clicking a hostname opens the correct device detail route and displays all categories with freshness and availability states.
- Windows domain-joined, Windows workgroup, Debian/Ubuntu, Arch/CachyOS, Fedora/Nobara, Bazzite, and future macOS providers are tested separately before support is claimed.
- IP, MAC, gateway, DNS, DHCP, lease expiry, connection type, connecting IP, and connection stats are correct in single-NIC and multi-NIC scenarios.
- Boot time, OS installation time, timezone, current user/session, RAM, CPU, GPU, DPU, BIOS, serial, SMBIOS GUID, battery, storage, and agent version are tested where available.
- GPS remains disabled on a clean install and after upgrade; with local GPS disabled, remote policy cannot cause any location API call or GPS upload.
- When GPS is enabled locally, platform permission behavior, opt-in display, timestamps, accuracy, authorization, audit, retention, and redaction are tested.
- Private keys and device bearer credentials never appear in device inventory responses.
- Real server/agent integration tests populate the database and render realistic details without mocked device records.
