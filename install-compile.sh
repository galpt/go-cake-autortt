#!/bin/sh

# install-compile.sh
# Install and compile cake-autortt on the target machine (OpenWrt / Linux).
# This script automates installing Go (prefer opkg), builds the local source
# and installs the binary and service files. Designed to replace download step
# in install.sh so the code is compiled locally (avoids CI embed issues).

set -e

VERSION="local-build"
REPO_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_NAME="cake-autortt"
SERVICE_NAME="cake-autortt"
CONFIG_FILE="/etc/cake-autortt.yaml"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
log_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
log_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
log_error() { echo -e "${RED}[ERROR]${NC} $1"; }

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root"
        log_info "Run with: sudo $0"
        exit 1
    fi
}

detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64) echo "amd64" ;;
        aarch64|arm64) echo "arm64" ;;
        armv7l) echo "arm" ;;
        armv6l) echo "arm" ;;
        i386|i686) echo "386" ;;
        mips) echo "mips" ;;
        mipsel) echo "mipsle" ;;
        mips64) echo "mips64" ;;
        mips64el) echo "mips64le" ;;
        *) echo "$arch" ;;
    esac
}

# Detect OS type for service install logic
detect_os() {
    if [ -f /etc/openwrt_release ]; then
        echo "openwrt"
    elif command -v systemctl > /dev/null 2>&1; then
        echo "systemd"
    else
        echo "linux"
    fi
}

check_dependencies() {
    log_info "Checking dependencies..."
    # tc required
    if ! command -v tc > /dev/null 2>&1; then
        log_error "tc (traffic control) is required but not installed"
        log_info "Please install iproute2 package"
        exit 1
    fi
    # wget or curl
    if ! command -v wget > /dev/null 2>&1 && ! command -v curl > /dev/null 2>&1; then
        log_error "wget or curl is required for downloading"
        exit 1
    fi
    log_success "Dependencies check passed"
}

# Ensure Go is installed. Try existing go, then opkg (OpenWrt), then fallback download
ensure_go() {
    if command -v go > /dev/null 2>&1; then
        log_info "Found existing go: $(go version)"
        return 0
    fi

    if command -v opkg > /dev/null 2>&1; then
        log_info "Attempting to install golang via opkg"
        opkg update || true
        if opkg install golang --force-depends 2>/dev/null; then
            if command -v go > /dev/null 2>&1; then
                log_success "golang installed via opkg"
                return 0
            fi
        else
            log_warning "opkg install golang failed or package not available"
        fi
    else
        log_info "opkg not available, skipping opkg install"
    fi

    # Fallback: try to download official Go binary for common arches (amd64, 386, arm64)
    ARCH=$(detect_arch)
    case "$ARCH" in
        amd64) GOARCH="amd64" ;;
        arm64) GOARCH="arm64" ;;
        386) GOARCH="386" ;;
        *)
            log_error "Automatic Go install not supported for arch: $ARCH"
            log_info "Please install Go manually on this device or use opkg when available"
            return 1
            ;;
    esac

    # Choose a stable Go version to download (compatible with your code)
    GO_VER="1.21.14"
    TAR_NAME="go${GO_VER}.linux-${GOARCH}.tar.gz"
    URL="https://dl.google.com/go/${TAR_NAME}"

    TMPDIR=$(mktemp -d)
    cd "$TMPDIR"
    log_info "Downloading $URL"
    if command -v wget > /dev/null 2>&1; then
        if ! wget -q "$URL" -O "$TAR_NAME"; then
            log_error "Failed to download $URL"
            cd /
            rm -rf "$TMPDIR"
            return 1
        fi
    else
        if ! curl -sL "$URL" -o "$TAR_NAME"; then
            log_error "Failed to download $URL"
            cd /
            rm -rf "$TMPDIR"
            return 1
        fi
    fi

    # Extract to /usr/local (create if needed)
    if [ ! -w /usr/local ]; then
        log_info "/usr/local not writable, will try /opt"
        INSTALL_PREFIX="/opt"
    else
        INSTALL_PREFIX="/usr/local"
    fi

    sudo mkdir -p "${INSTALL_PREFIX}" || true
    tar -C "${INSTALL_PREFIX}" -xzf "$TAR_NAME" || {
        log_error "Failed to extract Go archive"
        cd /
        rm -rf "$TMPDIR"
        return 1
    }

    # Symlink go binary to /usr/bin if not present
    if [ -f "${INSTALL_PREFIX}/go/bin/go" ]; then
        ln -sf "${INSTALL_PREFIX}/go/bin/go" /usr/bin/go || true
        ln -sf "${INSTALL_PREFIX}/go/bin/gofmt" /usr/bin/gofmt || true
    fi

    cd /
    rm -rf "$TMPDIR"

    if command -v go > /dev/null 2>&1; then
        log_success "Go installed to ${INSTALL_PREFIX}/go"
        return 0
    fi

    log_error "Go installation failed. Please install Go manually."
    return 1
}

