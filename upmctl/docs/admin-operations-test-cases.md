# upmctl 管理员日常运维测试用例

状态：`Ready for execution`
配套计划：[upmctl 管理员日常运维整体测试计划](admin-operations-test-plan.md)

## 1. 用例字段和公共变量

| 字段 | 说明 |
| --- | --- |
| ID | 稳定用例编号 |
| Story | 对应管理员功能故事 |
| Class | `CURRENT_IMPLEMENTED`、`CURRENT_REFUSAL_CONTRACT` 或 `V1_FUTURE_NOT_IMPLEMENTED` |
| Priority | P0 安全/主链路、P1 重要运维、P2 补充兼容性 |
| Level | Release、E2E、Human-TTY、Integration、Fault Injection |
| Preconditions | 可执行前提；不满足时判 BLOCKED，不得强行制造条件 |
| Expected | 业务语义、退出码、日志、现场和状态副作用 |
| Evidence | 必须归档的原始证据 |
| Recovery | 用例后的清理或恢复动作 |

真实环境公共变量：

```bash
HOST=192.168.21.95
WORKSPACE=/root/kubespray-upm-current/vagrant_setup_scripts/kubespray-upm
UPMCTL=/usr/local/bin/upmctl
REPORT_ROOT=/var/lib/upmctl/validation
RUN_ID=<UTC-time>-admin-ops
REPORT_DIR=${REPORT_ROOT}/${RUN_ID}
RUNTIME_LOG=${REPORT_DIR}/runtime.jsonl
```

测试人员不得把登录密码、kubeconfig 内容、SSH 私钥、审批 challenge 或 token 写入上述变量、命令文件和报告。

## 2. 用例总览

| 分组 | 用例 | 重点 |
| --- | --- | --- |
| A. Release/安装 | TC-REL-001..008 | 摘要、安装、重复安装、升级、回滚、卸载 |
| B. CLI基础契约 | TC-CLI-001..008 | help、version、capabilities、输出、requestId、参数 |
| C. 环境接管 | TC-ENV-001..010 | legacy、人工TTY、绑定、权限、重复和不安全接管 |
| D. 状态巡检 | TC-OBS-001..010 | context、config、status、VM三方身份关联和异常观察 |
| E. Plan | TC-PLAN-001..012 | NOOP、ACTION_REQUIRED、BLOCKED、持久化、读取、校验、漂移 |
| F. Preflight/Approval | TC-APR-001..012 | 固定阻塞、人工审批、查询、撤销、五态、非TTY拒绝 |
| G. 日志/安全/故障 | TC-OPS-001..014 | JSONL、权限、symlink、超时、依赖、支持包、恢复 |
| H. 未实现能力 | TC-FUT-001..010 | Apply、VM变更、节点、集群、Addon、Operation、MCP |

## 3. A — Release、安装、升级、回滚和卸载

### TC-REL-001 Release 外层和内部完整性

- Story/Class/Priority/Level：OPS-01 / `CURRENT_IMPLEMENTED` / P0 / Release
- Preconditions：取得待测 linux/amd64 archive、外层 `SHA256SUMS` 和来源记录。
- Steps：验证 archive SHA-256；列出 tar 内容；解包到全新私有目录；核对唯一顶层目录、`release-manifest.json`；执行内部 `sha256sum -c SHA256SUMS`。
- Expected：全部摘要 PASS；manifest 的 version/commit/buildDate/platform/archive/topLevelDir 与制品一致；包内包含二进制、`install.sh`、三本手册、Skill、验证脚本和报告模板；无绝对路径、`..`、设备文件或意外可执行文件。
- Evidence：archive hash、tar listing、manifest、内部 checksum 输出。
- Recovery：删除临时解包目录；保留只读证据。

### TC-REL-002 全新系统级安装

- Story/Class/Priority/Level：OPS-01 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：`/usr/local/bin/upmctl` 已按测试清理流程移除；工作区和 VM 不动。
- Steps：运行包内 `install.sh --prefix /usr/local`；检查二进制属主/权限；运行 `version` 和 `capabilities`。
- Expected：安装路径正确、普通文件、`root:root 0755`；版本元数据与 manifest 一致；不创建 systemd unit、监听端口、全局配置和工作区状态。
- Evidence：安装器输出、`stat`、version/capabilities JSON、安装前后文件差异。
- Recovery：保留安装供后续测试。

### TC-REL-003 重复安装同一不可变版本

- Story/Class/Priority/Level：OPS-01 / `CURRENT_IMPLEMENTED` / P1 / E2E
- Preconditions：TC-REL-002 PASS。
- Steps：记录二进制摘要；再次运行同一包安装器；重新取摘要和版本。
- Expected：安装成功或给出明确幂等行为；最终二进制摘要不变；工作区、VM 和 `.upmctl` 无变化。
- Evidence：两次摘要、安装器输出、现场差异。
- Recovery：无。

### TC-REL-004 平台不匹配制品拒绝

