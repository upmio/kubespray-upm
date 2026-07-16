# Kubespray UPM Libvirt 部署与运维指南

本文档按 `libvirt_kubespray_setup.sh`、`Vagrantfile`、网络模板和 `upm_setup.sh` 的当前实现编写。命令和限制均以代码实际行为为准。

## 1. 能力边界

主脚本完成以下工作：

1. 检查 RHEL 系宿主机、CPU、内存、磁盘、虚拟化和网络。
2. 修改宿主机安全配置，安装并启动 libvirt/QEMU。
3. 安装 Vagrant、`vagrant-libvirt`、pyenv、Python 3.12.11 和 Kubespray Python 依赖。
4. 在脚本目录下 clone/update 一个嵌套的 `kubespray-upm` 部署工作区。
5. 从 NAT 或 Bridge 模板生成 `vagrant/config.rb`，并替换工作区根目录的 `Vagrantfile`。
6. 执行 `vagrant up --provider=libvirt --no-parallel --provision`，由 Vagrant 调用 Kubespray。
7. 将生成的 kubectl 和 kubeconfig 链接到宿主机 `$HOME/bin/kubectl` 与 `$HOME/.kube/config`。

主脚本没有实现独立的 start、stop、scale、snapshot 或 uninstall 子命令。部署后的生命周期管理使用 Vagrant、virsh、kubectl 和 Kubespray 原生命令。

## 2. 重要风险

运行前必须理解以下宿主机级改动：

- firewalld 会被停止并禁用。
- SELinux 会执行 `setenforce 0`，并把 `/etc/selinux/config` 改为 disabled。
- 当前用户会加入 `libvirt` 组。
- shell 启动文件会加入 pyenv 配置。
- 如果 `$HOME/.pyenv` 已存在但 `pyenv` 命令不可用，安装流程会删除该目录后重新安装。
- 设置代理时会修改全局 Git proxy。
- 两个脚本会把各自日志文件权限设置为 `0666`，日志中可能包含代理、地址和拓扑信息。
- Bridge 模式会创建 `br0` 并把选定物理接口加入 bridge，可能导致当前 SSH 会话断开。
- 默认附加磁盘初始化会在 guest 内扫描全部非根磁盘并执行 `pvcreate`/`vgcreate`/`vgextend`，不能用于包含重要数据的磁盘。
- UPM Platform 安装会给 `upm-system/default` ServiceAccount 授予 `cluster-admin`。
- `--config_nginx` 会备份后完整覆盖宿主机 `/etc/nginx/nginx.conf`。

这套流程定位于专用实验、开发或验证宿主机，不应未经评审直接用于共享或生产宿主机。

## 3. 宿主机准备

### 3.1 操作系统和硬件

实际检查条件：

| 项目 | 当前实现 |
| --- | --- |
| 操作系统 | `/etc/os-release` 匹配 Red Hat、CentOS、Rocky 或 AlmaLinux；RHEL 仅允许 9 |
| CPU | 至少 12 核，否则终止 |
| 虚拟化 | `/proc/cpuinfo` 必须存在 `vmx` 或 `svm` |
| 内存 | 默认 5 VM 需要 32 GiB guest 内存；新建集群必须额外保留 20% `MemAvailable` 给宿主机 |
| 磁盘 | 根分区至少 200 GiB 可用，否则终止 |
| 架构 | 针对 x86_64；其他架构只警告，但 Vagrant box 和依赖未保证可用 |
| 权限 | sudo |

定制 Vagrantfile 当前识别的 guest OS key 为 `ubuntu2404`、`almalinux9`、`rockylinux9`、`opensuse`、`opensuse-tumbleweed` 和 `oraclelinux9`；自动化模板默认使用 `rockylinux9`。宿主机支持范围与 guest OS 列表不是同一个概念。

默认 5 VM 配置为 26 vCPU、32 GiB guest 内存，另有 4 块 200 GiB 附加盘。新建集群会在修改宿主机前执行容量门禁：guest 内存不得超过当前 `MemAvailable` 的 80%，总 vCPU 不得超过宿主 CPU 的 3 倍。默认拓扑至少需要约 40 GiB `MemAvailable`，建议宿主机提供 64 GiB 以上内存。

### 3.2 网络和仓库

宿主机需要访问：

- 系统 DNF/YUM 仓库；RHEL 需要有效订阅和 BaseOS、AppStream、CodeReady Builder。
- HashiCorp RPM 仓库。
- EPEL。
- GitHub、raw.githubusercontent.com、PyPI、Vagrant Cloud。
- Kubernetes/容器镜像仓库和所选 Helm 仓库。

代理由以下环境变量读取。建议同时设置 `HTTP_PROXY` 和 `HTTPS_PROXY`；脚本在任一变量非空时把整组代理写入生成配置：

```bash
export HTTP_PROXY=http://proxy.example.com:8080
export HTTPS_PROXY=http://proxy.example.com:8080
export NO_PROXY=localhost,127.0.0.1,192.168.0.0/16,10.0.0.0/8
export PIP_PROXY="$HTTP_PROXY"
export GIT_PROXY="$HTTP_PROXY"
```

脚本会测试外部地址。首次 clone 嵌套工作区时可能写入全局 Git proxy，且不会自动恢复。代理口令可能出现在进程参数、环境或日志中，应使用专用低权限凭据。

### 3.3 预检查

