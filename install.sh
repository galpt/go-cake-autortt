#!/bin/sh

# cake-autortt Go version installation script
# This script installs cake-autortt as a system service
# Compatible with OpenWrt (ash/busybox) and other POSIX shells

set -e

VERSION="2.0.0"
REPO_URL="https://github.com/galpt/go-cake-autortt"
BINARY_NAME="cake-autortt"
SERVICE_NAME="cake-autortt"

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
    # On OpenWrt, check if we're root without relying on sudo
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be run as root"
        log_info "On OpenWrt, run directly as root without sudo:"
        log_info "curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | ash"
        exit 1
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
    case "$OSTYPE" in
        linux-gnu*)
            echo "linux"
            ;;
        freebsd*)
            echo "freebsd"
            ;;
        *)
            # Fallback detection for systems without OSTYPE
            if [ -f /etc/openwrt_release ]; then
                echo "linux"
            elif uname -s | grep -q "Linux"; then
                echo "linux"
            elif uname -s | grep -q "FreeBSD"; then
                echo "freebsd"
            else
                log_error "Unsupported operating system: $(uname -s)"
                exit 1
            fi
            ;;
    esac
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
    local os=$(detect_os)
    local arch=$(detect_arch)
    local binary_name="${BINARY_NAME}-${os}-${arch}"
    local download_url="${REPO_URL}/releases/latest/download/${binary_name}.tar.gz"
    
    log_info "Downloading ${binary_name}..."
    log_info "URL: $download_url"
    
    # Create temporary directory
    local temp_dir=$(mktemp -d)
    cd "$temp_dir"
    
    # Download with wget or curl
    if command -v wget > /dev/null 2>&1; then
        wget -q "$download_url" -O "${binary_name}.tar.gz"
    else
        curl -sL "$download_url" -o "${binary_name}.tar.gz"
    fi
    
    if [ $? -ne 0 ]; then
        log_error "Failed to download binary"
        exit 1
    fi
    
    # Extract binary
    tar -xzf "${binary_name}.tar.gz"
    
    # Install binary (busybox compatible)
    cp "$binary_name" "/usr/bin/$BINARY_NAME"
    chmod 755 "/usr/bin/$BINARY_NAME"
    
    # Cleanup
    cd /
    rm -rf "$temp_dir"
    
    log_success "Binary installed to /usr/bin/$BINARY_NAME"
}

# Install configuration files
install_config() {
    log_info "Installing configuration files..."
    
    # Create directories
    mkdir -p /etc/config
    mkdir -p /etc/init.d
    mkdir -p /etc/hotplug.d/iface
    
    # Install config file if it doesn't exist
    if [ ! -f "/etc/config/$SERVICE_NAME" ]; then
        cat > "/etc/config/$SERVICE_NAME" << 'EOF'
config cake-autortt 'global'
	option rtt_update_interval '5'
	option min_hosts '3'
	option max_hosts '100'
	option rtt_margin_percent '10'
	option default_rtt_ms '100'
	option dl_interface ''
	option ul_interface ''
	option debug '0'
	option tcp_connect_timeout '3'
	option max_concurrent_probes '50'
	option web_enabled '1'
	option web_port '11111'
EOF
        log_success "Configuration file created at /etc/config/$SERVICE_NAME"
        log_warning "Please edit /etc/config/$SERVICE_NAME to configure your interfaces"
    else
        log_info "Configuration file already exists, skipping"
    fi
}

# Install OpenWrt service files
install_openwrt_service() {
    if [ -d "/etc/init.d" ] && command -v uci > /dev/null 2>&1; then
        log_info "Installing OpenWrt service files..."
        
        # Download and install init script
        local init_script_url="${REPO_URL}/raw/main/etc/init.d/cake-autortt"
        if command -v wget > /dev/null 2>&1; then
            wget -q "$init_script_url" -O "/etc/init.d/$SERVICE_NAME"
        else
            curl -sL "$init_script_url" -o "/etc/init.d/$SERVICE_NAME"
        fi
        
        chmod +x "/etc/init.d/$SERVICE_NAME"
        
        # Download and install hotplug script
        local hotplug_script_url="${REPO_URL}/raw/main/etc/hotplug.d/iface/99-cake-autortt"
        if command -v wget > /dev/null 2>&1; then
            wget -q "$hotplug_script_url" -O "/etc/hotplug.d/iface/99-cake-autortt"
        else
            curl -sL "$hotplug_script_url" -o "/etc/hotplug.d/iface/99-cake-autortt"
        fi
        
        chmod +x "/etc/hotplug.d/iface/99-cake-autortt"
        
        log_success "OpenWrt service files installed"
        return 0
    fi
    return 1
}

# Install systemd service
install_systemd_service() {
    if command -v systemctl > /dev/null 2>&1; then
        log_info "Installing systemd service..."
        
        cat > "/etc/systemd/system/$SERVICE_NAME.service" << EOF
[Unit]
Description=CAKE Auto RTT Service
After=network.target
Wants=network.target

[Service]
Type=simple
ExecStart=/usr/bin/$BINARY_NAME
Restart=always
RestartSec=5
User=root
Group=root

[Install]
WantedBy=multi-user.target
EOF
        
        systemctl daemon-reload
        log_success "Systemd service installed"
        return 0
    fi
    return 1
}

# Enable and start service
enable_service() {
    if command -v systemctl > /dev/null 2>&1; then
        log_info "Enabling and starting systemd service..."
        systemctl enable "$SERVICE_NAME"
        systemctl start "$SERVICE_NAME"
        log_success "Service enabled and started"
    elif [ -f "/etc/init.d/$SERVICE_NAME" ]; then
        log_info "Enabling OpenWrt service..."
        "/etc/init.d/$SERVICE_NAME" enable
        "/etc/init.d/$SERVICE_NAME" start
        log_success "Service enabled and started"
    else
        log_warning "No service manager detected. You'll need to start $BINARY_NAME manually."
    fi
}

# Main installation function
main() {
    log_info "Installing cake-autortt Go version v$VERSION"
    
    check_root
    check_dependencies
    download_binary
    install_config
    
    # Try to install service files
    if ! install_openwrt_service; then
        install_systemd_service
    fi
    
    enable_service
    
    log_success "Installation completed successfully!"
    log_info "Please edit /etc/config/$SERVICE_NAME to configure your network interfaces"
    log_info "Then restart the service with: /etc/init.d/$SERVICE_NAME restart"
    log_info "Web interface will be available at: http://localhost:11111/cake-autortt"
}

# Run main function
main "$@"