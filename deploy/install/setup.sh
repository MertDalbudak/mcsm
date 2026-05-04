#!/bin/sh
# mcsm cross-distro installer.
#
# Detects the distro and init system, installs build + runtime
# dependencies, builds the binaries from this checkout, drops the
# right service file (systemd unit or OpenRC init), and registers it
# for boot.
#
# Tested on: Alpine, Debian, Ubuntu, Arch, Manjaro, Fedora, Rocky,
# AlmaLinux, openSUSE Leap/Tumbleweed.
#
# Usage:  sudo ./deploy/install/setup.sh
#         doas ./deploy/install/setup.sh   (Alpine without sudo)
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

# Final install paths — kept identical across distros so the rest of
# the project (configs/docs/scripts) doesn't need branches.
USER_NAME="mcsm"
INSTALL_BIN="/usr/local/bin/mcsm"
TOKENS_BIN="/usr/local/bin/mcsm-tokens"
CONFIG_DIR="/etc/mcsm"
DATA_DIR="/var/lib/mcsm"
LOG_DIR="/var/log/mcsm"
SHARE_DIR="/usr/share/mcsm"

# ---------- preflight ----------

if [ "$(id -u)" -ne 0 ]; then
    echo "must run as root (use sudo or doas)" >&2
    exit 1
fi

if [ ! -r /etc/os-release ]; then
    echo "no /etc/os-release — cannot detect distro" >&2
    exit 1
fi
# shellcheck disable=SC1091
. /etc/os-release
DISTRO_ID="${ID:-unknown}"
DISTRO_LIKE="${ID_LIKE:-}"

# ---------- distro family ----------

# distro_kind collapses dozens of distro IDs into one of:
#   alpine | debian | arch | rhel | suse | unknown
distro_kind() {
    case "$DISTRO_ID" in
        alpine)
            echo alpine ;;
        debian|ubuntu|raspbian|linuxmint|pop|kali|elementary)
            echo debian ;;
        arch|manjaro|endeavouros|garuda|cachyos|artix)
            echo arch ;;
        fedora|rhel|centos|rocky|almalinux|amzn|ol)
            echo rhel ;;
        opensuse-leap|opensuse-tumbleweed|sles|sled)
            echo suse ;;
        *)
            # Fall back on ID_LIKE; many derivatives identify their parent here.
            for like in $DISTRO_LIKE; do
                case "$like" in
                    debian)             echo debian; return ;;
                    arch)               echo arch;   return ;;
                    rhel|fedora|"rhel fedora"|"fedora rhel") echo rhel; return ;;
                    suse|opensuse)      echo suse;   return ;;
                esac
            done
            echo unknown ;;
    esac
}

KIND="$(distro_kind)"
echo "==> detected distro: ${DISTRO_ID} (family: ${KIND})"

# ---------- init system ----------

# Prefer systemd if PID 1 is systemd (the canonical check is the
# /run/systemd/system directory, which only exists on systemd hosts).
# Fall back to OpenRC if rc-update is present.
if [ -d /run/systemd/system ]; then
    INIT_SYSTEM=systemd
elif command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
else
    INIT_SYSTEM=none
fi
echo "==> detected init: ${INIT_SYSTEM}"

# ---------- dependency install ----------

install_deps() {
    case "$KIND" in
        alpine)
            apk update -q
            apk add --no-cache \
                make go git \
                ca-certificates curl tar tini \
                openjdk21-jre-headless
            ;;
        debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq
            apt-get install -y --no-install-recommends \
                ca-certificates curl tar git make \
                golang-go \
                openjdk-21-jre-headless
            ;;
        arch)
            pacman -Sy --noconfirm
            pacman -S --needed --noconfirm \
                make go git \
                ca-certificates curl \
                jre21-openjdk-headless
            ;;
        rhel)
            if command -v dnf >/dev/null 2>&1; then
                PKG=dnf
            else
                PKG=yum
            fi
            $PKG -y install \
                make golang git \
                ca-certificates curl tar \
                java-21-openjdk-headless
            ;;
        suse)
            zypper -n install --no-recommends \
                make go git \
                ca-certificates curl tar \
                java-21-openjdk-headless
            ;;
        *)
            cat >&2 <<EOF