- Story/Class/Priority/Level：OPS-01 / `CURRENT_REFUSAL_CONTRACT` / P0 / Release
- Preconditions：取得实验性 linux/arm64 或 manifest 修改夹具，禁止执行其二进制。
- Steps：按部署门禁检查 manifest/platform，而不安装。
- Expected：交付流程拒绝将非 linux/amd64 制品安装到当前主机；现有二进制不变。
- Evidence：平台检查输出和拒绝记录。
- Recovery：销毁夹具。

### TC-REL-005 升级并读取旧控制状态

- Story/Class/Priority/Level：OPS-02 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：已接管环境并至少存在一份当前版本 Plan 或 Approval；目标版本声明 schema 兼容。
- Steps：保存旧版本和摘要；按手册原子替换新二进制；执行 version/capabilities/context/status/plan get/approval list。
- Expected：新版本元数据正确；旧工件按兼容声明安全读取，或以稳定错误拒绝而不修改；无半安装状态。
- Evidence：升级前后版本、摘要、命令输出、工件摘要。
- Recovery：进入 TC-REL-006 或保留新版本。

### TC-REL-006 兼容回滚

- Story/Class/Priority/Level：OPS-02 / `CURRENT_IMPLEMENTED` / P1 / E2E
- Preconditions：Release 说明确认向后兼容，旧二进制已备份。
- Steps：原子恢复旧二进制；运行 version/capabilities 和只读状态；读取控制状态。
- Expected：回滚版本正确；不删除或重写 `.upmctl`；若旧版不能解析新工件则稳定拒绝并要求恢复新版，禁止手改 JSON。
- Evidence：回滚命令、版本、工件摘要、错误 envelope（如有）。
- Recovery：恢复本轮最终批准版本。

### TC-REL-007 卸载保留环境和审计状态

- Story/Class/Priority/Level：OPS-11 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：已保存版本、二进制摘要和工作区基线。
- Steps：删除精确安装路径；验证命令不可发现；重新检查 `.upmctl`、`.vagrant`、libvirt domain 和 Kubernetes Node。
- Expected：仅二进制被移除；环境、VM、Node 和全部审计工件摘要不变；没有自动销毁或反安装动作。
- Evidence：卸载前后路径、文件/VM/Node 差异。
- Recovery：使用同一已验证 Release 重装并执行冒烟。

### TC-REL-008 重装后继续只读管理

- Story/Class/Priority/Level：OPS-11 / `CURRENT_IMPLEMENTED` / P1 / E2E
- Preconditions：TC-REL-007 PASS。
- Steps：重装同一版本；执行 version/context/status/approval list。
- Expected：CLI 恢复可用；正确读取原有 Managed State 和审计工件；不要求重新 adopt。
- Evidence：安装输出和只读命令 JSON。
- Recovery：保留最终安装。

## 4. B — CLI 基础和输出契约

| ID | Story/Class/Priority | Preconditions and Steps | Expected / Evidence / Recovery |
| --- | --- | --- | --- |
| TC-CLI-001 | OPS-01 / `CURRENT_IMPLEMENTED` / P0 | 在无工作区目录运行 `help`、`--help`、`-h` 和四个帮助主题 | 全部纯文本 stdout、exit 0；不发现环境、不创建 `.upmctl`、默认不写日志。保存 stdout 和目录差异 |
| TC-CLI-002 | OPS-01 / `CURRENT_REFUSAL_CONTRACT` / P1 | 运行未知 help topic | `UPMCTL_USAGE`、exit 2、stderr Error；不访问环境 |
| TC-CLI-003 | OPS-01 / `CURRENT_IMPLEMENTED` / P0 | 在无环境目录运行 version/capabilities，分别使用 text/json/jsonl | exit 0；JSON envelope kind 分别为 Version/Capabilities；phase 为 `phase-2b2a-human-approval` |
| TC-CLI-004 | OPS-01 / `CURRENT_IMPLEMENTED` / P0 | 检查 capabilities 全列表 | environment adopt、观察、Plan、Preflight、Approval 为 available；Apply、Executor、VM mutate、cluster、node scale、addon、operation、MCP 为 false |
| TC-CLI-005 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 显式 requestId 运行成功命令 | stdout requestId 精确匹配；日志同 requestId 有 start/complete；stderr 为空 |
| TC-CLI-006 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 显式 requestId 运行错误命令 | stderr Error requestId 精确匹配；日志为 start/error，errorCode 与 envelope 一致；stdout 为空 |
| TC-CLI-007 | OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 | 测试未知 output、非正 timeout、缺参数、重复业务参数、非法 Node/Plan/Approval ID | `UPMCTL_USAGE`、exit 2；不得创建控制状态或调用外部命令 |
| TC-CLI-008 | OPS-09 / `CURRENT_IMPLEMENTED` / P1 | 使用 `--no-color` 和 JSON/JSONL | 输出无 ANSI 控制码；自动化格式可逐条解析；保存解析结果 |

