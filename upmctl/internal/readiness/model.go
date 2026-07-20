// Package readiness builds read-only Phase 2b1 readiness artifacts.
//
// It deliberately has no executor, approval store, lock, filesystem, or
// command-runner dependency. Its results never imply that apply is available.
package readiness

import (
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
)

const (
	APIVersion = "upmctl.upm.io/v1alpha1"

	KindPlanInspection  = "PlanInspection"
	KindPlanValidation  = "PlanValidation"
	KindPreflightResult = "PreflightResult"

	StateValid       = "VALID"
	StateInvalid     = "INVALID"
	StateUnavailable = "UNAVAILABLE"
	// Compatibility aliases used by contract fixtures and callers that name
	// the values by their CurrentState role.
	CurrentStateValid       = StateValid
	CurrentStateInvalid     = StateInvalid
	CurrentStateUnavailable = StateUnavailable

	FreshnessCurrent = "CURRENT"
	FreshnessExpired = "EXPIRED"
	FreshnessInvalid = "INVALID"

	BindingMatch      = "MATCH"
	BindingMismatch   = "MISMATCH"
	BindingInvalid    = "INVALID"
	BindingUnknown    = "UNKNOWN"
	BindingNotChecked = "NOT_CHECKED"

	BasisMatch       = "MATCH"
	BasisDrift       = "DRIFT"
	BasisInvalid     = "INVALID"
	BasisUnavailable = "UNAVAILABLE"

	PreflightPassed  = "PASSED"
	PreflightBlocked = "BLOCKED"

	CheckPass    = "PASS"
	CheckFail    = "FAIL"
	CheckSkipped = "SKIPPED"

	ApplyDecisionBlocked = "BLOCKED"

	ApprovalMissing  = "MISSING"
	ApprovalApproved = "APPROVED"
	ApprovalRevoked  = "REVOKED"
	ApprovalExpired  = "EXPIRED"
	ApprovalInvalid  = "INVALID"
)

const (
	CheckPlanIntegrity      = "PLAN_INTEGRITY"
	CheckPlanTimeValid      = "PLAN_TIME_VALID"
	CheckEnvironmentMatch   = "ENVIRONMENT_MATCH"
	CheckConfigMatch        = "CONFIG_MATCH"
	CheckManagedStateMatch  = "MANAGED_STATE_MATCH"
	CheckObservationSafe    = "OBSERVATION_SAFE"
	CheckObservedStateMatch = "OBSERVED_STATE_MATCH"
	CheckExecutorCapability = "EXECUTOR_CAPABILITY"
	CheckConcurrencyControl = "CONCURRENCY_CONTROL"
	CheckApprovalSubsystem  = "APPROVAL_SUBSYSTEM"
)

const (
	CodeCheckPassed              = "UPMCTL_CHECK_PASSED"
	CodeCheckSkipped             = "UPMCTL_CHECK_SKIPPED"
	CodePlanTampered             = "UPMCTL_PLAN_TAMPERED"
	CodePlanExpired              = "UPMCTL_PLAN_EXPIRED"
	CodePlanTimeInvalid          = "UPMCTL_PLAN_TIME_INVALID"
	CodeEnvironmentMismatch      = "UPMCTL_ENVIRONMENT_MISMATCH"
	CodeEnvironmentUnknown       = "UPMCTL_ENVIRONMENT_UNKNOWN"
	CodeConfigDrift              = "UPMCTL_CONFIG_DRIFT"
	CodeConfigInvalid            = "UPMCTL_CONFIG_INVALID"
	CodeConfigUnavailable        = "UPMCTL_CONFIG_UNAVAILABLE"
	CodeManagedStateDrift        = "UPMCTL_MANAGED_STATE_DRIFT"
	CodeManagedStateInvalid      = "UPMCTL_MANAGED_STATE_INVALID"
	CodeManagedStateUnavailable  = "UPMCTL_MANAGED_STATE_UNAVAILABLE"
	CodeObservationUnsafe        = "UPMCTL_OBSERVED_STATE_UNSAFE"
	CodeObservedStateDrift       = "UPMCTL_OBSERVED_STATE_DRIFT"
	CodeObservedStateInvalid     = "UPMCTL_OBSERVED_STATE_INVALID"
	CodeObservedStateUnavailable = "UPMCTL_OBSERVED_STATE_UNAVAILABLE"
	CodeExecutorUnavailable      = "UPMCTL_CAPABILITY_UNAVAILABLE"
	CodeConcurrencyUnavailable   = "UPMCTL_CONCURRENCY_CHECK_NOT_IMPLEMENTED"
	CodeApprovalMissing          = "UPMCTL_APPROVAL_MISSING"
	CodeApprovalRevoked          = "UPMCTL_APPROVAL_REVOKED"
	CodeApprovalExpired          = "UPMCTL_APPROVAL_EXPIRED"
	CodeApprovalInvalid          = "UPMCTL_APPROVAL_INVALID"
)

type CurrentState struct {
	EnvironmentID       string
	ConfigDigest        string
	ConfigStatus        string
	ManagedStateDigest  string
	ManagedStateStatus  string
	ObservedStateDigest string
	ObservedStateStatus string
	ObservedStateSafe   bool
}

type PlanInspection struct {
	APIVersion         string    `json:"apiVersion"`
	Kind               string    `json:"kind"`
	Plan               plan.Plan `json:"plan"`
	Expired            bool      `json:"expired"`
	ExecutionAvailable bool      `json:"executionAvailable"`
	CheckedAt          string    `json:"checkedAt"`
}

type PlanValidation struct {
	APIVersion           string   `json:"apiVersion"`
	Kind                 string   `json:"kind"`
	PlanID               string   `json:"planId"`
	CheckedAt            string   `json:"checkedAt"`
	ArtifactStatus       string   `json:"artifactStatus"`
	FreshnessStatus      string   `json:"freshnessStatus"`
	EnvironmentBinding   string   `json:"environmentBinding"`
	ConfigBinding        string   `json:"configBinding"`
	ManagedStateBinding  string   `json:"managedStateBinding"`
	ObservedStateBinding string   `json:"observedStateBinding"`
	ExecutionAvailable   bool     `json:"executionAvailable"`
	Blockers             []string `json:"blockers"`
}

type ValidationInput struct {
	Plan    plan.Plan
	Now     time.Time
	Current CurrentState
}

type PreflightInput struct {
	Plan           plan.Plan
	Now            time.Time
	Current        CurrentState
	ApprovalStatus string
}

type Basis struct {
	Config        BasisComparison `json:"config"`
	ManagedState  BasisComparison `json:"managedState"`
	ObservedState BasisComparison `json:"observedState"`
}

type BasisComparison struct {
	Expected string  `json:"expected"`
	Current  *string `json:"current"`
	Status   string  `json:"status"`
}

type PreflightCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PreflightResult struct {
	APIVersion         string           `json:"apiVersion"`
	Kind               string           `json:"kind"`
	PlanID             string           `json:"planId"`
	PlanDigest         string           `json:"planDigest"`
	CheckedAt          string           `json:"checkedAt"`
	PreflightStatus    string           `json:"preflightStatus"`
	ApplyDecision      string           `json:"applyDecision"`
	ExecutionAvailable bool             `json:"executionAvailable"`
	ApprovalStatus     string           `json:"approvalStatus"`
	Basis              Basis            `json:"basis"`
	Checks             []PreflightCheck `json:"checks"`
	Blockers           []string         `json:"blockers"`
}