```bash
grep -E 'Red Hat|CentOS|Rocky|AlmaLinux' /etc/os-release
nproc
free -h
df -h /
grep -E -m1 'vmx|svm' /proc/cpuinfo
test -e /dev/kvm && echo /dev/kvm-ready
sudo -v
```

## 4. 获取代码和目录关系

```bash
git clone https://github.com/upmio/kubespray-upm.git
cd kubespray-upm
export REPO_ROOT="$PWD"
cd "$REPO_ROOT/vagrant_setup_scripts"
```

后续命令统一使用 `REPO_ROOT`，避免在外层仓库和嵌套部署工作区之间进入错误目录。重新打开 shell 后需要再次设置该变量。

运行后会形成嵌套工作区：

```text
当前仓库/vagrant_setup_scripts/
├── libvirt_kubespray_setup.sh
└── kubespray-upm/                 # 实际 Vagrant/Kubespray 工作区
    ├── Vagrantfile
    ├── vagrant/config.rb
    └── inventory/sample/artifacts/
```

如果嵌套目录已存在，脚本会在其中执行 `git pull`。外层仓库的未提交改动不会自动同步到这个工作区。

## 5. NAT 网络

### 5.1 当前地址规则

libvirt NAT 模式固定使用：

- libvirt network：`nat-200-network`
- 子网：`192.168.200.0/24`
- 默认第一台 VM：`192.168.200.101`
- 第 N 台 VM：`192.168.200.(100 + N)`

这些是 Vagrant 配置的静态地址，不是 DHCP 动态分配。

### 5.2 `nat-200-network`

主脚本的 `setup_libvirt()` 会自动创建、启动并设置 `nat-200-network` 开机自启。先检查：

```bash
sudo virsh net-info nat-200-network
```

如果不通过主脚本运行，或需要手工修复，可创建一个与当前 Vagrantfile 匹配的网络：

```bash
tmpfile=$(mktemp)
printf '%s\n' \
  '<network>' \
  '  <name>nat-200-network</name>' \
  "  <forward mode='nat'/>" \
  "  <bridge name='virbr200' stp='on' delay='0'/>" \
  "  <ip address='192.168.200.1' netmask='255.255.255.0'>" \
  '    <dhcp>' \
  "      <range start='192.168.200.2' end='192.168.200.100'/>" \
  '    </dhcp>' \
  '  </ip>' \
  '</network>' > "$tmpfile"

sudo virsh net-define "$tmpfile"
sudo virsh net-autostart nat-200-network
sudo virsh net-start nat-200-network
rm -f "$tmpfile"
```

验证：

```bash
sudo virsh net-info nat-200-network
sudo virsh net-dumpxml nat-200-network
ip addr show virbr200
```

如果宿主机已有 `192.168.200.0/24` 路由或同名 bridge，请先解决冲突。仅修改文档命令中的子网不够，还必须同步修改 `Vagrantfile` 中 libvirt NAT 地址规则及模板/预览逻辑。

## 6. Bridge 网络

推荐通过默认网络向导进入 Bridge 配置：

```bash
./libvirt_kubespray_setup.sh
```

先在 `VM Network Selection` 中选择 `2. Bridge`。如果使用 `-n bridge`，只是预先选择该项，以下内容仍需交互输入：

- 物理网络接口。
- 如果接口已有 IP：断网风险确认，并再次输入当前 IP 复核。
- VM 分配基址和 CIDR，例如 `192.168.29.50/20`；第一台 VM 使用下一个地址。
- 网关和 DNS。

新生成的 `config.rb` 会记录宿主物理网卡 `$bridge_host_interface`。检测到匹配的 libvirt VM/domain（包括关机状态）后再次执行脚本时，只有在 `br0` 为 UP、恰好存在一个 active NetworkManager `bridge-slave-*` 物理网卡连接，并且 profile 的 interface/master/slave-type 均匹配时才复用。虚拟机创建的 `vnet*` tap 不参与物理网卡判定。旧版本配置没有该字段时，脚本会从当前 active NetworkManager bridge slave 推导并迁移；无法唯一确定或现场状态不一致时安全失败，不会通过向导猜测或重新挂载其他网卡。

新建集群遇到同名 `br0`、slave profile 或 `bridge-network` 时也会逐项核对类型、interface、master、IP method 和 libvirt bridge XML；不一致时在激活连接前安全失败。

当前实现会：

1. 创建 NetworkManager bridge `br0`。
2. 将 `br0` 的 IPv4/IPv6 method 设置为 disabled，并关闭 STP。
3. 把所选物理接口加入 `br0`。
4. 创建名为 `bridge-network` 的 libvirt 网络。
5. VM 实际通过 `dev=br0,type=bridge` 直接连接 bridge。

脚本不会自动把宿主机原有 IP、默认路由和 DNS 迁移到 `br0`。远程执行很可能断开连接，应使用物理控制台或带外管理，并提前准备 NetworkManager 回滚命令。

地址算法只保留输入 IP 的前三段，并将第四段作为偏移。若输入 `192.168.29.100`，第一台 VM 实际为 `192.168.29.101`。请保证：

```text
起始第四段 + VM 数量 <= 254
```

当前算法不会正确跨越 `/24` 边界。Rocky/Alma guest 的路由配置还假设第二个连接名为 `System eth1`；其他连接命名需要修改 Vagrantfile。

## 7. Kubernetes 网络方案

