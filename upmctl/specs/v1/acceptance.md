# Acceptance and Release Gates

## Phase 2b2a 当前发布必须通过的场景

| ID | Scenario |
| --- | --- |
| AC-CTX-001 | 从仓库根目录发现标准嵌套部署工作区 |
| AC-CTX-002 | 本地人类TTY可将安全完整的现有libvirt legacy workspace接管为MANAGED_VALID，且唯一写入为0700/0600的`.upmctl/state.json` |
| AC-CTX-003 | adopt绑定Vagrantfile、config、存在的受支持kubeconfig、全部预期机器UUID及actor/reason/typed-challenge审计证据 |
| AC-CTX-004 | adopt拒绝非TTY、Skill/MCP/pipe/CI、其他或混合provider、缺失/未知节点、非法/重复UUID、symlink和不安全config，且不运行外部命令 |
| AC-CTX-005 | 已有任意`.upmctl`状态时adopt原子拒绝且不覆盖；并发adopt只有一个成功，失败清理和readback回滚不删除替换工件 |
| AC-CLI-001 | version/capabilities在无集群时输出合法JSON |
| AC-CFG-001 | 当前NAT模板通过安全config validate |
| AC-CFG-002 | 不安全Ruby、重复字段、摘要或路径漂移被拒绝 |
| AC-CFG-003 | Managed State拒绝未知字段、重复JSON key、非法文件身份和重复UUID |
| AC-STATUS-001 | status聚合context、trust、config和VM摘要 |
| AC-VM-001 | VM list关联Vagrant、libvirt UUID/domain和Kubernetes Node |
| AC-VM-002 | 健康running VM执行 `plan vm start` 返回NOOP |
| AC-VM-006 | vm inspect返回身份、电源、InternalIP、资源、磁盘和SSH endpoint元数据 |
| AC-VM-007 | Vagrant/libvirt/Kubernetes不一致产生INCONSISTENT或finding |
| AC-VM-008 | kubeconfig缺失时仍返回VM观察且Kubernetes为unknown |
| AC-VM-009 | legacy工作区不执行Vagrantfile或kubeconfig |
| AC-VM-010 | Managed State登记但metadata缺失的libvirt domain仍可观察并标记不一致 |
| AC-VM-011 | SSH endpoint配置不能被解释为SSH可达；只有固定执行`vagrant ssh NODE -c true`成功后才标记`reachable`并参与完整健康判定 |
| AC-PLAN-001 | Phase 2b2a只开放 `plan vm start --node NODE` 生成Plan；其他Plan生成、Apply和Operation返回UPMCTL_NOT_IMPLEMENTED |
| AC-PLAN-002 | 普通Worker start产生R1规划结果，`k8s-1`/`k8s-2` start产生R2规划结果 |
| AC-PLAN-003 | 已满足start目标返回NOOP且不写Plan文件 |
| AC-PLAN-004 | 上下文、配置、观察完整性、身份或安全前置条件异常返回BLOCKED且不写Plan文件 |
| AC-PLAN-005 | ACTION_REQUIRED原子持久化不可执行Plan到 `.upmctl/plans/`，TTL严格为30分钟 |
| AC-PLAN-006 | NOOP、BLOCKED、ACTION_REQUIRED均返回合法planId和语义稳定planDigest；只有ACTION_REQUIRED存在Plan文件 |
| AC-PLAN-007 | 生成任一规划结果都不调用Vagrant、virsh、kubectl、SSH或其他目标环境变更命令 |
| AC-PLAN-008 | `plan get`读取合法Plan并返回原Plan；exit 0，且不运行外部命令、不写文件 |
| AC-PLAN-009 | 非法、缺失或不存在的Plan ID被安全拒绝；禁止绝对路径、分隔符和路径逃逸 |
| AC-PLAN-010 | Plan未知字段、重复key、尾随JSON、超限或非普通文件被完整拒绝，不返回部分内容 |
| AC-PLAN-011 | 已存Plan的内容、planDigest或planId被篡改时，严格Store读取返回顶层UPMCTL_PLAN_INVALID和exit 3，不返回部分Plan；内存校验投影使用UPMCTL_PLAN_TAMPERED blocker |
| AC-PLAN-012 | `plan validate`检查Schema、内容绑定、严格30分钟TTL以及Environment、Config和Managed State绑定，且不观察现场 |
| AC-PLAN-013 | `now == expiresAt`或晚于expiresAt时Plan返回EXPIRED和exit 3 |
| AC-PLAN-014 | Plan属于其他Environment或Workspace时返回ENVIRONMENT_MISMATCH和exit 3 |
| AC-PREFLIGHT-001 | 对有效未过期Plan执行preflight会重新读取Context、Config、Managed State并重新观察现场 |
| AC-PREFLIGHT-002 | 三个basis摘要一致时preflightStatus为PASSED，但applyDecision仍为BLOCKED且executionAvailable为false |
| AC-PREFLIGHT-003 | ConfigDigest变化时返回UPMCTL_CONFIG_DRIFT并指出configDigest差异 |
| AC-PREFLIGHT-004 | ManagedStateDigest变化时返回BLOCKED并指出Managed State漂移 |
| AC-PREFLIGHT-005 | ObservedStateDigest变化时返回BLOCKED和计划值/当前值，且不扩大或重写Plan |
| AC-PREFLIGHT-006 | 身份冲突、orphan或观察不完整时返回BLOCKED和结构化检查及blockers |
| AC-PREFLIGHT-007 | Plan在preflight检查过程中到期时最终结果为BLOCKED |
| AC-PREFLIGHT-008 | Preflight只运行既有只读观察命令，禁止Vagrant、virsh、kubectl和SSH变更 |
| AC-PREFLIGHT-009 | Apply和Operation命令继续返回UPMCTL_NOT_IMPLEMENTED；Preflight不创建执行状态或回退legacy脚本 |
| AC-PREFLIGHT-010 | Preflight取消或超时时exit 6，不写Operation或临时控制面状态，也不报告PASSED |
| AC-APR-001 | R1、R2、R3 ACTION_REQUIRED Plan都只能由本地人类控制TTY执行approval grant，非TTY、Skill和MCP调用被拒绝 |
| AC-APR-002 | grant/revoke拒绝subject、reason和actor参数；reason与typed challenge从控制TTY读取，UID、用户名、主机名和终端由OS观察 |
| AC-APR-003 | Approval完整绑定Plan ID、摘要、Environment、action、target、riskLevel、approvalScope和basis，并记录policyVersion、requestId及cliVersion |
| AC-APR-004 | Approval TTL严格为min(approvedAt+10m, plan.expiresAt)，confirmedAt等于approvedAt，now等于expiresAt时状态为EXPIRED |
| AC-APR-005 | 每个Plan最多原子创建一个Approval；已有、撤销、过期或无效Approval都不能覆盖、续期或重新批准 |
| AC-APR-006 | approval get/list只读、稳定排序并对已存Approval返回APPROVED、REVOKED、EXPIRED或INVALID；Plan无Approval时由Preflight返回MISSING，且Skill只能使用这两个审批查询 |
| AC-APR-007 | revoke APPROVAL_ID仅由本地人类控制TTY原子创建与Approval和Plan绑定的ApprovalRevocation，不修改Approval；已有Admission槽时拒绝覆盖 |
| AC-APR-008 | Revocation与未来Plan Claim共享admissions/<planId>.json互斥槽；Phase 2b2a不开放或创建Claim |
| AC-APR-009 | Approval、Revocation、Admission和状态投影拒绝symlink、路径逃逸、非普通文件、重复key、未知字段、尾随JSON、超限、摘要和绑定篡改 |
| AC-APR-010 | Preflight报告五态approvalStatus，但任意状态下applyDecision仍为BLOCKED、executionAvailable仍为false，且不创建Operation、lock或Claim |
| AC-SKILL-001 | Skill全程不直接调用底层变更命令 |

