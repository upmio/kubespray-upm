# upmctl 管理员日常运维整体测试计划

状态：`Ready for execution`
适用版本：Phase 2b2a
测试对象：`upmctl` CLI、Release 安装包、Codex Skill 和本地控制状态契约

## 1. 目的

本计划以 Kubernetes 实验/研发环境管理员的日常工作为主线，验证 `upmctl` 当前交付能力是否可安装、可理解、可观察、可审计、可安全拒绝，并能在故障后给出稳定、可执行的恢复指引。

本次测试不以“命令能运行”为唯一成功标准。每项能力必须同时验证：

- 命令返回的业务语义、JSON kind、稳定错误码和退出码；
- `requestId`、JSONL 运行日志和证据文件能够关联；
- Vagrant、libvirt、Kubernetes 三方观察与真实现场一致；
- 只读操作不改变 VM、Guest、Kubernetes 或控制状态；
- 人工信任和审批操作只能由本地人类控制 TTY 完成；
- 未实现能力明确拒绝，且不回退调用 legacy Shell 或底层变更命令；
- 测试结束后能够恢复到已声明的基线，不遗留未知制品。

详细步骤和判定见 [管理员日常运维测试用例](admin-operations-test-cases.md)。

## 2. 能力分层与判定原则

所有测试项必须标记为下列三种能力类别之一。

| 类别 | 含义 | 当前阶段如何验收 |
| --- | --- | --- |
| `CURRENT_IMPLEMENTED` | Phase 2b2a 已实现能力 | 在真实测试环境执行成功、阻塞和故障分支；核对现场、输出、日志和状态工件 |
| `CURRENT_REFUSAL_CONTRACT` | 当前必须安全拒绝的命令、输入或环境 | 返回规定的 Error、退出码和 remediation，且无目标环境变更、无越权状态写入即为 PASS |
| `V1_FUTURE_NOT_IMPLEMENTED` | V1 Spec 最终目标，但 Phase 2b2a 尚未实现 | 只验证 `UPMCTL_NOT_IMPLEMENTED` 和零副作用；报告必须记为 `NOT_APPLICABLE_CURRENT_PHASE`，不能写成业务功能 PASS |

当前已实现范围：

- `help`、`version`、`capabilities`；
- `environment adopt`；
- `context discover`、`config validate`、`status`；
- `vm list/status/inspect`；
- `plan vm start`、`plan get/validate`；
- `preflight`；
- `approval grant/get/list/revoke`；
- text、JSON、JSONL 输出和显式 `--log-file` 运行日志；
- Release 安装、升级、回滚和卸载操作流程。

以下能力当前没有执行实现：

- `apply`、Executor、Operation、Plan Claim、环境锁；
- VM 实际 start/stop/restart、`vm ssh`；
- Cluster deploy/start/stop/restart/destroy；
- Kubernetes Node add/remove/list/status；
- Addon install、verify、report；
- MCP Server。

因此，本轮测试不得宣称 `upmctl` 已经完成 VM 启停、节点扩缩、集群部署或 Addon 运维。Approval 是人工意图证据，不是执行结果；Preflight 即使通过也必须保持：

```text
applyDecision=BLOCKED
executionAvailable=false
```

## 3. 管理员角色与功能故事

### 3.1 角色

| 角色 | 责任 |
| --- | --- |
| 发布管理员 | 接收、校验、安装、升级、回滚和卸载 Release |
| 环境管理员 | 建立受管环境信任、进行班前巡检和 VM 身份核对 |
| 变更发起人 | 生成并检查不可执行 Plan，执行 Preflight |
| 审批人 | 在本机控制 TTY 阅读摘要、输入原因和 challenge，批准或撤销 |
| 故障处理人 | 使用 requestId、错误 envelope、运行日志和支持包定位问题 |
| 复核人 | 独立核对证据、现场零副作用和最终交付结论 |

同一测试人员可以承担多个角色，但人工接管和审批步骤必须明确记录实际操作者、时间和变更单号。密码、challenge、私钥、kubeconfig 内容不得进入报告。

### 3.2 功能故事

