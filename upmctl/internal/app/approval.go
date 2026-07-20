package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/user"
	"strconv"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/admission"
	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/controlstate"
	upmplan "github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/readiness"
)

// ApprovalEvidence is the interactive evidence collected by the CLI from the
// controlling terminal. Actor identity is intentionally absent: the
// application observes the local operating-system identity itself.
type ApprovalEvidence struct {
	Reason          string `json:"reason"`
	Terminal        string `json:"terminal"`
	ChallengeDigest string `json:"challengeDigest"`
	RequestID       string `json:"requestId"`
	CLIVersion      string `json:"cliVersion"`
}

// ApprovalPreparation is the read-only result shown before the CLI asks a
// human for approval. Preparing never creates Approval or Admission state.
type ApprovalPreparation struct {
	APIVersion         string                    `json:"apiVersion"`
	Kind               string                    `json:"kind"`
	Plan               upmplan.Plan              `json:"plan"`
	Preflight          readiness.PreflightResult `json:"preflight"`
	ExecutionAvailable bool                      `json:"executionAvailable"`
}

// ApprovalInspection is the strict Plan-bound projection returned by get and
// list. Revocation is populated only when Status is REVOKED. A claimed Plan is
// reported as INVALID because a Claim permanently consumes its Approval and
// no CLAIMED value exists in the public five-state approval contract.
type ApprovalInspection struct {
	APIVersion         string                        `json:"apiVersion"`
	Kind               string                        `json:"kind"`
	CheckedAt          string                        `json:"checkedAt"`
	Approval           approval.Approval             `json:"approval"`
	Status             string                        `json:"status"`
	Revocation         *admission.ApprovalRevocation `json:"revocation,omitempty"`
	ExecutionAvailable bool                          `json:"executionAvailable"`
}

// PrepareApproval re-observes the environment through the normal read-only
// preflight path and rejects Plans that already have Approval evidence.
func (s *Service) PrepareApproval(ctx context.Context, cwd, workspace, planID string, clock func() time.Time) (ApprovalPreparation, *Error) {
	preflight, appErr := s.PreflightPlan(ctx, cwd, workspace, planID, clock)
	if appErr != nil {
		return ApprovalPreparation{}, appErr
	}
	if preflight.PreflightStatus != readiness.PreflightPassed {
		return ApprovalPreparation{}, preflightBlockedError(preflight)
	}
	if preflight.ApprovalStatus != readiness.ApprovalMissing {
		return ApprovalPreparation{}, approvalStateError(planID, preflight.ApprovalStatus)
	}

	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return ApprovalPreparation{}, appErr
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return ApprovalPreparation{}, appErr
	}
	if candidate.PlanDigest != preflight.PlanDigest {
		return ApprovalPreparation{}, invalidPlanError(planID, fmt.Errorf("Plan changed after approval preparation preflight"))
	}
	return ApprovalPreparation{
		APIVersion: readiness.APIVersion, Kind: "ApprovalPreparation",
		Plan: candidate, Preflight: preflight, ExecutionAvailable: false,
	}, nil
}

// GrantApproval performs its own fresh preflight; callers cannot turn a stale
// PrepareApproval result into an Approval. Only a PASSED baseline with MISSING
// approval state may be published.
func (s *Service) GrantApproval(ctx context.Context, cwd, workspace, planID string, evidence ApprovalEvidence, clock func() time.Time) (approval.Approval, *Error) {
	if clock == nil {
		clock = time.Now
	}
	var checkedAt time.Time
	preflight, appErr := s.PreflightPlan(ctx, cwd, workspace, planID, func() time.Time {
		checkedAt = clock()
		return checkedAt
	})
	if appErr != nil {
		return approval.Approval{}, appErr
	}
	if preflight.PreflightStatus != readiness.PreflightPassed {
		return approval.Approval{}, preflightBlockedError(preflight)
	}
	if preflight.ApprovalStatus != readiness.ApprovalMissing {
		return approval.Approval{}, approvalStateError(planID, preflight.ApprovalStatus)
	}

	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return approval.Approval{}, appErr
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return approval.Approval{}, appErr
	}
	if candidate.PlanDigest != preflight.PlanDigest {
		return approval.Approval{}, invalidPlanError(planID, fmt.Errorf("Plan changed after approval grant preflight"))
	}

	actor, actorErr := observeLocalActor()
	if actorErr != nil {
		return approval.Approval{}, actorErr
	}

	// Actor observation and a terminal exchange take time. Bind and time-check
	// the Plan again at the actual grant instant so a Plan that expired after
	// preflight can never be approved.
	grantAt := clock()
	finalPlan, appErr := readStoredPlan(deployment.Workspace, planID)
	if appErr != nil {
		return approval.Approval{}, appErr
	}
	if finalPlan.PlanDigest != candidate.PlanDigest {
		return approval.Approval{}, invalidPlanError(planID, fmt.Errorf("Plan changed before Approval publication"))
	}
	if err := upmplan.ValidateTiming(finalPlan, grantAt); err != nil {
		return approval.Approval{}, invalidPlanError(planID, fmt.Errorf("Plan is no longer approvable: %w", err))
	}
	// Store create-if-absent is the final concurrency boundary.
	status, _, statusErr := inspectApprovalState(deployment.Workspace, finalPlan, grantAt)
	if statusErr != nil {
		return approval.Approval{}, statusErr
	}
	if status != readiness.ApprovalMissing {
		return approval.Approval{}, approvalStateError(planID, status)
	}
	created, err := approval.New(finalPlan, actor, approval.Presence{
		Terminal: evidence.Terminal, ChallengeDigest: evidence.ChallengeDigest,
	}, evidence.Reason, evidence.RequestID, evidence.CLIVersion, grantAt)
	if err != nil {
		return approval.Approval{}, invalidApprovalError("", planID, err)
	}
	if _, err := approval.NewStore(deployment.Workspace).Save(created); err != nil {
		return approval.Approval{}, mapApprovalStoreError(err, created.ApprovalID, planID)
	}
	return created, nil
}

