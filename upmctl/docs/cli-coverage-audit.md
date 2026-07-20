# upmctl CLI 功能与测试覆盖审计

## 结论

当前 `upmctl` 可以作为 Phase 2b2a 的“环境接管、只读观察、VM start 规划和人工审批证据”候选版本继续做真实环境验收，但不能被判定为“满足管理员全部日常运维需求”。实际 VM 启停、Kubernetes 节点增减、集群生命周期、Addon、Apply、Executor 和 Operation 均未实现；真实测试只能证明这些入口安全失败关闭，不能把它们记录为功能 PASS。

机器可读的命令、风险和用例清单见 [cli-coverage-matrix.yaml](cli-coverage-matrix.yaml)。完整验收必须按功能故事执行，而不是只运行若干成功命令。

无需访问测试环境的离线命令契约检查位于 `upmctl/scripts/tests/audit-cli-contract.sh`。它覆盖帮助、JSON/JSONL、usage、关键解析回归、运行日志及未开放命令的 fail-closed 行为；通过 `UPMCTL_BIN=/absolute/path/to/upmctl` 绑定被测制品。

真实宿主机上的非变更覆盖脚本位于 `upmctl/scripts/tests/host-safe-cli-coverage.sh`。它要求显式 workspace/report-dir，只在 `MANAGED_VALID` 后执行全部帮助、version/capabilities、`vm status` 两种形式、参数负向和关闭命令，并对 `.upmctl`、Vagrant、libvirt、Kubernetes 状态做前后快照比较。Adopt、ACTION_REQUIRED Plan 和 Approval 生命周期只生成检查点，不由脚本模拟人类或改变 VM。

## 审计基线

- 本地 `go test ./... -coverprofile`：总语句覆盖率 `67.0%`。
- 跨包 `go test ./... -coverpkg=./...`：合并语句覆盖率 `68.7%`。
- `internal/cli` 当前包覆盖率约 `51.1%`；跨包运行中约 `53.4%`。
- `cmd/upmctl`、`internal/buildinfo` 没有直接语句覆盖。
- CLI 的 text writer 路径在当前覆盖报告中全部为 `0%`；主要 CLI 测试集中在 JSON。
- `runVM` 在当前包覆盖报告中为 `0%`，现有真实环境脚本只覆盖 `vm list` 和 `vm inspect`，没有覆盖 `vm status` 两种形式。

覆盖率只能说明代码是否被执行，不能证明环境行为正确。真实验收还必须检查 VM/libvirt/Kubernetes 身份关联、前后状态、文件权限、不可变工件和底层命令边界。

## 当前真实环境验证脚本覆盖

`upmctl/scripts/validate-test-environment.sh` 默认覆盖：

- `version`
- `capabilities`
- `context discover`
- `config validate`
- `status`
- `vm list`
- `vm inspect NODE`

使用 `--include-plan` 且产生持久化 `ACTION_REQUIRED` Plan 时，条件覆盖：

- `plan vm start --node NODE`
- `plan get PLAN_ID`
- `plan validate PLAN_ID`
- `preflight --plan-id PLAN_ID`

脚本已经正确区分 `PASS/BLOCKED/FAIL`，并把成功业务 envelope 的 Preflight exit `3` 当作 Phase 2b2a 契约，而不是普通失败。

脚本尚未覆盖：

- 根帮助、四个分组帮助、`--help` 和 `-h`；
- `vm status` 和 `vm status NODE`；
- 真实人类 TTY `environment adopt`；
- 真实人类 TTY `approval grant/revoke`；
- `approval get/list` 及过滤列表；
- 所有参数负向、未知参数、重复参数、空参数和三种输出格式；
- 所有未实现命令的真实二进制 fail-closed 检查；
- JSON Schema 校验，目前只使用 `grep` 检查 kind/requestId；
- 除运行日志和可选 Plan 外的前后文件树差异证明；
- VM、domain、Kubernetes Node 的前后身份与电源状态不变证明；
- 底层命令审计。运行日志只记录规范化 `upmctl` 命令，不能证明实现内部没有调用目标变更命令。

