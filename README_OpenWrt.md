# OpenWrt Installation Guide

**Languages:** [English](README_OpenWrt.md) | [中文](README_OpenWrt_zh.md)

**Main README:** [English](README.md) | [中文](README_zh.md)

This guide shows how to install cake-autortt on OpenWrt with **fully automated installation** and YAML configuration.

## 🚀 Automated Installation (Recommended)

The easiest way to install cake-autortt on OpenWrt is using the automated installation script:

```bash
# Run as root (no sudo needed on OpenWrt)
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | ash
```

### What the Installation Script Does

1. ✅ **Checks Dependencies**: Verifies `tc` and `wget`/`curl` are available
2. ✅ **Downloads Binary**: Gets the correct MIPS binary for your OpenWrt architecture
3. ✅ **Installs Binary**: Places it in `/usr/bin/cake-autortt` with proper permissions
4. ✅ **Auto-Detects Interfaces**: Scans for existing CAKE qdisc interfaces
5. ✅ **Creates Configuration**: Generates `/etc/cake-autortt.yaml` with detected settings
6. ✅ **Installs Service**: Creates `/etc/init.d/cake-autortt` service script
7. ✅ **Enables Auto-Start**: Configures service to start on boot
8. ✅ **Starts Service**: Immediately starts the service
9. ✅ **Shows Status**: Displays management commands and web interface URL

### Post-Installation

After installation completes:
- **Web Interface**: Access at `http://your-router-ip:11111`
- **Service Status**: Check with `/etc/init.d/cake-autortt status`
- **Configuration**: Edit `/etc/cake-autortt.yaml` if needed

## 📋 Manual Installation

If you prefer manual installation:

### 1. Download and Install Binary

```bash
# For MIPS architecture (most OpenWrt routers)
wget https://github.com/galpt/go-cake-autortt/releases/latest/download/cake-autortt-linux-mips.tar.gz
tar -xzf cake-autortt-linux-mips.tar.gz
cp cake-autortt-linux-mips /usr/bin/cake-autortt
chmod 755 /usr/bin/cake-autortt

# For other architectures, check: uname -m
# Available: mips, mipsle, mips64, mips64le, arm64, armv7, armv6
```

### 2. Create Configuration File

```bash
cat > /etc/cake-autortt.yaml << 'EOF'
# RTT measurement settings
rtt_update_interval: 5        # seconds between qdisc RTT updates
min_hosts: 3                  # minimum number of hosts needed for RTT calculation
max_hosts: 100                # maximum number of hosts to probe simultaneously
rtt_margin_percent: 10        # percentage margin added to measured RTT
default_rtt_ms: 100           # default RTT in case no hosts are available
tcp_connect_timeout: 3        # TCP connection timeout for RTT measurement
max_concurrent_probes: 50     # maximum concurrent TCP probes

# Network interfaces (configure your CAKE interfaces)
dl_interface: ""              # download interface (e.g., "ifb-wan")
ul_interface: ""              # upload interface (e.g., "wan")

# Logging
debug: false                  # enable debug logging

# Web interface
web_enabled: true             # enable web server
web_port: 11111               # web server port
EOF
```

### 3. Configure Interfaces

Find your CAKE interfaces and update the config:

```bash
# List interfaces with CAKE qdisc
tc qdisc show | grep cake

# Example output:
# qdisc cake 8001: dev ifb-wan root refcnt 2 bandwidth 100Mbit
# qdisc cake 8002: dev wan root refcnt 2 bandwidth 20Mbit

# Edit config with your interfaces
vi /etc/cake-autortt.yaml
# Set:
# dl_interface: "ifb-wan"
# ul_interface: "wan"
```

### 4. Create Service Script

```bash
cat > /etc/init.d/cake-autortt << 'EOF'
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

chmod +x /etc/init.d/cake-autortt
```

### 5. Enable and Start Service

```bash
# Enable auto-start on boot
/etc/init.d/cake-autortt enable

# Start the service
/etc/init.d/cake-autortt start

# Check status
/etc/init.d/cake-autortt status
```

## ⚙️ Configuration

### Configuration File: `/etc/cake-autortt.yaml`

The configuration uses YAML format (similar to AdGuard Home):

```yaml
# RTT measurement settings
rtt_update_interval: 5        # seconds between qdisc RTT updates
min_hosts: 3                  # minimum number of hosts needed for RTT calculation
max_hosts: 100                # maximum number of hosts to probe simultaneously
rtt_margin_percent: 10        # percentage margin added to measured RTT
default_rtt_ms: 100           # default RTT in case no hosts are available
tcp_connect_timeout: 3        # TCP connection timeout for RTT measurement
max_concurrent_probes: 50     # maximum concurrent TCP probes

# Network interfaces (auto-detected during installation)
dl_interface: "ifb-wan"       # download interface with CAKE qdisc
ul_interface: "wan"           # upload interface with CAKE qdisc

# Logging
debug: false                  # enable debug logging

# Web interface
web_enabled: true             # enable web server
web_port: 11111               # web server port
```

