package cli

import "fmt"

const rootHelp = `upmctl - Kubespray UPM environment CLI (Phase 2b2a)

Usage:
  upmctl [global options] COMMAND [arguments]
  upmctl help [approval|plan|vm|environment]
  upmctl --help
  upmctl -h

Global options:
  --workspace PATH       Deployment workspace (prefer an absolute path)
  --output FORMAT        text, json, or jsonl; help itself is always text
  --request-id ID        Correlation ID for output and optional runtime logs
  --timeout DURATION     Positive Go duration such as 30s or 2m
  --log-file PATH        Append privacy-minimized JSONL lifecycle logs
  --no-color             Keep text output color-free

Available command groups:
  environment            Adopt, discover, validate, and observe an environment
  vm                     List and inspect Vagrant/libvirt-backed VMs
  plan                   Create or inspect a non-executable vm.start Plan
  approval               Human TTY approval and read-only approval queries

Other available commands:
  version
  capabilities
  preflight --plan-id PLAN_ID

Safety boundary:
  Phase 2b2a does not implement apply, execution, environment locks,
  Operation journals, VM mutation, cluster deployment, or node changes.
  An APPROVED Approval does not make a Plan executable: applyDecision remains
  BLOCKED and executionAvailable remains false.

Run "upmctl help GROUP" for group-specific help.
`

const environmentHelp = `upmctl environment help (Phase 2b2a)

Usage:
  upmctl environment adopt --environment-id ENV_ID --workspace PATH
  upmctl context discover [--workspace PATH]
  upmctl config validate [--workspace PATH]
  upmctl status [--workspace PATH]

Commands:
  environment adopt      Bind a verified legacy libvirt workspace as managed
  context discover       Find the repository/deployment workspace without mutation
  config validate        Parse the supported config.rb subset without executing Ruby
  status                 Correlate context, config, VM, and Kubernetes observations

Boundary:
  Adoption is the only environment command that writes local control state. It
  creates only .upmctl/state.json, never runs Vagrant, virsh, kubectl, or Ruby,
  and refuses existing control-state, other providers, or unsafe metadata.
  Legacy or invalid workspaces receive conservative, limited observation. Managed
  VM observation requires a valid .upmctl/state.json binding. These commands do
  not deploy, repair, start, stop, or otherwise change the target environment.

Global options include --output, --request-id, --timeout, --log-file,
--workspace, and --no-color. Run "upmctl help" for their descriptions.
`

const vmHelp = `upmctl vm help (Phase 2b2a)

Usage:
  upmctl vm list [--workspace PATH]
  upmctl vm status [NODE] [--workspace PATH]
  upmctl vm inspect NODE [--workspace PATH]

Commands:
  list                   List expected VMs and correlated health
  status [NODE]          Show all VM status or one named VM
  inspect NODE           Show the complete read-only VM inspection

Boundary:
  VM commands are read-only. VM start, stop, restart, ssh, repair, and destroy
  are not available. Use "upmctl plan vm start --node k8s-N" only to create a
  non-executable Plan; it does not invoke a mutation command.

Global options include --output, --request-id, --timeout, --log-file,
--workspace, and --no-color. Run "upmctl help" for their descriptions.
`

const planHelp = `upmctl plan help (Phase 2b2a)

Usage:
  upmctl plan vm start --node k8s-N [--workspace PATH]
  upmctl plan get PLAN_ID [--workspace PATH]
  upmctl plan validate PLAN_ID [--workspace PATH]
  upmctl preflight --plan-id PLAN_ID [--workspace PATH]

Commands:
  plan vm start          Produce NOOP, BLOCKED, or ACTION_REQUIRED; never execute
  plan get               Safely read and inspect an immutable stored Plan
  plan validate          Validate Plan integrity, TTL, and local basis bindings
  preflight              Re-observe read-only state and report conservative readiness

Boundary:
  vm.start is the only Plan action currently generated. stop/restart, cluster,
  node, and addon Plans are unavailable. Apply is closed in Phase 2b2a; even a
  passed Preflight and APPROVED Approval keep applyDecision=BLOCKED and
  executionAvailable=false.

Global options include --output, --request-id, --timeout, --log-file,
--workspace, and --no-color. Run "upmctl help" for their descriptions.
`

const approvalHelp = `upmctl approval help (Phase 2b2a)

Usage:
  upmctl approval grant --plan-id PLAN_ID [--workspace PATH]
  upmctl approval get APPROVAL_ID [--workspace PATH]
  upmctl approval list [--plan-id PLAN_ID] [--workspace PATH]
  upmctl approval revoke APPROVAL_ID [--workspace PATH]

Commands:
  grant                  Create one immutable Approval for an eligible Plan
  get                    Read one Approval and its current status
  list                   Read Approvals, optionally filtered by Plan ID
  revoke                 Create an immutable revocation; never edit the Approval

Human boundary:
  grant and revoke must be run by a human from the local controlling TTY. Reason
  and exact challenge confirmation are read from /dev/tty. Skill, MCP, CI, pipes,
  background jobs, --yes, --force, and reason/actor flags cannot approve or revoke.
  TTY and OS actor observations are audit evidence, not cryptographic proof of a
  real-world identity. get and list are read-only and may be used by an Agent.

Execution boundary:
  Approval records human intent only. Apply is closed in Phase 2b2a; APPROVED
  never means executed or executable, and executionAvailable remains false.

Global options include --output, --request-id, --timeout, --log-file,
--workspace, and --no-color. Run "upmctl help" for their descriptions.
`

func parseHelpRequest(args []string) (topic string, requested bool, err error) {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return "", true, nil
	}
	if len(args) == 0 || args[0] != "help" {
		return "", false, nil
	}
	if len(args) == 1 {
		return "", true, nil
	}
	if len(args) != 2 || !knownHelpTopic(args[1]) {
		return "", true, fmt.Errorf("help topic must be one of approval, plan, vm, or environment")
	}
	return args[1], true, nil
}

func knownHelpTopic(topic string) bool {
	switch topic {
	case "approval", "plan", "vm", "environment":
		return true
	default:
		return false
	}
}

func helpText(topic string) string {
	switch topic {
	case "approval":
		return approvalHelp
	case "plan":
		return planHelp
	case "vm":
		return vmHelp
	case "environment":
		return environmentHelp
	default:
		return rootHelp
	}
}
