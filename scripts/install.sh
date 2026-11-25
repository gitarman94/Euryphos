#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

OWNER="gitarman94"
REPO="Euryphos"
API="https://api.github.com/repos/${OWNER}/${REPO}"
INSTALL_ROOT="/opt/euryphos"
RELEASES_DIR="${INSTALL_ROOT}/releases"
CURRENT_LINK="${INSTALL_ROOT}/current"
CONFIG_DIR="/etc/euryphos"
ENV_FILE="${CONFIG_DIR}/euryphos.env"
DATA_DIR="/var/lib/euryphos"
BACKUP_DIR="${DATA_DIR}/backups"
SERVICE="euryphos.service"
MODE="${1:-install}"
INSTALLER_REVISION="2026-11-25.5"

log() { printf '[euryphos] %s\n' "$*"; }
die() { printf '[euryphos] ERROR: %s\n' "$*" >&2; exit 1; }

# Small terminal UI built from Bash and standard Debian utilities; no extra TUI
# framework is required, so it works both in a console and when piped to Bash.
if [[ -t 1 && -z "\${NO_COLOR:-}" ]]; then
  UI_RESET=$'\033[0m'
  UI_BOLD=$'\033[1m'
  UI_CYAN=$'\033[36m'
  UI_GREEN=$'\033[32m'
  UI_YELLOW=$'\033[33m'
  UI_RED=$'\033[31m'
else
  UI_RESET=''
  UI_BOLD=''
  UI_CYAN=''
  UI_GREEN=''
  UI_YELLOW=''
  UI_RED=''
fi
ui_header() {
  printf '\n%s%s============================================================%s\n' "$UI_BOLD" "$UI_CYAN" "$UI_RESET"
  printf '%s  EURYPHOS SERVER INSTALLER  |  revision %s%s\n' "$UI_BOLD" "$INSTALLER_REVISION" "$UI_RESET"
  printf '%s  Debian 13 Proxmox CT  |  Go server  |  Embedded SQLite%s\n' "$UI_CYAN" "$UI_RESET"
  printf '%s============================================================%s\n\n' "$UI_BOLD" "$UI_CYAN" "$UI_RESET"
}
ui_section() { printf '\n%s== %s ==%s\n' "$UI_BOLD" "$*" "$UI_RESET"; }
ui_ok() { printf '  %s[OK]%s   %s\n' "$UI_GREEN" "$UI_RESET" "$*"; }
ui_warn() { printf '  %s[WARN]%s %s\n' "$UI_YELLOW" "$UI_RESET" "$*"; }
ui_fail() { printf '  %s[FAIL]%s %s\n' "$UI_RED" "$UI_RESET" "$*"; }
format_gib() { awk -v bytes="$1" 'BEGIN { printf "%.1f GiB", bytes/1073741824 }'; }

