# upmctl 日志与故障排除手册

本手册面向运行和支持 `upmctl` Phase 2b2a 的工程师。当前版本提供环境发现、配置校验、VM 只读观察、`vm.start` Plan、Preflight 以及本地人工 Approval 控制面；不提供 Apply、Executor、Operation journal、环境锁或实际 VM/Kubernetes 变更。

> 重要：`preflightStatus=PASSED` 或 `approvalStatus=APPROVED` 都不表示可以执行变更。当前版本始终返回 `applyDecision=BLOCKED` 和 `executionAvailable=false`。

## 1. 运行时记录模型

`upmctl` 是短进程 CLI，不是后台服务。当前实现：

- 不创建默认日志文件，也没有默认日志目录。只有显式传入 `--log-file PATH` 时才启用本地结构化运行日志。
- 成功结果写入 stdout；错误写入 stderr。
- `environment adopt`及`approval grant/revoke`的摘要、reason提示和typed challenge直接通过控制终端`/dev/tty`读写，不经由stdin/stdout/stderr。
- `--output json` 和 `--output jsonl` 输出稳定 envelope。当前 `jsonl` 每次命令也只输出一个 envelope，不是持续日志流。
- `--log-file` 日志独立追加到指定文件，不会混入 stdout 的 JSON/JSONL 业务 envelope，也不改变 stderr 错误契约。
- 外部命令的原始 stderr 不会被完整透传；不可用时通常在 `sources`、`findings` 或稳定错误码中表示。需要原始依赖输出时，应使用本手册给出的等价只读命令复现。

### 1.1 requestId 与关联查询

每次命令都有 `requestId`。未显式指定时，CLI 会生成 `req-...`；为了把工单、命令输出和支持包关联起来，生产运维建议显式指定。

```bash
REQUEST_ID="req-ticket-20260717-001"
upmctl --request-id "$REQUEST_ID" --workspace "$WORKSPACE" --output json status \
  >"status.${REQUEST_ID}.json" \
  2>"status.${REQUEST_ID}.error.json"
rc=$?
printf 'requestId=%s exitCode=%s\n' "$REQUEST_ID" "$rc"
```

注意：

- text 错误只显示 `code/message/remediation`，不显示 `requestId`。需要关联诊断时使用 JSON/JSONL。
- 要进入 Approval 工件的 `requestId` 建议使用简短可打印 ASCII，不超过 128 字符，且不包含换行、Tab 等控制字符。
- `requestId` 会同时出现在业务/错误 envelope 和显式启用的运行日志中，可用于本机关联。它不是分布式跟踪 ID，当前不会自动注入 Vagrant、libvirt 或 Kubernetes API 日志。

### 1.2 JSON 成功与错误 envelope

成功结果的顶层字段是：

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "EnvironmentStatus",
  "requestId": "req-ticket-20260717-001",
  "timestamp": "2026-07-17T00:00:00Z",
  "data": {}
}
```

错误 envelope 的顶层 `kind` 固定为 `Error`：

```json
{
  "apiVersion": "upmctl.upm.io/v1alpha1",
  "kind": "Error",
  "requestId": "req-ticket-20260717-001",
  "timestamp": "2026-07-17T00:00:00Z",
  "error": {
    "code": "UPMCTL_CONTEXT_NOT_FOUND",
    "message": "deployment workspace was not found",
    "details": {},
    "remediation": "pass --workspace or run context discover"
  }
}
```

自动化应优先判断进程退出码，再解析 `kind`、`error.code`、`data.blockers` 和具体状态字段；不要解析 text 列对齐。

### 1.3 可选结构化运行日志

需要关联一次 CLI 调用、工单或自动化任务时，先创建专用私有目录，再显式传入日志文件：

```bash
LOG_DIR="$HOME/.local/state/upmctl"
LOG_FILE="$LOG_DIR/runtime.jsonl"
install -d -m 700 "$LOG_DIR"

upmctl --request-id "$REQUEST_ID" \
  --workspace "$WORKSPACE" \
  --output json \
  --log-file "$LOG_FILE" \
  status
