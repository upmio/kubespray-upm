# Kubespray Libvirt 环境设置指南

## 概述

本文档详细介绍如何使用 `libvirt_kubespray_setup.sh` 脚本在 libvirt 虚拟化环境中快速部署 Kubespray Kubernetes 集群。该脚本专为 Red Hat 系列 Linux 系统（RHEL 9、Rocky Linux 9、CentOS 9、AlmaLinux 9）设计，提供完整的自动化环境配置、交互式部署体验以及企业级容器镜像仓库配置支持。

### 脚本特性

- **版本**: v1.0
- **模块化安装**: 支持选择性安装不同组件（K8s、LVM LocalPV、Prometheus、CloudNativePG、UPM Engine、UPM Platform）
- **交互式安装**: 提供详细的安装预览和确认机制
- **智能虚拟机管理**: 自动检测现有虚拟机，提供灵活的处理选项（保留、更新、重建或取消）
- **智能网络配置**: 自动检测和配置网络模式（NAT/桥接）
- **统一输入验证**: 改进的用户输入处理和验证机制
- **完整日志记录**: 详细的操作日志和错误处理机制
- **一键部署**: 环境设置完成后可直接部署 Kubernetes 集群
- **多组件支持**: 支持安装 Kubernetes 生态系统的多种组件
- **企业级支持**: 支持容器镜像仓库转发和私有仓库认证配置
- **虚拟机生命周期管理**: 提供完整的虚拟机创建、更新、销毁和状态管理功能

### ⚡ 一键命令

如果您希望快速体验，可以使用以下一键命令：

**下载脚本并安装 Kubernetes 集群（NAT 模式）**

```bash
curl -sSL https://raw.githubusercontent.com/upmio/kubespray-upm/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh -o ./libvirt_kubespray_setup.sh && chmod +x ./libvirt_kubespray_setup.sh && bash ./libvirt_kubespray_setup.sh --k8s -y
```

## 系统要求

### 硬件要求

- **CPU**: 最少 12 核心（推荐 24+ 核心）
- **内存**: 最少 32GB（推荐 64GB+）
- **磁盘空间**: 最少 200GB 可用空间
- **架构**: x86_64

### 软件要求

- **操作系统**: Rocky Linux 9、CentOS 9、AlmaLinux 9、Red Hat Enterprise Linux (RHEL) 9
- **网络**: 稳定的互联网连接（Proxy 配置可选）
- **权限**: sudo 访问权限

### 基础系统要求

#### 网络连接要求

- **互联网连接**: 稳定的互联网连接，用于下载软件包和容器镜像
- **DNS 解析**: 系统能够正常解析域名（如 github.com、registry.k8s.io）
- **防火墙配置**: 允许出站 HTTP/HTTPS 连接（脚本会自动禁用 firewalld）
- **代理支持**: 如在企业环境中，支持 HTTP/HTTPS 代理配置

#### 软件仓库要求

- **DNF/YUM 仓库**: 系统软件仓库必须可用且配置正确
- **EPEL 仓库**: 脚本会自动安装和启用 EPEL 仓库
- **PowerTools/CRB 仓库**: 脚本会自动启用 PowerTools（CentOS/Rocky/AlmaLinux）或 CodeReady Builder（RHEL）仓库
- **仓库缓存**: 建议运行前执行 `sudo dnf makecache` 更新仓库缓存

#### 虚拟化支持要求

- **硬件虚拟化**: CPU 必须支持硬件虚拟化（Intel VT-x 或 AMD-V）
- **BIOS/UEFI 设置**: 在 BIOS/UEFI 中启用虚拟化功能
- **嵌套虚拟化**: 如在虚拟机中运行，需要启用嵌套虚拟化
- **KVM 模块**: 系统内核必须支持 KVM 模块

#### 系统服务要求

- **NetworkManager**: 网络管理服务必须运行（用于桥接网络配置）
- **systemd**: 系统必须使用 systemd 作为初始化系统
- **时间同步**: 系统时间必须准确（建议启用 chronyd 或 ntpd）

#### 用户权限要求

- **sudo 权限**: 当前用户必须具有 sudo 权限
- **用户组**: 脚本会自动将用户添加到 libvirt 组
- **文件权限**: 用户主目录必须可写（用于存储配置文件和密钥）

#### 磁盘空间分布

- **根分区 (/)**: 至少 50GB 可用空间（用于系统软件和工具）
- **用户主目录**: 至少 20GB 可用空间（用于 kubespray 项目和配置）
- **临时目录 (/tmp)**: 至少 10GB 可用空间（用于下载和解压）
- **虚拟机存储**: 至少 120GB 可用空间（默认位置：/var/lib/libvirt/images）

#### 预检查命令

在运行脚本前，可以使用以下命令检查系统是否满足要求：

```bash
# 检查虚拟化支持
egrep -c '(vmx|svm)' /proc/cpuinfo
# 输出应该大于 0

# 检查 KVM 模块
lsmod | grep kvm
# 应该显示 kvm 相关模块

# 检查网络连接
curl -I https://github.com
# 应该返回 HTTP 200 状态

# 检查 DNS 解析
nslookup github.com
# 应该返回 IP 地址

# 检查磁盘空间
df -h
# 检查各分区可用空间

# 检查仓库状态
sudo dnf repolist
# 应该显示可用的软件仓库

# 检查 sudo 权限
sudo whoami
# 应该返回 root
```

#### RHEL 系统特殊要求

对于 Red Hat Enterprise Linux (RHEL) 系统，脚本会自动进行以下检查和配置：

**订阅管理要求**:

- 系统必须已注册到 Red Hat 订阅管理服务
- 需要有效的 RHEL 订阅许可证
- `subscription-manager` 工具必须可用且配置正确

**必需的软件仓库**:

脚本会自动检查并启用以下 RHEL 仓库：

- `rhel-{version}-for-{arch}-baseos-rpms` - 基础操作系统软件包
- `rhel-{version}-for-{arch}-appstream-rpms` - 应用程序流软件包
- `codeready-builder-for-rhel-{version}-{arch}-rpms` - 开发工具和库

**注意事项**:

