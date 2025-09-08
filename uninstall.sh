#!/bin/sh

# cake-autortt Go version uninstallation script
# This script removes cake-autortt service from the system
# Compatible with OpenWrt (ash/busybox) and systemd systems

set -e

VERSION="2.0.0"
BINARY_NAME="cake-autortt"
SERVICE_NAME="cake-autortt"
INSTALL_ROOT="${INSTALL_ROOT:-}"

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
        log_info "Please run with sudo or as root user"
        exit 1
    fi
}

# Stop and disable OpenWrt service
stop_openwrt_service() {
    if [ -f "${INSTALL_ROOT}/etc/init.d/$SERVICE_NAME" ] && [ -z "$INSTALL_ROOT" ]; then
        log_info "Stopping OpenWrt service..."
        
        # Stop the service
        if "/etc/init.d/$SERVICE_NAME" status >/dev/null 2>&1; then
            log_info "Stopping $SERVICE_NAME service..."
            "/etc/init.d/$SERVICE_NAME" stop 2>/dev/null || true
        fi
        
        # Disable the service
        log_info "Disabling $SERVICE_NAME service..."
        "/etc/init.d/$SERVICE_NAME" disable 2>/dev/null || true
        
        return 0
    fi
    return 1
}

# Stop and disable systemd service
stop_systemd_service() {
    if command -v systemctl > /dev/null 2>&1 && [ -f "${INSTALL_ROOT}/etc/systemd/system/$SERVICE_NAME.service" ]; then
        log_info "Stopping systemd service..."
        
        # Stop the service
        systemctl stop "$SERVICE_NAME" 2>/dev/null || true
        
        # Disable the service
        systemctl disable "$SERVICE_NAME" 2>/dev/null || true
        
        # Reload systemd
        systemctl daemon-reload 2>/dev/null || true
        
        return 0
    fi
    return 1
}

# Stop and disable service
stop_and_disable_service() {
    log_info "Stopping and disabling $SERVICE_NAME service..."
    
    # Try OpenWrt first, then systemd
    if ! stop_openwrt_service; then
        if ! stop_systemd_service; then
            log_warning "No service manager detected or service not found"
        fi
    fi
}

# Remove files
remove_files() {
    log_info "Removing $SERVICE_NAME files..."
    
    # Remove executable
    if [ -f "${INSTALL_ROOT}/usr/bin/$BINARY_NAME" ]; then
        log_info "Removing executable: /usr/bin/$BINARY_NAME"
        rm -f "${INSTALL_ROOT}/usr/bin/$BINARY_NAME"
    fi
    
    # Remove OpenWrt init script
    if [ -f "${INSTALL_ROOT}/etc/init.d/$SERVICE_NAME" ]; then
        log_info "Removing OpenWrt init script: /etc/init.d/$SERVICE_NAME"
        rm -f "${INSTALL_ROOT}/etc/init.d/$SERVICE_NAME"
    fi
    
    # Remove OpenWrt hotplug script
    if [ -f "${INSTALL_ROOT}/etc/hotplug.d/iface/99-$SERVICE_NAME" ]; then
        log_info "Removing hotplug script: /etc/hotplug.d/iface/99-$SERVICE_NAME"
        rm -f "${INSTALL_ROOT}/etc/hotplug.d/iface/99-$SERVICE_NAME"
    fi
    
    # Remove systemd service file
    if [ -f "${INSTALL_ROOT}/etc/systemd/system/$SERVICE_NAME.service" ]; then
        log_info "Removing systemd service file: /etc/systemd/system/$SERVICE_NAME.service"
        rm -f "${INSTALL_ROOT}/etc/systemd/system/$SERVICE_NAME.service"
        # Reload systemd after removing service file
        if command -v systemctl > /dev/null 2>&1; then
            systemctl daemon-reload 2>/dev/null || true
        fi
    fi
    
    # Remove runtime files
    if [ -f "${INSTALL_ROOT}/var/run/$SERVICE_NAME.pid" ]; then
        log_info "Removing PID file: /var/run/$SERVICE_NAME.pid"
        rm -f "${INSTALL_ROOT}/var/run/$SERVICE_NAME.pid"
    fi
    
    # Remove temporary files
    if [ -f "${INSTALL_ROOT}/tmp/$SERVICE_NAME-hosts" ]; then
        log_info "Removing temporary hosts file: /tmp/$SERVICE_NAME-hosts"
        rm -f "${INSTALL_ROOT}/tmp/$SERVICE_NAME-hosts"
    fi
    
    if [ -f "${INSTALL_ROOT}/dev/shm/$SERVICE_NAME-hosts" ]; then
        log_info "Removing temporary hosts file: /dev/shm/$SERVICE_NAME-hosts"
        rm -f "${INSTALL_ROOT}/dev/shm/$SERVICE_NAME-hosts"
    fi
}