```

父目录必须预先存在、是真实目录且不是 symlink；CLI 不会自动创建父目录。日志文件不存在时以 `0600` 创建。已存在的目标必须是真实普通文件且权限恰好为 `0600`；CLI 拒绝 symlink、目录、设备、FIFO、其他非普通文件和已有非 `0600` 文件，不会自动放宽或修正已有权限。

每次调用最多追加一个 `start` 和一个终态事件：

- `start`：命令进入 CLI，`exitCode=null`、`errorCode=null`。
- `complete`：命令返回业务 envelope；`exitCode` 记录真实退出码，`errorCode=null`。Preflight 等即使因策略返回非零 exit code，仍记录为 `complete`。
- `error`：命令返回错误 envelope，同时记录真实 `exitCode` 和稳定 `errorCode`。

```json
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:03Z","requestId":"req-example","command":"preflight","event":"start","exitCode":null,"errorCode":null}
{"logVersion":"upmctl.runtime/v1","timestamp":"2026-07-17T01:02:04Z","requestId":"req-example","command":"preflight","event":"complete","exitCode":3,"errorCode":null}
```

日志只允许包含以下七个字段：

```text
logVersion
timestamp
requestId
command
event
exitCode
errorCode
```

`command` 是不含参数和参数值的规范化命令路径，例如 `approval grant`。运行日志明确不记录：

- 命令参数或 workspace 路径。
- Plan ID、Approval ID 或完整 Plan/Approval/Admission。
- TTY 输入、reason 或 typed challenge。
- 环境快照、VM/Kubernetes 状态或外部命令输出。

因此，该文件只是隐私最小化的故障关联和本机运行审计线索，不是 Operation journal、安全审计日志、强身份认证证据或执行记录。当前 Phase 2b2a 仍未实现 Operation journal。

日志安全检查：

```bash
ls -ld "$LOG_DIR"
ls -l "$LOG_FILE"
find "$LOG_DIR" -maxdepth 1 -type l -print
tail -n 20 "$LOG_FILE"
```

## 2. 标准诊断流程

按以下顺序定位问题，不要在还未确认工作区身份时直接运行 Vagrant 或 `kubectl`：

```bash
upmctl --output json version
upmctl --output json capabilities
upmctl --workspace "$WORKSPACE" --output json context discover
upmctl --workspace "$WORKSPACE" --output json config validate
upmctl --workspace "$WORKSPACE" --timeout 2m --output json status
upmctl --workspace "$WORKSPACE" --timeout 2m --output json vm list
```

解读要点：

1. `context discover` 必须指向预期工作区，关注 `workspace/environmentId/trust/managed/findings`。
2. `config validate` 必须是 `safe=true`、`valid=true`、`complete=true`。解析器不执行 Ruby，仅接受当前 allowlist 语法。
3. `status` 在 legacy/untrusted 工作区仅做被动文件观察，不调用 Vagrant 或加载 kubeconfig。
4. `vm list/status/inspect`、Plan 和 Preflight 只在 digest-bound `MANAGED_VALID` 工作区中运行外部只读观察。
5. `status` 成功不等于 CNI、Addon 或业务端点验收成功；继续检查 VM `health/consistency`、`sources` 和 `findings`。

## 3. 工作区、Managed State 与路径安全

### 3.1 工作区最低结构

`--workspace` 必须是目录，且至少包含：

```text
Vagrantfile
vagrant/config.rb
```

一个可执行观察和规划的 Managed Environment 还必须有有效的：

```text
.upmctl/state.json
```

`state.json` 绑定 `environmentId`、工作区真实路径、受管文件摘要和可选 VM/libvirt UUID。该文件不能是 symlink，不能超过 1 MiB，不能有重复 JSON key、未知字段或尾随 JSON。受管 `Vagrantfile`、`vagrant/config.rb` 和已绑定 kubeconfig 也必须是工作区内的真实普通文件，且摘要匹配。

不要手工修改 `state.json` 中的 digest 来“消除”错误。应先确认文件为什么变更，再使用受控的环境管理/迁移流程重建 Managed State。

### 3.2 安全检查命令

```bash
WORKSPACE="/absolute/path/to/workspace"

pwd -P
realpath "$WORKSPACE"
ls -ld "$WORKSPACE" "$WORKSPACE/.upmctl"
find "$WORKSPACE/.upmctl" -maxdepth 3 -type l -print
find "$WORKSPACE/.upmctl" -maxdepth 3 -type d -exec stat -f '%Sp %N' {} \; 2>/dev/null || \
  find "$WORKSPACE/.upmctl" -maxdepth 3 -type d -exec stat -c '%A %n' {} \;