detect_cpu_count() {
  local count quota period limited quota_file period_file
  count="$(nproc 2>/dev/null || getconf _NPROCESSORS_ONLN 2>/dev/null || printf '1')"
  [[ "$count" =~ ^[0-9]+$ ]] || count=1
  if [[ -r /sys/fs/cgroup/cpu.max ]]; then
    read -r quota period < /sys/fs/cgroup/cpu.max || true
    if [[ "\${quota:-max}" != "max" && "\${quota:-}" =~ ^[0-9]+$ && "\${period:-}" =~ ^[0-9]+$ && "$period" -gt 0 ]]; then
      limited=$(( (quota + period - 1) / period ))
      (( limited < 1 )) && limited=1
      (( limited < count )) && count="$limited"
    fi
  else
    for quota_file in /sys/fs/cgroup/cpu/cpu.cfs_quota_us /sys/fs/cgroup/cpu,cpuacct/cpu.cfs_quota_us; do
      [[ -r "$quota_file" ]] || continue
      quota="$(cat "$quota_file" 2>/dev/null || true)"
      period_file="\${quota_file%/*}/cpu.cfs_period_us"
      period="$(cat "$period_file" 2>/dev/null || true)"
      if [[ "$quota" =~ ^[0-9]+$ && "$period" =~ ^[0-9]+$ && "$period" -gt 0 ]]; then
        limited=$(( (quota + period - 1) / period ))
        (( limited < 1 )) && limited=1
        (( limited < count )) && count="$limited"
      fi
      break
    done
  fi
  printf '%s' "$count"
}
detect_memory_bytes() {
  local value memory_file
  # Prefer the real container ceiling (cgroup v2, then common cgroup v1 paths).
  for memory_file in /sys/fs/cgroup/memory.max /sys/fs/cgroup/memory/memory.limit_in_bytes /sys/fs/cgroup/memory.limit_in_bytes; do
    [[ -r "$memory_file" ]] || continue
    value="$(cat "$memory_file" 2>/dev/null || true)"
    if [[ "$value" =~ ^[0-9]+$ ]] && (( value > 0 && value < 1152921504606846976 )); then
      printf '%s|container memory limit' "$value"
      return 0
    fi
  done
  # Some LXC configurations do not expose a cgroup ceiling. MemTotal may then
  # describe host-visible RAM, so surface that uncertainty rather than guessing.
  value="$(awk '/^MemTotal:/ { printf "%.0f", $2 * 1024; exit }' /proc/meminfo 2>/dev/null || true)"
  if [[ "$value" =~ ^[0-9]+$ && "$value" -gt 0 ]]; then
    printf '%s|visible system memory (container limit not exposed)' "$value"
    return 0
  fi
  return 1
}
preflight_resources() {
  local cpu_count memory_info memory_bytes memory_source disk_probe disk_stats
  local disk_total_kib disk_free_kib disk_total_bytes disk_free_bytes
  local warnings=0 reply
  ui_section "1/6  System and resource pre-flight"
  ui_ok "Operating system: Debian \${VERSION_ID}; systemd is running."
  ui_ok "Architecture: $(uname -m)."
  cpu_count="$(detect_cpu_count)"
  if (( cpu_count < 2 )); then
    ui_warn "CPU: \${cpu_count} vCPU visible. Increase to at least 2; 4 is recommended for larger fleets."
    warnings=$((warnings + 1))
  elif (( cpu_count < 4 )); then
    ui_ok "CPU: \${cpu_count} vCPU available (2 minimum; 4 recommended for larger fleets)."
  else
    ui_ok "CPU: \${cpu_count} vCPU available."
  fi
  if memory_info="$(detect_memory_bytes)"; then
    memory_bytes="\${memory_info%%|*}"
    memory_source="\${memory_info#*|}"
    if (( memory_bytes < 2147483648 )); then
      ui_warn "RAM: $(format_gib "$memory_bytes") detected (\${memory_source}). Increase the CT to at least 2 GiB; 4 GiB is recommended."
      warnings=$((warnings + 1))
    elif (( memory_bytes < 4294967296 )); then
      ui_ok "RAM: $(format_gib "$memory_bytes") detected (2 GiB baseline met; 4 GiB recommended)."
    else
      ui_ok "RAM: $(format_gib "$memory_bytes") detected."
    fi
    if [[ "$memory_source" == *"not exposed"* ]]; then
      ui_warn "The LXC memory ceiling is not exposed to this installer. Verify assigned RAM in Proxmox; host-visible RAM may exceed the CT limit."
      warnings=$((warnings + 1))
    fi
  else
    ui_warn "RAM: unable to determine memory from cgroups or /proc/meminfo."
    warnings=$((warnings + 1))
  fi
  disk_probe="/var/lib/euryphos"
  while [[ ! -d "$disk_probe" && "$disk_probe" != "/" ]]; do disk_probe="$(dirname "$disk_probe")"; done
  disk_stats="$(df -Pk "$disk_probe" 2>/dev/null | awk 'END { if (NF >= 4) print $2, $4 }')"
  read -r disk_total_kib disk_free_kib <<< "$disk_stats" || true
  if [[ "\${disk_total_kib:-}" =~ ^[0-9]+$ && "\${disk_free_kib:-}" =~ ^[0-9]+$ ]]; then
    disk_total_bytes=$((disk_total_kib * 1024))
    disk_free_bytes=$((disk_free_kib * 1024))
    if (( disk_total_bytes < 10737418240 || disk_free_bytes < 5368709120 )); then
      ui_warn "Disk at \${disk_probe}: $(format_gib "$disk_total_bytes") total, $(format_gib "$disk_free_bytes") available."
      ui_warn "Baseline: at least 10 GiB total and 5 GiB free. Recommended: 20 GiB total and 10 GiB free."
      warnings=$((warnings + 1))
    elif (( disk_total_bytes < 21474836480 || disk_free_bytes < 10737418240 )); then
      ui_ok "Disk at \${disk_probe}: $(format_gib "$disk_total_bytes") total, $(format_gib "$disk_free_bytes") available (baseline met)."
      ui_warn "Recommended: 20 GiB total and 10 GiB free; fleet inventory growth depends on device count and retention."
    else
      ui_ok "Disk at \${disk_probe}: $(format_gib "$disk_total_bytes") total, $(format_gib "$disk_free_bytes") available."
    fi
  else
    ui_warn "Disk: could not read filesystem capacity for \${disk_probe}."
    warnings=$((warnings + 1))
  fi
  if (( warnings > 0 )); then
    printf '\n'
    ui_warn "Pre-flight found \${warnings} warning(s). They do not automatically block a small test deployment, but undersized resources can make builds, startup, and database operations unreliable."
    if [[ -r /dev/tty && -w /dev/tty ]]; then
      printf 'Continue with installation using the detected resources? [y/N] ' > /dev/tty
      read -r reply < /dev/tty || reply=""
      case "$reply" in
        y|Y|yes|YES) ui_warn "Continuing despite resource warnings, as requested." ;;
        *) die "Pre-flight stopped before package installation. Increase CT resources in Proxmox and rerun the installer." ;;
      esac
    else
      ui_warn "No interactive terminal is available; continuing non-interactively with the warnings shown."
    fi
  else
    ui_ok "Resource pre-flight passed the baseline checks."
  fi
}
show_web_access() {
  local listen_addr port addresses ip found=0 cookie_secure
  listen_addr="$(read_env_value EURYPHOS_LISTEN)"
  listen_addr="\${listen_addr:-:8080}"
  port="\${listen_addr##*:}"
  cookie_secure="$(read_env_value EURYPHOS_COOKIE_SECURE)"
  ui_section "6/6  Installation complete"
  ui_ok "Euryphos service is active and passed its local health check."
  printf '  Service:  systemctl status %s\n' "$SERVICE"
  printf '  Logs:     journalctl -u %s -f\n' "$SERVICE"
  printf '  Tokens:   %s (root-only; do not share)\n' "$ENV_FILE"
  printf '  %sWeb access%s\n' "$UI_BOLD" "$UI_RESET"
  addresses="$(hostname -I 2>/dev/null || true)"
  for ip in $addresses; do
    case "$ip" in 127.*|::1|fe80:*) continue ;; esac
    if [[ "$ip" == *:* ]]; then
      printf '  HTTP login page: http://[%s]:%s/login\n' "$ip" "$port"
    else
      printf '  HTTP login page: http://%s:%s/login\n' "$ip" "$port"
    fi
    found=1
  done
  if (( found == 0 )); then
    printf '  HTTP login page: http://<CT-IP>:%s/login\n' "$port"
    printf '  Find the CT address in Proxmox or run: hostname -I\n'
  fi
  if [[ "$cookie_secure" == "true" ]]; then
    printf '\n'
    ui_warn "Secure cookies are enabled. The login page can be viewed over HTTP, but authenticated browser sessions require HTTPS."
    ui_warn "Configure a trusted HTTPS reverse proxy and use https://<your-hostname>/login. This installer does not configure TLS."
  else
    printf '\n'
    ui_warn "Secure cookies are disabled in the existing configuration. Use this HTTP URL only on an isolated trusted network; HTTPS is required before exposure."
  fi
  ui_warn "Do not expose the raw HTTP listener on port \${port} directly to the public internet."
}