虚拟机网络和 Kubernetes CNI 是两个不同层次：

| 层次 | 可选方案 | 作用 |
| --- | --- | --- |
| libvirt/VM 网络 | NAT、Bridge | 决定宿主机、VM 和物理网络如何互通 |
| Kubernetes CNI | Calico、Cilium | 提供 Pod 网络、NetworkPolicy 和 Service 数据面 |
| Service LoadBalancer | Cilium LB IPAM + L2 Announcement | 为裸机/libvirt 集群的 `LoadBalancer` Service 分配并通告 IP |

### 7.1 CNI 方案对比

| 方案 | kube-proxy | LoadBalancer | 适用场景 |
| --- | --- | --- | --- |
| Calico | 保留 | 不由本自动化提供 | 默认方案、传统 Kubernetes 网络 |
| Cilium 标准模式 | 保留 | 不启用 L2 LB | 需要 Cilium eBPF、策略或可观测能力 |
| Cilium kube-proxy replacement | 不部署 | 可选 | 使用 Cilium eBPF 处理 Kubernetes Service |
| Cilium + LoadBalancer | 必须替换 | LB IPAM + L2 | 从宿主机或物理二层网络访问 Service |

Cilium 模式会向 Kubespray 传入 `kube_owner: root`。选择替换 kube-proxy 时会同时设置：

```yaml
cilium_kube_proxy_replacement: true
kube_proxy_remove: true
```

### 7.2 交互向导和参数预置

推荐只指定与集群规模相关的参数，让向导根据现场网络逐步填写：

```bash
./libvirt_kubespray_setup.sh -c 8
```

向导决策顺序：

```text
VM 网络：NAT / Bridge
  -> Bridge：宿主物理网卡、风险复核、VM 分配基址/CIDR、网关、DNS
Kubernetes CNI：Calico / Cilium
  -> Cilium：是否替换 kube-proxy
  -> Cilium：是否启用 LoadBalancer
     -> LB 地址范围、guest L2 网卡
系统资源、仓库和NTP检查
  -> 第一次确认：允许修改宿主机并安装依赖
生成 config.rb、显示VM地址和资源计划
  -> 第二次确认：开始 vagrant up 和 Kubespray
```

CLI网络参数保留为可选预置值，用于重复部署或自动化；它们只跳过对应问题：

```bash
# Calico，保留 kube-proxy
./libvirt_kubespray_setup.sh -y -n nat -p calico

# Cilium，保留 kube-proxy
./libvirt_kubespray_setup.sh -y -p cilium

# Cilium 替换 kube-proxy
./libvirt_kubespray_setup.sh -y -p cilium \
  --cilium-kube-proxy-replacement

# NAT + Cilium LoadBalancer
./libvirt_kubespray_setup.sh -y -n nat -p cilium \
  --cilium-load-balancer \
  --cilium-lb-range 192.168.200.201-192.168.200.220 \
  --cilium-lb-interface eth1
```

`--cilium-lb-range` 本身会启用 LoadBalancer。使用 `-y --cilium-load-balancer` 时必须同时提供地址范围；非自动确认模式会继续交互询问缺失的地址范围和 guest 网卡。`--cilium-lb-interface` 只在启用 LoadBalancer 时有效。

Cilium L2 Announcement 依赖 kube-proxy replacement，因此启用 LoadBalancer 时脚本会自动将 `cilium_kube_proxy_replacement` 和 `kube_proxy_remove` 设置为 `true`，并关闭 Cilium transparent DNS proxy 模式以避免与 DNS 配置冲突。

### 7.3 配置传递与手工 group_vars 对照

使用 `libvirt_kubespray_setup.sh` 时，脚本先把选择写入生成的 `vagrant/config.rb`，随后由 Vagrantfile 通过 `ansible.extra_vars` 传递给 Kubespray。主脚本不会直接修改 inventory 中的 `group_vars`，extra vars 的优先级高于 group vars。

Cilium replacement + LoadBalancer 最终等价于以下配置。

`group_vars/k8s_cluster/k8s-cluster.yml`：

```yaml
kube_network_plugin: cilium
kube_owner: root
```

`group_vars/all/all.yml`：

```yaml
kube_proxy_remove: true
```

`group_vars/k8s_cluster/k8s-net-cilium.yml`：

```yaml
cilium_kube_proxy_replacement: true
cilium_dns_proxy_enable_transparent_mode: false
cilium_l2announcements: true

cilium_loadbalancer_ip_pools:
  - name: default-lb-pool
    ranges:
      - start: "192.168.25.20"
        stop: "192.168.25.49"
```

地址只是示例，必须按实际 VM 网段规划。YAML 中不能保留 diff 标记、错误缩进或中文标点。

如果直接运行 `ansible-playbook cluster.yml`，或独立执行 `vagrant up`/`vagrant provision` 而不经过主脚本，Kubespray 只会创建 `CiliumLoadBalancerIPPool`，不会执行本项目部署后的 Policy 创建函数，因此仍需手工应用 `CiliumL2AnnouncementPolicy`。

### 7.4 Kubernetes API Server endpoint

Cilium kube-proxy replacement 在启动阶段必须能访问真实 Kubernetes API Server。当前项目没有把业务 Service LoadBalancer 用作 API endpoint，而是保留 Kubespray 默认行为：

```yaml
loadbalancer_apiserver_localhost: true
```

