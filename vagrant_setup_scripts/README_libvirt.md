# Kubespray Libvirt 环境设置指南

## 概述

本文档描述如何使用 `libvirt_kubespray_setup.sh` 脚本在 libvirt 虚拟化环境中设置 Kubespray Kubernetes 集群。该脚本专为 Red Hat 系列 Linux 系统设计，提供完整的自动化环境配置和交互式部署体验。

### 脚本特性

- **版本**: v1.0
- **模块化安装**: 支持选择性安装不同组件（K8s、LVM LocalPV、Prometheus、CloudNativePG、UPM Engine、UPM Platform）
- **交互式安装**: 提供详细的安装预览和确认
- **智能网络配置**: 自动检测和配置网络模式
- **统一输入验证**: 改进的用户输入处理和验证
- **完整日志记录**: 详细的操作日志和错误处理
- **一键部署**: 环境设置完成后可直接部署 Kubernetes 集群
- **多组件支持**: 支持安装 Kubernetes 生态系统的多种组件

### ⚡ 一键命令

如果您想要最快速的体验，可以使用以下一键命令：

下载并安装 Kubernetes 集群（NAT 模式）

```bash
curl -sSL https://raw.githubusercontent.com/upmio/kubespray-upm/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh && chmod +x ./libvirt_kubespray_setup.sh && bash ./libvirt_kubespray_setup.sh --k8s -y
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

- 脚本会自动检测系统环境并安装必要的依赖
- **网络模式选择**：脚本会智能检测并提示选择网络模式
  - 🌉 **桥接模式**：VM 直接连接物理网络，适合生产环境（需要配置网络接口）
  - 🔒 **NAT 模式**：VM 通过 NAT 访问网络，适合开发测试（自动配置）
- 整个安装过程约 15-25 分钟，需要稳定的网络连接

> 💡 **网络配置详情**：如需了解网络模式的详细配置，请参考 [网络配置选项](#网络配置选项) 章节

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
| `-n <network_type>` | | 设置网络类型（private\|public，默认：private）<br/>仅在使用 `--k8s` 或完整安装模式时有效<br/>设置为 'public' 时需要交互式配置 |

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
   🌐 Public Network Configuration
   Please provide the network configuration for public network:
   
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
bash ./libvirt_kubespray_setup.sh --k8s -n private         # NAT 模式（默认）
bash ./libvirt_kubespray_setup.sh --k8s -n public          # 桥接模式

# 模块化安装
bash ./libvirt_kubespray_setup.sh --lvmlocalpv             # 安装 LVM LocalPV 存储
bash ./libvirt_kubespray_setup.sh --cnpg                   # 安装 CloudNativePG 数据库
bash ./libvirt_kubespray_setup.sh --prometheus             # 安装 Prometheus 监控
bash ./libvirt_kubespray_setup.sh --upm-engine            # 安装 UPM Engine
bash ./libvirt_kubespray_setup.sh --upm-platform          # 安装 UPM Platform

# 完整安装（所有组件）
bash ./libvirt_kubespray_setup.sh --all -y
```

### 安装组件说明

脚本会自动安装和配置以下组件：

#### 系统基础组件
- **系统依赖**: Development Tools、Git、curl、wget、vim 等基础工具
- **虚拟化组件**: libvirt、qemu-kvm、virt-manager、libguestfs-tools
- **开发环境**: Vagrant、vagrant-libvirt、pyenv、Python 3.11.10

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

## 安全配置

脚本会自动执行以下安全配置：

- **防火墙**: 停止并禁用 `firewalld` 服务，确保 VM 网络通信正常
- **SELinux**: 临时和永久禁用 SELinux（需要重启系统使永久配置生效）
- **SSH 密钥**: 自动生成和管理 SSH 密钥（`~/.ssh/vagrant_rsa`）
- **网络隔离**: 支持 NAT 和桥接两种网络模式

## 自动化部署

脚本提供完全自动化的部署流程：

1. **环境准备**: 系统检查、依赖安装、虚拟化配置
2. **集群部署**: Vagrant 初始化、虚拟机创建、Kubernetes 安装
3. **组件安装**: 根据选项安装存储、数据库、监控、UPM 组件
4. **配置完成**: kubectl 配置、状态验证、访问信息显示

脚本会在关键步骤显示详细预览和确认信息，确保用户了解将要执行的操作。

## 集群访问和管理

### kubectl 本地访问

脚本会自动配置 kubectl 本地访问：

