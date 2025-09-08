# go-cake-autortt

[![构建状态](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml/badge.svg)](https://github.com/galpt/go-cake-autortt/actions/workflows/build.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/galpt/go-cake-autortt)](https://goreportcard.com/report/github.com/galpt/go-cake-autortt)
[![许可证: GPL v2](https://img.shields.io/badge/License-GPL%20v2-blue.svg)](https://www.gnu.org/licenses/old-licenses/gpl-2.0.en.html)
[![Docker Pulls](https://img.shields.io/docker/pulls/arasseo/go-cake-autortt)](https://hub.docker.com/r/arasseo/go-cake-autortt)

**语言:** [English](README.md) | [中文](README_zh.md)

**OpenWrt 指南:** [English](README_OpenWrt.md) | [中文](README_OpenWrt_zh.md)

原始基于shell的`cake-autortt`工具的高性能Go重写版本。该服务基于实时网络测量自动调整CAKE qdisc RTT参数，为动态网络条件提供最佳的缓冲区膨胀控制。

## 🚀 特性

- **高性能**: Go实现，支持并发TCP RTT测量
- **智能主机发现**: 自动从conntrack提取活跃主机
- **接口自动检测**: 自动检测启用CAKE的接口
- **可配置阈值**: 灵活的最小/最大主机限制和RTT边距
- **实时Web界面**: 深色主题的Web UI用于监控系统状态和日志
- **WebSocket支持**: 无需手动刷新页面的实时更新
- **多种部署选项**: 原生二进制、Docker或OpenWrt包
- **实时监控**: 调试模式提供详细日志
- **生产就绪**: 全面的错误处理和优雅关闭

## 📋 系统要求

- 支持CAKE qdisc的Linux系统
- `tc`（流量控制）工具
- `/proc/net/nf_conntrack`（netfilter连接跟踪）
- 网络接口管理的root权限

### 测试平台

- OpenWrt 24.10.1+（主要目标）
- Ubuntu 20.04+
- Debian 11+
- Alpine Linux
- 任何支持CAKE的Linux发行版

## 🔧 安装

### 快速安装（推荐）

**对于大多数Linux发行版：**
```bash
# 下载并运行安装脚本
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | sudo bash
```

**对于OpenWrt（以root身份运行，无需sudo）：**
```bash
# 直接以root身份下载并运行安装脚本
curl -fsSL https://raw.githubusercontent.com/galpt/go-cake-autortt/main/install.sh | ash
```

脚本将会：
- 检测您的系统（OpenWrt/Linux）
- 下载适当的二进制文件
- 安装配置文件
- 设置并启动服务
- 配置开机自启动
- 在11111端口启用Web界面

安装完成后，通过以下地址访问Web界面：`http://路由器IP:11111/cake-autortt`

### 手动安装

1. **下载最新版本:**
   ```bash
   wget https://github.com/galpt/go-cake-autortt/releases/latest/download/cake-autortt-linux-amd64.tar.gz
   tar -xzf cake-autortt-linux-amd64.tar.gz
   sudo install -m 755 cake-autortt-linux-amd64 /usr/bin/cake-autortt
   ```

2. **创建配置文件:**
   ```bash
   # 创建UCI格式配置（用于OpenWrt兼容性）
   sudo mkdir -p /etc/config
   sudo wget https://raw.githubusercontent.com/galpt/go-cake-autortt/main/etc/config/cake-autortt -O /etc/config/cake-autortt
   
   # 创建YAML格式配置（用于直接二进制使用）
   sudo wget https://raw.githubusercontent.com/galpt/go-cake-autortt/main/cake-autortt.yaml.template -O /etc/cake-autortt.yaml
   ```

3. **编辑配置:**
   ```bash
   # 编辑UCI配置（如果使用OpenWrt服务）
   sudo nano /etc/config/cake-autortt
   
   # 或编辑YAML配置（如果直接运行二进制文件）
   sudo nano /etc/cake-autortt.yaml
   ```

### Docker安装

```bash
# 拉取镜像
docker pull arasseo/go-cake-autortt:latest

# 使用主机网络运行（访问接口所需）
docker run -d --name cake-autortt \
  --network host \
  --privileged \
  -v /proc/net/nf_conntrack:/proc/net/nf_conntrack:ro \
  arasseo/go-cake-autortt:latest
```

### OpenWrt包安装

```bash
# 添加仓库（如果可用）
opkg update
opkg install go-cake-autortt

# 或手动安装
wget https://github.com/galpt/go-cake-autortt/releases/latest/download/go-cake-autortt_2.0.0_mips.ipk
opkg install go-cake-autortt_2.0.0_mips.ipk
```

## ⚙️ 配置

应用程序支持两种配置格式：

### OpenWrt UCI 格式（用于服务使用）

编辑 `/etc/config/cake-autortt`:

```bash
config cake-autortt 'global'
    option rtt_update_interval '5'        # RTT测量间隔（秒）
    option min_hosts '3'                  # RTT计算的最小主机数
    option max_hosts '100'                # 探测的最大主机数
    option rtt_margin_percent '10'        # 安全边距百分比
    option default_rtt_ms '100'           # 无测量时的默认RTT
    option dl_interface 'ifb-wan'         # 下载接口（空则自动检测）
    option ul_interface 'wan'             # 上传接口（空则自动检测）
    option web_enabled '1'                # 启用Web界面
    option web_port '11111'               # Web界面端口
    option debug '0'                      # 启用调试日志
    option tcp_connect_timeout '3'        # TCP连接超时（秒）
    option max_concurrent_probes '50'     # 最大并发RTT探测数
```

### YAML 格式（用于直接二进制使用）

编辑 `/etc/cake-autortt.yaml`:

```yaml
rtt_update_interval: 5        # RTT测量间隔（秒）
min_hosts: 3                  # RTT计算的最小主机数
max_hosts: 100                # 探测的最大主机数
rtt_margin_percent: 10        # 安全边距百分比
default_rtt_ms: 100           # 无测量时的默认RTT
dl_interface: ""              # 下载接口（空则自动检测）
ul_interface: ""              # 上传接口（空则自动检测）
web_enabled: true             # 启用Web界面
web_port: 11111               # Web界面端口
debug: false                  # 启用调试日志
tcp_connect_timeout: 3        # TCP连接超时（秒）
max_concurrent_probes: 50     # 最大并发RTT探测数
```

**注意：** 安装脚本会自动创建两个配置文件。OpenWrt服务使用UCI格式配合命令行参数，而直接二进制执行使用YAML格式。

### 接口配置

**自动检测（推荐）:**
将`dl_interface`和`ul_interface`留空以进行自动检测。

**手动配置:**
- `dl_interface`: 通常是`ifb-wan`或类似的IFB接口用于下载整形
- `ul_interface`: 通常是`wan`、`eth1`或您的WAN接口用于上传整形

## 🎯 使用方法

### 命令行选项

```bash
# 使用默认配置运行
sudo cake-autortt

# 使用自定义配置文件运行
sudo cake-autortt --config /path/to/config

# 使用自定义Web端口运行
sudo cake-autortt --web-port 11111

# 禁用Web界面
sudo cake-autortt --web-enabled=false

# 启用调试模式
sudo cake-autortt --debug

# 显示版本
cake-autortt --version

# 显示帮助
cake-autortt --help
```

### 服务管理

**OpenWrt:**
```bash
# 启动服务
/etc/init.d/cake-autortt start

# 停止服务
/etc/init.d/cake-autortt stop

# 重启服务
/etc/init.d/cake-autortt restart

# 启用自动启动
/etc/init.d/cake-autortt enable

# 检查状态
/etc/init.d/cake-autortt status
```

**Systemd:**
```bash
# 启动服务
sudo systemctl start cake-autortt

# 停止服务
sudo systemctl stop cake-autortt

# 重启服务
sudo systemctl restart cake-autortt

# 启用自动启动
sudo systemctl enable cake-autortt

# 检查状态
sudo systemctl status cake-autortt
```

### Web界面

服务运行后，可通过Web界面访问：
- **网址**: `http://路由器IP:11111/cake-autortt`（默认11111端口）
- **功能**:
  - 实时系统状态监控
  - 实时CAKE qdisc统计信息（`tc -s qdisc`）
  - 最近的应用程序日志，自动刷新
  - 深色主题，更好的可视性
  - 基于WebSocket的实时更新

## 📊 监控

### Web界面（推荐）

监控cake-autortt最简单的方法是通过Web界面：
- 导航到 `http://路由器IP:11111/cake-autortt`
- 查看实时系统状态、RTT测量和日志
- 监控CAKE qdisc统计信息的实时更新
- 无需SSH到路由器进行基本监控

![Web界面截图](images/web-ui-cake-autortt.png)

### 命令行监控

启用调试模式查看详细操作日志:

```bash
# 临时调试模式
sudo cake-autortt --debug

# 或编辑配置文件
sudo nano /etc/config/cake-autortt
# 设置: option debug '1'
```

调试输出示例:
```
2024/01/15 10:30:15 [INFO] 启动 cake-autortt v2.0.0
2024/01/15 10:30:15 [INFO] 自动检测接口: dl=ifb-wan, ul=wan
2024/01/15 10:30:15 [INFO] 从conntrack提取45个主机
2024/01/15 10:30:18 [INFO] 测量RTT: 平均=25ms, 最差=45ms（来自12个响应主机）
2024/01/15 10:30:18 [INFO] 调整CAKE RTT为50ms（45ms + 10%边距）
```

## 🔍 故障排除

### 常见问题

**1. 未找到CAKE接口:**
```bash
# 检查是否配置了CAKE qdisc
sudo tc qdisc show

# 在接口上配置CAKE（示例）
sudo tc qdisc add dev wan root cake bandwidth 100mbit
```

**2. 权限被拒绝:**
```bash
# 确保以root身份运行
sudo cake-autortt

# 检查文件权限
ls -la /usr/bin/cake-autortt
```

**3. 无conntrack文件:**
```bash
# 检查conntrack是否可用
ls -la /proc/net/nf_conntrack

# 如果缺失则启用conntrack
sudo modprobe nf_conntrack
```

**4. TCP连接超时:**
```bash
# 在配置中增加超时时间
option tcp_connect_timeout '5'

# 减少并发探测数
option max_concurrent_probes '25'
```

### 调试命令

```bash
# 检查当前CAKE设置
sudo tc qdisc show | grep cake

# 监控RTT变化
sudo cake-autortt --debug | grep "Adjusted CAKE RTT"

# 检查活跃连接
sudo cat /proc/net/nf_conntrack | head -10

# 测试TCP连接性
sudo cake-autortt --debug | grep "TCP probe"
```

## 🏗️ 从源码构建

### 前置要求

- Go 1.21或更高版本
- Git

### 构建步骤

```bash
# 克隆仓库
git clone https://github.com/galpt/go-cake-autortt.git
cd go-cake-autortt

# 下载依赖
go mod download

# 为当前平台构建
go build -o cake-autortt .

# 为特定平台构建
GOOS=linux GOARCH=mips go build -o cake-autortt-mips .

# 构建所有平台（需要make）
make build-all
```

### 交叉编译目标

- `linux/amd64` - x86_64 Linux
- `linux/arm64` - ARM64 Linux
- `linux/armv7` - ARMv7 Linux
- `linux/armv6` - ARMv6 Linux
- `linux/mips` - MIPS Linux（OpenWrt）
- `linux/mipsle` - MIPS小端序
- `linux/mips64` - MIPS64
- `linux/mips64le` - MIPS64小端序
- `freebsd/amd64` - FreeBSD x86_64

## 🤝 贡献

欢迎贡献！请阅读我们的[贡献指南](CONTRIBUTING.md)了解详情。

### 开发环境设置

```bash
# 克隆并设置
git clone https://github.com/galpt/go-cake-autortt.git
cd go-cake-autortt
go mod download

# 运行测试
go test ./...

# 运行代码检查
golangci-lint run

# 格式化代码
go fmt ./...
```

## 📄 许可证

本项目采用GNU通用公共许可证v2.0许可 - 详见[LICENSE](LICENSE)文件。

## 🙏 致谢

- OpenWrt社区的CAKE qdisc开发
- Go社区提供的优秀网络库

## 📞 支持

- **问题反馈**: [GitHub Issues](https://github.com/galpt/go-cake-autortt/issues)

## 🔗 相关项目

- [cake-autortt (Shell脚本版)](https://github.com/galpt/cake-autortt) - 原始shell脚本版本
- [cake-autorate](https://github.com/lynxthecat/cake-autorate) - 自动CAKE带宽调整
- [OpenWrt](https://openwrt.org/) - 嵌入式设备Linux发行版
- [CAKE qdisc](https://www.bufferbloat.net/projects/codel/wiki/Cake/) - 综合队列管理

---

**如果您觉得有用，请为此仓库点星 ⭐！**