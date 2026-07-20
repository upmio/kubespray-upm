# upmctl 使用手册

本文面向在测试环境中使用 `upmctl` 的平台工程师、Kubernetes 工程师和运维工程师，说明 Phase 2b2a 已交付命令的正确用法。

> **重要边界**：当前版本提供legacy工作区安全接管、工作区发现、配置校验、VM/集群只读观察、VM 启动计划、Plan 校验、只读 Preflight 以及人工 Approval 管理。它**不会启动、停止或重启 VM**，不会增减 Kubernetes 节点，也不会执行 Apply。Plan 和 Approval 都是控制面审计工件，不代表目标环境已经发生变更。

## 1. 开始之前

在尚未安装Vagrant、OpenSSH、virsh、kubectl或尚未准备工作区时，可以先使用离线帮助：

```bash
upmctl help
upmctl --help
upmctl -h
upmctl help environment
upmctl help vm
upmctl help plan
upmctl help approval
```

这些帮助入口只输出稳定纯文本，不发现或读取工作区，不执行外部命令，不打开控制TTY，也不创建`.upmctl`控制状态。即使传入`--output json`，帮助仍为面向人类的纯文本；默认不写日志，只有显式传入`--log-file`时才写最小化生命周期日志。`help approval`用于确认人类TTY审批边界，`help plan`用于确认当前Apply和执行能力仍关闭。

完成部署后，先确认二进制可执行并检查当前能力，而不要根据文档或版本号推测命令是否可用：

```bash
command -v upmctl
upmctl version
upmctl capabilities --output json
```

真实环境观察还要求当前用户能够运行环境中的只读依赖命令：

- `vagrant status --machine-readable`；
- 对 running VM 执行 `vagrant ssh-config NODE` 和固定的 `vagrant ssh NODE -c true` 可达性探针；
- `virsh list`、`virsh domstate`、`virsh dominfo` 和 `virsh domblklist`；
- 使用工作区 kubeconfig 执行 Kubernetes API 只读查询；
- 读取工作区中的 `Vagrantfile`、`vagrant/config.rb`、`.vagrant/`、kubeconfig 和 `.upmctl/state.json`。

`environment adopt`及Approval的`grant/revoke`要求人类直接使用本机控制终端；管道、后台任务、CI、Skill、MCP或伪造TTY均不能完成这些信任/审批写操作。

## 2. 核心概念

### 2.1 Deployment Workspace

Workspace 是一个实际部署实例的根目录，至少包含：

```text
<workspace>/
├── Vagrantfile
└── vagrant/
    └── config.rb
```

推荐每次都显式传递绝对路径：

```bash
export WORKSPACE=/absolute/path/to/deployment/workspace
upmctl context discover --workspace "$WORKSPACE" --output json
```

如果未传 `--workspace`，CLI 会按以下顺序发现：

1. 从当前目录向父目录查找 deployment workspace；
2. 找到仓库根目录后，检查标准目录 `vagrant_setup_scripts/kubespray-upm`；
3. 只能发现仓库但找不到 workspace 时，返回 repository-only 上下文；
4. 都无法发现时返回错误。

自动化、Skill 和生产式运维流程应始终显式传递 `--workspace`，防止在错误环境中读取状态。

### 2.2 Managed Environment 与信任状态

`.upmctl/state.json` 把 workspace、关键文件摘要和 VM 的 libvirt UUID 绑定为一个 Managed Environment。

| Trust | 含义 | 允许的行为 |
| --- | --- | --- |
| `MANAGED_VALID` | Managed State 完整，路径和摘要绑定有效 | 可执行完整只读 VM 观察、Plan、Preflight 和 Approval 流程 |
| `LEGACY_UNTRUSTED_READONLY` | 没有 `.upmctl/state.json` | 只允许被动 `context discover`、`config validate` 和有限 `status`；不加载 Ruby、Vagrantfile 或 kubeconfig 执行观察 |
| `INVALID` | Managed State 存在但损坏、摘要漂移或身份不匹配 | 停止目标环境操作，按错误 remediation 修复 |
| `UNKNOWN` | 只找到仓库或上下文不足 | 显式指定正确 workspace |

不要手工编辑 `.upmctl/state.json`、Plan、Approval 或 Admission 文件来绕过校验。

