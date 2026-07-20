package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/buildinfo"
	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	upmdigest "github.com/upmio/kubespray-upm/upmctl/internal/digest"
	"github.com/upmio/kubespray-upm/upmctl/internal/managedenv"
	upmplan "github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/readiness"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
	upmstatus "github.com/upmio/kubespray-upm/upmctl/internal/status"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

type Capability struct {
	Name        string `json:"name"`
	Available   bool   `json:"available"`
	Description string `json:"description"`
}

type Capabilities struct {
	Phase        string       `json:"phase"`
	Capabilities []Capability `json:"capabilities"`
}

type Error struct {
	Code        string
	Message     string
	Details     map[string]any
	Remediation string
	ExitCode    int
}

func (e *Error) Error() string { return e.Message }

type Service struct {
	vm *vm.Service
}

type ConfigValidation struct {
	Context    upmcontext.Deployment `json:"context"`
	Validation upmconfig.Result      `json:"validation"`
	Executable bool                  `json:"executable"`
}

func New(commandRunner runner.Runner) *Service {
	return &Service{vm: vm.NewService(commandRunner)}
}

func (s *Service) Version() buildinfo.Info {
	return buildinfo.Current()
}

func (s *Service) Capabilities() Capabilities {
	return Capabilities{
		Phase: "phase-2b2a-human-approval",
		Capabilities: []Capability{
			{Name: "environment.adopt", Available: true, Description: "safely adopt an existing legacy libvirt Vagrant workspace without executing it"},
			{Name: "context.discover", Available: true, Description: "discover repository and deployment workspace"},
			{Name: "config.validate", Available: true, Description: "parse and validate the supported config.rb subset without executing Ruby"},
			{Name: "status", Available: true, Description: "aggregate context, config and managed VM observed state"},
			{Name: "vm.observe.basic", Available: true, Description: "list and query VM state for digest-bound managed workspaces"},
			{Name: "vm.observe.full", Available: true, Description: "correlate topology, identity, IP, resources, disks and SSH endpoint metadata"},
			{Name: "vm.inspect", Available: true, Description: "return the complete Phase 1b read-only VM inspection"},
			{Name: "plan.vm.start", Available: true, Description: "create an immutable, non-executable VM start plan"},
			{Name: "plan.get", Available: true, Description: "read and inspect an immutable persisted Plan"},
			{Name: "plan.validate", Available: true, Description: "validate Plan integrity, expiry and local workspace bindings"},
			{Name: "preflight.plan", Available: true, Description: "re-observe read-only state and report Apply readiness without enabling Apply"},
			{Name: "plan.apply", Available: false, Description: "locking, claiming and execution are not available in Phase 2b2a"},
			{Name: "approval.manage", Available: true, Description: "prepare, grant, inspect, list and revoke immutable local human approval evidence"},
			{Name: "operation.manage", Available: false, Description: "planned for a later Phase 2b2 increment"},
			{Name: "executor.vm.start", Available: false, Description: "the mutation executor remains unavailable in Phase 2b2a"},
			{Name: "vm.mutate", Available: false, Description: "planned for a later phase"},
			{Name: "cluster.deploy", Available: false, Description: "planned for a later phase"},
			{Name: "node.scale", Available: false, Description: "planned for a later phase"},
			{Name: "addon.manage", Available: false, Description: "planned for a later phase"},
			{Name: "mcp.server", Available: false, Description: "V1 reserves the adapter contract only"},
		},
	}
}

type EnvironmentAdoptionEvidence struct {
	Reason          string
	Terminal        string
	ChallengeDigest string
	RequestID       string
	CLIVersion      string
}

func (s *Service) PrepareEnvironmentAdoption(cwd, workspace, environmentID string) (managedenv.State, *Error) {
	if workspace == "" {
		return managedenv.State{}, &Error{Code: "UPMCTL_WORKSPACE_REQUIRED", Message: "environment adopt requires an explicit --workspace", Remediation: "pass the existing legacy deployment workspace with --workspace PATH", ExitCode: 2}
	}
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return managedenv.State{}, appErr
	}
	state, err := managedenv.Prepare(deployment.Workspace, environmentID)
	if err != nil {
		return managedenv.State{}, managedEnvironmentError(err, deployment.Workspace)
	}
	return state, nil
}

