# Kubespray Libvirt 环境设置指南

## 概述

本文档描述如何使用 `libvirt_kubespray_setup.sh` 脚本在 libvirt 虚拟化环境中设置 Kubespray Kubernetes 集群。该脚本专为 Red Hat 系列 Linux 系统设计，提供完整的自动化环境配置和交互式部署体验。

### 脚本特性

- **版本**: v3.0
- **交互式安装**: 提供详细的安装预览和确认
- **智能网络配置**: 自动检测和配置网络模式
- **统一输入验证**: 改进的用户输入处理和验证
- **完整日志记录**: 详细的操作日志和错误处理
- **一键部署**: 环境设置完成后可直接部署 Kubernetes 集群

## 系统要求

### 硬件要求

- **CPU**: 最少 16 核心（推荐 24+ 核心）
- **内存**: 最少 32GB（推荐 64GB+）
- **磁盘空间**: 最少 200GB 可用空间
- **架构**: x86_64

### 软件要求

- **操作系统**: Rocky Linux 9、CentOS 9、AlmaLinux 9、Red Hat Enterprise Linux (RHEL) 9
- **网络**: 稳定的互联网连接（Proxy 配置可选）
- **权限**: sudo 访问权限

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
- 桥接接口名称固定为 `br0`

**交互式配置流程**:

当选择桥接网络模式时，脚本会进行以下交互式配置：

1. **安全确认**（两次确认）:

   ```bash
   ⚠️ WARNING: Configuring bridge will remove this IP address and may disconnect existing connections!
   Continue with bridge configuration? (y/N)
   
   🔐 Second Confirmation Required
   🔒 Security Check: To proceed with bridge configuration
   Please enter the current IP address of 'ens33'
   ⚠️ This confirms you understand that IP '192.168.1.100' will be permanently removed
   Enter current IP address to confirm deletion: [用户需输入当前IP地址]
   ```

2. **网络配置输入**:

   ```bash
   🌐 Public Network Configuration
   Please provide the network configuration for public network:
   
   Enter starting IP for VM allocation (e.g., 192.168.1.10): [用户输入起始IP]
   Enter netmask (e.g., 255.255.255.0): [用户输入子网掩码]
   Enter gateway IP (e.g., 192.168.1.1): [用户输入网关IP]
   Enter DNS server IP (e.g., 8.8.8.8 or 192.168.1.1): [用户输入DNS服务器]
   ```

3. **配置确认和VM预览**:

   ```bash
   ✅ Network configuration summary:
      ├─ Starting IP: 192.168.1.10
      ├─ Netmask: 255.255.255.0
      ├─ Gateway: 192.168.1.1
      ├─ DNS Server: 8.8.8.8
      └─ Bridge NIC: br0
   
   🖥️ Virtual Machine IP Address Preview
   The following VMs will be created with these IP addresses:
      ├─ VM 1: k8s-1 → 192.168.1.11 (Master Node)
      ├─ VM 2: k8s-2 → 192.168.1.12 (Worker Node)
      ├─ VM 3: k8s-3 → 192.168.1.13 (Worker Node)
      └─ Total: 6 VMs from 192.168.1.11 to 192.168.1.16
   ```

**输入验证**:

- **IP 地址格式验证**: 确保输入的是有效的 IPv4 地址格式
- **VM IP 范围验证**: 检查 VM IP 范围是否与现有网络冲突
- **网络配置一致性**: 验证网关、DNS 与子网的一致性
- **重试机制**: 输入错误时提供重新输入的机会

### 2. NAT 网络模式 + Host-only 网络模式

**NAT 网络模式特点**:

- VM 通过 NAT 访问外部网络
- 网络范围: `192.168.121.0/24`
- DHCP 范围: `192.168.121.10-192.168.121.254`
- 网关: `192.168.121.1`

**Host-only 网络模式特点**:

- 仅主机与 VM 之间通信
- 网络范围: `192.168.200.0/24`
- 网关: `192.168.200.1`
- DHCP: 禁用（需要静态 IP 配置）

**适用场景**:

- 开发和测试环境
- 不需要外部直接访问 VM
- 网络隔离要求

## 安装组件

脚本会自动安装和配置以下组件：

### 系统依赖

- Development Tools 组
- Git, curl, wget, vim 等基础工具
- 网络工具（bridge-utils, NetworkManager）
- 构建工具（gcc, make, autoconf 等）

### 虚拟化组件

- **libvirt**: 虚拟化管理
- **qemu-kvm**: KVM 虚拟化
- **virt-manager**: 图形化管理工具
- **libguestfs-tools**: 虚拟机镜像工具

### 开发环境

- **Vagrant**: 虚拟机管理
- **vagrant-libvirt**: libvirt 提供程序插件
- **pyenv**: Python 版本管理
- **Python 3.11.10**: 指定 Python 版本

## 使用方法

### 快速开始

#### 1. 下载并运行脚本

```bash
# 下载脚本
curl -sSL https://raw.githubusercontent.com/upmio/kubespray/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh -o "libvirt_kubespray_setup.sh"
chmod +x ./libvirt_kubespray_setup.sh
bash ./libvirt_kubespray_setup.sh
```

#### 2. 命令行选项

```bash
# 查看帮助
bash ./libvirt_kubespray_setup.sh --help

# 指定日志文件
bash ./libvirt_kubespray_setup.sh --log-file /path/to/logfile.log
```

### 代理环境配置（可选）

