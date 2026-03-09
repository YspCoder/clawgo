#!/usr/bin/env bash
set -euo pipefail

OWNER="YspCoder"
REPO="clawgo"
BIN="clawgo"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="$HOME/.clawgo"
CONFIG_PATH="$CONFIG_DIR/config.json"
WORKSPACE_DIR="$HOME/.clawgo/workspace"
LEGACY_WORKSPACE_DIR="$HOME/.openclaw/workspace"

usage() {
  cat <<EOF
Usage: $0

Install or upgrade ClawGo from the latest GitHub release.

Notes:
  - WebUI is embedded in the binary and initialized when you run 'clawgo onboard'.
  - OpenClaw migration is offered only when a legacy workspace is detected.
EOF
}

log() {
  printf '%s\n' "$*"
}

warn() {
  printf 'Warning: %s\n' "$*" >&2
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    warn "Missing required command: $1"
    exit 1
  fi
}

prompt_yes_no() {
  local prompt="$1"
  local default="${2:-N}"
  local reply
  if [[ ! -r /dev/tty ]]; then
    [[ "$default" =~ ^[Yy]$ ]]
    return
  fi
  if [[ "$default" =~ ^[Yy]$ ]]; then
    read -r -p "$prompt [Y/n]: " reply < /dev/tty || true
    reply="${reply:-Y}"
  else
    read -r -p "$prompt [y/N]: " reply < /dev/tty || true
    reply="${reply:-N}"
  fi
  [[ "$reply" =~ ^[Yy]$ ]]
}

tty_read() {
  local __var_name="$1"
  local __prompt="$2"
  local __default="${3:-}"
  local __silent="${4:-0}"
  local __reply=""

  if [[ ! -r /dev/tty ]]; then
    printf -v "$__var_name" '%s' "$__default"
    return
  fi

  if [[ "$__silent" == "1" ]]; then
    read -r -s -p "$__prompt" __reply < /dev/tty || true
    echo > /dev/tty
  else
    read -r -p "$__prompt" __reply < /dev/tty || true
  fi
  __reply="${__reply:-$__default}"
  printf -v "$__var_name" '%s' "$__reply"
}

detect_platform() {
  OS="$(uname | tr '[:upper:]' '[:lower:]')"
  ARCH="$(uname -m)"
  case "$ARCH" in
    x86_64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
      warn "Unsupported architecture: $ARCH"
      exit 1
      ;;
  esac
  case "$OS" in
    linux|darwin) ;;
    *)
      warn "Unsupported operating system: $OS"
      exit 1
      ;;
  esac
  log "Detected OS=$OS ARCH=$ARCH"
}

fetch_latest_tag() {
  require_cmd curl
  local api="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
  TAG="$(curl -fsSL "$api" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "${TAG:-}" ]]; then
    warn "Unable to get latest release tag from GitHub"
    exit 1
  fi
  log "Latest Release: $TAG"
}

install_binary() {
  local file="${BIN}-${OS}-${ARCH}.tar.gz"
  local url="https://github.com/$OWNER/$REPO/releases/download/$TAG/$file"
  local out="$TMPDIR/$file"

  log "Downloading $file..."
  curl -fSL "$url" -o "$out"
  tar -xzf "$out" -C "$TMPDIR"

  local extracted_bin=""
  if [[ -f "$TMPDIR/$BIN" ]]; then
    extracted_bin="$TMPDIR/$BIN"
  elif [[ -f "$TMPDIR/${BIN}-${OS}-${ARCH}" ]]; then
    extracted_bin="$TMPDIR/${BIN}-${OS}-${ARCH}"
  else
    extracted_bin="$(find "$TMPDIR" -maxdepth 3 -type f \( -name "$BIN" -o -name "${BIN}-${OS}-${ARCH}" -o -name "${BIN}-*" \) ! -name "*.tar.gz" ! -name "*.zip" | head -n1)"
  fi

  if [[ -z "$extracted_bin" || ! -f "$extracted_bin" ]]; then
    warn "Failed to locate extracted binary from $file"
    exit 1
  fi

  chmod +x "$extracted_bin"
  if command -v "$BIN" >/dev/null 2>&1 || [[ -f "$INSTALL_DIR/$BIN" ]]; then
    log "Updating existing $BIN in $INSTALL_DIR ..."
  else
    log "Installing $BIN to $INSTALL_DIR ..."
  fi
  sudo mv "$extracted_bin" "$INSTALL_DIR/$BIN"
  log "Installed $BIN to $INSTALL_DIR/$BIN"
}

