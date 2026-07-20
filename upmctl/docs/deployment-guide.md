# upmctl 部署手册

本文面向负责安装、升级和交付 `upmctl` 的平台工程师。本文只覆盖当前 Phase 2b2a 已实现能力；命令真值以 `capabilities` 的运行结果为准，源码规范位于 `upmctl/specs/v1/cli-contract.md`。

## 1. 交付范围

当前版本是本地、无守护进程的单文件 CLI。它能够：

- 发现 Kubespray UPM 工作区并验证受支持的 `config.rb` 子集；
- 在受管工作区中只读观察 Vagrant、libvirt 和 Kubernetes VM 状态；
- 为 `vm start` 生成不可执行 Plan，读取和校验 Plan，并运行只读 Preflight；
- 由本机控制 TTY 上的人类创建或撤销 Approval，及只读查询 Approval。

当前版本**不能**执行 Plan，不能启动、停止或重启 VM，不能部署集群，不能增减 Kubernetes 节点，不能安装 Addon，也不提供 Operation、Executor、Plan Claim 或 MCP Server。即使 Preflight 通过且 Approval 为 `APPROVED`，结果仍固定为：

```text
applyDecision=BLOCKED
executionAvailable=false
```

不要使用 legacy Shell、直接调用 Vagrant/Kubespray，或手工修改 `.upmctl` 文件来补齐上述缺口。完整边界见源码中的 `upmctl/specs/v1/cli-contract.md` 和 `upmctl/specs/v1/state-and-safety.md`。

## 2. 支持平台和依赖

### 2.1 正式支持矩阵

| 项目 | 当前交付支持 |
| --- | --- |
| 真实E2E候选平台 | Rocky Linux 9 / RHEL 9，x86_64 / amd64；须由绑定archive SHA-256的PASS报告授予认证 |
| 可构建实验制品 | Darwin arm64、Linux arm64；未经过本次真实环境E2E |
| 虚拟化 | libvirt |
| VM 编排 | Vagrant |
| Kubernetes 编排 | Kubespray |
| 容器运行时 | containerd |
| Go 源码构建 | Go 1.24 或更高版本 |

Darwin/Linux都有控制TTY实现，开发者可以在macOS构建和运行基础命令；这不等同于三个制品都完成生产认证。`linux/amd64`的manifest标记为`rocky9-e2e-candidate`，只有绑定该不可变archive SHA-256的测试环境报告自动判定PASS并完成人工签署后，才能授予本次认证。`darwin/arm64`和`linux/arm64`标记为`experimental-build-only`。

### 2.2 运行时依赖

`upmctl` 本身不需要常驻服务。二进制启动不依赖 Go，但不同命令需要以下程序位于执行用户的 `PATH`：

| 依赖 | 用途 | 何时必须 |
| --- | --- | --- |
| `vagrant` | VM 状态、`ssh-config` endpoint 元数据、固定 `vagrant ssh NODE -c true` 可达性探针 | 受管 VM 观察、Plan、Preflight |
| `vagrant-libvirt` 插件 | Vagrant 的 libvirt provider | libvirt 测试环境 |
| OpenSSH 客户端 `ssh` | 由 `vagrant ssh` 使用；只执行固定 `true` 探针 | running VM 的完整健康判定 |
| `virsh` | VM UUID、状态、资源和磁盘观察 | 受管 VM 观察、Plan、Preflight |
| `kubectl` | 使用工作区绑定的 kubeconfig 查询 Node | 受管 VM 观察、Plan、Preflight |
| `sha256sum` | 验证 Linux Release 文件 | 从 tarball 安装 |
| `tar` | 解包 Release | 从 tarball 安装 |
| `git`、Go 1.24+、`make`、Bash | 拉取、构建和运行门禁 | 仅源码安装 |

`upmctl` 使用执行用户当前的 libvirt URI、Vagrant home、`PATH` 和网络环境。应由拥有该测试工作区及其 `.vagrant` 元数据、并能读取绑定 kubeconfig 的同一非 root 用户运行。不要给二进制设置 SUID、文件 capabilities，也不要日常使用 `sudo upmctl`；这样会改变 Vagrant/libvirt 上下文并产生 root 所有的控制状态。

安装前检查：

```bash
uname -s
uname -m
command -v vagrant
command -v ssh
command -v virsh
command -v kubectl
vagrant --version
vagrant plugin list
ssh -V
virsh uri
kubectl version --client
```

