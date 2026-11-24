# Euryphos platform and distribution support matrix

**Status:** Planning baseline only. None of the distributions below is formally certified by the current Go agent. A distribution becomes supported only after its exact release, architecture, install/update path, inventory providers, patch behavior, service lifecycle, rollback, and security controls pass the release tests.

## Support policy

Euryphos separates four independent capabilities:

1. **Agent installation and service:** Can the signed agent be installed, started, upgraded, repaired, and removed safely?
2. **Inventory:** Can the agent reliably collect and normalize hardware, OS, software, services, configuration, and security facts?
3. **Patch/update management:** Can it discover and install updates safely using the host's supported package/update system?
4. **Configuration enforcement:** Can it apply and verify supported configuration resources without breaking the OS or vendor update model?

A successful install alone does not mean the OS has full inventory, patch, or configuration support.

## Linux distribution candidates

| Family / provider | Named distribution targets | Primary native interfaces | Planned priority | Current status |
|---|---|---|---|---|
| Arch | Arch Linux | `pacman`, `pacman-contrib`, `libalpm` where justified | Core family | Planned; not certified |
| Arch derivatives | CachyOS, EndeavourOS, Manjaro | `pacman` family; detect derivative-specific repo and package behavior | Named derivative targets | Planned; not certified |
| Debian | Debian stable releases | `dpkg-query`, `apt`, `apt-get` | Core family | Planned; not certified |
| Ubuntu | Ubuntu LTS releases | `dpkg-query`, `apt`, `apt-get` | Core family | Planned; not certified |
| Linux Mint | Linux Mint Ubuntu-based editions | `dpkg-query`, `apt`; detect Mint release/base relationship | Named derivative target | Planned; not certified |
| Linux Mint Debian Edition | LMDE | `dpkg-query`, `apt`; treat as Debian branch, not Ubuntu branch | Named derivative target | Planned; not certified |
| Fedora | Fedora Workstation and Server | `rpm`, `dnf` / `dnf5` | Core family | Planned; not certified |
| Fedora derivative | Nobara | `rpm`, `dnf` / `dnf5`, plus Nobara repository and packaging differences | Named derivative target | Planned; not certified |
| Fedora Atomic / image-based | Bazzite | `rpm-ostree` / bootable image deployment model; image status and rollback APIs | Named high-priority special case | Planned; not certified |
| Fedora Atomic / image-based | Fedora Silverblue, Kinoite, other declared Atomic variants | `rpm-ostree` / image deployment model | Later extension | Planned; not certified |
| Debian/Ubuntu derivatives | Pop!_OS, Zorin OS, elementary OS | `dpkg-query`, `apt`; derivative-specific validation | Later extension | Planned; not certified |
| RHEL-compatible | RHEL, Rocky Linux, AlmaLinux, CentOS Stream | `rpm`, `dnf` | Later extension | Planned; not certified |

### Mandatory family behavior

**Arch family:** Do not run unattended package upgrades by composing arbitrary `pacman -Syu` commands without considering rolling-release synchronization, mirror/keyring state, package conflicts, and the user's update policy. Use supported command interfaces, record transaction output, and detect partial failures.

**Debian family:** Distinguish package inventory (`dpkg-query`) from update/transaction operations (`apt`/`apt-get`). Test Debian, Ubuntu, Mint's Ubuntu-based editions, and LMDE separately even though they share the dpkg/apt family.

**Fedora/RPM family:** Support `rpm` inventory and `dnf`/`dnf5` update workflows with Fedora release-aware metadata and transaction results. Do not assume Fedora derivatives have identical repositories or defaults; Nobara requires its own validation.

**Bazzite and other image-based variants:** Treat the host OS as an image/deployment-managed system. Do not use ordinary Fedora `dnf` host-update assumptions. The provider must account for rpm-ostree/image deployment, reboot coordination, and rollback. Layering packages should not be the default agent installation method. Agent installation should use an explicitly supported host-safe method and be tested against image upgrades/rebases.