[[ "$MODE" == "install" || "$MODE" == "upgrade" ]] || die "Usage: install.sh [install|upgrade]"
log "Installer revision ${INSTALLER_REVISION}."

[[ "${EUID}" -eq 0 ]] || die "Run as root inside the Debian 13 Proxmox CT."
[[ -r /etc/os-release ]] || die "Cannot identify the operating system."
. /etc/os-release
[[ "${ID:-}" == "debian" && "${VERSION_ID:-}" == "13" ]] || die "This installer targets Debian 13 only."
[[ -d /run/systemd/system ]] || die "systemd is not running. Enable systemd in the Proxmox CT before installing."

ui_header
log "Mode: ${MODE}"
preflight_resources

ui_section "2/6  Install operating-system dependencies"
export DEBIAN_FRONTEND=noninteractive
log "Refreshing Debian package indexes."
apt-get update
log "Installing required runtime and verification tools."
apt-get install -y --no-install-recommends ca-certificates curl jq openssl sqlite3

for command_name in curl jq openssl sqlite3 sha256sum systemctl su; do
  command -v "$command_name" >/dev/null 2>&1 || die "Required command not found after package installation: $command_name"
done

if ! getent passwd euryphos >/dev/null 2>&1; then
  useradd --system --home-dir "$DATA_DIR" --create-home --shell /usr/sbin/nologin --user-group euryphos