migrate_local_openclaw() {
  local src="${1:-$LEGACY_WORKSPACE_DIR}"
  local dst="${2:-$WORKSPACE_DIR}"

  if [[ ! -d "$src" ]]; then
    warn "OpenClaw workspace not found: $src"
    return 1
  fi

  log "[INFO] source: $src"
  log "[INFO] target: $dst"
  mkdir -p "$dst" "$dst/memory"
  local ts
  ts="$(date -u +%Y%m%dT%H%M%SZ)"
  local backup_dir="$dst/.migration-backup-$ts"
  mkdir -p "$backup_dir"

  for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
    if [[ -f "$dst/$f" ]]; then
      cp -a "$dst/$f" "$backup_dir/$f"
    fi
  done
  if [[ -d "$dst/memory" ]]; then
    cp -a "$dst/memory" "$backup_dir/memory" || true
  fi

  for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
    if [[ -f "$src/$f" ]]; then
      cp -a "$src/$f" "$dst/$f"
      log "[OK] migrated $f"
    fi
  done
  if [[ -d "$src/memory" ]]; then
    rsync -a "$src/memory/" "$dst/memory/"
    log "[OK] migrated memory/"
  fi

  log "[DONE] local migration complete"
}

migrate_remote_openclaw() {
  require_cmd sshpass
  require_cmd scp
  require_cmd ssh

  local remote_host remote_port remote_pass migration_script
  tty_read remote_host "Enter remote host (e.g. user@hostname): "
  tty_read remote_port "Enter remote port (default 22): " "22"
  tty_read remote_pass "Enter remote password: " "" "1"

  migration_script="$TMPDIR/openclaw2clawgo.sh"
  cat > "$migration_script" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SRC_DEFAULT="$HOME/.openclaw/workspace"
DST_DEFAULT="$HOME/.clawgo/workspace"
SRC="${1:-$SRC_DEFAULT}"
DST="${2:-$DST_DEFAULT}"

mkdir -p "$DST" "$DST/memory"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_DIR="$DST/.migration-backup-$TS"
mkdir -p "$BACKUP_DIR"

for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
  if [[ -f "$DST/$f" ]]; then
    cp -a "$DST/$f" "$BACKUP_DIR/$f"
  fi
done
if [[ -d "$DST/memory" ]]; then
  cp -a "$DST/memory" "$BACKUP_DIR/memory" || true
fi

for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
  if [[ -f "$SRC/$f" ]]; then
    cp -a "$SRC/$f" "$DST/$f"
    echo "[OK] migrated $f"
  fi
done
if [[ -d "$SRC/memory" ]]; then
  rsync -a "$SRC/memory/" "$DST/memory/"
  echo "[OK] migrated memory/"
fi
echo "[DONE] remote migration complete"
EOF

  chmod +x "$migration_script"
  sshpass -p "$remote_pass" scp -P "$remote_port" "$migration_script" "$remote_host:/tmp/openclaw2clawgo.sh"
  sshpass -p "$remote_pass" ssh -p "$remote_port" "$remote_host" "bash /tmp/openclaw2clawgo.sh"
  log "[INFO] Remote migration completed."
}

offer_openclaw_migration() {
  if [[ ! -d "$LEGACY_WORKSPACE_DIR" ]]; then
    return
  fi
  if ! prompt_yes_no "Detected OpenClaw workspace at $LEGACY_WORKSPACE_DIR. Migrate it to ClawGo now?" "N"; then
    return
  fi

  local migration_type
  if [[ -r /dev/tty ]]; then
    log "Choose migration type:"
    log "  1. Local migration"
    log "  2. Remote migration"
    tty_read migration_type "Enter your choice (1 or 2): " "1"
  else
    migration_type="1"
  fi

  case "$migration_type" in
    1)
      warn "Migration will overwrite files in $WORKSPACE_DIR when names collide."
      if prompt_yes_no "Continue local migration?" "N"; then
        migrate_local_openclaw
      else
        log "Migration skipped."
      fi
      ;;
    2)
      migrate_remote_openclaw
      ;;
    *)
      log "Invalid choice. Skipping migration."
      ;;
  esac
}

offer_onboard() {
  log "Refreshing embedded WebUI assets..."
  "$INSTALL_DIR/$BIN" onboard --sync-webui >/dev/null 2>&1 || warn "Failed to refresh embedded WebUI assets automatically. You can run 'clawgo onboard --sync-webui' later."

  if [[ -f "$CONFIG_PATH" ]]; then
    log "Existing config detected at $CONFIG_PATH"
    log "WebUI assets were refreshed from the embedded bundle."
    log "Run 'clawgo onboard' only if you want to regenerate config or missing workspace templates."
    return
  fi

  log "WebUI assets were refreshed from the embedded bundle."
  if prompt_yes_no "No config found. Run 'clawgo onboard' now?" "N"; then
    "$INSTALL_DIR/$BIN" onboard
  else
    log "Skipped onboard. Run 'clawgo onboard' when you are ready."
  fi
}

main() {
  if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
  fi

  detect_platform
  fetch_latest_tag

  TMPDIR="$(mktemp -d)"
  trap 'rm -rf "$TMPDIR"' EXIT

  install_binary
  offer_openclaw_migration
  offer_onboard

  log "Done."
  log "Run 'clawgo --help' to verify the installation."
}

main "$@"