- 如果系统未正确注册或订阅已过期，脚本会报错并停止执行
- 确保在运行脚本前已完成 RHEL 系统的订阅注册
- 脚本会跳过 CRB (CodeReady Builder) 仓库的通用配置，因为 RHEL 使用专门的 `codeready-builder-for-rhel` 仓库

## 快速开始

### 🚀 三步快速使用 Kubernetes 集群

#### 第一步：下载脚本

```bash
# 下载安装脚本
curl -sSL https://raw.githubusercontent.com/upmio/kubespray-upm/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh -o "libvirt_kubespray_setup.sh" && chmod +x ./libvirt_kubespray_setup.sh
```

#### 第二步：运行脚本

```bash
# NAT 模式自动配置网络，一键安装 Kubernetes 集群
bash ./libvirt_kubespray_setup.sh --k8s -y
```

**安装过程说明**：

- 脚本会自动检测系统环境并安装必要的依赖组件
- **网络模式选择**：脚本会智能检测并提示选择网络模式
  - 🌉 **桥接模式**：VM 直接连接物理网络，适合生产环境（需要配置网络接口）
  - 🔒 **NAT 模式**：VM 通过 NAT 访问网络，适合开发测试（自动配置）
- 整个安装过程约 15-25 分钟，需要稳定的网络连接
- 支持企业环境的代理配置和私有镜像仓库设置

