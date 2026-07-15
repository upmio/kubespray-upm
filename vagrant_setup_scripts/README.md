# Kubespray UPM 自动化脚本

本目录是 `kubespray-upm` 相对上游 Kubespray 的主要改造区域。它面向一台 RHEL 系 Linux 主机，通过 libvirt/KVM 创建虚拟机，由 Vagrant 调用 Kubespray 部署 Kubernetes，并可在集群上继续安装存储、监控和 UPM 组件。

详细的主机改动、网络配置、默认拓扑和故障处理请阅读 [README_libvirt.md](README_libvirt.md)。

## 文件说明

| 文件 | 实际用途 |
| --- | --- |
| `libvirt_kubespray_setup.sh` | 检查并配置宿主机，安装 libvirt、Vagrant、pyenv，创建 VM 并运行 Kubespray |
| `Vagrantfile` | 定义 VM、网络、附加磁盘、Ansible inventory 和 Kubespray 参数 |
| `vagrant-config/nat_network-config.rb` | NAT 模式的 `vagrant/config.rb` 模板 |
| `vagrant-config/bridge_network-config.rb` | Bridge 模式的 `vagrant/config.rb` 模板 |
| `containerd-example.yml` | 可选的 Kubespray containerd 配置样例 |
| `upm_setup.sh` | 在已可访问的 Kubernetes 集群中安装 OpenEBS、Prometheus 和 UPM 组件 |

## 运行模型

`libvirt_kubespray_setup.sh` 不直接在当前仓库根目录运行 Vagrant。脚本会在自己的目录下创建或更新一个部署工作区：

```text
vagrant_setup_scripts/
├── libvirt_kubespray_setup.sh
├── containerd.yml                 # 可选，由用户创建
└── kubespray-upm/                 # 脚本 clone/update 的部署工作区
    ├── Vagrantfile                # 被本目录的 Vagrantfile 覆盖
    ├── vagrant/config.rb          # 由 nat/bridge 模板生成
    ├── inventory/sample/artifacts/
    │   ├── admin.conf
    │   └── kubectl
    └── vagrant_setup_scripts/upm_setup.sh
```

因此部署完成后，`vagrant status`、`vagrant ssh`、`vagrant halt` 等命令必须在生成的 `vagrant_setup_scripts/kubespray-upm` 目录中执行。扩展组件也应使用该工作区内的 `upm_setup.sh`，这样它才能读取同一工作区的 `vagrant/config.rb`。

## 宿主机要求

脚本的实际检查条件如下：

- Linux，`/etc/os-release` 必须匹配 Red Hat、CentOS、Rocky Linux 或 AlmaLinux；RHEL 仅支持 9。
- 至少 12 个 CPU 核心；少于 12 核会终止。
- 根分区至少 200 GB 可用空间；不足会终止。
- 建议至少 32 GB 可用内存；不足只警告，但默认集群通常无法稳定运行。
- CPU 必须暴露 `vmx` 或 `svm` 虚拟化标志。
- 当前用户具有 sudo 权限，并可访问系统软件仓库、GitHub、PyPI、Vagrant Cloud 和容器镜像仓库。

运行脚本会对宿主机产生以下改动：

- 停止并禁用 firewalld。
- 将 SELinux 临时和永久设置为 disabled。
- 安装 libvirt/QEMU、Vagrant、`vagrant-libvirt`、构建依赖、pyenv 和 Python 3.12.11。
- 将当前用户加入 `libvirt` 组。
- 在 shell 启动文件中加入 pyenv 环境配置。
- 配置了代理时，会写入全局 Git proxy。

请勿在不了解这些副作用的共享宿主机或生产宿主机上直接运行。

## 快速部署

```bash
git clone https://github.com/upmio/kubespray-upm.git
cd kubespray-upm
export REPO_ROOT="$PWD"
cd "$REPO_ROOT/vagrant_setup_scripts"

# 先确认当前代码支持的参数
./libvirt_kubespray_setup.sh --help

# 默认 NAT 模式、5 台 VM；-y 自动确认普通 yes/no 提示
./libvirt_kubespray_setup.sh -y
```