// GetApproval returns one strictly Plan-bound Approval inspection.
func (s *Service) GetApproval(cwd, workspace, approvalID string, now time.Time) (ApprovalInspection, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return ApprovalInspection{}, appErr
	}
	if deployment.Workspace == "" {
		return ApprovalInspection{}, workspaceNotFound(deployment)
	}
	value, err := approval.NewStore(deployment.Workspace).GetByApprovalID(approvalID)
	if err != nil {
		return ApprovalInspection{}, mapApprovalStoreError(err, approvalID, "")
	}
	return inspectStoredApproval(deployment.Workspace, value, now)
}

// ListApprovals returns a complete, validated view. planID may be empty for
// all Approvals or an exact Plan ID; the store never returns a partial list.
func (s *Service) ListApprovals(cwd, workspace, planID string, now time.Time) ([]ApprovalInspection, *Error) {
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return nil, appErr
	}
	if deployment.Workspace == "" {
		return nil, workspaceNotFound(deployment)
	}
	store := approval.NewStore(deployment.Workspace)
	var (
		values []approval.Approval
		err    error
	)
	if planID == "" {
		values, err = store.List()
	} else {
		values, err = store.List(planID)
	}
	if err != nil {
		return nil, mapApprovalStoreError(err, "", planID)
	}
	result := make([]ApprovalInspection, 0, len(values))
	for _, value := range values {
		inspection, appErr := inspectStoredApproval(deployment.Workspace, value, now)
		if appErr != nil {
			return nil, appErr
		}
		result = append(result, inspection)
	}
	return result, nil
}

