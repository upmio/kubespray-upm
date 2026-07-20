# State, Plan and Safety

## 风险等级

| Level | Examples | Approval |
| --- | --- | --- |
| R0 | discover/status/inspect/verify/report | 不需要 |
| R1 | 启动已停止的普通Worker VM | 人工批准 |
| R2 | `k8s-1`/`k8s-2` start、Worker stop/restart、node add、Addon install | 人工批准 |
| R3 | cluster stop/restart/destroy、node remove、Bridge、磁盘初始化、`k8s-2` stop/restart | 明确人工批准 |

从Phase 2b2a开始，所有会改变目标环境的R1、R2、R3 Plan均必须由人类批准。不存在“低风险自动批准”；Skill、MCP、后台进程和非交互调用都不能创建或撤销审批。

## Legacy Environment Adoption

Legacy工作区默认只能被动读取，因为Vagrant观察会执行Vagrantfile，Kubernetes观察可能执行kubeconfig credential plugin。把它升级为`MANAGED_VALID`属于信任边界变更，必须由本地人类控制TTY执行`environment adopt`，并记录OS actor、reason、typed challenge、request ID、CLI版本和UTC时间；PTY证据用于审计，不是现实身份的强认证。

接管前必须安全解析完整config，并闭合集合验证`.vagrant/machines`：节点只允许config声明的`k8s-1..N`，每个节点只能有真实`libvirt` provider目录和单一合法唯一UUID。拒绝其他provider、未知/缺失节点、symlink、路径逃逸和任何既有`.upmctl`状态。接管不得执行任何外部命令或修改目标环境。

接管状态以`.upmctl/state.json`原子无覆盖发布，绑定canonical workspace、Vagrantfile/config、存在的受支持kubeconfig及全部机器UUID。目录和文件固定`0700/0600`。发布失败清理本次新建空目录；readback失败只删除仍与本次内容和文件身份完全相同的工件，不触碰替换文件或并发创建的其他控制状态。

## Plan

### Phase 2a：Plan-only

Phase 2a只开放：

```text
upmctl plan vm start --node NODE
```

该命令只在 `MANAGED_VALID` 工作区中基于安全完整的当前观察生成规划结果。它不得执行Vagrant、virsh、kubectl、SSH或其他会改变宿主机、VM、Guest或Kubernetes状态的命令，也不得创建Operation或审批记录。

规划结果为以下三种之一：

| Result | Meaning | Persistence |
| --- | --- | --- |
| `NOOP` | 目标VM已经处于满足start目标的健康状态 | 只返回审计结果，不写Plan文件 |
| `BLOCKED` | 上下文、配置、观察完整性、身份一致性或安全前置条件不满足 | 只返回拒绝原因，不写Plan文件 |
| `ACTION_REQUIRED` | 目标尚未运行且允许规划start步骤 | 原子写入不可执行Plan |

三种结果都必须返回格式稳定的 `planId` 和语义稳定的 `planDigest` 以便审计；时间变化可以产生新的Plan实例ID，但不能改变相同语义输入的 `planDigest`。只有 `ACTION_REQUIRED` 的Plan会保存到 `<workspace>/.upmctl/plans/<planId>.json`。该目录是 `upmctl` 控制面状态，不是目标环境；这是Phase 2a唯一允许的持久写入，不能被解释为对VM或集群的修改。

持久化必须拒绝symlink、非普通目录、路径逃逸和覆盖已有同名Plan，并使用同目录排他临时文件、文件同步和原子无覆盖发布。Plan创建后30分钟过期，即 `expiresAt = createdAt + 30m`；过期Plan仍可用于审计，但未来Apply不得执行。

普通Worker start为R1；`k8s-1`或`k8s-2` start为R2。Phase 2a没有审批或执行能力，因此风险等级只用于Plan披露，不能让Plan变为可执行。

`plan vm stop/restart`、所有 `plan cluster/node/addon`、approval、`apply` 和 `operation` 在Phase 2a均必须返回稳定的 `UPMCTL_NOT_IMPLEMENTED`。

### Plan内容

Plan必须包含：