func (s *Service) AdoptEnvironment(cwd, workspace, environmentID string, evidence EnvironmentAdoptionEvidence, now time.Time) (managedenv.State, *Error) {
	state, appErr := s.PrepareEnvironmentAdoption(cwd, workspace, environmentID)
	if appErr != nil {
		return managedenv.State{}, appErr
	}
	actor, actorErr := observeLocalActor()
	if actorErr != nil {
		return managedenv.State{}, actorErr
	}
	state, err := managedenv.BindAdoption(state, managedenv.ActorObservation{
		Subject: actor.Subject, UID: actor.UID, Username: actor.Username, Hostname: actor.Hostname,
	}, managedenv.PresenceObservation{
		Terminal: evidence.Terminal, ChallengeDigest: evidence.ChallengeDigest,
	}, evidence.Reason, evidence.RequestID, evidence.CLIVersion, now)
	if err != nil {
		return managedenv.State{}, &Error{Code: "UPMCTL_ADOPTION_EVIDENCE_INVALID", Message: err.Error(), Remediation: "rerun adoption directly from a local controlling terminal", ExitCode: 3}
	}
	store := managedenv.NewStore(state.Workspace)
	if _, err := store.Save(state); err != nil {
		return managedenv.State{}, managedEnvironmentError(err, state.Workspace)
	}
	finalDeployment, err := upmcontext.Discover(cwd, state.Workspace)
	if err != nil || !finalDeployment.Managed || finalDeployment.Trust != upmcontext.TrustManagedValid || finalDeployment.EnvironmentID != environmentID {
		message := "published managed environment identity did not pass strict readback validation"
		if err != nil {
			message = err.Error()
		}
		_ = store.Rollback(state)
		return managedenv.State{}, &Error{Code: "UPMCTL_MANAGED_STATE_STORE_FAILED", Message: message, Details: map[string]any{"workspace": state.Workspace}, Remediation: "do not execute the workspace; inspect .upmctl/state.json and its bound source files", ExitCode: 70}
	}
	return state, nil
}

func managedEnvironmentError(err error, workspace string) *Error {
	failure := managedenv.FailureOf(err)
	details := map[string]any{"workspace": workspace}
	switch failure.Code {
	case managedenv.FailureInvalidEnvironmentID:
		return &Error{Code: "UPMCTL_ENVIRONMENT_ID_INVALID", Message: failure.Reason, Details: details, Remediation: "use an ID such as env-lab-01", ExitCode: 2}
	case managedenv.FailureAlreadyControlled, managedenv.FailureStateExists:
		return &Error{Code: "UPMCTL_ENVIRONMENT_ALREADY_CONTROLLED", Message: failure.Reason, Details: details, Remediation: "inspect the existing .upmctl state; adoption never overwrites or merges control-state", ExitCode: 3}
	case managedenv.FailureConfigInvalid:
		return &Error{Code: "UPMCTL_CONFIG_INVALID", Message: failure.Reason, Details: details, Remediation: "make config.rb pass safe, complete config validation before adoption", ExitCode: 3}
	case managedenv.FailureUnsupportedProvider:
		return &Error{Code: "UPMCTL_PROVIDER_UNSUPPORTED", Message: failure.Reason, Details: details, Remediation: "adopt only a workspace whose expected machines have libvirt Vagrant metadata and no other provider metadata", ExitCode: 3}
	case managedenv.FailureMetadataInvalid:
		return &Error{Code: "UPMCTL_VAGRANT_METADATA_INVALID", Message: failure.Reason, Details: details, Remediation: "repair missing, unknown, unsafe, invalid, or duplicate Vagrant libvirt machine identities", ExitCode: 3}
	case managedenv.FailureUnsafeWorkspace:
		return &Error{Code: "UPMCTL_WORKSPACE_UNSAFE", Message: failure.Reason, Details: details, Remediation: "use a real local workspace with bounded regular files and no symlinked identity paths", ExitCode: 3}
	default:
		return &Error{Code: "UPMCTL_MANAGED_STATE_STORE_FAILED", Message: failure.Reason, Details: details, Remediation: "inspect workspace ownership, .upmctl path identity, permissions, and free space", ExitCode: 70}
	}
}