Legacy环境可由人类运行以下命令接管：

```bash
upmctl environment adopt --environment-id env-lab-01 --workspace "$WORKSPACE"
```

Adopt只验证安全完整config和现有libvirt Vagrant metadata并原子创建Managed State；不运行Vagrant、virsh、kubectl，不修改VM或Kubernetes。TTY会显示所有绑定摘要/UUID并读取reason和typed challenge。已有任何`.upmctl`状态、非libvirt/mixed provider、缺失/未知节点、非法/重复UUID或symlink都会被拒绝。

### 2.3 Observation

`status`、`vm list`、`vm status` 和 `vm inspect` 会关联以下只读事实：

- `vagrant` 机器状态；
- SSH endpoint 元数据和固定 `vagrant ssh NODE -c true` 的可达性结果；
- libvirt domain、UUID、电源状态、CPU、内存和磁盘；
- Kubernetes Node 是否存在、Ready 状态和 Internal IP；
- Vagrant、Managed State、libvirt 和 Kubernetes 之间的身份一致性。

`ssh-config` 只证明 endpoint 已配置，不能证明 SSH 可达。只有固定探针成功后 `sshState` 才是 `reachable`，running VM 才可能成为 `RUNNING_HEALTHY`；失败时为 `unavailable`/`RUNNING_DEGRADED`。该探针不接受用户命令、不读取 Guest 文件、不使用 `sudo`。`RUNNING_HEALTHY` 只表示当前实现覆盖的观察项一致，不等于 CNI、Addon、存储、Ingress 或业务服务已完成端到端验收。

### 2.4 Plan

当前唯一可生成的变更计划是：

```text
vm.start -> VirtualMachine/k8s-1 ... k8s-8
```

`plan vm start` 生成三种 disposition：

| Disposition | 含义 | 是否保存 Plan | 是否修改 VM |
| --- | --- | --- | --- |
| `NOOP` | VM 已满足 start 目标 | 否 | 否 |
| `BLOCKED` | 安全前置条件不满足 | 否 | 否 |
| `ACTION_REQUIRED` | 需要后续变更 | 是，保存 30 分钟 | 否 |

Plan 包含 Environment、Config、Managed State 和 Observed State 摘要，且绑定创建时间和过期时间。普通 Worker start 为 R1；`k8s-1` 和 `k8s-2` start 为 R2。

### 2.5 Preflight

`preflight` 会重新执行只读现场观察，并把当前三项 basis 与 Plan 进行比较：

- Config；
- Managed State；
- Observed State。

Phase 2b2a 中，即使所有已实现检查通过，结果也固定包含：

```text
preflightStatus=PASSED
applyDecision=BLOCKED
executionAvailable=false
```

因此 `PASSED` 只能解释为“只读检查通过”，不能解释为“可以执行”或“环境已经变更”。

### 2.6 Approval

Approval 是对某个不可变 Plan 的本机人工意图证据。它绑定 Plan 摘要、Environment、action、target、risk、basis、reason、OS actor 观察和 typed challenge。

- 所有 R1、R2、R3 计划都要求人工审批；
- 每个 Plan 最多一个 Approval，不能覆盖、续期或重复批准；
- Approval 有效期最长 10 分钟，且不会超过 Plan 的过期时间；
- Revocation 不修改原 Approval，而是创建独立 Admission 工件；
- TTY 和 OS actor 信息用于审计，不是密码学身份认证；
- Approval 为 `APPROVED` 时，Apply 仍然不可用。

## 3. 当前命令树

Phase 2b2a 已实现：

```text
upmctl version
upmctl capabilities
upmctl environment adopt --environment-id ENV_ID --workspace PATH
upmctl context discover
upmctl config validate
upmctl status
upmctl vm list
upmctl vm status [NODE]
upmctl vm inspect NODE
upmctl plan vm start --node NODE
upmctl plan get PLAN_ID
upmctl plan validate PLAN_ID
upmctl preflight --plan-id PLAN_ID
upmctl approval grant --plan-id PLAN_ID
upmctl approval get APPROVAL_ID
upmctl approval list [--plan-id PLAN_ID]
upmctl approval revoke APPROVAL_ID
```

