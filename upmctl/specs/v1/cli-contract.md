# upmctl V1 CLI Contract

## 全局参数

```text
--workspace PATH
--output text|json|jsonl
--request-id ID
--timeout DURATION
--log-file PATH
--no-color
```

默认输出为 `text`。自动化和 Skill 必须使用 `json` 或 `jsonl`。

以下帮助入口是稳定的纯文本接口：

```text
upmctl help
upmctl --help
upmctl -h
upmctl help approval|plan|vm|environment
```

帮助不发现或读取工作区，不调用Vagrant、virsh、kubectl或其他外部观察，不打开控制TTY，也不创建`.upmctl`状态。帮助始终写纯文本stdout并返回0，即使同时指定`--output json|jsonl`也不生成JSON envelope。默认不创建日志；只有显式指定`--log-file`时，才按本节相同的最小化生命周期日志契约记录规范化的`help`命令路径。未知帮助主题返回`UPMCTL_USAGE`和退出码2，仍不得访问环境。

`--log-file`是显式启用的本地运行生命周期日志；默认不创建日志。它独立于`--output`：业务结果仍只写stdout，错误仍只写stderr，日志以JSONL追加到指定文件，不能混入业务JSON envelope。

日志路径的父目录必须已经存在且是实际目录。CLI创建新日志文件时使用`0600`；已有日志文件必须是权限恰好为`0600`的普通文件。CLI拒绝符号链接、目录、设备、FIFO及其他非普通文件，也不会自动创建父目录或自动放宽/修正已有文件权限。日志初始化失败返回`UPMCTL_LOG_OPEN_FAILED`和退出码70；已经显式请求日志时，不允许静默降级为无日志运行。

每次调用最多写入一个`start`和一个终态事件。终态为`complete`或`error`：返回错误envelope时记录`error`及稳定`errorCode`；Preflight等具有非零策略退出码但成功返回业务envelope的调用仍记录`complete`及真实`exitCode`。

```json
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:03Z","requestId":"req-example","command":"preflight","event":"start","exitCode":null,"errorCode":null}
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:04Z","requestId":"req-example","command":"preflight","event":"complete","exitCode":3,"errorCode":null}
```

运行日志只允许记录以下字段：`logVersion/timestamp/requestId/command/event/exitCode/errorCode`。`command`必须是规范化命令路径（例如`approval grant`），不得包含参数或参数值。实现不得记录TTY输入、reason、typed challenge、Plan ID、Approval ID、workspace路径、环境快照、完整Plan/Approval/Admission或外部命令输出。该日志是故障关联与本机运行审计线索，不是Operation journal、安全审计日志或强身份认证证据。

## V1命令树

```text
upmctl version
upmctl capabilities
upmctl help
upmctl help approval|plan|vm|environment
upmctl environment adopt --environment-id ENV_ID --workspace PATH
upmctl context discover
upmctl config validate
upmctl preflight --plan-id PLAN_ID
upmctl status
upmctl vm list
upmctl vm status [NODE]
upmctl vm inspect NODE
upmctl vm ssh NODE
upmctl node list
upmctl node status [NODE]
upmctl plan cluster deploy|start|stop|restart|destroy
upmctl plan vm start|stop|restart --node NODE
upmctl plan node add
upmctl plan node remove --node NODE
upmctl plan addon install --name NAME
upmctl plan get PLAN_ID
upmctl plan validate PLAN_ID
upmctl approval grant --plan-id PLAN_ID
upmctl approval get APPROVAL_ID
upmctl approval list
upmctl approval revoke APPROVAL_ID
upmctl apply --plan-id ID
upmctl operation get|cancel|resume ID
upmctl verify
upmctl report generate
```

未实现的V1命令必须返回稳定的 `UPMCTL_NOT_IMPLEMENTED`，不能静默执行legacy脚本。

## 当前阶段可用性

Phase 2b2a当前可用：