## 5. C — Legacy 环境接管

### TC-ENV-001 Legacy 被动发现和配置校验

- Story/Class/Priority/Level：OPS-03 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：精确工作区没有 `.upmctl`；Vagrant metadata 完整。
- Steps：运行 context discover、config validate 和 status；记录外部命令审计。
- Expected：context 标识 legacy/unmanaged；config 安全完整；legacy validate/status 不执行 Vagrantfile、不加载 kubeconfig、不调用 Vagrant/virsh/kubectl；不创建控制状态。
- Evidence：JSON、运行日志、进程/runner 审计、文件差异。
- Recovery：无。

### TC-ENV-002 非 TTY 接管拒绝

- Story/Class/Priority/Level：OPS-03 / `CURRENT_REFUSAL_CONTRACT` / P0 / E2E
- Preconditions：同 TC-ENV-001。
- Steps：从 SSH 非控制 TTY、pipe 或后台任务调用 `environment adopt`；不得使用 `expect` 模拟。
- Expected：`UPMCTL_HUMAN_TTY_REQUIRED`、exit 3；不创建 `.upmctl/state.json`；不调用外部观察或变更命令。
- Evidence：stderr、exit code、工作区差异。
- Recovery：无。

### TC-ENV-003 本地人类 TTY 成功接管

- Story/Class/Priority/Level：OPS-03 / `CURRENT_IMPLEMENTED` / P0 / Human-TTY E2E
- Preconditions：安全完整 legacy 工作区，操作者直接使用主机控制 TTY。
- Steps：执行 adopt；阅读 canonical workspace、绑定文件摘要和全部 VM UUID；输入非空原因和随机 challenge。
- Expected：exit 0、kind ManagedEnvironment；唯一持久变更为 `.upmctl/state.json`；目录/文件为 0700/0600；状态记录 environmentId、canonical workspace、文件/机器摘要、actor、humanPresence、reason、requestId、cliVersion；不修改 VM/Kubernetes/Vagrant metadata。
- Evidence：脱敏 TTY 记录、JSON、state 摘要/权限、现场前后差异。不得记录 challenge。
- Recovery：保留 Managed State 供后续测试。

### TC-ENV-004 接管后严格 readback

- Story/Class/Priority/Level：OPS-03 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：TC-ENV-003 PASS。
- Steps：重新运行 context discover 和 config validate；安全解析 state.json。
- Expected：`managed=true`、`trust=MANAGED_VALID`、environmentId 一致；绑定文件和 UUID 摘要全部匹配。
- Evidence：context/config JSON 和摘要核对表。
- Recovery：无。

### TC-ENV-005 重复接管拒绝

- Story/Class/Priority/Level：OPS-03 / `CURRENT_REFUSAL_CONTRACT` / P0 / Human-TTY E2E
- Preconditions：已有合法 state.json。
- Steps：再次运行相同或不同 environmentId 的 adopt。
- Expected：`UPMCTL_ENVIRONMENT_ALREADY_CONTROLLED`、exit 3；原 state 摘要和 mtime 不变；不合并、不覆盖。
- Evidence：错误 envelope、前后 stat/hash。
- Recovery：无。

### TC-ENV-006 已有任意控制状态时拒绝接管

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Integration
- Preconditions：隔离 legacy fixture 中只放置 plan/approval/admission/lock 等任一控制状态。
- Steps：从人工 TTY 调用 adopt。
- Expected：原子拒绝；不创建 state、不覆盖已有工件、不留下空 `.upmctl`。
- Evidence：错误、完整目录树差异。
- Recovery：销毁 fixture。

### TC-ENV-007 不安全 config 拒绝

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Fault Injection
- Preconditions：隔离副本；分别构造不安全 Ruby、重复字段、不完整配置。
- Steps：运行 config validate 和 adopt。
- Expected：validate 返回 invalid/blocked；adopt 返回 `UPMCTL_CONFIG_INVALID`、exit 3；不执行 Ruby，不创建状态。
- Evidence：fixture 摘要、JSON、外部命令审计。
- Recovery：销毁副本。

### TC-ENV-008 Provider 和 metadata 异常拒绝

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Fault Injection
- Preconditions：隔离副本；逐项构造其他/混合 provider、缺失/未知节点、非法/重复 UUID。
- Steps：运行 adopt。
- Expected：`UPMCTL_PROVIDER_UNSUPPORTED` 或 `UPMCTL_VAGRANT_METADATA_INVALID`、exit 3；不创建 state，不接触真实 domain。
- Evidence：每个变体的错误和目录差异。
- Recovery：销毁副本。

### TC-ENV-009 路径和 symlink 拒绝

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Fault Injection
- Preconditions：隔离副本；构造 workspace、Vagrantfile、config、kubeconfig、metadata symlink 或路径逃逸。
- Steps：运行 discover/validate/adopt。
- Expected：`UPMCTL_WORKSPACE_UNSAFE` 或对应安全错误、exit 3；不跟随 symlink 写入。
- Evidence：symlink 目标未变化的证明、Error。
- Recovery：销毁副本和外部哨兵文件。

