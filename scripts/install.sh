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

log() { printf '[euryphos] %s\n' "$*"; }
die() { printf '[euryphos] ERROR: %s\n' "$*" >&2; exit 1; }

[[ "$MODE" == "install" || "$MODE" == "upgrade" ]] || die "Usage: install.sh [install|upgrade]"

[[ "${EUID}" -eq 0 ]] || die "Run as root inside the Debian 13 Proxmox CT."
[[ -r /etc/os-release ]] || die "Cannot identify the operating system."
. /etc/os-release
[[ "${ID:-}" == "debian" && "${VERSION_ID:-}" == "13" ]] || die "This installer targets Debian 13 only."
[[ -d /run/systemd/system ]] || die "systemd is not running. Enable systemd in the Proxmox CT before installing."

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends ca-certificates curl jq openssl sqlite3

for command_name in curl jq openssl sqlite3 sha256sum systemctl; do
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
  die "Could not read the requested GitHub release. For a private repository, supply a read-only GitHub token in GITHUB_TOKEN. A release must be published before installation."
fi

VERSION="$(jq -er '.tag_name' <<< "$RELEASE_JSON")" || die "GitHub response did not contain a release tag."
ARCHIVE="euryphos-linux-${ARCH}.tar.gz"
ARCHIVE_ID="$(jq -er --arg name "$ARCHIVE" '.assets[] | select(.name == $name) | .id' <<< "$RELEASE_JSON")" || die "Release $VERSION has no asset named $ARCHIVE."
CHECKSUM_ID="$(jq -er '.assets[] | select(.name == "SHA256SUMS") | .id' <<< "$RELEASE_JSON")" || die "Release $VERSION has no SHA256SUMS asset."

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
log "Downloading $VERSION for Linux $ARCH."
curl -fsSL -L "${asset_headers[@]}" "${API}/releases/assets/${ARCHIVE_ID}" -o "${TMP_DIR}/${ARCHIVE}"
curl -fsSL -L "${asset_headers[@]}" "${API}/releases/assets/${CHECKSUM_ID}" -o "${TMP_DIR}/SHA256SUMS"

EXPECTED_SHA="$(awk -v name="$ARCHIVE" '$2 == name { print $1; exit }' "${TMP_DIR}/SHA256SUMS")"
[[ "$EXPECTED_SHA" =~ ^[[:xdigit:]]{64}$ ]] || die "The release checksum manifest does not contain a valid checksum for $ARCHIVE."
printf '%s  %s\n' "$EXPECTED_SHA" "$ARCHIVE" | (cd "$TMP_DIR" && sha256sum -c -) || die "Release checksum verification failed."

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
  mv "$STAGE_DIR" "$NEW_RELEASE"
fi

OLD_TARGET="$(readlink -f "$CURRENT_LINK" 2>/dev/null || true)"
if [[ -e "$CURRENT_LINK" && ! -L "$CURRENT_LINK" ]]; then
  die "$CURRENT_LINK exists but is not a symbolic link; refusing to replace it."
fi

WAS_INSTALLED=0
if [[ -n "$OLD_TARGET" || -f "/etc/systemd/system/${SERVICE}" ]]; then
  WAS_INSTALLED=1
fi

if [[ "$WAS_INSTALLED" -eq 1 ]]; then
  log "Stopping the existing service before backing up SQLite."
  systemctl stop "$SERVICE" || die "Could not stop $SERVICE; the existing installation was left in place."
fi

BACKUP_PATH=""
if [[ -s "$DB_PATH" ]]; then
  BACKUP_PATH="${BACKUP_DIR}/euryphos-$(date -u +%Y%m%dT%H%M%SZ)-${VERSION_DIR_NAME}.sqlite"
  log "Creating a consistent SQLite backup at $BACKUP_PATH."
  if ! sqlite3 "$DB_PATH" ".backup '$BACKUP_PATH'"; then
    [[ "$WAS_INSTALLED" -eq 0 ]] || systemctl start "$SERVICE" || true
    die "SQLite backup failed; the active application version was not changed."
  fi
  BACKUP_CHECK="$(sqlite3 "$BACKUP_PATH" 'PRAGMA integrity_check;' 2>/dev/null || true)"
  if [[ "$BACKUP_CHECK" != "ok" ]]; then
    rm -f "$BACKUP_PATH"
    [[ "$WAS_INSTALLED" -eq 0 ]] || systemctl start "$SERVICE" || true
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
    systemctl start "$SERVICE" || log "WARNING: the previous service version did not restart automatically."
  else
    systemctl disable "$SERVICE" >/dev/null 2>&1 || true
  fi
}

atomic_link "$NEW_RELEASE"
install -o root -g root -m 0644 "${NEW_RELEASE}/euryphos.service" "/etc/systemd/system/${SERVICE}"
systemctl daemon-reload
systemctl enable "$SERVICE"
if ! systemctl start "$SERVICE"; then
  restore_previous_installation
  die "The new service failed to start. The previous installation was restored when available."
fi

LISTEN_ADDR="$(read_env_value EURYPHOS_LISTEN)"
LISTEN_ADDR="${LISTEN_ADDR:-:8080}"
PORT="${LISTEN_ADDR##*:}"
[[ "$PORT" =~ ^[0-9]{1,5}$ && "$PORT" -gt 0 && "$PORT" -le 65535 ]] || {
  restore_previous_installation
  die "EURYPHOS_LISTEN must end in a TCP port number for the installer health check."
}
HEALTHY=0
for attempt in {1..120}; do
  if curl -fsS --max-time 2 "http://127.0.0.1:${PORT}/api/health" 2>/dev/null | jq -e '.status == "ok"' >/dev/null 2>&1; then
    HEALTHY=1
    break
  fi
  sleep 1
done

if [[ "$HEALTHY" -ne 1 ]]; then
  restore_previous_installation
  die "Health check failed for $VERSION. Check 'journalctl -u $SERVICE' for details."
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