下文命令沿用此处设置的 `REPO_ROOT`。重新打开 shell 后需要再次设置该变量。

可用参数以脚本为准：

| 参数 | 含义 |
| --- | --- |
| `-h`, `--help` | 显示帮助 |
| `-v`, `--version` | 显示版本 |
| `-y` | 自动确认普通 yes/no 提示 |
| `-c <3-50>` | 设置 VM 总数；只修改总数，不修改 etcd、control-plane 或 UPM 节点数 |
| `-n nat\|bridge` | 选择网络模式，默认 `nat` |
| `-p, --network-plugin calico\|cilium` | 选择 Kubernetes CNI，默认 `calico` |
| `--cilium-kube-proxy-replacement` | 使用 Cilium eBPF 替换 kube-proxy |
| `--cilium-load-balancer` | 启用 Cilium LB IPAM 和 L2 Announcement，并自动启用 kube-proxy replacement |
| `--cilium-lb-range START-STOP` | 设置 LoadBalancer 地址池，同时启用 LoadBalancer |
| `--cilium-lb-interface NAME` | L2 Announcement 使用的 guest 网卡，默认 `eth1` |

当前脚本不支持 `--nodes`、`--memory`、`--cpus`、`--disk-size`、`--k8s-version` 或组件开关。高级配置需修改生成的 `kubespray-upm/vagrant/config.rb` 后手工运行 Vagrant；重新执行主脚本会再次从模板覆盖该文件。

`-y` 不是完全无人值守模式：Bridge 网络配置和发现已有 VM 后的处理菜单仍会等待交互输入。

## 默认集群

当前 NAT/Bridge 模板的默认值一致：

| 项目 | 默认值 |
| --- | --- |
| VM 总数 | 5 |
| 节点名 | `k8s-1` 到 `k8s-5` |
| etcd | `k8s-1` |
| Kubernetes control plane | `k8s-1` |
| Kubernetes worker | 全部 5 个节点都属于 `kube_node` 组 |
| UPM 专用调度节点 | `k8s-2`，这是资源/标签约定，不是独立 Kubernetes 角色 |
| Kubernetes | 1.36.1 |
| Guest OS | Rocky Linux 9，box 为 `bento/rockylinux-9.6` |
| CNI | Calico（默认），可选 Cilium |
| cert-manager | 启用 |
| local-path-provisioner | 禁用 |

默认资源档位：`k8s-1` 为 4 CPU/4 GiB，`k8s-2` 为 4 CPU/4 GiB，其余节点为 8 CPU/16 GiB。模板还会为 `k8s-2` 到最后一个节点各添加一块 200 GiB 磁盘，并尝试将所有检测到的非根磁盘加入 `local_vg_dev`。

> 附加磁盘初始化会扫描虚拟机内全部非根磁盘并执行 LVM 初始化。请只在没有重要数据的专用虚拟机中使用。

## 网络方案

本项目存在三个相互独立的网络层次：

| 层次 | 选项 | 作用 |
| --- | --- | --- |
| VM 网络 | NAT / Bridge | 宿主机、虚拟机和物理网络之间的连接 |
| Kubernetes CNI | Calico / Cilium | Pod 网络、Service 转发和 NetworkPolicy |
| Service LoadBalancer | Cilium LB IPAM + L2 | 为 `LoadBalancer` Service 分配并通告外部 IP |

`-n` 选择 VM 网络，`-p` 选择 Kubernetes CNI，两者不能混为同一个“网络插件”。

### NAT

NAT 模式固定使用 `192.168.200.0/24`，默认 VM 地址为 `192.168.200.101` 到 `192.168.200.105`。主脚本会创建并启动名为 `nat-200-network` 的 libvirt 网络；也可以用以下命令检查：

```bash
sudo virsh net-info nat-200-network
```