find "$WORKSPACE/.upmctl" -maxdepth 3 -type f -exec stat -f '%Sp %N' {} \; 2>/dev/null || \
  find "$WORKSPACE/.upmctl" -maxdepth 3 -type f -exec stat -c '%A %n' {} \;
```

Plan/Approval/Admission 控制状态的安全要求：

- `.upmctl`、`.upmctl/plans`、`.upmctl/approvals/by-plan` 和 `.upmctl/admissions` 等存在的控制目录必须是真实目录，权限为 `0700`。
- 持久化 Plan、Approval 和 Admission 必须是真实普通文件，权限为 `0600`，大小不超过 1 MiB。
- 任何 symlink、路径逃逸、文件身份竞态、非普通文件、错误权限或未知目录条目都应当作不安全状态处理。
- 工件是不可变证据，CLI 创建时原子发布且不覆盖已有文件。不要就地编辑 Plan、Approval 或 Revocation，也不要通过重命名/链接绕过唯一性。

如果发现意外 symlink、身份替换或内容篡改，先停止 Approval 操作，保留文件元数据和摘要并按安全事件处理。不建议在未保留证据时直接 `chmod`、删除或重写。

## 4. Vagrant、OpenSSH、libvirt 和 kubectl 依赖

### 4.1 CLI 实际使用的只读命令

`upmctl` 从当前进程 `PATH` 查找依赖，并继承当前环境。VM 观察实际使用：

```text
vagrant status --machine-readable
vagrant ssh-config NODE
vagrant ssh NODE -c true
virsh list --all --name
virsh domuuid DOMAIN
virsh domstate UUID
virsh dominfo UUID
virsh domblklist UUID --details
kubectl --kubeconfig KUBECONFIG get nodes -o json
```

对 Vagrant 和 libvirt 均为 running 的 VM，`ssh-config` 仅提供 endpoint 元数据；只有固定 `vagrant ssh NODE -c true` 成功后，CLI 才设置 `sshState=reachable`，该 VM 才可能判定为 `RUNNING_HEALTHY`。探针参数不可由用户替换，不读取 Guest 文件，不使用 `sudo`，也不执行其他 Guest 命令。

当某个依赖不存在、连接失败或输出不可解析时，命令不一定整体失败；它可能成功返回但将数据源标为 `unavailable`，并附带 `VAGRANT_*`、`SSH_*`、`LIBVIRT_*` 或 `KUBERNETES_*` finding。这些 finding 会使 Preflight 的观察安全检查无法通过。

### 4.2 依赖预检

```bash
command -v vagrant
command -v ssh
command -v virsh
command -v kubectl

vagrant --version
ssh -V
virsh --version
kubectl version --client
```

使用与 `upmctl` 相同的用户、shell、`PATH`、libvirt 默认连接环境和工作区复现：

```bash
cd "$WORKSPACE"
vagrant status --machine-readable
vagrant ssh-config NODE
vagrant ssh NODE -c true
virsh uri
virsh list --all --name

