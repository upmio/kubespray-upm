package readiness

import (
	"fmt"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
)

func BuildInspection(candidate plan.Plan, now time.Time) (PlanInspection, error) {
	if now.IsZero() {
		return PlanInspection{}, fmt.Errorf("inspection time is required")
	}
	if err := validateArtifact(candidate); err != nil {
		return PlanInspection{}, fmt.Errorf("inspect plan: %w", err)
	}
	expired, err := candidate.Expired(now)
	if err != nil {
		return PlanInspection{}, fmt.Errorf("inspect plan expiry: %w", err)
	}
	return PlanInspection{
		APIVersion: APIVersion, Kind: KindPlanInspection, Plan: candidate,
		Expired: expired, ExecutionAvailable: false, CheckedAt: formatTime(now),
	}, nil
}

func BuildValidation(input ValidationInput) PlanValidation {
	result := PlanValidation{
		APIVersion: APIVersion, Kind: KindPlanValidation, PlanID: input.Plan.PlanID,
		CheckedAt: formatTime(input.Now), ArtifactStatus: StateValid,
		FreshnessStatus: FreshnessCurrent, EnvironmentBinding: BindingUnknown,
		ConfigBinding: BindingUnknown, ManagedStateBinding: BindingUnknown,
		ObservedStateBinding: BindingNotChecked, ExecutionAvailable: false, Blockers: []string{},
	}
	if err := validateArtifact(input.Plan); err != nil {
		result.ArtifactStatus = StateInvalid
		result.FreshnessStatus = FreshnessInvalid
		result.Blockers = append(result.Blockers, CodePlanTampered)
		return result
	}
	if err := plan.ValidateTiming(input.Plan, input.Now); err != nil {
		expired, _ := input.Plan.Expired(input.Now)
		if expired {
			result.FreshnessStatus = FreshnessExpired
			result.Blockers = append(result.Blockers, CodePlanExpired)
		} else {
			result.FreshnessStatus = FreshnessInvalid
			result.Blockers = append(result.Blockers, CodePlanTimeInvalid)
		}
	}
	result.EnvironmentBinding = environmentBinding(input.Plan.EnvironmentID, input.Current.EnvironmentID)
	if result.EnvironmentBinding == BindingMismatch {
		result.Blockers = append(result.Blockers, CodeEnvironmentMismatch)
	} else if result.EnvironmentBinding == BindingUnknown {
		result.Blockers = append(result.Blockers, CodeEnvironmentUnknown)
	}
	result.ConfigBinding, result.Blockers = validationBinding(input.Plan.Basis.ConfigDigest, input.Current.ConfigDigest, input.Current.ConfigStatus, CodeConfigDrift, CodeConfigInvalid, CodeConfigUnavailable, result.Blockers)
	result.ManagedStateBinding, result.Blockers = validationBinding(input.Plan.Basis.ManagedStateDigest, input.Current.ManagedStateDigest, input.Current.ManagedStateStatus, CodeManagedStateDrift, CodeManagedStateInvalid, CodeManagedStateUnavailable, result.Blockers)
	return result
}

func BuildPreflight(input PreflightInput) PreflightResult {
	approvalStatus := normalizeApprovalStatus(input.ApprovalStatus)
	result := PreflightResult{
		APIVersion: APIVersion, Kind: KindPreflightResult,
		PlanID: input.Plan.PlanID, PlanDigest: input.Plan.PlanDigest, CheckedAt: formatTime(input.Now),
		PreflightStatus: PreflightPassed, ApplyDecision: ApplyDecisionBlocked,
		ExecutionAvailable: false, ApprovalStatus: approvalStatus,
		Basis: Basis{
			Config:        compareBasis(input.Plan.Basis.ConfigDigest, input.Current.ConfigDigest, input.Current.ConfigStatus),
			ManagedState:  compareBasis(input.Plan.Basis.ManagedStateDigest, input.Current.ManagedStateDigest, input.Current.ManagedStateStatus),
			ObservedState: compareBasis(input.Plan.Basis.ObservedStateDigest, input.Current.ObservedStateDigest, input.Current.ObservedStateStatus),
		},
		Checks: make([]PreflightCheck, 0, 10), Blockers: []string{},
	}

	artifactOK := validateArtifact(input.Plan) == nil
	if !artifactOK {
		result.ApprovalStatus = ApprovalInvalid
	}
	result.add(CheckPlanIntegrity, artifactOK, CodePlanTampered, "Plan artifact and closed action contract are valid")
	if !artifactOK {
		for _, item := range []struct{ id, message string }{
			{CheckPlanTimeValid, "Plan time validation requires a valid artifact"},
			{CheckEnvironmentMatch, "Environment comparison requires a valid artifact"},
			{CheckConfigMatch, "Config comparison requires a valid artifact"},
			{CheckManagedStateMatch, "Managed State comparison requires a valid artifact"},
			{CheckObservationSafe, "Observation safety requires a valid artifact"},
			{CheckObservedStateMatch, "Observed State comparison requires a valid artifact"},
		} {
			result.Checks = append(result.Checks, PreflightCheck{ID: item.id, Status: CheckSkipped, Code: CodeCheckSkipped, Message: item.message})
		}
		result.add(CheckExecutorCapability, false, CodeExecutorUnavailable, "The vm.start executor is not implemented in Phase 2b1")
		result.add(CheckConcurrencyControl, false, CodeConcurrencyUnavailable, "Operation locking and concurrency admission are not implemented in Phase 2b1")
		result.addApprovalCheck()
		result.PreflightStatus = PreflightBlocked
		return result
	}
	timeOK := artifactOK && plan.ValidateTiming(input.Plan, input.Now) == nil
	timeCode := CodePlanTimeInvalid
	if expired, _ := input.Plan.Expired(input.Now); expired {
		timeCode = CodePlanExpired
	}
	result.addOrSkip(CheckPlanTimeValid, artifactOK, timeOK, timeCode, "Plan creation time, fixed TTL and expiry are valid")

	environmentOK := input.Current.EnvironmentID != "" && input.Plan.EnvironmentID == input.Current.EnvironmentID
	environmentCode := CodeEnvironmentMismatch
	if input.Current.EnvironmentID == "" {
		environmentCode = CodeEnvironmentUnknown
	}
	result.addOrSkip(CheckEnvironmentMatch, artifactOK, environmentOK, environmentCode, "Plan environment matches the current workspace")
	result.addBindingCheck(CheckConfigMatch, result.Basis.Config, CodeConfigDrift, CodeConfigInvalid, CodeConfigUnavailable, "Config digest matches the Plan basis")
	result.addBindingCheck(CheckManagedStateMatch, result.Basis.ManagedState, CodeManagedStateDrift, CodeManagedStateInvalid, CodeManagedStateUnavailable, "Managed State digest matches the Plan basis")

	observationAvailable := input.Current.ObservedStateStatus == StateValid
	result.addOrSkip(CheckObservationSafe, observationAvailable, observationAvailable && input.Current.ObservedStateSafe, CodeObservationUnsafe, "Observed VM and cluster identity is safe for comparison")
	result.addBindingCheck(CheckObservedStateMatch, result.Basis.ObservedState, CodeObservedStateDrift, CodeObservedStateInvalid, CodeObservedStateUnavailable, "Observed State digest matches the Plan basis")

	result.add(CheckExecutorCapability, false, CodeExecutorUnavailable, "The vm.start executor is not implemented in Phase 2b1")
	result.add(CheckConcurrencyControl, false, CodeConcurrencyUnavailable, "Operation locking and concurrency admission are not implemented in Phase 2b1")
	result.addApprovalCheck()
	for index := 0; index < 7; index++ {
		if result.Checks[index].Status != CheckPass {
			result.PreflightStatus = PreflightBlocked
			break
		}
	}
	return result
}

