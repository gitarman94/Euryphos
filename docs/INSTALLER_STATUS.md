# Euryphos Server installer and deployment status

**Scope:** This document covers the central server only. Endpoint installers belong in the separate `Euryphos-Agent` repository.

## Current state

- The server repository includes a Go server binary entry point, Dockerfile, and Docker Compose deployment file.
- There is no supported Linux package installer, systemd service installer, Windows server installer, or Proxmox LXC bootstrap script yet.
- The Docker image and Compose deployment have not been built and run in the current validation environment.
- A real-driver Go server build/test is still blocked until `modernc.org/sqlite` can be downloaded in a network-enabled Go environment.
- No one-command Debian 13 / Proxmox CT install or in-place update workflow is available yet.

## Planned Debian 13 / Proxmox LXC deployment

The intended first server deployment target is a Debian 13 LXC container on Proxmox. The planned installer must:

- Download a versioned Euryphos Server release rather than build source inside the CT.
- Verify release checksums and cryptographic signatures before installation.
- Create a dedicated unprivileged `euryphos` service account and a systemd unit.
- Store configuration and credentials with restrictive permissions.
- Store the SQLite database under `/var/lib/euryphos/`, preserving database files across upgrades.
- Apply schema migrations with a safe backup/rollback gate.
- Configure the listen address, TLS/reverse-proxy guidance, firewall guidance, logs, and health checks.
- Provide idempotent install, update, repair, backup, restore, and uninstall commands.
- Detect systemd/container constraints and report unsupported CT settings rather than silently changing Proxmox configuration.

## Required deliverables

- [ ] Reproducible Linux release artifacts for supported server architectures.
- [ ] SHA-256 checksums and signed release manifest.
- [ ] Debian 13 install script or package and systemd unit.
- [ ] Safe update/rollback path that preserves SQLite data and configuration.
- [ ] Backup and restore commands using SQLite's online backup mechanism or a tested quiesced procedure.
- [ ] Health check, service logs, diagnostic collection, and recovery instructions.
- [ ] Proxmox Debian 13 CT acceptance tests covering fresh install, restart, reboot, update, backup/restore, and uninstall.

## Production gate

Do not publish an executable one-line `curl | bash` command until release artifacts are versioned, checksums/signatures are verified, the install/update logic has passed disposable Debian 13 CT tests, and the server has passed actual SQLite-driver startup and API tests.

## Endpoint client installers

Linux, Windows, and future macOS endpoint installers are maintained in the separate `Euryphos-Agent` repository. They are not server installation dependencies and are not included in this repository. See `docs/REPOSITORY_LAYOUT.md`.
