# Euryphos repository layout and separate agent distribution

**Status:** Local source split prepared; GitHub repository publication and end-to-end compatibility remain outstanding.

## Repository ownership

Euryphos is distributed as two separate repositories:

| Repository | Owns | Must not own |
|---|---|---|
| `gitarman94/Euryphos` | Central Go server, HTTP API, SQLite persistence, migrations, server configuration, server packaging, and future operator UI | Endpoint binaries/source, endpoint installers, per-endpoint database services |
| `gitarman94/Euryphos-Agent` | Go endpoint-agent source, platform inventory providers, client configuration, endpoint service integration, client-only installers, signed endpoint releases, and agent upgrade/rollback | Management server, server-side UI, or SQLite database runtime |

The server archive contains only server code. The separate agent archive contains only endpoint client code. Neither project needs to be installed on the same host as the other.

## Intended end-user deployment

1. Install Euryphos Server once on a central host.
2. Configure administrator and enrollment secrets, TLS, and server database/backup locations.
3. Install Euryphos-Agent independently on each managed endpoint.
4. The endpoint agent initiates authenticated outbound communication to the server.
5. Approve each new enrollment before normal inventory and job communication is authorized.
6. Upgrade and back up the server independently from endpoint agents.

## Historical standalone client repository

The previous repository was `https://github.com/gitarman94/CommandPilot-Agent`. It is the historical client-only project and should be treated as a migration source, not as a validated Euryphos client. Its former product identifiers, protocol, installer scripts, dependencies, notices, and release artifacts require a full audit before reuse.

A prior copy of its README indicated a GPL-3.0 license. Preserve all original license and copyright notices if source is imported from that repository. This standalone agent archive is built from the agent entry point in the Euryphos development tree; it does not silently import old installer scripts or claim those scripts have been migrated.

## Current split deliverables

### Euryphos server repository

- `cmd/euryphos-server/` — Go HTTP server only.
- `migrations/` — SQLite schema reference.
- `Dockerfile`, `docker-compose.yml` — server-only deployment.
- `docs/` — product, API/server architecture, support, installer, and completion documentation.
- No `cmd/euryphos-agent/` directory and no agent binary in the server container image.

### Euryphos-Agent repository

- `cmd/euryphos-agent/` — standalone Go endpoint agent.
- `docs/API_PROTOCOL.md` — protocol contract matching the current server routes.
- No server source, database driver, migrations, Docker server deployment, or admin UI.

## Protocol and release synchronization

The server and agent currently share a small early API contract: enrollment, administrator approval, heartbeat, inventory upload, job polling, and result reporting. This contract is documented in the agent repository and the server architecture documentation.

Before publishing supported endpoint releases:

- [ ] Add a versioned OpenAPI/JSON Schema contract and compatibility policy.
- [ ] Add shared contract tests against the real server and released agent binary.
- [ ] Verify TLS certificate validation, custom CA/proxy support, bounded retries, timeouts, and credential file permissions.
- [ ] Implement platform inventory and job executors; current job actions are placeholders except for the basic inventory refresh response.
- [ ] Replace or build client-only Linux and Windows installers with immutable, checksum- and signature-verified release artifacts.
- [ ] Test Linux package manager branches separately for Arch/pacman, Debian/apt+dpkg, Fedora/dnf+rpm, and image-based Bazzite/rpm-ostree.
- [ ] Implement native Windows service install, repair, upgrade, uninstall, and rollback behavior.
- [ ] Add macOS later: launchd, signed/notarized PKG, Keychain, Intel/Apple Silicon builds, MDM deployment, and Apple-native inventory/update providers.
- [ ] Publish versioned releases and installation instructions only after end-to-end validation.
- [ ] Rename the historical GitHub repository to `Euryphos-Agent` after its source and history have been audited and the migration plan approved.

## Important installer constraint

The one-line `curl` and PowerShell install commands must not be published as usable production instructions until release assets, signatures/checksums, installer parameters, upgrade behavior, and actual server-agent enrollment have passed tests. The project is not yet at that stage.
