# Euryphos repository layout and separate agent distribution

**Status:** The server and standalone agent repositories exist on GitHub and are source-separated. End-to-end protocol compatibility and release validation remain outstanding.

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

The former `CommandPilot-Agent` project is historical. The current standalone endpoint project is `https://github.com/gitarman94/Euryphos-Agent`. Do not treat legacy installers, dependencies, release assets, or old protocol behavior as compatible until they have been audited and migrated.

A prior copy of the historical agent README indicated GPL-3.0. Preserve all original notices and previously granted GPL-3.0 rights for any code that is derived from or was distributed under that license. The Euryphos Personal Use License does not revoke or narrow prior grants. Audit source provenance before assuming the personal/commercial policy applies to every agent file.

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
- [x] Create the separately maintained `Euryphos-Agent` GitHub repository. The legacy source/history and license audit remains open.

## Important installer constraint

The one-line `curl` and PowerShell install commands must not be published as usable production instructions until release assets, signatures/checksums, installer parameters, upgrade behavior, and actual server-agent enrollment have passed tests. The project is not yet at that stage.
