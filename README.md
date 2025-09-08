# go-cake-autortt

[![Build Status](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml/badge.svg)](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/galpt/go-cake-autortt)](https://goreportcard.com/report/github.com/galpt/go-cake-autortt)
[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
[![Docker Pulls](https://img.shields.io/docker/pulls/arasseo/go-cake-autortt)](https://hub.docker.com/r/arasseo/go-cake-autortt)

**Languages:** [English](README.md) | [中文](README_zh.md)

**OpenWrt Guide:** [English](README_OpenWrt.md) | [中文](README_OpenWrt_zh.md)

A high-performance Go rewrite of the original shell-based `cake-autortt` tool. This service automatically adjusts CAKE qdisc RTT parameters based on real-time network measurements, providing optimal bufferbloat control for dynamic network conditions.

## 🚀 Features

- **High Performance**: Go implementation with concurrent TCP-based RTT measurements
- **Smart Host Discovery**: Automatically extracts active hosts from conntrack
- **Interface Auto-Detection**: Automatically detects CAKE-enabled interfaces during installation
- **Real-time Web Interface**: Dark-themed web UI for monitoring system status and logs
- **WebSocket Support**: Live updates without manual page refresh
- **Configurable Thresholds**: Flexible min/max host limits and RTT margins
- **Multiple Deployment Options**: Native binary, Docker, or OpenWrt package
- **Real-time Monitoring**: Debug mode with detailed logging
- **Production Ready**: Comprehensive error handling and graceful shutdown
- **Zero-Touch Installation**: Fully automated setup with service management

## 📋 Requirements

- Linux system with CAKE qdisc support
- `tc` (traffic control) utility
- `/proc/net/nf_conntrack` (netfilter connection tracking)
- Root privileges for network interface management

### Tested Platforms

- OpenWrt 24.10.1+ (primary target)
- Ubuntu 20.04+
- Debian 11+
- Alpine Linux
- Any Linux distribution with CAKE support

## 🔧 Installation

### Automated Installation (Recommended)

The installation script provides a **zero-touch experience** - it automatically:
- Downloads and installs the binary
- Creates optimized YAML configuration with auto-detected interfaces
- Sets up system service (OpenWrt init.d or systemd)
- Enables automatic startup on boot
- Starts the service immediately

**For most Linux distributions:**
```bash
# One-command installation with full automation
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | sudo bash
```

**For OpenWrt (run as root, no sudo needed):**
```bash
# One-command installation for OpenWrt
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | ash
```

**What happens during installation:**
1. ✅ Checks system dependencies (`tc`, `wget`/`curl`)
2. ✅ Downloads the correct binary for your architecture
3. ✅ Installs binary to `/usr/bin/cake-autortt`
4. ✅ Creates `/etc/cake-autortt.yaml` with auto-detected CAKE interfaces
5. ✅ Installs appropriate service (init.d for OpenWrt, systemd for others)
6. ✅ Enables service for automatic startup
7. ✅ Starts the service immediately
8. ✅ Displays service management commands and web interface URL

After installation, access the web interface at: `http://your-router-ip:11111`

### Manual Installation

If you prefer manual installation:

1. **Download the latest release:**
   ```bash
   wget https://github.com/galpt/go-cake-autortt/releases/latest/download/cake-autortt-linux-amd64.tar.gz
   tar -xzf cake-autortt-linux-amd64.tar.gz
   sudo install -m 755 cake-autortt-linux-amd64 /usr/bin/cake-autortt
   ```

2. **Create configuration file:**
   ```bash
   sudo tee /etc/cake-autortt.yaml > /dev/null << EOF
   # RTT measurement settings
   rtt_update_interval: 5
   min_hosts: 3
   max_hosts: 100
   rtt_margin_percent: 10
   default_rtt_ms: 100
   tcp_connect_timeout: 3
   max_concurrent_probes: 50
   
   # Network interfaces (configure your CAKE interfaces)
   dl_interface: ""  # e.g., "ifb-wan" for download
   ul_interface: ""  # e.g., "wan" for upload
   
   # Web interface
   web_enabled: true
   web_port: 11111
   
   # Logging
   debug: false
   EOF
   ```

3. **Configure interfaces:**
   ```bash
   # Edit the config to set your CAKE interfaces
   sudo nano /etc/cake-autortt.yaml
   ```

4. **Run manually or set up service:**
   ```bash
   # Test run
   sudo cake-autortt --config /etc/cake-autortt.yaml
   ```

### Docker Installation

```bash
# Pull the image
docker pull arasseo/go-cake-autortt:latest

# Create config file
mkdir -p ./config
wget https://raw.githubusercontent.com/galpt/go-cake-autortt/main/etc/cake-autortt.yaml -O ./config/cake-autortt.yaml

# Run with host networking (required for interface access)
docker run -d --name cake-autortt \
  --network host \
  --privileged \
  -v $(pwd)/config/cake-autortt.yaml:/etc/cake-autortt.yaml:ro \
  -v /proc/net/nf_conntrack:/proc/net/nf_conntrack:ro \
  arasseo/go-cake-autortt:latest
```

## ⚙️ Configuration

The application uses **YAML configuration only** (simplified from previous UCI+YAML approach).

### Configuration File: `/etc/cake-autortt.yaml`

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

### Interface Configuration

**Auto-detection (Default):**
The installation script automatically detects interfaces with CAKE qdisc and configures them.

**Manual Configuration:**
- `dl_interface`: Usually `ifb-wan` or similar IFB interface for download shaping
- `ul_interface`: Usually `wan`, `eth1`, or your WAN interface for upload shaping

**To find your CAKE interfaces:**
```bash
# List all interfaces with CAKE qdisc
tc qdisc show | grep cake
```

## 🎯 Usage

### Command Line Options

```bash
# Run with default config
sudo cake-autortt

# Run with custom config file
sudo cake-autortt --config /path/to/config.yaml

# Override config options
sudo cake-autortt --web-port 8080 --debug

# Show version
cake-autortt --version

# Show help
cake-autortt --help
```

### Service Management

The automated installation sets up the service for you. Here are the management commands:

**OpenWrt:**
```bash
# Service management
/etc/init.d/cake-autortt start
/etc/init.d/cake-autortt stop
/etc/init.d/cake-autortt restart
/etc/init.d/cake-autortt status

# View logs
logread | grep cake-autortt
```

**Systemd (Ubuntu, Debian, etc.):**
```bash
# Service management
sudo systemctl start cake-autortt
sudo systemctl stop cake-autortt
sudo systemctl restart cake-autortt
sudo systemctl status cake-autortt

# View logs
sudo journalctl -u cake-autortt -f
```

### Configuration Changes

After modifying `/etc/cake-autortt.yaml`, restart the service:

```bash
# OpenWrt
/etc/init.d/cake-autortt restart

# Systemd
sudo systemctl restart cake-autortt
```

## 📊 Monitoring

### Web Interface (Recommended)

The easiest way to monitor cake-autortt:
- Navigate to `http://your-router-ip:11111`
- View real-time system status, RTT measurements, and logs
- Monitor CAKE qdisc statistics with live updates
- No need to SSH into the router for basic monitoring

### Command Line Monitoring

Enable debug mode for detailed operation logs:

```bash
# Edit config file
sudo nano /etc/cake-autortt.yaml
# Set: debug: true

# Restart service
sudo systemctl restart cake-autortt  # or /etc/init.d/cake-autortt restart
```

Example debug output:
```
2024/01/15 10:30:15 [INFO] Starting cake-autortt v2.1.0
2024/01/15 10:30:15 [INFO] Auto-detected interfaces: dl=ifb-wan, ul=wan
2024/01/15 10:30:15 [INFO] Extracted 45 hosts from conntrack
2024/01/15 10:30:18 [INFO] Measured RTT: avg=25ms, worst=45ms (from 12 responsive hosts)
2024/01/15 10:30:18 [INFO] Adjusted CAKE RTT to 50ms (45ms + 10% margin)
```

## 🔍 Troubleshooting

### Common Issues

**1. Service not starting after installation:**
```bash
# Check service status
systemctl status cake-autortt  # or /etc/init.d/cake-autortt status

# Check configuration
sudo cake-autortt --config /etc/cake-autortt.yaml --debug
```

**2. No CAKE interfaces detected:**
```bash
# Check if CAKE qdisc is configured
sudo tc qdisc show | grep cake

# Configure CAKE on interface (example)
sudo tc qdisc add dev wan root cake bandwidth 100mbit
```

**3. Web interface not accessible:**
```bash
# Check if service is running
sudo systemctl status cake-autortt

# Check firewall (if applicable)
sudo ufw allow 11111  # Ubuntu/Debian
```

**4. Permission denied:**
```bash
# Ensure binary has correct permissions
sudo chmod 755 /usr/bin/cake-autortt

# Check config file permissions
sudo chmod 644 /etc/cake-autortt.yaml
```

### Debug Commands

```bash
# Test configuration
sudo cake-autortt --config /etc/cake-autortt.yaml --debug

# Check current CAKE settings
sudo tc qdisc show | grep cake

# Check active connections
sudo cat /proc/net/nf_conntrack | head -10

# Verify interfaces
ip link show
```

### Reinstallation

If you encounter issues, you can safely reinstall:

```bash
# The script will backup existing config and reinstall
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | sudo bash
```

## 🏗️ Building from Source

### Prerequisites

- Go 1.21 or later
- Git

### Build Steps

```bash
# Clone repository
git clone https://github.com/galpt/go-cake-autortt.git
cd go-cake-autortt

# Download dependencies
go mod download

# Build for current platform
go build -o cake-autortt .

# Build for specific platform
GOOS=linux GOARCH=mips go build -o cake-autortt-mips .

# Build all platforms (requires make)
make build-all
```

### Cross-compilation Targets

- `linux/amd64` - x86_64 Linux
- `linux/arm64` - ARM64 Linux
- `linux/armv7` - ARMv7 Linux
- `linux/armv6` - ARMv6 Linux
- `linux/mips` - MIPS Linux (OpenWrt)
- `linux/mipsle` - MIPS Little Endian
- `linux/mips64` - MIPS64
- `linux/mips64le` - MIPS64 Little Endian

## 🤝 Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup

```bash
# Clone and setup
git clone https://github.com/galpt/go-cake-autortt.git
cd go-cake-autortt
go mod download

# Run tests
go test ./...

# Run linter
golangci-lint run

# Format code
go fmt ./...
```

## 📄 License

This project is licensed under the GNU General Public License v2.0 - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- OpenWrt community for CAKE qdisc development
- Go community for excellent networking libraries

## 📞 Support

- **Issues**: [GitHub Issues](https://github.com/galpt/go-cake-autortt/issues)
- **Documentation**: [OpenWrt Installation Guide](README_OpenWrt.md)

## 🔗 Related Projects

- [cake-autortt (Shell Script)](https://github.com/galpt/cake-autortt) - Original shell script version
- [cake-autorate](https://github.com/lynxthecat/cake-autorate) - Automatic CAKE bandwidth adjustment
- [OpenWrt](https://openwrt.org/) - Linux distribution for embedded devices
- [CAKE qdisc](https://www.bufferbloat.net/projects/codel/wiki/Cake/) - Comprehensive queue management

---

**Star ⭐ this repository if you find it useful!**