如果不存在，请按 [完整指南的 NAT 网络章节](README_libvirt.md#5-nat-网络) 创建后再部署。

### Bridge

```bash
./libvirt_kubespray_setup.sh -n bridge
```

Bridge 模式会交互选择物理网卡，并在宿主机创建 `br0`、把所选网卡作为 bridge slave。当前实现不会把宿主机原有 IP 自动迁移到 `br0`，远程执行可能立即断开网络；只应通过本地控制台操作并事先准备恢复方案。

输入的起始地址按“偏移量”处理，第一台 VM 的第四段是输入值再加 1。地址生成不会跨越第四段边界，请保证所有 VM 地址都位于同一前三段且不超过 `.254`。

### Kubernetes CNI

```bash
# 默认 Calico，保留 kube-proxy
./libvirt_kubespray_setup.sh -y -p calico

# Cilium 标准模式，保留 kube-proxy
./libvirt_kubespray_setup.sh -y -p cilium

# Cilium 替换 kube-proxy
./libvirt_kubespray_setup.sh -y -p cilium \
  --cilium-kube-proxy-replacement

# Cilium LoadBalancer；地址范围必须与节点处于同一二层网络
./libvirt_kubespray_setup.sh -y -p cilium \
  --cilium-load-balancer \
  --cilium-lb-range 192.168.200.201-192.168.200.220 \
  --cilium-lb-interface eth1
```

不使用 `-y` 且未显式传入 `-p` 时，脚本会交互选择 Calico/Cilium；选择 Cilium 后还会询问是否替换 kube-proxy、是否启用 LoadBalancer。Cilium 专属参数不能与 Calico 同时使用。

Cilium L2 LoadBalancer 依赖 kube-proxy replacement；启用 LoadBalancer 时脚本会自动启用 replacement。Kubespray 会创建 `CiliumLoadBalancerIPPool`，随后主脚本校验节点 InternalIP 所在网卡并创建 `CiliumL2AnnouncementPolicy`。地址池不得与网关、VM 地址、DHCP 池、Pod CIDR、Service CIDR或其他设备地址重叠。

`START-STOP` 是 Cilium IPPool 的显式地址范围，因此不单独携带掩码。NAT 使用固定 `/24`；Bridge 模式沿用配置 VM 起始地址时输入的 CIDR/netmask，脚本按该真实掩码检查地址池是否位于节点二层子网，并排除网络地址和广播地址。

主脚本通过 Vagrant `ansible.extra_vars` 传递 `kube_owner`、`kube_proxy_remove` 和 Cilium 变量，不会直接编辑 inventory 下的 `group_vars`。如果绕过主脚本直接运行 `ansible-playbook cluster.yml`，需要自行维护 `group_vars`，并在部署后手工创建 `CiliumL2AnnouncementPolicy`。

Cilium Service LoadBalancer 只用于业务 `LoadBalancer` Service，不是 Kubernetes API Server 的负载均衡器。当前脚本保留 Kubespray 默认的本地 API proxy；不要把普通 worker 节点地址写成 `kube_apiserver_global_endpoint`。

NAT 模式的 LoadBalancer IP 通常只在宿主机和 libvirt 网络内可达；需要物理局域网其他主机访问时，应使用 Bridge，并从物理二层网段中预留地址。Service 获得 `EXTERNAL-IP` 不代表访问链路已经可用，必须从目标客户端实际连接验证。

不要通过简单的 `vagrant provision` 把已有集群从 Calico 切换到 Cilium或反向切换，也不要就地改变 Cilium 的 kube-proxy replacement、LoadBalancer 地址池或 L2 接口。这些都是集群安装级决策；主脚本检测到已有 Vagrant/libvirt VM 且参数不同会拒绝执行，需销毁旧集群后重新创建。

## 可选 containerd 配置

主脚本只查找与其同目录的 `containerd.yml`；仓库中的 `containerd-example.yml` 不会自动生效。

```bash
cd "$REPO_ROOT/vagrant_setup_scripts"
cp containerd-example.yml containerd.yml
$EDITOR containerd.yml
./libvirt_kubespray_setup.sh -y
```

发现 `containerd.yml` 后，脚本会用它覆盖部署工作区中的 `inventory/sample/group_vars/all/containerd.yml`。认证信息应使用安全的密钥管理方式，不要把真实口令提交到 Git。

## 集群管理

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"

vagrant status
vagrant ssh k8s-1
vagrant halt
vagrant up --provider=libvirt --no-parallel
vagrant provision --provision-with ansible
vagrant destroy -f
```

成功部署后，脚本会备份已有的 `$HOME/bin/kubectl` 和 `$HOME/.kube/config`，再建立到部署工作区 `inventory/sample/artifacts/` 中对应文件的符号链接。

```bash
kubectl cluster-info
kubectl get nodes -o wide
kubectl get pods -A
```

主脚本没有独立的扩容、缩容、卸载或快照子命令。修改 VM 数量或其他参数后，需要直接维护 `vagrant/config.rb` 并使用标准 Vagrant/Kubespray 流程；销毁前请确认附加磁盘中没有需要保留的数据。

## 安装 OpenEBS、Prometheus 和 UPM

请使用部署工作区中的脚本：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"

./vagrant_setup_scripts/upm_setup.sh --help
./vagrant_setup_scripts/upm_setup.sh -y --all
```

`upm_setup.sh` 在解析 `--help` 前就检查 `kubectl`，未安装 kubectl 时帮助命令也会失败。

除 `--all` 外，每次只能指定一个安装选项：

| 选项 | 行为 |
| --- | --- |
| `--lvmlocalpv` | 安装 OpenEBS LVM LocalPV 1.9.1，创建 `lvm-localpv` StorageClass |
| `--prometheus` | 安装 kube-prometheus-stack 87.10.1；Prometheus PVC 使用 `lvm-localpv`，建议先安装 LVM LocalPV |
| `--upm-engine` | 安装 UPM Engine 1.2.4 |
| `--upm-platform` | 安装 UPM Platform 1.2.4；会检查 LVM LocalPV 和 StorageClass |
| `--config_nginx` | 在宿主机安装并覆盖配置 Nginx，将 UPM UI/API 暴露到 80 端口 |
| `--all` | 依次安装 LVM LocalPV、Prometheus、UPM Engine、UPM Platform；不包含 Nginx |

不要组合多个普通安装选项，例如 `--lvmlocalpv --prometheus` 会被脚本拒绝。需要分步安装时逐条执行。

UPM Platform 默认通过 `<节点IP>:32010/upm-ui/#/login` 访问，默认用户为 `super_root`。默认口令来自环境变量 `UPM_PWD`，未设置时为 `Upm@2024!`；部署前应显式设置强口令。该安装还会将 `upm-system` 命名空间的 `default` ServiceAccount 绑定到 `cluster-admin`，请在非实验环境中重新评估并收紧权限。

## 日志与验证

```bash
# 宿主机部署日志
tail -f "$REPO_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.log"

# 扩展组件日志
tail -f "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm/vagrant_setup_scripts/upm_setup.log"

# 虚拟机与集群
sudo virsh list --all
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm" && vagrant status
kubectl get nodes -o wide
kubectl get pods -A
```

组件安装后至少检查 Helm release、Pod、StorageClass 和服务是否符合预期，不能只以脚本退出码作为完成标准。

## 已知实现限制

- NAT 模式由主脚本自动创建并启动 `nat-200-network`；如果同名网络已存在但不是 `192.168.200.0/24`，脚本会拒绝继续。
- Bridge 配置可能中断宿主机网络，且 guest 网络配置假设 Rocky/Alma 的第二连接名为 `System eth1`。
- `-y` 不能跳过所有交互。
- 主脚本会创建嵌套部署工作区；外层脚本与内层配置不能混用。
- VM worker 资源当前以模板中的 8 CPU/16 GiB 为准，脚本显示的自适应资源值可能与实际配置不一致。
- libvirt 附加磁盘文件目录配置当前未被实际使用。
- `--all` 不包含 Nginx；`upm_setup.sh` 不接受多个普通组件选项。

更完整的操作步骤和故障处理见 [README_libvirt.md](README_libvirt.md)。
