# upmctl rc3 管理员运维真实环境测试记录

状态：`COMPLETED — IMPLEMENTED HOST-SAFE SCOPE PASSED`
测试日期：2026-07-20
测试主机：`192.168.21.95` / `mg-95node`
精确工作区：`/root/kubespray-upm-current/vagrant_setup_scripts/kubespray-upm`

## 结论

`upmctl 0.1.0-rc3`已在真实Rocky Linux/libvirt/Vagrant/Kubernetes环境中完成当前已实现、不会改变基础设施状态的管理员运维功能验证：

- host-safe测试结果：`55 PASS / 0 FAIL / 0 BLOCKED`；
- 5台Vagrant虚拟机均为`RUNNING_HEALTHY`，SSH状态均为`reachable`；
- Kubernetes中的5个对应节点均为`Ready`；
- 对全部5台运行中虚拟机执行`plan vm start`均得到`NOOP`，没有产生变更计划；
- 测试前后管理状态、Vagrant、libvirt和Kubernetes快照一致；
- 接管状态保持`MANAGED_VALID`，状态文件权限和内容摘要符合安全要求。

以上结论只覆盖当前已实现的只读观察、接管状态读取、安全拒绝和只读规划能力，不代表Phase 2变更执行能力已经实现。当前commit带`-dirty`，该制品只能作为测试候选，不能作为正式生产Release签发。

## rc3制品与升级记录

- Version：`0.1.0-rc3`
- Platform：`linux/amd64`
- Build：`2026-07-20T01:01:36Z`
- Git commit：`3e367671adb84e2852499d736bf8835edb392780-dirty`
- Archive SHA-256：`11caf6c086f96429707aac728e6d7f4a4b2437efc635c770ea2f856545648bb1`
- Archive/installed binary SHA-256：`528b0af1ca9526aecf070db59e1f733159aad2c12350bf8449b1049fe66a9de0`
- Validation tier：`rocky9-e2e-candidate`
- 被替换二进制备份：`/usr/local/bin/upmctl.backup.20260720T024740Z.sYZvkJ`
- rc3 host-safe最终报告目录：`/var/lib/upmctl/validation/20260720-host-safe-managed-rc3-final`

rc3最终Release包连续两次构建得到相同Archive SHA-256。升级安装保留了原二进制备份；最终归档内二进制与已安装二进制SHA-256相同。外层校验和、包内manifest及交付文件完整性验证均通过。

## 环境接管和安全状态

管理员已通过本人控制的TTY完成工作区接管。当前环境状态为：

- Environment ID：`env-mg-95node`
- Managed：`true`
- Trust：`MANAGED_VALID`
- `.upmctl`目录权限：`0700`
- `.upmctl/state.json`权限：`0600`
- `state.json` SHA-256：`3d5cba7a5438760bcd05a37657742c87326c94aa3600c66e5db3936302014dcd`

测试未手工修改`.upmctl`工件，报告未记录接管challenge、管理员reason、密码或kubeconfig内容。

## rc3已执行结果

| 验证项 | 结果 | 证据或结论 |
| --- | --- | --- |
| 本地Go、契约和安全门禁 | PASS | vet、unit、race、Schema/CLI contract、legacy、delivery fixture、Skill validator通过 |
| 离线CLI审计 | PASS | 最终Release包内CLI审计`31 PASS / 0 FAIL` |
| Release可重复构建 | PASS | 两次rc3构建Archive SHA-256一致 |
| 外层archive校验 | PASS | linux-amd64 SHA-256匹配 |
| 包内完整性 | PASS | manifest及交付文件校验通过 |
| 替换升级 | PASS | 旧二进制已保存至精确备份路径，rc3安装成功 |
| 环境接管状态 | PASS | `managed=true`、`trust=MANAGED_VALID` |
| 状态文件安全 | PASS | `.upmctl`为`0700`，`state.json`为`0600`，SHA-256保持一致 |
| host-safe全覆盖 | PASS | `55 PASS / 0 FAIL / 0 BLOCKED` |
| VM观察 | PASS | 5台VM均为`RUNNING_HEALTHY`、`sshState=reachable` |
| Kubernetes节点观察 | PASS | 5个对应节点均为`Ready` |
| `plan vm start`只读规划 | PASS | 全部5台运行中VM均返回`NOOP` |
| 非TTY人工操作保护 | PASS | Adopt和Approval人工边界保持，不由自动化脚本代输challenge |
| 前后快照比较 | PASS | 管理状态、Vagrant、libvirt、Kubernetes状态一致，无基础设施副作用 |