## 命令风险分层

| 分类 | 命令 | 真实环境执行策略 |
| --- | --- | --- |
| 离线 | help、version、capabilities、参数负向 | 可直接自动执行 |
| 被动本地读取 | context、config、plan get/validate、approval get/list | 可直接执行；保留文件树前后摘要 |
| 外部只读观察 | status、vm list/status/inspect、preflight | 可执行；必须绑定 `MANAGED_VALID` 工作区，并保存 VM/libvirt/Kubernetes 前后快照 |
| 本地控制状态写入 | `plan vm start` 的 ACTION_REQUIRED | 可由自动化执行；只允许新增一个不可变 Plan，不得改变目标环境 |
| 人类 TTY 控制状态写入 | environment adopt、approval grant/revoke | 只能由管理员在真实控制终端执行；Agent 不得代输 reason/challenge |
| 当前关闭 | apply、VM 变更、cluster/node/addon/operation/verify/report | 只测试 `UPMCTL_NOT_IMPLEMENTED`、exit 3、零底层调用和零执行状态写入 |

## 真实完整故事的测试条件

健康的全运行集群只能验证 `plan vm start` 的 `NOOP`。要覆盖 `ACTION_REQUIRED -> preflight -> approval grant -> get/list -> revoke`，必须先有一个停止的隔离普通 Worker。当前 `upmctl` 本身不能停止 VM，因此只能采用以下一种方式：

1. 使用专门构造且初始就有一个停止 Worker 的一次性测试环境；或
2. 在取得明确变更授权后，由管理员通过测试夹具流程停止普通 Worker，记录前置状态，并在验收结束后恢复。

未经显式授权，不应为了获得 ACTION_REQUIRED 而直接运行 `vagrant halt`、`virsh shutdown` 或其他变更命令。控制平面节点不应作为首选测试目标。

## 发现的缺口与问题

### P0：产品范围不能满足“全部管理员日常运维”

以下故事当前只能标记 `NOT_APPLICABLE_CURRENT_PHASE` 或“安全关闭已验证”，不能标记功能通过：

- VM start/stop/restart 和交互 SSH；
- Kubernetes node add/remove；
- cluster deploy/start/stop/restart/destroy；
- Addon install/upgrade/uninstall；
- Apply、Plan Claim、环境锁、Operation journal、cancel/resume；
- 完整 CNI、存储、监控、UPM 平台和业务后置验证。

因此本阶段的正确发布声明应是“Phase 2b2a 控制面候选版本”，而不是“管理员完整运维 CLI”。

### 已修复：无参数命令对未知或多余参数没有统一失败关闭

审计时发现原实现存在以下行为：

```text
upmctl version unexpected --output json     -> exit 0
upmctl capabilities --typo --output json    -> exit 0
```

本轮已修复 `version`、`capabilities`、`status`、`context discover` 和 `config validate` 的多余位置参数/未知选项处理，并补充未知前置全局选项的 usage 分支；这些情况现在返回 `UPMCTL_USAGE`/exit 2。定向测试覆盖了上述命令且不访问环境。

本轮进一步关闭了全局参数歧义：`--workspace`、`--output`、`--request-id`、`--timeout`、`--log-file`和`--no-color`重复出现时统一返回`UPMCTL_USAGE`/exit 2；空workspace、request ID和日志路径也被拒绝，不再使用后值静默覆盖。

### P1：完整真实 E2E 仍缺少可重复的隔离状态

`environment adopt` 不提供 reset/unadopt，且已有任意 `.upmctl` 状态会拒绝再次接管。清理测试环境只能通过 CLI 外的受控流程完成。直接删除真实工作区 `.upmctl` 会破坏审计链，不应作为普通日常命令。

发布级 E2E 应使用可丢弃 workspace 快照或专用克隆，并记录：

