# go-cake-autortt

[![Build Status](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml/badge.svg)](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/galpt/go-cake-autortt)](https://goreportcard.com/report/github.com/galpt/go-cake-autortt)
[![License: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
[![Docker Pulls](https://img.shields.io/docker/pulls/arasseo/go-cake-autortt)](https://hub.docker.com/r/arasseo/go-cake-autortt)

**Languages:** [English](README.md) | [中文](README_zh.md)

A high-performance Go rewrite of the original shell-based `cake-autortt` tool. This service automatically adjusts CAKE qdisc RTT parameters based on real-time network measurements, providing optimal bufferbloat control for dynamic network conditions.

## 🚀 Features

- **High Performance**: Go implementation with concurrent TCP-based RTT measurements
- **Smart Host Discovery**: Automatically extracts active hosts from conntrack
- **Interface Auto-Detection**: Automatically detects CAKE-enabled interfaces
- **Real-time Web Interface**: Dark-themed web UI for monitoring system status and logs
- **WebSocket Support**: Live updates without manual page refresh
- **Configurable Thresholds**: Flexible min/max host limits and RTT margins
- **Multiple Deployment Options**: Native binary, Docker, or OpenWrt package
- **Real-time Monitoring**: Debug mode with detailed logging
- **Production Ready**: Comprehensive error handling and graceful shutdown

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

### Quick Install (Recommended)

**For most Linux distributions:**
```bash
# Download and run the installation script
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | sudo bash
```

**For OpenWrt (run as root, no sudo needed):**
```bash
# Download and run the installation script directly as root
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | ash
```

After installation, access the web interface at: `http://your-router-ip/cake-autortt`

### Manual Installation

1. **Download the latest release:**
   ```bash
   wget https://github.com/galpt/go-cake-autortt/releases/latest/download/cake-autortt-linux-amd64.tar.gz
   tar -xzf cake-autortt-linux-amd64.tar.gz
   sudo install -m 755 cake-autortt-linux-amd64 /usr/bin/cake-autortt
   ```

2. **Create configuration:**
   ```bash
   sudo mkdir -p /etc/config
   sudo wget https://raw.githubusercontent.com/galpt/go-cake-autortt/main/etc/config/cake-autortt -O /etc/config/cake-autortt
   ```

3. **Edit configuration:**
   ```bash
   sudo nano /etc/config/cake-autortt
   ```

### Docker Installation

```bash
# Pull the image
docker pull arasseo/go-cake-autortt:latest

# Run with host networking (required for interface access)
docker run -d --name cake-autortt \
  --network host \
  --privileged \
  -v /proc/net/nf_conntrack:/proc/net/nf_conntrack:ro \
  arasseo/go-cake-autortt:latest
```

### OpenWrt Package Installation

```bash
# Add the repository (if available)
opkg update
opkg install go-cake-autortt

# Or install manually
wget https://github.com/galpt/go-cake-autortt/releases/latest/download/go-cake-autortt_2.0.0_mips.ipk
opkg install go-cake-autortt_2.0.0_mips.ipk
```

## ⚙️ Configuration

Edit `/etc/config/cake-autortt`:

```bash
config cake-autortt 'global'
    option rtt_update_interval '5'        # RTT measurement interval (seconds)
    option min_hosts '3'                  # Minimum hosts for RTT calculation
    option max_hosts '100'                # Maximum hosts to probe
    option rtt_margin_percent '10'        # Safety margin percentage
    option default_rtt_ms '100'           # Default RTT when no measurements
    option dl_interface 'ifb-wan'         # Download interface (auto-detect if empty)
    option ul_interface 'wan'             # Upload interface (auto-detect if empty)
    option web_enabled '1'                # Enable web interface
    option web_port '80'                  # Web interface port
    option debug '0'                      # Enable debug logging
    option tcp_connect_timeout '3'        # TCP connection timeout (seconds)
    option max_concurrent_probes '50'     # Maximum concurrent RTT probes
```

### Interface Configuration

**Auto-detection (Recommended):**
Leave `dl_interface` and `ul_interface` empty for automatic detection.

**Manual Configuration:**
- `dl_interface`: Usually `ifb-wan` or similar IFB interface for download shaping
- `ul_interface`: Usually `wan`, `eth1`, or your WAN interface for upload shaping

## 🎯 Usage

### Command Line Options

```bash
# Run with default config (includes web server)
sudo cake-autortt

# Run with custom config file
sudo cake-autortt --config /path/to/config

# Run with custom web port
sudo cake-autortt --web-port 8080

# Disable web interface
sudo cake-autortt --web-enabled=false

# Enable debug mode
sudo cake-autortt --debug

# Show version
cake-autortt --version

# Show help
cake-autortt --help
```

### Service Management

**OpenWrt:**
```bash
# Start service
/etc/init.d/cake-autortt start

# Stop service
/etc/init.d/cake-autortt stop

# Restart service
/etc/init.d/cake-autortt restart

# Enable auto-start
/etc/init.d/cake-autortt enable

# Check status
/etc/init.d/cake-autortt status
```

**Systemd:**
```bash
# Start service
sudo systemctl start cake-autortt

# Stop service
sudo systemctl stop cake-autortt

# Restart service
sudo systemctl restart cake-autortt

# Enable auto-start
sudo systemctl enable cake-autortt

# Check status
sudo systemctl status cake-autortt
```

## 📊 Monitoring

### Web Interface (Recommended)

The easiest way to monitor cake-autortt is through the web interface:
- Navigate to `http://your-router-ip/cake-autortt`
- View real-time system status, RTT measurements, and logs
- Monitor CAKE qdisc statistics with live updates
- No need to SSH into the router for basic monitoring

### Command Line Monitoring

Enable debug mode to see detailed operation logs:

```bash
# Temporary debug mode
sudo cake-autortt --debug

# Or edit config file
sudo nano /etc/config/cake-autortt
# Set: option debug '1'
```

Example debug output:
```
2024/01/15 10:30:15 [INFO] Starting cake-autortt v2.0.0
2024/01/15 10:30:15 [INFO] Auto-detected interfaces: dl=ifb-wan, ul=wan
2024/01/15 10:30:15 [INFO] Extracted 45 hosts from conntrack
2024/01/15 10:30:18 [INFO] Measured RTT: avg=25ms, worst=45ms (from 12 responsive hosts)
2024/01/15 10:30:18 [INFO] Adjusted CAKE RTT to 50ms (45ms + 10% margin)
```

## 🔍 Troubleshooting

### Common Issues

**1. No CAKE interfaces found:**
```bash
# Check if CAKE qdisc is configured
sudo tc qdisc show

# Configure CAKE on interface (example)
sudo tc qdisc add dev wan root cake bandwidth 100mbit
```

**2. Permission denied:**
```bash
# Ensure running as root
sudo cake-autortt

# Check file permissions
ls -la /usr/bin/cake-autortt
```

**3. No conntrack file:**
```bash
# Check if conntrack is available
ls -la /proc/net/nf_conntrack

# Enable conntrack if missing
sudo modprobe nf_conntrack
```

**4. TCP connection timeouts:**
```bash
# Increase timeout in config
option tcp_connect_timeout '5'

# Reduce concurrent probes
option max_concurrent_probes '25'
```

### Debug Commands

```bash
# Check current CAKE settings
sudo tc qdisc show | grep cake

# Monitor RTT changes
sudo cake-autortt --debug | grep "Adjusted CAKE RTT"

# Check active connections
sudo cat /proc/net/nf_conntrack | head -10

# Test TCP connectivity
sudo cake-autortt --debug | grep "TCP probe"
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
- `freebsd/amd64` - FreeBSD x86_64

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

## 🔗 Related Projects

- [cake-autortt (Shell Script)](https://github.com/galpt/cake-autortt) - Original shell script version
- [cake-autorate](https://github.com/lynxthecat/cake-autorate) - Automatic CAKE bandwidth adjustment
- [OpenWrt](https://openwrt.org/) - Linux distribution for embedded devices
- [CAKE qdisc](https://www.bufferbloat.net/projects/codel/wiki/Cake/) - Comprehensive queue management

---

**Star ⭐ this repository if you find it useful!**