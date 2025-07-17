# Kubespray Libvirt 环境设置指南

## 概述

本文档详细介绍如何使用 `libvirt_kubespray_setup.sh` 脚本在 libvirt 虚拟化环境中快速部署 Kubespray Kubernetes 集群。该脚本专为 Red Hat 系列 Linux 系统（RHEL 9、Rocky Linux 9、CentOS 9、AlmaLinux 9）设计，提供完整的自动化环境配置、交互式部署体验以及企业级容器镜像仓库配置支持。

### 脚本架构特点

- **模块化设计**: 采用功能模块化架构，支持独立安装和配置各个组件
- **智能检测**: 自动检测系统环境、网络配置和虚拟机状态
- **跨平台兼容**: 支持多种 RHEL 系发行版，自动适配不同系统特性

### 脚本特性

- **版本**: v1.0
- **模块化安装**: 支持选择性安装不同组件（K8s、LVM LocalPV、Prometheus、CloudNativePG、UPM Engine、UPM Platform）
- **智能系统检测**: 自动检测操作系统类型、硬件资源和虚拟化支持
- **网络配置管理**: 自动检测和配置网络模式（NAT/桥接），支持智能网络参数配置
- **虚拟机生命周期管理**: 提供完整的虚拟机创建、更新、销毁和状态管理功能
- **交互式配置**: 提供详细的安装预览和确认机制
- **错误处理**: 完善的错误处理和恢复机制
- **安全特性**: 交互式确认、权限验证、RHEL订阅验证、网络安全检查

### ⚡ 一键命令

如果您希望快速体验，可以使用以下一键命令：

### 下载脚本并安装 Kubernetes 集群（NAT 模式）

```bash
curl -sSL https://raw.githubusercontent.com/upmio/kubespray-upm/refs/heads/master/vagrant_setup_scripts/libvirt_kubespray_setup.sh -o ./libvirt_kubespray_setup.sh && chmod +x ./libvirt_kubespray_setup.sh && bash ./libvirt_kubespray_setup.sh --k8s -y
```

## 系统要求

### 硬件要求

| 组件 | 最低要求 | 推荐配置 | 说明 |
|------|----------|----------|------|
| **CPU** | 12 核心 | 24+ 核心 | 支持硬件虚拟化 (Intel VT-x/AMD-V) |
| **内存** | 32 GB | 64 GB+ | 用于主机系统和虚拟机 |
| **磁盘空间** | 200 GB | 500 GB+ | SSD 推荐，用于虚拟机镜像和数据 |
| **网络** | 1 Gbps | 10 Gbps | 稳定的网络连接 |

### 软件要求

#### 支持的操作系统

脚本会自动检测以下 RHEL 系发行版：

- **Red Hat Enterprise Linux (RHEL)** 9.x
- **Rocky Linux** 9.x
- **CentOS Stream** 9.x
- **AlmaLinux** 9.x

> **重要**: `--all` 和 `--config_nginx` 选项仅支持 Linux 系统

#### 系统组件要求

- **内核版本**: 5.14+ (支持 KVM 虚拟化)
- **Python**: 3.9+ (系统自带)
- **Bash**: 4.0+ (系统自带)
- **硬件虚拟化**: CPU 支持 Intel VT-x 或 AMD-V
- **嵌套虚拟化**: 如果在虚拟机中运行需要启用

#### 网络要求

- **互联网访问**: 用于下载软件包和容器镜像
- **DNS 解析**: 正常的域名解析功能
- **代理支持**: 支持 HTTP/HTTPS 代理环境（可选）
- **防火墙**: 脚本会自动配置防火墙规则

#### 用户权限要求

- **sudo 权限**: 当前用户必须具有 sudo 权限
- **用户组**: 脚本会自动将用户添加到 libvirt 和 kvm 组

### 系统检查功能

脚本内置以下自动检查功能：

#### 操作系统检测

- 自动检测 RHEL 系发行版类型和版本
- 验证系统是否为 Linux（针对特定选项）
- 检查系统架构兼容性

#### 硬件资源检查

- **CPU 核心数**: 最少 12 核心
- **内存容量**: 最少 32GB
- **磁盘空间**: 最少 200GB 可用空间
- **虚拟化支持**: 检查 KVM 和硬件虚拟化功能

### 磁盘空间分布建议

| 目录 | 用途 | 最低要求 | 推荐配置 |
|------|------|----------|----------|
| `/` | 系统根目录 | 50 GB | 100 GB |
| `/var` | 容器镜像和日志 | 100 GB | 200 GB |
| `/home` | 用户数据和项目文件 | 50 GB | 100 GB |
| `/tmp` | 临时文件 | 10 GB | 20 GB |

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