- 清理前后文件清单和 SHA-256；
- 只删除本轮测试创建的 `.upmctl` 工件；
- 不删除 `.vagrant`、Vagrantfile、config、kubeconfig 或 VM domain；
- 清理动作由人与测试负责人复核。

### P1：帮助与能力发现对 Agent 不够精细

`capabilities` 只提供 `approval.manage=true`，但该能力同时包含 Agent 可读的 `get/list` 和仅人类 TTY 可写的 `grant/revoke`。Agent 不能仅靠这个布尔值安全决策。建议后续拆分为至少：

- `approval.read=true`
- `approval.grant.humanTty=true`
- `approval.revoke.humanTty=true`
- `environment.adopt.humanTty=true`
- `executionAvailable=false`

或在 capability 对象中增加 `actor`、`sideEffects`、`requiresTTY`、`executionAvailable` 字段。

另外，常见的 `upmctl vm --help` 当前返回 `UPMCTL_NOT_IMPLEMENTED`/exit 3；只支持 `upmctl help vm`。这不是安全缺陷，但会影响管理员上手和常见 CLI 习惯。

### P1：错误码文档不完整

实现中存在、但故障排除错误码表没有完整列出的顶层错误至少包括：

- `UPMCTL_WORKSPACE_REQUIRED`
- `UPMCTL_ENVIRONMENT_ID_INVALID`
- `UPMCTL_WORKSPACE_UNSAFE`
- `UPMCTL_ADOPTION_NOT_CONFIRMED`
- `UPMCTL_ADOPTION_EVIDENCE_INVALID`
- `UPMCTL_ADOPTION_EVIDENCE_FAILED`
- `UPMCTL_APPROVAL_EVIDENCE_FAILED`
- `UPMCTL_MANAGED_STATE_STORE_FAILED`
- `UPMCTL_OUTPUT_WRITE_FAILED`

真实测试报告必须使用实现返回的稳定 code；文档表应补齐 code、exit、原因和恢复动作。

### 已冻结：Plan工件与漂移错误语义

- 已存Plan在严格Store读取阶段发生Schema、摘要、ID或封闭动作契约破坏时，顶层错误统一为`UPMCTL_PLAN_INVALID`、exit 3，且不返回部分Plan。
- `UPMCTL_PLAN_TAMPERED`保留为安全解码后的内存校验投影blocker，不与Store读取错误混用。
- ConfigDigest漂移统一为`UPMCTL_CONFIG_DRIFT`；Managed State和Observed State分别使用对应的drift code。

### P2：输出和参数组合覆盖不足

- text writer 当前覆盖报告为 `0%`，但管理员默认使用 text；
- JSONL 只在 output 包层验证，缺少所有主要命令的 CLI 契约验证；
- `--no-color` 没有直接测试；
- `--request-id` 没有长度、字符集和控制字符约束，且该值会进入日志；
- 缺少 stdout 写失败、stderr 写失败和日志结束事件写失败的系统级测试；
- `traceability.yaml` 中 `UPMCTL-OP-002` 出现重复键，应在发布门禁增加 YAML 重复 key 检查。

## 推荐的发布门禁

完整测试报告必须分别给出：

1. `IMPLEMENTED_PASS`：已实现命令的正向、负向和真实环境证据全部通过；
2. `SAFETY_BOUNDARY_PASS`：所有关闭命令均稳定失败关闭，没有目标变化；
3. `NOT_APPLICABLE_CURRENT_PHASE`：未来 VM/Node/Cluster/Addon/Operation 故事，不能计入 PASS 分母；
4. `BLOCKED`：因缺少停止 Worker、人工 TTY 或依赖而未执行的当前阶段必测场景；
5. `FAIL`：命令、Schema、退出码、日志、文件权限、身份关联或前后状态不符合契约。

只有当前阶段所有 `requiredLiveCoverage=true` 用例没有 `FAIL/BLOCKED`，且人工签署确认测试环境已恢复，才能把 Phase 2b2a 标记为真实环境验收通过。即使如此，也不能宣称未实现的管理员变更能力已经可用。
