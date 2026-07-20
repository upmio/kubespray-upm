# upmctl

`upmctl` 是 Kubespray UPM 的 Go 管理 CLI。当前开发阶段为 Phase 2b2a人工Approval控制面：在Plan审计与只读Preflight基础上，新增本地人类TTY审批、撤销和只读查询；不会启动VM或修改目标运行配置，但会按显式命令写入列明的`.upmctl`控制状态和可选运行日志，也不会创建Plan Claim、Operation或环境锁。

规范入口：[specs/v1/README.md](specs/v1/README.md)。

## 当前可用命令

```bash
go run ./cmd/upmctl help
go run ./cmd/upmctl --help
go run ./cmd/upmctl help environment
go run ./cmd/upmctl help vm
go run ./cmd/upmctl help plan
go run ./cmd/upmctl help approval
go run ./cmd/upmctl version
go run ./cmd/upmctl capabilities --output json
go run ./cmd/upmctl environment adopt --environment-id env-lab-01 --workspace /path/to/legacy/deployment/workspace
go run ./cmd/upmctl context discover --output json
go run ./cmd/upmctl config validate --workspace /path/to/deployment/workspace
go run ./cmd/upmctl status --workspace /path/to/deployment/workspace
go run ./cmd/upmctl vm list --workspace /path/to/deployment/workspace
go run ./cmd/upmctl vm status k8s-1 --workspace /path/to/deployment/workspace
go run ./cmd/upmctl vm inspect k8s-1 --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl plan vm start --node k8s-3 --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl plan get plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl plan validate plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl preflight --plan-id plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl approval grant --plan-id plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --workspace /path/to/deployment/workspace
go run ./cmd/upmctl approval get approval-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl approval list --output json --workspace /path/to/deployment/workspace
go run ./cmd/upmctl approval revoke approval-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef --workspace /path/to/deployment/workspace
```

帮助命令是离线、稳定的纯文本入口，不发现工作区，不调用Vagrant、virsh或kubectl，也不创建`.upmctl`状态。帮助始终输出文本，即使指定`--output json`；默认不写日志，只有显式传入`--log-file`时才记录最小化的CLI生命周期事件。`help approval`会明确本地人类控制TTY边界，`help plan`会明确Phase 2b2a中Apply仍关闭。

VM外部观察只会对`.upmctl/state.json`中绑定了Vagrantfile、config.rb和kubeconfig摘要的Managed Environment执行。Legacy或损坏工作区可以运行被动`config validate/status`，但不会加载其中的Ruby、Vagrantfile或kubeconfig。

`environment adopt`用于安全接管已有的legacy libvirt Vagrant工作区。它必须由工程师直接在本地控制TTY执行：CLI会展示workspace、绑定文件摘要和所有机器UUID，并从TTY读取reason及随机挑战。该命令拒绝Skill/MCP/pipe/CI、parallels或混合provider、未知/缺失节点、非法/重复UUID、symlink、不安全config和任何已有`.upmctl`状态。成功时仅原子创建权限为`0700/0600`的`.upmctl/state.json`，记录OS actor、人机证据、request ID和CLI版本；不会运行Vagrant、virsh、kubectl或修改目标环境。OS/TTY记录是审计线索，不是现实身份的强认证。

`plan vm start`只生成审计结果：

- `NOOP`：VM已经满足start目标，不写Plan文件。
- `BLOCKED`：安全条件不满足，不写Plan文件。
- `ACTION_REQUIRED`：在 `<workspace>/.upmctl/plans/` 原子保存30分钟有效的不可执行Plan。

三种结果都包含 `planId` 和 `planDigest`。普通Worker start为R1，`k8s-1`/`k8s-2` start为R2。写入 `.upmctl/plans/` 只是 `upmctl` 控制面状态，不会执行Vagrant、virsh、kubectl或SSH变更命令。

`plan get`安全读取并校验持久化Plan；`plan validate`只校验Plan工件、TTL及Environment、Config和Managed State绑定，不重新观察现场。`preflight --plan-id`会重新运行既有只读观察并比较Plan中的三个basis摘要。三个命令都不写文件。

即使Preflight全部只读检查通过，也固定返回：

```text
preflightStatus=PASSED
applyDecision=BLOCKED
executionAvailable=false
approvalStatus=MISSING|APPROVED|REVOKED|EXPIRED|INVALID
```

`approval grant/revoke`只能由人类直接在本地控制TTY运行。grant只接受`--plan-id`，revoke只接受`APPROVAL_ID`；reason与typed challenge从控制TTY读取，UID、用户名、主机名和终端由OS观察，不接受subject/reason/actor参数。记录的OS和TTY证据用于审计，不代表独立强身份认证。所有R1、R2、R3变更都需要人工批准；每个Plan最多一个Approval，有效期为10分钟且不超过Plan过期时间。

Approval保存在`.upmctl/approvals/by-plan/<planId>.json`；撤销记录保存在`.upmctl/admissions/<planId>.json`。Admission槽也为未来Plan Claim预留，因此撤销与开始执行不能并发成功；当前阶段不开放或创建Claim。`approval get/list`只读，可供Skill使用；Skill和MCP禁止grant/revoke。

即使Approval状态为`APPROVED`，也不表示Plan可执行。Preflight仍固定`applyDecision=BLOCKED`和`executionAvailable=false`，不会创建Operation、journal、lock或Claim。

## 可选运行日志

默认情况下`upmctl`不创建日志。工程师需要将一次CLI调用与故障单或自动化任务关联时，可显式指定JSONL日志文件：

