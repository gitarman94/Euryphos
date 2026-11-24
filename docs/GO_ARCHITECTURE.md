# Go and SQLite architecture baseline

## Runtime

- `cmd/euryphos-server`: Go HTTP management API (this repository contains server code only).
- `Euryphos-Agent` is maintained in the separate `gitarman94/Euryphos-Agent` repository.
- `modernc.org/sqlite`: pure-Go SQLite driver; no PostgreSQL process and no CGO requirement.
- `migrations/`: reviewed SQLite schema migration reference; the server currently applies the initial idempotent schema at startup when `EURYPHOS_AUTO_MIGRATE=true` (default).
- `docs/`: product, architecture, support matrix, installer status, implementation status, and completion plan.

## Persistence decision

Use one local SQLite database file, configured with `EURYPHOS_DB_PATH` (default `./data/euryphos.db`). Apply WAL mode, foreign-key enforcement, a 10-second busy timeout, `synchronous=NORMAL`, indexes on common filters, and a bounded SQL connection pool. The database file and its containing directory must be access-restricted and stored on a local filesystem with reliable locking.

SQLite is embedded and removes a separately operated database service. It supports concurrent readers in WAL mode but still serializes writes. Performance is workload-dependent; it must not be described as universally faster than a client/server database. Do not run multiple Euryphos server replicas against the same SQLite file. Test heartbeat/inventory write bursts, job dispatch, retention, backups, and restore against the expected fleet size.

The SQL schema uses SQLite-compatible `TEXT` storage for JSON and UTC timestamps, `?` placeholders, foreign keys, and atomic `UPDATE ... RETURNING` job claims. Inventory categories live in `device_inventory` rows rather than a PostgreSQL JSONB merge expression.

## Security and reliability

1. Never ship default administrator or enrollment secrets.
2. Store only a SHA-256 hash of each endpoint bearer token.
3. Require administrator approval before an enrolled endpoint is authorized for normal requests.
4. Require TLS at the deployment edge; agents must validate certificates and support enterprise CA/proxy configuration.
5. Bound HTTP body sizes, timeouts, connection pools, and request concurrency.
6. Back up SQLite using its online backup facility or a tested quiesced procedure; account for WAL/SHM sidecar files during live backup.
7. Add database integrity checks, migration rollback/backup gates, retention/compaction, and restore drills before production.
8. Use outbound agent connections; do not expose managed endpoints to inbound control connections.

## Platform provider model

Keep shared protocol, inventory schema, job lifecycle, telemetry, and policy evaluation in common Go packages. Put native operations behind platform-specific interfaces and build-tagged implementations. Linux package management must be distribution-family aware, not a single `apt` branch. Bazzite and other immutable/atomic systems need image-aware update workflows rather than assuming conventional mutable DNF behavior. See `SUPPORT_MATRIX.md`.

Windows uses native service, registry, WMI/CIM, Windows Update, and MSI/provider interfaces. A macOS agent is a planned later deliverable and must use native launchd, Keychain, inventory, update, signing, notarization, and MDM-compatible mechanisms.

## Current API routes

- `GET /api/health`
- `POST /api/v1/enrollment`
- `POST /api/v1/devices/{id}/approve`
- `POST /api/v1/devices/{id}/revoke`
- `POST /api/v1/devices/{id}/heartbeat`
- `POST /api/v1/devices/{id}/inventory`
- `GET /api/v1/devices`
- `POST /api/v1/jobs`
- `GET /api/v1/devices/{id}/jobs/next`
- `POST /api/v1/devices/{id}/jobs/{job}/result`
- `GET /api/v1/jobs/{id}`

## Next engineering steps

1. Complete and verify the SQLite dependency lock (`go.sum`) in a network-enabled Go environment; run full tests against the real driver.
2. Add durable, versioned migration files and migration checksums.
3. Complete the protocol integration test with the built agent executable, not just handler-level tests.
4. Implement Linux distro identification and providers for Arch/pacman, Debian/apt+dpkg, Fedora/RPM, and immutable Fedora Atomic/Bazzite workflows.
5. Build signed Linux and Windows installers, service management, upgrade, repair, uninstall, and rollback paths.
6. Implement Windows and Linux native inventory and patch providers, with distribution/OS-specific VM acceptance.
7. Add a future macOS agent and installer (launchd, PKG signing/notarization, Keychain, Apple inventory and update APIs).
8. Benchmark SQLite under realistic fleet-write load; define supported scale, backup/restore, and disaster-recovery targets.