在该模式下，Kubespray会在非 control-plane 节点部署本地 nginx/haproxy API proxy，自动推导：

```yaml
kube_apiserver_global_endpoint: "https://localhost:6443"
```

Cilium Helm values 的 `k8sServiceHost` 和 `k8sServicePort` 来自这个派生 endpoint。即使设置了 `kube_proxy_remove: true`，本地 API proxy 的部署逻辑仍然保留。

如果明确不使用本地 API proxy，可以设置：

```yaml
loadbalancer_apiserver_localhost: false
```

此时 Kubespray会自动使用第一个 control-plane 节点的访问地址，通常不需要直接覆盖 `kube_apiserver_global_endpoint`。普通 worker 节点没有监听 kube-apiserver 6443，不能作为 global endpoint。

如果存在真正的外部负载均衡器或稳定 VIP，应配置源变量：

```yaml
loadbalancer_apiserver:
  address: 192.168.25.50
  port: 6443
```

不要写 Markdown 链接格式：

```yaml
# 错误
kube_apiserver_global_endpoint: "[https://192.168.25.51:6443](https://192.168.25.51:6443)"
```

如果确实需要覆盖，必须是普通 URL：

```yaml
kube_apiserver_global_endpoint: "https://192.168.25.51:6443"
```

但直接覆盖派生变量可能使 Cilium、kubeadm discovery、kubeconfig 和证书 SAN 使用不同 endpoint，不推荐作为常规方案。Cilium Service LoadBalancer 在 Cilium 启动后才可用，不能承担 Cilium 启动前的 API Server bootstrap endpoint。

### 7.5 Cilium LoadBalancer 工作方式

Kubespray 根据 `cilium_loadbalancer_ip_pools` 创建 `CiliumLoadBalancerIPPool`，但上游逻辑不会创建 `CiliumL2AnnouncementPolicy`。本项目在 Kubernetes 部署完成后补充以下步骤：

1. 等待 Cilium LoadBalancer 和 L2 Announcement CRD Ready。
2. 确认 Kubespray 已创建指定 IPPool。
3. 通过 Vagrant SSH 验证每个节点的 InternalIP 位于所选 guest 网卡。
4. 创建允许 LoadBalancer IP 的 `CiliumL2AnnouncementPolicy`。
5. 重新读取 IPPool 和 Policy 作为配置证明。

直接运行 Kubespray 时可手工应用：

```yaml
apiVersion: cilium.io/v2alpha1
kind: CiliumL2AnnouncementPolicy
metadata:
  name: default-l2-announcement-policy
spec:
  serviceSelector: {}
  nodeSelector: {}
  interfaces:
    - "^eth1$"
  externalIPs: false
  loadBalancerIPs: true
```

如果 guest 网卡不是默认的 `eth1`，部署前先根据 box/网络配置确定名称并使用 `--cilium-lb-interface`。接口选错时可能出现 Service 已获得 `EXTERNAL-IP`，但集群外访问超时。

脚本中的创建逻辑位于 `configure_cilium_load_balancer()`。它只在 CNI 为 Cilium 且启用 LoadBalancer 时执行，并在 `vagrant up` 或 `vagrant provision` 成功、宿主机 kubectl 配置完成后运行。

### 7.6 地址规划

LoadBalancer 地址范围必须：

- 与 Kubernetes 节点位于同一二层网段。
- 避开网关、VM 静态地址和 DHCP 地址池。
- 不与 Pod CIDR、Service CIDR、其他 Cilium IPPool 或物理设备地址重叠。
- 起始地址不大于结束地址，并避开网络地址和广播地址。

`--cilium-lb-range` 使用 `START_IP-STOP_IP`，不需要再提供独立掩码：范围本身写入 `CiliumLoadBalancerIPPool`，掩码用于判断地址是否属于节点二层子网。NAT 固定使用 `255.255.255.0`；Bridge 使用前面输入 VM 起始地址时解析出的 CIDR/netmask。因此 Bridge 为 `/20`、`/25` 等配置时，校验也按对应掩码计算网络地址和广播地址，而不是固定比较 IPv4 的前三段。

脚本会自动检查本项目默认 Service CIDR `10.233.0.0/18` 和 Pod CIDR `10.233.64.0/18`。DHCP租约、其他Cilium IPPool及物理设备占用无法在安装前完整发现，仍需人工确认。

NAT 模式下，LB 地址通常只对宿主机及 libvirt 网络可达。需要物理局域网客户端直接访问时，应使用 Bridge，并从物理网段中预留未使用地址。

不要直接通过 `vagrant provision` 在已有集群上切换 Calico/Cilium或启用 kube-proxy replacement。CNI 数据面属于安装级配置，建议销毁旧集群后重新部署。

### 7.7 网络测试矩阵

先运行静态向导测试：

```bash
bash -n vagrant_setup_scripts/libvirt_kubespray_setup.sh
shellcheck --severity=error \
  vagrant_setup_scripts/libvirt_kubespray_setup.sh \
  vagrant_setup_scripts/tests/test_network_wizard.sh
bash vagrant_setup_scripts/tests/test_network_wizard.sh
```

该测试覆盖网络选择、CIDR/LB校验、已有集群保护、资源门禁和配置回读；真实虚拟化及外部访问仍必须使用下面的集成场景验证。