### TC-ENV-010 并发接管原子性

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P1 / Integration
- Preconditions：专用 TTY 测试 fixture，可由两个人类会话同时确认；不可用自动 challenge 代输。
- Steps：并发发起两次 adopt。
- Expected：最多一次成功；另一请求稳定拒绝；最终只有一份完整合法 state；失败路径无空目录/临时文件，不能删除成功者工件。
- Evidence：两个 requestId、结果和最终目录树。
- Recovery：销毁 fixture。

## 6. D — 日常状态巡检和 VM 观察

| ID | Story/Class/Priority | Preconditions and Steps | Expected / Evidence / Recovery |
| --- | --- | --- | --- |
| TC-OBS-001 | OPS-04 / `CURRENT_IMPLEMENTED` / P0 | 在 Managed Valid 工作区运行 context discover | canonical workspace、environmentId、managed/trust 与 state 一致；exit 0 |
| TC-OBS-002 | OPS-04 / `CURRENT_IMPLEMENTED` / P0 | 运行 config validate | safe/valid/complete 为 true，配置值与 config.rb 支持子集一致；不执行 Ruby |
| TC-OBS-003 | OPS-04 / `CURRENT_IMPLEMENTED` / P0 | 运行 status | kind EnvironmentStatus；聚合 context/trust/config/VM；不能将结果描述为 CNI/Addon/业务 endpoint 完整健康 |
| TC-OBS-004 | OPS-05 / `CURRENT_IMPLEMENTED` / P0 | 运行 vm list 和无节点参数的 vm status | kind VMList；节点稳定排序；两次结果语义一致 |
| TC-OBS-005 | OPS-05 / `CURRENT_IMPLEMENTED` / P0 | 对 `k8s-1..k8s-N` 逐个运行 vm status | kind VMStatus；名称、UUID、电源状态、Kubernetes 映射与真实查询一致 |
| TC-OBS-006 | OPS-05 / `CURRENT_IMPLEMENTED` / P0 | 对控制面、约定节点和普通 Worker 运行 vm inspect | kind VMInspection；身份、InternalIP、资源、磁盘和 SSH endpoint 元数据齐全；endpoint 不等于可达性 |
| TC-OBS-007 | OPS-05 / `CURRENT_IMPLEMENTED` / P0 | 独立执行只读 Vagrant/virsh/kubectl 查询并与 CLI 关联 | 每台 VM 的 metadata UUID、libvirt UUID/domain 和 Kubernetes Node UID/IP 可解释；差异产生 finding，不被隐藏 |
| TC-OBS-008 | OPS-05 / `CURRENT_IMPLEMENTED` / P1 | 在隔离/可恢复条件下使 kubeconfig 不可用后运行 VM 观察 | Vagrant/libvirt 观察仍返回；Kubernetes 为 unknown/finding；不把未知写成 Ready。恢复 kubeconfig 后复测 |
| TC-OBS-009 | OPS-05 / `CURRENT_IMPLEMENTED` / P1 | 使用 metadata 已登记但 domain 缺失的安全 fixture | 仍返回该机器并标记不一致；不得凭缺失 domain 删除 metadata 或 VM |
| TC-OBS-010 | OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 | 请求未知节点、非法节点名或错误参数 | 稳定 Error/exit 2或3；不执行变更、不创建控制状态 |

每个真实观察用例执行前后均保存 VM 电源、domain UUID 和 Node UID/Ready 快照，差异必须为零或有独立变更单解释。

## 7. E — VM start Plan

### TC-PLAN-001 运行中 VM 返回 NOOP

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：目标 VM 真实 running，身份关联完整。
- Steps：对普通 Worker 和至少一个固定角色节点分别运行 `plan vm start --node`。
- Expected：kind Plan、exit 0、disposition `NOOP`；有合法 planId/planDigest；不写 `.upmctl/plans`；不调用任何变更命令。
- Evidence：Plan JSON、目录前后差异、VM/Node 快照和外部命令审计。
- Recovery：无。

### TC-PLAN-002 普通 Worker 产生 ACTION_REQUIRED

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：经独立批准的普通 Worker 已安全 poweroff，身份仍完整。
- Steps：运行 `plan vm start --node <worker>`。
- Expected：exit 0、`ACTION_REQUIRED`、riskLevel R1、TTL 严格 30 分钟；唯一写入为 0600 的不可变 Plan；VM 保持 poweroff，Node 不因 Plan 被改变。
- Evidence：变更单、Plan、文件权限/摘要、现场前后快照。
- Recovery：保留 Plan 进入 Approval 流程；最终由已批准的外部恢复步骤恢复 Worker，因为当前 upmctl 不能 Apply。