#### 用户和权限配置

- **sudo 权限**: 当前用户必须具有 sudo 权限
- **用户组**: 脚本会自动将用户添加到 libvirt 组
- **文件权限**: 用户主目录必须可写（用于存储配置文件和密钥）

#### 磁盘空间分布

- **根分区 (/)**: 至少 50GB 可用空间（用于系统软件和工具）
- **用户主目录**: 至少 20GB 可用空间（用于 kubespray 项目和配置）
- **虚拟机存储**: 至少 200GB 可用空间（默认位置：/var/lib/libvirt/images）

#### RHEL 系统额外配置要求

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

| 参数 | 长选项 | 描述 | 系统要求 |
|------|--------|------|----------|
| `-h` | `--help` | 显示帮助信息 | 所有系统 |
| `-v` | `--version` | 显示详细版本信息 | 所有系统 |
| | `--version-short` | 显示简要版本信息 | 所有系统 |
| | `--version-changelog` | 显示版本更新日志 | 所有系统 |
| `-y` | | 自动确认所有是/否提示（网络桥接配置除外） | 所有系统 |
| `-n <network_type>` | | 设置网络类型（nat\|bridge，默认：nat）。仅在使用 `--k8s` 或完整安装模式时有效。设置为 'bridge' 时需要交互式配置 | 所有系统 |

### 安装选项（必须指定其中一个）

| 选项 | 描述 | 安装时间 | 要求 | 系统要求 |
|------|------|----------|------|----------|
| `--k8s` | 仅安装 Kubernetes 集群环境 | ~15 分钟 | 基础系统要求 | 所有支持的系统 |
| `--lvmlocalpv` | 仅安装 OpenEBS LVM LocalPV 存储解决方案 | ~3 分钟 | 已有 K8s 集群 + Helm 3.x | 所有支持的系统 |
| `--cnpg` | 仅安装 CloudNative-PG PostgreSQL 数据库 | ~5 分钟 | 已有 K8s 集群 + Helm 3.x | 所有支持的系统 |
| `--upm-engine` | 仅安装 UPM Engine 管理组件 | ~5 分钟 | 已有 K8s 集群 + Helm 3.x | 所有支持的系统 |
| `--upm-platform` | 仅安装 UPM Platform 平台界面 | ~3 分钟 | 已有 K8s 集群 + Helm 3.x | 所有支持的系统 |
| `--prometheus` | 仅安装 Prometheus 监控和告警系统 | ~8 分钟 | 已有 K8s 集群 + Helm 3.x | 所有支持的系统 |
| `--all` | 安装所有组件（K8s + 存储 + 数据库 + 监控 + UPM） | ~25 分钟 | 基础系统要求 | **仅限 Linux 系统** |
| `--config_nginx` | 配置 Nginx 反向代理 | ~2 分钟 | 已有 K8s 集群 | **仅限 Linux 系统** |

**重要提示：** 必须指定且仅能指定一个安装选项。`--all` 和 `--config_nginx` 选项仅支持 Linux 系统。

### 虚拟机管理选项（可选）

| 选项 | 描述 | 要求 | 说明 |
|------|------|------|------|
| `--status` | 查看虚拟机状态和基本信息 | 已部署的虚拟机 | 显示所有 VM 的运行状态 |
| `--ssh <node_name>` | SSH 连接到指定节点（如 k8s-1, k8s-2） | 已部署且运行的虚拟机 | 支持交互式选择节点 |
| `--destroy` | 销毁所有 kubespray 虚拟机 | 已部署的虚拟机 | 不可恢复操作，需确认 |
| `--halt` | 停止所有 kubespray 虚拟机 | 已部署且运行的虚拟机 | 保留 VM 配置和数据 |
| `--up` | 启动所有 kubespray 虚拟机 | 已部署但停止的虚拟机 | 启动已停止的 VM |

**注意事项：** 虚拟机管理选项不能与安装选项同时使用。

### 详细的安装选项要求

#### `--k8s` - Kubernetes 集群安装

**功能描述**：

- 自动配置 libvirt 虚拟化环境
- 部署基础 Kubernetes 集群（使用 Kubespray）
- 配置网络（支持 NAT 和桥接模式）
- 安装必要的系统组件和依赖

**系统要求**：

- 满足基础硬件要求（12+ CPU 核心，32+ GB 内存）
- RHEL 系发行版（RHEL/Rocky/CentOS/AlmaLinux 9.x）
- 网络连接正常，支持代理配置
- 用户具有 sudo 权限