| Story ID | 管理员故事 | 当前交付结论 |
| --- | --- | --- |
| OPS-01 | 作为发布管理员，我希望验证 Release 来源、摘要和平台后安全安装 CLI | 已实现 |
| OPS-02 | 作为发布管理员，我希望原子升级并在兼容前提下回滚二进制 | 已实现为部署流程 |
| OPS-03 | 作为环境管理员，我希望把身份完整的 legacy libvirt 工作区一次性接管为受管环境 | 已实现，仅本地人类 TTY |
| OPS-04 | 作为值班管理员，我希望快速确认上下文、配置和总体状态是否可信 | 已实现 |
| OPS-05 | 作为值班管理员，我希望关联 Vagrant、libvirt 和 Kubernetes Node 身份并查看单 VM 详情 | 已实现，只读 |
| OPS-06 | 作为变更发起人，我希望对 VM start 先生成不可变计划并判断 NOOP、BLOCKED 或 ACTION_REQUIRED | 已实现，仅 Plan |
| OPS-07 | 作为变更发起人，我希望在审批和执行前重新检查配置、身份和现场漂移 | 已实现，只读且 Apply 固定阻塞 |
| OPS-08 | 作为审批人，我希望在可信 TTY 上批准或撤销一份精确 Plan，并可供他人查询 | 已实现，不授予执行能力 |
| OPS-09 | 作为故障处理人，我希望按 requestId 关联 stdout/stderr 和最小化 JSONL 日志 | 已实现 |
| OPS-10 | 作为安全管理员，我希望 symlink、路径逃逸、不安全权限、篡改工件和非 TTY 操作被拒绝 | 已实现的安全契约 |
| OPS-11 | 作为发布管理员，我希望卸载 CLI 时保留环境、VM 和审计状态 | 已实现为部署流程 |
| OPS-12 | 作为环境管理员，我希望通过 upmctl 实际启动、停止和重启 VM | V1 Future；当前不支持 |
| OPS-13 | 作为集群管理员，我希望添加下一个 Worker 或安全删除最高编号 Worker | V1 Future；当前不支持 |
| OPS-14 | 作为平台管理员，我希望部署集群、管理 Addon、执行、恢复和生成报告 | V1 Future；当前不支持 |

## 4. 测试环境

### 4.1 真实环境基线

| 项目 | 本轮取值 |
| --- | --- |
| 测试主机 | `192.168.21.95` |
| 操作系统 | Rocky Linux/RHEL 9 系列，实际版本写入证据 |
| 架构 | `linux/amd64` |
| 登录用户 | `root`，仅用于当前专用测试环境；凭据由带外渠道管理 |
| 精确工作区 | `/root/kubespray-upm-current/vagrant_setup_scripts/kubespray-upm` |
| Provider | libvirt，URI 预期为 `qemu:///system` |
| VM | `k8s-1..k8s-5`，实际数量和状态以测试开始时证据为准 |
| Kubernetes | 对应 Node 身份和 Ready 状态以绑定 kubeconfig 的实时查询为准 |
| 安装路径 | `/usr/local/bin/upmctl` |
| 运行日志 | `/var/log/upmctl/runtime.jsonl` 或本次证据目录中的私有日志 |
| 证据根目录 | `/var/lib/upmctl/validation/<run-id>` |

不得改用 `/root/mg-95node/kubespray-upm` 或其他相邻 checkout。所有环境相关命令必须显式传递上述 `--workspace`。

### 4.2 隔离负向测试区

以下故障注入禁止在真实受管工作区直接进行，必须使用位于同一主机、权限隔离的临时副本或测试夹具：

- 修改 `config.rb`、Vagrantfile、kubeconfig 或 Managed State；
- 构造重复 JSON key、未知字段、超限文件、错误权限或 symlink；
- 篡改 Plan、Approval、Admission；
- 模拟重复 UUID、混合 provider、缺失 metadata；
- 日志 FIFO、设备文件、symlink 和非 `0600` 文件测试。

临时副本不得连接或操作真实 libvirt domain。所有可能触发外部观察的用例应使用专门 fixture 或在调用前证明命令会在安全解析阶段拒绝。

## 5. 测试前清理与基线重建

### 5.1 清理边界

“清理测试环境”仅表示清除上一次 `upmctl` 测试安装和本地测试工件，并把 VM/Kubernetes 恢复到声明基线，不等于销毁集群。

允许在取得测试负责人批准后处理：

- 旧的 `/usr/local/bin/upmctl` 和测试用版本备份；
- 本轮专用 `/var/log/upmctl/` 日志；
- 本轮专用 `/var/lib/upmctl/validation/<run-id>` 证据目录；
- 为重复测试 adoption 而清除的精确工作区 `.upmctl` 控制状态。