# KUBECONFIG 必须使用 context discover 返回的精确路径
kubectl --kubeconfig "$KUBECONFIG" get nodes -o json
```

排查方向：

- `VAGRANT_STATUS_UNAVAILABLE`：检查 Vagrant 安装、provider/plugin、工作目录和当前用户权限。
- `VAGRANT_STATUS_INVALID`：保存 `vagrant status --machine-readable` 原始输出；当前解析要求至少有一条 machine state。
- `SSH_PROBE_FAILED`：先确认 `ssh-config` endpoint，再以相同用户在同一工作区复现固定 `vagrant ssh NODE -c true`；检查本机 OpenSSH、Vagrant SSH 密钥/权限、网络和 Guest SSH 服务。不要改成其他命令来规避探针。
- `LIBVIRT_INVENTORY_UNAVAILABLE`：检查 `virsh uri` 和 `virsh list --all --name`，确认 CLI 连到与 Vagrant provider 相同的 libvirt。
- `LIBVIRT_DOMAIN_UUID_UNAVAILABLE`：针对具体 domain 运行 `virsh domuuid DOMAIN`。
- `KUBECONFIG_NOT_FOUND`：当前只发现 `inventory/sample/artifacts/admin.conf` 或 `artifacts/admin.conf`；确认文件存在且已进入 Managed State 绑定。
- `KUBERNETES_API_UNAVAILABLE`：使用发现到的 kubeconfig 直接运行上述 `kubectl` 命令，检查 API 可达性、证书和认证。
- `KUBERNETES_NODE_OUTPUT_INVALID`：保存原始 JSON，确认命令没有被 wrapper/alias 改写且输出是 Kubernetes NodeList。

## 5. 超时、中断和时钟

`--timeout` 接受 Go duration，且必须大于 0，例如 `30s`、`2m`。它主要限制 `status`、VM 观察、Plan 生成和 Preflight 中的外部只读命令。

```bash
upmctl --workspace "$WORKSPACE" --timeout 2m --output json vm list
upmctl --workspace "$WORKSPACE" --timeout 5m --output json preflight --plan-id "$PLAN_ID"
```

- 非正数或无法解析的 duration 返回 `UPMCTL_USAGE` / exit 2。
- 超时或 context 取消返回 `UPMCTL_INTERRUPTED` / exit 6。
- 请先直接测量慢的底层只读命令，然后在不超过 Plan 生命周期的前提下适当增加 timeout；不要无限放大。
- Plan 固定 30 分钟后过期。Approval 默认有效 10 分钟，且不能晚于 Plan 过期时间。
- `approval grant` 在人工输入前准备 Preflight，在实际发布前再做新鲜 Preflight 和时间绑定校验。人工停留太久可能导致 timeout 或 Plan 过期，这是预期的保守失败。
- 出现时间相关错误时，运行 `date -u`，并检查宿主机时钟同步；不要手工改写工件时间字段。

## 6. Plan 与漂移

### 6.1 两类校验

```bash
upmctl --workspace "$WORKSPACE" --output json plan validate "$PLAN_ID"
upmctl --workspace "$WORKSPACE" --timeout 5m --output json preflight --plan-id "$PLAN_ID"
```

- `plan validate` 只检查 Plan 工件、TTL、Environment、Config 和 Managed State 本地绑定；不调用 Vagrant/libvirt/kubectl，`observedStateBinding=NOT_CHECKED`。
- `preflight` 会重新读取 Context、Config、Managed State 和 Plan，再执行现场只读观察。它比较 `basis.config`、`basis.managedState` 和 `basis.observedState` 中的 `expected/current/status`。
- Config 或 Managed State 文件变更、VM/domain/Node 状态变化、身份不一致、orphan、重复 UUID 或依赖不可观察都可能阻止 Preflight。

漂移的正确处理：

1. 保留原 Plan ID、Plan digest、requestId 和 Preflight JSON。
2. 根据 `basis` 和 `blockers` 确认是 Config、Managed State 还是 Observed State 变化。
3. 确认现场变化的来源，并在必要时修复依赖/身份不一致。
4. 不修改原 Plan；使用当前已验证现场重新生成 Plan。
5. 新 Plan 需要新的人工 Approval。旧 Approval 不能迁移到新 Plan。

### 6.2 Preflight 的退出码

当前版本即使 `preflightStatus=PASSED`，`preflight` 命令也会以 exit 3 结束，因为 Apply 必然被阻止。这不是 CLI 崩溃；应同时检查：

```text
preflightStatus
applyDecision
executionAvailable
approvalStatus
checks
blockers
```

不要仅依据 exit 3 将所有 Preflight 归类为同一种故障。

## 7. Approval 五态与 TTY 问题

### 7.1 Approval 五态

| 状态 | 含义 | 运维处理 |
| --- | --- | --- |
| `MISSING` | 该 Plan 没有 Approval 证据 | 先确认只读 Preflight 基线可接受，再由人类在控制 TTY 直接运行 `approval grant` |
| `APPROVED` | 存在当前有效、与 Plan 完整绑定的 Approval | 仅表示人工意图证据；当前仍不能 Apply |
| `REVOKED` | Admission 槽中存在有效 Revocation | 不能重新批准同一 Plan；重新生成 Plan |
| `EXPIRED` | Approval TTL 已到期 | 不能续期或覆盖；重新生成 Plan 并重新批准 |
| `INVALID` | Approval/Admission/Plan 绑定、摘要、Schema 或存储安全无法被证明 | 停止变更流程，收集元数据和脱敏证据；不编辑原工件，在确认原因后生成新 Plan |

`approval get/list` 是只读命令，可以在自动化中查询。`approval grant/revoke` 是人类专用写操作；Skill、MCP、CI、pipe、`expect` 和任何伪造 TTY 的方式都不受支持。

`environment adopt`同样是人类专用信任边界写操作。出现拒绝时先检查config、`.vagrant/machines/k8s-N/libvirt/id`闭合集合、UUID唯一性、provider目录和`.upmctl`是否已存在；不要手工创建`state.json`或用自动化模拟TTY。

### 7.2 `UPMCTL_HUMAN_TTY_REQUIRED`

常见原因：

- 进程在 CI、后台 job、cron 或无 PTY 的 SSH session 中运行。
- `/dev/tty` 不可打开，或输入/输出端点不是支持 terminal ioctl 的字符设备。
- 命令被容器、IDE task runner 或自动化 wrapper 启动，没有控制终端。

诊断：

```bash
tty
test -r /dev/tty && test -w /dev/tty
ls -l /dev/tty
```

处理：在运行 `upmctl` 的那台主机上，由工程师直接进入交互式终端后重试。不要将 reason/challenge 管道输入。远程操作时必须由终端会话正常分配控制 TTY，而不是由 agent 代替人类输入。

另外：

- reason 必须非空；challenge 必须大小写完全一致。
- 单行交互输入上限为 4096 bytes。
- 通过 `sudo` 运行时，记录的 actor 是实际 effective UID 对应用户。除非这是期望的审计身份，否则不要为了绕过文件权限而使用 `sudo`。
- 本地 OS/TTY 观察是审计证据，不是密码学强身份认证。

## 8. 常见错误码

CLI 退出码契约为：`0` 表示 SUCCEEDED/NOOP/ACTION_REQUIRED，`2` 表示用法错误，`3` 表示环境/前置条件/安全策略 BLOCKED，`4` 表示外部依赖失败，`5` 保留给 PARTIAL，`6` 表示 INTERRUPTED/CANCELLED，`70` 表示内部错误。

### 8.1 CLI 错误 envelope

| 错误码 | exit | 常见原因 | 首选处理 |
| --- | ---: | --- | --- |
| `UPMCTL_USAGE` | 2 | 命令树、ID、`--node`、输出格式或 timeout 不合法 | 检查 `capabilities`和 CLI 契约；Plan ID 必须是 `plan-` + 64 位小写 hex，Approval ID 类似 |
| `UPMCTL_LOG_OPEN_FAILED` | 70 | `--log-file` 父目录不存在/是 symlink，目标是 symlink、非普通文件、已有非 `0600` 文件，或打开权限不足 | 核对父目录和文件身份；新文件由 CLI 以 `0600` 创建，已有文件必须预先是安全 `0600` 普通文件 |
| `UPMCTL_LOG_WRITE_FAILED` | 70 | JSONL 事件追加、短写或同步到文件失败 | 检查可写性、磁盘空间、inode/配额、只读文件系统和 I/O 错误；不要将显式日志请求静默降级为无日志运行 |
| `UPMCTL_NOT_IMPLEMENTED` | 3 | 调用 Apply、Operation、VM stop/restart、Node/Addon 等当前未开放能力 | 运行 `capabilities`；不要回退到 legacy 脚本由 agent 自动执行 |
| `UPMCTL_CONTEXT_NOT_FOUND` | 3 | 当前目录无法发现仓库/工作区 | 传递工作区绝对路径，运行 `context discover` |
| `UPMCTL_WORKSPACE_NOT_FOUND` | 3 | 没有找到部署工作区或 `config.rb` | 检查 `Vagrantfile` 和 `vagrant/config.rb` |
| `UPMCTL_HUMAN_TTY_REQUIRED` | 3 | adopt/grant/revoke不在本地控制TTY运行 | 由人类在本机交互终端直接运行；禁止pipe、CI、Skill、MCP和伪造TTY |
| `UPMCTL_ENVIRONMENT_ALREADY_CONTROLLED` | 3 | workspace已有state或其他`.upmctl`控制状态 | 检查现有状态；Adopt不覆盖、合并或迁移任何控制工件 |
| `UPMCTL_PROVIDER_UNSUPPORTED` / `UPMCTL_VAGRANT_METADATA_INVALID` | 3 | provider不是纯libvirt，或节点/UUID metadata不闭合 | 修复legacy Vagrant metadata，确保仅有config声明节点、每节点单一libvirt provider和唯一合法UUID |
| `UPMCTL_WORKSPACE_UNTRUSTED` | 3 | 工作区是 legacy read-only 或 Managed State 无效 | 检查 `context.data.trust/findings`；按受控流程迁移/修复 Managed Environment |
| `UPMCTL_CONFIG_INVALID` | 3 | `config.rb` 不安全、不完整、不合法或 digest 漂移 | 运行 `config validate --output json`，处理 findings；不执行未支持 Ruby |
| `UPMCTL_MANAGED_STATE_INVALID` | 3 | `.upmctl/state.json` 不是安全普通文件、身份/摘要无效或读取期间变化 | 停止规划，核对 Managed State 和受管文件；不手工伪造 digest |
| `UPMCTL_VM_NOT_FOUND` | 3 | NODE 不在联合观察结果中 | 先运行 `vm list`，使用列出的精确名称 |
| `UPMCTL_STATUS_FAILED` / `UPMCTL_VM_OBSERVE_FAILED` | 4 | 外部只读观察返回未被容错的失败 | 分别直接复现 Vagrant、virsh、kubectl 命令，检查用户和环境 |
| `UPMCTL_INTERRUPTED` | 6 | timeout 或 context 取消 | 定位慢依赖，在可控范围内增加正 timeout |
| `UPMCTL_PLAN_NOT_FOUND` | 3 | 当前工作区下没有该 Plan | 确认工作区和 Plan ID；必要时生成新 `ACTION_REQUIRED` Plan |
| `UPMCTL_PLAN_INVALID` | 3 | Plan Schema、摘要、ID、TTL、封闭 step 模板或绑定被破坏 | 保留证据，不编辑原 Plan，重新生成 |
| `UPMCTL_PLAN_STORE_UNSAFE` | 3 | Plan 目录/文件的权限、symlink、路径或身份不安全 | 检查 `.upmctl/plans` 的 0700/0600 和真实路径；警惕篡改 |
| `UPMCTL_PLAN_STORE_FAILED` / `UPMCTL_PLAN_FAILED` / `UPMCTL_PREFLIGHT_FAILED` | 4/70 | 存储 I/O、摘要或内部生成失败 | 保留 requestId、完整 JSON、版本、磁盘/文件系统信息并升级支持 |
| `UPMCTL_PREFLIGHT_BLOCKED` | 3 | Approval 要求的前置 Preflight 不是 `PASSED` | 处理 `details.blockers`；若 basis 改变则重新生成 Plan |
| `UPMCTL_HUMAN_TTY_REQUIRED` | 3 | 没有可用控制 TTY | 由人类在当地交互终端直接运行 |
| `UPMCTL_HUMAN_INTERACTION_FAILED` | 3 | reason/challenge 读写失败、空 reason、输入关闭或超限 | 检查 TTY，使用非空简短单行 reason 重试 |
| `UPMCTL_APPROVAL_NOT_CONFIRMED` / `UPMCTL_REVOCATION_NOT_CONFIRMED` | 3 | challenge 未精确匹配 | 重新阅读摘要，由同一人在 TTY 中精确输入 |
| `UPMCTL_ACTOR_OBSERVE_FAILED` | 4 | effective UID 无法解析为本地账号，或 hostname 获取失败 | 修复本地账号/NSS 或主机名解析后重试 |
| `UPMCTL_APPROVAL_NOT_FOUND` | 3 | Approval ID 不存在于当前工作区 | 运行 `approval list`，检查工作区和 ID |
| `UPMCTL_APPROVAL_EXISTS` | 3 | 同一 Plan 已有不可变 Approval | 查看已有 Approval；不能覆盖、续期或重新批准 |
| `UPMCTL_APPROVAL_REVOKED` / `UPMCTL_APPROVAL_EXPIRED` | 3 | 该 Plan 的 Approval 已撤销/过期 | 生成新 Plan 并重新批准 |
| `UPMCTL_APPROVAL_INVALID` | 3 | Approval 内容、摘要或 Plan 绑定无效 | 保留证据并停止使用该工件；生成新 Plan |
| `UPMCTL_APPROVAL_STORE_UNSAFE` / `UPMCTL_ADMISSION_STORE_UNSAFE` | 3 | 控制状态路径、类型、权限或身份不安全 | 检查 0700/0600、symlink 和路径逃逸；保留可疑证据 |
| `UPMCTL_APPROVAL_STORE_FAILED` / `UPMCTL_ADMISSION_STORE_FAILED` | 4 | 控制状态 I/O 或原子发布失败 | 检查磁盘空间、文件系统和工作区可写性，保留完整错误 |
| `UPMCTL_ADMISSION_INVALID` | 3 | Admission 存储 key、kind、Plan 绑定或工件内容无效 | 停止使用该工件，保留证据并升级支持 |
| `UPMCTL_ADMISSION_CONFLICT` | 3 | 同一 Plan 的唯一 Admission 槽已有不可变工件 | 查看现有 Revocation/Claim，不覆盖 |
| `UPMCTL_PLAN_ALREADY_CLAIMED` | 3 | Admission 中存在有效 Plan Claim | 查看关联 operation；当前 Phase 2b2a 不会创建 Claim |

### 8.2 Preflight blocker 代码

Preflight 的 `data.blockers` 不是顶层错误 envelope，但同样是稳定诊断代码。常用分组：

- Plan：严格读取损坏工件时使用顶层`UPMCTL_PLAN_INVALID`；对已安全解码的内存校验投影可使用`UPMCTL_PLAN_TAMPERED` blocker；时间类为`UPMCTL_PLAN_EXPIRED`、`UPMCTL_PLAN_TIME_INVALID`。
- Environment：`UPMCTL_ENVIRONMENT_MISMATCH`、`UPMCTL_ENVIRONMENT_UNKNOWN`。
- Config：`UPMCTL_CONFIG_DRIFT`、`UPMCTL_CONFIG_INVALID`、`UPMCTL_CONFIG_UNAVAILABLE`。
- Managed State：`UPMCTL_MANAGED_STATE_DRIFT`、`UPMCTL_MANAGED_STATE_INVALID`、`UPMCTL_MANAGED_STATE_UNAVAILABLE`。
- Observed State：`UPMCTL_OBSERVED_STATE_UNSAFE`、`UPMCTL_OBSERVED_STATE_DRIFT`、`UPMCTL_OBSERVED_STATE_INVALID`、`UPMCTL_OBSERVED_STATE_UNAVAILABLE`。
- 当前阶段固定未开放：`UPMCTL_CAPABILITY_UNAVAILABLE`、`UPMCTL_CONCURRENCY_CHECK_NOT_IMPLEMENTED`。
- Approval：`UPMCTL_APPROVAL_MISSING`、`UPMCTL_APPROVAL_REVOKED`、`UPMCTL_APPROVAL_EXPIRED`、`UPMCTL_APPROVAL_INVALID`。

`UPMCTL_CAPABILITY_UNAVAILABLE` 和 `UPMCTL_CONCURRENCY_CHECK_NOT_IMPLEMENTED` 在 Phase 2b2a 不能通过修改环境消除，它们明确表示 Executor 和并发控制尚未实现。

## 9. 支持包收集

### 9.1 默认低敏支持包

下面的示例不复制 `.upmctl` 原始工件，不运行 Plan/Approval 写操作，也不包含 kubeconfig 内容。`status` 和 `vm list` 会调用只读观察依赖。

```bash
WORKSPACE="/absolute/path/to/workspace"
BUNDLE="upmctl-support-$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -m 700 "$BUNDLE"
RUNTIME_LOG="$BUNDLE/runtime.jsonl"

