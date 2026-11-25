<!--
Euryphos server repository landing page.
Detailed engineering state and continuation instructions live in docs/PROJECT_HANDOFF.md.
-->

<div align="center">

# Euryphos

### See your fleet clearly. Manage it deliberately.

Centralized endpoint visibility, inventory, and controlled operations for Linux and Windows environments.

[Server repository](https://github.com/gitarman94/Euryphos) · [Endpoint agent](https://github.com/gitarman94/Euryphos-Agent) · [Project handoff](docs/PROJECT_HANDOFF.md)

[![Server CI](https://github.com/gitarman94/Euryphos/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/gitarman94/Euryphos/actions/workflows/go.yml)

</div>

> [!WARNING]
> **Early development — not production-ready.** Euryphos is being built toward enterprise fleet management, but its endpoint workflows, deployment process, security controls, and supported-platform behavior have not completed production qualification. Do not use it for unattended patching or security-critical fleet operations.

---

## One management plane, two small components

Euryphos is designed to give administrators a dependable picture of every enrolled computer and a controlled, auditable way to manage those systems. It is intended to grow beyond patching into inventory, software lifecycle, configuration, and day-to-day endpoint operations.

| Component | Responsibility |
| --- | --- |
| **[Euryphos server](https://github.com/gitarman94/Euryphos)** | Central Go service, HTTP API, SQLite database, and browser console |
| **[Euryphos Agent](https://github.com/gitarman94/Euryphos-Agent)** | Separately installed endpoint service that inventories devices and communicates with the server |

The server and agent are intentionally kept in separate repositories. The agent initiates authenticated outbound communication; the server does not need to open inbound management ports on every endpoint.

## What is implemented today

The current foundation includes:

- **Device enrollment and identity** — enrollment tokens, per-device credentials, administrator approval, and revocation.
- **Fleet inventory** — system, network, storage, service, and installed-software data, with normalized records and inventory history.
- **Device visibility** — searchable, filterable, paginated device records and server-derived online/offline state.
- **Duplicate review** — identify possible duplicate computers and explicitly merge records while preserving device identity continuity.
- **Groups and targeting** — static and rule-based groups, with target snapshots recorded when jobs are created.
- **Controlled job workflow** — queued work, per-device claims, result reporting, stale-claim protection, and an administrator queue for overdue jobs.
- **Operational audit trail** — records for important administrative, enrollment, group, job, and alert actions.
- **Browser console** — initial fleet overview, device details, duplicate review, groups, jobs, and audit views.

The endpoint agent has initial Linux and Windows inventory providers and an initial service-control executor. The server recognizes additional future job types, but an accepted action name is **not** proof that an endpoint executor is implemented.

## What is still being built

The project is not yet a complete endpoint-management suite. Major work remains, including:

- Completing and runtime-testing the server-to-agent workflow on real Linux and Windows systems.
- Proving safe job execution, interruption handling, recovery, and verification before enabling broader automation.
- Implementing and validating patch discovery and deployment.
- Completing software installation/removal and configuration-management workflows.
- Expanding the dashboard, inventory depth, reporting, and enterprise access controls.
- Qualifying fresh installation, upgrades, rollback, reboot recovery, and database restore on Debian 13 Proxmox.
- Security review, scale testing, and platform-specific support certification.

See the [Project Context and Continuation Handoff](docs/PROJECT_HANDOFF.md) for the verified implementation state, owner decisions, architecture, test evidence, and prioritized work remaining.

## Architecture

Euryphos is deliberately straightforward:

- **Go** for the server and endpoint agent.
- **SQLite** embedded in the server process; no separate database service.
- **Native systemd service** for the server deployment.
- **Separate server and agent repositories.**
- **Typed, auditable operations** rather than an unrestricted remote-shell design.
- **Bounded inventory, queries, and job processing** to keep fleet growth manageable.

The server is intended to run as a single instance with its SQLite database on persistent local storage. SQLite WAL improves reader concurrency, but it does not make a shared database file suitable for multiple active server replicas.

## Install the server

The first deployment target is a **Debian 13 Proxmox LXC container** running systemd. The installer supports Linux `amd64` and `arm64`.

For an **initial test installation before the first versioned release**, run this single command as `root` inside the Debian 13 container:

```bash
apt-get update && apt-get install -y ca-certificates curl && EURYPHOS_SOURCE_BUILD=1 bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/gitarman94/Euryphos/main/scripts/install.sh | bash -s -- install'
```

This explicitly builds the current public `main` source inside the CT. It installs the Debian `golang-go` build package, downloads the exact main-branch commit selected by GitHub at install time, builds the server for the CT architecture, and then uses the same service setup, SQLite backup, and health-check path as the release installer. The Go build tools remain installed in the CT. This is a **testing path**, not an immutable release deployment.

After the first versioned GitHub release is published, the normal prebuilt-binary install command will be:

```bash
apt-get update && apt-get install -y ca-certificates curl && bash -o pipefail -c 'curl -fsSL https://raw.githubusercontent.com/gitarman94/Euryphos/main/scripts/install.sh | bash -s -- install'
```

The release installer downloads the matching Linux archive and verifies its SHA-256 checksum before installation. Both paths create the unprivileged service account, install the systemd unit, and create the initial configuration and secrets. The installer explicitly sets release-directory traversal permissions despite its restrictive file-creation umask, then checks that the `euryphos` service account can read and execute the binary before switching the active version. It prints systemd diagnostics if startup or the health check fails. The default service listener is port `8080`; place it behind a trusted HTTPS reverse proxy before exposing it to untrusted networks.

Fresh-install, in-place upgrade, failed-upgrade rollback, reboot, and database restore testing in a Debian 13 Proxmox CT are still required.

After installation, the service is managed with:

```bash
systemctl status euryphos
journalctl -u euryphos -f
```

The initial administrator and enrollment tokens are stored in the root-readable configuration file at `/etc/euryphos/euryphos.env`.

## Build from source

Requirements: Go 1.23 or later.

```bash
git clone https://github.com/gitarman94/Euryphos.git
cd Euryphos
go test ./...
go vet ./...
go build ./cmd/euryphos-server
```

For local development, set independent high-entropy values for `EURYPHOS_ADMIN_TOKEN` and `EURYPHOS_ENROLLMENT_TOKEN`, choose a persistent `EURYPHOS_DB_PATH`, then run `go run ./cmd/euryphos-server`. The default development listener is `:8080`.

## Platform direction

The intended endpoint support matrix covers Arch-family distributions (including CachyOS), Debian-family distributions (including Debian, Ubuntu, Mint, and LMDE), Fedora-family distributions (including Fedora and Nobara), Bazzite/Atomic systems using image-aware workflows, and Windows. A native macOS agent is planned for a later phase. These are project targets, not a statement that every distribution is currently certified.

## Project status and contribution context

This repository is the **server only**. For current engineering instructions, source map, API behavior, constraints, and next tasks, read the single detailed [Project Context and Continuation Handoff](docs/PROJECT_HANDOFF.md). Keep that file synchronized with meaningful implementation changes; avoid creating duplicate roadmap and status documents.

## License

Euryphos is distributed under the **Euryphos Personal Use License (EPUL) 1.0**. It is not an OSI-approved open-source license. Personal-use permissions and separate commercial-use requirements are described in [LICENSE](LICENSE). Previously granted GPL-3.0 and third-party rights remain in force; review the license and source-provenance terms before reusing or redistributing code.