fi

install -d -o root -g root -m 0755 "$INSTALL_ROOT" "$RELEASES_DIR"
install -d -o root -g root -m 0750 "$CONFIG_DIR"
install -d -o euryphos -g euryphos -m 0700 "$DATA_DIR"
install -d -o root -g root -m 0700 "$BACKUP_DIR"

if [[ -f "$ENV_FILE" ]]; then
  [[ "$MODE" != "upgrade" || -s "$ENV_FILE" ]] || die "The existing configuration file is empty; refusing to replace it."
  grep -q '^EURYPHOS_ADMIN_TOKEN=.' "$ENV_FILE" || die "Existing configuration is missing EURYPHOS_ADMIN_TOKEN; refusing to rotate it."
  grep -q '^EURYPHOS_ENROLLMENT_TOKEN=.' "$ENV_FILE" || die "Existing configuration is missing EURYPHOS_ENROLLMENT_TOKEN; refusing to rotate it."
  log "Existing configuration found; secrets and settings will be preserved."
else
  [[ "$MODE" != "upgrade" ]] || die "No existing installation configuration found; use install for a first installation."
  tmp_config="$(mktemp "${CONFIG_DIR}/euryphos.env.XXXXXX")"
  cat > "$tmp_config" <<EOF
EURYPHOS_ADMIN_TOKEN=$(openssl rand -hex 32)
EURYPHOS_ENROLLMENT_TOKEN=$(openssl rand -hex 32)
EURYPHOS_DB_PATH=${DATA_DIR}/euryphos.db
EURYPHOS_LISTEN=:8080
EURYPHOS_AUTO_MIGRATE=true
EURYPHOS_COOKIE_SECURE=true
EURYPHOS_ONLINE_GRACE_SECONDS=90
EURYPHOS_JOB_STALE_SECONDS=180
EOF
  chown root:root "$tmp_config"
  chmod 0600 "$tmp_config"
  mv "$tmp_config" "$ENV_FILE"
  log "Created first-install secrets and configuration at $ENV_FILE."
fi

read_env_value() {
  local key="$1"
  awk -F= -v key="$key" '$1 == key { value=substr($0,index($0,"=")+1) } END { print value }' "$ENV_FILE"
}