**安装内容**：

- libvirt 虚拟化环境和相关工具
- Vagrant 和 vagrant-libvirt 插件
- Kubespray 项目和 Python 虚拟环境
- Kubernetes 集群（默认 1 master + 4 worker 节点）
- 网络配置（Calico CNI）
- 基础存储类和服务配置

#### `--lvmlocalpv` - LVM LocalPV 存储

**功能描述**：

- 安装 OpenEBS LVM LocalPV 存储驱动
- 配置动态存储供应和存储类
- 提供高性能本地存储解决方案

**前置要求**：

- 已有运行的 Kubernetes 集群
- Helm 3.x 已安装并配置
- 集群节点支持 LVM 和有可用磁盘空间
- 节点已安装 LVM2 工具

**安装内容**：

- OpenEBS LVM LocalPV Operator
- 默认存储类配置（openebs-lvmpv）
- 节点标签和污点配置
- LVM 卷组管理工具

#### `--cnpg` - CloudNativePG 数据库

**功能描述**：

- 安装 CloudNative-PG PostgreSQL Operator
- 提供云原生 PostgreSQL 数据库服务
- 支持高可用、自动故障转移和备份恢复

**前置要求**：

- 已有运行的 Kubernetes 集群
- Helm 3.x 已安装并配置
- 推荐配置持久化存储类
- 足够的计算和存储资源

**安装内容**：

- CloudNative-PG Operator（最新稳定版）
- PostgreSQL 集群 CRD 定义
- 默认配置模板和示例
- 备份和恢复策略配置

#### `--upm-engine` - UPM Engine

**功能描述**：

- 安装 UPM (Unified Platform Management) Engine
- 提供统一的平台管理和编排能力
- 支持多集群管理和资源调度

**前置要求**：

- 已有运行的 Kubernetes 集群
- Helm 3.x 已安装并配置
- 网络连接正常，支持镜像拉取
- 推荐配置监控和存储组件

**安装内容**：

- UPM Engine 核心组件
- 管理 API 服务
- 资源调度器
- 集群管理界面

#### `--upm-platform` - UPM Platform

**功能描述**：

- 安装 UPM Platform 用户界面
- 提供 Web 管理控制台和仪表板
- 集成监控、日志和管理功能

**前置要求**：

- 已有运行的 Kubernetes 集群
- Helm 3.x 已安装并配置
- 推荐先安装 UPM Engine
- 网络访问和负载均衡配置

**安装内容**：

- UPM Platform Web 界面
- 用户认证和授权服务
- 管理 API 和代理服务
- 集成监控仪表板

#### `--prometheus` - Prometheus 监控

**功能描述**：

- 安装完整的 Prometheus 监控栈
- 包含 Grafana 可视化和 AlertManager 告警
- 提供集群和应用级别的监控能力

**前置要求**：

- 已有运行的 Kubernetes 集群
- Helm 3.x 已安装并配置
- 推荐配置持久化存储类
- 足够的计算资源用于监控组件

**安装内容**：

- Prometheus Server 和时序数据库
- Grafana 仪表板和可视化
- AlertManager 告警管理
- Node Exporter 和 kube-state-metrics
- 预配置的监控规则和仪表板

#### `--config_nginx` - Nginx 配置 (仅限 Linux)

**功能描述**：

- 配置 Nginx 反向代理服务
- 提供负载均衡和 SSL 终止
- 支持多服务路由和访问控制

**前置要求**：

- 已有运行的 Kubernetes 集群
- Linux 操作系统（脚本会自动检查）
- 网络配置和域名解析

**安装内容**：

- Nginx 配置文件
- 反向代理规则
- SSL 证书配置
- 访问日志和监控配置

#### `--all` - 完整安装 (仅限 Linux)

**功能描述**：

- 一次性安装所有组件和服务
- 提供完整的企业级 Kubernetes 生态系统
- 适合生产环境和开发测试环境

**系统要求**：

- Linux 操作系统（脚本会自动检查）
- 满足所有组件的硬件要求（推荐 24+ CPU 核心，64+ GB 内存）
- 充足的网络带宽和稳定连接
- 足够的磁盘空间（推荐 500+ GB）

**安装内容**：

- 包含上述所有组件（除 --config_nginx）
- 完整的监控、存储和数据库解决方案
- 企业级管理平台和用户界面
- 预配置的集成和优化设置

## 网络配置选项

脚本支持两种网络配置模式，通过 `-n` 参数指定网络类型：

### NAT 网络模式（默认）

```bash
bash ./libvirt_kubespray_setup.sh --k8s -n nat
```

