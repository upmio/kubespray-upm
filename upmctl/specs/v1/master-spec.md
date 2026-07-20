# upmctl V1 Master Spec

状态：`Accepted`

> 本文定义V1最终目标范围，不代表当前Phase 2b2a已经实现全部能力。当前交付能力必须以运行时`capabilities`和`cli-contract.md`的“当前阶段可用性”为准；VM实际启停、Kubernetes节点增减、Apply、Executor和Operation目前仍未实现。

## 1. 产品定义

`upmctl` 是在单台专用 Rocky Linux/RHEL 9 x86_64 宿主机上管理单个实验、研发或 PoC Kubernetes 环境的确定性 CLI。它复用 Vagrant/libvirt 创建和管理 VM，复用 Kubespray 管理 Kubernetes，复用 Helm/kubectl 安装和验证 UPM 组件。

Go 负责配置、现场发现、计划、审批、执行状态、验收证据和审计；不重写 Vagrant、Kubespray 或现有 Ansible roles/playbooks。

## 2. V1 必须交付

- Go `upmctl` CLI。
- 受管环境发现、配置校验、预检、状态、计划、审批、执行、验证和报告。
- 集群 deploy/start/stop/restart/destroy 的领域契约。
- Vagrant VM list/status/inspect/start/stop/restart 和人类交互 SSH。
- 普通 Worker 尾部单节点添加和减少。
- NAT/Bridge、Calico/Cilium、Cilium KPR、LB IPAM 和 L2 Announcement。
- OpenEBS LVM LocalPV、Prometheus、UPM Engine、UPM Platform。
- Codex Skill 固定工作流。
- MCP Application Service 和 Schema 预留。
- text、JSON、JSONL、稳定错误码和操作审计。

## 3. 支持矩阵

| 项目 | V1 边界 |
| --- | --- |
| 宿主机 | Rocky Linux 9.x、RHEL 9.x，专用主机 |
| 架构 | x86_64，KVM/libvirt |
| 环境 | 每宿主机一个受管环境 |
| VM | 3-8 台，默认 5 台，连续 `k8s-1..k8s-N` |
| Control plane/etcd | 固定 `k8s-1` |
| UPM/存储约定节点 | 固定 `k8s-2` |
| 可变拓扑 | 仅普通 Worker 尾部增减，每计划一个节点 |
| Guest | 产品版本矩阵中认证的 Rocky Linux 9 box |
| Kubernetes | 版本矩阵中固定版本，containerd |
| 网络 | NAT 或条件支持 Bridge；Calico 或 Cilium |

## 4. 硬性安全原则

- 只读命令可以直接执行；所有变更必须 `plan -> approval -> apply`。
- Plan 必须绑定 ConfigDigest 和 ObservedStateDigest。
- R1、R2、R3 操作均必须由本地人类控制TTY批准，Skill/MCP和非交互调用不得代批。
- Managed State、Vagrant metadata、libvirt domain 和 Kubernetes Node 身份不一致时拒绝变更。
- 默认禁止强制关机、跳过 drain、绕过 PDB、任意磁盘扫描和通用 Shell。
- 成功必须由实时后置条件和证据决定，不能只依据命令退出码。
- 失败不承诺事务级回滚，但必须保留 journal、现场状态和恢复入口。

## 5. 明确不支持

- 生产 SLA、多宿主机、多集群、HA control plane/etcd。
- Control plane/etcd 增减、角色转换、中间节点删除、一次多节点扩缩、自动伸缩。
- 通用 libvirt/Vagrant 管理、接管未受管 VM、公开 VM create/destroy。
- Snapshot、clone、suspend/resume、live migration、rename、在线资源调整。
- Kubernetes 升级、在线变更 CNI/网络模式/安装期拓扑。
- 自动迁移 LocalPV/hostPath 数据、失联节点强制删除。
- Agent SSH、通用远程命令、通用 sudo 或任意 Vagrant provisioner。
- 自动恢复宿主机 firewalld、SELinux、RPM、Bridge 或 shell 配置到安装前状态。

## 6. 成功定义

集群成功必须同时满足 VM 身份和状态正确、API 可达、预期 Node Ready、控制面/etcd/DNS/CNI 健康、kubeconfig 可用并生成带证据报告。Addon 成功必须验证 Helm release、工作负载、CRD discovery、存储依赖和实际 endpoint。

## 7. 冻结条款

> V1 只管理由 upmctl 在单台专用 Rocky Linux/RHEL 9 x86_64 宿主机上创建的单个 Vagrant/libvirt/Kubespray 集群；支持受控 VM 生命周期、普通 Worker 尾部增减、规定网络组合和 UPM Addons。任何通用 VM 管理、控制面扩缩、本地数据自动迁移或 Agent 自治高风险操作均不属于 V1。