全局参数：

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `--workspace PATH` | 自动发现 | 绑定准确的 deployment workspace |
| `--output text\|json\|jsonl` | `text` | 选择人类或机器输出 |
| `--request-id ID` | 自动生成 | 为一次调用指定可关联的请求 ID |
| `--timeout DURATION` | 无全局超时 | 限制外部只读观察时间，例如 `30s`、`2m` |
| `--log-file PATH` | 关闭 | 将本次调用的最小生命周期事件追加到独立 JSONL 日志 |
| `--no-color` | 无颜色 | 当前 text 本身不使用颜色；保留该开关便于脚本兼容 |

全局参数可以出现在命令参数前后，但为便于审计，建议统一放在业务参数之后。

`--log-file` 必须显式指定，CLI 默认不创建运行日志。它独立于 `--output`：业务结果仍只写 stdout，错误仍只写 stderr，日志不会混入业务 JSON envelope。日志父目录必须由运维人员预先创建为真实目录；新日志文件由 CLI 以 `0600` 创建，已有文件必须是权限恰好为 `0600` 的非 symlink 普通文件。

运行日志只包含 `logVersion`、UTC `timestamp`、`requestId`、规范化命令名、`event`、`exitCode` 和稳定 `errorCode`。它不记录命令参数值、workspace、Plan/Approval ID、完整 Plan/Approval/Admission、审批 reason、typed challenge、TTY 输入、环境快照或外部命令输出。显式请求日志后，如果日志路径不安全、父目录不存在、权限不正确或文件不可写，命令会失败，不会静默退回无日志模式。

## 4. Text、JSON 和 JSONL

### 4.1 Text

Text 面向终端中的人类阅读：

```bash
upmctl vm list --workspace "$WORKSPACE"
```

Text 字段和排版不是自动化兼容契约，不要用 `grep`、`awk` 或固定列位置解析。

### 4.2 JSON

JSON 面向脚本和 Agent。成功输出写入 stdout，并使用统一 envelope：

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "VMList",
  "requestId": "req-...",
  "timestamp": "2026-07-17T00:00:00Z",
  "data": {}
}
```

错误输出写入 stderr，结构为：

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "Error",
  "requestId": "req-...",
  "timestamp": "2026-07-17T00:00:00Z",
  "error": {
    "code": "UPMCTL_WORKSPACE_NOT_FOUND",
    "message": "deployment workspace was not found",
    "details": {},
    "remediation": "pass --workspace for the managed deployment"
  }
}
```

自动化必须同时保留 stdout、stderr、退出码和 `requestId`。

### 4.3 JSONL

`jsonl` 将单次调用的 envelope 压缩为一行，便于追加到结构化日志：

```bash
upmctl status \
  --workspace "$WORKSPACE" \
  --output jsonl \
  --request-id "operator-$(date +%Y%m%dT%H%M%S)" \
  >>upmctl-audit.jsonl
```

当前每次调用只输出一个结果对象；`jsonl` 不表示命令提供持续事件流。

这里的 `--output jsonl` 是业务结果格式，与 `--log-file` 的最小运行生命周期日志是两个独立数据流。前者写 stdout，后者只追加到显式指定的日志文件。

## 5. 十分钟快速上手

### 5.1 建立固定变量

```bash
export WORKSPACE=/absolute/path/to/deployment/workspace
export UPMCTL_OUTPUT=json
```

`UPMCTL_OUTPUT` 只是本文示例变量，CLI 不读取该环境变量；调用时仍需显式传 `--output "$UPMCTL_OUTPUT"`。

### 5.2 检查二进制和能力

```bash
upmctl version --output "$UPMCTL_OUTPUT"
upmctl capabilities --output "$UPMCTL_OUTPUT"
```

至少确认以下 capability 为 `available=true`：

```text
context.discover
config.validate
status
vm.observe.basic
vm.observe.full
vm.inspect
plan.vm.start
plan.get
plan.validate
preflight.plan
approval.manage
```

同时确认以下能力仍为 `false`：

```text
plan.apply
executor.vm.start
vm.mutate
node.scale
mcp.server
```

### 5.3 准备可选运行日志

默认情况下可以跳过本节。如果需要把 CLI 调用与工单、测试记录或自动化任务关联，先预创建私有目录和 `0600` 日志文件：