- **隔离安全**: 虚拟机网络与宿主机网络隔离
- **自动配置**: 无需手动配置网络参数
- **适用场景**: 开发测试环境、安全隔离环境

### 桥接网络模式

```bash
bash ./libvirt_kubespray_setup.sh --k8s -n bridge
```

- **直接访问**: 虚拟机获得真实网络IP，可被外部直接访问
- **交互配置**: 需要手动配置网络参数
- **适用场景**: 生产环境、需要外部访问的场景
- **⚠️ 警告**: 配置过程可能导致SSH连接中断，建议本地执行

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
bash ./libvirt_kubespray_setup.sh --all -y -n nat            # NAT 模式（默认）
bash ./libvirt_kubespray_setup.sh --all -y -n bridge         # 桥接模式


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

### 镜像仓库配置说明

在企业环境中，通常需要配置容器镜像仓库转发以提高镜像拉取速度或使用私有镜像仓库。本脚本支持通过 containerd 配置文件自定义镜像仓库设置。

### 配置文件说明

脚本提供了 `containerd-example.yml` 样例文件，展示了如何配置 containerd 镜像仓库转发。该文件位于：

```bash
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
kubectl get pods -n upm-system

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

```bash
# 进入工作目录
cd $KUBESPRAY_DIR

# 基本操作
vagrant status          # 查看状态
vagrant up             # 启动集群
vagrant halt           # 停止集群
vagrant destroy -f     # 销毁集群
vagrant ssh k8s-1      # SSH连接主节点
```

## 故障排除

### 常见问题

#### 1. 网络连接失败

```bash
# 检查网络和代理
curl -I https://github.com
echo $HTTP_PROXY

# 配置代理（如需要）
export HTTP_PROXY="http://proxy.company.com:8080"
export HTTPS_PROXY="http://proxy.company.com:8080"
```

#### 2. libvirt 服务问题

```bash
# 检查和启动服务
sudo systemctl status libvirtd
sudo systemctl start libvirtd
sudo systemctl enable libvirtd

# 启动默认网络
sudo virsh net-start default
```

#### 3. Vagrant 插件安装失败

```bash
# 安装开发依赖
sudo dnf groupinstall "Development Tools" -y
sudo dnf install libvirt-devel ruby-devel -y

# 重新安装插件
vagrant plugin uninstall vagrant-libvirt
vagrant plugin install vagrant-libvirt
```

#### 4. 桥接网络配置失败

```bash
# 检查网络状态
ip link show
nmcli device status

# 重启网络服务
sudo systemctl restart NetworkManager
sudo firewall-cmd --add-service=libvirt --permanent
sudo firewall-cmd --reload

# 重启libvirt网络
sudo virsh net-destroy default
sudo virsh net-start default
```

#### 5. RHEL 系统特定问题

```bash
# 检查订阅状态
subscription-manager status

# 重新注册和附加订阅
sudo subscription-manager register --username=<用户名> --password=<密码>
sudo subscription-manager attach --auto

# 启用必需仓库
sudo subscription-manager repos --enable=rhel-9-for-x86_64-baseos-rpms
sudo subscription-manager repos --enable=rhel-9-for-x86_64-appstream-rpms
sudo subscription-manager repos --enable=codeready-builder-for-rhel-9-x86_64-rpms

# 清理缓存
sudo dnf clean all && sudo dnf makecache
```

### 调试和日志

```bash
# 启用调试模式
bash -x ./libvirt_kubespray_setup.sh --k8s

# 查看日志
tail -f /tmp/libvirt_kubespray_setup.log
sudo journalctl -u libvirtd -f

# 检查虚拟机状态
cd $KUBESPRAY_DIR && vagrant status
virsh list --all
virsh net-list --all
```

## 注意事项

### 重要警告

- **桥接网络风险**: 配置桥接网络可能导致SSH连接中断，建议本地执行
- **资源要求**: CPU 12+核心，内存 32GB+，磁盘 200GB+
- **RHEL 订阅**: RHEL 系统需要有效订阅和启用必需仓库
- **权限要求**: 需要sudo权限，添加用户组后需重新登录
- **安全配置**: 脚本会禁用防火墙和SELinux，生产环境需重新配置

## 支持的配置

### 默认集群配置

- **Kubernetes**: v1.33.2
- **操作系统**: Rocky Linux 9
- **网络插件**: Calico
- **节点配置**: 1个Master + 1个UPM Control + 3个Worker
- **总资源**: 40 CPU核心, 74GB 内存
- **配置文件**: `$KUBESPRAY_DIR/config.rb`

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