`vagrant plugin list`应包含`vagrant-libvirt`。Vagrant、插件、OpenSSH、libvirt和kubectl版本应记录在测试环境验证报告或组织批准的兼容矩阵中；`release-manifest.json`只声明本CLI制品自身，不伪造外部依赖版本。

## 3. Release tarball 安装

### 3.1 Release 接收门禁

可交付 Release 至少应提供：

```text
upmctl_<version>_linux_amd64.tar.gz
SHA256SUMS
```

tarball 解包后必须在唯一顶层目录中包含`upmctl`、`install.sh`、`release-manifest.json`、内部`SHA256SUMS`、三本手册、测试验证脚本与报告模板，以及完整的`skills/upmctl-environment/`目录。`release-manifest.json`必须声明版本、commit、构建时间、平台、认证等级、archive名、顶层目录、内部校验文件和支持文件清单。

先把 tarball 和 `SHA256SUMS` 放在一个空目录中，并验证下载结果：

```bash
VERSION=0.1.0
ARCHIVE="upmctl_${VERSION}_linux_amd64.tar.gz"

grep "  ${ARCHIVE}$" SHA256SUMS | sha256sum -c -
tar -tzf "${ARCHIVE}"
```

以下任一情况必须停止安装：

- 校验和不通过，或 `SHA256SUMS` 中没有该 tarball；
- tarball 包含绝对路径、`..` 路径逃逸、设备文件或意外的可执行文件；
- 缺少 Release manifest，或 manifest 的平台不是 `linux/amd64`；
- Release 的来源、签名或审批无法按组织供应链策略确认。

在暂存目录解包，然后安装系统级二进制：

```bash
STAGE="$(mktemp -d)"
tar -xzf "${ARCHIVE}" -C "${STAGE}"
ROOT="${STAGE}/upmctl_${VERSION}_linux_amd64"
test -f "${ROOT}/release-manifest.json"
grep -F '"validationTier": "rocky9-e2e-candidate"' "${ROOT}/release-manifest.json"
(cd "${ROOT}" && sha256sum -c SHA256SUMS)
sudo "${ROOT}/install.sh" --prefix /usr/local
```

还必须取得引用同一`${ARCHIVE}` SHA-256的测试环境验收报告；候选manifest自身不能证明E2E
通过。不要使用`find`结果猜测二进制路径，也不要绕过包内安装器。完成后使用普通运行用户验证：

```bash
command -v upmctl
ls -l /usr/local/bin/upmctl
LOG_DIR="${HOME}/.local/state/upmctl"
LOG_FILE="${LOG_DIR}/runtime.jsonl"
install -d -m 0700 "${LOG_DIR}"

upmctl version --output json --request-id deploy-install-version-001 \
  --log-file "${LOG_FILE}"
upmctl capabilities --output json
stat -c '%a %U %G %n' "${LOG_DIR}" "${LOG_FILE}"
tail -n 2 "${LOG_FILE}"
```

正式Release的`version`、`gitCommit`和`buildDate`应与manifest一致，且`gitCommit`、`buildDate`不应为`unknown`。待验收目标的`platform`必须为`linux/amd64`且`validationTier=rocky9-e2e-candidate`，`apiVersion`当前应为`upmctl.upm.io/v1alpha1`。最终认证来自绑定archive SHA-256的PASS报告与人工签署，不来自manifest自证。不要仅以命令退出码0判断安装正确。

日志目录必须是运行用户所有的真实目录，建议权限 `0700`。日志文件不存在时由 CLI 以 `0600` 创建；上述冒烟应产生同一 `requestId` 的 `start` 和 `complete` 两个 JSONL 事件，且不会污染 stdout 的 Version JSON。默认不指定 `--log-file` 时，CLI 不创建任何日志。

### 3.2 无 root 的用户级安装

不能写 `/usr/local/bin` 时，可安装到用户目录：

```bash
"${ROOT}/install.sh" --prefix "${HOME}/.local"
export PATH="${HOME}/.local/bin:${PATH}"
upmctl version --output json
```

应通过 shell profile 或受管环境配置持久设置 `PATH`。不要把二进制复制到部署工作区或 `.upmctl` 中。

## 4. 从源码构建和安装

源码构建适用于开发验证或组织内部的可复现构建。正式生产交付应从已审核的 tag/commit 构建，并记录工具链版本和构建日志。

