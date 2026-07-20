---
name: upmctl-environment
description: Inspect, diagnose, and safely plan operations for a Kubespray UPM environment through the Go upmctl CLI. Use when Codex is asked to discover a kubespray-upm workspace, inspect Vagrant/libvirt/Kubernetes VM state, generate or validate an upmctl Plan, run read-only preflight, or report Approval state. Enforce human-only Approval writes and never bypass upmctl with direct infrastructure mutation commands.
---

# UPMCTL Environment

Use `upmctl` as the sole management interface. Keep every operation bound to the exact workspace named or discovered for the current repository.

## Establish the CLI and workspace

1. Prefer `upmctl` from `PATH`. Otherwise use the repository binary at `upmctl/bin/upmctl` when present.
2. Run `capabilities --output json` before any environment operation.
3. Run `context discover --output json` with the user's explicit `--workspace` when supplied.
4. Stop if the workspace is unknown, untrusted, ambiguous, or different from the environment the user named.
5. Use `--output json` for every automated call. Preserve request IDs and stable error codes in the report.

Read [references/phase-2b2a-contract.md](references/phase-2b2a-contract.md) when selecting commands or interpreting status fields.

## Follow the controlled workflow

For inspection or diagnosis:

```text
capabilities -> context discover -> config validate -> status -> vm list/status/inspect -> report
```

For a requested environment change:

```text
capabilities -> context discover -> status -> plan -> preflight -> explain
-> pause for human approval -> read approval -> capability-gated apply -> verify -> report
```

At each step:

- Treat an error, incomplete observation, drift, `BLOCKED`, `INVALID`, `EXPIRED`, or identity mismatch as a stop condition.
- Do not reinterpret VM `running`, command exit 0, or resource existence as end-to-end health.
- Report the Plan ID, Plan digest, risk, target, expiry, blockers, Approval status, and `applyDecision` exactly.
- Never modify a Plan or add a command, step, target, force flag, or repair action that is not in the Plan.

## Handle human Approval

Never execute or automate environment adoption:

```text
upmctl environment adopt --environment-id ENV_ID --workspace PATH
```

Adoption changes the workspace trust boundary. Do not simulate its TTY, provide its reason or challenge, or create `.upmctl/state.json`. Explain the checks and give the exact command to a human engineer to run locally, then resume with `context discover` only after they confirm completion.

Never execute or automate either command:

```text
upmctl approval grant --plan-id PLAN_ID
upmctl approval revoke APPROVAL_ID
```

Do not simulate a TTY, type a challenge, use `expect`, pipe input, inject actor or reason values, or ask another agent/MCP tool to approve.

When Approval is required:

1. Explain the exact Plan impact and risk.
2. Show the human the exact `approval grant --plan-id ... --workspace ...` command to run directly in their own local terminal.
3. Pause until the user confirms that action is complete.
4. Resume with only `approval get` or `approval list`.
5. Treat `APPROVED` as evidence only. It is not execution and does not override `applyDecision=BLOCKED`.

## Enforce capability boundaries

- Call `apply` only if `capabilities` reports `plan.apply.available=true`, Preflight is current, and a human Approval is `APPROVED`.
- In Phase 2b2a, stop after reporting that Apply is unavailable; do not fall back to legacy Shell.
- Never call `vagrant`, `virsh`, `ssh`, `kubectl`, `helm`, `ansible-playbook`, or the legacy setup scripts to perform a mutation.
- Never call `vm ssh` from this Skill.
- Never create or edit `.upmctl/plans`, `.upmctl/approvals`, `.upmctl/admissions`, operations, or lock files directly.
- Do not claim support for VM stop/restart, cluster lifecycle, node add/remove, Addon mutation, Operation, Claim, or MCP writes unless `capabilities` explicitly enables them.

## Report the result

Return a compact audit-oriented summary containing:

- workspace and environment ID;
- observed health and incomplete sources;
- Plan/Approval identifiers and statuses when present;
- blockers and remediation from upmctl;
- whether any target environment mutation occurred;
- the exact next human or CLI action.

State clearly when no mutation occurred. Never describe a successful Plan or Approval as a successfully changed environment.
