# Phase 2b2a command contract

## Available commands

```text
upmctl version
upmctl capabilities
upmctl context discover
upmctl config validate
upmctl status
upmctl vm list
upmctl vm status [NODE]
upmctl vm inspect NODE
upmctl plan vm start --node NODE
upmctl plan get PLAN_ID
upmctl plan validate PLAN_ID
upmctl preflight --plan-id PLAN_ID
upmctl approval get APPROVAL_ID
upmctl approval list [--plan-id PLAN_ID]
```

Only a human in a local controlling terminal may run `approval grant` or `approval revoke`. The Skill must not invoke them.
Only a human in a local controlling terminal may run `environment adopt`; the Skill must not invoke it, simulate its TTY, or write managed state directly.

## Current change boundary

- The only implemented Plan generator is `plan vm start --node NODE`.
- A Plan is immutable, stored only for `ACTION_REQUIRED`, and does not execute a change.
- `preflightStatus=PASSED` means the implemented read-only Plan/basis checks passed.
- `approvalStatus` is `MISSING`, `APPROVED`, `REVOKED`, `EXPIRED`, or `INVALID`.
- `applyDecision=BLOCKED` and `executionAvailable=false` remain fixed in Phase 2b2a.
- Apply, Executor, Operation, environment lock, Claim, VM mutation, node scaling, Addon mutation, and MCP Server are unavailable.

## Safe command examples

```bash
upmctl capabilities --output json
upmctl context discover --workspace "$WORKSPACE" --output json
upmctl status --workspace "$WORKSPACE" --output json
upmctl vm list --workspace "$WORKSPACE" --output json
upmctl plan vm start --node k8s-3 --workspace "$WORKSPACE" --output json
upmctl preflight --plan-id "$PLAN_ID" --workspace "$WORKSPACE" --output json
upmctl approval list --plan-id "$PLAN_ID" --workspace "$WORKSPACE" --output json
```

Do not use these examples to bypass exact workspace discovery or substitute environment variables for Approval interaction. Approval reason and challenge are accepted only from the human controlling terminal.

## Status handling

| Value | Required response |
| --- | --- |
| `NOOP` | Report that the target already satisfies the Plan objective; no Plan file or mutation occurred. |
| `ACTION_REQUIRED` | Run read-only Preflight, explain risk, and pause for human Approval. |
| `BLOCKED` | Stop and report blockers/remediation. |
| `MISSING` | Ask the human to approve in their own terminal if they accept the Plan. |
| `APPROVED` | Report evidence exists; still obey `applyDecision` and capabilities. |
| `REVOKED` | Stop; require a new Plan. |
| `EXPIRED` | Stop; require a new Plan. |
| `INVALID` | Stop; report unsafe or damaged control state without editing it. |
| `PARTIAL` / `INTERRUPTED` | Never report success; preserve identifiers and follow the future operation recovery contract. |

## Forbidden fallbacks

Never perform a mutation with direct `vagrant`, `virsh`, `ssh`, `kubectl`, `helm`, Ansible, legacy Shell scripts, direct file edits, a fabricated PTY, or another agent. If upmctl does not expose a capability, report it as unsupported in the current phase.