DB_PATH="$(read_env_value EURYPHOS_DB_PATH)"
DB_PATH="${DB_PATH:-${DATA_DIR}/euryphos.db}"
DB_PATH="${DB_PATH#\"}"
DB_PATH="${DB_PATH%\"}"
[[ "$DB_PATH" == /var/lib/euryphos/* ]] || die "EURYPHOS_DB_PATH must remain under /var/lib/euryphos for service sandboxing and automatic backups."
[[ "$DB_PATH" != *"'"* && "$DB_PATH" != *$'\n'* ]] || die "EURYPHOS_DB_PATH contains unsupported characters."
install -d -o euryphos -g euryphos -m 0700 "$(dirname "$DB_PATH")"

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "Unsupported Proxmox CT architecture: $(uname -m). Supported architectures are amd64 and arm64." ;;
esac

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
ARCHIVE="euryphos-linux-${ARCH}.tar.gz"

ui_section "3/6  Obtain server software"
if [[ "${EURYPHOS_SOURCE_BUILD:-0}" == "1" ]]; then
  log "Source-build mode enabled; resolving the current main commit."
  apt-get install -y --no-install-recommends golang-go

  GO_VERSION="$(go env GOVERSION)"
  if [[ "$GO_VERSION" =~ ^go([0-9]+)\.([0-9]+) ]]; then
    GO_MAJOR="${BASH_REMATCH[1]}"
    GO_MINOR="${BASH_REMATCH[2]}"
  else
    die "Could not parse the installed Go version: $GO_VERSION"
  fi
  if (( GO_MAJOR < 1 || (GO_MAJOR == 1 && GO_MINOR < 23) )); then
    die "Source builds require Go 1.23 or later; found $GO_VERSION."
  fi

  api_headers=(-H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28")
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    api_headers+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  fi
  SOURCE_SHA="$(curl -fsSL "${api_headers[@]}" "${API}/commits/main" | jq -er '.sha')" || die "Could not resolve the current public main commit."
  [[ "$SOURCE_SHA" =~ ^[[:xdigit:]]{40}$ ]] || die "GitHub returned an invalid source commit SHA."

  SOURCE_DIR="${TMP_DIR}/source"
  PACKAGE_DIR="${TMP_DIR}/package"
  mkdir -m 0700 "$SOURCE_DIR" "$PACKAGE_DIR"
  log "Downloading source commit ${SOURCE_SHA:0:12} and building Linux $ARCH."
  curl -fsSL "${API}/tarball/${SOURCE_SHA}" -o "${TMP_DIR}/source.tar.gz" || die "Could not download the source archive."
  tar -tzf "${TMP_DIR}/source.tar.gz" >/dev/null || die "The source archive is invalid."
  tar --no-same-owner --no-same-permissions -xzf "${TMP_DIR}/source.tar.gz" --strip-components=1 -C "$SOURCE_DIR"
  for required_source in go.mod cmd/euryphos-server/main.go deploy/euryphos.service LICENSE; do
    [[ -f "${SOURCE_DIR}/${required_source}" ]] || die "Source archive is missing ${required_source}."
  done

  if ! (
    cd "$SOURCE_DIR"
    GOTOOLCHAIN=local GOMODCACHE="${TMP_DIR}/gomodcache" GOCACHE="${TMP_DIR}/gocache" \
      CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" \
      go build -trimpath -ldflags="-s -w" -o "${TMP_DIR}/euryphos-server" ./cmd/euryphos-server
  ); then
    die "Building the Euryphos server from source failed."
  fi

  install -m 0755 "${TMP_DIR}/euryphos-server" "${PACKAGE_DIR}/euryphos-server"
  install -m 0644 "${SOURCE_DIR}/deploy/euryphos.service" "${PACKAGE_DIR}/euryphos.service"
  install -m 0644 "${SOURCE_DIR}/LICENSE" "${PACKAGE_DIR}/LICENSE"
  VERSION="main-${SOURCE_SHA:0:12}"
  printf '%s\n' "$VERSION" > "${PACKAGE_DIR}/VERSION"
  tar -C "$PACKAGE_DIR" -czf "${TMP_DIR}/${ARCHIVE}" .
  EXPECTED_SHA="$(sha256sum "${TMP_DIR}/${ARCHIVE}" | awk '{ print $1 }')"
  log "Built test package $VERSION from source."
else
  api_headers=(-H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28")
  asset_headers=(-H "Accept: application/octet-stream" -H "X-GitHub-Api-Version: 2022-11-28")
  if [[ -n "${GITHUB_TOKEN:-}" ]]; then
    api_headers+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
    asset_headers+=(-H "Authorization: Bearer ${GITHUB_TOKEN}")
  fi

  if [[ -n "${EURYPHOS_VERSION:-}" ]]; then
    RELEASE_URL="${API}/releases/tags/${EURYPHOS_VERSION}"
  else
    RELEASE_URL="${API}/releases/latest"
  fi

  log "Resolving the GitHub release."
  if ! RELEASE_JSON="$(curl -fsSL "${api_headers[@]}" "$RELEASE_URL")"; then
    die "Could not read the requested GitHub release. A published release is required; use EURYPHOS_SOURCE_BUILD=1 for an untagged test build."
  fi

  VERSION="$(jq -er '.tag_name' <<< "$RELEASE_JSON")" || die "GitHub response did not contain a release tag."
  ARCHIVE_ID="$(jq -er --arg name "$ARCHIVE" '.assets[] | select(.name == $name) | .id' <<< "$RELEASE_JSON")" || die "Release $VERSION has no asset named $ARCHIVE."
  CHECKSUM_ID="$(jq -er '.assets[] | select(.name == "SHA256SUMS") | .id' <<< "$RELEASE_JSON")" || die "Release $VERSION has no SHA256SUMS asset."

  log "Downloading $VERSION for Linux $ARCH."
  curl -fsSL -L "${asset_headers[@]}" "${API}/releases/assets/${ARCHIVE_ID}" -o "${TMP_DIR}/${ARCHIVE}"
  curl -fsSL -L "${asset_headers[@]}" "${API}/releases/assets/${CHECKSUM_ID}" -o "${TMP_DIR}/SHA256SUMS"

  EXPECTED_SHA="$(awk -v name="$ARCHIVE" '$2 == name { print $1; exit }' "${TMP_DIR}/SHA256SUMS")"
fi

ui_section "4/6  Verify and stage server package"
[[ "$EXPECTED_SHA" =~ ^[[:xdigit:]]{64}$ ]] || die "The expected SHA-256 checksum is missing or invalid."
printf '%s  %s\n' "$EXPECTED_SHA" "$ARCHIVE" | (cd "$TMP_DIR" && sha256sum -c -) || die "Package checksum verification failed."

VERSION_DIR_NAME="${VERSION//[^A-Za-z0-9._-]/_}-${EXPECTED_SHA:0:12}"
NEW_RELEASE="${RELEASES_DIR}/${VERSION_DIR_NAME}"
if [[ ! -d "$NEW_RELEASE" ]]; then
  STAGE_DIR="${RELEASES_DIR}/.${VERSION_DIR_NAME}.tmp.$$"
  mkdir -m 0755 "$STAGE_DIR"
  if ! tar -tzf "${TMP_DIR}/${ARCHIVE}" > "${TMP_DIR}/archive.list"; then
    rm -rf "$STAGE_DIR"
    die "Could not read the GitHub release archive."
  fi
  if grep -Eq '(^/|(^|/)\.\.(/|$))' "${TMP_DIR}/archive.list"; then
    rm -rf "$STAGE_DIR"
    die "Release archive contains an unsafe path."
  fi
  for required_file in ./euryphos-server ./euryphos.service ./LICENSE ./VERSION; do
    grep -Fxq "$required_file" "${TMP_DIR}/archive.list" || {
      rm -rf "$STAGE_DIR"
      die "Release archive is missing $required_file."
    }
  done
  if ! tar --no-same-owner --no-same-permissions -xzf "${TMP_DIR}/${ARCHIVE}" -C "$STAGE_DIR"; then
    rm -rf "$STAGE_DIR"
    die "Could not unpack the GitHub release."
  fi
  [[ -x "${STAGE_DIR}/euryphos-server" ]] || { rm -rf "$STAGE_DIR"; die "Release archive is missing euryphos-server."; }
  [[ -f "${STAGE_DIR}/euryphos.service" ]] || { rm -rf "$STAGE_DIR"; die "Release archive is missing euryphos.service."; }
  [[ "$(cat "${STAGE_DIR}/VERSION")" == "$VERSION" ]] || { rm -rf "$STAGE_DIR"; die "Release archive version does not match the GitHub release tag."; }
  chmod 0755 "${STAGE_DIR}/euryphos-server"
  chown -R root:root "$STAGE_DIR"
  # The installer runs with umask 077; explicitly restore traversal permissions
  # after staging so the unprivileged systemd account can enter the release.
  chmod 0755 "$STAGE_DIR"
  mv "$STAGE_DIR" "$NEW_RELEASE"
fi

# Also repair a previously staged release that was created with the restrictive
# umask by an earlier installer revision. The executable bit alone is not enough:
# every parent directory must be searchable by the service account.
chmod 0755 "$INSTALL_ROOT" "$RELEASES_DIR" "$NEW_RELEASE"
chmod 0755 "${NEW_RELEASE}/euryphos-server"
chown root:root "$NEW_RELEASE" "${NEW_RELEASE}/euryphos-server"
if ! su -s /bin/sh euryphos -c 'test -x "$1" && test -r "$1"' sh "${NEW_RELEASE}/euryphos-server"; then
  die "The euryphos service account cannot traverse to or execute the staged server binary; refusing to switch the active installation."
fi
log "Verified that the euryphos service account can access the server binary."

ui_section "5/6  Preserve data and activate service"
OLD_TARGET="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
if [[ -e "$CURRENT_LINK" && ! -L "$CURRENT_LINK" ]]; then
  die "$CURRENT_LINK exists but is not a symbolic link; refusing to replace it."
fi

# Refresh systemd's view before deciding whether there is a service to stop.
systemctl daemon-reload || die "Could not reload systemd unit definitions."

UNIT_LOAD_STATE="$(systemctl show "$SERVICE" --property=LoadState --value 2>/dev/null || true)"
UNIT_ACTIVE_STATE="not-loaded"
if [[ "$UNIT_LOAD_STATE" == "loaded" ]]; then
  UNIT_ACTIVE_STATE="$(systemctl show "$SERVICE" --property=ActiveState --value 2>/dev/null || true)"
fi

WAS_INSTALLED=0
SERVICE_WAS_ACTIVE=0
if [[ -n "$OLD_TARGET" || "$UNIT_LOAD_STATE" == "loaded" ]]; then
  WAS_INSTALLED=1
fi

case "$UNIT_ACTIVE_STATE" in
  active|activating|reloading|deactivating)
    SERVICE_WAS_ACTIVE=1
    log "Stopping the active Euryphos service before backing up SQLite."
    systemctl stop "$SERVICE" || die "Could not stop active $SERVICE; the existing installation was left in place."
    ;;
  inactive|failed|dead|not-loaded)
    log "No running Euryphos service needs to be stopped (systemd state: $UNIT_ACTIVE_STATE)."
    ;;
  *)
    die "Cannot safely determine whether $SERVICE is running (load state: $UNIT_LOAD_STATE; active state: $UNIT_ACTIVE_STATE)."
    ;;
esac

BACKUP_PATH=""
if [[ -s "$DB_PATH" ]]; then
  BACKUP_PATH="${BACKUP_DIR}/euryphos-$(date -u +%Y%m%dT%H%M%SZ)-${VERSION_DIR_NAME}.sqlite"
  log "Creating a consistent SQLite backup at $BACKUP_PATH."
  if ! sqlite3 "$DB_PATH" ".backup '$BACKUP_PATH'"; then
    if [[ "$SERVICE_WAS_ACTIVE" -eq 1 ]]; then systemctl start "$SERVICE" || true; fi
    die "SQLite backup failed; the active application version was not changed."
  fi
  BACKUP_CHECK="$(sqlite3 "$BACKUP_PATH" 'PRAGMA integrity_check;' 2>/dev/null || true)"
  if [[ "$BACKUP_CHECK" != "ok" ]]; then
    rm -f "$BACKUP_PATH"
    if [[ "$SERVICE_WAS_ACTIVE" -eq 1 ]]; then systemctl start "$SERVICE" || true; fi
    die "SQLite backup integrity check failed; the active application version was not changed."
  fi
  chown root:root "$BACKUP_PATH"
  chmod 0600 "$BACKUP_PATH"
fi

atomic_link() {
  local target="$1"
  local temporary="${CURRENT_LINK}.tmp.$$"
  rm -f "$temporary"
  ln -s "$target" "$temporary"
  mv -Tf "$temporary" "$CURRENT_LINK"
}

restore_previous_installation() {
  log "The new service did not become healthy; restoring the previous installation."
  systemctl stop "$SERVICE" >/dev/null 2>&1 || true
  if [[ -n "$OLD_TARGET" && -x "${OLD_TARGET}/euryphos-server" ]]; then
    atomic_link "$OLD_TARGET"
    if [[ -f "${OLD_TARGET}/euryphos.service" ]]; then
      install -o root -g root -m 0644 "${OLD_TARGET}/euryphos.service" "/etc/systemd/system/${SERVICE}"
    fi
  else
    rm -f "$CURRENT_LINK"
  fi
  if [[ -n "$BACKUP_PATH" && -s "$BACKUP_PATH" ]]; then
    rm -f "${DB_PATH}-wal" "${DB_PATH}-shm"
    install -o euryphos -g euryphos -m 0600 "$BACKUP_PATH" "$DB_PATH"
    log "Restored the pre-upgrade SQLite backup."
  fi
  systemctl daemon-reload
  if [[ -n "$OLD_TARGET" && -x "${OLD_TARGET}/euryphos-server" ]]; then
    if [[ "$SERVICE_WAS_ACTIVE" -eq 1 ]]; then
      systemctl start "$SERVICE" || log "WARNING: the previous service version did not restart automatically."
    else
      log "The previous service was not running before the update; leaving it stopped after rollback."
    fi
  else
    systemctl disable "$SERVICE" >/dev/null 2>&1 || true
  fi
}

show_service_diagnostics() {
  log "Service diagnostics:"
  systemctl status "$SERVICE" --no-pager -l 2>&1 | tail -n 60 || true
  log "Recent service journal:"
  journalctl -u "$SERVICE" -n 80 --no-pager 2>&1 | tail -n 80 || true
}

atomic_link "$NEW_RELEASE"
install -o root -g root -m 0644 "${NEW_RELEASE}/euryphos.service" "/etc/systemd/system/${SERVICE}"
log "Reloading systemd and enabling ${SERVICE}."
systemctl daemon-reload || die "systemd could not reload the Euryphos service definition."
systemctl enable "$SERVICE" || die "systemd could not enable ${SERVICE}."
log "Requesting service startup (non-blocking); the installer will report health-check progress."
if ! systemctl start --no-block "$SERVICE"; then
  show_service_diagnostics
  restore_previous_installation
  die "systemd could not queue the new service start. The previous installation was restored when available."
fi

LISTEN_ADDR="$(read_env_value EURYPHOS_LISTEN)"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"
PORT="${LISTEN_ADDR##*:}"
[[ "$PORT" =~ ^[0-9]{1,5}$ && "$PORT" -gt 0 && "$PORT" -le 65535 ]] || {
  show_service_diagnostics
  restore_previous_installation
  die "EURYPHOS_LISTEN must end in a TCP port number for the installer health check."
}
log "Waiting for the local health endpoint on 127.0.0.1:${PORT} (up to 120 seconds)."
HEALTHY=0
for attempt in {1..120}; do
  if curl -fsS --connect-timeout 2 --max-time 3 "http://127.0.0.1:${PORT}/api/health" 2>/dev/null | jq -e '.status == "ok"' >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  if (( attempt % 10 == 0 )); then
    ACTIVE_STATE="$(systemctl show "$SERVICE" --property=ActiveState --value 2>/dev/null || true)"
    SUB_STATE="$(systemctl show "$SERVICE" --property=SubState --value 2>/dev/null || true)"
    log "Health check pending (${attempt}/120 seconds; systemd state: ${ACTIVE_STATE:-unknown}/${SUB_STATE:-unknown})."
    if [[ "$ACTIVE_STATE" == "failed" ]]; then
      break
    fi
  fi
  sleep 1
done

if [[ "$HEALTHY" -ne 1 ]]; then
  show_service_diagnostics
  restore_previous_installation
  die "Health check failed for $VERSION. The diagnostics above show why the service did not become healthy."
fi

chown euryphos:euryphos "$DATA_DIR"
chmod 0700 "$DATA_DIR"
log "Euryphos $VERSION is active and passed its local SQLite health check."
if [[ -n "$BACKUP_PATH" ]]; then
  log "Pre-upgrade database backup retained at $BACKUP_PATH."
fi
if [[ "$WAS_INSTALLED" -eq 0 ]]; then
  log "Initial administrator token is stored in $ENV_FILE."
  log "Read it with: sudo grep '^EURYPHOS_ADMIN_TOKEN=' $ENV_FILE"
  log "The enrollment token is stored in the same root-only file."
fi
log "Service status: systemctl status $SERVICE"
log "Logs: journalctl -u $SERVICE -f"
show_web_access
