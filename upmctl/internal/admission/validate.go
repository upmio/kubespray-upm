package admission

import (
	"fmt"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
)

func ValidateApprovalRevocation(r ApprovalRevocation, a approval.Approval, p plan.Plan) error {
	if err := ValidateApprovalRevocationIntegrity(r); err != nil {
		return err
	}
	if err := approval.Validate(a, p); err != nil {
		return fmt.Errorf("bound approval is invalid: %w", err)
	}
	if r.ApprovalID != a.ApprovalID || r.ApprovalDigest != a.ApprovalDigest {
		return fmt.Errorf("revocation does not bind the supplied approval")
	}
	if r.PlanID != p.PlanID || r.PlanDigest != p.PlanDigest || r.EnvironmentID != p.EnvironmentID {
		return fmt.Errorf("revocation does not bind the supplied plan")
	}
	if a.PlanID != p.PlanID || a.PlanDigest != p.PlanDigest || a.EnvironmentID != p.EnvironmentID {
		return fmt.Errorf("approval does not bind the supplied plan")
	}
	revokedAt, _ := time.Parse(time.RFC3339Nano, r.RevokedAt)
	approvedAt, _ := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	if revokedAt.Before(approvedAt) {
		return fmt.Errorf("revokedAt is before approvedAt")
	}
	if expired, err := a.Expired(revokedAt); err != nil || expired {
		return fmt.Errorf("approval is expired at revocation time")
	}
	return nil
}

