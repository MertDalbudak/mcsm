#!/bin/sh
# Install mcsm on Alpine Linux (incl. minimal LXC). Requires root.
# Usage:  doas ./setup-alpine.sh   OR   sudo ./setup-alpine.sh
#
# Builds from the repo checkout this script lives in (so run it after
# `git clone`). Builds the binary, installs it + OpenRC service +
# config skeleton, and adds the service to the default runlevel.
set -eu

if [ "$(id -u)" != "0" ]; then
    echo "must run as root (use doas/sudo)" >&2
    exit 1
fi

USER_NAME="mcsm"
INSTALL_BIN="/usr/local/bin/mcsm"
CONFIG_DIR="/etc/mcsm"
DATA_DIR="/var/lib/mcsm"
LOG_DIR="/var/log/mcsm"
SHARE_DIR="/usr/share/mcsm"
INIT_SCRIPT="/etc/init.d/mcsm"

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

echo "==> updating package index"
apk update -q

echo "==> installing build + runtime dependencies"
apk add --no-cache \
    build-base make go git \
    ca-certificates curl tar tini \
    openjdk21-jre-headless

if ! id -u "$USER_NAME" >/dev/null 2>&1; then
    echo "==> creating system user $USER_NAME"
    adduser -D -u 1000 -h "$DATA_DIR" -s /sbin/nologin "$USER_NAME"
fi

echo "==> creating directories"
install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
install -d -m 0755 "$SHARE_DIR"

echo "==> building mcsm binary"
cd "$REPO_ROOT"
make build

echo "==> installing binaries to /usr/local/bin"
install -m 0755 bin/mcsm "$INSTALL_BIN"
install -m 0755 bin/mcsm-tokens /usr/local/bin/mcsm-tokens

echo "==> installing OpenRC service to $INIT_SCRIPT"
install -m 0755 "$REPO_ROOT/deploy/openrc/mcsm.initd" "$INIT_SCRIPT"

echo "==> installing example config to $SHARE_DIR/config.example.yaml"
install -m 0644 "$REPO_ROOT/configs/config.example.yaml" "$SHARE_DIR/config.example.yaml"

if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
    echo "==> seeding $CONFIG_DIR/config.yaml from example"
    install -o "$USER_NAME" -g "$USER_NAME" -m 0640 \
        "$SHARE_DIR/config.example.yaml" "$CONFIG_DIR/config.yaml"
fi

echo "==> registering service for default runlevel"
rc-update add mcsm default

cat <<EOF

mcsm installed.

  binary: $INSTALL_BIN
  config: $CONFIG_DIR/config.yaml          (← edit before first start)
  data:   $DATA_DIR
  logs:   $LOG_DIR/{out,err}.log

next steps:
  1) Generate a real bearer token:
       echo -n 'your-secret-token' | mcsm-tokens
     Or generate a random one:
       mcsm-tokens --random
     Paste the resulting \$argon2id\$… line into api.tokens[].hash.

  2) Edit discovery.roots, slots, etc. in $CONFIG_DIR/config.yaml.

  3) Start the service:
       rc-service mcsm start
       rc-service mcsm status
       tail -f $LOG_DIR/out.log
EOF
