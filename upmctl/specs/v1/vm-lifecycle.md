# Vagrant VM Lifecycle

## 只读观察

`vm list/status/inspect`聚合：受管拓扑、Vagrant machine状态、`.vagrant`中的libvirt UUID、libvirt domain状态、IP、SSH可达性、Kubernetes Node状态、资源规格和当前操作。

状态枚举：

```text
RUNNING_HEALTHY RUNNING_DEGRADED STARTING STOPPING STOPPED
PROVISIONING PROVISION_FAILED MISSING ORPHANED INCONSISTENT UNKNOWN
```

## Phase 1b字段来源

| 字段 | 来源 |
| --- | --- |
| name/index/expected/role | 安全解析的锁定config.rb |
| vagrantState | `vagrant status --machine-readable` |
| libvirt UUID/state/domain | 受限metadata读取、`virsh domstate/dominfo` |
| Kubernetes Ready/UID/InternalIP | 摘要绑定kubeconfig的Node JSON |
| CPU/内存 | `virsh dominfo`，不可用时回退到声明配置 |
| SSH endpoint | 可信工作区`vagrant ssh-config`；仅固定执行`vagrant ssh NODE -c true`成功后状态为`reachable` |
| block devices | `virsh domblklist --details`，只报告观察结果，不声明归属或可删除性 |

单一来源不可用时必须返回`unknown`和finding，不能虚构健康。Phase 1b只允许固定、无参数注入的`true`命令探测SSH认证和Guest可达性；仅有endpoint配置仍不能判定`RUNNING_HEALTHY`。探针不读取Guest文件、不使用sudo、不接受用户命令，也不判断磁盘是否可删除。

## Start

已有VM使用：

```text
vagrant up NODE --provider=libvirt --no-provision
```

随后等待domain、SSH、Guest、containerd/kubelet、Node Ready、CNI和DaemonSet。运行且健康返回NOOP；运行但不健康不得静默重启。

## Stop和Restart

普通Worker stop/restart前必须检查PDB、Pod、容量、PV/PVC、VolumeAttachment、LocalPV和hostPath，并执行cordon/drain。Restart复用受控stop/start且必须观察boot ID变化。

- `k8s-1`不支持独立stop/restart，只能随cluster操作。
- `k8s-2`单独stop/restart为R3；存在不可迁移本地数据时拒绝。
- 默认禁止force poweroff、`virsh destroy`和跳过drain。

## Cluster顺序

Start：`k8s-1 -> control plane健康 -> k8s-2 -> k8s-3..N -> verify`。

Stop：`k8s-N..3 -> k8s-2 -> k8s-1`。非控制面停止失败时保留控制面运行。

## SSH

`vm ssh NODE`仅支持受管running VM和真实交互TTY。禁止`--command`、stdin脚本、自动sudo、后台模式、Skill和MCP调用。它是break-glass入口，不提供命令级审计保证。

## Create、Destroy和Provision

- 不公开VM create/destroy。
- Create仅是node add内部步骤。
- Destroy仅是node remove、cluster destroy或失败add清理的内部步骤。
- 内部动作`guest bootstrap`只允许新建或PROVISION_FAILED VM，不是公开CLI命令。
- 已加入集群节点的维护预留命名为`node reconcile`。
- 使用锁定安装期配置重新应用整个集群预留命名为`cluster reprovision`。
- `node reconcile`和`cluster reprovision`需要独立Spec后才能进入命令树；不得调用原始`vagrant provision`或透传任意provisioner。
