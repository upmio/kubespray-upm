# upmctl rc4 真实环境交付验收记录

状态：`CANDIDATE PASS — HOST-SAFE AND HUMAN STATEFUL SCOPE`
测试日期：2026-07-20
测试主机：`192.168.21.95` / `mg-95node`
精确工作区：`/root/kubespray-upm-current/vagrant_setup_scripts/kubespray-upm`

## 1. 结论

`upmctl 0.1.0-rc4`已从干净源码提交构建，并在真实 Rocky Linux、libvirt、Vagrant 和 Kubernetes 环境中完成 host-safe 与人工状态型交付验收：

- Release 包内 CLI 离线审计：`31 PASS / 0 FAIL`；
- host-safe 覆盖：`55 PASS / 0 FAIL / 0 BLOCKED`；
- 5 台 VM 全部为 `RUNNING_HEALTHY`，`sshState=reachable`；
- 5 个对应 Kubernetes Node 全部为 `Ready`；
- 对全部 5 台运行中 VM 计算 `plan vm start`均返回 `NOOP`；
- 管理状态、Vagrant、libvirt 和 Kubernetes 前后快照均一致；
- 升级前后 `.upmctl/state.json` 权限和 SHA-256 未变化；
- `k8s-4` 完成 cordon、drain、正常停机、无 provision 恢复和 uncordon；
- `ACTION_REQUIRED` Plan、get、validate 与 Preflight 契约通过；
- 管理员本人在真实 TTY 中完成 Approval grant/revoke；
- Approval 在 `MISSING`、`APPROVED`、`REVOKED` 三种状态下均保持 Apply `BLOCKED`；
- 恢复后 5 个 Node、全部 Deployment/DaemonSet 和 Pod 恢复健康。

## 2. 源码和制品身份

- Branch：`codex/upmctl-v1-rc4`
- Source commit：`7da7dc4deb6f6510327bedca32f511a5078b1b99`
- Version：`0.1.0-rc4`
- Build：`2026-07-20T04:15:43Z`
- Platform：`linux/amd64`
- Archive SHA-256：`2843f4b89240f527c498259ae3c9de3bcebb15c881d296e71c0a8c1bdd28c3b2`
- Installed/archive binary SHA-256：`d5d2ed97ffffbb8e4fd9933f9ab2c87a59e02075a828b72b2a9f06fc6f39f8af`
- Validation tier：`rocky9-e2e-candidate`

相同输入连续两次构建得到相同的三平台归档 SHA-256；Release smoke、内部 manifest 和校验和验证通过。版本输出中的 commit 不带 `-dirty`。

## 3. 安装记录

- 上一版本：`0.1.0-rc3`
- rc3 提交标识：`3e367671adb84e2852499d736bf8835edb392780-dirty`
- rc3 备份：`/usr/local/bin/upmctl.backup.20260720T043059Z.qNVD84`
- rc4 安装位置：`/usr/local/bin/upmctl`
- rc4 解包目录：`/root/upmctl-rc4-install.ZtmMtz/upmctl_0.1.0-rc4_linux_amd64`
- rc4 host-safe 报告：`/var/lib/upmctl/validation/20260720-host-safe-managed-rc4`
- rc4 人工状态型报告：`/var/lib/upmctl/validation/20260720-stateful-k8s-4-rc4`

安装前先校验外层 Archive SHA-256，然后使用 Release 包内 `install.sh --replace`替换旧二进制。安装脚本保留了 rc3 备份，包内二进制与已安装二进制摘要一致。

## 4. 托管环境安全状态

- Environment ID：`env-mg-95node`
- Managed：`true`
- Trust：`MANAGED_VALID`
- `.upmctl` 权限：`0700`
- `.upmctl/state.json` 权限：`0600`
- `state.json` SHA-256：`3d5cba7a5438760bcd05a37657742c87326c94aa3600c66e5db3936302014dcd`

测试没有手工修改 `.upmctl` 工件，没有记录密码、Adoption/Approval challenge、管理员 reason 或 kubeconfig 内容。

## 5. 执行结果

