# Kubespray Vagrant 自动化部署脚本 (Libvirt)

本项目提供了一套完整的自动化脚本，用于在 Libvirt 虚拟化平台上快速部署 Kubespray Kubernetes 集群。Libvirt 是 Linux 系统上的原生虚拟化解决方案，提供最佳的性能和资源效率。

## 🚀 为什么选择 Libvirt

- **最佳性能**: 原生 Linux 虚拟化，性能最优
- **资源效率**: 内存和 CPU 开销最小
- **企业级**: 生产环境首选，稳定可靠
- **开源免费**: 完全开源，无需许可证费用
- **广泛支持**: 支持多种 Linux 发行版
- **丰富功能**: 支持快照、克隆、迁移等高级功能

## 📋 系统要求

### 硬件要求

- **CPU**: 8 核心或更多 (支持虚拟化扩展)
- **内存**: 16GB RAM 或更多 (推荐 32GB+)
- **存储**: 50GB 可用磁盘空间或更多
- **网络**: 稳定的互联网连接

### 软件要求

- **操作系统**: Linux (Ubuntu 22.04+, CentOS 9+, RHEL 9+, Fedora 30+)
- **Vagrant**: 2.4.0 或更高版本
- **Libvirt**: 最新稳定版本
- **QEMU/KVM**: 支持硬件虚拟化
- **Git**: 用于克隆项目

### 权限要求

- 用户需要加入 `libvirt` 用户组
- 具有 sudo 权限以安装依赖包

## 🚀 快速开始

### 1. 环境准备

最小化安装系统即可，不需要安装桌面环境

### 2. 克隆项目

```bash
git clone <repository-url>
cd kubespray-upm/vagrant_setup_scripts
```

### 3. 配置和部署

```bash
# 使用默认配置快速部署
./libvirt_kubespray_setup.sh

# 自定义配置示例
./libvirt_kubespray_setup.sh \
  --nodes 5 \
  --memory 4096 \
  --cpus 2 \
  --disk-size 50 \
  --network-mode bridge \
  --k8s-version v1.28.2
```

## 📖 详细配置说明

### 脚本参数

#### 基础选项

- `--nodes, -n`: 节点数量 (1-10，默认: 3)
- `--memory, -m`: 每个节点内存 MB (默认: 2048)
- `--cpus, -c`: 每个节点 CPU 核数 (默认: 2)
- `--disk-size, -d`: 磁盘大小 GB (默认: 20)
- `--network-mode`: 网络模式 (nat/bridge，默认: nat)
- `--k8s-version`: Kubernetes 版本 (默认: v1.28.2)
- `--help, -h`: 显示帮助信息

#### 功能选项

- `--enable-dashboard`: 启用 Kubernetes Dashboard
- `--enable-ingress`: 启用 Ingress Controller
- `--enable-metrics`: 启用 Metrics Server
- `--enable-cert-manager`: 启用 Cert Manager
- `--enable-local-path`: 启用 Local Path Provisioner
- `--skip-upm`: 跳过 UPM 组件安装

### 网络配置

#### NAT 模式 (默认)

```bash
./libvirt_kubespray_setup.sh --network-mode nat
```

- 虚拟机通过 NAT 访问外网
- 主机可通过端口转发访问服务
- 适合开发和测试环境

#### 桥接模式
```bash
./libvirt_kubespray_setup.sh --network-mode bridge
```
- 虚拟机获得真实网络 IP
- 网络中其他设备可直接访问
- 适合生产环境模拟

## 🔧 脚本功能特性

### 自动化部署
- **一键部署**: 自动创建和配置虚拟机
- **Kubernetes 集群**: 使用 Kubespray 自动安装
- **网络配置**: 支持 NAT 和桥接网络模式
- **存储管理**: 自动配置持久化存储

### 虚拟机管理
- **多节点支持**: 支持 1-10 个节点的集群
- **资源配置**: 可自定义 CPU、内存、磁盘
- **快照管理**: 支持虚拟机快照创建和恢复
- **批量操作**: 支持批量启动、停止、删除