// RevokeApproval records a separate immutable Admission artifact. It never
// changes the original Approval and refuses expired, revoked, or claimed
// Approvals.
func (s *Service) RevokeApproval(cwd, workspace, approvalID string, evidence ApprovalEvidence, clock func() time.Time) (admission.ApprovalRevocation, *Error) {
	if clock == nil {
		clock = time.Now
	}
	checkedAt := clock()
	deployment, appErr := s.DiscoverContext(cwd, workspace)
	if appErr != nil {
		return admission.ApprovalRevocation{}, appErr
	}
	if deployment.Workspace == "" {
		return admission.ApprovalRevocation{}, workspaceNotFound(deployment)
	}
	value, err := approval.NewStore(deployment.Workspace).GetByApprovalID(approvalID)
	if err != nil {
		return admission.ApprovalRevocation{}, mapApprovalStoreError(err, approvalID, "")
	}
	candidate, appErr := readStoredPlan(deployment.Workspace, value.PlanID)
	if appErr != nil {
		return admission.ApprovalRevocation{}, appErr
	}
	if err := approval.Validate(value, candidate); err != nil {
		return admission.ApprovalRevocation{}, invalidApprovalError(value.ApprovalID, value.PlanID, err)
	}
	expired, err := value.Expired(checkedAt)
	if err != nil {
		return admission.ApprovalRevocation{}, invalidApprovalError(value.ApprovalID, value.PlanID, err)
	}
	if expired {
		return admission.ApprovalRevocation{}, approvalStateError(value.PlanID, readiness.ApprovalExpired)
	}
	if existing, err := admission.NewStore(deployment.Workspace).Read(value.PlanID); err == nil {
		return admission.ApprovalRevocation{}, admissionConflictError(existing, value.PlanID)
	} else if !errors.Is(err, admission.ErrAdmissionNotFound) {
		return admission.ApprovalRevocation{}, mapAdmissionStoreError(err, value.PlanID)
	}

	actor, actorErr := observeLocalActor()
	if actorErr != nil {
		return admission.ApprovalRevocation{}, actorErr
	}

	// Re-read every immutable binding and the shared admission slot at the
	// actual revocation instant. This closes the expiry boundary and recognizes
	// a Claim that won the admission race while the human was interacting.
	revokedAt := clock()
	finalApproval, err := approval.NewStore(deployment.Workspace).GetByApprovalID(approvalID)
	if err != nil {
		return admission.ApprovalRevocation{}, mapApprovalStoreError(err, approvalID, value.PlanID)
	}
	if finalApproval.ApprovalID != value.ApprovalID || finalApproval.ApprovalDigest != value.ApprovalDigest {
		return admission.ApprovalRevocation{}, invalidApprovalError(approvalID, value.PlanID, fmt.Errorf("Approval changed before revocation publication"))
	}
	finalPlan, appErr := readStoredPlan(deployment.Workspace, finalApproval.PlanID)
	if appErr != nil {
		return admission.ApprovalRevocation{}, appErr
	}
	if finalPlan.PlanID != candidate.PlanID || finalPlan.PlanDigest != candidate.PlanDigest {
		return admission.ApprovalRevocation{}, invalidPlanError(candidate.PlanID, fmt.Errorf("Plan changed before revocation publication"))
	}
	if err := approval.Validate(finalApproval, finalPlan); err != nil {
		return admission.ApprovalRevocation{}, invalidApprovalError(finalApproval.ApprovalID, finalApproval.PlanID, err)
	}
	if expired, err := finalApproval.Expired(revokedAt); err != nil {
		return admission.ApprovalRevocation{}, invalidApprovalError(finalApproval.ApprovalID, finalApproval.PlanID, err)
	} else if expired {
		return admission.ApprovalRevocation{}, approvalStateError(finalApproval.PlanID, readiness.ApprovalExpired)
	}
	if existing, err := admission.NewStore(deployment.Workspace).Read(finalApproval.PlanID); err == nil {
		return admission.ApprovalRevocation{}, admissionConflictError(existing, finalApproval.PlanID)
	} else if !errors.Is(err, admission.ErrAdmissionNotFound) {
		return admission.ApprovalRevocation{}, mapAdmissionStoreError(err, finalApproval.PlanID)
	}
	revocation, err := admission.NewApprovalRevocation(finalApproval, finalPlan, actor, approval.Presence{
		Terminal: evidence.Terminal, ChallengeDigest: evidence.ChallengeDigest,
	}, evidence.Reason, revokedAt)
	if err != nil {
		return admission.ApprovalRevocation{}, invalidApprovalError(value.ApprovalID, value.PlanID, err)
	}
	store := admission.NewStore(deployment.Workspace)
	if _, err := store.Save(admission.RevocationArtifact(revocation)); err != nil {
		if errors.Is(err, admission.ErrAdmissionExists) {
			if existing, readErr := store.Read(value.PlanID); readErr == nil {
				return admission.ApprovalRevocation{}, admissionConflictError(existing, value.PlanID)
			}
		}
		return admission.ApprovalRevocation{}, mapAdmissionStoreError(err, value.PlanID)
	}
	return revocation, nil
}

func inspectStoredApproval(workspace string, value approval.Approval, now time.Time) (ApprovalInspection, *Error) {
	candidate, appErr := readStoredPlan(workspace, value.PlanID)
	if appErr != nil {
		return ApprovalInspection{}, appErr
	}
	if err := approval.Validate(value, candidate); err != nil {
		return ApprovalInspection{}, invalidApprovalError(value.ApprovalID, value.PlanID, err)
	}
	status, revocation, appErr := inspectApprovalState(workspace, candidate, now)
	if appErr != nil {
		return ApprovalInspection{}, appErr
	}
	if status == readiness.ApprovalMissing {
		return ApprovalInspection{}, invalidApprovalError(value.ApprovalID, value.PlanID, fmt.Errorf("Approval disappeared during inspection"))
	}
	current, err := approval.NewStore(workspace).ReadByPlan(value.PlanID)
	if err != nil || current.ApprovalID != value.ApprovalID || current.ApprovalDigest != value.ApprovalDigest {
		if err == nil {
			err = fmt.Errorf("Approval changed during inspection")
		}
		return ApprovalInspection{}, invalidApprovalError(value.ApprovalID, value.PlanID, err)
	}
	return ApprovalInspection{
		APIVersion: readiness.APIVersion, Kind: "ApprovalInspection",
		CheckedAt: now.UTC().Format(time.RFC3339Nano), Approval: value,
		Status: status, Revocation: revocation, ExecutionAvailable: false,
	}, nil
}

