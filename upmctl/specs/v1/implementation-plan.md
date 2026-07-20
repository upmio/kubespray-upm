# Implementation Plan

## Phase 0：Spec与工程骨架

- 冻结Master Spec、需求ID、CLI/输出/错误契约和验收场景。
- 创建独立Go模块、Runner、Application Service和测试约定。

完成标准：文档可追踪；`upmctl version/capabilities`可构建和测试。

## Phase 1a：安全只读基础

- Context discover和Managed State摘要绑定。
- version、capabilities、基础VM list/status。
- text/JSON/JSONL和稳定错误Envelope。
- 未验证legacy工作区不得执行Vagrantfile或kubeconfig。

完成标准：只对摘要绑定的Managed Environment执行外部观察；所有能力明确标记preview或不可用。

## Phase 1b：完整只读控制面

- Context discover、config validate、status。
- Vagrant/libvirt/Kubernetes VM完整observed state聚合。
- Node InternalIP、可信SSH endpoint元数据、固定`vagrant ssh NODE -c true`可达性探针、libvirt资源规格和只读块设备清单。
- 角色和期望拓扑必须来自安全解析的锁定配置。
- text/JSON/JSONL和稳定错误。

不属于Phase 1b：Operation journal、磁盘归属/删除判断、固定`true`探针之外的SSH命令或Guest观察、preflight、plan/apply、Addon状态和legacy工作区外部命令观察。

完成标准：不修改环境即可被动识别legacy工作区；只有可信Managed Environment可执行外部只读观察，并通过fixture测试。

## Phase 2a：Plan-only核心

- 只开放 `plan vm start --node NODE`。
- 实现配置摘要、现场摘要、规范化Plan摘要、风险模型和不可变Plan。
- 普通Worker start为R1；`k8s-1`/`k8s-2` start为R2。
- 规划结果固定为 `NOOP`、`BLOCKED` 或 `ACTION_REQUIRED`，三者均返回 `planId/planDigest`。
- 只有 `ACTION_REQUIRED` 将30分钟TTL的不可执行Plan原子持久化到 `.upmctl/plans/`。
- `.upmctl/plans/` 是控制面状态，也是本阶段唯一允许的持久写入；不得修改宿主机、VM、Guest或Kubernetes。
- `plan vm stop/restart`、所有cluster/node/addon Plan、Approval、Apply和Operation继续返回 `UPMCTL_NOT_IMPLEMENTED`。
- 覆盖确定性、Schema、路径安全、只读性、NOOP、BLOCKED和风险分级测试。

完成标准：`plan vm start`只在 `MANAGED_VALID` 且观察安全完整时产生 `ACTION_REQUIRED` Plan；生成Plan不会调用任何目标环境变更命令；未开放能力不能执行或回退到legacy脚本。

## Phase 2b1：Plan审计与只读Preflight

- 开放`plan get PLAN_ID`，安全读取并校验Plan Schema、摘要、ID、路径和文件身份。
- 开放`plan validate PLAN_ID`，只检查Plan工件、严格30分钟TTL、Environment、Config和Managed State绑定；不重新观察现场。
- 开放`preflight --plan-id PLAN_ID`，重新执行只读现场观察并比较Config、Managed State和Observed State摘要。
- Preflight固定报告Plan完整性、时效、环境、三个basis、观察安全、Executor、并发控制和Approval子系统检查。
- 即使只读检查全部通过，也固定`applyDecision=BLOCKED`、`executionAvailable=false`和`approvalStatus=NOT_AVAILABLE`。
- 不创建Approval、Operation、journal、lock或其他控制面状态，不修改Plan，不执行目标环境变更。
- Approval、Apply、Operation和Phase 2a未开放的Plan生成命令继续返回`UPMCTL_NOT_IMPLEMENTED`。
- 覆盖Plan路径逃逸、symlink、非普通文件、大小限制、重复key、未知字段、尾随JSON、摘要/ID篡改、TTL边界、环境不匹配、三类漂移、观察不完整、超时取消和只读性测试。

完成标准：三个新增命令只提供审计和执行前检查；Preflight重新观察但不改变环境或控制面状态；任何结果都不能进入Approval、Apply或Operation状态机。

## Phase 2b2a：人工Approval控制面

- 开放`approval grant/get/list/revoke`；`grant/revoke`仅允许本地人类控制TTY调用，`get/list`为只读接口。
- 所有R1、R2、R3目标变更都必须人工批准，不提供自动批准。
- grant只接受`--plan-id`，revoke只接受`APPROVAL_ID`；reason和typed challenge从控制TTY读取，actor由操作系统观察，禁止subject/reason/actor参数。
- 本地OS/TTY证据只用于审计，不冒充独立强身份认证。
- 每个Plan最多一个Approval，TTL为`min(approvedAt + 10m, plan.expiresAt)`，禁止覆盖、续期或重新批准同一Plan。
- Approval原子保存到`.upmctl/approvals/by-plan/<planId>.json`；Revocation与未来Claim共享`.upmctl/admissions/<planId>.json`原子互斥槽。
- 只开放Revocation写入；Plan Claim仅冻结Schema和内部边界，不开放命令且本阶段不得创建。
- Preflight读取审批状态并返回`MISSING|APPROVED|REVOKED|EXPIRED|INVALID`，但仍固定`applyDecision=BLOCKED`和`executionAvailable=false`。
- Skill可调用`approval get/list`，但Skill和MCP禁止调用或暴露`grant/revoke`。
- Apply、Executor、Operation journal、环境锁、cancel和resume继续返回`UPMCTL_NOT_IMPLEMENTED`，不得创建执行状态。

完成标准：Approval和Revocation工件具备严格Schema、摘要/Plan/Environment绑定、路径安全、原子无覆盖和TTL边界测试；非TTY及自动化写审批被拒绝；只读查询不写状态；任何审批状态都不能执行Plan或创建Claim、Operation和锁。

## Phase 2b2b：Apply和Operation核心

- Plan Claim与有效Approval绑定，并通过Admission原子槽防止撤销/执行竞态和Plan重放。
- Apply前重新观察并校验TTL、配置摘要、现场摘要和审批范围。
- Operation journal、工作区锁、cancel和重新观察后的resume。
- stale plan、漂移、并发、审批缺失及PARTIAL/INTERRUPTED测试。

完成标准：Apply只能执行获批Plan中的步骤，不得扩大范围；所有执行均可审计，失败和中断不能被报告为成功。

## Phase 3：VM生命周期

- VM start、普通Worker stop/restart。
- Cluster start/stop/restart顺序和验收。
- 人类TTY SSH。

## Phase 4：Cluster deploy和destroy

- 宿主机预检和准备适配器。
- 工作区、网络、Vagrant/Kubespray部署。
- kubeconfig、verify和destroy证据。

## Phase 5：Worker add/remove

- 连续拓扑planner。
- Guest baseline、facts、scale.yml。
- PDB/存储检查、remove_node.yml和内部VM destroy。

## Phase 6：Addon

- LVM allowlist和初始化。
- Prometheus、UPM Engine、UPM Platform、独立Nginx计划。
- 实时discovery和endpoint验收。

## Phase 7：Agent交付

- Codex Skill、CLI兼容检查和固定工作流测试。
- MCP Schema/adapter Preview；MCP Server不阻塞V1。
