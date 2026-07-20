package cli

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/admission"
	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/buildinfo"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	upmdigest "github.com/upmio/kubespray-upm/upmctl/internal/digest"
	upmlogging "github.com/upmio/kubespray-upm/upmctl/internal/logging"
	"github.com/upmio/kubespray-upm/upmctl/internal/managedenv"
	"github.com/upmio/kubespray-upm/upmctl/internal/output"
	upmplan "github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/readiness"
	upmstatus "github.com/upmio/kubespray-upm/upmctl/internal/status"
	"github.com/upmio/kubespray-upm/upmctl/internal/terminal"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

type CLI struct {
	service   *app.Service
	stdout    io.Writer
	stderr    io.Writer
	now       func() time.Time
	cwd       string
	openTTY   func() (terminal.HumanTerminal, error)
	challenge func() (string, error)
}

type options struct {
	format    output.Format
	workspace string
	requestID string
	timeout   time.Duration
	logFile   string
	result    *runResult
}

type runResult struct {
	errorCode string
}

func New(service *app.Service, stdout, stderr io.Writer) *CLI {
	return &CLI{
		service:   service,
		stdout:    stdout,
		stderr:    stderr,
		now:       time.Now,
		cwd:       app.CurrentDirectory(),
		openTTY:   func() (terminal.HumanTerminal, error) { return terminal.Open() },
		challenge: terminal.RandomChallenge,
	}
}

func (c *CLI) Run(args []string) (exitCode int) {
	opts, remaining, err := parseOptions(args)
	if opts.requestID == "" {
		opts.requestID = requestID(c.now())
	}
	opts.result = &runResult{}
	commandName := canonicalCommand(remaining)
	if opts.logFile != "" {
		runtimeLog, logErr := upmlogging.Open(opts.logFile)
		if logErr != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_LOG_OPEN_FAILED", Message: logErr.Error(), Remediation: "use an existing real directory and a non-symlink regular log file with mode 0600", ExitCode: 70})
		}
		if logErr := runtimeLog.Start(c.now(), opts.requestID, commandName); logErr != nil {
			_ = runtimeLog.Close()
			return c.writeError(opts, &app.Error{Code: "UPMCTL_LOG_WRITE_FAILED", Message: logErr.Error(), Remediation: "verify that the log filesystem is writable and has free space", ExitCode: 70})
		}
		defer func() {
			if logErr := runtimeLog.Finish(c.now(), opts.requestID, commandName, exitCode, opts.result.errorCode); logErr != nil && exitCode == 0 {
				exitCode = c.writeError(opts, &app.Error{Code: "UPMCTL_LOG_WRITE_FAILED", Message: logErr.Error(), Remediation: "verify that the log filesystem is writable and has free space", ExitCode: 70})
			}
			_ = runtimeLog.Close()
		}()
	}
	if err != nil {
		return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: err.Error(), ExitCode: 2})
	}
	if len(remaining) == 0 {
		return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "a command is required", Remediation: "run upmctl capabilities", ExitCode: 2})
	}
	if topic, requested, helpErr := parseHelpRequest(remaining); requested {
		if helpErr != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: helpErr.Error(), Remediation: "run upmctl help", ExitCode: 2})
		}
		return c.writeHelp(opts, topic)
	}

	ctx := context.Background()
	if opts.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.timeout)
		defer cancel()
	}

	switch remaining[0] {
	case "version":
		if len(remaining) != 1 {
			return c.writeError(opts, noArgumentCommandUsage("version", remaining[1:]))
		}
		return c.writeSuccess(opts, "Version", c.service.Version(), textVersion)
	case "capabilities":
		if len(remaining) != 1 {
			return c.writeError(opts, noArgumentCommandUsage("capabilities", remaining[1:]))
		}
		return c.writeSuccess(opts, "Capabilities", c.service.Capabilities(), textCapabilities)
	case "environment":
		if len(remaining) >= 2 && remaining[1] == "adopt" {
			environmentID, parseErr := parseEnvironmentIDOption(remaining[2:])
			if parseErr != nil {
				return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: parseErr.Error(), Remediation: "usage: upmctl environment adopt --environment-id ENV_ID --workspace PATH", ExitCode: 2})
			}
			if opts.workspace == "" {
				return c.writeError(opts, &app.Error{Code: "UPMCTL_WORKSPACE_REQUIRED", Message: "environment adopt requires an explicit --workspace", Remediation: "usage: upmctl environment adopt --environment-id ENV_ID --workspace PATH", ExitCode: 2})
			}
			humanTTY, ttyErr := c.requireHumanTTY(opts)
			if ttyErr != nil {
				return c.writeError(opts, ttyErr)
			}
			defer humanTTY.Close()
			preparation, appErr := c.service.PrepareEnvironmentAdoption(c.cwd, opts.workspace, environmentID)
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			reason, err := humanTTY.ReadReason(adoptionReasonPrompt(preparation))
			if err != nil {
				return c.writeError(opts, humanInteractionError("read adoption reason", err))
			}
			challenge, err := c.challenge()
			if err != nil {
				return c.writeError(opts, humanInteractionError("generate adoption challenge", err))
			}
			confirmed, err := humanTTY.ConfirmChallenge("Adopt this exact legacy workspace and trust its bound files for read-only observation?", challenge)
			if err != nil {
				return c.writeError(opts, humanInteractionError("confirm environment adoption", err))
			}
			if !confirmed {
				return c.writeError(opts, &app.Error{Code: "UPMCTL_ADOPTION_NOT_CONFIRMED", Message: "the adoption challenge was not confirmed exactly", Remediation: "review every displayed digest and UUID, then rerun from the local controlling terminal", ExitCode: 3})
			}
			challengeDigest, err := upmdigest.Sum(challenge)
			if err != nil {
				return c.writeError(opts, &app.Error{Code: "UPMCTL_ADOPTION_EVIDENCE_FAILED", Message: err.Error(), ExitCode: 70})
			}
			created, appErr := c.service.AdoptEnvironment(c.cwd, opts.workspace, environmentID, app.EnvironmentAdoptionEvidence{
				Reason: reason, Terminal: "/dev/tty", ChallengeDigest: challengeDigest,
				RequestID: opts.requestID, CLIVersion: c.service.Version().Version,
			}, c.now())
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			return c.writeSuccess(opts, "ManagedEnvironment", created, textManagedEnvironment)
		}
	case "context":
		if len(remaining) >= 2 && remaining[1] == "discover" {
			if len(remaining) != 2 {
				return c.writeError(opts, noArgumentCommandUsage("context discover", remaining[2:]))
			}
			deployment, appErr := c.service.DiscoverContext(c.cwd, opts.workspace)
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			return c.writeSuccess(opts, "DeploymentContext", deployment, textContext)
		}
	case "config":
		if len(remaining) >= 2 && remaining[1] == "validate" {
			if len(remaining) != 2 {
				return c.writeError(opts, noArgumentCommandUsage("config validate", remaining[2:]))
			}
			validation, appErr := c.service.ValidateConfig(c.cwd, opts.workspace)
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			code := c.writeSuccess(opts, "ConfigValidation", validation, textConfigValidation)
			if code != 0 {
				return code
			}
			if !validation.Validation.Valid || !validation.Validation.Complete {
				return 3
			}
			return 0
		}
	case "status":
		if len(remaining) != 1 {
			return c.writeError(opts, noArgumentCommandUsage("status", remaining[1:]))
		}
		status, appErr := c.service.Status(ctx, c.cwd, opts.workspace)
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		return c.writeSuccess(opts, "EnvironmentStatus", status, textStatus)
	case "vm":
		return c.runVM(ctx, opts, remaining[1:])
	case "plan":
		return c.runPlan(ctx, opts, remaining[1:])
	case "preflight":
		planID, parseErr := parsePlanIDOption(remaining[1:])
		if parseErr != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: parseErr.Error(), Remediation: "usage: upmctl preflight --plan-id PLAN_ID", ExitCode: 2})
		}
		result, appErr := c.service.PreflightPlan(ctx, c.cwd, opts.workspace, planID, c.now)
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		if code := c.writeSuccess(opts, "PreflightResult", result, textPreflight); code != 0 {
			return code
		}
		return 3
	case "approval":
		return c.runApproval(ctx, opts, remaining[1:])
	case "apply":
		if _, parseErr := parsePlanIDOption(remaining[1:]); parseErr != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: parseErr.Error(), Remediation: "usage: upmctl apply --plan-id PLAN_ID", ExitCode: 2})
		}
		return c.writeError(opts, &app.Error{Code: "UPMCTL_NOT_IMPLEMENTED", Message: "apply is not implemented in the current Phase 2b2a release", Details: map[string]any{"capability": "vm.mutate", "available": false}, Remediation: "use preflight --plan-id to inspect read-only readiness", ExitCode: 3})
	}

	if strings.HasPrefix(remaining[0], "-") {
		return c.writeError(opts, &app.Error{
			Code:        "UPMCTL_USAGE",
			Message:     fmt.Sprintf("unknown global option %q", remaining[0]),
			Remediation: "run upmctl help and use only documented global options",
			ExitCode:    2,
		})
	}
	return c.writeError(opts, &app.Error{
		Code:        "UPMCTL_NOT_IMPLEMENTED",
		Message:     fmt.Sprintf("command %q is not implemented in the current phase", strings.Join(remaining, " ")),
		Remediation: "run upmctl capabilities to inspect currently available commands",
		ExitCode:    3,
	})
}

