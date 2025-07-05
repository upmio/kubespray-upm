# Kubespray Vagrant 虚拟化环境快速部署指南

## 项目概述

本项目提供了使用 Vagrant 和不同虚拟化技术快速部署 Kubernetes 集群的自动化脚本。通过这些脚本，您可以在几分钟内搭建一个完整的 Kubernetes 开发或测试环境。

## 支持的虚拟化平台

### 🖥️ Libvirt (推荐 Linux 用户)

**适用场景**:
- Linux 开发环境
- 服务器环境部署
- 高性能虚拟化需求
- 生产环境测试

**优势**:
- ✅ 原生 Linux 虚拟化，性能优异
- ✅ 支持桥接网络，VM 可获得真实 IP
- ✅ 资源利用率高
- ✅ 免费开源
- ✅ 支持大规模集群部署

**系统要求**:
- 操作系统：Rocky Linux 9（推荐）
- CPU：8+ 核心
- 内存：16GB+
- 磁盘：50GB+

📖 **详细文档**: [Libvirt 部署指南](./README_libvirt.md)

---

### 🪟 VirtualBox (推荐 Linux 用户)

**适用场景**:
- Linux 开发环境
- 桌面虚拟化
- 学习和实验
- RHEL 系列系统

**优势**:
- ✅ 专为 Linux 环境优化
- ✅ 图形化管理界面
- ✅ 易于安装和配置
- ✅ 免费使用
- ✅ 社区支持丰富

**系统要求**:
- 操作系统：RHEL, CentOS, Rocky Linux, AlmaLinux
- CPU：4+ 核心（支持虚拟化）
- 内存：8GB+
- 磁盘：30GB+

📖 **详细文档**: [VirtualBox 部署指南](./README_virtualbox.md)

---

### 🖥️ Parallels Desktop (Apple Silicon Mac 专用)

**适用场景**:
- Apple Silicon Mac 开发环境
- ARM 原生虚拟化
- macOS 专业开发
- 高性能 ARM64 环境

**优势**:
- ✅ ARM64 原生性能，无需模拟
- ✅ 完美集成 macOS 系统
- ✅ 优秀的启动和运行速度
- ✅ 简单易用的管理界面
- ✅ 良好的网络虚拟化支持

**系统要求**:
- 操作系统：macOS (Apple Silicon)
- CPU：Apple M1/M2/M3 系列
- 内存：16GB+
- 磁盘：100GB+
- 许可证：**Parallels Desktop Business Edition**

**重要提醒**:
- ⚠️ **必须使用 Business Edition**
- ⚠️ **Pro Edition 和标准版不支持 Vagrant 插件**
- ⚠️ **需要有效的商业许可证**

📖 **详细文档**: [Parallels 部署指南](./README_parallels.md)

## 快速选择指南

### 🤔 我应该选择哪种虚拟化方式？

| 场景 | 推荐方案 | 原因 |
|------|----------|------|
| **Linux 服务器** | Libvirt | 原生支持，性能最佳 |
| **RHEL 系列 Linux** | VirtualBox | 免费，图形化管理 |
| **Apple Silicon Mac** | Parallels Desktop | ARM64 原生性能 |
| **企业环境** | Libvirt | 高性能，企业级稳定性 |
| **学习测试** | VirtualBox | 免费，资源要求低 |
| **生产模拟** | Libvirt | 高性能，真实网络环境 |

### 📊 性能对比

| 特性 | Libvirt | VirtualBox | Parallels |
|------|---------|------------|----------|
| **性能** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **易用性** | ⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐ |
| **跨平台** | ⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| **免费使用** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | ⭐⭐ |
| **网络功能** | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | ⭐⭐⭐⭐ |
| **ARM 支持** | ⭐⭐ | ⭐ | ⭐⭐⭐⭐⭐ |

## 通用前置要求

### 硬件要求
- **CPU**: 支持硬件虚拟化（Intel VT-x 或 AMD-V）
- **内存**: 最少 8GB（推荐 16GB+）
- **磁盘**: 最少 30GB 可用空间
- **网络**: 稳定的互联网连接

### 软件要求

#### 通用软件
- **Vagrant**: 2.4.0+
- **Git**: 用于克隆项目
- **SSH 客户端**: 用于连接虚拟机
- **Python**: 3.8+ (用于 Ansible)
- **Ansible**: 2.12+ (通过脚本自动安装)

#### 虚拟化平台软件

**Libvirt (Linux)**:
- `libvirt-daemon-system`
- `libvirt-clients`
- `qemu-kvm`
- `vagrant-libvirt` 插件

**VirtualBox (Linux)**:
- VirtualBox 7.1+
- VirtualBox Extension Pack (可选)
- 仅支持 RHEL 系列发行版

**Parallels Desktop (macOS)**:
- Parallels Desktop Business Edition
- `vagrant-parallels` 插件
- ⚠️ **注意**: Pro Edition 和标准版不支持 Vagrant

## 扩展功能

### 监控和日志
- Prometheus + Grafana
- ELK Stack
- Jaeger 链路追踪

### 存储解决方案
- Ceph 分布式存储
- NFS 共享存储
- 本地持久卷

### 网络插件
- Calico（默认）
- Flannel
- Cilium
- Weave Net

## 社区和支持

### 官方资源
- [Kubespray 官方文档](https://kubespray.io/)
- [Kubernetes 官方文档](https://kubernetes.io/docs/)
- [Vagrant 官方文档](https://www.vagrantup.com/docs)

### 社区支持
- [GitHub Issues](https://github.com/kubernetes-sigs/kubespray/issues)
- [Kubernetes Slack](https://kubernetes.slack.com/)
- [Stack Overflow](https://stackoverflow.com/questions/tagged/kubespray)

### 贡献指南
- [贡献代码](../CONTRIBUTING.md)
- [报告问题](https://github.com/kubernetes-sigs/kubespray/issues/new)
- [功能请求](https://github.com/kubernetes-sigs/kubespray/issues/new)

## 许可证

本项目遵循 [Apache 2.0 许可证](../LICENSE)。

---

## 下一步

选择适合您环境的虚拟化方案，点击对应的详细文档开始部署：

- 🐧 **Linux 用户**: [Libvirt 部署指南](./README_libvirt.md)
- 🐧 **RHEL 系列 Linux 用户**: [VirtualBox 部署指南](./README_virtualbox.md)
- 🍎 **Apple Silicon Mac 用户**: [Parallels 部署指南](./README_parallels.md)

祝您部署愉快！🚀