func ValidateApprovalRevocationIntegrity(r ApprovalRevocation) error {
	if r.APIVersion != APIVersion || r.Kind != KindApprovalRevocation || r.Disposition != DispositionRevoked {
		return fmt.Errorf("revocation identity or disposition is invalid")
	}
	if !revocationPattern.MatchString(r.RevocationID) || !approvalIDPattern.MatchString(r.ApprovalID) || !planIDPattern.MatchString(r.PlanID) {
		return fmt.Errorf("revocation, approval, or plan ID is invalid")
	}
	for name, value := range map[string]string{
		"revocationDigest": r.RevocationDigest, "approvalDigest": r.ApprovalDigest,
		"planDigest": r.PlanDigest,
	} {
		if !digestPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	if err := validateText("environmentId", r.EnvironmentID, 128); err != nil {
		return err
	}
	if err := validateText("reason", r.Reason, 2048); err != nil {
		return err
	}
	if err := validateActor(r.Actor, r.RevokedAt); err != nil {
		return fmt.Errorf("actor: %w", err)
	}
	if r.Actor.Source != approval.SourceHumanCLI || r.Actor.AuthMethod != approval.AuthMethodInteractiveTTY {
		return fmt.Errorf("revocation actor source or authMethod is invalid")
	}
	if err := validateHumanPresence(r.HumanPresence, r.RevokedAt); err != nil {
		return err
	}
	if err := validateCanonicalTime("revokedAt", r.RevokedAt); err != nil {
		return err
	}
	expectedDigest, err := r.ExpectedDigest()
	if err != nil {
		return err
	}
	if r.RevocationDigest != expectedDigest {
		return fmt.Errorf("revocationDigest does not match revocation semantics")
	}
	expectedID, err := r.ExpectedID()
	if err != nil {
		return err
	}
	if r.RevocationID != expectedID {
		return fmt.Errorf("revocationId does not match revocation digest")
	}
	return nil
}

func ValidatePlanClaim(c PlanClaim, a approval.Approval, p plan.Plan) error {
	if err := ValidatePlanClaimIntegrity(c); err != nil {
		return err
	}
	if err := approval.Validate(a, p); err != nil {
		return fmt.Errorf("bound approval is invalid: %w", err)
	}
	if c.PlanID != p.PlanID || c.PlanDigest != p.PlanDigest || c.EnvironmentID != p.EnvironmentID || c.Action != p.Action || c.Scope != p.ApprovalScope || c.Basis != p.Basis {
		return fmt.Errorf("claim does not bind the supplied plan")
	}
	if c.ApprovalID != a.ApprovalID || c.ApprovalDigest != a.ApprovalDigest {
		return fmt.Errorf("claim does not bind the supplied approval")
	}
	if a.PlanID != p.PlanID || a.PlanDigest != p.PlanDigest || a.EnvironmentID != p.EnvironmentID || a.Action != p.Action || a.ApprovalScope != p.ApprovalScope || a.Basis != p.Basis {
		return fmt.Errorf("approval does not bind the supplied plan")
	}
	claimedAt, _ := time.Parse(time.RFC3339Nano, c.ClaimedAt)
	approvedAt, _ := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	if claimedAt.Before(approvedAt) {
		return fmt.Errorf("claimedAt is before approvedAt")
	}
	if expired, err := a.Expired(claimedAt); err != nil || expired {
		return fmt.Errorf("approval is expired at claim time")
	}
	if err := plan.ValidateTiming(p, claimedAt); err != nil {
		return fmt.Errorf("plan is not claimable: %w", err)
	}
	checkedAt, _ := time.Parse(time.RFC3339Nano, c.AdmissionBasis.CheckedAt)
	if checkedAt.Before(approvedAt) {
		return fmt.Errorf("admissionBasis.checkedAt is before approvedAt")
	}
	return nil
}

func ValidatePlanClaimIntegrity(c PlanClaim) error {
	if c.APIVersion != APIVersion || c.Kind != KindPlanClaim {
		return fmt.Errorf("claim identity is invalid")
	}
	if !claimIDPattern.MatchString(c.ClaimID) || !planIDPattern.MatchString(c.PlanID) || !approvalIDPattern.MatchString(c.ApprovalID) || !operationPattern.MatchString(c.OperationID) {
		return fmt.Errorf("claim, plan, approval, or operation ID is invalid")
	}
	for name, value := range map[string]string{
		"claimDigest": c.ClaimDigest, "planDigest": c.PlanDigest,
		"approvalDigest": c.ApprovalDigest, "configDigest": c.Basis.ConfigDigest,
		"managedStateDigest": c.Basis.ManagedStateDigest, "observedStateDigest": c.Basis.ObservedStateDigest,
	} {
		if !digestPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	for name, value := range map[string]string{
		"environmentId": c.EnvironmentID, "action": c.Action, "scope": c.Scope,
	} {
		if err := validateText(name, value, 256); err != nil {
			return err
		}
	}
	if c.Action != plan.ActionVMStart || !claimScopePattern.MatchString(c.Scope) {
		return fmt.Errorf("claim action or scope is invalid")
	}
	if err := validateCanonicalTime("claimedAt", c.ClaimedAt); err != nil {
		return err
	}
	if err := validateActor(c.Claimer, c.ClaimedAt); err != nil {
		return fmt.Errorf("claimer: %w", err)
	}
	if c.AdmissionBasis.PlanValidation != AdmissionPlanValid || c.AdmissionBasis.ApprovalValidation != AdmissionApprovalApproved || c.AdmissionBasis.EnvironmentValidation != AdmissionEnvironmentMatch || c.AdmissionBasis.DriftValidation != AdmissionDriftMatch {
		return fmt.Errorf("admissionBasis is invalid")
	}
	if err := validateCanonicalTime("admissionBasis.checkedAt", c.AdmissionBasis.CheckedAt); err != nil {
		return err
	}
	checkedAt, _ := time.Parse(time.RFC3339Nano, c.AdmissionBasis.CheckedAt)
	claimedAt, _ := time.Parse(time.RFC3339Nano, c.ClaimedAt)
	if checkedAt.After(claimedAt) {
		return fmt.Errorf("admissionBasis.checkedAt is after claimedAt")
	}
	if c.LockFencing != nil {
		if err := validateText("lockFencing.lockId", c.LockFencing.LockID, 256); err != nil {
			return err
		}
		if c.LockFencing.Token == 0 {
			return fmt.Errorf("lockFencing.token must be positive")
		}
	}
	expectedOperationID, err := DeriveOperationID(c.PlanID, c.ApprovalID, c.EnvironmentID, c.Action, c.Scope)
	if err != nil {
		return err
	}
	if c.OperationID != expectedOperationID {
		return fmt.Errorf("operationId does not match the claim admission tuple")
	}
	expectedDigest, err := c.ExpectedDigest()
	if err != nil {
		return err
	}
	if c.ClaimDigest != expectedDigest {
		return fmt.Errorf("claimDigest does not match claim semantics")
	}
	expectedID, err := c.ExpectedID()
	if err != nil {
		return err
	}
	if c.ClaimID != expectedID {
		return fmt.Errorf("claimId does not match claim digest")
	}
	return nil
}

func validateActor(actor ActorObservation, eventTime string) error {
	for name, value := range map[string]string{
		"subject": actor.Subject, "uid": actor.UID, "username": actor.Username,
		"hostname": actor.Hostname, "source": actor.Source, "authMethod": actor.AuthMethod,
	} {
		if err := validateText(name, value, 256); err != nil {
			return err
		}
	}
	if err := validateCanonicalTime("observedAt", actor.ObservedAt); err != nil {
		return err
	}
	if actor.ObservedAt != eventTime {
		return fmt.Errorf("observedAt must equal artifact event time")
	}
	return nil
}

func validateHumanPresence(presence approval.HumanPresence, eventTime string) error {
	if presence.Method != approval.PresenceMethodTyped {
		return fmt.Errorf("humanPresence.method is invalid")
	}
	if err := validateText("humanPresence.terminal", presence.Terminal, 512); err != nil {
		return err
	}
	if !digestPattern.MatchString(presence.ChallengeDigest) {
		return fmt.Errorf("humanPresence.challengeDigest is invalid")
	}
	if presence.ConfirmedAt != eventTime {
		return fmt.Errorf("humanPresence.confirmedAt must equal revokedAt")
	}
	return validateCanonicalTime("humanPresence.confirmedAt", presence.ConfirmedAt)
}

func validateCanonicalTime(name, value string) error {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return fmt.Errorf("%s is invalid: %w", name, err)
	}
	if value != parsed.UTC().Format(time.RFC3339Nano) {
		return fmt.Errorf("%s must be canonical UTC RFC3339", name)
	}
	return nil
}

func validateText(name, value string, max int) error {
	if strings.TrimSpace(value) == "" || value != strings.TrimSpace(value) || len(value) > max {
		return fmt.Errorf("%s is invalid", name)
	}
	return nil
}