- planId、createdAt、expiresAt、riskLevel。
- ConfigDigest、ManagedStateDigest、ObservedStateDigest。
- 目标、受影响资源、前置条件和拒绝条件。
- 不可逆动作、数据影响和预计中断。
- 有序步骤、每步后置条件和验收引用。
- 所需 approval scope。

除上述 `.upmctl/plans/` 控制面状态外，`plan`阶段禁止任何持久写入；临时数据只能写入临时目录且不能改变目标系统。

## Phase 2b1：Plan审计与只读Preflight

Phase 2b1只在Phase 2a基础上开放：

```text
upmctl plan get PLAN_ID
upmctl plan validate PLAN_ID
upmctl preflight --plan-id PLAN_ID
```

`plan get`从当前Managed Environment的`.upmctl/plans/`安全读取Plan，严格校验Plan ID、文件身份、大小、JSON结构、Plan Schema、`planDigest`和`planId`内容绑定，返回原Plan、检查时间、过期标志和固定的`executionAvailable=false`。它不得运行外部命令或写入任何文件。

`plan validate`只检查本地控制面工件以及当前工作区的Environment、Config和Managed State绑定；它不重新观察Vagrant、libvirt或Kubernetes，因此`observedStateBinding`固定为`NOT_CHECKED`。Plan在`now == expiresAt`时已经过期。校验结果不能被解释为批准或可执行。

`preflight`必须重新读取Context、Config、Managed State和Plan，并通过现有只读观察适配器重新观察Vagrant、libvirt和Kubernetes。它比较Plan中三个basis摘要与当前摘要，拒绝过期、环境不匹配、配置或Managed State漂移、现场漂移、身份冲突、orphan和观察不完整。检查期间到期的Plan最终也必须被拒绝；不得重写Plan或自动扩大其步骤。

Preflight固定输出以下十项有序检查：

```text
PLAN_INTEGRITY
PLAN_TIME_VALID
ENVIRONMENT_MATCH
CONFIG_MATCH
MANAGED_STATE_MATCH
OBSERVATION_SAFE
OBSERVED_STATE_MATCH
EXECUTOR_CAPABILITY
CONCURRENCY_CONTROL
APPROVAL_SUBSYSTEM
```

前七项报告Plan和现场是否满足只读检查；后三项明确本阶段的执行器、并发控制和审批子系统均未开放。即使前七项全部通过，结果也必须是：

```text
preflightStatus=PASSED
applyDecision=BLOCKED
executionAvailable=false
approvalStatus=NOT_AVAILABLE
```

Phase 2b2a开始该字段扩展为五态。审批状态不改变`preflightStatus`的现场安全语义，也不解除固定的`applyDecision=BLOCKED`。

`PASSED`只表示已实现的只读检查通过，不表示`READY_TO_APPLY`。Phase 2b1不得创建或修改Approval、Operation、journal、lock、Plan或其他控制面状态；也不得执行Vagrant/virsh/Kubernetes/SSH变更命令。

`apply`、所有approval和operation命令，以及Phase 2a未开放的Plan生成命令，在Phase 2b1继续返回稳定的`UPMCTL_NOT_IMPLEMENTED`，不得创建状态或回退到legacy脚本。

## Phase 2b2a：人工Approval控制面

Phase 2b2a只新增以下命令：

```text
upmctl approval grant --plan-id PLAN_ID
upmctl approval get APPROVAL_ID
upmctl approval list
upmctl approval revoke APPROVAL_ID
```

`grant`和`revoke`只接受本地控制终端TTY上的直接人类调用。CLI从操作系统观察UID、用户名、主机名和终端，形成`approver/actor`审计上下文；这些字段以及reason都不能通过CLI参数、环境变量、配置文件、stdin管道、Skill或MCP传入。Reason和typed challenge必须从控制TTY同步读取。

```text
approver.source=human-cli
approver.authMethod=interactive-tty
humanPresence.method=typed-challenge
```

这是一条可审计的人机边界，不是强身份认证或授权系统；操作系统观察值不能证明对应现实身份。无控制TTY、Skill、MCP和其他自动化写审批调用必须被拒绝；重定向stdin不能成为reason或challenge输入源。`approval get/list`是只读接口，可由Skill调用来解释当前状态；它们不得改变Approval、Admission、Plan或目标环境。

