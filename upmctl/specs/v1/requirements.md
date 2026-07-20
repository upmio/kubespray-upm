# upmctl V1 Requirements

优先级：`MUST`、`SHOULD`、`WONT_V1`。状态：`accepted`、`implemented`、`verified`。

## 产品和上下文

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-SCOPE-001 | MUST | 只管理单台专用支持宿主机上的一个受管环境 |
| UPMCTL-CTX-001 | MUST | 从显式路径、当前工作区或标准嵌套路径发现部署工作区 |
| UPMCTL-CTX-002 | MUST | 区分受管环境、legacy 可读环境和未知环境 |
| UPMCTL-CTX-003 | MUST | 仅允许本地人类控制TTY将安全完整、仅含libvirt metadata且无既有控制状态的legacy工作区原子接管为Managed Environment |
| UPMCTL-CFG-001 | MUST | 安装期拓扑、网络、CNI、版本和资源配置必须锁定 |
| UPMCTL-CFG-002 | MUST | `config validate`必须只解析允许的有限语法并拒绝不安全Ruby、摘要和路径漂移 |

## CLI和状态

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-CLI-001 | MUST | 提供稳定 text、JSON、JSONL 输出 |
| UPMCTL-CLI-002 | MUST | `version` 和 `capabilities` 可在无环境时运行 |
| UPMCTL-OBS-001 | MUST | VM 状态必须关联 Vagrant、libvirt 和 Kubernetes 状态 |
| UPMCTL-OBS-002 | MUST | 顶层`status`必须聚合Context、Config Validation和VM Observed State |
| UPMCTL-ERR-001 | MUST | 错误包含稳定 code、message、details、remediation 和 requestId |

## Plan、审批和执行

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-PLAN-001 | MUST | 所有变更先生成只读计划 |
| UPMCTL-PLAN-002 | MUST | Apply 必须校验计划有效期、配置摘要和现场摘要 |
| UPMCTL-SAFE-001 | MUST | R1、R2、R3 变更必须由本地人类控制TTY批准，Skill和MCP不得代批 |
| UPMCTL-OP-001 | MUST | 操作支持 journal、cancel 和重新观察后的 resume |
| UPMCTL-OP-002 | MUST | PARTIAL 和 INTERRUPTED 不得当作成功 |

## VM生命周期

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-VAGR-001 | MUST | `vm list/status/inspect` 严格只读 |
| UPMCTL-VAGR-002 | MUST | 单 VM start 不得隐式执行 provision |
| UPMCTL-VAGR-003 | MUST | Worker stop/restart 前执行工作负载和存储安全检查 |
| UPMCTL-VAGR-004 | MUST | 不支持独立 stop/restart `k8s-1` |
| UPMCTL-VAGR-005 | MUST | Cluster start/stop 使用固定串行顺序 |
| UPMCTL-VAGR-006 | MUST | 不公开 VM create/destroy |
| UPMCTL-VAGR-007 | MUST | `vm ssh` 仅人类交互 TTY，不暴露给 Skill/MCP |

## Worker增减

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-NODE-001 | MUST | 每个计划只添加下一个连续编号普通 Worker |
| UPMCTL-NODE-002 | MUST | 每个计划只删除当前最高编号普通 Worker |
| UPMCTL-NODE-003 | MUST | 节点总数保持 3-8 |
| UPMCTL-NODE-004 | MUST | 添加使用 Guest Baseline、facts 和 `scale.yml`，不触发完整 `cluster.yml` |
| UPMCTL-NODE-005 | MUST | 删除使用安全检查、`remove_node.yml` 后再销毁 VM |
| UPMCTL-NODE-006 | MUST | LocalPV/hostPath 数据不可安全迁移时拒绝删除 |

## 网络、磁盘和Addon

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-NET-001 | MUST | 支持 NAT 和条件支持 Bridge |
| UPMCTL-NET-002 | MUST | 支持 Calico、Cilium、KPR、LB IPAM 和 L2 Announcement |
| UPMCTL-NET-003 | MUST | 安装后禁止在线切换网络和 CNI 决策 |
| UPMCTL-SAFE-002 | MUST | 磁盘必须显式 allowlist，禁止扫描初始化全部非根磁盘 |
| UPMCTL-ADDON-001 | MUST | 支持 LVM LocalPV、Prometheus、UPM Engine、UPM Platform |
| UPMCTL-ADDON-002 | MUST | `addon all` 使用固定依赖顺序，Nginx 不包含其中 |
| UPMCTL-VERIFY-001 | MUST | Helm 或外部命令退出 0 不等于成功 |

## Agent接口

| ID | Priority | Requirement |
| --- | --- | --- |
| UPMCTL-SKILL-001 | MUST | Skill 只通过 upmctl 执行变更能力 |
| UPMCTL-SKILL-002 | MUST | Skill 固定使用 discover-status-plan-preflight-explain-human-approval-apply-verify-report，并在人工审批处暂停 |
| UPMCTL-SKILL-003 | MUST | Skill 不得 SSH、调用底层工具或自主批准风险操作 |
| UPMCTL-MCP-001 | MUST | CLI 和未来 MCP 共用 Application Service |
| UPMCTL-MCP-002 | MUST | V1 冻结 Schema 和 adapter 边界，不要求发布 Server |
| UPMCTL-MCP-003 | MUST | MCP 不提供 Shell、SSH 或审批绕过接口 |