upmctl --log-file "$RUNTIME_LOG" --output json version \
  >"$BUNDLE/version.json" 2>"$BUNDLE/version.error.json"
upmctl --log-file "$RUNTIME_LOG" --output json capabilities \
  >"$BUNDLE/capabilities.json" 2>"$BUNDLE/capabilities.error.json"
upmctl --log-file "$RUNTIME_LOG" --workspace "$WORKSPACE" --output json context discover \
  >"$BUNDLE/context.json" 2>"$BUNDLE/context.error.json"
upmctl --log-file "$RUNTIME_LOG" --workspace "$WORKSPACE" --output json config validate \
  >"$BUNDLE/config-validation.json" 2>"$BUNDLE/config-validation.error.json"
upmctl --log-file "$RUNTIME_LOG" --workspace "$WORKSPACE" --timeout 2m --output json status \
  >"$BUNDLE/status.json" 2>"$BUNDLE/status.error.json"
upmctl --log-file "$RUNTIME_LOG" --workspace "$WORKSPACE" --timeout 2m --output json vm list \
  >"$BUNDLE/vm-list.json" 2>"$BUNDLE/vm-list.error.json"

command -v vagrant virsh kubectl >"$BUNDLE/dependency-paths.txt" 2>&1
vagrant --version >"$BUNDLE/vagrant-version.txt" 2>&1
virsh --version >"$BUNDLE/virsh-version.txt" 2>&1
kubectl version --client >"$BUNDLE/kubectl-client-version.txt" 2>&1
date -u >"$BUNDLE/time-utc.txt"
uname -a >"$BUNDLE/uname.txt"