| 场景 | 关键预期 |
| --- | --- |
| 无网络参数启动 | 依次询问 VM 网络和 CNI，回车采用 NAT + Calico |
| `-y` 且无网络预置 | 不等待普通功能选择，使用 NAT + Calico |
| 部分 CLI 预置 | 只跳过对应问题，其余字段继续由向导询问 |
| 检测到匹配的 libvirt VM/domain（包括关机状态） | 复用并锁定安装时 VM 网络、Bridge、CNI 和 Cilium/LB 配置 |
| 已有集群与 CLI 冲突 | 在修改宿主网络或覆盖 `config.rb` 之前拒绝 |
| Calico | `calico-node` 和 kube-proxy DaemonSet Ready |
| Cilium 标准模式 | Cilium Ready，kube-proxy 仍存在 |
| Cilium replacement | Cilium Ready，kube-proxy 不存在，Cilium 状态显示 replacement 已启用 |
| Cilium + LoadBalancer | 自动启用 replacement，IPPool、L2 Policy 存在，测试 Service 获得池内地址 |
| Cilium replacement + LoadBalancer | kube-proxy-free、LB IPAM、L2 通告同时正常 |
| 非法 CNI 或 Calico + Cilium 参数 | 脚本在创建 VM 前拒绝 |
| LB 地址与 VM/网关重叠 | 脚本在创建 VM 前拒绝 |
| L2 接口错误 | 部署后接口校验失败，并输出 `ip -br addr` 排查命令 |

LoadBalancer 场景必须创建临时 `LoadBalancer` Service，依次确认集群内部、宿主机，以及 Bridge 模式下物理局域网客户端都能访问。只看到 `EXTERNAL-IP` 不算通过。

## 8. 执行部署

### 8.1 参数

```text
./libvirt_kubespray_setup.sh [OPTIONS]

-h, --help          帮助
-v, --version       版本
-y                  未预置项使用 NAT + Calico，并自动确认普通 yes/no 提示
-c <3-50>           VM 总数
-n nat|bridge       可选预置 VM 网络；省略时由向导询问
-p calico|cilium    可选预置 Kubernetes CNI；省略时由向导询问
--cilium-kube-proxy-replacement
--cilium-load-balancer
--cilium-lb-range START_IP-STOP_IP
--cilium-lb-interface INTERFACE
```

`-c` 只覆盖 `$num_instances`，不会改变 etcd、control-plane 或 UPM 节点数。普通 worker 固定使用 6 CPU/8 GiB；节点数变化后脚本会重新计算集群总资源，并在修改宿主机前执行内存和 CPU 容量门禁。

网络参数是“预置值”，不是必须参数。正常人工部署建议省略 `-n`、`-p` 和 Cilium 参数，通过向导阅读说明后填写。`-y` 面向自动化：未指定的功能使用安全默认 NAT + Calico，但 Bridge 宿主网络输入、已有 IP 的双重确认和已有 VM 处理菜单不会被跳过。

脚本需要读取向导输入但当前标准输入不是 TTY 时会直接报错，不会在 CI 或无 PTY 的 SSH 会话里无限等待。Bridge 模式始终需要交互终端，因为宿主物理网卡、CIDR、网关、DNS和断网风险必须结合现场确认。

### 8.2 NAT 示例

以下命令启动交互向导；在 `VM Network Selection` 中按回车接受默认值或输入 `1` 选择 NAT。

```bash
cd "$REPO_ROOT/vagrant_setup_scripts"
sudo virsh net-info nat-200-network
./libvirt_kubespray_setup.sh
```

### 8.3 Bridge 示例

```bash
cd "$REPO_ROOT/vagrant_setup_scripts"
./libvirt_kubespray_setup.sh
# 在 VM Network Selection 中选择 2. Bridge
```

### 8.4 安装流程

脚本依次执行：

```text
读取并锁定已有集群网络配置
  -> 交互网络向导或应用 CLI 预置值
  -> 宿主机资源、集群容量与 RHEL 仓库检查
  -> chrony/NTP 检查
  -> 第一次确认：允许宿主机改动和依赖安装
  -> 禁用 firewalld 和 SELinux
  -> 安装 libvirt/QEMU
  -> 安装 Vagrant 和 vagrant-libvirt
  -> 安装 pyenv/Python 3.12.11/依赖
  -> clone 或 pull 嵌套工作区
  -> 生成 vagrant/config.rb 并替换 Vagrantfile
  -> 显示 VM 地址、CNI/LB 和实际资源计划
  -> 第二次确认：开始创建 VM
  -> 可选覆盖 containerd.yml
  -> 检查已有 VM
  -> vagrant up + Kubespray
  -> 配置宿主机 kubectl/kubeconfig
  -> Cilium LB 模式下校验网卡并创建 L2 Announcement Policy
```

已有 VM 数量与配置一致时，脚本会要求选择继续 `vagrant up --provision`、仅 provision、删除重建或取消；数量不一致时只能删除全部匹配 VM 或取消。该菜单不受 `-y` 控制。检测到真实 libvirt VM 后，脚本会锁定并复用 VM 数量、资源、VM 网络、Bridge 参数、CNI、replacement、LoadBalancer 地址池和 L2 接口；任何 CLI 冲突都会在修改宿主机或覆盖 `config.rb` 前失败。删除重建优先执行 `vagrant destroy -f`，随后清理残留domain、Vagrant metadata和global-status。只有残留 `config.rb`、但没有匹配 libvirt VM 时，旧值会被忽略并重新生成。安装级网络方案要改变时必须删除并重建集群。

