#!/usr/bin/env bash
set -e

# ====================
# Config
# ====================
OWNER="YspCoder"
REPO="clawgo"
BIN="clawgo"
INSTALL_DIR="/usr/local/bin"
WEBUI_DIR="$HOME/.clawgo/workspace/webui"

# ====================
# Detect OS/ARCH
# ====================
OS="$(uname | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH"
    exit 1
    ;;
esac

echo "Detected OS=$OS ARCH=$ARCH"

# ====================
# Check if already installed
# ====================
if command -v "$BIN" &> /dev/null; then
  echo "$BIN is already installed. Removing existing version..."
  sudo rm -f "$INSTALL_DIR/$BIN"
fi

# ====================
# Get Latest GitHub Release
# ====================
echo "Fetching latest release..."
API="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
TAG=$(curl -s "$API" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$TAG" ]; then
  echo "Unable to get latest release tag from GitHub"
  exit 1
fi

echo "Latest Release: $TAG"

# ====================
# Construct Download URL for the binary and WebUI
# ====================
FILE="${BIN}-${OS}-${ARCH}.tar.gz"
WEBUI_FILE="webui.tar.gz"
URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/$FILE"
WEBUI_URL="https://github.com/$OWNER/$REPO/releases/download/$TAG/$WEBUI_FILE"

echo "Trying to download: $URL"

# Try to download binary release
TMPDIR="$(mktemp -d)"
OUT="$TMPDIR/$FILE"

# Now try downloading the file
if curl -fSL "$URL" -o "$OUT"; then
  echo "Downloaded $FILE"
  tar -xzf "$OUT" -C "$TMPDIR"

  EXTRACTED_BIN=""
  if [[ -f "$TMPDIR/$BIN" ]]; then
    EXTRACTED_BIN="$TMPDIR/$BIN"
  else
    EXTRACTED_BIN="$(find "$TMPDIR" -maxdepth 2 -type f -name "${BIN}*" ! -name "*.tar.gz" ! -name "*.zip" | head -n1)"
  fi

  if [[ -z "$EXTRACTED_BIN" || ! -f "$EXTRACTED_BIN" ]]; then
    echo "Failed to locate extracted binary from $FILE"
    exit 1
  fi

  chmod +x "$EXTRACTED_BIN"
  echo "Installing $BIN to $INSTALL_DIR (may require sudo)..."
  sudo mv "$EXTRACTED_BIN" "$INSTALL_DIR/$BIN"
  echo "Installed $BIN to $INSTALL_DIR/clawgo"
else
  echo "No prebuilt binary found, exiting..."
  exit 1
fi

# ====================
# Download WebUI
# ====================
echo "Downloading ClawGo WebUI..."
WEBUI_OUT="$TMPDIR/$WEBUI_FILE"
if curl -fSL "$WEBUI_URL" -o "$WEBUI_OUT"; then
  echo "Downloaded WebUI"
  mkdir -p "$WEBUI_DIR"
  WEBUI_TMP="$TMPDIR/webui_extract"
  rm -rf "$WEBUI_TMP"
  mkdir -p "$WEBUI_TMP"
  tar -xzf "$WEBUI_OUT" -C "$WEBUI_TMP"

  WEBUI_DIST_DIR=""
  if [[ -d "$WEBUI_TMP/dist" ]]; then
    WEBUI_DIST_DIR="$WEBUI_TMP/dist"
  else
    WEBUI_DIST_DIR="$(find "$WEBUI_TMP" -mindepth 2 -maxdepth 4 -type d -name dist | head -n1)"
  fi

  if [[ -n "$WEBUI_DIST_DIR" && -d "$WEBUI_DIST_DIR" ]]; then
    rsync -a --delete "$WEBUI_DIST_DIR/" "$WEBUI_DIR/"
  else
    rsync -a --delete "$WEBUI_TMP/" "$WEBUI_DIR/"
  fi
  echo "WebUI installed to $WEBUI_DIR"
else
  echo "Failed to download WebUI"
  exit 1
fi

# ====================
# Migrate (Embedded openclaw2clawgo Script)
# ====================
read -p "Do you want to migrate your OpenClaw workspace to ClawGo? (y/n): " MIGRATE
if [[ "$MIGRATE" == "y" || "$MIGRATE" == "Y" ]]; then
  echo "Choose migration type: "
  echo "1. Local migration"
  echo "2. Remote migration"
  read -p "Enter your choice (1 or 2): " MIGRATION_TYPE

  if [[ "$MIGRATION_TYPE" == "1" ]]; then
    echo "Proceeding with local migration..."

    # Default paths for local migration
    SRC_DEFAULT="$HOME/.openclaw/workspace"
    DST_DEFAULT="$HOME/.clawgo/workspace"
    SRC="${SRC_DEFAULT}"
    DST="${DST_DEFAULT}"

    # Prompt user about overwriting existing data
    echo "Warning: Migration will overwrite the contents of $DST"
    read -p "Are you sure you want to continue? (y/n): " CONFIRM
    if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
      echo "Migration canceled."
      exit 0
    fi

    echo "[INFO] source: $SRC"
    echo "[INFO] target: $DST"

    mkdir -p "$DST" "$DST/memory"
    TS="$(date -u +%Y%m%dT%H%M%SZ)"
    BACKUP_DIR="$DST/.migration-backup-$TS"
    mkdir -p "$BACKUP_DIR"

    # Backup existing key files if present
    for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
      if [[ -f "$DST/$f" ]]; then
        cp -a "$DST/$f" "$BACKUP_DIR/$f"
      fi
    done
    if [[ -d "$DST/memory" ]]; then
      cp -a "$DST/memory" "$BACKUP_DIR/memory" || true
    fi

    # Migrate core persona/context files
    for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
      if [[ -f "$SRC/$f" ]]; then
        cp -a "$SRC/$f" "$DST/$f"
        echo "[OK] migrated $f"
      fi
    done

    # Merge memory directory
    if [[ -d "$SRC/memory" ]]; then
      rsync -a "$SRC/memory/" "$DST/memory/"
      echo "[OK] migrated memory/"
    fi

    # Optional: sync into embedded workspace template used by clawgo builds
    echo "[INFO] Syncing embed workspace template..."
    if [[ -d "$DST" ]]; then
      mkdir -p "$DST"
      for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
        if [[ -f "$DST/$f" ]]; then
          cp -a "$DST/$f" "$DST/$f"
        fi
      done
      if [[ -d "$DST/memory" ]]; then
        mkdir -p "$DST/memory"
        rsync -a "$DST/memory/" "$DST/memory/"
      fi
      echo "[OK] synced embed workspace template"
    fi

    echo "[DONE] migration complete"

  elif [[ "$MIGRATION_TYPE" == "2" ]]; then
    echo "Proceeding with remote migration..."

    read -p "Enter remote host (e.g., user@hostname): " REMOTE_HOST
    read -p "Enter remote port (default 22): " REMOTE_PORT
    REMOTE_PORT="${REMOTE_PORT:-22}"
    read -sp "Enter remote password: " REMOTE_PASS
    echo

    # Create a temporary SSH key for non-interactive SSH authentication (assuming sshpass is installed)
    SSH_KEY=$(mktemp)
    sshpass -p "$REMOTE_PASS" ssh-copy-id -i "$SSH_KEY" "$REMOTE_HOST -p $REMOTE_PORT"

    # Prepare migration script
    MIGRATION_SCRIPT="$TMPDIR/openclaw2clawgo.sh"
    cat << 'EOF' > "$MIGRATION_SCRIPT"
#!/bin/bash
set -e

SRC_DEFAULT="$HOME/.openclaw/workspace"
DST_DEFAULT="$HOME/.clawgo/workspace"
SRC="${1:-$SRC_DEFAULT}"
DST="${2:-$DST_DEFAULT}"

echo "[INFO] source: $SRC"
echo "[INFO] target: $DST"

mkdir -p "$DST" "$DST/memory"
TS="$(date -u +%Y%m%dT%H%M%SZ)"
BACKUP_DIR="$DST/.migration-backup-$TS"
mkdir -p "$BACKUP_DIR"

# Backup existing key files if present
for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
  if [[ -f "$DST/$f" ]]; then
    cp -a "$DST/$f" "$BACKUP_DIR/$f"
  fi
done
if [[ -d "$DST/memory" ]]; then
  cp -a "$DST/memory" "$BACKUP_DIR/memory" || true
fi

# Migrate core persona/context files
for f in AGENTS.md SOUL.md USER.md IDENTITY.md TOOLS.md MEMORY.md HEARTBEAT.md; do
  if [[ -f "$SRC/$f" ]]; then
    cp -a "$SRC/$f" "$DST/$f"
    echo "[OK] migrated $f"
  fi
done

# Merge memory directory
if [[ -d "$SRC/memory" ]]; then
  rsync -a "$SRC/memory/" "$DST/memory/"
  echo "[OK] migrated memory/"
fi

echo "[DONE] migration complete"
EOF

    # Copy migration script to remote server and execute it
    sshpass -p "$REMOTE_PASS" scp -P "$REMOTE_PORT" "$MIGRATION_SCRIPT" "$REMOTE_HOST:/tmp/openclaw2clawgo.sh"
    sshpass -p "$REMOTE_PASS" ssh -p "$REMOTE_PORT" "$REMOTE_HOST" "bash /tmp/openclaw2clawgo.sh"

    echo "[INFO] Remote migration completed."

  else
    echo "Invalid choice. Skipping migration."
  fi
fi

echo "Cleaning up..."
rm -rf "$TMPDIR"

echo "Done 🎉"
echo "Run 'clawgo --help' to verify"