### 容器镜像仓库配置
通过 `containerd-example.yml` 文件可以：
- 配置私有镜像仓库
- 设置镜像加速器
- 配置认证信息
- 支持多个镜像源

### UPM 平台集成
通过 `upm_setup.sh` 脚本可以在 Kubernetes 集群上自动安装：
- **存储组件**: Longhorn, OpenEBS, Rook-Ceph
- **监控组件**: Prometheus, Grafana, AlertManager
- **数据库**: PostgreSQL, MySQL, Redis, MongoDB
- **平台管理**: UPM Dashboard, 用户管理, 权限控制

## 🖥️ 虚拟机管理

### Vagrant 命令
```bash
# 查看虚拟机状态
vagrant status

# 启动所有虚拟机
vagrant up

# 停止所有虚拟机
vagrant halt

# 重启虚拟机
vagrant reload

# 删除虚拟机
vagrant destroy

# SSH 连接到节点
vagrant ssh k8s-1  # 连接到第一个节点
```

### virsh 命令
```bash
# 列出所有虚拟机
virsh list --all

# 启动虚拟机
virsh start kubespray_k8s-1

# 停止虚拟机
virsh shutdown kubespray_k8s-1

# 强制停止虚拟机
virsh destroy kubespray_k8s-1

# 删除虚拟机
virsh undefine kubespray_k8s-1 --remove-all-storage
```

## 🔑 集群访问和管理

### kubectl 本地访问
```bash
# 复制 kubeconfig 文件
mkdir -p ~/.kube
vagrant ssh k8s-1 -c "sudo cat /etc/kubernetes/admin.conf" > ~/.kube/config

# 验证集群连接
kubectl get nodes
kubectl get pods --all-namespaces
```

### 基础组件管理
```bash
# 查看集群信息
kubectl cluster-info

# 查看节点详情
kubectl describe nodes

# 查看系统 Pod
kubectl get pods -n kube-system

# 查看服务
kubectl get svc --all-namespaces
```

## 🛠️ 故障排除

### 常见问题

#### 1. 虚拟机启动失败
```bash
# 检查 libvirt 服务状态
sudo systemctl status libvirtd

# 重启 libvirt 服务
sudo systemctl restart libvirtd

# 检查网络配置
virsh net-list --all
virsh net-start default
```

#### 2. 网络连接问题
```bash
# 检查虚拟机网络
vagrant ssh k8s-1 -c "ip addr show"

# 测试网络连通性
vagrant ssh k8s-1 -c "ping -c 3 8.8.8.8"

# 检查 DNS 解析
vagrant ssh k8s-1 -c "nslookup kubernetes.default.svc.cluster.local"
```

#### 3. Kubernetes 组件问题
```bash
# 检查 kubelet 状态
vagrant ssh k8s-1 -c "sudo systemctl status kubelet"

# 查看 kubelet 日志
vagrant ssh k8s-1 -c "sudo journalctl -u kubelet -f"

# 检查容器运行时
vagrant ssh k8s-1 -c "sudo crictl ps"
```

### 调试和日志
```bash
# 查看 Vagrant 详细日志
VAGRANT_LOG=info vagrant up

# 查看 Ansible 执行日志
vagrant ssh k8s-1 -c "sudo cat /tmp/kubespray.log"

# 检查系统资源使用
vagrant ssh k8s-1 -c "free -h && df -h"
```

## 📚 相关文档

- [容器镜像仓库配置](containerd-example.yml) - 镜像仓库设置
- [UPM 平台安装](upm_setup.sh) - 平台组件部署
- [Vagrant 配置文件](Vagrantfile) - 虚拟机配置

## 📄 许可证

本项目采用 MIT 许可证，允许自由使用、修改和分发。

---

## 下一步

选择适合您环境的虚拟化方案，点击对应的详细文档开始部署：

- 🐧 **Linux 用户**: [Libvirt 部署指南](./README_libvirt.md)


祝您部署愉快！🚀