func noArgumentCommandUsage(command string, unexpected []string) *app.Error {
	message := fmt.Sprintf("%s does not accept arguments", command)
	if len(unexpected) > 0 && strings.HasPrefix(unexpected[0], "-") {
		message = fmt.Sprintf("%s does not accept option %q", command, unexpected[0])
	}
	return &app.Error{
		Code:        "UPMCTL_USAGE",
		Message:     message,
		Remediation: fmt.Sprintf("usage: upmctl %s", command),
		ExitCode:    2,
	}
}

var planNodePattern = regexp.MustCompile(`^k8s-[1-8]$`)
var planIdentifierPattern = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)
var approvalIdentifierPattern = regexp.MustCompile(`^approval-[0-9a-f]{64}$`)

func parseEnvironmentIDOption(args []string) (string, error) {
	var environmentID string
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--environment-id":
			index++
			if index >= len(args) || environmentID != "" {
				return "", fmt.Errorf("--environment-id requires exactly one value")
			}
			environmentID = args[index]
		case strings.HasPrefix(args[index], "--environment-id="):
			if environmentID != "" {
				return "", fmt.Errorf("--environment-id may be specified only once")
			}
			environmentID = strings.TrimPrefix(args[index], "--environment-id=")
		default:
			return "", fmt.Errorf("unexpected environment adopt argument %q", args[index])
		}
	}
	if !managedenv.ValidEnvironmentID(environmentID) {
		return "", fmt.Errorf("--environment-id must match env-<lowercase letters, digits, and internal hyphens>")
	}
	return environmentID, nil
}

type approvalList struct {
	APIVersion         string                   `json:"apiVersion"`
	Kind               string                   `json:"kind"`
	CheckedAt          string                   `json:"checkedAt"`
	Items              []app.ApprovalInspection `json:"items"`
	ExecutionAvailable bool                     `json:"executionAvailable"`
}

