# upmctl V1 Specifications

本目录是 `upmctl V1` 的规范权威。实现顺序固定为：

```text
Master Spec -> Requirements -> Contracts/Schemas -> Acceptance -> Implementation -> User Documentation
```

## 文档索引

| 文档 | 权威内容 |
| --- | --- |
| [master-spec.md](master-spec.md) | 产品范围、支持矩阵、非目标和冻结条款 |
| [requirements.md](requirements.md) | 带稳定 ID 的功能和非功能需求 |
| [architecture.md](architecture.md) | Go 核心、适配器和交付边界 |
| [cli-contract.md](cli-contract.md) | CLI、输出、退出码和兼容规则 |
| [state-and-safety.md](state-and-safety.md) | 状态、计划、审批、漂移和操作状态机 |
| [vm-lifecycle.md](vm-lifecycle.md) | Vagrant/libvirt VM 查询、启停、重启和 SSH 边界 |
| [node-lifecycle.md](node-lifecycle.md) | Worker 添加、减少和存储安全边界 |
| [integrations.md](integrations.md) | Legacy Shell、Vagrant、Kubespray、kubectl 和 Helm 集成 |
| [codex-skill.md](codex-skill.md) | Codex Skill 的固定工作流和禁用能力 |
| [mcp-reservation.md](mcp-reservation.md) | MCP V1 预留接口 |
| [acceptance.md](acceptance.md) | 场景验收和发布门禁 |
| [implementation-plan.md](implementation-plan.md) | 分阶段开发计划 |
| [traceability.yaml](traceability.yaml) | 需求到代码、测试和 legacy 来源的追踪 |
| [versions.yaml](versions.yaml) | 产品和依赖版本策略 |

## 治理规则

1. `master-spec.md` 是 V1 唯一总权威，下级文档不得扩大范围。
2. 每项实现必须关联 `UPMCTL-<DOMAIN>-<NNN>` 需求 ID。
3. README 和 Skill 不得定义新的业务行为。
4. Shell 是迁移基线，不是 Go V1 的最终产品契约。
5. `Implemented` 不等于 `Verified`；验收未通过不能标记发布完成。
6. Spec 变更必须先更新需求、契约和验收，再修改实现。