// inspectApprovalState is deliberately side-effect free. Store corruption or
// a Claim is represented as INVALID for preflight instead of being mistaken
// for missing approval evidence.
func inspectApprovalState(workspace string, candidate upmplan.Plan, now time.Time) (string, *admission.ApprovalRevocation, *Error) {
	value, err := approval.NewStore(workspace).ReadByPlan(candidate.PlanID)
	if errors.Is(err, approval.ErrApprovalNotFound) {
		// A Revocation or Claim without its immutable Approval is not an empty
		// slot. Treat the orphaned admission state as INVALID so Grant cannot
		// publish over a previously consumed or corrupted Plan lifecycle.
		if _, admissionErr := admission.NewStore(workspace).Read(candidate.PlanID); errors.Is(admissionErr, admission.ErrAdmissionNotFound) {
			return readiness.ApprovalMissing, nil, nil
		}
		return readiness.ApprovalInvalid, nil, nil
	}
	if err != nil {
		return readiness.ApprovalInvalid, nil, nil
	}
	if err := approval.Validate(value, candidate); err != nil {
		return readiness.ApprovalInvalid, nil, nil
	}

	artifact, err := admission.NewStore(workspace).Read(candidate.PlanID)
	if err == nil {
		switch {
		case artifact.Revocation != nil:
			if err := admission.ValidateApprovalRevocation(*artifact.Revocation, value, candidate); err != nil {
				return readiness.ApprovalInvalid, nil, nil
			}
			copy := *artifact.Revocation
			return readiness.ApprovalRevoked, &copy, nil
		case artifact.Claim != nil:
			if err := admission.ValidatePlanClaim(*artifact.Claim, value, candidate); err != nil {
				return readiness.ApprovalInvalid, nil, nil
			}
			return readiness.ApprovalInvalid, nil, nil
		default:
			return readiness.ApprovalInvalid, nil, nil
		}
	}
	if !errors.Is(err, admission.ErrAdmissionNotFound) {
		return readiness.ApprovalInvalid, nil, nil
	}
	expired, err := value.Expired(now)
	if err != nil {
		return readiness.ApprovalInvalid, nil, nil
	}
	if expired {
		return readiness.ApprovalExpired, nil, nil
	}
	return readiness.ApprovalApproved, nil, nil
}

func observeLocalActor() (approval.Actor, *Error) {
	uid := strconv.Itoa(os.Geteuid())
	account, err := user.LookupId(uid)
	if err != nil {
		return approval.Actor{}, &Error{Code: "UPMCTL_ACTOR_OBSERVE_FAILED", Message: fmt.Sprintf("resolve effective OS user %s: %v", uid, err), Remediation: "repair local account resolution before recording human approval evidence", ExitCode: 4}
	}
	hostname, err := os.Hostname()
	if err != nil {
		return approval.Actor{}, &Error{Code: "UPMCTL_ACTOR_OBSERVE_FAILED", Message: fmt.Sprintf("observe local hostname: %v", err), Remediation: "repair local hostname resolution before recording human approval evidence", ExitCode: 4}
	}
	return approval.Actor{
		Subject: "os-user:" + uid, UID: uid, Username: account.Username, Hostname: hostname,
	}, nil
}

func preflightBlockedError(result readiness.PreflightResult) *Error {
	return &Error{Code: "UPMCTL_PREFLIGHT_BLOCKED", Message: "Approval requires a PASSED read-only preflight baseline", Details: map[string]any{"planId": result.PlanID, "blockers": result.Blockers}, Remediation: "resolve read-only preflight blockers and generate a new Plan if its basis changed", ExitCode: 3}
}