### TC-PLAN-003 固定角色节点风险为 R2

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P1 / Integration/E2E only if safely available
- Preconditions：不得为测试停止 `k8s-1` 或 `k8s-2`；优先使用真实观察 fixture 构造 poweroff 状态。
- Steps：生成 k8s-1/k8s-2 start Plan。
- Expected：ACTION_REQUIRED 时 riskLevel R2；若真实节点 running 则 NOOP。不得为取得 R2 结果破坏真实集群。
- Evidence：Plan 或 NOOP 输出。
- Recovery：销毁 fixture。

### TC-PLAN-004 不安全现场返回 BLOCKED

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P0 / Fault Injection
- Preconditions：安全 fixture 分别提供身份冲突、观察不完整、配置/Managed State 不可信状态。
- Steps：运行 plan vm start。
- Expected：disposition BLOCKED 或稳定 Error；有 blockers；不写 Plan 文件，不扩大目标范围。
- Evidence：输入 fixture、输出、文件差异。
- Recovery：销毁 fixture。

### TC-PLAN-005 Plan 原子和不可变持久化

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P0 / Integration
- Preconditions：TC-PLAN-002 产生 ACTION_REQUIRED。
- Steps：检查 plans 目录/文件类型、权限、大小、摘要；再次规划同一目标。
- Expected：每次 Plan 是独立不可变实例；无覆盖、无临时残留；只有 ACTION_REQUIRED 写文件。
- Evidence：目录树、stat、hash、Plan ID 列表。
- Recovery：保留本轮工件。

### TC-PLAN-006 Plan get 只读检查

- Story/Class/Priority/Level：OPS-06 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：有效 Plan 未过期。
- Steps：运行 plan get，记录执行前后工件摘要和外部命令调用。
- Expected：kind PlanInspection、exit 0；原 Plan 完整返回；`executionAvailable=false`；不观察现场、不写文件。
- Evidence：JSON、hash、外部命令审计。
- Recovery：无。

### TC-PLAN-007 Plan validate 本地绑定检查

- Story/Class/Priority/Level：OPS-07 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：有效 Plan 未过期且绑定未漂移。
- Steps：运行 plan validate。
- Expected：kind PlanValidation、exit 0、无 blockers；`observedStateBinding=NOT_CHECKED`、`executionAvailable=false`；不调用 Vagrant/virsh/kubectl。
- Evidence：JSON 和调用审计。
- Recovery：无。

### TC-PLAN-008 非法或不存在 Plan ID

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / E2E
- Preconditions：Managed Valid 工作区。
- Steps：测试短 ID、大写 hex、绝对路径、分隔符、`..` 和格式合法但不存在 ID。
- Expected：参数错误 exit 2 或 not-found 安全错误；不发生路径逃逸，不返回其他 Plan 数据。
- Evidence：Error 和外部哨兵文件未变化证明。
- Recovery：无。

### TC-PLAN-009 Plan 篡改拒绝

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Fault Injection
- Preconditions：隔离控制状态副本；分别修改 planId、planDigest、内容、增加未知字段/重复 key/尾随 JSON/超限/错误权限/symlink。
- Steps：运行 plan get 和 validate。
- Expected：严格Store读取返回顶层`UPMCTL_PLAN_INVALID`、exit 3；内存校验投影可使用`UPMCTL_PLAN_TAMPERED` blocker；不返回部分 Plan，不跟随 symlink。
- Evidence：每个变体的 Error 和哨兵证明。
- Recovery：销毁副本。

### TC-PLAN-010 Plan 到期边界

- Story/Class/Priority/Level：OPS-07 / `CURRENT_IMPLEMENTED` / P1 / E2E/Integration
- Preconditions：有效 Plan；可等待到 expiresAt，或用可控时钟集成测试。
- Steps：在到期前和 `now >= expiresAt` 分别 get/validate/preflight。
- Expected：get 可用于审计且 expired=true；validate/preflight 阻塞，exit 3；不得续期或改写 Plan。
- Evidence：时间戳、JSON、Plan hash。
- Recovery：无。

### TC-PLAN-011 Config/Managed State 漂移

- Story/Class/Priority/Level：OPS-07 / `CURRENT_IMPLEMENTED` / P0 / Fault Injection
- Preconditions：隔离副本中的有效 Plan。
- Steps：分别改变 config 或 Managed State 绑定后运行 validate/preflight。
- Expected：稳定漂移 blocker/Error、exit 3；不重写 Plan；不执行变更。
- Evidence：前后摘要、JSON。
- Recovery：销毁副本，禁止在真实工作区手工恢复 JSON。

### TC-PLAN-012 其他 Plan 命令拒绝

- Story/Class/Priority/Level：OPS-12/13/14 / `V1_FUTURE_NOT_IMPLEMENTED` / P0 / E2E refusal
- Steps：执行 plan vm stop/restart、plan cluster 全部动作、plan node add/remove、plan addon install。
- Expected：`UPMCTL_NOT_IMPLEMENTED`、exit 3；无 Plan、Operation、lock、Claim；无 legacy 脚本或目标环境变更。
- Evidence：每条 Error、运行日志、目录和现场差异。
- Recovery：无；业务状态记 `NOT_APPLICABLE_CURRENT_PHASE`。