func (c *CLI) runApproval(ctx context.Context, opts options, args []string) int {
	if len(args) == 0 {
		return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "approval subcommand is required", Remediation: "use approval grant|get|list|revoke", ExitCode: 2})
	}
	switch args[0] {
	case "grant":
		planID, err := parsePlanIDOption(args[1:])
		if err != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: err.Error(), Remediation: "usage: upmctl approval grant --plan-id PLAN_ID", ExitCode: 2})
		}
		humanTTY, appErr := c.requireHumanTTY(opts)
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		defer humanTTY.Close()

		preparation, serviceErr := c.service.PrepareApproval(ctx, c.cwd, opts.workspace, planID, c.now)
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		reason, err := humanTTY.ReadReason(grantReasonPrompt(preparation))
		if err != nil {
			return c.writeError(opts, humanInteractionError("read approval reason", err))
		}
		challenge, err := c.challenge()
		if err != nil {
			return c.writeError(opts, humanInteractionError("generate approval challenge", err))
		}
		confirmed, err := humanTTY.ConfirmChallenge("Approve this exact immutable Plan?", challenge)
		if err != nil {
			return c.writeError(opts, humanInteractionError("confirm approval challenge", err))
		}
		if !confirmed {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_APPROVAL_NOT_CONFIRMED", Message: "the approval challenge was not confirmed exactly", Remediation: "review the Plan and rerun approval grant from the local controlling terminal", ExitCode: 3})
		}
		evidence, evidenceErr := c.approvalEvidence(reason, challenge, opts.requestID)
		if evidenceErr != nil {
			return c.writeError(opts, evidenceErr)
		}
		created, serviceErr := c.service.GrantApproval(ctx, c.cwd, opts.workspace, planID, evidence, c.now)
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		return c.writeSuccess(opts, "Approval", created, textApproval)
	case "get":
		if len(args) != 2 || !approvalIdentifierPattern.MatchString(args[1]) {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "APPROVAL_ID must match approval-<64 lowercase hex>", Remediation: "usage: upmctl approval get APPROVAL_ID", ExitCode: 2})
		}
		inspection, serviceErr := c.service.GetApproval(c.cwd, opts.workspace, args[1], c.now())
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		return c.writeSuccess(opts, "ApprovalInspection", inspection, textApprovalInspection)
	case "list":
		planID, err := parseOptionalPlanID(args[1:])
		if err != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: err.Error(), Remediation: "usage: upmctl approval list [--plan-id PLAN_ID]", ExitCode: 2})
		}
		checkedAt := c.now()
		items, serviceErr := c.service.ListApprovals(c.cwd, opts.workspace, planID, checkedAt)
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		return c.writeSuccess(opts, "ApprovalInspectionList", approvalList{
			APIVersion: readiness.APIVersion, Kind: "ApprovalInspectionList",
			CheckedAt: checkedAt.UTC().Format(time.RFC3339Nano), Items: items,
			ExecutionAvailable: false,
		}, textApprovalList)
	case "revoke":
		if len(args) != 2 || !approvalIdentifierPattern.MatchString(args[1]) {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "APPROVAL_ID must match approval-<64 lowercase hex>", Remediation: "usage: upmctl approval revoke APPROVAL_ID", ExitCode: 2})
		}
		humanTTY, appErr := c.requireHumanTTY(opts)
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		defer humanTTY.Close()
		inspection, serviceErr := c.service.GetApproval(c.cwd, opts.workspace, args[1], c.now())
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		reason, err := humanTTY.ReadReason(revokeReasonPrompt(inspection))
		if err != nil {
			return c.writeError(opts, humanInteractionError("read revocation reason", err))
		}
		challenge, err := c.challenge()
		if err != nil {
			return c.writeError(opts, humanInteractionError("generate revocation challenge", err))
		}
		confirmed, err := humanTTY.ConfirmChallenge("Revoke this exact immutable Approval?", challenge)
		if err != nil {
			return c.writeError(opts, humanInteractionError("confirm revocation challenge", err))
		}
		if !confirmed {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_REVOCATION_NOT_CONFIRMED", Message: "the revocation challenge was not confirmed exactly", Remediation: "review the Approval and rerun approval revoke from the local controlling terminal", ExitCode: 3})
		}
		evidence, evidenceErr := c.approvalEvidence(reason, challenge, opts.requestID)
		if evidenceErr != nil {
			return c.writeError(opts, evidenceErr)
		}
		revoked, serviceErr := c.service.RevokeApproval(c.cwd, opts.workspace, args[1], evidence, c.now)
		if serviceErr != nil {
			return c.writeError(opts, serviceErr)
		}
		return c.writeSuccess(opts, "ApprovalRevocation", revoked, textApprovalRevocation)
	default:
		return c.writeError(opts, &app.Error{Code: "UPMCTL_NOT_IMPLEMENTED", Message: fmt.Sprintf("approval command %q is not implemented", strings.Join(args, " ")), Remediation: "available approval commands are grant, get, list and revoke", ExitCode: 3})
	}
}

func (c *CLI) requireHumanTTY(_ options) (terminal.HumanTerminal, *app.Error) {
	humanTTY, err := c.openTTY()
	if err != nil {
		return nil, &app.Error{Code: "UPMCTL_HUMAN_TTY_REQUIRED", Message: err.Error(), Remediation: "run this human-only command directly from a local interactive terminal; Skill, MCP, pipes, CI and background jobs cannot perform it", ExitCode: 3}
	}
	return humanTTY, nil
}