### First certification test matrix

The initial VM/physical test suite should cover at least:

- Arch Linux x86_64 and aarch64 if offered by the distribution.
- CachyOS x86_64.
- Debian stable x86_64 and aarch64.
- Ubuntu current and previous supported LTS, x86_64 and aarch64 where available.
- Linux Mint current supported Ubuntu-based release x86_64; LMDE must be a separate test target.
- Fedora current supported Workstation and Server releases, x86_64 and aarch64 where available.
- Nobara current supported release x86_64.
- Bazzite current stable image on each officially supported target device class; validate image updates/rebases and reboot/rollback.

Additional derivatives may be listed as **best-effort** only after the exact derivative and release have been smoke-tested. They must not inherit a support claim solely because they share a package manager.

## Windows

| Target | Architectures | Planned installation/service | Current status |
|---|---|---|---|
| Windows client workstations | x64; ARM64 to be qualified | Signed MSI, native Windows service, DPAPI-protected config/secrets | Planned; not certified |
| Windows Server | x64; ARM64 only if product scope requires it | Same agent service with server-specific inventory and patch providers | Planned; not certified |

The supported Windows version/build floor must be declared before release. Testing must include service lifecycle, UAC, GPO, proxy/custom CA, code signing, EDR/AV interaction, locked-file upgrades, rollback, and reboot-required update handling.

## macOS (future client)

A native macOS endpoint agent is an explicit later roadmap requirement; it is **not implemented in the current release**.

- Architectures: Intel (`amd64`) and Apple Silicon (`arm64`).
- Installation: signed and notarized PKG; unattended MDM-friendly install/upgrade/removal.
- Service: native `launchd` LaunchDaemon with least privilege and restart/logging behavior.
- Credentials: Keychain-backed storage.
- Inventory: hardware, macOS version/build, apps/packages, services/launch agents, profiles, security posture, and supported configuration facts.
- Updates: Apple-native update discovery/install mechanisms, with safe deferral, maintenance windows, reboot handling, and policy controls.
- Platform constraints: SIP, TCC/privacy permission model, FileVault, Gatekeeper, Apple code signing/notarization, and MDM deployment must be addressed explicitly.
- Support policy: choose the minimum supported macOS release and test current/previous supported releases on Intel and Apple Silicon before making support claims.

## Cross-platform release gates

For every declared OS/distribution/release/architecture combination, record:

- Install, silent install, re-install, upgrade, repair, uninstall, and rollback outcomes.
- Service startup, crash recovery, shutdown, and reboot persistence.
- Enrollment, approval, token rotation/revocation, heartbeat, inventory, job dispatch, result reporting, and offline reconnect.
- Hardware/OS/software/service/configuration inventory completeness and source/freshness metadata.
- Patch scan/install/reboot/rollback behavior for each supported native update provider.
- Signature validation, least privilege, file/secret permissions, custom CA, proxy, and audit evidence.
- Performance and resource use while idle and during inventory/patch activity.

No OS or distribution is marked **supported** until the above evidence is retained in CI and the release qualification report.

## Reference notes

- Bazzite documents itself as a Fedora Atomic Desktop image and documents rpm-ostree package layering and image-style update/rollback behavior: https://docs.bazzite.gg/General/FAQ/ and https://docs.bazzite.gg/Installing_and_Managing_Software/rpm-ostree/.
- Nobara describes itself as Fedora-based and independent, with its own repository and default choices: https://nobaraproject.org/.
- CachyOS documentation recommends the pacman command-line interface for system package maintenance: https://wiki.cachyos.org/cachyos_basic/faq/.
- Arch's package management interface is pacman: https://wiki.archlinux.org/title/Pacman.
- Debian's FAQ explicitly treats Ubuntu and Linux Mint as Debian-based derivatives rather than Debian itself: https://www.debian.org/doc/manuals/debian-faq/choosing.
