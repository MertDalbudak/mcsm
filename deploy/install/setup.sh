#!/bin/sh
# mcsm cross-distro installer.
#
# Detects the distro and init system, installs build + runtime
# dependencies, builds the binaries, prompts for the basics
# (instance name, discovery root, one slot, a bearer token), writes
# /etc/mcsm/config.yaml, and registers the service.
#
# Tested on: Alpine, Debian, Ubuntu, Arch, Manjaro, Fedora, Rocky,
# AlmaLinux, openSUSE Leap/Tumbleweed.
#
# Flags:
#   --no-config     skip the interactive prompts; copy the example
#                   config verbatim (you can edit it later)
#   --keep-config   if /etc/mcsm/config.yaml already exists, don't
#                   touch it (default is to ask)
#
# Usage:
#   sudo ./deploy/install/setup.sh
#   doas ./deploy/install/setup.sh         # Alpine without sudo
#   sudo ./deploy/install/setup.sh --no-config
set -eu

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

USER_NAME="mcsm"
INSTALL_BIN="/usr/local/bin/mcsm"
TOKENS_BIN="/usr/local/bin/mcsm-tokens"
CONFIG_DIR="/etc/mcsm"
CONFIG_FILE="$CONFIG_DIR/config.yaml"
DATA_DIR="/var/lib/mcsm"
LOG_DIR="/var/log/mcsm"
SHARE_DIR="/usr/share/mcsm"

INTERACTIVE=1
KEEP_CONFIG=0
for arg in "$@"; do
    case "$arg" in
        --no-config)   INTERACTIVE=0 ;;
        --keep-config) KEEP_CONFIG=1 ;;
        --help|-h)
            sed -n '2,/^set -eu/p' "$0" | sed 's/^# \?//' | head -n -1
            exit 0 ;;
        *) echo "unknown flag: $arg" >&2; exit 2 ;;
    esac
done

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

distro_kind() {
    case "$DISTRO_ID" in
        alpine) echo alpine ;;
        debian|ubuntu|raspbian|linuxmint|pop|kali|elementary) echo debian ;;
        arch|manjaro|endeavouros|garuda|cachyos|artix) echo arch ;;
        fedora|rhel|centos|rocky|almalinux|amzn|ol) echo rhel ;;
        opensuse-leap|opensuse-tumbleweed|sles|sled) echo suse ;;
        *)
            for like in $DISTRO_LIKE; do
                case "$like" in
                    debian) echo debian; return ;;
                    arch)   echo arch;   return ;;
                    rhel|fedora|"rhel fedora"|"fedora rhel") echo rhel; return ;;
                    suse|opensuse) echo suse; return ;;
                esac
            done
            echo unknown ;;
    esac
}
KIND="$(distro_kind)"
echo "==> distro: ${DISTRO_ID} (family: ${KIND})"

# ---------- init system ----------

if [ -d /run/systemd/system ]; then
    INIT_SYSTEM=systemd
elif command -v rc-update >/dev/null 2>&1; then
    INIT_SYSTEM=openrc
else
    INIT_SYSTEM=none
fi
echo "==> init: ${INIT_SYSTEM}"

# ---------- prompt helpers ----------

# Decide once where to read user input from:
#   - If /dev/tty is openable, use it (works for both `./setup.sh`
#     interactively and `curl | sh` pipes).
#   - Otherwise fall back to stdin (works for piped answers in CI).
# The subshell isolates the redirection probe from this shell's stderr.
if (exec 3</dev/tty) 2>/dev/null; then
    exec 3</dev/tty
    INPUT_FD=3
else
    INPUT_FD=0
fi

# ask "Prompt" "default" -> sets REPLY (default returned on empty input)
ask() {
    if [ -n "${2:-}" ]; then
        printf "  %s [%s]: " "$1" "$2"
    else
        printf "  %s: " "$1"
    fi
    IFS= read -r REPLY <&"$INPUT_FD" || REPLY=""
    [ -z "$REPLY" ] && REPLY="${2:-}"
}

# ask_yn "Prompt" "y|n" -> 0 if yes, 1 if no
ask_yn() {
    while :; do
        if [ "$2" = y ]; then
            printf "  %s [Y/n]: " "$1"
        else
            printf "  %s [y/N]: " "$1"
        fi
        IFS= read -r ans <&"$INPUT_FD" || ans=""
        case "${ans:-$2}" in
            y|Y|yes|YES) return 0 ;;
            n|N|no|NO)   return 1 ;;
        esac
    done
}

