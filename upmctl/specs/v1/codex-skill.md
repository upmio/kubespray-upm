# Codex Skill Contract

Skill是受控入口，不包含基础设施业务逻辑。

## 固定工作流

```text
capabilities / context discover
-> status
-> plan
-> preflight
-> explain impact and risk
-> human approval
-> apply
-> verify
-> report
```

## 必须行为

- 使用准确的workspace和Managed Environment。
- 调用`capabilities`确认CLI兼容性。
- 自动化调用使用JSON/JSONL。
- 只读调用`approval get/list`解释审批状态；不得调用`approval grant/revoke`或写`.upmctl/approvals`、`.upmctl/admissions`。
- 诊断请求只做只读发现，不自动修复。
- PARTIAL/INTERRUPTED必须说明最后成功阶段和恢复入口。
- 含糊的“重启环境”先确认节点或全集群目标。

## 禁止行为

- 直接调用vagrant、virsh、ssh、kubectl、helm或sudo进行变更。
- 调用`vm ssh`或通用Shell。
- 自主批准或撤销任何R1、R2、R3 Plan。
- 伪造本地TTY、OS actor、reason、typed challenge或其他人工审批上下文。
- 跳过plan、漂移检查、PDB、drain或LocalPV检查。
- 将命令退出0、资源存在或VM running当作完整成功。

Phase 2b2a中Skill工作流在`human approval`处暂停，由人类在本地控制TTY直接运行`upmctl approval grant`。Skill随后只能用`approval get/list`读取结果。因为Apply仍关闭，Skill必须明确报告`applyDecision=BLOCKED`，不能把`APPROVED`解释为已执行或可执行。

Skill实现目录和发布方式在Go CLI的只读和plan契约稳定后创建，Skill引用本Spec，不复制另一套安全规则。