func normalizeApprovalStatus(status string) string {
	switch status {
	case ApprovalApproved, ApprovalRevoked, ApprovalExpired, ApprovalInvalid:
		return status
	default:
		return ApprovalMissing
	}
}

func (result *PreflightResult) addApprovalCheck() {
	switch result.ApprovalStatus {
	case ApprovalApproved:
		result.add(CheckApprovalSubsystem, true, "", "A current immutable human Approval is bound to this Plan")
	case ApprovalRevoked:
		result.add(CheckApprovalSubsystem, false, CodeApprovalRevoked, "The Approval bound to this Plan was revoked")
	case ApprovalExpired:
		result.add(CheckApprovalSubsystem, false, CodeApprovalExpired, "The Approval bound to this Plan has expired")
	case ApprovalInvalid:
		result.add(CheckApprovalSubsystem, false, CodeApprovalInvalid, "Approval evidence is invalid or cannot be bound to this Plan")
	default:
		result.add(CheckApprovalSubsystem, false, CodeApprovalMissing, "This Plan has no human Approval evidence")
	}
}

func validateArtifact(candidate plan.Plan) error {
	if err := plan.Validate(candidate); err != nil {
		return err
	}
	return plan.ValidateActionContract(candidate)
}

func (result *PreflightResult) add(id string, passed bool, failureCode, message string) {
	status, code := CheckFail, failureCode
	if passed {
		status, code = CheckPass, CodeCheckPassed
	} else {
		result.Blockers = append(result.Blockers, failureCode)
	}
	result.Checks = append(result.Checks, PreflightCheck{ID: id, Status: status, Code: code, Message: message})
}

func (result *PreflightResult) addOrSkip(id string, available, passed bool, failureCode, message string) {
	if !available {
		result.Checks = append(result.Checks, PreflightCheck{ID: id, Status: CheckSkipped, Code: CodeCheckSkipped, Message: message})
		return
	}
	result.add(id, passed, failureCode, message)
}

func (result *PreflightResult) addBindingCheck(id string, comparison BasisComparison, driftCode, invalidCode, unavailableCode, message string) {
	switch comparison.Status {
	case BasisMatch:
		result.add(id, true, "", message)
	case BasisDrift:
		result.add(id, false, driftCode, message)
	case BasisInvalid:
		result.add(id, false, invalidCode, message)
	default:
		result.add(id, false, unavailableCode, message)
	}
}

func compareBasis(expected, current, state string) BasisComparison {
	comparison := BasisComparison{Expected: expected, Status: BasisUnavailable}
	if state == StateInvalid {
		comparison.Status = BasisInvalid
		return comparison
	}
	if state != StateValid || current == "" {
		return comparison
	}
	comparison.Current = stringPointer(current)
	comparison.Status = BasisDrift
	if expected == current {
		comparison.Status = BasisMatch
	}
	return comparison
}

func validationBinding(expected, current, state, driftCode, invalidCode, unavailableCode string, blockers []string) (string, []string) {
	comparison := compareBasis(expected, current, state)
	switch comparison.Status {
	case BasisMatch:
		return BindingMatch, blockers
	case BasisDrift:
		return BindingMismatch, append(blockers, driftCode)
	case BasisInvalid:
		return BindingInvalid, append(blockers, invalidCode)
	default:
		return BindingUnknown, append(blockers, unavailableCode)
	}
}

func environmentBinding(expected, current string) string {
	if current == "" {
		return BindingUnknown
	}
	if expected == current {
		return BindingMatch
	}
	return BindingMismatch
}

func stringPointer(value string) *string { return &value }

func formatTime(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