func (c *CLI) approvalEvidence(reason, challenge, requestID string) (app.ApprovalEvidence, *app.Error) {
	challengeDigest, err := upmdigest.Sum(challenge)
	if err != nil {
		return app.ApprovalEvidence{}, &app.Error{Code: "UPMCTL_APPROVAL_EVIDENCE_FAILED", Message: err.Error(), ExitCode: 70}
	}
	return app.ApprovalEvidence{
		Reason: reason, Terminal: "/dev/tty", ChallengeDigest: challengeDigest,
		RequestID: requestID, CLIVersion: c.service.Version().Version,
	}, nil
}

func humanInteractionError(operation string, err error) *app.Error {
	return &app.Error{Code: "UPMCTL_HUMAN_INTERACTION_FAILED", Message: fmt.Sprintf("%s: %v", operation, err), Remediation: "rerun the command directly in a local controlling terminal", ExitCode: 3}
}

func grantReasonPrompt(preparation app.ApprovalPreparation) string {
	p := preparation.Plan
	return fmt.Sprintf("Plan: %s\nAction: %s\nTarget: %s\nRisk: %s\nScope: %s\nExpires: %s\nApply remains BLOCKED in Phase 2b2a.\nApproval reason: ", p.PlanID, p.Action, p.Target.Name, p.RiskLevel, p.ApprovalScope, p.ExpiresAt)
}

func revokeReasonPrompt(inspection app.ApprovalInspection) string {
	a := inspection.Approval
	return fmt.Sprintf("Approval: %s\nPlan: %s\nAction: %s\nTarget: %s\nRisk: %s\nCurrent status: %s\nRevocation reason: ", a.ApprovalID, a.PlanID, a.Action, a.Target.Name, a.RiskLevel, inspection.Status)
}

func adoptionReasonPrompt(state managedenv.State) string {
	var builder strings.Builder
	fmt.Fprintf(&builder, "Environment: %s\nWorkspace: %s\nProvider: libvirt\n", state.EnvironmentID, state.Workspace)
	for _, path := range managedenv.SortedFileNames(state.Files) {
		fmt.Fprintf(&builder, "Bound file: %s %s\n", path, state.Files[path])
	}
	for _, name := range managedenv.SortedMachineNames(state.Machines) {
		fmt.Fprintf(&builder, "Machine: %s %s\n", name, state.Machines[name])
	}
	builder.WriteString("Adoption allows future read-only tools to execute the digest-bound Vagrantfile and use the bound kubeconfig.\nAdoption reason: ")
	return builder.String()
}

func parseOptionalPlanID(args []string) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	return parsePlanIDOption(args)
}

func (c *CLI) runPlan(ctx context.Context, opts options, args []string) int {
	if len(args) > 0 && (args[0] == "get" || args[0] == "validate") && len(args) != 2 {
		return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: fmt.Sprintf("usage: upmctl plan %s PLAN_ID", args[0]), ExitCode: 2})
	}
	if len(args) == 2 && (args[0] == "get" || args[0] == "validate") {
		if !planIdentifierPattern.MatchString(args[1]) {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "PLAN_ID must match plan-<64 lowercase hex>", ExitCode: 2})
		}
		if args[0] == "get" {
			inspection, appErr := c.service.GetPlan(c.cwd, opts.workspace, args[1], c.now())
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			return c.writeSuccess(opts, "PlanInspection", inspection, textPlanInspection)
		}
		validation, appErr := c.service.ValidatePlan(c.cwd, opts.workspace, args[1], c.now())
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		if code := c.writeSuccess(opts, "PlanValidation", validation, textPlanValidation); code != 0 {
			return code
		}
		if len(validation.Blockers) > 0 {
			return 3
		}
		return 0
	}
	if len(args) >= 2 && args[0] == "vm" && args[1] == "start" {
		node, err := parseNodeOption(args[2:])
		if err != nil {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: err.Error(), Remediation: "usage: upmctl plan vm start --node k8s-N", ExitCode: 2})
		}
		created, appErr := c.service.PlanVMStart(ctx, c.cwd, opts.workspace, node, c.now())
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		return c.writeSuccess(opts, "Plan", created, textPlan)
	}
	return c.writeError(opts, &app.Error{
		Code: "UPMCTL_NOT_IMPLEMENTED", Message: fmt.Sprintf("plan command %q is not implemented in the current Phase 2b2a release", strings.Join(args, " ")),
		Remediation: "the only available Plan command is: upmctl plan vm start --node NODE", ExitCode: 3,
	})
}

func parsePlanIDOption(args []string) (string, error) {
	var planID string
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--plan-id":
			index++
			if index >= len(args) || planID != "" {
				return "", fmt.Errorf("--plan-id requires exactly one value")
			}
			planID = args[index]
		case strings.HasPrefix(args[index], "--plan-id="):
			if planID != "" {
				return "", fmt.Errorf("--plan-id may be specified only once")
			}
			planID = strings.TrimPrefix(args[index], "--plan-id=")
		default:
			return "", fmt.Errorf("unexpected argument %q", args[index])
		}
	}
	if !planIdentifierPattern.MatchString(planID) {
		return "", fmt.Errorf("--plan-id must match plan-<64 lowercase hex>")
	}
	return planID, nil
}

func parseNodeOption(args []string) (string, error) {
	var node string
	for index := 0; index < len(args); index++ {
		switch {
		case args[index] == "--node":
			index++
			if index >= len(args) || node != "" {
				return "", fmt.Errorf("--node requires exactly one value")
			}
			node = args[index]
		case strings.HasPrefix(args[index], "--node="):
			if node != "" {
				return "", fmt.Errorf("--node may be specified only once")
			}
			node = strings.TrimPrefix(args[index], "--node=")
		default:
			return "", fmt.Errorf("unexpected plan argument %q", args[index])
		}
	}
	if !planNodePattern.MatchString(node) {
		return "", fmt.Errorf("--node must be one of k8s-1 through k8s-8")
	}
	return node, nil
}

