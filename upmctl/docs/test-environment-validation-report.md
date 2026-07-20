# upmctl 测试环境验收报告与执行说明

本文既是交付验收记录模板，也是 `upmctl/scripts/validate-test-environment.sh` 的执行说明。每次测试环境部署都应保留一份独立证据目录，不得复用或覆盖历史目录。

## 1. 验收目标与安全边界

验收脚本验证以下已交付能力：

- 二进制版本、Capabilities、requestId 和结构化运行日志契约；
- 工作区发现、配置校验和环境状态；
- Vagrant、libvirt、Kubernetes 的真实只读观察；
- VM列表及指定或自动选中节点的Inspection；
- 可选的不可执行 `vm.start` Plan、Plan读取、Plan校验和Preflight。

默认验收完全只读。显式传入 `--include-plan` 后，唯一允许的写入是由 `plan vm start` 在测试工作区 `.upmctl/plans/` 发布不可变、不可执行Plan。脚本绝不调用：

- `approval grant` 或 `approval revoke`；
- `apply`、Operation、Executor或环境锁；
- `vagrant up/halt/destroy`、`virsh start/shutdown/destroy`、`kubectl apply/delete/drain`；
- SSH或legacy Shell变更入口。

脚本不读取或复制kubeconfig内容、私钥、Shell环境变量或Approval TTY输入。报告仍可能包含主机名、用户名、工作区路径、VM名称、内部IP和依赖路径，应按内部运维证据管理，默认目录权限为`0700`、文件权限受`umask 077`保护。

## 2. 执行前提

使用与Vagrant工作区、libvirt会话和kubeconfig属于同一上下文的普通运维用户执行，不建议使用`sudo upmctl`。测试主机必须已安装：

```text
upmctl
vagrant + vagrant-libvirt
virsh
kubectl
sha256sum 或 shasum
```

工作区和报告目录必须使用绝对路径。报告目录必须尚不存在，其父目录必须是真实目录而非symlink，以防证据被混入旧结果或写入意外位置。

## 3. 标准执行

只读验收：

```bash
WORKSPACE=/absolute/path/to/managed-workspace
REPORT_ROOT=$HOME/upmctl-validation
REPORT_DIR=$REPORT_ROOT/$(date -u +%Y%m%dT%H%M%SZ)

install -d -m 0700 "$REPORT_ROOT"
UPMCTL_BIN=/usr/local/bin/upmctl \
  ./upmctl/scripts/validate-test-environment.sh \
  --workspace "$WORKSPACE" \
  --report-dir "$REPORT_DIR" \
  --node k8s-3
```

包含Plan控制面验证：

```bash
UPMCTL_BIN=/usr/local/bin/upmctl \
  UPMCTL_TIMEOUT=3m \
  ./upmctl/scripts/validate-test-environment.sh \
  --workspace "$WORKSPACE" \
  --report-dir "$REPORT_DIR" \
  --node k8s-3 \
  --include-plan
```

`--node`省略时，脚本从`vm list`结果选择第一个`k8s-1`至`k8s-8`节点。为保证证据稳定，正式交付建议显式指定节点。

`--include-plan`要求目标节点能够产生`ACTION_REQUIRED` Plan。如果节点已经满足start目标，CLI可能返回`NOOP`；如果安全条件不足，可能返回`BLOCKED`。这两种结果都不会持久化Plan，因此脚本不会伪造Plan ID或继续调用get/validate/preflight，最终判定为`BLOCKED`。应选择合适的测试Worker后重新执行，不得为了通过验收而启动、停止或破坏VM。

当前Phase 2b2a的Preflight即使检查通过也固定返回exit code 3、`applyDecision=BLOCKED`、`executionAvailable=false`。只有stdout是`PreflightResult`成功envelope、requestId和runtime exit code均匹配时，脚本才把这个exit code 3视为正确契约证据；同为exit 3的`Error` envelope仍判定为FAIL。

## 4. 自动生成的证据目录