### Interface Detection

**Automatic Detection (Recommended):**
The installation script automatically detects interfaces with CAKE qdisc.

**Manual Configuration:**
```bash
# Find CAKE interfaces
tc qdisc show | grep cake

# Common OpenWrt interface patterns:
# dl_interface: "ifb-wan"    # Download shaping (IFB interface)
# ul_interface: "wan"        # Upload shaping (WAN interface)
# ul_interface: "eth1"       # Alternative WAN interface
```

## 🎯 Service Management

```bash
# Start service
/etc/init.d/cake-autortt start

# Stop service
/etc/init.d/cake-autortt stop

# Restart service
/etc/init.d/cake-autortt restart

# Check status
/etc/init.d/cake-autortt status

# Enable auto-start on boot
/etc/init.d/cake-autortt enable

# Disable auto-start
/etc/init.d/cake-autortt disable

# View logs
logread | grep cake-autortt

# Follow logs in real-time
logread -f | grep cake-autortt
```

## 📊 Monitoring

### Web Interface

Access the web interface at: `http://your-router-ip:11111`

Features:
- Real-time RTT measurements
- CAKE qdisc statistics
- Live system logs
- Interface status
- Configuration overview

![Web UI Screenshot](images/web-ui-cake-autortt.png)

### Command Line Monitoring

```bash
# Enable debug logging
vi /etc/cake-autortt.yaml
# Set: debug: true

# Restart service to apply changes
/etc/init.d/cake-autortt restart

# View debug logs
logread | grep cake-autortt
```

### Manual Testing

```bash
# Test configuration
/usr/bin/cake-autortt --config /etc/cake-autortt.yaml --debug

# Check CAKE qdisc status
tc qdisc show | grep cake

# Monitor RTT changes
logread -f | grep "Adjusted CAKE RTT"
```

## 🔍 Troubleshooting

### Common Issues

**1. Service won't start:**
```bash
# Check service status
/etc/init.d/cake-autortt status

# Check configuration
/usr/bin/cake-autortt --config /etc/cake-autortt.yaml --debug

# Verify config file exists
ls -la /etc/cake-autortt.yaml
```

**2. No CAKE interfaces detected:**
```bash
# Check if CAKE qdisc is configured
tc qdisc show | grep cake

# If no CAKE found, configure it (example):
tc qdisc add dev wan root cake bandwidth 100mbit
tc qdisc add dev ifb-wan root cake bandwidth 100mbit
```

**3. Web interface not accessible:**
```bash
# Check if service is running
/etc/init.d/cake-autortt status

# Check firewall (if enabled)
iptables -L | grep 11111

# Test local access
wget -O- http://localhost:11111 2>/dev/null | head
```

**4. No RTT measurements:**
```bash
# Check conntrack
ls -la /proc/net/nf_conntrack

# Check if hosts are being extracted
logread | grep "Extracted.*hosts"

# Enable debug mode for detailed logs
vi /etc/cake-autortt.yaml  # Set debug: true
/etc/init.d/cake-autortt restart
```

### Debug Commands

```bash
# Check system info
uname -a
cat /etc/openwrt_release

# Check network interfaces
ip link show

# Check CAKE configuration
tc qdisc show
tc -s qdisc show | grep -A5 cake

# Check active connections
head -10 /proc/net/nf_conntrack

# Test binary directly
/usr/bin/cake-autortt --version
/usr/bin/cake-autortt --help
```

## 🔄 Configuration Changes

After modifying `/etc/cake-autortt.yaml`:

```bash
# Restart service to apply changes
/etc/init.d/cake-autortt restart

# Verify changes took effect
logread | tail -20 | grep cake-autortt
```

## 🚀 Advantages of YAML Configuration

- **Simple**: No UCI complexity, just edit a YAML file
- **Portable**: Same config format across all Linux distributions
- **Reliable**: No format conversion between UCI and YAML
- **Familiar**: Works like AdGuard Home and other modern services
- **Auto-Detection**: Installation script automatically configures interfaces
- **Zero-Touch**: Fully automated installation and service setup

## 📞 Support

If you encounter issues:

1. **Check the logs**: `logread | grep cake-autortt`
2. **Test manually**: `/usr/bin/cake-autortt --config /etc/cake-autortt.yaml --debug`
3. **Reinstall**: Run the installation script again (it will backup existing config)
4. **Report issues**: [GitHub Issues](https://github.com/galpt/go-cake-autortt/issues)

---

**For more information, see the main [README.md](README.md)**