每个Plan最多创建一个Approval，保存为`<workspace>/.upmctl/approvals/by-plan/<planId>.json`。Approval必须完整复制并绑定Plan的ID、摘要、Environment、action、target、riskLevel、approvalScope和basis，并记录`policyVersion=human-approval-v1`、`requestId`及`cliVersion`。只允许批准完整、未过期、`ACTION_REQUIRED`且风险为R1/R2/R3的Plan。Approval有效期为：

```text
min(approvedAt + 10m, plan.expiresAt)
```

`humanPresence.confirmedAt`必须等于`approvedAt`，`expiresAt`必须严格晚于`approvedAt`。已有Approval无论当前为APPROVED、REVOKED、EXPIRED或INVALID，都不能被覆盖或重新授予；需要重新规划并批准新Plan。Approval文件必须使用与Plan相同的路径身份、防symlink、大小、重复key、未知字段、摘要绑定和原子无覆盖发布规则。

撤销不是修改Approval。`revoke`在`<workspace>/.upmctl/admissions/<planId>.json`原子无覆盖写入`ApprovalRevocation`。未来Apply将在同一Admission槽写入`PlanClaim`；同一Plan的Revocation和Claim互斥，从而避免“撤销”和“开始执行”并发成功。Phase 2b2a只定义并校验PlanClaim Schema，不开放任何Claim命令，也不得创建PlanClaim。

Approval状态规范化为：

| Status | Meaning |
| --- | --- |
| `MISSING` | 没有Approval文件 |
| `APPROVED` | Approval完整、绑定正确、未过期，且Admission槽不存在 |
| `REVOKED` | Admission槽包含与Approval和Plan正确绑定的ApprovalRevocation |
| `EXPIRED` | Approval完整但`now >= expiresAt` |
| `INVALID` | 文件身份、Schema、摘要、时间边界、Plan绑定或Admission内容无效 |

状态判定必须保守：无法安全证明时返回`INVALID`，不能降级为`MISSING`或`APPROVED`。`preflight`继续重新观察现场，并把`approvalStatus`扩展为上述五态；因为Executor、锁、Operation journal和Claim仍未开放，任何状态下都固定：

```text
applyDecision=BLOCKED
executionAvailable=false
```

Phase 2b2a仍不支持`apply`、Operation、Executor、环境锁、Plan Claim或目标环境变更。这些命令和内部能力必须保持关闭，不得创建`operations/`、lock或Claim，也不得回退调用legacy脚本。

## Phase 2b2b：Apply和Operation核心

Phase 2b2b才实现Plan Claim、Apply、Operation journal、锁、cancel和resume。在此前阶段这些能力没有兼容执行路径，也不得回退调用legacy脚本。

Apply前必须重新观察现场并拒绝：计划过期、摘要变化、身份不一致、并发操作、审批缺失。Apply只能执行Plan中的步骤，不能在运行时自行扩大范围。

## 状态机

```text
PLANNED -> AWAITING_APPROVAL -> RUNNING
RUNNING -> SUCCEEDED | NOOP | PARTIAL | BLOCKED | FAILED | INTERRUPTED | CANCELLED
PARTIAL/INTERRUPTED -> re-observe -> RUNNING
```

该状态机是Phase 2b2b及后续执行阶段的目标契约。Phase 2a/2b1最多产生、读取和检查不可执行的 `PLANNED` 审计工件；Phase 2b2a可以创建或撤销Approval，但不进入`RUNNING`，也不创建Operation journal。

- `PARTIAL`：至少一个有副作用阶段完成，但目标未完全达到；调用方按失败处理。
- `INTERRUPTED`：无法确认最终状态；恢复前必须重新观察，不能直接重放。
- `BLOCKED`：安全前置条件不满足，拒绝继续。
- `cancel`只阻止未开始阶段，不承诺回滚。

## 漂移

以下任一不一致均阻止变更：Managed State、`config.rb`、Vagrant metadata、domain UUID、磁盘归属、Kubernetes Node身份、安装期网络/CNI/版本。人工SSH后必须重新观察。