func (s *Service) GetPlan(cwd, workspace, planID string, now time.Time) (readiness.PlanInspection, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return readiness.PlanInspection{}, appErr
	}
	if deployment.Workspace == "" {
		return readiness.PlanInspection{}, workspaceNotFound(deployment)
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return readiness.PlanInspection{}, appErr
	}
	inspection, err := readiness.BuildInspection(candidate, now)
	if err != nil {
		return readiness.PlanInspection{}, invalidPlanError(planID, err)
	}
	return inspection, nil
}

func (s *Service) ValidatePlan(cwd, workspace, planID string, now time.Time) (readiness.PlanValidation, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return readiness.PlanValidation{}, appErr
	}
	if deployment.Workspace == "" {
		return readiness.PlanValidation{}, workspaceNotFound(deployment)
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return readiness.PlanValidation{}, appErr
	}
	current := localCurrentState(deployment)
	return readiness.BuildValidation(readiness.ValidationInput{Plan: candidate, Now: now, Current: current}), nil
}

func (s *Service) PreflightPlan(ctx context.Context, cwd, workspace, planID string, clock func() time.Time) (readiness.PreflightResult, *Error) {
	if clock == nil {
		clock = time.Now
	}
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return readiness.PreflightResult{}, appErr
	}
	if deployment.Workspace == "" {
		return readiness.PreflightResult{}, workspaceNotFound(deployment)
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return readiness.PreflightResult{}, appErr
	}
	current := localCurrentState(deployment)
	validation := upmconfig.Result{}
	if deployment.ConfigFile != "" {
		validation = upmconfig.ParseFile(deployment.ConfigFile)
	}
	if deployment.Managed && deployment.Trust == upmcontext.TrustManagedValid && validation.Safe && validation.Valid && validation.Complete {
		observed, err := s.vm.List(ctx, deployment, validation.Config)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return readiness.PreflightResult{}, &Error{Code: "UPMCTL_INTERRUPTED", Message: err.Error(), Remediation: "rerun preflight with a longer --timeout", ExitCode: 6}
			}
			return readiness.PreflightResult{}, &Error{Code: "UPMCTL_VM_OBSERVE_FAILED", Message: err.Error(), Remediation: "restore read-only observation before rerunning preflight", ExitCode: 4}
		}
		observedDigest, err := upmplan.ObservedDigest(observed)
		if err != nil {
			return readiness.PreflightResult{}, &Error{Code: "UPMCTL_PREFLIGHT_FAILED", Message: err.Error(), ExitCode: 70}
		}
		current.ObservedStateDigest = observedDigest
		current.ObservedStateStatus = readiness.StateValid
		current.ObservedStateSafe = observedStateSafe(observed)
	}

	// Re-read all local bindings and the immutable Plan after observation so a
	// concurrent local edit cannot pass on values captured before the check.
	finalDeployment, finalErr := s.DiscoverContext(cwd, workspace)
	if finalErr != nil {
		return readiness.PreflightResult{}, finalErr
	}
	finalLocal := localCurrentState(finalDeployment)
	current.EnvironmentID = finalLocal.EnvironmentID
	current.ConfigDigest, current.ConfigStatus = finalLocal.ConfigDigest, finalLocal.ConfigStatus
	current.ManagedStateDigest, current.ManagedStateStatus = finalLocal.ManagedStateDigest, finalLocal.ManagedStateStatus
	finalPlan, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return readiness.PreflightResult{}, appErr
	}
	if !reflect.DeepEqual(finalPlan, candidate) {
		return readiness.PreflightResult{}, invalidPlanError(planID, fmt.Errorf("Plan changed while preflight was observing the environment"))
	}
	checkedAt := clock()
	approvalStatus, _, approvalErr := inspectApprovalState(finalDeployment.Workspace, finalPlan, checkedAt)
	if approvalErr != nil {
		return readiness.PreflightResult{}, approvalErr
	}
	return readiness.BuildPreflight(readiness.PreflightInput{
		Plan: candidate, Now: checkedAt, Current: current, ApprovalStatus: approvalStatus,
	}), nil
}