valid_name() {
    # [a-z0-9-], 1-64
    case "$1" in
        ''|*[!a-z0-9-]*) return 1 ;;
    esac
    [ "${#1}" -le 64 ] && [ "${#1}" -ge 1 ]
}

# ---------- dependency install ----------

install_deps() {
    case "$KIND" in
        alpine)
            apk update -q
            apk add --no-cache make go git ca-certificates curl tar tini openjdk21-jre-headless ;;
        debian)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -qq
            apt-get install -y --no-install-recommends \
                ca-certificates curl tar git make golang-go openjdk-21-jre-headless ;;
        arch)
            pacman -Sy --noconfirm
            pacman -S --needed --noconfirm \
                make go git ca-certificates curl jre21-openjdk-headless ;;
        rhel)
            PKG=$(command -v dnf 2>/dev/null || command -v yum)
            "$PKG" -y install make golang git ca-certificates curl tar java-21-openjdk-headless ;;
        suse)
            zypper -n install --no-recommends make go git ca-certificates curl tar java-21-openjdk-headless ;;
        *)
            cat >&2 <<EOF
Unsupported distro "${DISTRO_ID}".
Install manually: go (>=1.23 or set GOTOOLCHAIN=auto), make, git,
openjdk-21-jre-headless, ca-certificates, curl. Then re-run.
EOF
            exit 1 ;;
    esac
}

# ---------- system user ----------

create_user() {
    if id -u "$USER_NAME" >/dev/null 2>&1; then return 0; fi
    echo "==> creating system user ${USER_NAME}"
    if command -v useradd >/dev/null 2>&1; then
        useradd --system --home-dir "$DATA_DIR" --create-home \
                --shell /usr/sbin/nologin "$USER_NAME" 2>/dev/null || \
        useradd --system --home-dir "$DATA_DIR" --create-home \
                --shell /sbin/nologin "$USER_NAME"
    elif command -v adduser >/dev/null 2>&1; then
        adduser -D -h "$DATA_DIR" -s /sbin/nologin "$USER_NAME"
    else
        echo "neither useradd nor adduser found" >&2; exit 1
    fi
}

# ---------- build + install ----------

build_binaries() {
    echo "==> building mcsm binaries"
    cd "$REPO_ROOT"
    if command -v go >/dev/null 2>&1; then
        ver="$(go version | awk '{print $3}' | sed 's/^go//')"
        major="$(echo "$ver" | cut -d. -f1)"
        minor="$(echo "$ver" | cut -d. -f2)"
        if [ "${major:-0}" -lt 1 ] || \
           { [ "${major:-0}" -eq 1 ] && [ "${minor:-0}" -lt 23 ]; }; then
            echo "    system go is ${ver}; need >= 1.23"
            echo "    setting GOTOOLCHAIN=auto so go fetches the required version"
            export GOTOOLCHAIN=auto
        fi
    fi
    make build
}

install_artifacts() {
    install -d -o "$USER_NAME" -g "$USER_NAME" -m 0750 \
        "$CONFIG_DIR" "$DATA_DIR" "$LOG_DIR"
    install -d -m 0755 "$SHARE_DIR"
    install -m 0755 bin/mcsm        "$INSTALL_BIN"
    install -m 0755 bin/mcsm-tokens "$TOKENS_BIN"
    install -m 0644 configs/config.example.yaml "$SHARE_DIR/config.example.yaml"
}

# ---------- config wizard ----------