默认禁止清理：

- `.vagrant`、Vagrantfile、`vagrant/config.rb`、inventory、kubeconfig；
- libvirt domain、磁盘、网络和存储池；
- Kubernetes Node、Pod、PV/PVC、CNI、Addon；
- SSH key、Vagrant 用户目录或其他 checkout。

Phase 2b2a 没有 `environment reset` 命令。若为测试 adoption 必须删除 `.upmctl`，应先停止所有 `upmctl` 会话，记录目录清单和摘要，将旧证据归档到工作区外，再由测试负责人对精确绝对路径进行人工复核。删除后必须证明 `.vagrant`、VM UUID 和 Kubernetes Node 未变化。该清理动作是测试夹具管理，不是产品能力。

### 5.2 基线证据

清理前后均应记录：

- `hostnamectl`、`uname -a`、运行身份；
- Release archive 和二进制 SHA-256；
- Vagrant、vagrant-libvirt、virsh、kubectl 版本和 libvirt URI；
- 精确工作区 canonical path；
- `.vagrant/machines/*/libvirt/id` 的名称和摘要，不复制私密内容；
- `virsh list --all --uuid --name` 结果；
- Kubernetes Node 名称、UID、InternalIP、Ready 状态；
- 工作区 `.upmctl` 是否存在及其文件清单、权限和摘要；
- 测试前 VM 电源状态和 Node Ready 状态。

## 6. 测试策略

### 6.1 测试层级

| 层级 | 目标 |
| --- | --- |
| Release Gate | `make check`、Release manifest、archive/内部摘要、release smoke |
| CLI Contract | 参数、输出格式、kind、requestId、错误码和退出码 |
| Real E2E Read-only | 真实 Vagrant/libvirt/Kubernetes 观察和身份关联 |
| Human TTY E2E | environment adopt、approval grant/revoke 的人工边界 |
| Control-state Integration | Plan、Approval、Revocation 的原子、不可变和安全读取 |
| Fault Injection | 路径、权限、漂移、篡改、依赖缺失、超时和中断 |
| Refusal E2E | 未实现命令和自动化审批绕过的零副作用拒绝 |
| Install Lifecycle | 安装、重复安装、升级、兼容回滚、卸载和重装 |

### 6.2 执行批次

1. `Wave 0 — 清理和取证`：确认精确主机/工作区，归档旧证据，恢复基线。
2. `Wave 1 — Release 与安装`：校验 archive、manifest、内部摘要并安装。
3. `Wave 2 — 未接管状态`：help/version/capabilities、legacy discover/validate、安全拒绝。
4. `Wave 3 — 人工接管`：本地 TTY adopt、状态文件权限、绑定和重复接管拒绝。
5. `Wave 4 — 日常只读巡检`：status、VM list/status/inspect、真实身份关联。
6. `Wave 5 — Plan`：运行中节点验证 NOOP；经单独授权准备的关机普通 Worker 验证 ACTION_REQUIRED。
7. `Wave 6 — Preflight 与 Approval`：get/validate、漂移前后 Preflight、grant/get/list/revoke、到期行为。
8. `Wave 7 — 故障和安全负向`：日志、路径、权限、依赖、超时、非 TTY、篡改夹具。
9. `Wave 8 — 未实现能力拒绝`：Apply、VM 变更、Cluster、Node、Addon、Operation、Verify/Report。
10. `Wave 9 — 升级、回滚和卸载`：验证二进制生命周期，证明工作区和 VM 不受影响。
11. `Wave 10 — 恢复与复核`：恢复全部 VM/Node 基线，生成摘要和签署结论。

### 6.3 ACTION_REQUIRED 测试前提

如果全部 VM 已经运行，`plan vm start` 的正确结果是 `NOOP`。为了真实验证持久化 Plan、Approval 和 Preflight，需要测试环境负责人通过独立变更单预先把一个无关键负载的普通 Worker 置为 poweroff，并记录操作工具、时间和前后状态。

该准备动作不是 `upmctl` 能力证明，不能计入产品 PASS。禁止停止 `k8s-1` 或 `k8s-2`，禁止在未核对工作负载、LocalPV/hostPath 和 PDB 的情况下停止 Worker。若无法安全建立此前提，ACTION_REQUIRED、Approval 和相关 Preflight 用例应判为 `BLOCKED`，不得伪造工件。