func (c *CLI) runVM(ctx context.Context, opts options, args []string) int {
	if len(args) == 0 {
		return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "vm subcommand is required", ExitCode: 2})
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "usage: upmctl vm list", ExitCode: 2})
		}
		list, appErr := c.service.ListVMs(ctx, c.cwd, opts.workspace)
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		return c.writeSuccess(opts, "VMList", list, textVMList)
	case "status":
		if len(args) == 1 && args[0] == "status" {
			list, appErr := c.service.ListVMs(ctx, c.cwd, opts.workspace)
			if appErr != nil {
				return c.writeError(opts, appErr)
			}
			return c.writeSuccess(opts, "VMList", list, textVMList)
		}
		if len(args) != 2 {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "usage: upmctl vm status|inspect NODE", ExitCode: 2})
		}
		machine, appErr := c.service.GetVM(ctx, c.cwd, opts.workspace, args[1])
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		return c.writeSuccess(opts, "VMStatus", machine, textVM)
	case "inspect":
		if len(args) != 2 {
			return c.writeError(opts, &app.Error{Code: "UPMCTL_USAGE", Message: "usage: upmctl vm inspect NODE", ExitCode: 2})
		}
		machine, appErr := c.service.GetVM(ctx, c.cwd, opts.workspace, args[1])
		if appErr != nil {
			return c.writeError(opts, appErr)
		}
		return c.writeSuccess(opts, "VMInspection", machine, textVM)
	default:
		return c.writeError(opts, &app.Error{
			Code:        "UPMCTL_NOT_IMPLEMENTED",
			Message:     fmt.Sprintf("vm subcommand %q is not implemented in the read-only phase", args[0]),
			Remediation: "available VM commands are list, status and inspect",
			ExitCode:    3,
		})
	}
}

func (c *CLI) writeSuccess(opts options, kind string, data any, textWriter func(io.Writer, any) error) int {
	if opts.format == output.Text {
		if err := textWriter(c.stdout, data); err != nil {
			setRunError(opts, "UPMCTL_OUTPUT_WRITE_FAILED")
			return 70
		}
		return 0
	}
	err := output.WriteEnvelope(c.stdout, opts.format, output.Envelope{
		Kind:      kind,
		RequestID: opts.requestID,
		Timestamp: c.now().UTC(),
		Data:      data,
	})
	if err != nil {
		setRunError(opts, "UPMCTL_OUTPUT_WRITE_FAILED")
		return 70
	}
	return 0
}

func (c *CLI) writeError(opts options, appErr *app.Error) int {
	setRunError(opts, appErr.Code)
	if opts.format == "" {
		opts.format = output.Text
	}
	_ = output.WriteError(c.stderr, opts.format, output.ErrorEnvelope{
		Kind:      "Error",
		RequestID: opts.requestID,
		Timestamp: c.now().UTC(),
		Error: output.ErrorBody{
			Code:        appErr.Code,
			Message:     appErr.Message,
			Details:     appErr.Details,
			Remediation: appErr.Remediation,
		},
	})
	if appErr.ExitCode == 0 {
		return 70
	}
	return appErr.ExitCode
}

func setRunError(opts options, code string) {
	if opts.result != nil {
		opts.result.errorCode = code
	}
}

func (c *CLI) writeHelp(opts options, topic string) int {
	if _, err := io.WriteString(c.stdout, helpText(topic)); err != nil {
		setRunError(opts, "UPMCTL_OUTPUT_WRITE_FAILED")
		return 70
	}
	return 0
}