```bash
# kubectl 二进制文件位置
~/.local/bin/kubectl

# kubeconfig 文件位置
~/.kube/config

# 基本命令
kubectl get nodes
kubectl get pods --all-namespaces
kubectl get services --all-namespaces
```

### 组件管理命令

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

```bash
# 进入项目目录
cd $(pwd)/kubespray

# 激活 Python 虚拟环境
source venv/bin/activate

# SSH 连接到主节点
vagrant ssh k8s-1

# 在节点内查看集群状态
sudo kubectl get nodes
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

> **重要提示**：所有 Vagrant 命令都必须在包含 `Vagrantfile` 的目录中执行，通常是 `$KUBESPRAY_DIR` 目录（默认为 `$(pwd)/kubespray`）。

## 故障排除

### 常见问题

#### 1. 网络连接失败

```bash
# 检查网络连通性
curl -I https://github.com

# 检查代理设置
echo $HTTP_PROXY

# 测试代理连接
curl --proxy $HTTP_PROXY -I https://github.com
```

#### 2. libvirt 服务问题

```bash
# 检查服务状态
sudo systemctl status libvirtd

# 重启服务
sudo systemctl restart libvirtd

# 检查网络
sudo virsh net-list --all
```

#### 3. Vagrant 插件安装失败

```bash
# 检查 libvirt 开发包
sudo dnf install libvirt-devel
# 检查插件
vagrant plugin list
# 重新安装插件
vagrant plugin uninstall vagrant-libvirt
vagrant plugin install vagrant-libvirt
```

#### 4. 桥接网络配置失败

```bash
# 检查网络接口
ip link show
# 检查桥接状态
ip addr show br0
# 检查 NetworkManager 连接
nmcli con show
# 重新配置桥接网络
sudo nmcli con down "System $BRIDGE_INTERFACE"
sudo nmcli con up "Bridge br0"
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

#### 调试模式

```bash
# 启用详细输出
bash -x libvirt_kubespray_setup.sh

# 检查脚本语法
bash -n libvirt_kubespray_setup.sh
```

## 注意事项

### 重要警告

1. **桥接网络风险**: 配置桥接网络会移除现有 IP 地址，可能导致连接中断
2. **系统重启**: 如果内核更新，需要重启系统后才能使用 libvirt
3. **用户组**: 需要注销并重新登录以使组权限生效
4. **资源要求**: 确保系统有足够的 CPU、内存和磁盘空间
5. **网络验证**: 脚本会验证 VM IP 范围，确保不与现有网络冲突
6. **RHEL 订阅要求**: RHEL 系统必须已注册并有有效订阅，否则脚本会失败
7. **RHEL 仓库依赖**: 脚本需要启用特定的 RHEL 仓库，确保订阅包含所需的仓库访问权限

### 最佳实践

#### 基础环境

1. **备份配置**: 在修改网络配置前备份当前设置
2. **本地执行**: 桥接网络配置建议在本地控制台执行
3. **资源监控**: 部署期间监控系统资源使用情况
4. **网络规划**: 提前规划 IP 地址分配和网络拓扑
5. **分阶段执行**: 先完成环境设置，再进行集群部署
6. **日志检查**: 定期检查日志文件以发现潜在问题
7. **配置验证**: 部署前验证 Vagrant 配置文件的正确性
8. **桥接网络准备**: 运行脚本前准备好所有网络配置信息，避免中途查找
9. **IP 范围规划**: 确保为 VM 分配的 IP 范围有足够的连续地址且不与现有设备冲突
10. **网络测试**: 配置完成后测试 VM 与主机、外部网络的连通性
11. **RHEL 订阅验证**: 运行脚本前确认 RHEL 系统已正确注册和订阅
12. **仓库权限检查**: 确保 RHEL 订阅包含所需仓库的访问权限
13. **代理配置**: 如果在企业环境中，确保为 subscription-manager 配置正确的代理设置

#### 组件安装

14. **模块化安装**: 根据实际需求选择安装组件，避免不必要的资源消耗
15. **依赖顺序**: 按照依赖关系安装组件（如先安装 K8s 再安装存储和监控）
16. **资源规划**: 为每个组件预留足够的计算和存储资源
17. **存储准备**: 安装 LVM LocalPV 前确保节点有足够的磁盘空间
18. **监控配置**: 安装 Prometheus 时合理配置存储类和节点亲和性
19. **数据库规划**: 部署 CloudNativePG 前规划数据库集群的高可用配置
20. **UPM 配置**: 安装 UPM 组件前确认网络和存储配置满足要求

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

- **位置**: `$KUBESPRAY_DIR/config.rb`（默认为 `$(pwd)/kubespray/config.rb`）
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
