# upmctl 安全与负向测试方案

## 1. 目标与判定原则

本方案验证 `upmctl` 在不可信路径、控制状态篡改、人工在场缺失、依赖异常和未支持命令下能否安全失败。负向用例的通过标准不是“命令一定返回非零”：只读观察允许在依赖不可用时返回结构化的 `unavailable` 数据源和 finding，但后续 Plan/Preflight 必须因此阻断，且任何场景均不得回退执行 legacy Shell、Vagrant 变更、kubectl 变更或写入未授权控制状态。

测试分为两层：

1. `LOCAL-AUTO`：临时 fixture、Go 单元/集成测试，可重复执行，不接触真实 VM 或 Kubernetes。
2. `HOST-SAFE`：在测试主机使用发布二进制验证文件权限、非 TTY、依赖可见性和 unsupported 契约；不得停止 VM、修改 Kubernetes 或模拟人工 TTY。

人工 `environment adopt`、`approval grant` 和 `approval revoke` 的正确 challenge 流程属于单独的人类验收。自动化只能验证非 TTY 被拒绝，不能代替工程师输入 reason/challenge。

## 2. 测试数据和隔离要求

- 所有破坏性 fixture 必须位于测试框架创建的临时目录，禁止直接篡改真实工作区的 `.upmctl`、`.vagrant`、Vagrantfile、config.rb 或 kubeconfig。
- `HOST-SAFE` 用例需要写文件时，只能写入独立的测试目录和专用日志目录。
- 测试不得记录密码、kubeconfig 内容、私钥、环境变量、reason、typed challenge、Plan/Approval 完整内容或外部命令输出。
- 每个主机用例保存命令、UTC 时间、exit code、稳定 error code、stdout/stderr 摘要和测试后状态证明。
- 对返回 `0` 但数据源为 `unavailable` 的观察命令，结果应记为“契约通过、环境阻断”，不得误记为环境健康。

## 3. 专业负向用例