func parseOptions(args []string) (options, []string, error) {
	opts := options{format: output.Text}
	remaining := make([]string, 0, len(args))
	seenOutput := false
	seenWorkspace := false
	seenRequestID := false
	seenTimeout := false
	seenLogFile := false
	seenNoColor := false
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--output":
			if seenOutput {
				return opts, remaining, fmt.Errorf("--output may be specified only once")
			}
			seenOutput = true
			index++
			if index >= len(args) {
				return opts, nil, fmt.Errorf("--output requires a value")
			}
			opts.format = output.Format(args[index])
		case strings.HasPrefix(argument, "--output="):
			if seenOutput {
				return opts, remaining, fmt.Errorf("--output may be specified only once")
			}
			seenOutput = true
			opts.format = output.Format(strings.TrimPrefix(argument, "--output="))
		case argument == "--workspace":
			if seenWorkspace {
				return opts, remaining, fmt.Errorf("--workspace may be specified only once")
			}
			seenWorkspace = true
			index++
			if index >= len(args) {
				return opts, nil, fmt.Errorf("--workspace requires a value")
			}
			opts.workspace = args[index]
		case strings.HasPrefix(argument, "--workspace="):
			if seenWorkspace {
				return opts, remaining, fmt.Errorf("--workspace may be specified only once")
			}
			seenWorkspace = true
			opts.workspace = strings.TrimPrefix(argument, "--workspace=")
		case argument == "--request-id":
			if seenRequestID {
				return opts, remaining, fmt.Errorf("--request-id may be specified only once")
			}
			seenRequestID = true
			index++
			if index >= len(args) {
				return opts, nil, fmt.Errorf("--request-id requires a value")
			}
			opts.requestID = args[index]
		case strings.HasPrefix(argument, "--request-id="):
			if seenRequestID {
				return opts, remaining, fmt.Errorf("--request-id may be specified only once")
			}
			seenRequestID = true
			opts.requestID = strings.TrimPrefix(argument, "--request-id=")
		case argument == "--timeout":
			if seenTimeout {
				return opts, remaining, fmt.Errorf("--timeout may be specified only once")
			}
			seenTimeout = true
			index++
			if index >= len(args) {
				return opts, nil, fmt.Errorf("--timeout requires a value")
			}
			duration, err := time.ParseDuration(args[index])
			if err != nil {
				return opts, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			if duration <= 0 {
				return opts, nil, fmt.Errorf("--timeout must be greater than zero")
			}
			opts.timeout = duration
		case strings.HasPrefix(argument, "--timeout="):
			if seenTimeout {
				return opts, remaining, fmt.Errorf("--timeout may be specified only once")
			}
			seenTimeout = true
			duration, err := time.ParseDuration(strings.TrimPrefix(argument, "--timeout="))
			if err != nil {
				return opts, nil, fmt.Errorf("invalid --timeout: %w", err)
			}
			if duration <= 0 {
				return opts, nil, fmt.Errorf("--timeout must be greater than zero")
			}
			opts.timeout = duration
		case argument == "--log-file":
			if seenLogFile {
				return opts, remaining, fmt.Errorf("--log-file may be specified only once")
			}
			seenLogFile = true
			index++
			if index >= len(args) {
				return opts, nil, fmt.Errorf("--log-file requires a value")
			}
			opts.logFile = args[index]
		case strings.HasPrefix(argument, "--log-file="):
			if seenLogFile {
				return opts, remaining, fmt.Errorf("--log-file may be specified only once")
			}
			seenLogFile = true
			opts.logFile = strings.TrimPrefix(argument, "--log-file=")
			if opts.logFile == "" {
				return opts, nil, fmt.Errorf("--log-file requires a value")
			}
		case argument == "--no-color":
			if seenNoColor {
				return opts, remaining, fmt.Errorf("--no-color may be specified only once")
			}
			seenNoColor = true
			// Text output is intentionally color-free in the initial contract.
		default:
			remaining = append(remaining, argument)
		}
	}
	if opts.format != output.Text && opts.format != output.JSON && opts.format != output.JSONL {
		return opts, remaining, fmt.Errorf("unsupported output format %q", opts.format)
	}
	if seenWorkspace && strings.TrimSpace(opts.workspace) == "" {
		return opts, remaining, fmt.Errorf("--workspace requires a non-empty value")
	}
	if seenRequestID && strings.TrimSpace(opts.requestID) == "" {
		return opts, remaining, fmt.Errorf("--request-id requires a non-empty value")
	}
	if seenLogFile && strings.TrimSpace(opts.logFile) == "" {
		return opts, remaining, fmt.Errorf("--log-file requires a non-empty value")
	}
	return opts, remaining, nil
}

// canonicalCommand returns only a fixed command path. It never copies option
// values or unknown arguments into the runtime log.
func canonicalCommand(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	switch args[0] {
	case "help", "--help", "-h":
		if len(args) == 1 {
			return "help"
		}
		if args[0] == "help" && len(args) == 2 && knownHelpTopic(args[1]) {
			return "help " + args[1]
		}
	case "version", "capabilities", "status", "preflight", "apply", "verify":
		return args[0]
	case "environment":
		if len(args) > 1 && args[1] == "adopt" {
			return "environment adopt"
		}
	case "context":
		if len(args) > 1 && args[1] == "discover" {
			return "context discover"
		}
	case "config":
		if len(args) > 1 && args[1] == "validate" {
			return "config validate"
		}
	case "vm":
		if len(args) > 1 {
			switch args[1] {
			case "list", "status", "inspect", "ssh":
				return "vm " + args[1]
			}
		}
	case "node":
		if len(args) > 1 && (args[1] == "list" || args[1] == "status") {
			return "node " + args[1]
		}
	case "plan":
		if len(args) > 1 {
			if args[1] == "get" || args[1] == "validate" {
				return "plan " + args[1]
			}
			if len(args) > 2 && knownPlanAction(args[1], args[2]) {
				return "plan " + args[1] + " " + args[2]
			}
		}
	case "approval":
		if len(args) > 1 {
			switch args[1] {
			case "grant", "get", "list", "revoke":
				return "approval " + args[1]
			}
		}
	case "operation":
		if len(args) > 1 {
			switch args[1] {
			case "get", "cancel", "resume":
				return "operation " + args[1]
			}
		}
	case "report":
		if len(args) > 1 && args[1] == "generate" {
			return "report generate"
		}
	}
	return "unknown"
}

func knownPlanAction(group, action string) bool {
	switch group {
	case "vm":
		return action == "start" || action == "stop" || action == "restart"
	case "cluster":
		return action == "deploy" || action == "start" || action == "stop" || action == "restart" || action == "destroy"
	case "node":
		return action == "add" || action == "remove"
	case "addon":
		return action == "install"
	default:
		return false
	}
}

func requestID(now time.Time) string {
	random := make([]byte, 4)
	if _, err := rand.Read(random); err != nil {
		return fmt.Sprintf("req-%d", now.UnixNano())
	}
	return fmt.Sprintf("req-%d-%s", now.UnixNano(), hex.EncodeToString(random))
}

func textVersion(writer io.Writer, value any) error {
	info := value.(buildinfo.Info)
	_, err := fmt.Fprintf(
		writer,
		"Version:  %s\nCommit:   %s\nBuilt:    %s\nGo:       %s\nPlatform: %s\nAPI:      %s\n",
		info.Version,
		info.GitCommit,
		info.BuildDate,
		info.GoVersion,
		info.Platform,
		info.APIVersion,
	)
	return err
}