# write_config_yaml — emits a fresh config.yaml from the prompted values.
# Reads slots/roots from the temp files populated by run_wizard.
write_config_yaml() {
    {
        cat <<EOF
# Generated by deploy/install/setup.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ).
# Edit freely; mcsm reloads on restart.

instance:
  name: ${WZ_INSTANCE}
  data_dir: ${DATA_DIR}

api:
  bind: ${WZ_API_BIND}
  tokens:
    - name: default
      hash: "${WZ_TOKEN_HASH}"
      scopes: ["*"]
  cors:
    allowed_origins: []
  public_meta: true

discovery:
  roots:
EOF
        cat "$ROOTS_TMP"
        cat <<EOF
  scan_interval: 60s

slots:
EOF
        cat "$SLOTS_TMP"
        cat <<EOF

peers:
  peers: []
EOF
        if [ -n "${WZ_TEMP_SENSOR:-}" ]; then
            cat <<EOF

system:
  temperature:
    sensor: ${WZ_TEMP_SENSOR}
EOF
        fi
        cat <<EOF

logging:
  level: info
  format: json
  output: stdout

audit:
  enabled: true
  retention: 720h

metrics:
  enabled: true
  path: /metrics
  require_auth: false
EOF
    } > "$CONFIG_FILE"
    chown "$USER_NAME:$USER_NAME" "$CONFIG_FILE" 2>/dev/null || true
    chmod 0640 "$CONFIG_FILE"
}

# Generates the bearer token. Sets WZ_TOKEN_PLAIN (32-hex-char secret
# the operator will give to clients) and WZ_TOKEN_HASH (the argon2id
# string that lives in config.yaml).
generate_token() {
    WZ_TOKEN_PLAIN="$(head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n')"
    WZ_TOKEN_HASH="$(printf '%s' "$WZ_TOKEN_PLAIN" | "$TOKENS_BIN")"
}

# run_wizard prompts for every value and renders config.yaml.
run_wizard() {
    cat <<'EOF'

==> Configure mcsm
    Answer the prompts below. Defaults in [brackets] — press Enter to accept.
    You can re-run this script later, or edit /etc/mcsm/config.yaml directly.

EOF
    DEFAULT_NAME="$(hostname 2>/dev/null | tr '[:upper:]' '[:lower:]' | sed 's/[^a-z0-9-]/-/g' | cut -c1-64)"
    [ -z "$DEFAULT_NAME" ] && DEFAULT_NAME="mcsm-host"
    while :; do
        ask "Instance name (a-z, 0-9, -)" "$DEFAULT_NAME"
        if valid_name "$REPLY"; then WZ_INSTANCE="$REPLY"; break; fi
        echo "    invalid — must match [a-z0-9-]{1,64}"
    done

    ask "API bind address" "0.0.0.0:8124"
    WZ_API_BIND="$REPLY"

    # Discovery roots — collect into a temp file we'll cat back out
    # during config rendering. Avoids in-shell newline accumulation.
    ROOTS_TMP="$(mktemp)"
    SLOTS_TMP="$(mktemp)"
    # shellcheck disable=SC2064
    trap "rm -f $ROOTS_TMP $SLOTS_TMP" EXIT INT TERM

    n=0
    while :; do
        n=$((n + 1))
        if [ $n -eq 1 ]; then
            ask "Discovery root #1 (parent dir of your MC server folders)" "/mnt/servers"
        else
            ask "Discovery root #${n} (Enter to finish)" ""
            [ -z "$REPLY" ] && break
        fi
        printf "    - %s\n" "$REPLY" >> "$ROOTS_TMP"
        ask_yn "Add another discovery root?" "n" || break
    done

    # Slots
    n=0
    while :; do
        n=$((n + 1))
        echo
        echo "  Slot #${n}:"
        if [ $n -eq 1 ]; then
            ask "    Slot name (a-z, 0-9, -)" "creative"
        else
            ask "    Slot name (Enter to finish)" ""
            [ -z "$REPLY" ] && break
        fi
        SLOT_NAME="$REPLY"
        ask "    Port (the public Minecraft port)" "$((25564 + n))"
        SLOT_PORT="$REPLY"
        ask "    Public address (e.g. mc.example.com — optional)" ""
        SLOT_PUBLIC="$REPLY"
        printf "  - name: %s\n    port: %s\n" "$SLOT_NAME" "$SLOT_PORT" >> "$SLOTS_TMP"
        if [ -n "$SLOT_PUBLIC" ]; then
            printf "    public_address: %s\n" "$SLOT_PUBLIC" >> "$SLOTS_TMP"
        fi
        ask_yn "  Add another slot?" "n" || break
    done

    # Optional CPU temperature sensor
    DEFAULT_SENSOR=""
    if [ -r /sys/class/thermal/thermal_zone0/temp ]; then
        DEFAULT_SENSOR="/sys/class/thermal/thermal_zone0/temp"
    fi
    echo
    if [ -n "$DEFAULT_SENSOR" ]; then
        ask "CPU temperature sensor (Enter to use detected, '-' to disable)" "$DEFAULT_SENSOR"
        if [ "$REPLY" = "-" ]; then WZ_TEMP_SENSOR=""; else WZ_TEMP_SENSOR="$REPLY"; fi
    else
        ask "CPU temperature sensor (Enter to skip — none detected)" ""
        WZ_TEMP_SENSOR="$REPLY"
    fi

    # Bearer token
    echo
    echo "==> generating bearer token"
    generate_token
    cat <<EOF

  ┌──────────────────────────────────────────────────────────────────────┐
  │ Bearer token (give this to your client / mcsw — IT IS NOT STORED):  │
  │                                                                      │
  │   ${WZ_TOKEN_PLAIN}                                  │
  │                                                                      │
  │ The argon2id hash is what lives in config.yaml; the plaintext above  │
  │ is what you paste into the mcsw env or pass as Authorization: Bearer.│
  └──────────────────────────────────────────────────────────────────────┘

EOF
    ask_yn "Save this token now? You won't see it again" "y" || {
        echo "Aborting — re-run setup.sh when you're ready."
        exit 1
    }

    write_config_yaml
    echo "==> wrote ${CONFIG_FILE}"
}

