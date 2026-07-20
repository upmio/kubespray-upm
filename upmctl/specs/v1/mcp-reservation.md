# MCP Reservation

V1交付接口边界，不强制交付可用MCP Server。

## 预留工具

```text
upm_capabilities
upm_discover_context
upm_get_status
upm_run_preflight
upm_create_plan
upm_get_approval
upm_list_approvals
upm_apply_plan
upm_get_operation
upm_cancel_operation
upm_resume_operation
upm_verify
upm_generate_report
```

所有工具映射到与CLI相同的Application Service和领域类型。MCP身份必须进入request和未来operation审计上下文，但不得作为Approval的授予或撤销主体。

`upm_get_approval`和`upm_list_approvals`只映射只读查询。MCP不得暴露grant、revoke、approve、deny或任何能写Approval/Admission的工具；也不得通过通用参数间接调用这些命令。未来MCP身份可以进入请求审计，但不能替代CLI从本地OS和控制TTY直接观察的人类在场证据。

## 禁止接口

- shell/exec/ssh。
- 任意vagrant、virsh、kubectl或helm参数透传。
- 直接create/destroy VM。
- 绕过plan、risk和approval。
- 创建、覆盖、续期或撤销Approval，以及创建Plan Claim。
- 读取或返回私钥、默认密码和未脱敏日志。

首版Schema使用`upmctl.upm.io/v1alpha1`；与CLI Envelope和错误字段保持一致。