func readStoredPlan(workspace, planID string) (upmplan.Plan, *Error) {
	candidate, err := upmplan.NewStore(workspace).Read(planID)
	if err == nil {
		if contractErr := upmplan.ValidateActionContract(candidate); contractErr != nil {
			return upmplan.Plan{}, invalidPlanError(planID, contractErr)
		}
		return candidate, nil
	}
	switch {
	case errors.Is(err, os.ErrNotExist):
		return upmplan.Plan{}, &Error{Code: "UPMCTL_PLAN_NOT_FOUND", Message: fmt.Sprintf("Plan %q was not found", planID), Details: map[string]any{"planId": planID}, Remediation: "generate a new ACTION_REQUIRED Plan in this workspace", ExitCode: 3}
	case errors.Is(err, upmplan.ErrUnsafeStore):
		return upmplan.Plan{}, &Error{Code: "UPMCTL_PLAN_STORE_UNSAFE", Message: err.Error(), Details: map[string]any{"planId": planID}, Remediation: "repair private .upmctl/plans ownership, permissions and path identity", ExitCode: 3}
	default:
		return upmplan.Plan{}, invalidPlanError(planID, err)
	}
}

func invalidPlanError(planID string, err error) *Error {
	return &Error{Code: "UPMCTL_PLAN_INVALID", Message: err.Error(), Details: map[string]any{"planId": planID}, Remediation: "discard the damaged Plan and generate a new one", ExitCode: 3}
}

func workspaceNotFound(deployment upmcontext.Deployment) *Error {
	return &Error{Code: "UPMCTL_WORKSPACE_NOT_FOUND", Message: "deployment workspace was not found", Details: map[string]any{"repositoryRoot": deployment.RepositoryRoot}, Remediation: "pass --workspace for the managed deployment", ExitCode: 3}
}

func localCurrentState(deployment upmcontext.Deployment) readiness.CurrentState {
	current := readiness.CurrentState{EnvironmentID: deployment.EnvironmentID, ConfigStatus: readiness.StateUnavailable, ManagedStateStatus: readiness.StateUnavailable, ObservedStateStatus: readiness.StateUnavailable}
	if deployment.ConfigFile != "" {
		validation := upmconfig.ParseFile(deployment.ConfigFile)
		if validation.Safe && validation.Valid && validation.Complete {
			current.ConfigDigest, current.ConfigStatus = validation.Digest, readiness.StateValid
		} else {
			current.ConfigStatus = readiness.StateInvalid
		}
	}
	if deployment.StateFile != "" {
		managedDigest, err := managedStateDigest(deployment.StateFile)
		if err == nil {
			current.ManagedStateDigest, current.ManagedStateStatus = managedDigest, readiness.StateValid
		} else if _, statErr := os.Lstat(deployment.StateFile); errors.Is(statErr, os.ErrNotExist) {
			current.ManagedStateStatus = readiness.StateUnavailable
		} else {
			current.ManagedStateStatus = readiness.StateInvalid
		}
	}
	return current
}

func observedStateSafe(observed vm.List) bool {
	if observed.Sources.Vagrant != "observed" || observed.Sources.Libvirt != "observed" || observed.Sources.Kubernetes != "observed" || observed.Sources.KubernetesAPI != "reachable" {
		return false
	}
	for _, finding := range observed.Findings {
		if strings.Contains(finding.Code, "UNEXPECTED") || strings.Contains(finding.Code, "DUPLICATE") || strings.Contains(finding.Code, "INVALID") || strings.Contains(finding.Code, "UNAVAILABLE") {
			return false
		}
	}
	for _, machine := range observed.Machines {
		switch machine.Health {
		case "ORPHANED", "INCONSISTENT", "MISSING", "UNKNOWN":
			return false
		}
	}
	return true
}