## 8. F — Preflight 和人工 Approval

### TC-APR-001 无 Approval 的 Preflight

- Story/Class/Priority/Level：OPS-07 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Preconditions：有效未过期 ACTION_REQUIRED Plan，现场未漂移。
- Steps：运行 preflight 并捕获真实退出码。
- Expected：kind PreflightResult；十项检查顺序稳定；可为 `preflightStatus=PASSED`，但 exit 3、approvalStatus MISSING、applyDecision BLOCKED、executionAvailable false；不创建任何状态。
- Evidence：JSON、runtime exitCode=3、工件/现场差异。
- Recovery：无。

### TC-APR-002 Preflight 现场漂移

- Story/Class/Priority/Level：OPS-07 / `CURRENT_IMPLEMENTED` / P0 / Fault Injection/E2E if independently changed
- Preconditions：有效 Plan；通过安全 fixture 或独立变更改变 observed state。
- Steps：运行 preflight。
- Expected：BLOCKED，显示 expected/current observed digest 和稳定 blocker；不扩大或重写 Plan。
- Evidence：前后现场、JSON、Plan hash。
- Recovery：按独立恢复步骤恢复现场并重新观察。

### TC-APR-003 Preflight 超时或取消

- Story/Class/Priority/Level：OPS-10 / `CURRENT_IMPLEMENTED` / P1 / Fault Injection
- Preconditions：可安全模拟慢只读依赖。
- Steps：使用极短 `--timeout` 运行 preflight。
- Expected：`UPMCTL_INTERRUPTED`、exit 6；不报告 PASSED，不创建 Operation/lock/Claim/临时状态。
- Evidence：Error、日志和目录差异。
- Recovery：恢复依赖，使用正常 timeout 复测。

### TC-APR-004 非 TTY grant 拒绝

- Story/Class/Priority/Level：OPS-08 / `CURRENT_REFUSAL_CONTRACT` / P0 / E2E
- Preconditions：有效 ACTION_REQUIRED Plan。
- Steps：通过 pipe、后台、Agent/Skill 或无控制 TTY 调用 grant。
- Expected：`UPMCTL_HUMAN_TTY_REQUIRED`、exit 3；不创建 Approval。
- Evidence：Error、approvals 目录差异。
- Recovery：无。

### TC-APR-005 人工 grant 成功

- Story/Class/Priority/Level：OPS-08 / `CURRENT_IMPLEMENTED` / P0 / Human-TTY E2E
- Preconditions：有效 R1/R2/R3 ACTION_REQUIRED Plan，操作者直接使用本机控制 TTY。
- Steps：阅读 Plan 摘要；输入非空原因和精确 challenge；保存 approvalId。
- Expected：kind Approval、exit 0；唯一创建不可变 0600 Approval；完整绑定 Plan/environment/action/target/risk/scope/basis；TTL 为 `min(approvedAt+10m, plan.expiresAt)`；不创建 Claim/Operation，不改变 VM。
- Evidence：脱敏 TTY 记录、Approval JSON/hash、现场差异。禁止记录 challenge。
- Recovery：保留供查询和撤销。

### TC-APR-006 grant 绕过参数拒绝

- Story/Class/Priority/Level：OPS-08 / `CURRENT_REFUSAL_CONTRACT` / P0 / E2E
- Steps：测试 `--yes`、`--force`、`--reason`、`--subject`、`--actor` 和重复 plan-id。
- Expected：`UPMCTL_USAGE`、exit 2；不会打开绕过路径，不创建 Approval。
- Evidence：Error 和目录差异。
- Recovery：无。

### TC-APR-007 重复 grant 拒绝

- Story/Class/Priority/Level：OPS-08 / `CURRENT_REFUSAL_CONTRACT` / P0 / Human-TTY E2E
- Preconditions：Plan 已有 Approval。
- Steps：再次 grant。
- Expected：稳定 already-exists/invalid-state 错误、exit 3；不覆盖、续期或重新批准；原 Approval hash/mtime 不变。
- Evidence：Error、前后 stat/hash。
- Recovery：无。

### TC-APR-008 Approval get/list

- Story/Class/Priority/Level：OPS-08 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Steps：按 ID get；无过滤和按 plan-id list。
- Expected：只读、exit 0；状态为 APPROVED；list 按 planId 稳定排序；executionAvailable false；不调用现场观察、不写文件。
- Evidence：JSON、目录 hash、调用审计。
- Recovery：无。

### TC-APR-009 有 Approval 的 Preflight 仍阻塞

- Story/Class/Priority/Level：OPS-07/08 / `CURRENT_IMPLEMENTED` / P0 / E2E
- Steps：再次运行 preflight。
- Expected：approvalStatus APPROVED，但 applyDecision BLOCKED、executionAvailable false、exit 3；不创建 Claim/Operation/lock。
- Evidence：Preflight JSON 和目录差异。
- Recovery：无。