```text
help
help approval|plan|vm|environment
version
capabilities
environment adopt --environment-id ENV_ID --workspace PATH
context discover
config validate
status
vm list
vm status [NODE]
vm inspect NODE
plan vm start --node NODE
plan get PLAN_ID
plan validate PLAN_ID
preflight --plan-id PLAN_ID
approval grant --plan-id PLAN_ID
approval get APPROVAL_ID
approval list
approval revoke APPROVAL_ID
```

`config validate`使用有限语法allowlist解析`config.rb`，不执行Ruby。Legacy工作区的validate/status只做被动文件观察，不调用Vagrant或加载kubeconfig。`status`表示上下文、配置和VM观察状态，不等于完整CNI、Addon或业务端点验收。

### Environment adopt契约

`environment adopt --environment-id ENV_ID`要求显式`--workspace`，且只能由本地人类控制TTY运行。`ENV_ID`必须匹配`env-[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`。命令先进行完全被动检查并在TTY展示canonical workspace、Vagrantfile/config和存在的受支持kubeconfig摘要，以及每个预期`k8s-N`的libvirt UUID；reason和随机typed challenge只从`/dev/tty`读取，UID、用户名和主机名由操作系统观察。Skill、MCP、pipe、CI和后台任务不得调用或模拟该命令。

接管仅支持安全完整`config.rb`声明的现有3-8节点libvirt Vagrant workspace。每个预期节点必须且只能存在`.vagrant/machines/k8s-N/libvirt/id` provider目录，UUID必须合法且全局唯一；缺失节点、未知节点、parallels/其他或混合provider、非法/重复UUID、symlink、路径逃逸、超限文件和不安全Ruby全部拒绝。命令不得运行Ruby、Vagrant、virsh、kubectl或修改VM、Guest、Kubernetes和Vagrant metadata。

成功时唯一持久变更是原子无覆盖创建`<workspace>/.upmctl/state.json`，目录/文件权限分别为`0700/0600`。状态绑定canonical workspace、Vagrantfile、config、所有存在的受支持kubeconfig、机器UUID，并记录`adoptedAt/actor/humanPresence/reason/requestId/cliVersion`。已有`state.json`或任意Plan、Approval、Admission、Operation、lock等控制状态时拒绝，绝不覆盖、合并或迁移。发布后严格readback失败时，只在文件仍是本次精确私有工件时安全回滚。

`plan vm start --node NODE`仍是Phase 2b2a唯一开放的变更规划命令，但它本身不执行变更：

- `NOOP`：目标已满足start目标，只返回包含 `planId/planDigest` 的审计结果。
- `BLOCKED`：安全规划条件不满足，只返回包含 `planId/planDigest` 的拒绝结果。
- `ACTION_REQUIRED`：返回规划结果，并将30分钟TTL的不可执行Plan原子写入 `<workspace>/.upmctl/plans/<planId>.json`。

只有 `ACTION_REQUIRED` 写Plan文件。该写入属于CLI控制面状态，不改变宿主机、VM、Guest或Kubernetes。普通Worker start标记R1，`k8s-1`/`k8s-2` start标记R2；所有R1、R2和R3目标变更都需要人工审批。

`plan get PLAN_ID`安全读取并完整校验已持久化Plan，返回`PlanInspection`。`expired`表示检查时Plan是否已经到期，`executionAvailable`固定为`false`。该命令不执行任何外部命令，也不写控制面状态。

`plan validate PLAN_ID`返回`PlanValidation`，检查Plan工件、严格30分钟TTL、当前时间、Environment、Config和Managed State绑定。它不重新观察目标环境，因此`observedStateBinding`固定为`NOT_CHECKED`，`executionAvailable`固定为`false`。篡改的Plan不返回部分校验结果，而是稳定错误。

`preflight --plan-id PLAN_ID`返回`PreflightResult`并重新执行只读现场观察。它比较Config、Managed State和Observed State的expected/current摘要，输出固定十项检查和稳定排序blockers。即使`preflightStatus=PASSED`，以下字段仍固定：

```text
applyDecision=BLOCKED
executionAvailable=false
approvalStatus=MISSING|APPROVED|REVOKED|EXPIRED|INVALID
```