## 7. 通用测试协议

每条 CLI 用例都应：

1. 使用唯一且可读的 `--request-id`；
2. 环境命令显式使用精确 `--workspace`；
3. 自动化检查使用 `--output json` 或 `jsonl`；
4. 显式启用测试日志，记录真实退出码；
5. 分别保存 stdout、stderr，不把二者合并；
6. 校验 envelope 的 `apiVersion/kind/requestId/timestamp`；
7. 校验运行日志恰有一个 `start` 和一个终态事件；
8. 对 Error 校验 `code/message/remediation`；
9. 执行前后对允许写入的路径做文件清单和摘要差异；
10. 执行前后重新观察 VM、libvirt 和 Kubernetes 状态，证明无非预期变化。

## 8. 结果状态

| 状态 | 定义 |
| --- | --- |
| `PASS` | 实际结果完全符合当前类别的成功或拒绝契约，且证据完整、无非预期副作用 |
| `FAIL` | CLI 语义、退出码、日志、安全边界、真实现场或恢复结果任一不符合契约 |
| `BLOCKED` | 环境、依赖、授权或安全前提不足，尚不能判断产品正确性 |
| `NOT_APPLICABLE_CURRENT_PHASE` | V1 Future 业务结果当前不能测试；只完成拒绝契约检查 |

预期拒绝返回 2、3、4、6 或 70 并不自动是 FAIL。只有错误码、退出码、remediation 和零副作用同时符合对应契约，拒绝用例才可 PASS。

## 9. 进入与退出标准

### 9.1 进入标准

- Release 由可审计 commit 构建，工作树不是 `dirty`；
- archive 和内部文件 SHA-256 均通过；
- 测试主机、精确工作区和维护窗口已批准；
- Vagrant/libvirt/Kubernetes 基线证据已完成；
- 操作员能够使用真实本机控制 TTY；
- 故障注入副本和真实工作区严格分离；
- 若执行 ACTION_REQUIRED 流程，已有普通 Worker 安全下线授权和恢复方案。

### 9.2 当前 Phase 交付通过标准

- 所有 P0/P1 `CURRENT_IMPLEMENTED` 用例 PASS；
- 所有 P0/P1 `CURRENT_REFUSAL_CONTRACT` 用例 PASS；
- V1 Future 用例均明确标记 `NOT_APPLICABLE_CURRENT_PHASE`，且拒绝契约 PASS；
- 没有未解释的 VM、Node、文件、权限或摘要差异；
- 日志不含 workspace、参数值、Plan/Approval ID、reason、challenge 或凭据；
- 最终全部 VM/Node 恢复到批准的基线；
- 自动报告、人工 TTY 记录、制品摘要和复核签署齐全。

以下任一情况禁止宣告“完整可用交付”：

- ACTION_REQUIRED/Approval 链路因无安全测试节点而 BLOCKED；
- 只完成单元测试或 fixture，没有真实 Vagrant/libvirt/Kubernetes 观察；
- 由 Agent、CI、pipe 或 `expect` 代替人类完成 adopt/grant/revoke；
- Release commit 含 `-dirty` 或与报告中的 SHA-256 不一致；
- 把 `UPMCTL_NOT_IMPLEMENTED` 写成 VM 启停、节点扩缩或 Apply 已验证。

## 10. 证据与报告

每轮测试使用全新目录，至少包含：

```text
<run-id>/
├── test-summary.md
├── environment-baseline-before/
├── environment-baseline-after/
├── release/
├── commands/
├── runtime.jsonl
├── filesystem-diff/
├── human-tty-record.md
├── issues.md
└── evidence-sha256.txt
```

`human-tty-record.md`只记录操作者、命令、requestId、原因摘要、开始/结束时间和结果，不记录实际 challenge。支持包必须按 [日志与故障排除手册](operations-troubleshooting.md) 脱敏。

最终报告需分别给出：

- 当前已实现能力通过率；
- 安全拒绝契约通过率；
- BLOCKED 项及解除条件；
- V1 Future `NOT_APPLICABLE_CURRENT_PHASE` 清单；
- 测试前后现场差异；
- 已知限制、缺陷编号、责任人和复测结论；
- 发布负责人和独立复核人的签署。