```bash
export UPMCTL_LOG_DIR="$HOME/.local/state/upmctl"
export UPMCTL_LOG_FILE="$UPMCTL_LOG_DIR/runtime.jsonl"

install -d -m 700 "$UPMCTL_LOG_DIR"
touch "$UPMCTL_LOG_FILE"
chmod 600 "$UPMCTL_LOG_FILE"
```

使用同一个非敏感 `requestId` 关联业务输出、stderr 和运行日志：

```bash
REQUEST_ID=INC-20260717-001

upmctl status \
  --workspace "$WORKSPACE" \
  --output json \
  --request-id "$REQUEST_ID" \
  --log-file "$UPMCTL_LOG_FILE" \
  >/tmp/upmctl-status.json \
  2>/tmp/upmctl-status-error.json

jq --arg requestId "$REQUEST_ID" 'select(.requestId == $requestId)' \
  "$UPMCTL_LOG_FILE"
```

日志示例：

```json
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:03Z","requestId":"INC-20260717-001","command":"status","event":"start","exitCode":null,"errorCode":null}
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:04Z","requestId":"INC-20260717-001","command":"status","event":"complete","exitCode":0,"errorCode":null}
```

不要把 Plan ID、Approval ID、reason、challenge、用户名、凭据或其他敏感信息放入 `--request-id`。运行日志是故障关联线索，不是 Operation journal，也不能替代业务 JSON 和控制状态工件。

如果显式使用 `--log-file`，以下错误会在业务命令执行前或结束写日志时令调用失败：

- `UPMCTL_LOG_OPEN_FAILED`：父目录不存在、路径是 symlink/非普通文件、已有文件不是 `0600` 或当前用户无法打开；
- `UPMCTL_LOG_WRITE_FAILED`：磁盘空间、只读文件系统、配额或运行中写入失败。

修复日志路径后重新运行原命令，不要通过放宽文件权限或使用共享日志目录绕过检查。

### 5.4 发现并确认 workspace

```bash
upmctl context discover \
  --workspace "$WORKSPACE" \
  --output "$UPMCTL_OUTPUT" \
  --request-id onboarding-context
```

检查：

```bash
upmctl context discover --workspace "$WORKSPACE" --output json \
  | jq '.data | {workspace, environmentId, managed, trust, source, findings}'
```

继续 Plan 流程前，应满足：

```text
.data.workspace == $WORKSPACE 的规范化路径
.data.managed == true
.data.trust == "MANAGED_VALID"
.data.environmentId 非空
```

### 5.5 校验配置

```bash
upmctl config validate --workspace "$WORKSPACE" --output json \
  | tee /tmp/upmctl-config-validation.json
```

应确认：

```text
.data.validation.safe == true
.data.validation.valid == true
.data.validation.complete == true
```

解析器只接受受限语法 allowlist，不执行 `config.rb` 中的 Ruby 代码。出现 unsupported/unsafe finding 时，应修改配置来源并重新生成受管状态，不要尝试让 CLI 执行 Ruby。

### 5.6 查看总体状态

```bash
upmctl status \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s \
  | tee /tmp/upmctl-status.json
```

快速查看关键项：

```bash
jq '.data | {
  mode,
  health,
  observationComplete,
  managedState,
  cluster,
  vmSummary,
  findings
}' /tmp/upmctl-status.json
```

`health=HEALTHY` 仍需结合 `observationComplete=true` 和空/可接受的 findings 解读。

### 5.7 查看 VM

```bash
upmctl vm list --workspace "$WORKSPACE" --output text

upmctl vm status k8s-3 \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s

upmctl vm inspect k8s-3 \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s \
  | tee /tmp/upmctl-k8s-3.json
```

重点检查：

```bash
jq '.data | {
  name,
  expected,
  managed,
  health,
  consistency,
  vagrantState,
  libvirtState,
  kubernetesState,
  identity,
  power,
  network,
  resources,
  sources,
  findings
}' /tmp/upmctl-k8s-3.json
```

常见 VM health：