> 💡 **网络配置详情**：如需了解网络模式的详细配置，请参考 [网络配置选项](#网络配置选项) 章节
> 🏢 **企业环境配置**：如需配置容器镜像仓库转发，请参考 [容器镜像仓库配置](#容器镜像仓库配置) 章节

#### 第三步：访问集群

```bash
# 脚本完成后，使用 kubectl 访问集群
kubectl get nodes
kubectl get pods --all-namespaces
```

## 脚本参数说明

### 基础选项

| 参数 | 长选项 | 描述 |
|------|--------|------|
| `-h` | `--help` | 显示帮助信息 |
| `-v` | `--version` | 显示详细版本信息 |
| | `--version-short` | 显示简要版本信息 |
| | `--version-changelog` | 显示版本更新日志 |
| `-y` | | 自动确认所有是/否提示（网络桥接配置除外） |
| `-n <network_type>` | | 设置网络类型（nat\|bridge，默认：nat）<br/>仅在使用 `--k8s` 或完整安装模式时有效<br/>设置为 'bridge' 时需要交互式配置 |

### 安装选项（必须指定其中一个）

| 选项 | 描述 | 安装时间 | 要求 |
|------|------|----------|------|
| `--k8s` | 仅安装 Kubernetes 集群环境 | ~15 分钟 | 基础系统要求 |
| `--lvmlocalpv` | 仅安装 OpenEBS LVM LocalPV 存储解决方案 | ~3 分钟 | 已有 K8s 集群 + Helm 3.x |
| `--cnpg` | 仅安装 CloudNative-PG PostgreSQL 数据库 | ~5 分钟 | 已有 K8s 集群 + Helm 3.x |
| `--upm-engine` | 仅安装 UPM Engine 管理组件 | ~5 分钟 | 已有 K8s 集群 + Helm 3.x |
| `--upm-platform` | 仅安装 UPM Platform 平台界面 | ~3 分钟 | 已有 K8s 集群 + Helm 3.x |
| `--prometheus` | 仅安装 Prometheus 监控和告警系统 | ~8 分钟 | 已有 K8s 集群 + Helm 3.x |
| `--all` | 安装所有组件（K8s + 存储 + 数据库 + 监控 + UPM） | ~25 分钟 | 基础系统要求 |

**重要提示：** 必须指定且仅能指定一个安装选项。

### 虚拟机管理选项（可选）

| 选项 | 描述 | 要求 |
|------|------|------|
| `--status` | 查看虚拟机状态和基本信息 | 已部署的虚拟机 |
| `--ssh <node_name>` | SSH 连接到指定节点（如 k8s-1, k8s-2） | 已部署且运行的虚拟机 |
| `--destroy` | 销毁所有 kubespray 虚拟机 | 已部署的虚拟机 |
| `--halt` | 停止所有 kubespray 虚拟机 | 已部署且运行的虚拟机 |
| `--up` | 启动所有 kubespray 虚拟机 | 已部署但停止的虚拟机 |

**注意事项：** 虚拟机管理选项不能与安装选项同时使用。

### 安装选项详细要求

#### Kubernetes 集群 (`--k8s`)
- **系统要求**: RHEL/Rocky/AlmaLinux 9 (x86_64)
- **硬件要求**: 12+ 核 CPU，32GB+ 内存，200GB+ 存储
- **网络要求**: 互联网连接，sudo 权限
- **安装内容**: 完整的 Kubernetes 集群环境

#### 其他组件 (`--lvmlocalpv`, `--cnpg`, `--upm-engine`, `--upm-platform`, `--prometheus`)
- **前置要求**: 已存在的 Kubernetes 集群，kubectl 访问权限
- **依赖组件**: Helm 3.x（如不存在会自动安装）
- **权限要求**: 集群管理员权限（用于 CRD 安装）
- **网络要求**: 互联网连接下载 Helm charts
- **特殊要求**: 
  - LVM LocalPV: 工作节点需要 LVM 卷组和正确的节点标签
  - Prometheus: 需要持久化存储用于监控数据
  - UPM Engine: 需要正确的节点标签用于调度

## 网络配置选项

### 1. 桥接网络模式（推荐生产环境）

**特点**:

- VM 直接连接到物理网络
- VM 获得与主机同网段的 IP 地址
- 外部网络可直接访问 VM

**注意事项**:

- ⚠️ **重要警告**: 配置桥接网络会移除指定网络接口的当前 IP 地址
- 可能导致 SSH 连接中断，建议在本地控制台执行
- 脚本会要求用户确认并输入当前 IP 地址以确保理解风险
- 网桥名称固定为 `br0`（用户选择的物理网络接口将作为桥接接口设备连接到此网桥）

**交互式配置流程**:

当选择桥接网络模式时，脚本会进行以下交互式配置：

1. **选择网络接口**:

   ```bash
   🌐 Available Network Interfaces:
   ┌─────────────────────────────────────────────────────────────────────────────────┐
   │ Interface │ IP Address      │ Status │ MAC Address       │ Speed    │
   ├───────────┼─────────────────┼────────┼───────────────────┼──────────┤
   │ ens33     │ 192.168.1.100   │ UP     │ 00:0c:29:xx:xx:xx │ 1000 Mb/s│
   │ ens34     │ 10.0.0.50       │ UP     │ 00:0c:29:yy:yy:yy │ 1000 Mb/s│
   └───────────┴─────────────────┴────────┴───────────────────┴──────────┘
   
   Please select the network interface for bridge configuration:
   Enter interface name (e.g., ens33): [用户选择网络接口]
   ```

2. **安全确认**（两次确认）:

   ```bash
   ⚠️ WARNING: Configuring bridge will remove this IP address and may disconnect existing connections!
   Continue with bridge configuration? (y/N)
   
   🔐 Second Confirmation Required
   🔒 Security Check: To proceed with bridge configuration
   Please enter the current IP address of 'ens33'
   ⚠️ This confirms you understand that IP '192.168.1.100' will be permanently removed
   Enter current IP address to confirm deletion: [用户需输入当前IP地址]
   ```

3. **网络配置输入**:

   ```bash
   🌐 Bridge Network Configuration
Please provide the network configuration for bridge network:
   
   Enter starting IP with CIDR for VM allocation (e.g., 192.168.1.10/24): [用户输入带CIDR的起始IP]
   Enter gateway IP (e.g., 192.168.1.1): [用户输入网关IP]
   Enter DNS server IP (e.g., 8.8.8.8 or 192.168.1.1): [用户输入DNS服务器]
   ```

4. **配置确认和VM预览**:

   ```bash
   ✅ Network configuration summary:
      ├─ Starting IP: 192.168.1.10+
      ├─ Netmask: 255.255.255.0
      ├─ Gateway: 192.168.1.1
      ├─ DNS Server: 8.8.8.8
      └─ Bridge Interface: ens33
   
   🖥️ Virtual Machine IP Address Preview
   The following VMs will be created with these IP addresses:
      ├─ VM 1: k8s-1 → 192.168.1.11 (Master Node)
      ├─ VM 2: k8s-2 → 192.168.1.12 (Worker Node)
      ├─ VM 3: k8s-3 → 192.168.1.13 (Worker Node)
      └─ Total: 6 VMs from 192.168.1.11 to 192.168.1.16
   ```

**输入验证**:

- **CIDR 格式验证**: 确保输入的是有效的 IPv4 地址/CIDR 格式（如 192.168.1.10/24）
- **IP 地址范围验证**: 检查起始 IP 是否在 CIDR 范围内
- **网络配置一致性**: 验证网关、DNS 与子网的一致性
- **重试机制**: 输入错误时提供重新输入的机会

### 2. NAT 网络模式

**NAT 网络模式特点**:

- VM 通过 NAT 访问外部网络
- 网络范围: `192.168.200.0/24`
- DHCP 范围: `192.168.200.10-192.168.200.254`
- 网关: `192.168.200.1`

**适用场景**:

- 开发和测试环境
- 不需要外部直接访问 VM
- 网络隔离要求

## 使用方法

### 命令行示例

```bash
# 下载脚本
curl -sSL https://raw.githubusercontent.com/upmio/kubespray-upm/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh -o "libvirt_kubespray_setup.sh"
chmod +x ./libvirt_kubespray_setup.sh

# 查看帮助和版本信息
bash ./libvirt_kubespray_setup.sh -h
bash ./libvirt_kubespray_setup.sh --version

# 基础安装（仅 Kubernetes 集群）
bash ./libvirt_kubespray_setup.sh --k8s

# 自动确认模式（非交互）
bash ./libvirt_kubespray_setup.sh --k8s -y

# 设置网络类型
bash ./libvirt_kubespray_setup.sh --k8s -n nat            # NAT 模式（默认）
bash ./libvirt_kubespray_setup.sh --k8s -n bridge         # 桥接模式

# 模块化安装
bash ./libvirt_kubespray_setup.sh --lvmlocalpv             # 安装 LVM LocalPV 存储
bash ./libvirt_kubespray_setup.sh --cnpg                   # 安装 CloudNativePG 数据库
bash ./libvirt_kubespray_setup.sh --prometheus             # 安装 Prometheus 监控
bash ./libvirt_kubespray_setup.sh --upm-engine            # 安装 UPM Engine
bash ./libvirt_kubespray_setup.sh --upm-platform          # 安装 UPM Platform

# 完整安装（所有组件）
bash ./libvirt_kubespray_setup.sh --all -y

# 虚拟机管理命令
bash ./libvirt_kubespray_setup.sh --status                # 查看虚拟机状态
bash ./libvirt_kubespray_setup.sh --ssh k8s-1            # SSH 连接到指定节点
bash ./libvirt_kubespray_setup.sh --destroy              # 销毁所有虚拟机
bash ./libvirt_kubespray_setup.sh --halt                 # 停止所有虚拟机
bash ./libvirt_kubespray_setup.sh --up                   # 启动所有虚拟机
```

### 安装组件说明

脚本会自动安装和配置以下组件：

#### 系统基础组件

- **系统依赖**: Development Tools、Git、curl、wget、vim 等基础工具
- **虚拟化组件**: libvirt、qemu-kvm、virt-manager、libguestfs-tools
- **开发环境**: Vagrant、vagrant-libvirt、pyenv、Python 3.11.10
- **虚拟机管理**: 智能虚拟机检测、生命周期管理、状态监控和交互式处理

#### Kubernetes 生态组件

- **Kubernetes 集群** (`--k8s`): 基础 Kubernetes 集群部署
- **LVM LocalPV** (`--lvmlocalpv`): 本地持久卷存储解决方案
- **CloudNativePG** (`--cnpg`): 云原生 PostgreSQL 数据库
- **Prometheus** (`--prometheus`): 监控和告警系统
- **UPM Engine** (`--upm-engine`): UPM 核心引擎组件
- **UPM Platform** (`--upm-platform`): UPM 平台管理界面

### 环境配置（可选）

#### 代理配置

如果在企业网络环境中，可以设置代理：

```bash
export HTTP_PROXY="http://proxy.company.com:8080"
export HTTPS_PROXY="http://proxy.company.com:8080"
export NO_PROXY="localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16"
```

#### 桥接网络准备

如果选择桥接网络模式，建议提前准备以下信息：

- **当前网络接口的 IP 地址**: 用于安全确认
- **VM 起始 IP 地址（带 CIDR）**: 例如 `192.168.1.10/24`
- **网关 IP 地址**: 例如 `192.168.1.1`
- **DNS 服务器 IP**: 例如 `8.8.8.8`

## 容器镜像仓库配置

### 概述

在企业环境中，通常需要配置容器镜像仓库转发以提高镜像拉取速度或使用私有镜像仓库。本脚本支持通过 containerd 配置文件自定义镜像仓库设置。

### 配置文件说明

脚本提供了 `containerd-example.yml` 样例文件，展示了如何配置 containerd 镜像仓库转发。该文件位于：

```
vagrant_setup_scripts/containerd-example.yml
```

####### 配置步骤

#### 1. 准备配置文件

```bash
# 基于样例文件创建配置文件（脚本会自动检测并使用）
cp vagrant_setup_scripts/containerd-example.yml containerd.yml
```

> **注意**: 脚本会自动检测脚本目录下的 `containerd.yml` 文件，如果存在则自动应用配置。无需手动复制到 kubespray 目录。

#### 2. 编辑配置文件

根据您的环境需求编辑 `containerd.yml` 文件：

```yaml
# 启用镜像仓库转发配置
containerd_registries_mirrors:
  # 配置 Docker Hub 转发
  - prefix: docker.io
    mirrors:
    - host: http://your-harbor.company.com  # 替换为您的私有仓库地址
      capabilities: ["pull", "resolve"]
      skip_verify: true  # true: 跳过TLS验证, false: 启用TLS验证
      header:
        # 如果需要认证，配置Authorization头
        Authorization: "Basic <base64-encoded-credentials>"
  
  # 配置 Quay.io 转发
  - prefix: quay.io
    mirrors:
    - host: http://your-harbor.company.com
      capabilities: ["pull", "resolve"]
      skip_verify: true
      header:
        Authorization: "Basic <base64-encoded-credentials>"
  
  # 配置 Kubernetes 镜像仓库转发
  - prefix: registry.k8s.io
    mirrors:
    - host: http://your-harbor.company.com
      capabilities: ["pull", "resolve"]
      skip_verify: true
```

#### 3. 认证配置

如果您的私有仓库需要认证，需要生成 Base64 编码的认证信息：

```bash
# 生成 Base64 编码的用户名:密码
echo -n "username:password" | base64
# 输出示例: dXNlcm5hbWU6cGFzc3dvcmQ=

# 在配置文件中使用
Authorization: "Basic dXNlcm5hbWU6cGFzc3dvcmQ="
```

#### 4. 常见配置示例

**Harbor 私有仓库配置**：

```yaml
containerd_registries_mirrors:
  - prefix: docker.io
    mirrors:
    - host: https://harbor.company.com
      capabilities: ["pull", "resolve"]
      skip_verify: false  # 如果使用有效SSL证书
      header:
        Authorization: "Basic YWRtaW46SGFyYm9yMTIzNDU="  # admin:Harbor12345
```

**阿里云镜像加速器配置**：

```yaml
containerd_registries_mirrors:
  - prefix: docker.io
    mirrors:
    - host: https://your-id.mirror.aliyuncs.com
      capabilities: ["pull", "resolve"]
      skip_verify: false
```

**腾讯云镜像加速器配置**：

```yaml
containerd_registries_mirrors:
  - prefix: docker.io
    mirrors:
    - host: https://mirror.ccs.tencentyun.com
      capabilities: ["pull", "resolve"]
      skip_verify: false
```

### 部署应用配置

配置完成后，脚本会在部署过程中自动检测并应用 `containerd.yml` 配置：

```bash
# 运行部署脚本（脚本会自动应用 containerd 配置）
bash ./libvirt_kubespray_setup.sh --k8s

# 如果已经部署了集群，需要重新部署以应用新配置
# 1. 销毁现有集群
bash ./libvirt_kubespray_setup.sh --destroy

# 2. 重新部署集群
bash ./libvirt_kubespray_setup.sh --k8s
```

> **自动化说明**: 脚本在部署前会自动检测脚本目录下的 `containerd.yml` 文件，如果存在则自动备份原配置并应用新配置。

### 验证配置

部署完成后，可以验证镜像仓库配置是否生效：

```bash
# SSH 到集群节点（使用脚本提供的 SSH 命令）
bash ./libvirt_kubespray_setup.sh --ssh k8s-1

# 或者直接使用 vagrant ssh（需要在 kubespray-upm 目录下）
cd kubespray-upm
vagrant ssh k8s-1

# 检查 containerd 配置
sudo cat /etc/containerd/config.toml | grep -A 10 "mirrors"

# 测试镜像拉取
sudo crictl pull nginx:latest

# 查看镜像拉取日志
sudo journalctl -u containerd -f

# 验证配置是否已应用
sudo crictl info | grep -A 20 "registry"
```

### 重要注意事项

1. **TLS 验证**: 生产环境建议启用 TLS 验证（`skip_verify: false`）
2. **认证安全**: 避免在配置文件中明文存储密码，使用 Base64 编码
3. **网络连通性**: 确保集群节点能够访问配置的镜像仓库地址
4. **配置备份**: 建议备份自定义的 containerd 配置文件
5. **版本兼容性**: 确保镜像仓库支持所需的 containerd API 版本

## 虚拟机管理

### 智能虚拟机检测与处理

脚本提供了智能的虚拟机管理功能，能够自动检测现有的 kubespray 虚拟机并提供灵活的处理选项。

#### 虚拟机检测机制

脚本会在部署前自动检测以下虚拟机：
- 名称匹配 `k8s-*` 模式的虚拟机
- 由 kubespray 创建的相关虚拟机
- 当前配置目录下的 Vagrant 管理的虚拟机

#### 处理策略

根据检测到的虚拟机数量与目标配置的匹配情况，脚本提供不同的处理选项：

##### 1. 虚拟机数量匹配（现有VM数量 = 目标配置数量）

当检测到的虚拟机数量与当前配置的节点数量一致时，脚本提供以下选项：

```bash
# 交互式选择
[1] 保留现有虚拟机，仅运行 vagrant provision（推荐）
[2] 保留现有虚拟机，重新运行完整的 kubespray 部署
[3] 删除所有现有虚拟机，重新创建并部署
[4] 取消部署，退出程序
```

**选项说明**：
- **选项1（推荐）**: 保留现有虚拟机，仅更新配置和服务，速度最快
- **选项2**: 保留虚拟机但重新运行完整部署流程
- **选项3**: 完全重新创建，适用于需要全新环境的场景
- **选项4**: 安全退出，不做任何更改

##### 2. 虚拟机数量不匹配（现有VM数量 ≠ 目标配置数量）

当检测到的虚拟机数量与配置不一致时，为确保部署的一致性，脚本提供以下选项：

```bash
# 交互式选择
[1] 删除所有现有虚拟机，重新创建并部署（推荐）
[2] 取消部署，退出程序
```

**选项说明**：
- **选项1（推荐）**: 删除现有虚拟机并按新配置重新创建
- **选项2**: 安全退出，允许用户手动处理虚拟机

#### 自动化模式

使用 `-y` 参数时，脚本会采用以下自动化策略：

```bash
# 自动化部署
bash ./libvirt_kubespray_setup.sh --k8s -y
```

**自动化行为**：
- **数量匹配**: 自动选择"保留现有虚拟机，仅运行 vagrant provision"
- **数量不匹配**: 自动选择"删除所有现有虚拟机，重新创建并部署"

#### 虚拟机管理命令

脚本还提供了便捷的虚拟机管理命令：

```bash
# 查看虚拟机状态
bash ./libvirt_kubespray_setup.sh --status

# SSH 连接到指定节点
bash ./libvirt_kubespray_setup.sh --ssh k8s-1
bash ./libvirt_kubespray_setup.sh --ssh k8s-2

# 销毁所有虚拟机
bash ./libvirt_kubespray_setup.sh --destroy

# 停止所有虚拟机
bash ./libvirt_kubespray_setup.sh --halt

# 启动所有虚拟机
bash ./libvirt_kubespray_setup.sh --up
```

#### 安全特性

- **交互式确认**: 默认情况下，所有删除操作都需要用户确认
- **智能检测**: 只处理 kubespray 相关的虚拟机，不影响其他虚拟机
- **状态验证**: 在执行操作前验证虚拟机状态
- **错误处理**: 提供详细的错误信息和恢复建议

#### 最佳实践

1. **首次部署**: 直接运行部署命令，脚本会自动创建虚拟机
2. **配置更新**: 使用"保留现有虚拟机，仅运行 vagrant provision"选项
3. **节点数量变更**: 选择删除并重新创建虚拟机
4. **故障恢复**: 使用 `--destroy` 清理后重新部署
5. **开发测试**: 使用 `-y` 参数进行快速自动化部署

## 安全配置

脚本会自动执行以下安全配置：

- **防火墙**: 停止并禁用 `firewalld` 服务，确保 VM 网络通信正常
- **SELinux**: 临时和永久禁用 SELinux（需要重启系统使永久配置生效）
- **SSH 密钥**: 自动生成和管理 SSH 密钥（`~/.ssh/vagrant_rsa`）
- **网络隔离**: 支持 NAT 和桥接两种网络模式
- **镜像仓库安全**: 支持私有镜像仓库的 TLS 和认证配置

## 自动化部署

脚本提供完全自动化的部署流程，特别适合 CI/CD 环境和批量部署场景：

### 部署流程

1. **环境准备**: 系统检查、依赖安装、虚拟化配置
2. **集群部署**: Vagrant 初始化、虚拟机创建、Kubernetes 安装
3. **组件安装**: 根据选项安装存储、数据库、监控、UPM 组件
4. **配置完成**: kubectl 配置、状态验证、访问信息显示

### 自动化选项

```bash
# 完全自动化部署（使用默认配置）
bash ./libvirt_kubespray_setup.sh --k8s -y

# 指定网络模式的自动化部署
bash ./libvirt_kubespray_setup.sh --k8s -n nat -y     # NAT 模式
bash ./libvirt_kubespray_setup.sh --k8s -n bridge -y   # 桥接模式（需要交互配置）
```

### 环境变量配置

脚本支持通过环境变量进行高级配置：

```bash
# 设置代理配置
export HTTP_PROXY="http://proxy.company.com:8080"
export HTTPS_PROXY="http://proxy.company.com:8080"
export NO_PROXY="localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16"

# 运行部署
bash ./libvirt_kubespray_setup.sh --k8s -y
```

脚本会在关键步骤显示详细预览和确认信息，确保用户了解将要执行的操作。

## 集群访问和管理

### kubectl 本地访问

脚本会自动配置 kubectl 本地访问，无需手动设置：

```bash
# kubectl 二进制文件位置
~/.local/bin/kubectl

# kubeconfig 文件位置
~/.kube/config

# 基本命令
kubectl get nodes
kubectl get pods --all-namespaces
kubectl get services --all-namespaces

# 查看集群信息
kubectl cluster-info
kubectl get nodes -o wide
kubectl top nodes  # 查看资源使用情况
```

### 基础组件管理命令

```bash
# 查看所有组件状态
kubectl get componentstatuses

# 查看系统 Pod 状态
kubectl get pods -n kube-system
kubectl get pods -n kube-system -o wide

# 查看服务状态
kubectl get services --all-namespaces

# 查看存储类和持久卷
kubectl get storageclass
kubectl get pv,pvc --all-namespaces

# 查看网络策略
kubectl get networkpolicies --all-namespaces
```

### 专用组件管理命令

#### LVM LocalPV 存储管理

```bash
# 查看存储类
kubectl get storageclass

# 查看 LVM LocalPV 组件
kubectl get pods -n openebs

# 查看持久卷
kubectl get pv
kubectl get pvc --all-namespaces

# 查看节点标签
kubectl get nodes --show-labels | grep openebs
```

#### CloudNativePG 数据库管理

```bash
# 查看 PostgreSQL 集群
kubectl get clusters.postgresql.cnpg.io --all-namespaces

# 查看数据库 Pod
kubectl get pods -l cnpg.io/cluster --all-namespaces

# 查看 CloudNativePG Operator
kubectl get pods -n cnpg-system

# 查看数据库服务
kubectl get services -l cnpg.io/cluster --all-namespaces
```

#### Prometheus 监控管理

```bash
# 查看 Prometheus 组件
kubectl get pods -n monitoring

# 查看 Prometheus 服务
kubectl get services -n monitoring

# 访问 Prometheus Web UI（端口转发）
kubectl port-forward -n monitoring svc/prometheus-server 9090:80
# 然后访问 http://localhost:9090

# 访问 Grafana（端口转发）
kubectl port-forward -n monitoring svc/prometheus-grafana 3000:80
# 然后访问 http://localhost:3000
# 默认用户名: admin, 密码: prom-operator

# 查看 AlertManager
kubectl get pods -n monitoring -l app.kubernetes.io/name=alertmanager
```

#### UPM 组件管理

```bash
# 查看 UPM Engine
kubectl get pods -n upm-system -l app=upm-engine

# 查看 UPM Platform
kubectl get pods -n upm-system -l app=upm-platform

# 查看 UPM 服务
kubectl get services -n upm-system

# 查看 UPM 配置
kubectl get configmaps -n upm-system
```

### SSH 访问集群节点

#### 基本访问命令

```bash
# 进入项目目录
cd $KUBESPRAY_DIR

# 激活 Python 虚拟环境
source venv/bin/activate

# SSH 连接到主节点（控制平面）
vagrant ssh k8s-1

# 访问工作节点
vagrant ssh k8s-2
vagrant ssh k8s-3

# 查看所有节点状态
vagrant status
```

#### 节点管理操作

```bash
# 在节点上查看容器运行时状态
vagrant ssh k8s-1 -c "sudo crictl ps"
vagrant ssh k8s-1 -c "sudo crictl images"

# 查看节点系统资源
vagrant ssh k8s-1 -c "free -h && df -h"

# 查看节点网络配置
vagrant ssh k8s-1 -c "ip addr show"

# 在节点内查看集群状态
vagrant ssh k8s-1 -c "sudo kubectl get nodes"
```

### 集群管理命令

#### 前置条件

在执行以下 Vagrant 命令之前，必须确保：

1. **进入正确的工作目录**：

   ```bash
   cd $KUBESPRAY_DIR
   ```

2. **确认 Vagrantfile 存在**：

   ```bash
   ls -la Vagrantfile
   # 应该显示 Vagrantfile 文件
   ```

3. **验证配置文件**：

   ```bash
   ls -la config.rb
   # 确认 config.rb 配置文件存在且配置正确
   ```

4. **检查 libvirt 服务状态**：

   ```bash
   sudo systemctl status libvirtd
   # 确保 libvirt 服务正在运行
   ```

#### 管理命令

| 操作 | 命令 | 说明 |
|------|------|------|
| 停止集群 | `vagrant halt` | 停止所有虚拟机 |
| 启动集群 | `vagrant up` | 启动所有虚拟机 |
| 销毁集群 | `vagrant destroy -f` | 完全删除集群 |
| SSH 连接 | `vagrant ssh k8s-1` | 连接到主节点 |
| 查看状态 | `vagrant status` | 查看虚拟机状态 |
| 重新部署 | `vagrant up --provider=libvirt --no-parallel` | 重新创建集群 |

> **重要提示**：所有 Vagrant 命令都必须在包含 `Vagrantfile` 的目录中执行，通常是 `$KUBESPRAY_DIR` 目录（默认为 `$(pwd)/kubespray-upm`）。

## 故障排除

### 常见问题

#### 1. 网络连接失败

**症状**: 脚本无法下载软件包或访问远程仓库

**诊断步骤**:

```bash
# 检查网络连通性
curl -I https://github.com

# 检查代理设置
echo $HTTP_PROXY
echo $HTTPS_PROXY

# 测试代理连接
curl --proxy $HTTP_PROXY -I https://github.com
```

**解决方案**:

```bash
# 配置代理（如果需要）
export HTTP_PROXY="http://proxy.company.com:8080"
export HTTPS_PROXY="http://proxy.company.com:8080"
export NO_PROXY="localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16"

# 测试网络连接
ping -c 4 8.8.8.8
nslookup github.com
```

#### 2. libvirt 服务问题

**症状**: 无法创建或管理虚拟机，出现连接错误

**诊断步骤**:
```bash
# 检查服务状态
sudo systemctl status libvirtd
virsh list --all
```

**解决方案**:
```bash
# 启动并启用 libvirt 服务
sudo systemctl start libvirtd
sudo systemctl enable libvirtd

# 重启相关服务
sudo systemctl restart libvirtd
sudo systemctl restart virtlogd

# 检查网络
sudo virsh net-list --all
sudo virsh net-start default
```

#### 3. Vagrant 插件安装失败

**症状**: 插件安装过程中出现编译错误或依赖缺失

**常见错误信息**:

- `Failed to build gem native extension`
- `libvirt development headers not found`
- `ruby development headers missing`

**解决方案**:

```bash
# 安装必要的开发工具和依赖
sudo dnf groupinstall "Development Tools" -y
sudo dnf install libvirt-devel ruby-devel libguestfs-tools-c -y

# 清理并重新安装插件
vagrant plugin uninstall vagrant-libvirt
vagrant plugin install vagrant-libvirt

# 如果仍然失败，尝试指定版本
vagrant plugin install vagrant-libvirt --plugin-version 0.12.2
```

#### 4. 桥接网络配置失败

**症状**: 无法创建桥接网络或VM无法获取IP地址

**诊断步骤**:

```bash
# 检查网络接口
ip link show
nmcli device status

# 检查桥接配置
sudo brctl show
virsh net-list --all
```

**解决方案**:

```bash
# 重新配置网络管理器
sudo nmcli connection reload
sudo systemctl restart NetworkManager

# 检查防火墙设置
sudo firewall-cmd --list-all
sudo firewall-cmd --add-service=libvirt --permanent
sudo firewall-cmd --reload

# 重新创建桥接网络
sudo virsh net-destroy default
sudo virsh net-start default
```

#### 5. RHEL 系统特定问题

**订阅管理问题**:

```bash
# 检查系统注册状态
subscription-manager status

# 检查可用订阅
subscription-manager list --available

# 重新注册系统（如果需要）
sudo subscription-manager register --username=<用户名> --password=<密码>

# 附加订阅
sudo subscription-manager attach --auto
```

**仓库配置问题**:

```bash
# 检查已启用的仓库
subscription-manager repos --list-enabled

# 手动启用必需的仓库（替换 {version} 和 {arch}）
sudo subscription-manager repos --enable=rhel-9-for-x86_64-baseos-rpms
sudo subscription-manager repos --enable=rhel-9-for-x86_64-appstream-rpms
sudo subscription-manager repos --enable=codeready-builder-for-rhel-9-x86_64-rpms

# 清理并重建仓库缓存
sudo dnf clean all
sudo dnf makecache
```

**RHEL 系统检测问题**:

```bash
# 验证系统识别
cat /etc/redhat-release
# 应该包含 "Red Hat Enterprise Linux"

# 检查订阅管理器身份
subscription-manager identity
# 输出应该包含 "Red Hat"

# 如果检测失败，检查文件权限
ls -la /etc/redhat-release
sudo chmod 644 /etc/redhat-release
```

**网络和代理问题（RHEL 环境）**:

```bash
# 配置订阅管理器代理
sudo subscription-manager config --server.proxy_hostname=<代理主机>
sudo subscription-manager config --server.proxy_port=<代理端口>

# 测试订阅管理器连接
subscription-manager refresh
```

#### 6. 桥接网络交互输入问题

**症状**: 脚本在桥接网络配置时卡住或输入验证失败

**IP 地址验证失败**:

```bash
# 检查当前网络接口IP
ip addr show ens33

# 确认输入的IP地址格式正确
# 正确格式: 192.168.1.100
# 错误格式: 192.168.1.100/24 或 192.168.1
```

**VM IP 范围冲突**:

```bash
# 检查网络中已使用的IP
nmap -sn 192.168.1.0/24

# 或使用ping检查特定IP
ping -c 1 192.168.1.10

# 选择未被占用的IP范围作为VM起始IP
```

**网络配置不一致**:

```bash
# 确保网关IP在同一子网内
# 例如: 起始IP 192.168.1.10, 网关应为 192.168.1.1
# 而不是 192.168.2.1

# 检查DNS服务器可达性
ping -c 1 8.8.8.8
nslookup google.com 8.8.8.8
```

**自动化环境解决方案**:

```bash
# 方案1: 使用 NAT 模式避免交互
bash ./libvirt_kubespray_setup.sh --k8s -n nat -y

# 方案2: 预先配置环境变量
export BRIDGE_INTERFACE="enp0s3"  # 替换为实际网络接口
export BRIDGE_IP="192.168.1.100"  # 设置桥接IP
export BRIDGE_NETMASK="255.255.255.0"
export BRIDGE_GATEWAY="192.168.1.1"
export BRIDGE_DNS="8.8.8.8"
bash ./libvirt_kubespray_setup.sh --k8s -n bridge -y
```

#### 7. 组件安装问题

**LVM LocalPV 安装失败**:

```bash
# 检查节点标签
kubectl get nodes --show-labels | grep openebs

# 检查 Helm 仓库
helm repo list | grep openebs

# 重新添加仓库
helm repo add openebs https://openebs.github.io/lvm-localpv
helm repo update

# 检查 LVM2 工具
sudo dnf install lvm2 -y

# 手动安装 LVM LocalPV
helm install lvm-localpv openebs/lvm-localpv -n openebs --create-namespace
```

**CloudNativePG 安装失败**:

```bash
# 检查 Operator 状态
kubectl get pods -n cnpg-system

# 检查 CRD
kubectl get crd | grep postgresql

# 重新安装 CloudNativePG
kubectl apply -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.24/releases/cnpg-1.24.1.yaml
```

**Prometheus 安装失败**:

```bash
# 检查节点标签
kubectl get nodes --show-labels | grep monitoring

# 检查 Helm 仓库
helm repo list | grep prometheus

# 重新添加仓库
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

# 检查存储类
kubectl get storageclass

# 手动安装 Prometheus
helm install prometheus prometheus-community/kube-prometheus-stack -n monitoring --create-namespace
```

**UPM 组件安装失败**:

```bash
# 检查 UPM 命名空间
kubectl get namespace upm-system

# 检查 UPM 配置
kubectl get configmaps -n upm-system

# 检查 UPM 服务
kubectl get services -n upm-system

# 查看 UPM Pod 日志
kubectl logs -n upm-system -l app=upm-engine
kubectl logs -n upm-system -l app=upm-platform
```

### 调试和日志

#### 调试模式

```bash
# 启用详细输出模式
bash -x ./libvirt_kubespray_setup.sh --k8s

# 检查脚本语法
bash -n ./libvirt_kubespray_setup.sh

# 启用 Vagrant 调试输出
VAGRANT_LOG=info vagrant up
```

#### 日志文件位置

```bash
# 脚本执行日志
tail -f /tmp/libvirt_kubespray_setup.log

# libvirt 日志
sudo journalctl -u libvirtd -f

# Vagrant 日志
ls -la .vagrant/logs/

# 系统日志
sudo journalctl -xe
```

#### 常用调试命令

```bash
# 检查虚拟机状态
cd $KUBESPRAY_DIR && vagrant status

# 查看虚拟机详细信息
virsh list --all
virsh dominfo k8s-1

# 检查网络配置
virsh net-list --all
virsh net-dumpxml default

# 查看资源使用情况
virsh domstats --cpu-total --balloon --block --vcpu k8s-1
```

## 注意事项

### 重要警告

#### 网络配置风险

1. **桥接网络风险**: 配置桥接网络会移除现有 IP 地址，可能导致SSH连接中断
   - 建议在本地控制台执行，避免远程连接中断
   - 脚本会要求二次确认以确保用户理解风险

2. **网络冲突**: 脚本会验证 VM IP 范围，但仍需手动确保不与现有设备冲突
   - 使用 `nmap` 或 `ping` 预先检查IP范围可用性
   - 避免使用DHCP分配范围内的静态IP

#### 系统要求警告

3. **系统重启需求**: 如果内核更新或SELinux配置变更，需要重启系统
   - 脚本会提示何时需要重启
   - 重启后需要重新验证虚拟化功能

4. **用户权限**: 添加用户组后需要注销并重新登录以使权限生效
   - 或使用 `newgrp libvirt` 临时获取权限
   - 确保当前用户具有sudo权限

5. **资源要求**: 确保系统有足够的硬件资源
   - CPU: 最少12核心（推荐24+核心）
   - 内存: 最少32GB（推荐64GB+）
   - 磁盘: 最少200GB可用空间

#### RHEL系统特殊要求

6. **RHEL 订阅要求**: RHEL 系统必须已注册并有有效订阅
   - 脚本会自动检查订阅状态
   - 订阅过期或未注册会导致脚本失败

7. **RHEL 仓库依赖**: 脚本需要启用特定的 RHEL 仓库
   - 确保订阅包含所需仓库的访问权限
   - 企业环境中可能需要配置内部仓库镜像

#### 数据安全警告

8. **配置备份**: 脚本会修改系统网络和虚拟化配置
   - 建议在运行前备份重要配置文件
   - 记录当前网络配置以便恢复

9. **防火墙和SELinux**: 脚本会禁用防火墙和SELinux
   - 这可能影响系统安全策略
   - 生产环境中需要重新配置安全策略

### 最佳实践

#### 部署前准备

1. **系统检查**: 运行预检查命令验证系统是否满足所有要求
2. **备份配置**: 在修改网络配置前备份当前设置和重要数据
3. **本地执行**: 桥接网络配置建议在本地控制台执行，避免SSH连接中断
4. **网络规划**: 提前规划 IP 地址分配和网络拓扑，避免地址冲突
5. **资源评估**: 确保系统有足够的 CPU、内存和磁盘空间
6. **RHEL 订阅验证**: 运行脚本前确认 RHEL 系统已正确注册和订阅
7. **代理配置**: 企业环境中提前配置代理设置和证书信任

#### 部署过程管理

8. **分阶段执行**: 先完成环境设置，再进行集群部署，便于问题定位
9. **资源监控**: 部署期间监控系统资源使用情况，及时发现瓶颈
10. **日志检查**: 定期检查日志文件以发现潜在问题和警告信息
11. **配置验证**: 部署前验证 Vagrant 配置文件的正确性
12. **网络测试**: 配置完成后测试 VM 与主机、外部网络的连通性
13. **进度跟踪**: 使用脚本提供的进度信息跟踪部署状态

#### 组件安装策略

14. **模块化安装**: 根据实际需求选择安装组件，避免不必要的资源消耗
15. **依赖顺序**: 按照依赖关系安装组件（如先安装 K8s 再安装存储和监控）
16. **资源规划**: 为每个组件预留足够的计算和存储资源
17. **存储准备**: 安装 LVM LocalPV 前确保节点有足够的磁盘空间和LVM配置
18. **监控配置**: 安装 Prometheus 时合理配置存储类和节点亲和性
19. **数据库规划**: 部署 CloudNativePG 前规划数据库集群的高可用配置
20. **UPM 配置**: 安装 UPM 组件前确认网络和存储配置满足要求

#### 安全和维护

21. **密钥管理**: 妥善保管生成的SSH密钥和认证信息
22. **定期备份**: 建立集群配置和数据的定期备份机制
23. **更新策略**: 制定组件更新和安全补丁的应用策略
24. **监控告警**: 配置适当的监控告警规则，及时发现问题
25. **文档记录**: 记录自定义配置和部署参数，便于后续维护

## 支持的配置

### 默认集群配置

脚本会自动从 `vagrant/config.rb` 读取配置：

#### 集群设置

- **Kubernetes 版本**: 1.33.2
- **操作系统**: Rocky Linux 9
- **网络插件**: Calico
- **节点前缀**: k8s
- **实例数量**: 5 个

#### 节点配置

- **Master 节点**: 1 个（4 CPU, 4GB 内存）
- **UPM Control**: 1 个（12 CPU, 24GB 内存）
- **Worker 节点**: 3 个（8 CPU, 16GB 内存）

#### 资源计算

- **总 CPU**: 40 核心
- **总内存**: 74 GB

#### 配置文件

- **位置**: `$KUBESPRAY_DIR/config.rb`（默认为 `$(pwd)/kubespray-upm/config.rb`）
- **模板**: 根据网络模式自动选择
- **自定义**: 可手动修改配置后重新部署

## 相关文档

### 基础组件

- [Kubespray 官方文档](https://kubespray.io/)
- [Vagrant 文档](https://www.vagrantup.com/docs)
- [libvirt 文档](https://libvirt.org/docs.html)
- [Rocky Linux 文档](https://docs.rockylinux.org/)
- [脚本源码](https://github.com/upmio/kubespray-upm/blob/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh)

### 存储组件

- [LVM LocalPV 文档](https://github.com/openebs/lvm-localpv)
- [OpenEBS 官方文档](https://openebs.io/docs/)
- [LVM2 用户指南](https://access.redhat.com/documentation/en-us/red_hat_enterprise_linux/9/html/configuring_and_managing_logical_volumes/index)

### 数据库组件

- [CloudNativePG 官方文档](https://cloudnative-pg.io/documentation/)
- [PostgreSQL 官方文档](https://www.postgresql.org/docs/)
- [Kubernetes Operator 模式](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)

### 监控组件

- [Prometheus 官方文档](https://prometheus.io/docs/)
- [Grafana 官方文档](https://grafana.com/docs/)
- [kube-prometheus-stack](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)
- [AlertManager 文档](https://prometheus.io/docs/alerting/latest/alertmanager/)

### UPM 组件

- [UPM Engine 文档](https://docs.upm.io/engine/)
- [UPM Platform 文档](https://docs.upm.io/platform/)
- [UPM 架构指南](https://docs.upm.io/architecture/)

### 工具和实用程序

- [Helm 官方文档](https://helm.sh/docs/)
- [kubectl 参考文档](https://kubernetes.io/docs/reference/kubectl/)
- [NetworkManager 文档](https://networkmanager.dev/docs/)
- [RHEL 订阅管理](https://access.redhat.com/documentation/en-us/red_hat_subscription_management/)