### TC-APR-010 人工 revoke 和重复撤销拒绝

- Story/Class/Priority/Level：OPS-08 / `CURRENT_IMPLEMENTED` / P0 / Human-TTY E2E
- Steps：在本机控制 TTY 输入原因/challenge 撤销；随后再次撤销。
- Expected：首次唯一创建不可变 Revocation，不修改 Approval；get/list 状态 REVOKED；第二次拒绝且不覆盖 Admission；Preflight approvalStatus REVOKED、仍固定阻塞。
- Evidence：脱敏 TTY 记录、Approval/Admission 前后摘要、Preflight。
- Recovery：保留审计工件。

### TC-APR-011 Approval 到期状态

- Story/Class/Priority/Level：OPS-08 / `CURRENT_IMPLEMENTED` / P1 / E2E/Integration
- Preconditions：未撤销 Approval；可等待到 expiresAt 或使用可控时钟。
- Steps：到期前后分别 get/list/preflight。
- Expected：状态从 APPROVED 变为 EXPIRED；`now == expiresAt` 即过期；不自动续期；Preflight 固定阻塞。
- Evidence：时间戳和 JSON。
- Recovery：无。

### TC-APR-012 Approval/Admission 篡改拒绝

- Story/Class/Priority/Level：OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 / Fault Injection
- Preconditions：隔离控制状态副本。
- Steps：构造 symlink、错误权限、重复 key、未知字段、尾随 JSON、超限、摘要或绑定篡改。
- Expected：get/list/preflight 报 INVALID 或稳定安全错误；不隐藏为 MISSING，不返回部分数据，不修改工件。
- Evidence：每个变体的 Error/状态。
- Recovery：销毁副本。

## 9. G — 日志、安全、故障恢复和支持包

| ID | Story/Class/Priority | Preconditions and Steps | Expected / Evidence / Recovery |
| --- | --- | --- | --- |
| TC-OPS-001 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 不指定 `--log-file` 运行命令 | 默认不创建日志；业务输出不变 |
| TC-OPS-002 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 在 0700 真实目录指定不存在的日志文件 | CLI 创建恰为 0600 普通文件；每次调用最多 start+终态两行 |
| TC-OPS-003 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 分别运行成功、Error 和 exit 3 的 Preflight | 成功 complete/0；Error error/真实码+errorCode；Preflight complete/3 且 errorCode=null |
| TC-OPS-004 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 扫描 JSONL 字段和值 | 仅七个允许字段；command 为规范化路径，不含参数、workspace、ID、reason、challenge、外部输出或凭据 |
| TC-OPS-005 | OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 | 日志父目录不存在、父路径非目录 | `UPMCTL_LOG_OPEN_FAILED`、exit 70；不静默降级，不执行业务命令 |
| TC-OPS-006 | OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 | 现有日志为 0644、symlink、目录、FIFO 或设备夹具 | 全部安全拒绝、exit 70；不自动 chmod、不跟随 symlink；哨兵不变 |
| TC-OPS-007 | OPS-10 / `CURRENT_IMPLEMENTED` / P1 | 对只读观察使用极短 timeout | 取消/超时返回 exit 6；无临时控制状态；正常 timeout 恢复成功 |
| TC-OPS-008 | OPS-10 / `CURRENT_IMPLEMENTED` / P1 | 在可恢复 PATH/权限夹具中使 vagrant、virsh、kubectl 分别不可用 | 返回外部依赖/观察错误 exit 4 或结构化 unknown/blocker；不得误报健康；恢复后复测 |
| TC-OPS-009 | OPS-10 / `CURRENT_REFUSAL_CONTRACT` / P0 | 检查 `.upmctl`、plans、approvals、admissions 目录和文件权限 | 新目录 0700、文件 0600、普通文件、当前用户所有；不安全身份被拒绝 |
| TC-OPS-010 | OPS-09 / `CURRENT_IMPLEMENTED` / P1 | 按故障手册收集默认低敏支持包 | 包含版本、能力、context/config/status/vm、requestId、依赖和摘要；不含 kubeconfig/私钥/env/TTY 输入 |
| TC-OPS-011 | OPS-10 / `CURRENT_IMPLEMENTED` / P0 | 对测试前后 `.vagrant`、domain UUID/磁盘、Node UID、工作区源文件做摘要/状态 diff | 除明确允许的 state/Plan/Approval/Revocation 外无差异；任何未知差异为 FAIL |
| TC-OPS-012 | OPS-10 / `CURRENT_IMPLEMENTED` / P0 | 在测试中断后重新运行 context/status/get/list | 不完整操作不被报告成功；现有不可变工件可审计；当前无 Operation resume 能力 |
| TC-OPS-013 | OPS-10 / `CURRENT_IMPLEMENTED` / P0 | 将为 ACTION_REQUIRED 准备的 Worker 按独立变更恢复并重新观察 | VM running、Node Ready、UUID/UID 不变；恢复动作明确不是 upmctl Apply 结果 |
| TC-OPS-014 | OPS-09 / `CURRENT_IMPLEMENTED` / P0 | 运行 `validate-test-environment.sh` 的只读模式和可用时 include-plan 模式 | 返回 0 PASS、3 BLOCKED、1 FAIL、2 脚本错误契约正确；报告目录全新、权限安全、evidence hash 可验证 |