| 验证项 | 结果 | 证据或结论 |
| --- | --- | --- |
| Git 边界 | PASS | 制品源提交的新增路径全部位于 `upmctl/` |
| 本地质量门禁 | PASS | vet、unit、race、contract、legacy、delivery fixture、Skill validator 通过 |
| 可重复构建 | PASS | 两次 rc4 构建的三平台 SHA-256 全部一致 |
| Release smoke | PASS | Darwin ARM64、Linux AMD64、Linux ARM64 均通过 |
| 离线 CLI 审计 | PASS | `31 PASS / 0 FAIL` |
| host-safe 全覆盖 | PASS | `55 PASS / 0 FAIL / 0 BLOCKED` |
| VM 观察 | PASS | `k8s-1` 至 `k8s-5` 全部 `RUNNING_HEALTHY` / `reachable` |
| Kubernetes 观察 | PASS | `k8s-1` 至 `k8s-5` 全部 `Ready` |
| 全 VM NOOP Plan | PASS | 5 台 VM 均为 exit 0 / `NOOP` |
| 缺失 Plan 语义 | PASS | get、validate、preflight 均命中 `UPMCTL_PLAN_NOT_FOUND` 契约 |
| 快照对比 | PASS | control-state、Vagrant、libvirt、Kubernetes 均为 identical |
| 托管状态 | PASS | 权限和 `state.json` SHA-256 升级前后一致 |
| Worker 维护准备 | PASS | `k8s-4` 无 PDB/PV/VolumeAttachment，drain 后所有控制器在其他节点恢复 |
| ACTION_REQUIRED Plan | PASS | R1、scope `vm.start:k8s-4`、30 分钟 TTL，只写入不可变 Plan |
| Plan get/validate | PASS | exit 0，无 blocker，`observedStateBinding=NOT_CHECKED` |
| Preflight / MISSING | PASS | `PASSED`、`MISSING`、Apply `BLOCKED`、exit 3 |
| Approval grant | PASS | 管理员真实 TTY 完成，工件 `0600` |
| Preflight / APPROVED | PASS | `PASSED`、`APPROVED`、Apply `BLOCKED`、exit 3 |
| Approval revoke | PASS | 管理员真实 TTY 完成，Admission 工件 `0600` |
| Preflight / REVOKED | PASS | `PASSED`、`REVOKED`、Apply `BLOCKED`、exit 3 |
| 工件不可变性 | PASS | Plan、Approval、Revocation SHA-256 后续校验一致 |
| Worker 恢复 | PASS | UUID、Node UID、InternalIP、磁盘路径不变，恢复 `RUNNING_HEALTHY` / `reachable` / `Ready` |
| 恢复后集群 | PASS | 5/5 Node Ready，全部控制器 Ready，异常 Pod 为 0 |

详细命令输出、运行时 JSONL、前后快照和逐 VM Plan 结果保存在：

`/var/lib/upmctl/validation/20260720-host-safe-managed-rc4`

人工状态型验收的命令输出、前后基线、工件摘要和 89 项证据校验保存在：

`/var/lib/upmctl/validation/20260720-stateful-k8s-4-rc4`

本次允许的 `.upmctl` 控制状态新增严格限于：

- `plans/<planId>.json`；
- `approvals/by-plan/<planId>.json`；
- `admissions/<planId>.json`。

Plan ID：`plan-d611a7dd237f2e9ddebfdcdd5e49255309fc6137c711058781e23f659891deed`，SHA-256：`2b3831e4d1365e4e459ae5342b1afbd2116f2dd8c4ce1919e256270267db2b52`。

Approval ID：`approval-7ad382796bde1faba3e08a293b39a9301d29aee5f45c99a13ef40d2a89394a09`，SHA-256：`6c5c5e0514bdd27f95177aaa6153ff40488ec96f3f3f2a2101ae9b640e87c839`。Revocation SHA-256：`1fd09efc368b709bcacabdf2546d4c0cf3a587152ea51393b580c25a50d6b620`。

Managed State 摘要保持 `3d5cba7a5438760bcd05a37657742c87326c94aa3600c66e5db3936302014dcd`。报告未保存 reason 或 challenge。

## 6. 未通过或未执行的范围

以下能力仍未实现，不得因 rc4 host-safe 通过而宣称可用：

- 实际 VM start/stop/restart/create/destroy；
- Kubernetes Worker 添加和减少；
- Apply 执行器和 Operation 生命周期；
- Cluster 创建、扩缩、升级、重置和删除；
- Addon 安装、升级、配置和删除。

本次用于验收的 cordon、drain、Vagrant halt/up 和 uncordon 均是独立获批的外部维护动作，不得计为 upmctl 已实现的 Apply 或 VM 生命周期能力。

## 7. 运维观察

SSH 客户端报告当前连接未使用 post-quantum key exchange algorithm。该警告不影响本次 upmctl 功能验收，但建议在主机 SSH 维护窗口中评估服务端 OpenSSH 升级和加密策略。

## 8. 发布判定

rc4 已通过当前已实现范围的 host-safe 和人工状态型验收，可作为干净源码发布候选。正式创建 `upmctl-v0.1.0-rc4` 标签、推送分支或对外发布仍需要产品/发布负责人批准。