handle_existing_config() {
    [ ! -f "$CONFIG_FILE" ] && return 0
    if [ "$KEEP_CONFIG" = 1 ]; then
        echo "==> ${CONFIG_FILE} already exists — keeping (--keep-config)"
        return 1
    fi
    echo
    echo "==> ${CONFIG_FILE} already exists."
    if ask_yn "Re-run the configuration wizard and overwrite it?" "n"; then
        cp "$CONFIG_FILE" "${CONFIG_FILE}.backup-$(date -u +%Y%m%d-%H%M%S)"
        echo "    saved backup → ${CONFIG_FILE}.backup-…"
        return 0
    fi
    return 1
}

# ---------- service registration ----------

START_CMD=""; STATUS_CMD=""; LOG_CMD=""

install_service() {
    case "$INIT_SYSTEM" in
        systemd)
            install -m 0644 deploy/systemd/mcsm.service /etc/systemd/system/mcsm.service
            systemctl daemon-reload
            systemctl enable mcsm.service
            START_CMD="systemctl start mcsm"; STATUS_CMD="systemctl status mcsm"; LOG_CMD="journalctl -u mcsm -f" ;;
        openrc)
            install -m 0755 deploy/openrc/mcsm.initd /etc/init.d/mcsm
            rc-update add mcsm default
            START_CMD="rc-service mcsm start"; STATUS_CMD="rc-service mcsm status"; LOG_CMD="tail -f ${LOG_DIR}/out.log" ;;
        *)
            echo "==> no recognized init system; binary installed but no service registered"
            echo "    start manually: ${INSTALL_BIN} --config ${CONFIG_FILE}" ;;
    esac
}

# ---------- run all phases ----------

echo "==> installing dependencies"; install_deps
create_user
build_binaries
install_artifacts

if [ "$INTERACTIVE" = 1 ]; then
    if [ ! -f "$CONFIG_FILE" ] || handle_existing_config; then
        run_wizard
    fi
else
    if [ ! -f "$CONFIG_FILE" ]; then
        echo "==> seeding ${CONFIG_FILE} from example (--no-config)"
        install -o "$USER_NAME" -g "$USER_NAME" -m 0640 \
            "$SHARE_DIR/config.example.yaml" "$CONFIG_FILE"
        echo "    edit api.tokens[0].hash before starting!"
    fi
fi

install_service

cat <<EOF

==============================================================
mcsm installed.
  binary:   ${INSTALL_BIN}
  config:   ${CONFIG_FILE}
  data:     ${DATA_DIR}
  init:     ${INIT_SYSTEM}
EOF
if [ -n "${WZ_TOKEN_PLAIN:-}" ]; then
    cat <<EOF

  TOKEN:    ${WZ_TOKEN_PLAIN}
            ↑ store this; you'll need it for mcsw and any API call.
EOF
fi
if [ -n "$START_CMD" ]; then
    cat <<EOF

start it now with:
  sudo ${START_CMD}
  sudo ${STATUS_CMD}
  sudo ${LOG_CMD}
EOF
fi
echo "=============================================================="
