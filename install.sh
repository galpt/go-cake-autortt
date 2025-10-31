#!/bin/sh

# cake-autortt Go version installation script
# Fully automated installation with service management
# Compatible with OpenWrt, Ubuntu, Debian, and other Linux distributions

set -e

VERSION="2.1.0"
REPO_URL="https://github.com/galpt/go-cake-autortt"
BINARY_NAME="cake-autortt"
SERVICE_NAME="cake-autortt"
CONFIG_FILE="/etc/cake-autortt.yaml"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if running as root
check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root"
        log_info "Run with: sudo $0 or as root user"
        exit 1
    fi
}

    # Install on-disk web templates so system service (with different working dir)
    # can prefer local templates. Copies templates from the script directory to
    # /usr/share/cake-autortt/web/templates when available.
    install_templates() {
        log_info "Installing web templates (if present)"

        # Resolve script directory (where install.sh lives)
        SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

        # On OpenWrt prefer /etc for persistent writable config/data; on full Linux
        # distros prefer /usr/share. Detect OpenWrt by presence of /etc/openwrt_release
        if [ -f /etc/openwrt_release ]; then
            TARGET_BASE="/etc/cake-autortt/web"
        else
            TARGET_BASE="/usr/share/cake-autortt/web"
        fi

        # Look for web/templates in multiple likely locations so the installer works
        # whether invoked as ./install.sh or as /install.sh (which makes $0's dir '/').
        FOUND=""
        for c in \
            "$SCRIPT_DIR/web/templates" \
            "$PWD/web/templates" \
            "$SCRIPT_DIR/../web/templates" \
            "$(cd "$SCRIPT_DIR" && cd .. >/dev/null 2>&1 && pwd)/web/templates"; do
            if [ -d "$c" ]; then
                FOUND="$c"
                break
            fi
        done

        if [ -n "$FOUND" ]; then
            log_info "Found local web/templates in $FOUND, copying to $TARGET_BASE"
            mkdir -p "$TARGET_BASE"
            cp -r "$FOUND" "$TARGET_BASE"
            chmod -R 755 "$TARGET_BASE"
            log_success "Web templates installed to $TARGET_BASE/templates (source: $FOUND)"
        else
            log_info "No local web/templates found in candidates, skipping template installation"
        fi
    }

# Detect system architecture
detect_arch() {
    local arch=$(uname -m)
    case $arch in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l)
            echo "armv7"
            ;;
        armv6l)
            echo "armv6"
            ;;
        i386|i686)
            echo "386"
            ;;
        mips)
            echo "mips"
            ;;
        mipsel)
            echo "mipsle"
            ;;
        mips64)
            echo "mips64"
            ;;
        mips64el)
            echo "mips64le"
            ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
}

# Detect operating system
detect_os() {
    if [ -f /etc/openwrt_release ]; then
        echo "openwrt"
    elif command -v systemctl > /dev/null 2>&1; then
        echo "systemd"
    else
        echo "linux"
    fi
}

# Check dependencies
check_dependencies() {
    log_info "Checking dependencies..."
    
    # Check for tc (traffic control)
    if ! command -v tc > /dev/null 2>&1; then
        log_error "tc (traffic control) is required but not installed"
        log_info "Please install iproute2 package"
        exit 1
    fi
    
    # Check for wget or curl
    if ! command -v wget > /dev/null 2>&1 && ! command -v curl > /dev/null 2>&1; then
        log_error "wget or curl is required for downloading"
        exit 1
    fi
    
    log_success "Dependencies check passed"
}

# Download binary
download_binary() {
    local arch=$(detect_arch)
    local binary_name="${BINARY_NAME}-linux-${arch}"
    local download_url="${REPO_URL}/releases/latest/download/${binary_name}.tar.gz"
    
    log_info "Downloading ${binary_name}..."
    
    # Create temporary directory
    local temp_dir=$(mktemp -d)
    cd "$temp_dir"
    
    # Download with wget or curl
    if command -v wget > /dev/null 2>&1; then
        wget -q "$download_url" -O "${binary_name}.tar.gz" || {
            log_error "Failed to download binary from $download_url"
            exit 1
        }
    else
        curl -sL "$download_url" -o "${binary_name}.tar.gz" || {
            log_error "Failed to download binary from $download_url"
            exit 1
        }
    fi
    
    # Extract binary
    tar -xzf "${binary_name}.tar.gz" || {
        log_error "Failed to extract binary"
        exit 1
    }
    
    # Install binary
    cp "$binary_name" "/usr/bin/$BINARY_NAME" || {
        log_error "Failed to install binary"
        exit 1
    }
    chmod 755 "/usr/bin/$BINARY_NAME"
    
    # Cleanup
    cd /
    rm -rf "$temp_dir"
    
    log_success "Binary installed to /usr/bin/$BINARY_NAME"
}