find "$WORKSPACE/.upmctl" -maxdepth 3 -type l -print \
  >"$BUNDLE/control-state-symlinks.txt" 2>&1
find "$WORKSPACE/.upmctl" -maxdepth 3 -exec ls -ld {} \; \
  >"$BUNDLE/control-state-metadata.txt" 2>&1
```

`runtime.jsonl` 只包含七个允许字段，可用 `requestId` 与各 JSON envelope 关联；它不包含底层命令原始输出，不能代替上述诊断附件。打包前必须人工检查每个文件。确认可分享后：

```bash
tar -czf "${BUNDLE}.tar.gz" "$BUNDLE"
```

### 9.2 指定 Plan 的诊断附件

仅在工单需要时增加：

```bash
upmctl --workspace "$WORKSPACE" --output json plan get "$PLAN_ID" \
  >"$BUNDLE/plan-inspection.json" 2>"$BUNDLE/plan-inspection.error.json"
upmctl --workspace "$WORKSPACE" --output json plan validate "$PLAN_ID" \
  >"$BUNDLE/plan-validation.json" 2>"$BUNDLE/plan-validation.error.json"
upmctl --workspace "$WORKSPACE" --timeout 5m --output json preflight --plan-id "$PLAN_ID" \
  >"$BUNDLE/preflight.json" 2>"$BUNDLE/preflight.error.json"
