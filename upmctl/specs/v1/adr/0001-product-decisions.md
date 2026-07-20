# ADR-0001: upmctl V1 Product and Architecture Decisions

状态：Accepted

## 决策

1. 使用Go实现确定性CLI和共享Application Service。
2. 保留Vagrant/libvirt和Kubespray，不重写provider或playbook。
3. V1只管理单宿主机单受管环境。
4. 所有变更必须plan-before-apply；R1、R2、R3均要求本地人类控制TTY审批，Skill、MCP和非交互调用不得代批。
5. Codex Skill是受控消费者，不承载业务逻辑。
6. MCP只预留类型化接口，V1不要求Server。
7. 不提供通用Shell、Agent SSH和底层参数透传。
8. V1采用journal和resume，不承诺事务级宿主机回滚。

## 结果

系统更容易构建为可测试的确定性产品，但不会覆盖通用VM平台、多集群控制面和自治Agent场景。Legacy Shell仍可作为迁移证据，不能成为绕过新安全策略的后门。