compile_source() {
    log_info "Compiling source in ${REPO_DIR}"
    cd "$REPO_DIR"

    # Ensure modules are downloaded
    if command -v go > /dev/null 2>&1; then
        go mod download || log_warning "go mod download failed (proceeding)"
    fi

    mkdir -p build/dist

    # Build in-place (local arch). Avoid stripping flags to keep embed intact.
    BUILD_OUT="build/dist/${BINARY_NAME}"
    log_info "Running go build..."
    if ! go build -v -ldflags "-X main.Version=${VERSION}" -o "${BUILD_OUT}" .; then
        log_error "go build failed"
        log_info "Go environment:"; go env || true
        exit 1
    fi

    log_success "Build successful: ${BUILD_OUT}"

    # Install binary to /usr/bin
    # Some minimal systems (OpenWrt) may not have 'install' command. Try to
    # use it when available, otherwise fallback to mkdir/cp/chmod.
    if command -v install > /dev/null 2>&1; then
        install -d /usr/bin || true
        install -m 0755 "${BUILD_OUT}" "/usr/bin/${BINARY_NAME}"
    else
        mkdir -p /usr/bin || true
        cp "${BUILD_OUT}" "/usr/bin/${BINARY_NAME}"
        chmod 0755 "/usr/bin/${BINARY_NAME}"
    fi
    log_success "Installed binary to /usr/bin/${BINARY_NAME}"
}

# Copy create_config and service installation logic from install.sh
create_config() {
    log_info "Creating configuration file..."
    if [ -f "$CONFIG_FILE" ]; then
        log_info "Configuration file already exists, backing up to ${CONFIG_FILE}.backup"
        cp "$CONFIG_FILE" "${CONFIG_FILE}.backup"
    fi

    dl_interface=""
    ul_interface=""
    if command -v tc > /dev/null 2>&1; then
        cake_interfaces=$(tc qdisc show | grep "qdisc cake" | awk '{print $5}' | head -2 || true)
        if [ -n "$cake_interfaces" ]; then
            dl_interface=$(echo "$cake_interfaces" | grep "ifb" | head -1 || true)
            [ -z "$dl_interface" ] && dl_interface=$(echo "$cake_interfaces" | head -1 || true)
            ul_interface=$(echo "$cake_interfaces" | grep -v "ifb" | head -1 || true)
            [ -z "$ul_interface" ] && ul_interface=$(echo "$cake_interfaces" | tail -1 || true)
        fi
    fi

    cat > "$CONFIG_FILE" << EOF
# cake-autortt configuration file
# Automatically generated during installation

rtt_update_interval: 5
min_hosts: 3
max_hosts: 100
rtt_margin_percent: 10
default_rtt_ms: 100
tcp_connect_timeout: 3
max_concurrent_probes: 50

dl_interface: "${dl_interface}"
ul_interface: "${ul_interface}"

# Logging
debug: false

# Web interface
web_enabled: true
web_port: 11111
EOF

    chmod 644 "$CONFIG_FILE"
    log_success "Configuration file created at $CONFIG_FILE"
    if [ -n "$dl_interface" ] || [ -n "$ul_interface" ]; then
        log_success "Auto-detected interfaces: DL=$dl_interface UL=$ul_interface"
    else
        log_warning "No CAKE interfaces detected. Please configure interfaces manually in $CONFIG_FILE"
    fi
}