func approvalStateError(planID, status string) *Error {
	code, message, remediation := "UPMCTL_APPROVAL_INVALID", "Approval evidence is invalid", "inspect the immutable Approval and Admission control state"
	switch status {
	case readiness.ApprovalApproved:
		code, message, remediation = "UPMCTL_APPROVAL_EXISTS", "This Plan already has an active Approval", "inspect the existing Approval instead of granting another"
	case readiness.ApprovalRevoked:
		code, message, remediation = "UPMCTL_APPROVAL_REVOKED", "The Approval for this Plan has been revoked", "generate a new Plan before requesting another Approval"
	case readiness.ApprovalExpired:
		code, message, remediation = "UPMCTL_APPROVAL_EXPIRED", "The Approval for this Plan has expired", "generate a new Plan and request a fresh Approval"
	case readiness.ApprovalMissing:
		code, message, remediation = "UPMCTL_APPROVAL_NOT_FOUND", "This Plan has no Approval", "grant human Approval from a controlling terminal"
	}
	return &Error{Code: code, Message: message, Details: map[string]any{"planId": planID, "approvalStatus": status}, Remediation: remediation, ExitCode: 3}
}

func invalidApprovalError(approvalID, planID string, err error) *Error {
	return &Error{Code: "UPMCTL_APPROVAL_INVALID", Message: err.Error(), Details: map[string]any{"approvalId": approvalID, "planId": planID}, Remediation: "discard the invalid control state and generate a new Plan", ExitCode: 3}
}

func mapApprovalStoreError(err error, approvalID, planID string) *Error {
	details := map[string]any{"approvalId": approvalID, "planId": planID}
	switch {
	case errors.Is(err, approval.ErrApprovalNotFound), errors.Is(err, os.ErrNotExist):
		return &Error{Code: "UPMCTL_APPROVAL_NOT_FOUND", Message: "Approval was not found", Details: details, Remediation: "list Approvals in this workspace and use an existing ID", ExitCode: 3}
	case errors.Is(err, approval.ErrApprovalExists):
		return &Error{Code: "UPMCTL_APPROVAL_EXISTS", Message: "An immutable Approval already exists for this Plan", Details: details, Remediation: "inspect the existing Approval; immutable evidence cannot be overwritten", ExitCode: 3}
	case errors.Is(err, approval.ErrInvalidPlanID), errors.Is(err, approval.ErrInvalidApprovalID):
		return invalidApprovalError(approvalID, planID, err)
	case errors.Is(err, controlstate.ErrUnsafe):
		return &Error{Code: "UPMCTL_APPROVAL_STORE_UNSAFE", Message: err.Error(), Details: details, Remediation: "repair private .upmctl/approvals ownership, permissions and path identity", ExitCode: 3}
	default:
		return &Error{Code: "UPMCTL_APPROVAL_STORE_FAILED", Message: err.Error(), Details: details, Remediation: "inspect the private immutable Approval store", ExitCode: 4}
	}
}

func mapAdmissionStoreError(err error, planID string) *Error {
	details := map[string]any{"planId": planID}
	switch {
	case errors.Is(err, admission.ErrInvalidPlanID):
		return &Error{Code: "UPMCTL_ADMISSION_INVALID", Message: err.Error(), Details: details, Remediation: "use the exact Plan ID bound to the Approval", ExitCode: 3}
	case errors.Is(err, controlstate.ErrUnsafe):
		return &Error{Code: "UPMCTL_ADMISSION_STORE_UNSAFE", Message: err.Error(), Details: details, Remediation: "repair private .upmctl/admissions ownership, permissions and path identity", ExitCode: 3}
	case errors.Is(err, admission.ErrAdmissionExists):
		return &Error{Code: "UPMCTL_ADMISSION_CONFLICT", Message: "This Plan already has immutable admission state", Details: details, Remediation: "inspect the existing Revocation or Claim", ExitCode: 3}
	default:
		return &Error{Code: "UPMCTL_ADMISSION_STORE_FAILED", Message: err.Error(), Details: details, Remediation: "inspect the private immutable Admission store", ExitCode: 4}
	}
}

func admissionConflictError(artifact admission.Artifact, planID string) *Error {
	switch {
	case artifact.Revocation != nil:
		return &Error{Code: "UPMCTL_APPROVAL_REVOKED", Message: "This Approval is already revoked", Details: map[string]any{"planId": planID, "revocationId": artifact.Revocation.RevocationID}, Remediation: "generate a new Plan before requesting another Approval", ExitCode: 3}
	case artifact.Claim != nil:
		return &Error{Code: "UPMCTL_PLAN_ALREADY_CLAIMED", Message: "This Plan and Approval have already been claimed by an operation", Details: map[string]any{"planId": planID, "claimId": artifact.Claim.ClaimID, "operationId": artifact.Claim.OperationID}, Remediation: "inspect the consuming operation; claimed Approvals cannot be revoked or reused", ExitCode: 3}
	default:
		return &Error{Code: "UPMCTL_ADMISSION_INVALID", Message: "Admission control state is invalid", Details: map[string]any{"planId": planID}, Remediation: "inspect the immutable Admission artifact", ExitCode: 3}
	}
}
