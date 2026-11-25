<div align="center">

# Euryphos

### Fleet visibility. Controlled operations.

A Go-based systems-management platform for discovering, understanding, and safely managing Linux and Windows endpoints.

[![Server CI](https://github.com/gitarman94/Euryphos/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/gitarman94/Euryphos/actions/workflows/go.yml)

[Server](https://github.com/gitarman94/Euryphos) · [Endpoint agent](https://github.com/gitarman94/Euryphos-Agent) · [Engineering handoff](docs/PROJECT_HANDOFF.md)

</div>

> [!WARNING]
> **Early development — not production-ready.** The server has been installed and health-checked in a Debian 13 Proxmox LXC test container. The endpoint agent has not yet completed a real server-to-agent deployment test. Do not use Euryphos for unattended patching or security-critical fleet operations.

## What it is

Euryphos is being built to provide one reliable view of an organization's managed computers, followed by controlled, auditable operations across individual devices, groups, or the fleet.

The product is designed around a simple lifecycle:

**Discover → Inventory → Search → Target → Approve → Dispatch → Execute → Verify → Audit**

A queued job is not a completed action. Euryphos must distinguish delivery, execution, verification, and failure instead of presenting optimistic status.

## Architecture

Euryphos is intentionally split into two small Go projects:

| Component | Responsibility |
| --- | --- |
| **[Euryphos server](https://github.com/gitarman94/Euryphos)** | Central API, SQLite persistence, browser console, fleet and job control |
| **[Euryphos-Agent](https://github.com/gitarman94/Euryphos-Agent)** | Separately installed endpoint client, native inventory providers, typed endpoint actions |

The server does not need to open inbound management ports on every computer. Agents initiate authenticated outbound connections to the server.

**Technology decisions:** Go for both components; embedded SQLite on the server; native systemd service in the Debian 13 Proxmox LXC deployment. No PostgreSQL, Docker, Compose, Python, or Rust runtime is required.

## Current capabilities

### Server

- Administrator-token login and per-device enrollment credentials.
- Pending device approval, identity, revocation, and duplicate review.
- Device inventory API with latest snapshots and change history.
- Normalized system, network, storage, service, and software records.
- Static and rule-based device groups.
- Typed job queue, delivery claims, result reporting, and overdue-job review.
- Initial service-control workflow and operational audit log.
- Browser views for Overview, Devices, Groups, Jobs, and Audit.

### Endpoint agent

The agent currently collects and uploads five inventory categories:

| Category | Current collection |
| --- | --- |
| **System** | OS and host identity, hardware/BIOS, CPU model and core counts, memory facts where exposed, uptime where exposed, root-volume capacity, timezone, and selected platform facts |
| **Network** | Interfaces, addresses, MAC addresses, DNS, gateway, and available DHCP details |
| **Storage** | Physical disks and mounted volumes, capacity, free space, model/serial data where available |
| **Services** | Linux systemd services or Windows services and their current/configured state |
| **Software** | Linux DPKG/RPM/pacman packages or Windows machine uninstall-registry entries |

Full inventory is sent after enrollment/approval and then on a 15-minute default interval. A server-requested `inventory.refresh` triggers an immediate upload. A lightweight heartbeat and job poll run every 30 seconds by default.

The current agent has an initial typed service-control executor. Most other management actions are not implemented.

## Important limitations

The five inventory categories above are real agent collection and upload paths, but they are not a complete inventory of every system fact. Provider coverage varies by operating system and permissions. Missing, unsupported, or unavailable facts must not be interpreted as zero or as a successful empty result.

**Performance monitoring is not implemented yet.** The current system inventory includes hardware capacity facts such as CPU model/core counts and total/available memory; it does not provide CPU utilization history, memory pressure trends, disk I/O rates, network throughput, or process-level monitoring. A separate, bounded telemetry pipeline and Performance UI are planned.

Also not production-ready:

- Linux/Windows agent installers, service integration, signed releases, self-update, and rollback.
- Real-platform server/agent end-to-end qualification.
- Patch discovery and installation.
- Software deployment and configuration enforcement.
- Enterprise identity controls such as RBAC, MFA, and SSO.
- Full security review, scale testing, and operational recovery qualification.

A server action being accepted or displayed does not prove that an agent executor exists. Unsupported actions must report that they are not implemented.

## Server installation

The first deployment target is a **Debian 13 Proxmox LXC container** with systemd. The installer supports Linux amd64 and arm64.

Run inside the container as root. This pre-release source-build command downloads a pinned installer revision, verifies its revision and Bash syntax, and builds the current public server source:

```bash
apt-get update && apt-get install -y ca-certificates curl && f="$(mktemp)" && curl --retry 3 --retry-all-errors -fsSL "https://raw.githubusercontent.com/gitarman94/Euryphos/39bd57efc27907612e74c26506fe9dc2da8a4522/scripts/install.sh" -o "$f" && grep -Fq 'INSTALLER_REVISION="2026-11-25.7"' "$f" && bash -n "$f" && EURYPHOS_SOURCE_BUILD=1 bash "$f" install; result=$?; [[ -z "${f:-}" ]] || rm -f "$f"; exit "$result"
```

The same installer supports `upgrade`, data-preserving `uninstall`, destructive `uninstall --purge`, and `reinstall --purge`. Read the [server deployment and recovery instructions](docs/PROJECT_HANDOFF.md#11-server-deployment-configuration-reference) before using lifecycle commands. Purge permanently deletes the local database, configuration, backups, and tokens.

### Initial sizing guidance

| Resource | Small test baseline | Recommended starting point |
| --- | ---: | ---: |
| CPU | 2 vCPU | 4 vCPU |
| RAM | 2 GiB for source-build testing | 4 GiB |
| Disk | 10 GiB total / 5 GiB free | 20 GiB total / 10 GiB free |

These are initial test figures, not a fleet-capacity guarantee. Actual requirements depend on endpoint count, inventory volume, collection cadence, history retention, and job concurrency.

### Secure access

The installed default is `EURYPHOS_COOKIE_SECURE=true`. Browser authentication requires HTTPS even though the HTTP login page can be viewed. Configure a trusted HTTPS reverse proxy and use `https://<your-hostname>/login` before normal use. Do not expose the raw port 8080 listener to the public internet.

## Enrolling computers

The server does not discover or install agents automatically. Each endpoint needs a separately installed Euryphos-Agent, configured with the server URL and enrollment secret. Enrollment creates a pending device; an administrator must approve it before the endpoint's normal heartbeat and inventory operations are accepted.

The agent repository is currently private and does not yet publish supported endpoint installers or signed releases. Its [README](https://github.com/gitarman94/Euryphos-Agent) and [project handoff](https://github.com/gitarman94/Euryphos-Agent/blob/main/docs/PROJECT_HANDOFF.md) describe the current source-build enrollment path and platform limitations.

## Project status and next work

The next integration milestone is to enroll a real Linux endpoint, approve it, verify all five inventory categories in the server and browser console, and exercise one safe typed job. Repeat the workflow on Windows before claiming platform support.

After that, priorities include reliable job recovery, read-only patch discovery, endpoint installers, and the separate performance-telemetry pipeline. Performance charts will be added only after the agent collects timestamped samples and the server stores and serves them with bounded retention.

For verified source state, architecture decisions, validation evidence, and the prioritized implementation sequence, see the **[Project Context and Continuation Handoff](docs/PROJECT_HANDOFF.md)**.