```bash
install -d -m 700 "$HOME/.local/state/upmctl"
touch "$HOME/.local/state/upmctl/runtime.jsonl"
chmod 600 "$HOME/.local/state/upmctl/runtime.jsonl"

upmctl status \
  --workspace /path/to/deployment/workspace \
  --output json \
  --request-id INC-20260717-001 \
  --log-file "$HOME/.local/state/upmctl/runtime.jsonl"
```

日志只记录`start/complete/error`生命周期、UTC时间、request ID、规范化命令名、退出码和稳定错误码。它不记录命令参数值、workspace、Plan/Approval ID、TTY输入、审批reason、challenge、Approval内容或外部命令输出。因此日志适合用`requestId`关联业务输出和工单，但不能替代未来的Operation journal。

`--log-file`不会污染stdout JSON。文件不存在时CLI以`0600`创建；父目录必须预先存在。为避免凭据泄露和路径劫持，CLI拒绝symlink、非普通文件，以及权限不是`0600`的已有文件。可用以下命令快速检查：

```bash
ls -ld "$HOME/.local/state/upmctl"
ls -l "$HOME/.local/state/upmctl/runtime.jsonl"
tail -n 20 "$HOME/.local/state/upmctl/runtime.jsonl"
```

出现`UPMCTL_LOG_OPEN_FAILED`时，依次确认父目录存在且不是symlink、日志目标是普通文件、权限为`0600`，以及运行用户有写权限。出现`UPMCTL_LOG_WRITE_FAILED`时，检查磁盘空间、文件系统只读状态和配额。显式请求日志后，日志不可安全写入会令命令失败，不会静默转为无日志运行。

`plan vm stop/restart`、cluster/node/addon Plan、Apply、Operation、Deploy、实际VM变更、节点增减、Addon和MCP Server仍未实现，调用时返回 `UPMCTL_NOT_IMPLEMENTED`，不会创建执行状态或回退到legacy脚本。

## 构建、发布与安装

本地构建会把版本、Git commit和可重复的UTC构建时间写入二进制。默认版本为
`0.1.0-dev`，默认构建时间取当前源码提交时间；正式发布时应显式设置版本：

```bash
make fmt
make check
make test-race
make build-linux-amd64
make release VERSION=0.1.0
```

`make release`生成：

```text
dist/upmctl_0.1.0_darwin_arm64.tar.gz
dist/upmctl_0.1.0_linux_amd64.tar.gz
dist/upmctl_0.1.0_linux_arm64.tar.gz
dist/SHA256SUMS
```

`linux_arm64`包用于aarch64/ARM64 Linux运维主机，例如当前测试环境中的
`localworknode`；`linux_amd64`用于x86-64 Linux，`darwin_arm64`用于Apple Silicon。
`linux/amd64`是等待绑定该archive SHA-256真实验收的Rocky/RHEL 9 E2E候选制品；在验收
报告自动判定PASS并由人类签署前，不得宣称已认证。`darwin/arm64`和`linux/arm64`是经过
构建、格式和包结构验证的实验制品，未进入本次真实环境E2E范围。
每个平台包同时携带`docs/upmctl/`中的部署、使用、故障排除、管理员故事测试计划、
专业测试用例、CLI覆盖矩阵和安全负向测试方案，以及完整的`skills/upmctl-environment/`
Codex Skill。包内还提供`validate-test-environment.sh`、`audit-cli-contract.sh`和
`host-safe-cli-coverage.sh`；测试环境报告模板位于
`docs/upmctl/test-environment-validation-report.md`。
安装器仍只安装`PREFIX/bin/upmctl`，不会把文档、Skill或验证脚本写入系统目录或Codex配置。
包内`release-manifest.json`绑定版本、commit、构建时间、目标平台、认证等级、archive布局、
内部`SHA256SUMS`和全部支持文件；外层`dist/SHA256SUMS`绑定三个tarball。

工作树存在未提交的`upmctl`或许可证变更时，commit字段会带`-dirty`，避免把开发构建
误认为来自干净提交。发布流水线可显式传入`GIT_COMMIT`和`SOURCE_DATE_EPOCH`；正式
发布应从已提交的干净工作树构建。

发布构建使用`-trimpath`、关闭VCS自动注入，并固定archive时间、属主、文件顺序和gzip
header。相同源码和相同`VERSION`、`GIT_COMMIT`、`SOURCE_DATE_EPOCH`输入会生成相同
内容的发布包。`make release`也会验证archive路径安全、SHA-256、二进制目标平台、版本元
数据、全部交付手册、测试脚本和Skill目录完整性，并在当前宿主平台执行安装冒烟测试及
离线CLI全命令拒绝契约审计。源码仓库可独立执行
`make test-skill`，使用本机Codex SDK中skill-creator的官方`quick_validate.py`验证Skill；
非默认Codex目录可通过`SKILL_QUICK_VALIDATE=/path/to/quick_validate.py`指定验证器。

安装前先校验下载包：

```bash
shasum -a 256 -c SHA256SUMS       # macOS
sha256sum -c SHA256SUMS            # Linux
tar -xzf upmctl_0.1.0_darwin_arm64.tar.gz
cd upmctl_0.1.0_darwin_arm64
./install.sh --prefix "$HOME/.local"
"$HOME/.local/bin/upmctl" version
```

安装器只写入`PREFIX/bin/upmctl`，要求prefix为绝对路径，拒绝symlink目标，并在复制前后
校验包内二进制。若目标已是同一文件则安全退出；若存在不同版本，默认拒绝覆盖。显式使用
`--replace`时，安装器会先在同目录保存带UTC时间戳的备份，再进行原子替换。系统目录安装
可由工程师显式使用`sudo ./install.sh --prefix /usr/local`，脚本本身不会调用或提升sudo。

Go模块不依赖真实VM执行单元测试。Vagrant、virsh和kubectl通过可替换Runner进行fixture测试。