```bash
git clone <approved-kubespray-upm-repository-url>
cd kubespray-upm
git checkout <approved-tag-or-commit>
git status --short

cd upmctl
go version
go env GOOS GOARCH
make check
make build-linux-amd64
```

`git status --short` 必须为空。`make check` 当前包括格式、vet、单元测试、race、契约测试、legacy Shell 回归和本机构建。Linux amd64 交叉构建输出默认为：

```text
upmctl/bin/upmctl-linux-amd64
```

Makefile 的开发构建默认保留 `0.1.0-dev/unknown` 构建信息。制作正式内部制品时应显式注入经过审核的版本、commit 和 UTC 构建时间：

```bash
VERSION=<approved-version>
GIT_COMMIT="$(git rev-parse HEAD)"
BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath \
  -ldflags "-s -w \
  -X github.com/upmio/kubespray-upm/upmctl/internal/buildinfo.Version=${VERSION} \
  -X github.com/upmio/kubespray-upm/upmctl/internal/buildinfo.GitCommit=${GIT_COMMIT} \
  -X github.com/upmio/kubespray-upm/upmctl/internal/buildinfo.BuildDate=${BUILD_DATE}" \
  -o bin/upmctl-linux-amd64 ./cmd/upmctl

sha256sum bin/upmctl-linux-amd64
```

随后在目标 Linux amd64 测试主机上执行安装和版本验证，不应把在 macOS 上交叉构建成功当作目标主机运行验证：

```bash
sudo install -o root -g root -m 0755 bin/upmctl-linux-amd64 /usr/local/bin/upmctl
upmctl version --output json
upmctl capabilities --output json
```

## 5. 测试环境部署

### 5.1 选择运行身份和工作区

设置测试工作区的绝对路径：

```bash
WORKSPACE=/absolute/path/to/vagrant_setup_scripts/kubespray-upm
test -d "${WORKSPACE}"
test -f "${WORKSPACE}/Vagrantfile"
test -f "${WORKSPACE}/vagrant/config.rb"
```

不要依赖当前目录猜测环境；所有验收命令都显式传入 `--workspace "${WORKSPACE}"`。不要在共享、生产或身份不明的工作区进行首次验证。

若该测试工作区是legacy环境，先由人类工程师在本机控制TTY执行安全接管：

```bash
upmctl environment adopt \
  --environment-id env-test-lab \
  --workspace "${WORKSPACE}"
```

CLI会在TTY展示canonical workspace、文件摘要和全部libvirt UUID，并要求非空reason和随机typed challenge。它拒绝Skill/MCP/pipe/CI、其他或混合provider、未知/缺失节点、非法/重复UUID、symlink、不安全config和已有控制状态。Adopt不执行Vagrant、virsh或kubectl，也不修改VM/Kubernetes；唯一写入是：

```text
<workspace>/.upmctl/state.json
```

该文件以`0700/0600`权限绑定工作区绝对身份、`Vagrantfile`、`vagrant/config.rb`、存在的受支持kubeconfig摘要、VM UUID及actor/reason/typed-challenge审计证据。**不要手工创建、复制或编辑该文件。** 若Adopt拒绝，先按稳定错误码修复legacy身份问题，再由人类重新运行。

### 5.2 二进制和上下文冒烟

```bash
LOG_DIR="${HOME}/.local/state/upmctl"
LOG_FILE="${LOG_DIR}/runtime.jsonl"
install -d -m 0700 "${LOG_DIR}"

upmctl version --output json --request-id deploy-version-001 --log-file "${LOG_FILE}"
upmctl capabilities --output json --request-id deploy-capabilities-001
upmctl context discover --workspace "${WORKSPACE}" --output json --request-id deploy-context-001
upmctl config validate --workspace "${WORKSPACE}" --output json --request-id deploy-config-001

test "$(stat -c '%a' "${LOG_DIR}")" = 700
test "$(stat -c '%a' "${LOG_FILE}")" = 600
tail -n 2 "${LOG_FILE}"
```

交付验收至少确认：

- 版本、commit、构建时间、平台和 API 版本符合 Release manifest；
- `capabilities.data.phase` 与 Release 说明一致；
- 上下文返回的 `workspace` 是预期绝对路径，没有指向其他 checkout；
- 完整测试要求 `trust=MANAGED_VALID`、`managed=true` 且 `environmentId` 非空；
- 配置验证为安全、完整、有效。Legacy 只读结果不是完整验收通过。
- 可选日志文件只有允许的生命周期字段，包含 `deploy-version-001` 的 `start/complete`，不包含工作区、参数值或环境数据。