Phase 2b2a的preflight只读取Approval和Admission状态，不得创建或修改它们；也不得创建Operation、journal、lock或Claim，不得修改Plan，不得执行任何变更命令。审批缺失、存在、撤销、过期或无效都不会解除固定的`applyDecision=BLOCKED`。

## Approval命令契约

`approval grant`和`approval revoke`是仅限本地人类控制TTY的控制面写操作。`grant`唯一的命令参数是`--plan-id PLAN_ID`，`revoke`唯一的业务参数是位置参数`APPROVAL_ID`；两者不得接受`--subject`、`--reason`、actor或其他身份参数。全局参数仍按全局契约处理。

CLI必须通过控制TTY展示Plan/Approval摘要，从控制TTY读取非空reason和typed challenge，并从操作系统观察UID、用户名、主机名及终端。Approval记录`approver.source=human-cli`、`approver.authMethod=interactive-tty`和`humanPresence.method=typed-challenge`；Revocation记录同等的actor与humanPresence证据。上述证据用于审计，但不是独立强认证，text和JSON输出不得将其描述为现实身份的密码学证明。

`approval grant`要求工作区为`MANAGED_VALID`，Plan完整、属于当前Environment、未过期、为`ACTION_REQUIRED`且riskLevel为R1、R2或R3。它为每个Plan原子创建唯一Approval，TTL为10分钟但不得超过Plan的`expiresAt`。如果`<workspace>/.upmctl/approvals/by-plan/<planId>.json`已经存在，命令必须拒绝，不能覆盖、续期或重新批准。

`approval revoke APPROVAL_ID`必须先安全解析并验证对应Approval及Plan。它不修改Approval，而是在`<workspace>/.upmctl/admissions/<planId>.json`原子创建Revocation。如果Admission槽已存在，命令必须拒绝，不能覆盖。Phase 2b2a不开放Plan Claim，因此CLI不得创建`PlanClaim`。

`approval get APPROVAL_ID`返回单个`ApprovalInspection`，其中包含完整不可变Approval、检查时间、状态和可选Revocation；`approval list`按`planId`稳定排序返回`ApprovalInspectionList`。两者只读，可供Skill使用。已存在Approval的状态规范化为`APPROVED|REVOKED|EXPIRED|INVALID`；`MISSING`用于按Plan执行的Preflight，因为按Approval ID无法查询一个不存在的工件。损坏、摘要错误、绑定错误、未知Admission或不安全文件身份必须报告`INVALID`，不能隐藏为不存在。

下列命令在Phase 2b2a必须返回 `UPMCTL_NOT_IMPLEMENTED`：

```text
plan vm stop|restart
plan cluster deploy|start|stop|restart|destroy
plan node add|remove
plan addon install
apply
operation get|cancel|resume
```

Plan Claim、Executor、环境锁和Operation journal同样不在Phase 2b2a实现。任何未开放命令都不得创建对应状态或回退执行legacy脚本。

## JSON Envelope

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "VMList",
  "requestId": "req-...",
  "timestamp": "2026-07-17T00:00:00Z",
  "data": {}
}
```

## 错误

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "Error",
  "requestId": "req-...",
  "timestamp": "2026-07-17T00:00:00Z",
  "error": {
    "code": "UPMCTL_CONTEXT_NOT_FOUND",
    "message": "deployment workspace was not found",
    "details": {},
    "remediation": "pass --workspace or run context discover"
  }
}
```

## 退出码

| Code | Meaning |
| --- | --- |
| 0 | SUCCEEDED/NOOP/ACTION_REQUIRED |
| 2 | 参数或用法错误 |
| 3 | BLOCKED：环境、前置条件或安全策略拒绝 |
| 4 | 外部依赖或外部命令失败 |
| 5 | PARTIAL |
| 6 | INTERRUPTED/CANCELLED |
| 70 | 内部错误 |

## 兼容策略

- `apiVersion` 在破坏性字段变化时升级。
- 新增可选字段不要求升级版本。
- 字段删除、重命名或语义变化必须走新版本。
- text 输出面向人类，不作为自动化解析契约。