func textCapabilities(writer io.Writer, value any) error {
	capabilities := value.(app.Capabilities)
	if _, err := fmt.Fprintf(writer, "Phase: %s\n", capabilities.Phase); err != nil {
		return err
	}
	for _, capability := range capabilities.Capabilities {
		state := "unavailable"
		if capability.Available {
			state = "available"
		}
		if _, err := fmt.Fprintf(writer, "%-20s %-11s %s\n", capability.Name, state, capability.Description); err != nil {
			return err
		}
	}
	return nil
}

func textContext(writer io.Writer, value any) error {
	deployment := value.(upmcontext.Deployment)
	_, err := fmt.Fprintf(writer, "Repository: %s\nWorkspace:  %s\nManaged:    %t\nSource:     %s\n", deployment.RepositoryRoot, deployment.Workspace, deployment.Managed, deployment.Source)
	if err != nil {
		return err
	}
	for _, finding := range deployment.Findings {
		if _, err := fmt.Fprintf(writer, "Finding:    %s\n", finding); err != nil {
			return err
		}
	}
	return nil
}

func textManagedEnvironment(writer io.Writer, value any) error {
	state := value.(managedenv.State)
	if _, err := fmt.Fprintf(writer, "Environment: %s\nWorkspace:   %s\nState:       %s\nProvider:    libvirt\nMachines:    %d\n", state.EnvironmentID, state.Workspace, filepath.Join(state.Workspace, ".upmctl", "state.json"), len(state.Machines)); err != nil {
		return err
	}
	for _, name := range managedenv.SortedMachineNames(state.Machines) {
		if _, err := fmt.Fprintf(writer, "Machine:     %s %s\n", name, state.Machines[name]); err != nil {
			return err
		}
	}
	return nil
}

func textVMList(writer io.Writer, value any) error {
	list := value.(vm.List)
	if _, err := fmt.Fprintf(writer, "Workspace: %s\n", list.Workspace); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(writer, "NAME\tROLE\tHEALTH\tVAGRANT\tLIBVIRT\tKUBERNETES"); err != nil {
		return err
	}
	for _, machine := range list.Machines {
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", machine.Name, machine.Role, machine.Health, machine.VagrantState, machine.LibvirtState, machine.KubernetesState); err != nil {
			return err
		}
	}
	for _, finding := range list.Findings {
		if _, err := fmt.Fprintf(writer, "Finding [%s]: %s\n", finding.Code, finding.Message); err != nil {
			return err
		}
	}
	return nil
}

func textVM(writer io.Writer, value any) error {
	machine := value.(vm.Machine)
	_, err := fmt.Fprintf(writer, "Name:        %s\nExpected:    %t\nRole:        %s\nHealth:      %s\nConsistency: %s\nVagrant:     %s\nLibvirt ID:  %s\nDomain:      %s\nLibvirt:     %s\nKubernetes:  %s\nInternal IP: %s\nSSH:         %s:%d (%s)\nResources:   %d CPU / %d MiB / %d data disks\n", machine.Name, machine.Expected, machine.Role, machine.Health, machine.Consistency, machine.VagrantState, machine.LibvirtID, machine.Identity.DomainName, machine.LibvirtState, machine.KubernetesState, machine.Network.InternalIP, machine.Network.SSHHost, machine.Network.SSHPort, machine.Network.SSHState, machine.Resources.CPU, machine.Resources.MemoryMiB, machine.Resources.DataDisks)
	if err != nil {
		return err
	}
	for _, finding := range machine.Findings {
		if _, err := fmt.Fprintf(writer, "Finding [%s]: %s\n", finding.Code, finding.Message); err != nil {
			return err
		}
	}
	return nil
}

func textPlan(writer io.Writer, value any) error {
	created := value.(upmplan.Plan)
	persisted := created.Disposition == upmplan.DispositionActionRequired
	if _, err := fmt.Fprintf(writer, "Plan ID:     %s\nAction:      %s\nTarget:      %s\nDisposition: %s\nRisk:        %s\nCreated:     %s\nExpires:     %s\nDigest:      %s\nPersisted:   %t\n", created.PlanID, created.Action, created.Target.Name, created.Disposition, created.RiskLevel, created.CreatedAt, created.ExpiresAt, created.PlanDigest, persisted); err != nil {
		return err
	}
	for _, blocker := range created.Blockers {
		if _, err := fmt.Fprintf(writer, "Blocker:     %s\n", blocker); err != nil {
			return err
		}
	}
	for _, step := range created.Steps {
		if _, err := fmt.Fprintf(writer, "Step:        %s %s %s\n", step.ID, step.Code, step.Resource); err != nil {
			return err
		}
	}
	return nil
}

func textPlanInspection(writer io.Writer, value any) error {
	inspection := value.(readiness.PlanInspection)
	if err := textPlan(writer, inspection.Plan); err != nil {
		return err
	}
	_, err := fmt.Fprintf(writer, "Expired:     %t\nExecutable:  %t\nChecked:     %s\n", inspection.Expired, inspection.ExecutionAvailable, inspection.CheckedAt)
	return err
}

func textPlanValidation(writer io.Writer, value any) error {
	validation := value.(readiness.PlanValidation)
	if _, err := fmt.Fprintf(writer, "Plan ID:       %s\nArtifact:      %s\nFreshness:     %s\nEnvironment:   %s\nConfig:        %s\nManaged State: %s\nObserved State: %s\nExecutable:    %t\nChecked:       %s\n", validation.PlanID, validation.ArtifactStatus, validation.FreshnessStatus, validation.EnvironmentBinding, validation.ConfigBinding, validation.ManagedStateBinding, validation.ObservedStateBinding, validation.ExecutionAvailable, validation.CheckedAt); err != nil {
		return err
	}
	for _, blocker := range validation.Blockers {
		if _, err := fmt.Fprintf(writer, "Blocker:       %s\n", blocker); err != nil {
			return err
		}
	}
	return nil
}