Unsupported distro "${DISTRO_ID}".

Install these manually, then re-run this script:
  - go (>= 1.23)
  - make, git
  - openjdk-21-jre-headless (or your distro's equivalent)
  - ca-certificates, curl
EOF
            exit 1 ;;
    esac
}

# ---------- system user ----------

create_user() {
    if id -u "$USER_NAME" >/dev/null 2>&1; then
        return 0
    fi
    echo "==> creating system user ${USER_NAME}"
    # busybox `adduser` (Alpine) and shadow `useradd` (everyone else)
    # take very different flags. Pick whichever is on PATH.
    if command -v useradd >/dev/null 2>&1; then
        # Try /usr/sbin/nologin first; fall back to /sbin/nologin.
        useradd --system --home-dir "$DATA_DIR" --create-home \
                --shell /usr/sbin/nologin "$USER_NAME" 2>/dev/null || \
        useradd --system --home-dir "$DATA_DIR" --create-home \
                --shell /sbin/nologin "$USER_NAME"
    elif command -v adduser >/dev/null 2>&1; then
        adduser -D -h "$DATA_DIR" -s /sbin/nologin "$USER_NAME"
    else
        echo "neither useradd nor adduser found" >&2
        exit 1
    fi
}

# ---------- build + install ----------

build_binaries() {
    echo "==> building mcsm binaries"
    cd "$REPO_ROOT"
    make build
}

install_artifacts() {
    install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 \
        "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
    install -d -m 0755 "$SHARE_DIR"

    install -m 0755 bin/mcsm        "$INSTALL_BIN"
    install -m 0755 bin/mcsm-tokens "$TOKENS_BIN"
    install -m 0644 configs/config.example.yaml "$SHARE_DIR/config.example.yaml"

    if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
        echo "==> seeding ${CONFIG_DIR}/config.yaml from example"
        install -o "$USER_NAME" -g "$USER_NAME" -m 0640 \
            "$SHARE_DIR/config.example.yaml" "$CONFIG_DIR/config.yaml"
    fi
}

# ---------- service registration ----------

START_CMD=""
STATUS_CMD=""
LOG_CMD=""

install_service() {
    case "$INIT_SYSTEM" in
        systemd)
            install -m 0644 deploy/systemd/mcsm.service /etc/systemd/system/mcsm.service
            systemctl daemon-reload
            systemctl enable mcsm.service
            START_CMD="systemctl start mcsm"
            STATUS_CMD="systemctl status mcsm"
            LOG_CMD="journalctl -u mcsm -f"
            ;;
        openrc)
            install -m 0755 deploy/openrc/mcsm.initd /etc/init.d/mcsm
            rc-update add mcsm default
            START_CMD="rc-service mcsm start"
            STATUS_CMD="rc-service mcsm status"
            LOG_CMD="tail -f ${LOG_DIR}/out.log"
            ;;
        *)
            echo "==> no recognized init system; binary installed but no service registered"
            echo "    start manually: ${INSTALL_BIN} --config ${CONFIG_DIR}/config.yaml" ;;
    esac
}

# ---------- run all phases ----------

echo "==> installing dependencies"
install_deps
create_user
build_binaries
install_artifacts
install_service

# ---------- post-install instructions ----------

cat <<EOF

mcsm installed.

  binary:   ${INSTALL_BIN}
  tokens:   ${TOKENS_BIN}
  config:   ${CONFIG_DIR}/config.yaml          (← edit before first start)
  data:     ${DATA_DIR}
  example:  ${SHARE_DIR}/config.example.yaml
  init:     ${INIT_SYSTEM}

next steps:

  1) Generate a real bearer token:
       echo -n 'your-secret-token' | mcsm-tokens
     Or have one generated:
       mcsm-tokens --random
     Paste the resulting \$argon2id\$… line into api.tokens[0].hash.

  2) Edit discovery.roots, slots, etc. in ${CONFIG_DIR}/config.yaml.

EOF

if [ -n "$START_CMD" ]; then
    cat <<EOF
  3) Start the service:
       sudo ${START_CMD}
       sudo ${STATUS_CMD}
       sudo ${LOG_CMD}

EOF
fi