```bash
# 设置代理环境变量
export HTTP_PROXY="http://proxy.company.com:8080"
export HTTPS_PROXY="http://proxy.company.com:8080"
export GIT_PROXY="http://proxy.company.com:8080"
export PIP_PROXY="http://proxy.company.com:8080"
export NO_PROXY="localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16"
```

### 网络模式选择

#### 1. 桥接网络模式

根据需求选择，如果是桥接网络模式，需要配置 `BRIDGE_INTERFACE` 环境变量。

```bash
# 设置桥接接口环境变量
export BRIDGE_INTERFACE="ens33"  # 替换为实际接口名
```

**准备工作**:

在运行脚本前，建议准备以下网络信息：

- **当前网络接口的 IP 地址**: 用于安全确认
- **VM 起始 IP 地址**: 例如 `192.168.1.10`（确保有足够的连续IP用于VM分配）
- **子网掩码**: 例如 `255.255.255.0`
- **网关 IP 地址**: 例如 `192.168.1.1`
- **DNS 服务器 IP**: 例如 `8.8.8.8` 或使用网关IP

#### 2. NAT 网络模式

如果选择 NAT 网络模式，不需要配置 `BRIDGE_INTERFACE`。

### 环境变量

| 变量名 | 描述 | 默认值 | 示例 |
|--------|------|--------|------|
| `BRIDGE_INTERFACE` | 桥接网络接口 | 未设置 | `ens33` |
| `HTTP_PROXY` | HTTP 代理 | 未设置 | `http://proxy:8080` |
| `HTTPS_PROXY` | HTTPS 代理 | 未设置 | `http://proxy:8080` |
| `GIT_PROXY` | Git 代理 | 未设置 | `http://proxy:8080` |
| `PIP_PROXY` | Pip 代理 | 未设置 | `http://proxy:8080` |
| `NO_PROXY` | 代理排除列表 | 未设置 | `localhost,127.0.0.1` |
| `KUBESPRAY_DIR` | Kubespray 项目目录（固定值，不可配置） | `$(pwd)/kubespray` | 固定为 `$(pwd)/kubespray` |

### 脚本执行流程

脚本采用分阶段执行模式：

#### 阶段 1: 环境验证和预览

- 系统要求检查（CPU、内存、磁盘空间）
- 网络连通性测试
- 安装预览显示
- 用户确认

#### 阶段 2: 环境设置

- 系统依赖安装
- 虚拟化环境配置
- Python 和 Vagrant 安装
- Kubespray 项目设置

#### 阶段 3: 集群部署（可选）

- Vagrant 配置解析和显示
- 部署确认
- 自动化 Kubernetes 集群部署
- kubectl 本地配置
- 集群信息显示

### Vagrant 配置

脚本会根据网络模式自动配置 `vagrant/config.rb`：

- **桥接模式**: 使用 `public_network-config.rb` 模板
- **NAT + Host-only 模式**: 使用 `private_network-config.rb` 模板

## 安全配置

脚本会自动执行以下安全配置：

### 防火墙

- 停止并禁用 `firewalld` 服务
- 确保 VM 网络通信正常

### SELinux

- 临时禁用 SELinux (`setenforce 0`)
- 永久禁用 SELinux（修改 `/etc/selinux/config`）
- **注意**: 需要重启系统使永久配置生效

## 交互式体验

### 安装预览

脚本会在安装前显示详细预览：

```bash
🚀 Kubespray Libvirt Environment Setup

📦 Will Install:
   • Virtualization: libvirt + QEMU/KVM
   • Container: Vagrant 2.4.7 + libvirt plugin
   • Python: pyenv + Python 3.11.10

🌐 Network Setup:
   • Bridge: br0 (using interface: ens33)
   • NAT: 192.168.121.0/24 (DHCP: Enabled)
   • Host-only: 192.168.200.0/24 (DHCP: Disabled)

⚠️  System Changes:
   • Security: Firewall & SELinux disabled
   • Services: libvirtd enabled
   • User: Added to libvirt group

⏱️  Estimates: 15-25 min, ~1GB download, ~5GB disk
⚠️  Requirements: sudo access, stable internet
```

### 部署确认

环境设置完成后，脚本会显示集群配置并提供部署选项：

```bash
🚀 Kubernetes Cluster Configuration

📋 Cluster:
   • Kubernetes: 1.33.2
   • OS: rockylinux9
   • Network Plugin: calico
   • Prefix: k8s

🖥️  Nodes:
   • Masters: 1 × 4C/4GB
   • Workers: 4 × 8C/16GB
   • UPM Control: 1 × 12C/24GB

📊 Total Resources:
   • Nodes: 6
   • CPUs: 60 cores
   • Memory: 92GB
```

### 自动化部署

确认后脚本会自动执行：

1. **切换目录**: `cd $KUBESPRAY_DIR`
2. **激活环境**: `source venv/bin/activate`
3. **启动部署**: `vagrant up --provider=libvirt --no-parallel`
4. **配置 kubectl**: 自动设置本地 kubectl 访问
5. **显示集群信息**: 节点状态、命名空间、系统 Pod 等

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

- [Kubespray 官方文档](https://kubespray.io/)
- [Vagrant 文档](https://www.vagrantup.com/docs)
- [libvirt 文档](https://libvirt.org/docs.html)
- [Rocky Linux 文档](https://docs.rockylinux.org/)
- [脚本源码](https://github.com/upmio/kubespray/blob/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh)