| ID | 层级 | 场景 | 预期结果 | 自动化证据 |
|---|---|---|---|---|
| SEC-FS-001 | LOCAL-AUTO | Adopt workspace 本身是 symlink | 拒绝，且不创建 `.upmctl` | `managedenv.TestPrepareRejectsInvalidEnvironmentIDAndSymlinkWorkspace` |
| SEC-FS-002 | LOCAL-AUTO | libvirt `id` 是 symlink、非法或重复 UUID | `METADATA_INVALID`，不接管 | `managedenv.TestPrepareRejectsUnsafeOrAmbiguousLegacyIdentity` |
| SEC-FS-003 | LOCAL-AUTO | `.upmctl`、plans、approval 路径为 symlink | 拒绝且不写入 symlink 目标 | `controlstate.TestSymlinkAndEscapeAttemptsAreRejected`、Plan/Approval store tests |
| SEC-FS-004 | LOCAL-AUTO | ID/相对目录包含 `..`、绝对路径、分隔符或控制字符 | 文件系统访问前拒绝 | `controlstate.TestInvalidPathsAreRejectedBeforeFilesystemAccess` |
| SEC-FS-005 | LOCAL-AUTO | state/control 目录权限由 `0600/0700` 放宽 | Trust 变为 `INVALID` 或 store unsafe | `context.TestManagedTrustFailsClosedAcrossIdentityAndPermissionChanges` |
| SEC-FS-006 | HOST-SAFE | 日志目标是 symlink、目录、FIFO 或已有非 0600 文件 | `UPMCTL_LOG_OPEN_FAILED`，exit 70，业务命令不运行 | logging/CLI tests；主机专用临时路径 |
| SEC-JSON-001 | LOCAL-AUTO | Managed State 重复 JSON key、未知字段、尾随 JSON | Trust=`INVALID` | Context strict JSON tests |
| SEC-JSON-002 | LOCAL-AUTO | Plan 重复 key、未知字段、尾随 JSON | Plan 读取失败，不生成后续状态 | `plan.TestStoreReadUsesStrictJSON` |
| SEC-JSON-003 | LOCAL-AUTO | Approval/Revocation 重复嵌套 key或尾随 JSON | 严格拒绝，不能投影为 APPROVED/REVOKED | Approval/Admission store tests |
| SEC-JSON-004 | LOCAL-AUTO | state 或控制工件超过大小上限 | 拒绝且不部分读取 | `context.TestManagedTrustFailsClosedAcrossIdentityAndPermissionChanges`、store size tests |
| SEC-DIG-001 | LOCAL-AUTO | Vagrantfile/config/kubeconfig 在接管后发生摘要漂移 | Trust=`INVALID`，禁止外部观察 | Managed Environment 与 Context tests |
| SEC-DIG-002 | LOCAL-AUTO | Plan basis 的 config/managed/observed digest 漂移 | Plan validate/Preflight 返回 blocker | App Preflight drift tests |
| SEC-DIG-003 | LOCAL-AUTO | Approval reason、ID、digest 或 Plan 绑定被修改 | Approval integrity 校验失败 | Approval model/store tests |
| SEC-TRUST-001 | LOCAL-AUTO | 无 state 的 legacy 工作区 | `LEGACY_UNTRUSTED_READONLY`，status 不执行外部命令 | CLI legacy status tests |
| SEC-TRUST-002 | LOCAL-AUTO | 合法 state 被移除 | 从 `MANAGED_VALID` 降级回 legacy read-only，environmentId 清空 | `context.TestTrustTransitionManagedStateRemovalReturnsToLegacyReadOnly` |
| SEC-TRUST-003 | LOCAL-AUTO | state、绑定文件或 machine UUID 身份失效 | fail closed 为 `INVALID`，不继续受管观察 | `context.TestManagedTrustFailsClosedAcrossIdentityAndPermissionChanges` |
| SEC-TTY-001 | LOCAL-AUTO/HOST-SAFE | 非 TTY 调用 environment adopt | `UPMCTL_HUMAN_TTY_REQUIRED`，不创建 state | Environment Adopt CLI test |
| SEC-TTY-002 | LOCAL-AUTO/HOST-SAFE | 非 TTY 调用 approval grant | 同上，且不做外部观察/写 Approval | Approval CLI test |
| SEC-TTY-003 | LOCAL-AUTO/HOST-SAFE | 非 TTY 调用 approval revoke | 同上，且不读取 workspace 控制状态 | `cli.TestNonTTYRevocationStopsBeforeWorkspaceOrDependencyAccess` |
| SEC-TTY-004 | LOCAL-AUTO | 传入 `--reason`、`--yes`、`--force` 或 actor/subject 注入参数 | `UPMCTL_USAGE`，不能旁路人工输入 | Approval CLI parsing tests |
| SEC-TTY-005 | 人工 | challenge 不精确匹配或 reason 为空 | 稳定拒绝，不发布 Adopt/Approval/Revocation | 由人在隔离 fixture TTY 执行，不自动化模拟 |
| SEC-LOG-001 | LOCAL-AUTO | 成功 Adopt 含 workspace、环境 ID、reason、challenge、UUID | JSONL 只保留允许字段，敏感值均不存在 | `cli.TestSuccessfulAdoptionRuntimeLogExcludesTrustBoundarySecrets` |
| SEC-LOG-002 | LOCAL-AUTO | 未知命令名包含客户 token/秘密 | 日志 command=`unknown`，不复制原始输入 | `cli.TestUnknownCommandRuntimeLogDoesNotCopyUserInput` |
| SEC-LOG-003 | LOCAL-AUTO | JSON stdout 与运行日志同时启用 | stdout 保持单一 envelope；日志为独立合法 JSONL | CLI runtime log tests |
| SEC-PROC-001 | LOCAL-AUTO/HOST-SAFE | 外部只读命令超过 `--timeout` | `UPMCTL_INTERRUPTED`，exit 6；底层错误可被 `errors.Is(...DeadlineExceeded)` 识别 | Runner 与 CLI timeout tests |
| SEC-PROC-002 | LOCAL-AUTO/HOST-SAFE | vagrant/virsh/kubectl 缺失或不可执行 | 观察结果明确标记 source=`unavailable` 并输出 finding；Plan/Preflight 不得放行 | `cli.TestMissingObservationDependenciesReturnExplicitUnavailableSources` |
| SEC-PROC-003 | LOCAL-AUTO | 参数包含 `$()`、`;` 等 shell 字符 | 原样作为 argv，不做 shell 展开 | Runner argument-preservation tests |
| SEC-CAP-001 | LOCAL-AUTO/HOST-SAFE | vm stop/restart、node add/remove、addon、operation、apply | `UPMCTL_NOT_IMPLEMENTED`，exit 3，runner 调用数为 0 | `cli.TestUnsupportedOperationsFailClosedWithoutRunnerCalls` |

## 4. 自动化执行

```bash
cd upmctl
env GOCACHE=/private/tmp/upmctl-security-go-build \
  go test ./internal/runner ./internal/context ./internal/managedenv \
          ./internal/controlstate ./internal/plan ./internal/approval \
          ./internal/admission ./internal/logging ./internal/cli -count=1
```

发布前仍需运行完整门禁：

```bash
cd upmctl
env GOCACHE=/private/tmp/upmctl-go-build make check
```

## 5. 主机安全验证重点

`HOST-SAFE` 应在清理并重新部署 RC 后执行，至少保存以下证据：

1. 安装包、manifest、内外 SHA-256 和安装目标权限。
2. `version`、`capabilities`、全部 help 和 unsupported 命令的 exit/error code。
3. 非 TTY adopt/grant/revoke 均被拒绝，且测试前后真实工作区控制状态快照一致。
4. 安全日志文件为 0600，JSONL 合法，无 workspace、ID、reason、challenge 和参数值。
5. 依赖存在时真实只读观察完整；人为隐藏单个依赖的隔离进程测试只能改变该进程 `PATH`，不能卸载主机软件。
6. 极短 timeout 应稳定返回 interrupted 或结构化 unavailable，不得挂死、泄露输出或留下临时状态。
7. 测试完成后证明 VM、libvirt domain、Kubernetes Node 及真实工作区内容未因负向测试发生变更。

## 6. 当前本地执行记录

- 日期：2026-07-17
- 平台：开发机本地 fixture
- 结果：Runner、Context、CLI 定向安全测试通过。
- 修复：Runner 在子进程被 context timeout 终止后显式传播 `context.DeadlineExceeded`，确保上层稳定映射 `UPMCTL_INTERRUPTED`。
- 边界：该记录不是 `192.168.21.95` 的真实主机验收结果；真实环境证据必须由整体验收报告单独记录。
