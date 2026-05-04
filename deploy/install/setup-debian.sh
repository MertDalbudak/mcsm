#!/usr/bin/env bash
# Install mcsm on Debian/Ubuntu hosts. Requires root.
# Builds from this checkout — no published binaries yet.
# Usage:  sudo ./setup-debian.sh
set -euo pipefail

USER_NAME="mcsm"
INSTALL_BIN="/usr/local/bin/mcsm"
TOKENS_BIN="/usr/local/bin/mcsm-tokens"
CONFIG_DIR="/etc/mcsm"
DATA_DIR="/var/lib/mcsm"
LOG_DIR="/var/log/mcsm"
SHARE_DIR="/usr/share/mcsm"
SERVICE_FILE="/etc/systemd/system/mcsm.service"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

if [[ $EUID -ne 0 ]]; then
  echo "must run as root (use sudo)" >&2
  exit 1
fi

echo "==> apt: installing build + runtime dependencies"
apt-get update -qq
apt-get install -y --no-install-recommends \
    ca-certificates curl tar git make build-essential \
    golang-go \
    openjdk-21-jre-headless

if ! id -u "$USER_NAME" >/dev/null 2>&1; then
  echo "==> creating system user $USER_NAME"
  useradd --system --home-dir "$DATA_DIR" --create-home \
          --shell /usr/sbin/nologin "$USER_NAME"
fi

echo "==> creating directories"
install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
install -d -m 0755 "$SHARE_DIR"

echo "==> building mcsm binaries"
cd "$REPO_ROOT"
make build

echo "==> installing binaries"
install -m 0755 bin/mcsm        "$INSTALL_BIN"
install -m 0755 bin/mcsm-tokens "$TOKENS_BIN"

echo "==> installing example config"
install -m 0644 configs/config.example.yaml "$SHARE_DIR/config.example.yaml"

if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
  echo "==> seeding $CONFIG_DIR/config.yaml from example"
  install -o "$USER_NAME" -g "$USER_NAME" -m 0640 \
      "$SHARE_DIR/config.example.yaml" "$CONFIG_DIR/config.yaml"
fi

echo "==> installing systemd unit"
install -m 0644 deploy/systemd/mcsm.service "$SERVICE_FILE"
systemctl daemon-reload
systemctl enable mcsm.service

cat <<EOF

mcsm installed.

  binary: $INSTALL_BIN
  tokens: $TOKENS_BIN
  config: $CONFIG_DIR/config.yaml          (← edit before first start)
  data:   $DATA_DIR
  logs:   journalctl -u mcsm -f

next steps:
  1) Generate a real bearer token:
       echo -n 'your-secret-token' | mcsm-tokens
     Or pick one for you:
       mcsm-tokens --random
     Paste the resulting \$argon2id\$… line into api.tokens[].hash.

  2) Edit discovery.roots, slots, etc. in $CONFIG_DIR/config.yaml.

  3) Start the service:
       sudo systemctl start mcsm
       sudo systemctl status mcsm
       journalctl -u mcsm -f
EOF