# Create YAML configuration file
create_config() {
    log_info "Creating configuration file..."
    
    if [ -f "$CONFIG_FILE" ]; then
        log_info "Configuration file already exists, backing up to ${CONFIG_FILE}.backup"
        cp "$CONFIG_FILE" "${CONFIG_FILE}.backup"
    fi
    
    # Auto-detect interfaces with CAKE qdisc
    local dl_interface=""
    local ul_interface=""
    
    if command -v tc > /dev/null 2>&1; then
        local cake_interfaces=$(tc qdisc show | grep "qdisc cake" | awk '{print $5}' | head -2)
        if [ -n "$cake_interfaces" ]; then
            # Try to detect download interface (typically ifb-*)
            dl_interface=$(echo "$cake_interfaces" | grep "ifb" | head -1)
            [ -z "$dl_interface" ] && dl_interface=$(echo "$cake_interfaces" | head -1)
            
            # Try to detect upload interface (typically physical interface)
            ul_interface=$(echo "$cake_interfaces" | grep -v "ifb" | head -1)
            [ -z "$ul_interface" ] && ul_interface=$(echo "$cake_interfaces" | tail -1)
        fi
    fi
    
    cat > "$CONFIG_FILE" << EOF
# cake-autortt configuration file
# Automatically generated during installation

# RTT measurement settings
rtt_update_interval: 5        # seconds between qdisc RTT updates
min_hosts: 3                  # minimum number of hosts needed for RTT calculation
max_hosts: 100                # maximum number of hosts to probe simultaneously
rtt_margin_percent: 10        # percentage margin added to measured RTT
default_rtt_ms: 100           # default RTT in case no hosts are available
tcp_connect_timeout: 3        # TCP connection timeout for RTT measurement
max_concurrent_probes: 50     # maximum concurrent TCP probes

# Network interfaces (auto-detected during installation)
dl_interface: "$dl_interface"             # download interface
ul_interface: "$ul_interface"             # upload interface

# Logging
debug: false                  # enable debug logging

# Web interface
web_enabled: true             # enable web server
web_port: 11111               # web server port
EOF
    
    chmod 644 "$CONFIG_FILE"
    log_success "Configuration file created at $CONFIG_FILE"
    
    if [ -n "$dl_interface" ] || [ -n "$ul_interface" ]; then
        log_success "Auto-detected interfaces: DL=$dl_interface UL=$ul_interface"
    else
        log_warning "No CAKE interfaces detected. Please configure interfaces manually in $CONFIG_FILE"
    fi
}

