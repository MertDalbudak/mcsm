#!/usr/bin/env bash
# Install mcsm on Debian/Ubuntu hosts. Requires root.
# Usage: sudo ./setup-debian.sh [version]
set -euo pipefail

VERSION="${1:-latest}"
USER_NAME="mcsm"
INSTALL_BIN="/usr/local/bin/mcsm"
CONFIG_DIR="/etc/mcsm"
DATA_DIR="/var/lib/mcsm"
LOG_DIR="/var/log/mcsm"
SERVICE_FILE="/etc/systemd/system/mcsm.service"
REPO_RAW="https://github.com/MertDalbudak/mcsm/releases/${VERSION}/download"

if [[ $EUID -ne 0 ]]; then
  echo "must run as root" >&2
  exit 1
fi

echo "==> apt: installing dependencies (java, tools)"
apt-get update -qq
apt-get install -y --no-install-recommends \
    ca-certificates curl tar \
    openjdk-21-jre-headless

if ! id -u "$USER_NAME" >/dev/null 2>&1; then
  echo "==> creating user $USER_NAME"
  useradd --system --home-dir "$DATA_DIR" --create-home --shell /usr/sbin/nologin "$USER_NAME"
fi

echo "==> creating directories"
install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"

# Phase 1: binary install left as TODO once releases are published. For
# now, use `make install` from a checkout.
if [[ "$VERSION" != "dev" ]]; then
  echo "==> downloading mcsm $VERSION"
  ARCH="$(dpkg --print-architecture)"   # amd64 | arm64
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' EXIT
  curl -fsSL "$REPO_RAW/mcsm-linux-$ARCH.tar.gz" -o "$TMP/mcsm.tgz"
  tar -xzf "$TMP/mcsm.tgz" -C "$TMP"
  install -m 0755 "$TMP/mcsm" "$INSTALL_BIN"
fi

if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
  echo "==> seeding $CONFIG_DIR/config.yaml from example"
  install -o "$USER_NAME" -g "$USER_NAME" -m 0640 \
      "$(dirname "$0")/../../configs/config.example.yaml" \
      "$CONFIG_DIR/config.yaml"
  echo "    edit this file before starting the service."
fi

echo "==> installing systemd unit"
install -m 0644 "$(dirname "$0")/../systemd/mcsm.service" "$SERVICE_FILE"
systemctl daemon-reload
systemctl enable mcsm.service

cat <<EOF

mcsm installed.
  config: $CONFIG_DIR/config.yaml
  data:   $DATA_DIR
  logs:   journalctl -u mcsm -f

next: edit $CONFIG_DIR/config.yaml, then:
  sudo systemctl start mcsm
EOF