| Health | 处理方式 |
| --- | --- |
| `RUNNING_HEALTHY` | 当前观察项一致，且固定 SSH 探针成功；仍按需要验证业务层 |
| `RUNNING_DEGRADED` | 查看 sources 和 findings；endpoint 已配置但固定 SSH 探针未成功时也属于此状态 |
| `STOPPED` | 可以生成 `vm.start` Plan，但当前不会执行 |
| `MISSING` | 核实配置、Managed State、Vagrant metadata 和 libvirt inventory |
| `ORPHANED` | 存在非预期资源，停止计划流程 |
| `INCONSISTENT` | 身份或多源状态矛盾，停止计划流程 |
| `UNKNOWN` | 观察来源不完整，先恢复依赖可见性 |

## 6. Plan、Preflight 与 Approval 完整示例

以下流程用于真实测试环境中的控制面验证。它会创建 `.upmctl/plans`、`.upmctl/approvals` 或 `.upmctl/admissions` 下的审计工件，但不会修改 VM 或 Kubernetes。

### 6.1 生成 VM start Plan

```bash
upmctl plan vm start \
  --node k8s-3 \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s \
  --request-id human-plan-k8s-3 \
  | tee /tmp/upmctl-plan.json
```

读取 disposition：

```bash
jq '.data | {
  planId,
  planDigest,
  environmentId,
  action,
  disposition,
  riskLevel,
  target,
  blockers,
  expectedDisruption,
  approvalScope,
  createdAt,
  expiresAt
}' /tmp/upmctl-plan.json
```

只在 `ACTION_REQUIRED` 时提取并继续使用 Plan ID：

```bash
DISPOSITION=$(jq -r '.data.disposition' /tmp/upmctl-plan.json)
PLAN_ID=$(jq -r '.data.planId' /tmp/upmctl-plan.json)

case "$DISPOSITION" in
  ACTION_REQUIRED)
    printf 'continue with Plan %s\n' "$PLAN_ID"
    ;;
  NOOP)
    printf 'target already satisfies vm.start; no Plan file was created\n'
    ;;
  BLOCKED)
    jq '.data.blockers' /tmp/upmctl-plan.json
    exit 3
    ;;
  *)
    printf 'unexpected disposition: %s\n' "$DISPOSITION" >&2
    exit 70
    ;;
esac
```

不要只根据命令退出码推断 Plan disposition，自动化必须读取结构化结果中的 `.data.disposition`。

### 6.2 查看不可变 Plan

```bash
upmctl plan get "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  | tee /tmp/upmctl-plan-inspection.json
```

检查 `.data.expired` 和 `.data.executionAvailable`。Plan 过期后仍可读取用于审计，但不能再批准。

### 6.3 校验本地绑定

```bash
upmctl plan validate "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  | tee /tmp/upmctl-plan-validation.json
```

`plan validate` 校验工件完整性、30 分钟 TTL、时间、Environment、Config 和 Managed State 绑定；它**不会重新观察目标环境**，所以 `observedStateBinding` 为 `NOT_CHECKED`。

应检查：

```bash
jq '.data | {
  planId,
  artifactStatus,
  freshnessStatus,
  environmentBinding,
  configBinding,
  managedStateBinding,
  observedStateBinding,
  executionAvailable,
  blockers
}' /tmp/upmctl-plan-validation.json
```

### 6.4 运行只读 Preflight

Phase 2b2a 的 Preflight 因 Apply 固定关闭，即使自身检查为 `PASSED`，CLI 也返回退出码 3。使用 `set -e` 的脚本必须显式捕获该退出码：

```bash
set +e
upmctl preflight \
  --plan-id "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s \
  --request-id human-preflight-k8s-3 \
  >/tmp/upmctl-preflight.json \
  2>/tmp/upmctl-preflight-error.json
PREFLIGHT_RC=$?
set -e

if [ "$PREFLIGHT_RC" -ne 3 ]; then
  printf 'unexpected preflight exit code: %s\n' "$PREFLIGHT_RC" >&2
  cat /tmp/upmctl-preflight-error.json >&2
  exit "$PREFLIGHT_RC"
fi

jq '.data | {
  planId,
  planDigest,
  preflightStatus,
  approvalStatus,
  applyDecision,
  executionAvailable,
  basis,
  checks,
  blockers
}' /tmp/upmctl-preflight.json
```

只有以下组合才表示已实现的只读检查通过：

```text
preflightStatus == PASSED
applyDecision == BLOCKED
executionAvailable == false
```