# Install OpenWrt service
install_openwrt_service() {
    log_info "Installing OpenWrt service..."
    
    cat > "/etc/init.d/$SERVICE_NAME" << 'EOF'
#!/bin/sh /etc/rc.common
# cake-autortt - automatically adjusts CAKE qdisc RTT parameter

START=99
USE_PROCD=1

PROG="/usr/bin/cake-autortt"
CONF="/etc/cake-autortt.yaml"

validate_config() {
	# Check if tc is available
	command -v tc >/dev/null 2>&1 || {
		echo "ERROR: tc (traffic control) is required but not installed"
		return 1
	}
	
	# Check if config file exists
	if [ ! -f "$CONF" ]; then
		echo "ERROR: Configuration file $CONF not found"
		return 1
	fi
	
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

stop_service() {
	echo "Stopping cake-autortt"
}

reload_service() {
	stop
	start
}

service_triggers() {
	procd_add_reload_trigger "cake-autortt"
}
EOF
    
    chmod +x "/etc/init.d/$SERVICE_NAME"
    log_success "OpenWrt service installed"
}

# Install systemd service
install_systemd_service() {
    log_info "Installing systemd service..."
    
    cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=CAKE Auto RTT Service
After=network.target
Wants=network.target

[Service]
Type=simple
# Ensure the service has a predictable working directory so on-disk templates
# (installed to /usr/share/cake-autortt/web/templates) are found by the server.
WorkingDirectory=/usr/share/cake-autortt
ExecStart=/usr/bin/$BINARY_NAME --config $CONFIG_FILE
Restart=always
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF
    
    systemctl daemon-reload
    log_success "Systemd service installed"
}

# Enable and start service automatically
enable_and_start_service() {
    local os_type=$(detect_os)
    
    case $os_type in
        "openwrt")
            log_info "Enabling and starting OpenWrt service..."
            "/etc/init.d/$SERVICE_NAME" enable
            "/etc/init.d/$SERVICE_NAME" start
            
            # Check if service started successfully
            sleep 2
            if "/etc/init.d/$SERVICE_NAME" status > /dev/null 2>&1; then
                log_success "Service enabled and started successfully"
            else
                log_warning "Service enabled but may not be running. Check configuration."
            fi
            ;;
        "systemd")
            log_info "Enabling and starting systemd service..."
            systemctl enable "$SERVICE_NAME"
            systemctl start "$SERVICE_NAME"
            
            # Check if service started successfully
            sleep 2
            if systemctl is-active "$SERVICE_NAME" > /dev/null 2>&1; then
                log_success "Service enabled and started successfully"
            else
                log_warning "Service enabled but may not be running. Check configuration."
            fi
            ;;
        *)
            log_warning "No supported service manager detected. Service not auto-started."
            log_info "You can start the service manually with: /usr/bin/$BINARY_NAME --config $CONFIG_FILE"
            ;;
    esac
}

# Display post-installation information
show_completion_info() {
    log_success "\n=== Installation completed successfully! ==="
    log_info "Version: $VERSION"
    log_info "Binary: /usr/bin/$BINARY_NAME"
    log_info "Config: $CONFIG_FILE"
    
    local os_type=$(detect_os)
    case $os_type in
        "openwrt")
            log_info "Service management:"
            log_info "  Start:   /etc/init.d/$SERVICE_NAME start"
            log_info "  Stop:    /etc/init.d/$SERVICE_NAME stop"
            log_info "  Restart: /etc/init.d/$SERVICE_NAME restart"
            log_info "  Status:  /etc/init.d/$SERVICE_NAME status"
            log_info "  Logs:    logread | grep cake-autortt"
            ;;
        "systemd")
            log_info "Service management:"
            log_info "  Start:   systemctl start $SERVICE_NAME"
            log_info "  Stop:    systemctl stop $SERVICE_NAME"
            log_info "  Restart: systemctl restart $SERVICE_NAME"
            log_info "  Status:  systemctl status $SERVICE_NAME"
            log_info "  Logs:    journalctl -u $SERVICE_NAME -f"
            ;;
    esac
    
    log_info "Web interface: http://localhost:11111"
    log_info "\nTo customize settings, edit: $CONFIG_FILE"
    log_info "Then restart the service to apply changes."
    
    # Show current service status
    case $os_type in
        "openwrt")
            if "/etc/init.d/$SERVICE_NAME" status > /dev/null 2>&1; then
                log_success "Service is currently running"
            else
                log_warning "Service is not running - check configuration"
            fi
            ;;
        "systemd")
            if systemctl is-active "$SERVICE_NAME" > /dev/null 2>&1; then
                log_success "Service is currently running"
            else
                log_warning "Service is not running - check configuration"
            fi
            ;;
    esac
}

# Main installation function
main() {
    log_info "Starting cake-autortt automated installation v$VERSION"
    log_info "This will install and configure cake-autortt as a system service"
    
    check_root
    check_dependencies
    download_binary
    create_config
    install_templates
    
    # Install appropriate service
    local os_type=$(detect_os)
    case $os_type in
        "openwrt")
            install_openwrt_service
            ;;
        "systemd")
            install_systemd_service
            ;;
        *)
            log_warning "No supported service manager detected"
            ;;
    esac
    
    # Automatically enable and start service
    enable_and_start_service
    
    # Show completion information
    show_completion_info
}

# Run main function
main "$@"