func (s *Service) PlanVMStart(ctx context.Context, cwd, workspace, node string, now time.Time) (upmplan.Plan, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return upmplan.Plan{}, appErr
	}
	if !deployment.Managed || deployment.Trust != upmcontext.TrustManagedValid {
		return upmplan.Plan{}, &Error{
			Code: "UPMCTL_WORKSPACE_UNTRUSTED", Message: "Plan generation requires a digest-bound Managed Environment",
			Details:     map[string]any{"workspace": deployment.Workspace, "trust": deployment.Trust},
			Remediation: "migrate the workspace to a valid .upmctl/state.json before generating plans", ExitCode: 3,
		}
	}
	validation := upmconfig.ParseFile(deployment.ConfigFile)
	if !validation.Safe || !validation.Valid || !validation.Complete {
		return upmplan.Plan{}, &Error{
			Code: "UPMCTL_CONFIG_INVALID", Message: "config.rb is not safe and complete for Plan generation",
			Details:     map[string]any{"status": validation.Status, "findings": validation.Findings},
			Remediation: "fix config validation findings before generating a Plan", ExitCode: 3,
		}
	}
	observed, err := s.vm.List(ctx, deployment, validation.Config)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return upmplan.Plan{}, &Error{Code: "UPMCTL_INTERRUPTED", Message: err.Error(), Remediation: "rerun Plan generation with a longer --timeout", ExitCode: 6}
		}
		return upmplan.Plan{}, &Error{Code: "UPMCTL_VM_OBSERVE_FAILED", Message: err.Error(), Remediation: "restore read-only Vagrant, libvirt and Kubernetes observation before generating a Plan", ExitCode: 4}
	}
	observedDigest, err := upmplan.ObservedDigest(observed)
	if err != nil {
		return upmplan.Plan{}, &Error{Code: "UPMCTL_PLAN_FAILED", Message: err.Error(), ExitCode: 70}
	}
	managedDigest, err := managedStateDigest(deployment.StateFile)
	if err != nil {
		return upmplan.Plan{}, &Error{Code: "UPMCTL_MANAGED_STATE_INVALID", Message: err.Error(), Remediation: "rediscover and repair the Managed Environment identity", ExitCode: 3}
	}
	created, err := upmplan.NewVMStart(upmplan.VMStartInput{
		EnvironmentID: deployment.EnvironmentID, ConfigDigest: validation.Digest,
		ManagedStateDigest: managedDigest, ObservedStateDigest: observedDigest,
		Observed: observed, Node: node, Now: now, TTL: upmplan.DefaultTTL,
	})
	if err != nil {
		return upmplan.Plan{}, &Error{Code: "UPMCTL_PLAN_FAILED", Message: err.Error(), ExitCode: 70}
	}
	if created.Disposition == upmplan.DispositionActionRequired {
		if _, err := upmplan.NewStore(deployment.Workspace).Save(created); err != nil {
			return upmplan.Plan{}, &Error{Code: "UPMCTL_PLAN_STORE_FAILED", Message: err.Error(), Remediation: "inspect the private .upmctl/plans control-state directory", ExitCode: 4}
		}
	}
	return created, nil
}

func managedStateDigest(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 1<<20 {
		return "", fmt.Errorf("managed state is not a safe regular file")
	}
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("open managed state: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return "", fmt.Errorf("managed state identity changed while reading")
	}
	contents, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
	if err != nil {
		return "", fmt.Errorf("read managed state: %w", err)
	}
	if len(contents) > 1<<20 {
		return "", fmt.Errorf("managed state exceeds maximum size")
	}
	var document json.RawMessage = contents
	value, err := upmdigest.Sum(document)
	if err != nil {
		return "", fmt.Errorf("digest managed state: %w", err)
	}
	return value, nil
}

func (s *Service) DiscoverContext(cwd, workspace string) (upmcontext.Deployment, *Error) {
	deployment, err := upmcontext.Discover(cwd, workspace)
	if err != nil {
		return upmcontext.Deployment{}, &Error{
			Code:        "UPMCTL_CONTEXT_NOT_FOUND",
			Message:     err.Error(),
			Remediation: "pass --workspace or run from the kubespray-upm repository",
			ExitCode:    3,
		}
	}
	return deployment, nil
}

func (s *Service) ValidateConfig(cwd, workspace string) (ConfigValidation, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return ConfigValidation{}, appErr
	}
	if deployment.Workspace == "" || deployment.ConfigFile == "" {
		return ConfigValidation{}, &Error{
			Code:        "UPMCTL_WORKSPACE_NOT_FOUND",
			Message:     "deployment workspace and config.rb were not found",
			Details:     map[string]any{"repositoryRoot": deployment.RepositoryRoot},
			Remediation: "create the deployment workspace or pass --workspace",
			ExitCode:    3,
		}
	}
	validation := upmconfig.ParseFile(deployment.ConfigFile)
	return ConfigValidation{
		Context:    deployment,
		Validation: validation,
		Executable: deployment.Managed && deployment.Trust == upmcontext.TrustManagedValid && validation.Safe && validation.Valid && validation.Complete,
	}, nil
}