## 10. H — V1 Future 未实现能力的拒绝测试

以下用例的拒绝契约可以 PASS，但对应业务功能结果必须记录为 `NOT_APPLICABLE_CURRENT_PHASE`。

| ID | Story | Command/Interface | Expected refusal and zero-side-effect proof |
| --- | --- | --- | --- |
| TC-FUT-001 | OPS-12 | `upmctl apply --plan-id ID` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；无 Claim/lock/Operation，VM 不变 |
| TC-FUT-002 | OPS-12 | `vm ssh`、任何实际 vm start/stop/restart 命令 | `UPMCTL_NOT_IMPLEMENTED`、exit 3；无 SSH、Vagrant/virsh 变更调用 |
| TC-FUT-003 | OPS-12 | `plan vm stop/restart` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；无 Plan 文件 |
| TC-FUT-004 | OPS-14 | `plan cluster deploy/start/stop/restart/destroy` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；集群和 VM 不变 |
| TC-FUT-005 | OPS-13 | `node list/status` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；不得冒充 kubectl Node 管理 |
| TC-FUT-006 | OPS-13 | `plan node add/remove` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；无 Vagrant/Kubespray/kubectl 调用，无节点变化 |
| TC-FUT-007 | OPS-14 | `plan addon install` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；无 Helm/kubectl/legacy Shell 调用 |
| TC-FUT-008 | OPS-14 | `operation get/cancel/resume` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；不创建 operations 目录或 journal |
| TC-FUT-009 | OPS-14 | `verify`、`report generate` | `UPMCTL_NOT_IMPLEMENTED`、exit 3；不伪造验证或报告成功 |
| TC-FUT-010 | OPS-14 | MCP Server/接口探测 | capabilities 中 `mcp.server=false`；没有监听端口、daemon 或审批绕过接口 |

每条用例都必须保存：命令、requestId、stdout/stderr、退出码、runtime JSONL、执行前后 `.upmctl` 树、Vagrant/libvirt/Kubernetes 快照。若任一未实现命令调用 legacy 脚本或底层变更工具，直接判定 P0 FAIL。

## 11. 管理员主流程验收场景

除逐条用例外，还必须串行执行一次完整日常故事：

1. 发布管理员验证制品并安装。
2. 环境管理员在 legacy 状态执行被动 discover/validate。
3. 人类在本机 TTY 完成 adopt。
4. 值班管理员运行 context/config/status/vm list，并逐台 inspect。
5. 对运行中 Worker 生成 NOOP Plan，证明不写状态。
6. 在独立授权准备 poweroff Worker 后生成 R1 ACTION_REQUIRED Plan。
7. 变更发起人执行 get/validate/preflight，确认 MISSING 且 Apply 固定 BLOCKED。
8. 人类审批人 grant；其他操作者 get/list；再次 Preflight 确认 APPROVED 仍不可执行。
9. 人类审批人 revoke；确认 REVOKED 且不可覆盖。
10. 验证 apply、VM 变更、节点、集群、Addon 和 Operation 全部安全拒绝。
11. 使用独立恢复步骤恢复 Worker，重新确认全部 VM running、Node Ready。
12. 收集支持包、验证 evidence SHA-256、卸载和重装 CLI，确认环境和审计状态保留。

该主流程只有在 1—12 全部完成、无未知差异且所有人工步骤真实发生于本机控制 TTY 时，才可标记为 Phase 2b2a 管理员日常运维链路 PASS。它仍不代表实际 Apply、VM 生命周期或节点扩缩可用。

## 12. 缺陷严重度和复测

| Severity | 判定示例 | 发布影响 |
| --- | --- | --- |
| S0 | 未授权 VM/Kubernetes 变更、凭据泄露、Agent 可绕过 TTY、路径逃逸写入 | 立即停止测试和发布 |
| S1 | 错误工件被接受、篡改未检出、拒绝命令调用底层变更、现场身份关联错误 | 阻断发布 |
| S2 | 稳定错误码/退出码/日志不符、升级回滚或支持包不可用 | 默认阻断，需负责人裁决 |
| S3 | 文案、帮助、非关键可用性问题 | 可带已知问题交付，但必须有修复计划 |

修复后必须重跑失败用例、同组回归用例和完整管理员主流程。不得只把原报告中的 FAIL 手工改成 PASS；每次复测必须使用新的 run-id、制品摘要和证据目录。