upmctl --workspace "$WORKSPACE" --output text approval list --plan-id "$PLAN_ID" \
  >"$BUNDLE/approval-status.txt" 2>"$BUNDLE/approval-status.error.txt"
```

此处故意使用 text `approval list`，因为 JSON 列表包含完整 Approval 工件，可能泄露 reason、用户名、主机名和其他审计字段。如果支持人员确实需要完整工件，必须经授权后使用 JSON，再脱敏。

### 9.3 敏感信息脱敏

支持包中可能包含：

- 工作区绝对路径、本地用户名和主机名。
- VM/domain UUID、Kubernetes Node UID、IP、SSH host/port 和磁盘源路径。
- `environmentId`、Plan/Approval/Revocation ID 及摘要。
- Approval/revocation reason、actor 和 human-presence 元数据。
- kubeconfig 中的 API endpoint、client certificate/key/token（不应收集原文）。

脱敏原则：

1. 不把 kubeconfig、SSH private key、token、证书私钥或 `.vagrant` 全量目录放入支持包。
2. 优先删除字段，不用另一个看似真实的值替换。如果必须保留相同对象的关联性，使用工单内稳定假名，例如 `node-A`、`host-A`。
3. 不修改工作区内的原工件；只对支持包副本脱敏。
4. 如果需要验证摘要/绑定问题，脱敏会破坏内容摘要。此时应在受控渠道中传递原始工件，或由支持人员在环境内远程观察，不要将脱敏副本冒充为可校验原件。
5. 传输前再次扫描常见秘密字段：

```bash
rg -n -i 'token|password|secret|private.?key|client-key-data|authorization|bearer' "$BUNDLE"
```

## 10. 升级支持的最小信息

向开发/平台团队升级时，至少提供：

- 问题发生时间及时区，以及 `date -u` 输出。
- 完整命令形式（脱敏路径）、exit code、requestId。
- `upmctl version` 和 `capabilities` 输出。
- 完整 JSON 错误 envelope，包括 `details` 和 `remediation`。
- `context discover`、`config validate` 以及相关 `status/vm list/plan validate/preflight` 输出。
- 底层依赖版本，以及用相同用户和环境复现的只读命令结果。
- 是否存在 symlink、权限异常、磁盘满、只读文件系统或工件被手工修改。
- 对 Plan/Approval 问题，提供 ID、状态、过期时间和脱敏后的 blocker/basis；不默认提供 reason 或完整 actor 信息。

如果出现 exit 70、同一工件在无现场变化时摘要不稳定、原子发布后读回失败、文件身份竞态或无法解释的 `INVALID`，应停止 Approval 和后续变更准备，保留现场并立即升级。