func (s *Service) Status(ctx context.Context, cwd, workspace string) (upmstatus.Environment, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return upmstatus.Environment{}, appErr
	}
	validation := upmconfig.Result{Status: "NOT_FOUND", Findings: []upmconfig.Finding{}}
	if deployment.ConfigFile != "" {
		validation = upmconfig.ParseFile(deployment.ConfigFile)
	}
	if !deployment.Managed || deployment.Trust != upmcontext.TrustManagedValid || !validation.Safe || !validation.Valid || !validation.Complete {
		return upmstatus.Build(deployment, validation, nil), nil
	}
	observed, err := s.vm.List(ctx, deployment, validation.Config)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return upmstatus.Environment{}, &Error{
				Code:        "UPMCTL_INTERRUPTED",
				Message:     err.Error(),
				Remediation: "rerun status with a positive longer --timeout",
				ExitCode:    6,
			}
		}
		return upmstatus.Environment{}, &Error{
			Code:        "UPMCTL_STATUS_FAILED",
			Message:     err.Error(),
			Remediation: "inspect the managed workspace and external read-only dependencies",
			ExitCode:    4,
		}
	}
	status := upmstatus.Build(deployment, validation, &observed)
	return status, nil
}

func (s *Service) ListVMs(ctx context.Context, cwd, workspace string) (vm.List, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return vm.List{}, appErr
	}
	if deployment.Workspace == "" {
		return vm.List{}, &Error{
			Code:        "UPMCTL_WORKSPACE_NOT_FOUND",
			Message:     "deployment workspace was not found",
			Details:     map[string]any{"repositoryRoot": deployment.RepositoryRoot},
			Remediation: "create the deployment workspace or pass --workspace",
			ExitCode:    3,
		}
	}
	if !deployment.Managed || deployment.Trust != upmcontext.TrustManagedValid {
		return vm.List{}, &Error{
			Code:    "UPMCTL_WORKSPACE_UNTRUSTED",
			Message: "VM observation cannot execute Vagrant or kubeconfig from an untrusted workspace",
			Details: map[string]any{
				"workspace": deployment.Workspace,
				"trust":     deployment.Trust,
			},
			Remediation: "migrate the workspace to a digest-bound Managed Environment before executing VM observation",
			ExitCode:    3,
		}
	}
	validation := upmconfig.ParseFile(deployment.ConfigFile)
	if !validation.Safe || !validation.Valid || !validation.Complete {
		return vm.List{}, &Error{
			Code:    "UPMCTL_CONFIG_INVALID",
			Message: "config.rb is not safe and complete for Vagrant observation",
			Details: map[string]any{
				"status":   validation.Status,
				"findings": validation.Findings,
			},
			Remediation: "fix config validation findings before running VM observation",
			ExitCode:    3,
		}
	}
	list, err := s.vm.List(ctx, deployment, validation.Config)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return vm.List{}, &Error{
				Code:        "UPMCTL_INTERRUPTED",
				Message:     err.Error(),
				Remediation: "rerun observation with a positive longer --timeout",
				ExitCode:    6,
			}
		}
		return vm.List{}, &Error{
			Code:        "UPMCTL_VM_OBSERVE_FAILED",
			Message:     err.Error(),
			Remediation: "verify Vagrant is installed and the deployment workspace is valid",
			ExitCode:    4,
		}
	}
	return list, nil
}

func (s *Service) GetVM(ctx context.Context, cwd, workspace, name string) (vm.Machine, *Error) {
	list, appErr := s.ListVMs(ctx, cwd, workspace)
	if appErr != nil {
		return vm.Machine{}, appErr
	}
	for _, machine := range list.Machines {
		if machine.Name == name {
			return machine, nil
		}
	}
	return vm.Machine{}, &Error{
		Code:        "UPMCTL_VM_NOT_FOUND",
		Message:     fmt.Sprintf("VM %q was not found in the deployment workspace", name),
		Details:     map[string]any{"workspace": list.Workspace, "name": name},
		Remediation: "run upmctl vm list and use an exact managed VM name",
		ExitCode:    3,
	}
}

func CurrentDirectory() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	return cwd
}