host-safe详细输出、快照和检查点保存在：

`/var/lib/upmctl/validation/20260720-host-safe-managed-rc3-final`

## 历史问题及修复闭环

下列问题是在旧的`0.1.0-rc2`真实环境验证中发现的，均已在rc3修复并重新验证：

| rc2历史问题 | 原影响 | rc3处理与验证结果 |
| --- | --- | --- |
| running VM仅检查`vagrant ssh-config` | 5台实际可SSH的VM被误判为`RUNNING_DEGRADED`，`plan vm start`被`TARGET_RUNNING_DEGRADED`阻断 | running VM增加固定SSH可达性探针；5台均为`RUNNING_HEALTHY`和`reachable`，全部规划结果为`NOOP` |
| 工作区没有`plans/`目录时读取不存在的Plan | 错误返回`UPMCTL_PLAN_STORE_UNSAFE`，不能准确表达资源不存在 | 正常缺失目录映射为`UPMCTL_PLAN_NOT_FOUND`；权限、symlink等不安全情况仍保持unsafe语义 |
| host-safe脚本将全局日志参数追加在业务参数之后 | 前置参数解析失败时日志参数未被解析，导致测试用例误判FAIL | 全局request-id和log-file参数前置，参数负向用例纳入rc3的55项全通过结果 |
| `checkpoints.md`使用未加引号heredoc | 文本中的反引号可能被Shell解释，检查点内容丢失 | heredoc改为字面量写入，rc3报告中的检查点内容完整 |

旧rc2制品信息仅作为历史追溯保留：Version为`0.1.0-rc2`，Archive SHA-256为`08f7c02274d4d57938c9e7780dc66d51c0b1b47d9befd9a0db8243ff2d77c949`。rc2测试候选已由rc3替换，不应继续用于验收。

## 前后状态一致性

rc3 host-safe覆盖在执行前后分别采集管理状态、Vagrant、libvirt和Kubernetes观察结果。对比结果一致：

- 5个libvirt domain保持运行；
- 5台Vagrant VM保持运行且可通过固定探针访问；
- 5个Kubernetes节点保持`Ready`；
- `state.json`权限和SHA-256未变化；
- `plan vm start`的`NOOP`结果未创建待执行的变更；
- 未删除工作区、`.vagrant`、kubeconfig、libvirt domain或磁盘。

## 尚未实现和不在本次通过范围内的能力

Phase 2变更运维能力仍未实现，当前不能通过`upmctl`完成以下实际操作：

- 启动、停止、重启、创建、删除Vagrant/libvirt虚拟机；
- 添加或减少Kubernetes节点；
- 执行已生成Plan或驱动Executor；
- 创建、扩缩、升级、重置或删除Kubernetes集群；
- 安装、升级、配置或删除Addon；
- 执行Operation生命周期和实际变更编排。

这些命令当前只能按CLI契约返回`UPMCTL_NOT_IMPLEMENTED`或安全拒绝，并验证没有副作用；不能将其计为管理员变更运维功能PASS。

ACTION_REQUIRED Plan、Preflight以及Approval grant/get/list/revoke的正向人工生命周期需要一台可安全停止和恢复的普通Worker，并要求管理员本人控制TTY完成授权。本次host-safe测试没有为了覆盖该链路而停止现有VM，也没有由自动化代替管理员完成grant/revoke。因此，`55 PASS / 0 FAIL / 0 BLOCKED`表示host-safe范围完整通过，不表示上述人工变更链路已完成业务验收。

## 发布判定

rc3可作为当前已实现范围的真实环境功能测试候选：其host-safe覆盖、VM健康观察、节点Ready观察、NOOP规划、安全边界和无副作用要求均通过。

rc3不能作为正式生产发布，原因是Git commit为`3e367671adb84e2852499d736bf8835edb392780-dirty`，制品无法对应到一个干净、可复现审计的源码提交。正式发布前至少需要：

1. 将预期源码和文档变更提交到干净commit；
2. 从该commit重新构建并验证可重复SHA-256；
3. 重新执行Release校验和目标环境验收；
4. 在Release说明中准确列出仍未实现的Phase 2能力。