func textPreflight(writer io.Writer, value any) error {
	result := value.(readiness.PreflightResult)
	if _, err := fmt.Fprintf(writer, "Plan ID:      %s\nPreflight:    %s\nApply:        %s\nExecutable:   %t\nApproval:     %s\nChecked:      %s\n", result.PlanID, result.PreflightStatus, result.ApplyDecision, result.ExecutionAvailable, result.ApprovalStatus, result.CheckedAt); err != nil {
		return err
	}
	for _, check := range result.Checks {
		if _, err := fmt.Fprintf(writer, "Check:         %-24s %-7s %s\n", check.ID, check.Status, check.Code); err != nil {
			return err
		}
	}
	for _, blocker := range result.Blockers {
		if _, err := fmt.Fprintf(writer, "Blocker:       %s\n", blocker); err != nil {
			return err
		}
	}
	return nil
}

func textApproval(writer io.Writer, value any) error {
	item := value.(approval.Approval)
	_, err := fmt.Fprintf(writer, "Approval ID: %s\nPlan ID:     %s\nDecision:    %s\nAction:      %s\nTarget:      %s\nRisk:        %s\nScope:       %s\nApproved:    %s\nExpires:     %s\nApprover:    %s (%s)\nReason:      %s\nExecutable:  false\n", item.ApprovalID, item.PlanID, item.Decision, item.Action, item.Target.Name, item.RiskLevel, item.ApprovalScope, item.ApprovedAt, item.ExpiresAt, item.Approver.Username, item.Approver.Subject, item.Reason)
	return err
}

func textApprovalInspection(writer io.Writer, value any) error {
	inspection := value.(app.ApprovalInspection)
	if err := textApproval(writer, inspection.Approval); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "Status:       %s\n", inspection.Status); err != nil {
		return err
	}
	if inspection.Revocation != nil {
		_, err := fmt.Fprintf(writer, "Revoked:      %s\nRevocation:   %s\n", inspection.Revocation.RevokedAt, inspection.Revocation.RevocationID)
		return err
	}
	return nil
}

func textApprovalList(writer io.Writer, value any) error {
	list := value.(approvalList)
	if _, err := fmt.Fprintln(writer, "APPROVAL ID\tPLAN ID\tSTATUS\tRISK\tTARGET\tEXPIRES"); err != nil {
		return err
	}
	for _, inspection := range list.Items {
		a := inspection.Approval
		if _, err := fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ApprovalID, a.PlanID, inspection.Status, a.RiskLevel, a.Target.Name, a.ExpiresAt); err != nil {
			return err
		}
	}
	return nil
}

func textApprovalRevocation(writer io.Writer, value any) error {
	item := value.(admission.ApprovalRevocation)
	_, err := fmt.Fprintf(writer, "Revocation ID: %s\nApproval ID:   %s\nPlan ID:       %s\nDisposition:   %s\nRevoked:       %s\nActor:         %s (%s)\nReason:        %s\nExecutable:    false\n", item.RevocationID, item.ApprovalID, item.PlanID, item.Disposition, item.RevokedAt, item.Actor.Username, item.Actor.Subject, item.Reason)
	return err
}

func textConfigValidation(writer io.Writer, value any) error {
	validation := value.(app.ConfigValidation)
	result := validation.Validation
	if _, err := fmt.Fprintf(writer, "Config:     %s\nStatus:     %s\nSafe:       %t\nComplete:   %t\nValid:      %t\nExecutable: %t\nDigest:     %s\n", result.Path, result.Status, result.Safe, result.Complete, result.Valid, validation.Executable, result.Digest); err != nil {
		return err
	}
	if result.Config.NodeCount > 0 {
		if _, err := fmt.Fprintf(writer, "Topology:   %s-1..%s-%d\nNetwork:    %s / %s\nKubernetes: %s\nResources:  %d CPU / %d MiB\n", result.Config.Prefix, result.Config.Prefix, result.Config.NodeCount, result.Config.Network.Mode, result.Config.NetworkPlugin, result.Config.KubernetesVersion, result.Config.Resources.TotalCPU, result.Config.Resources.TotalMemoryMiB); err != nil {
			return err
		}
	}
	for _, finding := range result.Findings {
		if _, err := fmt.Fprintf(writer, "%s [%s] %s: %s\n", strings.ToUpper(finding.Severity), finding.Code, finding.Field, finding.Message); err != nil {
			return err
		}
	}
	return nil
}

func textStatus(writer io.Writer, value any) error {
	status := value.(upmstatus.Environment)
	if _, err := fmt.Fprintf(writer, "Mode:         %s\nHealth:       %s\nComplete:     %t\nWorkspace:    %s\nConfig:       %s\nAPI:          %s\nExpected VMs: %d\nHealthy:      %d\nDegraded:     %d\nStopped:      %d\nMissing:      %d\nOrphaned:     %d\nInconsistent: %d\nUnknown:      %d\n", status.Mode, status.Health, status.ObservationComplete, status.Workspace, status.Config.Status, status.Cluster.APIState, status.VMSummary.Expected, status.VMSummary.Healthy, status.VMSummary.Degraded, status.VMSummary.Stopped, status.VMSummary.Missing, status.VMSummary.Orphaned, status.VMSummary.Inconsistent, status.VMSummary.Unknown); err != nil {
		return err
	}
	for _, finding := range status.Findings {
		if _, err := fmt.Fprintf(writer, "%s [%s] %s\n", strings.ToUpper(finding.Severity), finding.Code, finding.Message); err != nil {
			return err
		}
	}
	return nil
}