## 9. 默认拓扑和配置

### 9.1 节点与 Ansible 组

| 节点 | 资源档位 | Ansible/Kubernetes 用途 |
| --- | --- | --- |
| `k8s-1` | control-plane：4 CPU/4 GiB | etcd、`kube_control_plane`、`kube_node` |
| `k8s-2` | UPM：4 CPU/4 GiB | `kube_node`；后续脚本的 UPM/OpenEBS/Prometheus 标签节点 |
| `k8s-3` 至 `k8s-5` | worker：6 CPU/8 GiB | `kube_node` |

所有节点都属于 `kube_node`。所谓 UPM control 是资源和标签约定，Vagrantfile 没有创建名为 `upm_control` 的 Ansible group。

默认 5 VM 资源总计为 26 vCPU/32 GiB；8 VM 为 44 vCPU/56 GiB。新建集群使用当前 `MemAvailable` 进行80%内存上限检查，并限制总vCPU不超过宿主CPU的3倍。已有集群重跑不使用当前空闲内存重新规划，而是保留 `config.rb` 中的安装期资源。

### 9.2 软件

| 项目 | 模板值 |
| --- | --- |
| Kubernetes | 1.36.1 |
| OS | Rocky Linux 9 / `bento/rockylinux-9.6` |
| CNI | Calico（默认），可选 Cilium |
| cert-manager | True |
| local-path-provisioner | False |
| 时区 | Asia/Shanghai |

### 9.3 附加磁盘

默认对索引大于 control-plane/etcd 边界的 VM 添加一块 200 GiB 磁盘，因此默认是 `k8s-2` 到 `k8s-5`。guest provisioning 会尝试建立 `local_vg_dev`。

当前 libvirt 分支没有使用模板中的 `$kube_node_instances_with_disk_dir`，不要依赖该变量决定宿主机磁盘文件位置。更重要的是，guest 内的初始化逻辑会枚举全部非根磁盘；如 VM 中还挂载了其他数据盘，应先禁用：

```ruby
$kube_node_instances_with_disks = false
$kube_node_instances_create_vg = false
```

新建集群或只有残留配置而没有匹配的 VM/domain 时，`vagrant/config.rb` 会从模板重新生成；检测到匹配的 libvirt VM/domain（包括关机状态）时不会覆盖安装期配置。要持续变更默认资源，应同时修改 NAT/Bridge 模板并重新执行容量评审。

## 10. containerd 镜像仓库

创建 `containerd.yml` 才会触发覆盖：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts"
cp containerd-example.yml containerd.yml
$EDITOR containerd.yml
./libvirt_kubespray_setup.sh -y
```

目标文件是嵌套工作区的：

```text
kubespray-upm/inventory/sample/group_vars/all/containerd.yml
```

若目标是符号链接，脚本会先删除链接；若是普通文件，会把备份放在外层 `vagrant_setup_scripts` 中。请检查差异并避免在配置中提交明文凭据。

在 `download_run_once` 模式下，containerd镜像先在下载节点拉取，再通过 `nerdctl image save` 导出到缓存并分发。Save过程在本地缺少blob时仍可能访问registry，因此Pull成功不代表Save一定离线。本项目现在同时给containerd Pull和Save任务注入 `proxy_env`；代理环境验收应确认 `Download_container | Save and compress image` 不再出现registry直连 `i/o timeout`。

## 11. kubectl 配置

Kubespray 通过 Vagrant host vars 生成：

```text
inventory/sample/artifacts/kubectl
inventory/sample/artifacts/admin.conf
```

主脚本随后：

- 将已有 `$HOME/bin/kubectl` 和 `$HOME/.kube/config` 重命名为带时间戳的备份。
- 创建指向上述 artifacts 的符号链接。
- 最多测试 4 次 `cluster-info`；连接测试最终失败只产生警告。

部署后必须自行验证：

```bash
readlink -f "$HOME/bin/kubectl"
readlink -f "$HOME/.kube/config"
kubectl cluster-info
kubectl get nodes -o wide
kubectl get pods -A
```

## 12. VM 和集群生命周期

进入实际工作区：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
```

常用命令：

```bash
vagrant status
vagrant ssh k8s-1
vagrant ssh k8s-2
vagrant halt
vagrant up --provider=libvirt --no-parallel --provision
vagrant provision --provision-with ansible
vagrant destroy -f
```

virsh 中的 domain 名带有 `kubespray` 前缀，先以实际输出为准：

```bash
sudo virsh list --all
sudo virsh dominfo <domain-name>
```

强制删除会同时删除存储，执行前确认数据：

```bash
sudo virsh destroy <domain-name>
sudo virsh undefine <domain-name> --remove-all-storage
```

修改节点总数后直接运行 `vagrant up` 并不等于完成安全的 Kubernetes 扩缩容。已有节点的下线、etcd/control-plane 变更和 inventory 调整应遵循 Kubespray 的节点运维流程。

## 13. 扩展组件

### 13.1 正确脚本位置