install_openwrt_service() {
    log_info "Installing OpenWrt service..."
    cat > "/etc/init.d/${SERVICE_NAME}" << 'EOF'
#!/bin/sh /etc/rc.common
# cake-autortt - automatically adjusts CAKE qdisc RTT parameter

START=99
USE_PROCD=1

PROG="/usr/bin/cake-autortt"
CONF="/etc/cake-autortt.yaml"

validate_config() {
    command -v tc >/dev/null 2>&1 || { echo "ERROR: tc (traffic control) is required but not installed"; return 1; }
    if [ ! -f "$CONF" ]; then echo "ERROR: Configuration file $CONF not found"; return 1; fi
    return 0
}

start_service() {
    validate_config || return 1
    procd_open_instance
    procd_set_param command "$PROG"
    procd_append_param command --config "$CONF"
    procd_set_param pidfile /var/run/cake-autortt.pid
    procd_set_param stdout 1
    procd_set_param stderr 1
    procd_set_param respawn ${respawn_threshold:-3600} ${respawn_timeout:-5} ${respawn_retry:-5}
    echo "Starting cake-autortt with config: $CONF"
    procd_close_instance
}

stop_service() { echo "Stopping cake-autortt"; }
reload_service() { stop; start; }
service_triggers() { procd_add_reload_trigger "cake-autortt"; }
EOF
    chmod +x "/etc/init.d/${SERVICE_NAME}"
    log_success "OpenWrt service installed"
}

install_systemd_service() {
    log_info "Installing systemd service..."
    cat > "/etc/systemd/system/${SERVICE_NAME}.service" << EOF
[Unit]
Description=CAKE Auto RTT Service
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart=/usr/bin/${BINARY_NAME} --config ${CONFIG_FILE}
Restart=always
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload || true
    log_success "Systemd service installed"
}

enable_and_start_service() {
    os_type=$(detect_os)
    case $os_type in
        openwrt)
            log_info "Enabling and starting OpenWrt service..."
            "/etc/init.d/${SERVICE_NAME}" enable || true
            "/etc/init.d/${SERVICE_NAME}" start || true
            sleep 2
            if "/etc/init.d/${SERVICE_NAME}" status > /dev/null 2>&1; then
                log_success "Service enabled and started successfully"
            else
                log_warning "Service enabled but may not be running. Check configuration."
            fi
            ;;
        systemd)
            log_info "Enabling and starting systemd service..."
            systemctl enable "${SERVICE_NAME}" || true
            systemctl start "${SERVICE_NAME}" || true
            sleep 2
            if systemctl is-active "${SERVICE_NAME}" > /dev/null 2>&1; then
                log_success "Service enabled and started successfully"
            else
                log_warning "Service enabled but may not be running. Check configuration."
            fi
            ;;
        *)
            log_warning "No supported service manager detected. Service not auto-started."
            log_info "You can start the service manually with: /usr/bin/${BINARY_NAME} --config ${CONFIG_FILE}"
            ;;
    esac
}

show_completion_info() {
    log_success "\n=== Installation completed successfully! ==="
    log_info "Version: ${VERSION}"
    log_info "Binary: /usr/bin/${BINARY_NAME}"
    log_info "Config: ${CONFIG_FILE}"
    os_type=$(detect_os)
    case $os_type in
        openwrt)
            log_info "Service management:"
            log_info "  Start:   /etc/init.d/${SERVICE_NAME} start"
            log_info "  Stop:    /etc/init.d/${SERVICE_NAME} stop"
            log_info "  Restart: /etc/init.d/${SERVICE_NAME} restart"
            log_info "  Status:  /etc/init.d/${SERVICE_NAME} status"
            log_info "  Logs:    logread | grep ${SERVICE_NAME}"
            ;;
        systemd)
            log_info "Service management:"
            log_info "  Start:   systemctl start ${SERVICE_NAME}"
            log_info "  Stop:    systemctl stop ${SERVICE_NAME}"
            log_info "  Restart: systemctl restart ${SERVICE_NAME}"
            log_info "  Status:  systemctl status ${SERVICE_NAME}"
            log_info "  Logs:    journalctl -u ${SERVICE_NAME} -f"
            ;;
    esac
    log_info "Web interface: http://localhost:11111"
    log_info "To customize settings, edit: ${CONFIG_FILE} and restart the service"
}

# Main
main() {
    check_root
    check_dependencies

    if ! ensure_go; then
        log_error "Go installation failed - aborting"
        exit 1
    fi

    compile_source
    create_config

    os_type=$(detect_os)
    case $os_type in
        openwrt) install_openwrt_service ;;
        systemd) install_systemd_service ;;
        *) log_warning "No supported service manager detected" ;;
    esac

    enable_and_start_service
    show_completion_info
}

main "$@"