```text
<report-dir>/
├── validation-report.md
├── command-results.tsv
├── runtime.jsonl
├── dependencies.txt
├── host.txt
├── artifact-sha256.txt
├── evidence-sha256.txt
└── commands/
    ├── version.json
    ├── version.stderr.json
    └── ...
```

用途：

| 文件 | 证据 |
| --- | --- |
| `validation-report.md` | 主机、工作区、制品摘要、通过/失败/阻塞计数和最终交付判定 |
| `command-results.tsv` | 每个CLI命令的显式requestId、真实退出码、状态和stdout/stderr文件位置 |
| `runtime.jsonl` | CLI生成的隐私最小化start/complete/error生命周期事件 |
| `dependencies.txt` | Vagrant、virsh、kubectl版本，Vagrant插件和libvirt URI |
| `host.txt` | UTC时间、主机、运行身份、工作区和二进制路径/摘要 |
| `artifact-sha256.txt` | 被测`upmctl`二进制SHA-256 |
| `evidence-sha256.txt` | 本次报告和证据文件摘要，便于归档后验证完整性 |
| `commands/*.json` | 成功业务envelope或命令stdout |
| `commands/*.stderr.json` | 错误envelope或stderr；成功命令通常为空 |

验收结束后应将证据目录作为一个整体归档。归档前验证：

```bash
cd "$REPORT_DIR"
sha256sum -c evidence-sha256.txt
grep -n 'approval grant\|approval revoke\|"command":"apply"' runtime.jsonl
```

第二条命令预期无输出并返回1；它用于证明运行日志中没有禁用命令，不是验收失败。

## 5. 判定规则

### PASS

只有以下条件全部满足时才可标记为可交付：

- Vagrant、virsh、kubectl均可执行版本查询，Vagrant插件列表和libvirt URI可读取；
- version、capabilities、context、config、status、vm list、vm inspect均返回预期退出码；
- 每个命令的stdout或stderr envelope包含脚本指定的requestId，PASS命令的stdout kind与命令契约一致；
- runtime JSONL包含相同requestId和真实退出码；
- 若请求`--include-plan`，必须产生持久化`ACTION_REQUIRED` Plan，且get/validate成功、Preflight按当前契约返回3；
- 运行日志中不存在人工Approval或Apply命令。

### BLOCKED

工具本身未发现明确契约故障，但测试前提不足，例如：

- Vagrant、virsh或kubectl缺失/版本命令失败；
- 无可Inspection的`k8s-N`节点；
- 可选Plan返回`NOOP`或`BLOCKED`，无法继续验证持久化Plan；
- Vagrant插件列表或libvirt URI不可用。
- context、config、status、VM观察或Plan链路按CLI契约返回3（前置/安全策略阻塞）、4（外部依赖失败）或6（超时/中断），且其requestId和运行日志仍完整。

BLOCKED不是交付通过。修复测试环境或选择合适节点后，必须使用新的报告目录重新执行。

### FAIL

以下情况至少一项发生：

- CLI退出码不符合命令契约；
- envelope requestId与请求不一致或缺失；
- runtime JSONL缺少相同requestId/退出码；
- runtime JSONL出现禁用的Approval或Apply命令；
- 命令发生输出、日志或安全契约故障。

脚本自身返回码为：`0=PASS`、`3=BLOCKED`、`1=FAIL`、`2=脚本参数或本地安全前提错误`。

## 6. 人工签署模板

自动报告生成后，由发布负责人补充以下交付记录；不要修改原始命令证据。

```markdown
## 人工交付签署

- Release/版本：
- Release manifest或制品来源：
- Git commit：
- 测试主机/环境ID：
- 测试工作区：
- 被测二进制SHA-256：
- 报告目录/归档位置：
- 自动判定：PASS / BLOCKED / FAIL
- 已知限制或阻塞项：
- 故障单/变更单：
- 验收人：
- 复核人：
- 验收时间（UTC）：
- 最终交付结论：接受 / 拒绝 / 有条件接受
```

人工签署不能把自动`BLOCKED`或`FAIL`改写成`PASS`。有条件接受必须列出未完成能力、风险、责任人和重新验收期限。