使用嵌套工作区内的脚本：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
./vagrant_setup_scripts/upm_setup.sh --help
```

该脚本在解析 `--help`/`--version` 之前就检查 `kubectl`，因此没有 kubectl 的机器上帮助命令也会失败。

外层仓库中的 `upm_setup.sh` 查找的是外层 `vagrant/config.rb`，通常不存在；混用会导致无法获得正确节点拓扑和 VG 名称。

### 13.2 参数约束

```text
-h, --help
-v, --version
-y
--lvmlocalpv
--prometheus
--upm-engine
--upm-platform
--config_nginx
--all
```

解析器要求恰好一个安装选项。以下命令无效：

```bash
# 无效：指定了两个安装选项
./vagrant_setup_scripts/upm_setup.sh --lvmlocalpv --prometheus
```

分步安装应逐条执行，或者使用 `--all`。

### 13.3 组件顺序

```bash
# 推荐的一次性顺序；不包含 Nginx
./vagrant_setup_scripts/upm_setup.sh -y --all

# 如需宿主机 Nginx 入口，再单独执行
./vagrant_setup_scripts/upm_setup.sh -y --config_nginx
```

`--all` 的实际顺序：

```text
LVM LocalPV -> Prometheus -> UPM Engine -> UPM Platform
```

### 13.4 OpenEBS LVM LocalPV

- Helm chart：`openebs-lvmlocalpv/lvm-localpv`
- chart version：1.9.1，可由 `LVM_LOCALPV_CHART_VERSION` 覆盖
- namespace：`openebs`
- StorageClass：`lvm-localpv`
- VolumeGroup：从 `vagrant/config.rb` 读取，默认 `local_vg_dev`
- volume binding：`WaitForFirstConsumer`

```bash
./vagrant_setup_scripts/upm_setup.sh -y --lvmlocalpv
kubectl get pods -n openebs
kubectl get storageclass lvm-localpv -o yaml
kubectl get nodes --show-labels
```

### 13.5 Prometheus

- Helm chart：`prometheus-community/kube-prometheus-stack`
- chart version：87.10.1，可由 `PROMETHEUS_CHART_VERSION` 覆盖
- namespace：`prometheus`
- Prometheus PVC：30 GiB，StorageClass 固定为 `lvm-localpv`
- Operator、Prometheus、Alertmanager、Grafana 和 kube-state-metrics 被调度到带 `prometheus.node=true` 的 UPM 节点

脚本没有在安装前显式检查 StorageClass，所以单独安装 Prometheus 前应先确认：

```bash
kubectl get storageclass lvm-localpv
./vagrant_setup_scripts/upm_setup.sh -y --prometheus
kubectl get pods -n prometheus
helm list -n prometheus
```

访问方式：

```bash
kubectl port-forward -n prometheus svc/prometheus-kube-prometheus-prometheus 9090:9090
kubectl port-forward -n prometheus svc/prometheus-grafana 3000:80
```

具体 service 名应以 `kubectl get svc -n prometheus` 为准。Grafana 默认凭据由 chart 决定，当前脚本提示为 `admin/prom-operator`。

### 13.6 UPM Engine

```bash
./vagrant_setup_scripts/upm_setup.sh -y --upm-engine
kubectl get pods -n upm-system
helm list -n upm-system
```

chart version 默认为 1.2.4，可由 `UPM_CHART_VERSION` 覆盖。代码会给 UPM 节点添加 `upm.engine.node=enable`，但当前实现不检查 LVM LocalPV。

### 13.7 UPM Platform

UPM Platform 会检查 `openebs` 中的 LVM LocalPV release 和 `lvm-localpv` StorageClass。

建议先设置密码：

```bash
export UPM_PWD='replace-with-a-strong-password'
./vagrant_setup_scripts/upm_setup.sh -y --upm-platform
```

默认访问地址：

```text
http://<UPM节点IP>:32010/upm-ui/#/login
```

默认用户是 `super_root`；未设置 `UPM_PWD` 时默认口令为 `Upm@2024!`。安装后检查：

```bash
kubectl get pods -n upm-system
kubectl get svc -n upm-system
kubectl get clusterrolebinding upm-system-admin-default-account
```

当前脚本会创建以下高权限绑定：

```text
upm-system/default -> cluster-admin
```

实验完成后应根据实际控制器权限需求改为最小权限。

### 13.8 Nginx 入口

`--config_nginx` 会：

- 把 `upm-platform-gateway` 改为 NodePort 31404。
- 把 `upm-platform-ui` 改为 NodePort 31405。
- 在宿主机用 DNF 安装 Nginx。
- 备份并完整覆盖 `/etc/nginx/nginx.conf`。
- 在宿主机监听 80，并代理 `/upm-ui/` 与 `/api/`。

```bash
./vagrant_setup_scripts/upm_setup.sh -y --config_nginx
sudo nginx -t
systemctl status nginx
curl -I http://localhost/upm-ui/
```

该操作不在 `--all` 中，且不适合已经承载其他 Nginx 配置的宿主机。

## 14. 故障处理

### 14.1 查看日志

```bash
tail -f "$REPO_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.log"
tail -f "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm/vagrant_setup_scripts/upm_setup.log"
```

如果当前目录不同，请使用绝对路径确认读取的是外层主脚本日志还是嵌套工作区组件日志。

### 14.2 NAT network not found

```bash
sudo virsh net-list --all
sudo virsh net-info nat-200-network
```

按本文第 5 节创建并启动网络，然后重新执行：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
vagrant up --provider=libvirt --no-parallel
```

### 14.3 Bridge 后宿主机断网

优先使用本地控制台检查：

```bash
nmcli connection show
ip addr
ip route
```