### 5.3 真实环境只读验证

以下命令会在 `MANAGED_VALID` 工作区执行真实的只读 `vagrant`、`virsh` 和 `kubectl` 查询，但不会改变 VM 或 Kubernetes。对于 running VM，观察会先读取 `vagrant ssh-config NODE`，再执行固定 `vagrant ssh NODE -c true`；只有探针成功才设置 `sshState=reachable`，并允许判定 `RUNNING_HEALTHY`。该探针不接受用户命令、不读取 Guest 文件、不使用 `sudo`：

```bash
upmctl status --workspace "${WORKSPACE}" --output json --request-id deploy-status-001
upmctl vm list --workspace "${WORKSPACE}" --output json --request-id deploy-vm-list-001
upmctl vm status --workspace "${WORKSPACE}" --output json --request-id deploy-vm-status-001
upmctl vm inspect k8s-1 --workspace "${WORKSPACE}" --output json --request-id deploy-vm-inspect-001
```

检查 `findings`、不完整数据源、Vagrant/libvirt 状态差异、VM UUID、磁盘、SSH `endpoint-configured/reachable/unavailable` 状态和 Kubernetes Node 对应关系。仅有 endpoint 不能证明可达；固定 SSH 探针失败的 running VM 应为 `RUNNING_DEGRADED`。`status` 成功不等于 CNI、Addon 或业务端点已完成验收。

### 5.4 Plan、Preflight 和 Approval 验证

选择测试环境中一个确实处于 poweroff、且允许生成测试 Plan 的 Worker，例如 `k8s-3`。该步骤只生成不可执行控制面状态，不会启动 VM：

```bash
upmctl plan vm start --node k8s-3 --workspace "${WORKSPACE}" \
  --output json --request-id deploy-plan-001
```

只有结果为 `ACTION_REQUIRED` 时才会在 `.upmctl/plans/` 创建 Plan。保存返回的 `planId`，随后验证：

```bash
PLAN_ID=<plan-id-from-output>
upmctl plan get "${PLAN_ID}" --workspace "${WORKSPACE}" --output json --request-id deploy-plan-get-001
upmctl plan validate "${PLAN_ID}" --workspace "${WORKSPACE}" --output json --request-id deploy-plan-validate-001
```

Preflight 当前即使检查通过也按设计退出 3，因为 Apply 被阻断。测试脚本必须显式核对 JSON，而不能把退出码 3 误判为安装失败：

```bash
set +e
upmctl preflight --plan-id "${PLAN_ID}" --workspace "${WORKSPACE}" \
  --output json --request-id deploy-preflight-001
PREFLIGHT_RC=$?
set -e
test "${PREFLIGHT_RC}" -eq 3
```

Approval 写操作必须由人类直接在本机交互终端执行，不能放入部署脚本、CI、管道、`expect`、Agent 或 MCP：

```bash
upmctl approval grant --plan-id "${PLAN_ID}" --workspace "${WORKSPACE}" \
  --output json --request-id deploy-approval-grant-001
```

操作员应阅读 Plan 摘要，在控制 TTY 输入非空 reason，并精确输入随机 challenge。保存返回的 `approvalId`，再运行只读查询：

```bash
APPROVAL_ID=<approval-id-from-output>
upmctl approval get "${APPROVAL_ID}" --workspace "${WORKSPACE}" --output json
upmctl approval list --plan-id "${PLAN_ID}" --workspace "${WORKSPACE}" --output json
```

Approval 默认最多有效 10 分钟且不超过 Plan 到期时间；同一 Plan 不能重复审批或续期。若验收需要撤销测试，同样必须由人类在控制 TTY 运行：

```bash
upmctl approval revoke "${APPROVAL_ID}" --workspace "${WORKSPACE}" --output json
```

Approval/Revocation 是本机人工意图的审计证据，不是强身份认证，也不是执行许可。验收结束时不得运行 legacy 变更脚本或直接执行 `vagrant up` 来“完成”该 Plan。

### 5.5 部署验收记录

交付记录应保存以下信息，并按组织策略脱敏和限制访问：

