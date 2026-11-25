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

## Debian 13 Proxmox CT installation and upgrade

The server runs directly as a Go binary under **systemd inside a Debian 13 Proxmox LXC container**. It does not require Docker, Docker Compose, PostgreSQL, or a separate database service. SQLite lives on the CT's persistent filesystem at `/var/lib/euryphos/euryphos.db`.

The GitHub repository is currently private. Run the following as root inside the CT. Use a fine-grained GitHub token with **Contents: read** for this repository; the token is requested without echo and is not written to the Euryphos configuration.

First install or upgrade (the command is the same apart from the final argument):

```bash
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
```

For later upgrades, repeat the command and change the final argument from `install` to `upgrade`. The updater resolves the latest published GitHub release. To pin a version, export `EURYPHOS_VERSION=vX.Y.Z` before running it.

A `v*` Git tag triggers the release workflow. It tests the tagged Go source and publishes Linux amd64 and arm64 archives plus `SHA256SUMS`. The installer checks the archive checksum before installing it. A published release is required; the installer stops without replacing the running application when no matching release or valid asset is available.

The installer:
- Requires Debian 13, root, and a running systemd manager.
- Creates the unprivileged `euryphos` service account and installs the systemd unit.
- Installs the binary under a versioned directory in `/opt/euryphos/releases/` and switches `/opt/euryphos/current` atomically.
- Creates `/etc/euryphos/euryphos.env` on first install with generated administrator/enrollment secrets. Later upgrades preserve the file unchanged.
- Keeps the SQLite database under `/var/lib/euryphos/`; upgrades never delete, reset, or replace that live database.
- Stops the service before making a SQLite backup with the SQLite backup API, verifies the backup with `PRAGMA integrity_check`, then starts the new version.
- Checks `/api/health`. If the new version fails to start or pass the local health check, it switches back to the previous binary and restores the pre-upgrade database backup.
- Retains backups in `/var/lib/euryphos/backups/` for manual recovery.

Useful commands:

```bash
systemctl status euryphos
journalctl -u euryphos -f
grep '^EURYPHOS_ADMIN_TOKEN=' /etc/euryphos/euryphos.env
```

The default listener is `:8080`. Put it behind a trusted HTTPS reverse proxy before exposing it to untrusted networks. `EURYPHOS_COOKIE_SECURE=true` is the installed default; do not disable it for an internet-facing service.

**Deployment qualification is still required:** the installer and release workflow are implemented, but a fresh install, in-place upgrade, forced failed-upgrade rollback, reboot, and database restore must still be exercised in a disposable Debian 13 Proxmox CT. SHA-256 verifies transfer integrity against the GitHub release manifest; cryptographic release signing and provenance verification remain release-gate work.

## Platform direction

Planned endpoint targets include Arch Linux and CachyOS; Debian, Ubuntu, Linux Mint and LMDE; Fedora and Nobara; Bazzite/Atomic using image-aware workflows; and Windows. A native macOS agent is a later phase. None is currently certified for production use. A successful cross-build is not platform certification.

## Project continuation

The detailed project context, owner decisions, current implementation, test evidence, platform requirements, and prioritized work remaining are kept in one document:

**[Project Context and Continuation Handoff](docs/PROJECT_HANDOFF.md)**

Read that handoff before continuing development after losing chat history. It is the single detailed engineering plan; do not recreate multiple overlapping status, roadmap, architecture, support, API, or installer documents.

## Licensing

Euryphos is source-available under the custom **Euryphos Personal Use License (EPUL) 1.0**. It is not an OSI-approved open-source license. Free personal-use rights apply only to material the applicable rights holder can license. Corporate use requires a separate written commercial license for that material. Previously granted GPL-3.0 and third-party rights remain in force. Read [LICENSE](LICENSE) and the licensing/source-provenance section of the project handoff before use.