当前 bridge 实现不会迁移宿主 IP。恢复方式依赖运行前的 NetworkManager 配置，不能使用一套通用命令盲目覆盖；必要时删除 `bridge-slave-*`/`br0` 连接并重新启用原连接。

### 14.4 Vagrant/libvirt

```bash
systemctl status libvirtd
vagrant plugin list
sudo virsh list --all
sudo virsh net-list --all
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
vagrant status
vagrant up --provider=libvirt --no-parallel
```

### 14.5 Helm 看不到已存在的 CRD

如果 `kubectl get crd` 能看到 CRD，但 Helm 报 RESTMapper 或 `no matches for kind`，先用独立 discovery cache 验证：

```bash
KUBECACHEDIR=$(mktemp -d) kubectl api-resources
KUBECACHEDIR=$(mktemp -d) ./vagrant_setup_scripts/upm_setup.sh -y --prometheus
```

不要在未确认目标 kubeconfig 的情况下删除整个 `$HOME/.kube/cache`。

### 14.6 Pod 未 Ready

```bash
kubectl get pods -A -o wide
kubectl describe pod -n <namespace> <pod>
kubectl get events -n <namespace> --sort-by=.lastTimestamp | tail -50
helm list -A
kubectl get pvc -A
kubectl get storageclass
```

Prometheus 和 UPM Platform 依赖 `lvm-localpv`；同时检查节点标签、VG 和附加磁盘：

```bash
kubectl get nodes --show-labels
vagrant ssh k8s-2 -c 'sudo vgs; sudo pvs; lsblk'
```

### 14.7 Cilium 和 LoadBalancer

```bash
kubectl -n kube-system get ds cilium
kubectl -n kube-system get ds kube-proxy
kubectl get ciliumloadbalancerippool
kubectl get ciliuml2announcementpolicy
kubectl -n kube-system exec ds/cilium -c cilium-agent -- \
  cilium status --verbose
```

Cilium 标准模式应同时存在 Cilium 和 kube-proxy；replacement 模式下不应存在 kube-proxy DaemonSet。LoadBalancer 模式必须同时存在 IPPool 和 L2 Policy。

如 Service 已分配 `EXTERNAL-IP` 但无法访问：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
vagrant ssh k8s-1 -c 'ip -br addr'
kubectl describe ciliumloadbalancerippool
kubectl describe ciliuml2announcementpolicy
```

重点确认 Policy 的接口正则匹配节点 InternalIP 所在网卡，并检查 IPPool 是否出现 `PoolConflict`。只有从宿主机或 Bridge 局域网客户端实际访问成功，才能认为 LoadBalancer 可用。

可使用临时Nginx完成端到端验证：

```bash
kubectl create namespace lb-smoke-test
kubectl -n lb-smoke-test create deployment nginx \
  --image=nginx:alpine --replicas=2
kubectl -n lb-smoke-test expose deployment nginx \
  --name nginx-lb --type=LoadBalancer --port=80 --target-port=80

kubectl -n lb-smoke-test get pods -o wide
kubectl -n lb-smoke-test get svc nginx-lb -o wide
LB_IP=$(kubectl -n lb-smoke-test get svc nginx-lb \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
curl --noproxy '*' -fsS "http://${LB_IP}/"

# L2 Lease holder是动态的，不能假设固定节点
kubectl -n kube-system get lease | grep cilium-l2announce
ip neigh show "$LB_IP"
```

如果要核对ARP MAC，可读取Lease holder，再在对应VM检查 `eth1`：

```bash
LEASE=$(kubectl -n kube-system get lease -o name | grep lb-smoke-test)
HOLDER=$(kubectl -n kube-system get "$LEASE" -o jsonpath='{.spec.holderIdentity}')
vagrant ssh "$HOLDER" -c 'cat /sys/class/net/eth1/address'
```

测试完成后清理namespace；若Cilium的Lease尚未自动回收，再删除对应测试Lease：

```bash
kubectl delete namespace lb-smoke-test --wait=true
kubectl -n kube-system get lease -o name | grep lb-smoke-test | \
  xargs -r kubectl -n kube-system delete
```

Bridge面向物理局域网服务时，还应从目标局域网的另一台客户端访问一次。宿主机访问成功只能证明宿主机到L2 VIP链路可用。

## 15. 完成验收

Kubernetes 部署至少应满足：

```bash
cd "$REPO_ROOT/vagrant_setup_scripts/kubespray-upm"
vagrant status
kubectl get nodes -o wide
kubectl get pods -A
kubectl cluster-info
```

根据所选 CNI 继续检查：

```bash
# Calico
kubectl -n kube-system get ds calico-node
kubectl -n kube-system get ds kube-proxy

# Cilium
kubectl -n kube-system get ds cilium
kubectl -n kube-system exec ds/cilium -c cilium-agent -- \
  cilium status --verbose

# Cilium LoadBalancer
kubectl get ciliumloadbalancerippool
kubectl get ciliuml2announcementpolicy
kubectl get svc -A
```

如果安装扩展组件，还应检查：

```bash
helm list -A
kubectl get storageclass
kubectl get pvc -A
kubectl get pods -n openebs
kubectl get pods -n prometheus
kubectl get pods -n upm-system
kubectl get svc -n prometheus
kubectl get svc -n upm-system
```

只有 VM、Kubernetes 节点、系统 Pod、Helm release、存储和访问路径都通过验证，才能认为相应功能部署完成。