如果 `preflightStatus=BLOCKED`，按 blockers 和 checks 中的稳定 code 排查，不得继续审批。

### 6.5 人工 Grant Approval

先由工程师审阅 Plan 和 Preflight，然后在自己的本地交互终端直接运行：

```bash
upmctl approval grant \
  --plan-id "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  --request-id human-approval-k8s-3 \
  | tee /tmp/upmctl-approval.json
```

CLI 会在控制 TTY 中显示 Plan、action、target、risk、scope、过期时间和“Apply remains BLOCKED”提示，然后：

1. 要求人类输入非空审批原因；
2. 生成随机 challenge；
3. 要求人类逐字准确输入 challenge；
4. 重新运行只读 Preflight 并复核 Plan；
5. 原子创建唯一 Approval。

以下做法均不支持：

```bash
# 禁止：标准输入管道不能代替控制 TTY
printf 'yes\n' | upmctl approval grant --plan-id "$PLAN_ID"

# 禁止：CLI 不接受绕过参数
upmctl approval grant --plan-id "$PLAN_ID" --yes
upmctl approval grant --plan-id "$PLAN_ID" --reason maintenance
```

提取 Approval ID：

```bash
APPROVAL_ID=$(jq -r '.data.approvalId' /tmp/upmctl-approval.json)
printf '%s\n' "$APPROVAL_ID"
```

### 6.6 查询 Approval

```bash
upmctl approval get "$APPROVAL_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  | tee /tmp/upmctl-approval-inspection.json

upmctl approval list \
  --plan-id "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json

upmctl approval list \
  --workspace "$WORKSPACE" \
  --output text
```

Approval 状态：

| 状态 | 含义和处理 |
| --- | --- |
| `MISSING` | Preflight 没有找到该 Plan 的 Approval；若接受风险，由人类审批 |
| `APPROVED` | 有效审批证据存在；仍不能 Apply |
| `REVOKED` | 已撤销；停止并生成新 Plan |
| `EXPIRED` | Approval 已过期；生成新 Plan |
| `INVALID` | 工件、摘要、绑定、Admission 或安全文件身份异常；停止并排障 |

### 6.7 人工 Revoke Approval

只能在 Approval 尚有效且未被撤销/消费时，由人类在本地交互终端运行：

```bash
upmctl approval revoke "$APPROVAL_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  --request-id human-revoke-k8s-3 \
  | tee /tmp/upmctl-revocation.json
```

CLI 会要求输入撤销原因和随机 challenge。撤销成功后，原 Approval 保持不可变，查询状态变为 `REVOKED`。

### 6.8 最终确认没有执行变更

```bash
upmctl preflight \
  --plan-id "$PLAN_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s || true

upmctl vm inspect k8s-3 \
  --workspace "$WORKSPACE" \
  --output json \
  --timeout 60s
```

报告中必须明确记录：

```text
applyDecision=BLOCKED
executionAvailable=false
target environment mutation occurred=false
```

## 7. Codex Skill 使用方式

`upmctl-environment` Skill 的安全流程为：

```text
capabilities -> context discover -> config validate/status
-> vm observe -> plan -> preflight -> explain
-> pause for human approval -> approval get/list
-> capability-gated apply -> verify -> report
```

Phase 2b2a 中，Skill 必须在读取 Approval 后停止于 Apply 不可用，并报告没有发生目标环境变更。

Agent 可以执行：

- `version`、`capabilities`；
- context、config、status 和 VM 只读观察；
- `plan vm start`、`plan get`、`plan validate`；
- `preflight`；
- `approval get/list`；
- 解释 blocker、remediation 和下一步人工命令。

Agent 不得执行或模拟：

- `approval grant`；
- `approval revoke`；
- typed challenge 或 reason 输入；
- TTY 模拟、`expect`、标准输入管道或 actor 注入；
- 直接调用 `vagrant`、`virsh`、`ssh`、`kubectl`、`helm`、Ansible 或 legacy Shell 执行变更；
- 直接创建或编辑 `.upmctl` 控制状态。

当 Agent 提示需要审批时，人类应复制其提供的准确命令，在自己的本地终端运行；完成后让 Agent 只通过 `approval get/list` 继续读取。

## 8. 退出码和脚本处理