- tarball 和二进制 SHA-256；
- `version` 与 `capabilities` JSON；
- 测试主机 OS/架构及 Vagrant、libvirt、kubectl 版本；
- 工作区绝对路径、`environmentId` 和信任状态；
- 每条命令的 `requestId`、时间、退出码和 stdout/stderr；
- 使用 `--log-file` 时对应的本地生命周期 JSONL；
- Plan/Approval 标识、风险、状态和到期时间；
- 明确声明目标 VM/Kubernetes **未发生变更**。

不要收集 kubeconfig 内容、私钥、Vagrant SSH 私钥、令牌或未脱敏的凭据。

## 6. Codex Skill 安装

Skill 是 CLI 的安全工作流封装，不包含 CLI 二进制，也不扩展 CLI 能力。先完成二进制安装，再从同一 Release 或同一审核 commit 安装 Skill。

仓库中的 Skill 源目录为：

```text
skills/upmctl-environment/
```

安装到个人 Codex 目录：

```bash
CODEX_HOME="${CODEX_HOME:-${HOME}/.codex}"
SKILL_STAGE="${CODEX_HOME}/skills/upmctl-environment.new"
SKILL_TARGET="${CODEX_HOME}/skills/upmctl-environment"

install -d -m 0755 "${CODEX_HOME}/skills"
cp -R skills/upmctl-environment "${SKILL_STAGE}"
test -f "${SKILL_STAGE}/SKILL.md"
test -f "${SKILL_STAGE}/references/phase-2b2a-contract.md"
```

首次安装时将暂存目录改名为目标目录：

```bash
mv "${SKILL_STAGE}" "${SKILL_TARGET}"
```

升级 Skill 时，不要在原目录内混合覆盖。先退出正在使用该 Skill 的 Codex 任务，将原目录改名留作回滚，再把暂存目录改名到目标路径。重新启动或刷新 Codex 后，确认 `upmctl-environment` 可被发现。

Skill 必须与 CLI Phase 一致。当前 Skill 不得代替人类调用 `environment adopt`或`approval grant/revoke`，不得模拟 TTY，也不得回退直接调用 Vagrant、virsh、kubectl、Helm、Ansible 或 legacy Shell 执行变更。详细约束位于源码中的 `upmctl/skills/upmctl-environment/SKILL.md`。

## 7. 安装和状态目录

### 7.1 主机目录

| 路径 | 建议属主/权限 | 内容 |
| --- | --- | --- |
| `/usr/local/bin/upmctl` | `root:root`, `0755` | 系统级 CLI 二进制 |
| `~/.local/bin/upmctl` | 当前用户, `0755` | 可选用户级 CLI 二进制 |
| `~/.local/state/upmctl/` | 当前用户, `0700` | 建议的可选运行日志目录 |
| `~/.local/state/upmctl/runtime.jsonl` | 当前用户, `0600` | 显式启用的本地生命周期日志 |
| `$CODEX_HOME/skills/upmctl-environment/` | 当前用户, 目录通常 `0755` | 可选 Codex Skill |

当前版本不安装 systemd unit，不监听端口，不创建全局配置目录，也不写 `/var/log`。

运行日志是显式 opt-in 能力：不传 `--log-file` 时不会创建日志。CLI 不自动创建日志父目录，也不会自动修正已有文件权限。父目录必须预先存在且是实际目录；已有日志目标必须是权限**恰好**为 `0600` 的普通文件。CLI 拒绝 symlink、目录、设备、FIFO、其他非普通文件以及非 `0600` 的已有文件。

显式请求日志但无法安全打开时，命令返回 `UPMCTL_LOG_OPEN_FAILED` 和退出码 70，不会静默降级成无日志运行。已经打开但写入失败时返回 `UPMCTL_LOG_WRITE_FAILED`。应检查目录身份、属主、权限、磁盘空间、配额和只读文件系统，不要通过放宽到 `0644/0666` 或替换成 symlink 规避。

日志只记录 `logVersion/timestamp/requestId/command/event/exitCode/errorCode`。它不记录参数值、workspace、Plan/Approval ID、TTY 输入、审批 reason、challenge、完整控制工件或外部命令输出，因此只能用于用 `requestId` 关联故障，不能替代未来的 Operation journal 或安全审计系统。

### 7.2 工作区控制状态

```text
<workspace>/.upmctl/
├── state.json
├── plans/<plan-id>.json
├── approvals/by-plan/<plan-id>.json
└── admissions/<plan-id>.json
```