# Handle configuration file
handle_config_file() {
    if [ -f "${INSTALL_ROOT}/etc/config/$SERVICE_NAME" ]; then
        echo
        if [ "${FORCE_UNINSTALL:-}" = "true" ]; then
            remove_config="y"
        else
            printf "Remove configuration file /etc/config/$SERVICE_NAME? (y/N): "
            read -r remove_config
        fi
        
        if [ "$remove_config" = "y" ] || [ "$remove_config" = "Y" ]; then
            log_info "Removing configuration file: /etc/config/$SERVICE_NAME"
            rm -f "${INSTALL_ROOT}/etc/config/$SERVICE_NAME"
        else
            log_info "Keeping configuration file for potential future use"
        fi
    fi
}

# Handle backup files
handle_backup_files() {
    local backup_files=""
    
    # Check for backup files
    for file in \
        "${INSTALL_ROOT}/usr/bin/$BINARY_NAME.bak" \
        "${INSTALL_ROOT}/etc/init.d/$SERVICE_NAME.bak" \
        "${INSTALL_ROOT}/etc/config/$SERVICE_NAME.bak" \
        "${INSTALL_ROOT}/etc/hotplug.d/iface/99-$SERVICE_NAME.bak" \
        "${INSTALL_ROOT}/etc/systemd/system/$SERVICE_NAME.service.bak"; do
        if [ -f "$file" ]; then
            backup_files="$backup_files $file"
        fi
    done
    
    if [ -n "$backup_files" ]; then
        echo
        log_info "Found backup files from previous installation:"
        for file in $backup_files; do
            echo "  - $file"
        done
        
        if [ "${FORCE_UNINSTALL:-}" = "true" ]; then
            remove_backups="y"
        else
            printf "Remove backup files? (y/N): "
            read -r remove_backups
        fi
        
        if [ "$remove_backups" = "y" ] || [ "$remove_backups" = "Y" ]; then
            for file in $backup_files; do
                log_info "Removing backup file: $file"
                rm -f "$file"
            done
        else
            log_info "Keeping backup files"
        fi
    fi
}

# Show completion message
show_completion_message() {
    echo
    log_success "$SERVICE_NAME has been uninstalled successfully!"
    echo
    echo "The following may still be present on your system:"
    echo "- CAKE qdisc configuration (not modified by this script)"
    echo "- Log entries in system logs (this is normal system behavior)"
    if [ -f "${INSTALL_ROOT}/etc/config/$SERVICE_NAME" ]; then
        echo "- Configuration file (kept at your request)"
    fi
    echo
    echo "Note about system logs:"
    echo "- System logs are managed by the system's logging service"
    echo "- Old logs will naturally rotate out based on system configuration"
    echo "- This is expected behavior and not a cleanup issue"
    echo
    echo "If you want to completely remove all traces:"
    echo "- Review CAKE qdisc settings: tc qdisc show"
    echo "- System logs will clear naturally over time"
    echo "- Check for any remaining configuration in /etc/config/ (OpenWrt)"
}

# Main uninstallation function
main() {
    echo "$SERVICE_NAME Go version v$VERSION Uninstaller"
    echo "==============================================="
    echo
    
    # Check if cake-autortt is installed
    if [ ! -f "${INSTALL_ROOT}/usr/bin/$BINARY_NAME" ] && \
       [ ! -f "${INSTALL_ROOT}/etc/init.d/$SERVICE_NAME" ] && \
       [ ! -f "${INSTALL_ROOT}/etc/systemd/system/$SERVICE_NAME.service" ]; then
        log_warning "$SERVICE_NAME does not appear to be installed"
        exit 0
    fi
    
    if [ "${FORCE_UNINSTALL:-}" != "true" ]; then
        echo "This will remove $SERVICE_NAME from your system."
        printf "Are you sure you want to continue? (y/N): "
        read -r confirm
        if [ "$confirm" != "y" ] && [ "$confirm" != "Y" ]; then
            log_info "Uninstallation cancelled"
            exit 0
        fi
    fi
    
    echo
    
    # Stop and disable service
    stop_and_disable_service
    
    # Remove files
    remove_files
    
    # Handle configuration file
    handle_config_file
    
    # Handle backup files
    handle_backup_files
    
    # Show completion message
    show_completion_message
}

# Handle command line arguments
case "${1:-}" in
    --help|-h)
        echo "Usage: $0 [OPTIONS]"
        echo
        echo "Uninstalls $SERVICE_NAME service from the system"
        echo
        echo "OPTIONS:"
        echo "  --help, -h          Show this help message"
        echo "  --force             Skip confirmation prompts"
        echo
        echo "Environment Variables:"
        echo "  INSTALL_ROOT        Override installation root (for testing)"
        echo "  FORCE_UNINSTALL     Set to 'true' to skip all prompts"
        echo
        echo "Examples:"
        echo "  $0                  Interactive uninstallation"
        echo "  $0 --force          Force uninstallation without prompts"
        echo "  FORCE_UNINSTALL=true $0  Force uninstallation via environment"
        exit 0
        ;;
    --force)
        export FORCE_UNINSTALL="true"
        ;;
esac

# Check root privileges
check_root

# Run main function
main "$@"