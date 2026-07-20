# upmctl V1 Architecture

## 分层

```text
CLI / Codex Skill / future MCP
              |
       Application Service
              |
  Domain policies and state machines
              |
 Runner + Vagrant + libvirt + Kubespray + Kubernetes + Helm adapters
```

## Go模块

Go实现放在仓库根目录的 `upmctl/` 独立模块，避免改变上游 Kubespray 的依赖和构建方式。

```text
upmctl/
├── cmd/upmctl/
└── internal/
    ├── app/          # 用例编排，CLI/MCP共享入口
    ├── cli/          # 参数解析和呈现
    ├── context/      # 工作区和受管环境发现
    ├── output/       # 稳定Envelope和错误契约
    ├── runner/       # 外部命令、超时、取消、脱敏
    ├── vm/           # VM observed state与Vagrant/libvirt适配
    ├── plan/         # 不可变Plan、存储、读取、校验和只读preflight
    ├── readiness/    # 执行前basis比较、审批投影和固定安全检查
    ├── approval/     # 人工审批、撤销、Admission槽和只读状态
    ├── operation/    # 后续阶段：journal和状态机
    └── adapters/     # Kubespray、Kubernetes、Helm、host
```

## 依赖规则

- Domain 不依赖 CLI、终端文本或具体外部命令。
- CLI 和未来 MCP 只调用 Application Service。
- 外部命令必须通过 Runner；禁止业务包直接调用 `os/exec`。
- V1 优先使用系统已有 CLI，不引入 libvirt SDK、client-go 或 Helm SDK。
- 所有变更用例必须消费不可变 Plan。

## Managed Environment

受管状态默认位于部署工作区：

```text
.upmctl/
├── state.json
├── plans/<plan-id>.json
├── approvals/by-plan/<plan-id>.json                  # Phase 2b2a
├── admissions/<plan-id>.json                         # Revocation or future Claim
├── operations/<operation-id>/events/<sequence>.json  # Phase 2b2b reservation
└── reports/<operation-id>.json
```

`approvals/by-plan/<planId>.json`是每Plan至多一个、不可覆盖的Approval。`admissions/<planId>.json`是原子互斥槽，只能容纳ApprovalRevocation或未来内部PlanClaim，从存储层消除撤销与开始执行同时成功的竞态。Phase 2b2a不会创建PlanClaim。

`operation.schema.json`当前只作为未来API投影视图；Phase 2b2b的权威journal将采用不可变、连续编号的事件文件，而不是可能因崩溃留下半行的单个追加JSONL文件。Phase 2b2a不会创建`operations/`目录或环境锁。

Legacy 工作区只能被动发现文件布局并报告；只有本地人类控制TTY完成`environment adopt`、生成带文件/机器摘要和人机审计证据的不可变Managed State后，才允许加载摘要绑定的Vagrantfile或kubeconfig进行只读观察。Adopt本身不得执行外部命令或目标环境变更。

“只读观察”不等于可以加载任意工作区代码。Vagrant会执行Vagrantfile/config.rb，kubectl可能执行kubeconfig credential plugin，因此只有`managed-environment.schema.json`验证通过、工作区身份一致且相关文件摘要匹配时，CLI才允许执行这些外部观察命令。Legacy或损坏工作区只允许被动context报告。

## Phase 2b2a计划与审批链路

```text
status        -> plan -> preflight -> explain impact/risk -> human approval
plan get      -> secure Plan store read                  -> PlanInspection
plan validate -> secure Plan store read/local bindings   -> PlanValidation
approval get/list -> secure Approval/Admission read      -> ApprovalInspection/List
preflight     -> re-observe + Approval/Admission read    -> PreflightResult(BLOCKED)
```

- Plan读取与创建使用相同的工作区边界和文件身份策略，拒绝symlink、非普通文件、路径逃逸、超限、重复JSON key、未知字段和尾随JSON。
- `plan get`和`plan validate`不得调用外部观察或变更命令。
- `preflight`只能复用已允许的只读Runner路径；禁止`vagrant up/halt/reload`、`virsh start/shutdown/destroy`、kubectl写操作和SSH。
- Plan查询、Approval查询和Preflight均不得写`.upmctl/`或目标环境。只有本地控制TTY的人类可以通过`environment adopt`原子创建Managed State，或通过`approval grant/revoke`分别原子写入一个Approval或Revocation工件。
- `PreflightResult`中的`PASSED`只描述只读现场检查结果；Application Service必须固定返回`applyDecision=BLOCKED`和`executionAvailable=false`，CLI、Skill和未来MCP不得重新解释。
- Approval的approver、actor和humanPresence由CLI从本地OS与控制TTY观察并记录，不接受调用方字段；这些证据不构成独立强身份认证，这一限制必须进入Spec说明和CLI输出。
- Executor、Plan Claim、Operation和并发锁仍是Phase 2b2b保留边界，未实现时必须返回`UPMCTL_NOT_IMPLEMENTED`。