- `.upmctl` 及新增控制目录由实现按 `0700` 创建；新增工件按 `0600` 写入。
- `state.json` 是受信任环境身份，不是普通用户配置。
- Plan、Approval 和 Revocation 是不可变、原子、禁止覆盖的审计工件。
- symlink、路径逃逸、非普通文件、不安全权限/身份、重复 JSON key、未知字段和摘要不匹配会被拒绝。
- `operations/`、lock、reports 和 Plan Claim 当前不会被创建。

不要更改这些权限，不要把 `.upmctl` 放到共享写目录，不要通过 NFS/同步盘复制活动控制状态，不要手工删除或修复工件。备份时应保持目录权限并把内容视为敏感审计数据。

## 8. 升级

升级前：

1. 阅读目标 Release 的兼容性和 schema 变更说明。
2. 记录当前 `upmctl version --output json` 和 `upmctl capabilities --output json`。
3. 确认目标二进制的 SHA-256、平台、commit 和构建时间。
4. 完成当前人工审批交互；不要在交互中途替换二进制。
5. 不修改或迁移 `.upmctl`，除非 Release 提供单独且已审核的迁移步骤。

保留旧二进制并原子替换：

```bash
OLD_VERSION="$(upmctl version --output json | sed -n 's/.*"version": "\([^"]*\)".*/\1/p')"
sudo install -d -o root -g root -m 0755 /usr/local/lib/upmctl
sudo install -o root -g root -m 0755 /usr/local/bin/upmctl \
  "/usr/local/lib/upmctl/upmctl-${OLD_VERSION}"
sudo install -o root -g root -m 0755 <new-upmctl-binary> /usr/local/bin/upmctl.new
sudo mv /usr/local/bin/upmctl.new /usr/local/bin/upmctl
```

升级后重新运行版本、能力、显式工作区发现和只读状态验证。确认 `apiVersion`、能力变化及旧 Plan/Approval 的读取行为符合 Release 说明。不要假设新旧二进制可以双向读取所有控制工件。

## 9. 回滚

只有在 Release 说明确认 schema/API 向后兼容时才回滚二进制。回滚不会、也不应回滚 `.upmctl` 审计状态：

```bash
sudo install -o root -g root -m 0755 \
  "/usr/local/lib/upmctl/upmctl-<old-version>" /usr/local/bin/upmctl.new
sudo mv /usr/local/bin/upmctl.new /usr/local/bin/upmctl
upmctl version --output json
upmctl capabilities --output json
```

若旧版本不能安全解析新版本创建的 Plan、Approval 或 Admission，停止操作并恢复新二进制；不要编辑 JSON 以强行兼容。

Skill 回滚应使用与旧 CLI 同一 Release 的完整 Skill 目录。不要只回滚 `SKILL.md` 而保留新版 references。

## 10. 卸载

卸载二进制前记录版本和安装路径。系统级卸载需要组织批准：

```bash
command -v upmctl
upmctl version --output json
sudo rm /usr/local/bin/upmctl
```

用户级安装则删除 `~/.local/bin/upmctl`。Codex Skill 可单独移除 `$CODEX_HOME/skills/upmctl-environment/`。

默认**不要删除**任何工作区的 `.upmctl`、`.vagrant`、inventory、kubeconfig、VM 或 libvirt 资源。卸载 CLI 不等于销毁环境；`.upmctl` 包含环境身份和不可变审计记录。若确需清除控制状态，应走独立的数据保留、审批和审计流程，确认没有其他操作者或版本仍在使用后再执行。

## 11. 安全边界和交付判定

部署验收通过必须同时满足：

- Release 来源、校验和、平台、版本、commit 和构建时间均已验证；
- 二进制由预期非 root 运行用户调用，外部依赖与 libvirt/Vagrant 上下文正确；
- 显式工作区为 `MANAGED_VALID`，文件摘要和 VM 身份绑定有效；
- 当前开放的发现、配置、真实只读观察、Plan 读取/校验、Preflight 和人工 Approval 流程已按本手册验证；
- `capabilities` 明确显示执行、VM 变更、集群部署、节点伸缩、Addon 和 MCP 不可用；
- 验收证据可按 `requestId` 追踪，且明确证明测试未修改目标 VM/Kubernetes 状态。

如果测试环境只有 legacy 工作区、Release 构建信息仍为 `unknown`、真实依赖未连接、控制 TTY 未由人类验证，或只运行了 fixture/单元测试，则只能标记为“开发验证通过”，不能标记为“测试环境交付验收通过”。
