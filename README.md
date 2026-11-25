# Euryphos

**Enterprise systems management, fleet visibility, and controlled endpoint operations — built in Go.**

Euryphos is an early-development platform for discovering, inventorying, monitoring, patching, configuring, and operating Windows and Linux computers from one central management plane. Its purpose is to give administrators a trustworthy view of their managed estate and a controlled, auditable way to make changes across individual devices, groups, or the wider fleet.

> **Development status: not production-ready.** This repository contains the central Go server and SQLite persistence layer. It is not yet a complete enterprise endpoint-management product. Do not rely on it for production patching or security-critical fleet operations.

## Two separate repositories

| Repository | Responsibility |
|---|---|
| **[Euryphos](https://github.com/gitarman94/Euryphos)** (this repository) | Central Go server, HTTP API, SQLite database, migrations, browser console, and server deployment |
| **[Euryphos-Agent](https://github.com/gitarman94/Euryphos-Agent)** | Separately installed Go endpoint client, inventory providers, endpoint service integration, and client installers as implemented |

Install the server once on a central host. Install the agent separately on each managed endpoint. The agent initiates outbound authenticated communication. The endpoint repository does not require the server or SQLite.

## Current implementation

The server currently provides:

- Go standard-library HTTP API and Go-served browser console at `/login` and `/console`.
- Embedded SQLite using the pure-Go `modernc.org/sqlite` driver; no PostgreSQL service or CGO dependency.
- Enrollment, device credentials, administrator approval/revocation, heartbeat and categorized inventory ingestion.
- Typed device fields and normalized network-interface, disk, volume, service and installed-software records.
- Latest inventory and change-history APIs, bounded device search/filter/sort/pagination, and server-derived online/offline presence.
- Duplicate candidate review and explicit merge with endpoint-token continuity.
- Static and dynamic groups, directory-compatible matching, and per-device job targeting with group snapshots.
- Initial audit events, admin browser sessions and CSRF protection.
- Initial job queue, device claim/result endpoints, service-control job validation and a minimal expired-receive recovery path.

The matching endpoint agent is still early-stage. It has initial Linux/Windows inventory providers and an initial Linux/systemd and Windows service-control executor. Most patch, software, configuration, reboot/shutdown and self-update executors are not implemented. The agent's latest passing CI after service-control changes is not yet confirmed.

## Architecture decisions

- Go only; no Python or Rust runtime dependencies.
- SQLite, not PostgreSQL.
- One server process/database host. SQLite WAL supports concurrent readers but serializes writes; do not run multiple server replicas against the same database file.
- Keep server and agent in separate repositories.
- Keep the codebase compact and avoid unnecessary files or unrelated refactors.
- Device rows use a database-generated integer `database_id` plus a stable agent-facing UUID `id`.
- Duplicate devices are reviewed and explicitly merged; never auto-merge based only on a guess.
- GPS remains off by default and locally controlled by the agent. Server policy must not override a local disabled state.

## Development build

Requirements: Go 1.23 or later.

```bash
git clone https://github.com/gitarman94/Euryphos.git
cd Euryphos
go mod download
go test ./...
go vet ./...
```

Start a local development server with independent high-entropy secrets:

```bash
export EURYPHOS_ADMIN_TOKEN="$(openssl rand -hex 32)"
export EURYPHOS_ENROLLMENT_TOKEN="$(openssl rand -hex 32)"
export EURYPHOS_DB_PATH="./data/euryphos.db"
export EURYPHOS_LISTEN=":8080"
go run ./cmd/euryphos-server
```

Check health:

```bash
curl --fail http://127.0.0.1:8080/api/health
```

This is a development procedure, not a production install. Do not expose an unencrypted development listener to untrusted networks.

### Docker Compose

```bash
cp .env.example .env
# Replace both secret placeholders in .env with unique random values.
docker compose up -d --build
```

The SQLite database is persisted in the `euryphos-data` volume. Test backup and restore before relying on it.

## Deployment direction

The first intended server deployment is a Debian 13 Proxmox LXC container. There is not yet a supported one-command installer, signed release flow, systemd package, or tested in-place upgrade/backup/restore procedure. Do not use legacy CommandPilot/PatchPilot scripts; they target an obsolete implementation.

## Platform direction

Planned endpoint targets include Arch Linux and CachyOS; Debian, Ubuntu, Linux Mint and LMDE; Fedora and Nobara; Bazzite/Atomic using image-aware workflows; and Windows. A native macOS agent is a later phase. None is currently certified for production use. A successful cross-build is not platform certification.

## Project continuation

The detailed project context, owner decisions, current implementation, test evidence, platform requirements, and prioritized work remaining are kept in one document:

**[Project Context and Continuation Handoff](docs/PROJECT_HANDOFF.md)**

Read that handoff before continuing development after losing chat history. It is the single detailed engineering plan; do not recreate multiple overlapping status, roadmap, architecture, support, API, or installer documents.

## Licensing

Euryphos is source-available under the custom **Euryphos Personal Use License (EPUL) 1.0**. It is not an OSI-approved open-source license. Free personal-use rights apply only to material the applicable rights holder can license. Corporate use requires a separate written commercial license for that material. Previously granted GPL-3.0 and third-party rights remain in force. Read [LICENSE](LICENSE) and the licensing/source-provenance section of the project handoff before use.