## V1 Future 场景

以下场景属于V1最终目标，但在Phase 2b2a中统一为`NOT_APPLICABLE_CURRENT_PHASE`，不得作为当前发布已实现或已验收能力。

| ID | Scenario | Phase 2b2a status |
| --- | --- | --- |
| AC-VM-003 | 普通Worker安全stop/start并恢复Ready | NOT_APPLICABLE_CURRENT_PHASE |
| AC-VM-004 | 独立stop/restart k8s-1被拒绝 | NOT_APPLICABLE_CURRENT_PHASE |
| AC-VM-005 | cluster stop/start按固定顺序且完成全链路verify | NOT_APPLICABLE_CURRENT_PHASE |
| AC-NODE-001 | 添加下一个连续Worker并通过Addon reconcile | NOT_APPLICABLE_CURRENT_PHASE |
| AC-NODE-002 | 删除无本地数据的最高编号Worker | NOT_APPLICABLE_CURRENT_PHASE |
| AC-NODE-003 | 有Bound LocalPV时拒绝删除 | NOT_APPLICABLE_CURRENT_PHASE |
| AC-SAFE-001 | stale plan、状态漂移和审批缺失均被拒绝 | NOT_APPLICABLE_CURRENT_PHASE |
| AC-ADDON-001 | LVM、Prometheus、Engine、Platform执行真实后置验证 | NOT_APPLICABLE_CURRENT_PHASE |
| AC-OP-001 | PARTIAL/INTERRUPTED保留journal并重新观察后resume | NOT_APPLICABLE_CURRENT_PHASE |

## 发布门禁

- 所有MUST需求有实现和测试映射。
- Go格式化、单元测试、静态检查和构建通过。
- JSON Schema示例可验证。
- 无默认密码、私钥、token或未脱敏日志进入输出。
- destructive E2E只能在专用fixture环境运行且需要显式开关。
- Legacy差异已记录，README没有扩大Spec范围。
- Phase 2b2a发布物在Phase 2b1基础上新增安全`environment adopt`及`approval grant/get/list/revoke`；不得宣称Apply、Operation、Executor、环境锁、Plan Claim或其他Plan生成能力可用。
- V1 Future场景在当前阶段只能标记为`NOT_APPLICABLE_CURRENT_PHASE`，不得报告为PASS。
- Preflight通过仍必须固定`applyDecision=BLOCKED`和`executionAvailable=false`；`approvalStatus`只能是MISSING、APPROVED、REVOKED、EXPIRED或INVALID。
- Plan审计、approval get/list和Preflight前后的控制面状态不得发生变化；adopt、grant、revoke只能写各自规定的单个本地控制面工件。
- 自动化、Skill和MCP不能写审批；本地OS和TTY观察证据不得被描述为独立强认证。

## 测试分层

- Unit：参数、状态归一化、摘要、策略、解析器。
- Contract：JSON/JSONL、错误码、退出码以及PlanInspection、PlanValidation、PreflightResult、Approval、Revocation、Admission和ApprovalStatus Schema。
- Adapter：fixture模拟Vagrant/virsh/kubectl输出。
- Integration：临时工作区、`.upmctl/plans/`和Approval/Admission原子持久化、安全读取、TTY策略、TTL/漂移和fake runner只读断言。
- E2E：专用Rocky Linux/libvirt宿主机上的真实环境。