| 退出码 | 含义 |
| --- | --- |
| `0` | 成功、`NOOP` 或 `ACTION_REQUIRED` |
| `2` | 参数或命令用法错误 |
| `3` | 环境、前置条件、安全策略或当前阶段能力导致 `BLOCKED` |
| `4` | 外部依赖或外部命令失败 |
| `5` | `PARTIAL`，仅为后续 Operation 契约预留 |
| `6` | 中断、取消或超时 |
| `70` | CLI 内部错误或输出失败 |

脚本处理原则：

1. 退出码决定控制流；
2. JSON 中的稳定 `error.code`、`disposition`、`preflightStatus`、`approvalStatus` 和 `applyDecision` 决定业务语义；
3. stderr 中的 `remediation` 给出下一步建议；
4. 保存 `requestId`、Plan ID、Plan digest 和 Approval ID 便于审计；
5. 不把 text message 作为稳定解析接口。

特别注意：Phase 2b2a 的 `preflight` 固定因 Apply 不可用返回 3，即使 `preflightStatus=PASSED`。不要用 `command || true` 后直接宣称成功；必须解析结果并保留原退出码。

## 9. 控制状态文件

Phase 2b2a 会使用以下本地控制状态：

```text
<workspace>/.upmctl/
├── state.json
├── plans/
│   └── <planId>.json
├── approvals/
│   └── by-plan/
│       └── <planId>.json
└── admissions/
    └── <planId>.json
```

- `state.json` 描述 Managed Environment；
- `plans` 仅保存 `ACTION_REQUIRED` Plan；
- `approvals` 保存每个 Plan 唯一的不可变 Approval；
- `admissions` 当前只可能保存 Revocation，未来与 Plan Claim 共用排他槽位。

这些目录和文件使用私有权限、安全路径检查和原子无覆盖发布。运维人员可以按故障排除手册进行只读检查，但不能直接修改、替换、软链接、复制覆盖或“修复”摘要。

## 10. 当前不支持的范围

以下能力在 Phase 2b2a 未实现，调用时应返回稳定的 `UPMCTL_NOT_IMPLEMENTED` 或 capability unavailable：

- `apply --plan-id PLAN_ID`；
- VM 实际 start、stop、restart 和 `vm ssh`；
- `plan vm stop|restart`；
- cluster deploy、start、stop、restart、destroy；
- Kubernetes node list/status 的完整管理契约，以及 node add/remove；
- Addon install/upgrade/uninstall；
- Executor、Plan Claim、环境锁和 Operation journal；
- `operation get|cancel|resume`；
- `verify` 和 `report generate`；
- MCP Server 和 MCP 写操作；
- 回退执行 `libvirt_kubespray_setup.sh`、`upm_setup.sh` 或其他 legacy 脚本。

当命令不受支持时，正确行为是报告边界并停止。不得为了“完成任务”改用直接基础设施命令或 legacy 脚本。

## 11. 工程师交付检查清单

首次接手一个测试环境时，至少完成以下检查：

- [ ] `upmctl version` 能运行，平台和构建信息正确；
- [ ] `capabilities` 与 Phase 2b2a 范围一致；
- [ ] 使用绝对 `--workspace`，且 `context discover` 返回预期 Environment ID；
- [ ] trust 为 `MANAGED_VALID`；
- [ ] config 为 safe、valid、complete；
- [ ] `status` 和 `vm list` 的 observation sources 可解释；
- [ ] 对至少一个 VM 完成 `vm status` 和 `vm inspect`；
- [ ] 对停止的测试 VM 生成 `ACTION_REQUIRED` Plan，或记录 `NOOP/BLOCKED` 原因；
- [ ] 完成 `plan get`、`plan validate` 和只读 `preflight`；
- [ ] 人类在控制 TTY 完成 grant/get/list/revoke 验证；
- [ ] 记录 Preflight 即使通过仍为 `applyDecision=BLOCKED`；
- [ ] 确认没有 VM、Guest、Kubernetes 或宿主机目标变更；
- [ ] 保存请求 ID、结构化输出、退出码和测试时间作为交付证据。

部署方法参见同目录的[部署手册](deployment-guide.md)；日志采集、常见错误码和恢复方法参见[日志与故障排除手册](operations-troubleshooting.md)。
