// Package admission defines immutable admission-control artifacts. It contains
// no persistence or execution code; stores and executors must enforce their
// own atomicity and locking guarantees around these values.
package admission

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/digest"
	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
)

const (
	APIVersion = "upmctl.upm.io/v1alpha1"

	KindApprovalRevocation = "ApprovalRevocation"
	KindPlanClaim          = "PlanClaim"

	DispositionRevoked = "REVOKED"

	AdmissionPlanValid        = "VALID"
	AdmissionApprovalApproved = "APPROVED"
	AdmissionEnvironmentMatch = "MATCHED"
	AdmissionDriftMatch       = "MATCHED"
)

var (
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	planIDPattern     = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)
	approvalIDPattern = regexp.MustCompile(`^approval-[0-9a-f]{64}$`)
	revocationPattern = regexp.MustCompile(`^revocation-[0-9a-f]{64}$`)
	claimIDPattern    = regexp.MustCompile(`^claim-[0-9a-f]{64}$`)
	operationPattern  = regexp.MustCompile(`^operation-[0-9a-f]{64}$`)
	claimScopePattern = regexp.MustCompile(`^vm\.start:k8s-[1-8]$`)
)

// ActorObservation is an audit observation of the local principal that
// created an admission artifact. It is evidence reported by the local host,
// not a cryptographic identity assertion.
type ActorObservation struct {
	Subject    string `json:"subject"`
	UID        string `json:"uid"`
	Username   string `json:"username"`
	Hostname   string `json:"hostname"`
	Source     string `json:"source"`
	AuthMethod string `json:"authMethod"`
	ObservedAt string `json:"observedAt"`
}

// ApprovalRevocation is an immutable event that invalidates an Approval
// before it has been consumed by a PlanClaim. It never overwrites Approval.
type ApprovalRevocation struct {
	APIVersion       string                 `json:"apiVersion"`
	Kind             string                 `json:"kind"`
	RevocationID     string                 `json:"revocationId"`
	RevocationDigest string                 `json:"revocationDigest"`
	ApprovalID       string                 `json:"approvalId"`
	ApprovalDigest   string                 `json:"approvalDigest"`
	PlanID           string                 `json:"planId"`
	PlanDigest       string                 `json:"planDigest"`
	EnvironmentID    string                 `json:"environmentId"`
	RevokedAt        string                 `json:"revokedAt"`
	Actor            ActorObservation       `json:"actor"`
	HumanPresence    approval.HumanPresence `json:"humanPresence"`
	Reason           string                 `json:"reason"`
	Disposition      string                 `json:"disposition"`
}

type semanticRevocation struct {
	APIVersion     string                 `json:"apiVersion"`
	Kind           string                 `json:"kind"`
	ApprovalID     string                 `json:"approvalId"`
	ApprovalDigest string                 `json:"approvalDigest"`
	PlanID         string                 `json:"planId"`
	PlanDigest     string                 `json:"planDigest"`
	EnvironmentID  string                 `json:"environmentId"`
	RevokedAt      string                 `json:"revokedAt"`
	Actor          ActorObservation       `json:"actor"`
	HumanPresence  approval.HumanPresence `json:"humanPresence"`
	Reason         string                 `json:"reason"`
	Disposition    string                 `json:"disposition"`
}

func (r ApprovalRevocation) semantic() semanticRevocation {
	return semanticRevocation{
		APIVersion: r.APIVersion, Kind: r.Kind, ApprovalID: r.ApprovalID,
		ApprovalDigest: r.ApprovalDigest, PlanID: r.PlanID, PlanDigest: r.PlanDigest,
		EnvironmentID: r.EnvironmentID, RevokedAt: r.RevokedAt, Actor: r.Actor,
		HumanPresence: r.HumanPresence, Reason: r.Reason, Disposition: r.Disposition,
	}
}

func (r ApprovalRevocation) ExpectedDigest() (string, error) {
	return digest.Sum(r.semantic())
}

func (r ApprovalRevocation) ExpectedID() (string, error) {
	value, err := r.ExpectedDigest()
	if err != nil {
		return "", err
	}
	return "revocation-" + strings.TrimPrefix(value, "sha256:"), nil
}

// NewApprovalRevocation binds a revocation event to one exact Approval and
// Plan instance. The caller is responsible for proving interactive presence;
// this constructor records, but cannot independently establish, that proof.
func NewApprovalRevocation(a approval.Approval, p plan.Plan, actor approval.Actor, presence approval.Presence, reason string, now time.Time) (ApprovalRevocation, error) {
	var result ApprovalRevocation
	if now.IsZero() {
		return result, fmt.Errorf("revocation time is required")
	}
	result = ApprovalRevocation{
		APIVersion: APIVersion, Kind: KindApprovalRevocation,
		ApprovalID: a.ApprovalID, ApprovalDigest: a.ApprovalDigest,
		PlanID: p.PlanID, PlanDigest: p.PlanDigest, EnvironmentID: p.EnvironmentID,
		RevokedAt: now.UTC().Format(time.RFC3339Nano),
		Actor: ActorObservation{
			Subject: actor.Subject, UID: actor.UID, Username: actor.Username, Hostname: actor.Hostname,
			Source: approval.SourceHumanCLI, AuthMethod: approval.AuthMethodInteractiveTTY,
		},
		HumanPresence: approval.HumanPresence{
			Method: approval.PresenceMethodTyped, Terminal: presence.Terminal,
			ChallengeDigest: presence.ChallengeDigest,
		},
		Reason: reason, Disposition: DispositionRevoked,
	}
	result.Actor.ObservedAt = result.RevokedAt
	result.HumanPresence.ConfirmedAt = result.RevokedAt
	d, err := result.ExpectedDigest()
	if err != nil {
		return ApprovalRevocation{}, err
	}
	result.RevocationDigest = d
	result.RevocationID, err = result.ExpectedID()
	if err != nil {
		return ApprovalRevocation{}, err
	}
	if err := ValidateApprovalRevocation(result, a, p); err != nil {
		return ApprovalRevocation{}, err
	}
	return result, nil
}

// AdmissionBasis captures the exact successful admission checks asserted by
// the future apply path when it creates a claim under an environment lock.
type AdmissionBasis struct {
	PlanValidation        string `json:"planValidation"`
	ApprovalValidation    string `json:"approvalValidation"`
	EnvironmentValidation string `json:"environmentValidation"`
	DriftValidation       string `json:"driftValidation"`
	CheckedAt             string `json:"checkedAt"`
}

// LockFencing optionally records the environment-lock fencing generation.
// It remains optional until the lock implementation is delivered; once
// populated, both fields are integrity-protected by ClaimDigest.
type LockFencing struct {
	LockID string `json:"lockId"`
	Token  uint64 `json:"token"`
}

// PlanClaim permanently consumes one Plan/Approval pair. Persistence must use
// the Plan ID as its uniqueness boundary so a claimed Plan can never be
// admitted into a second operation, including after interruption or failure.
type PlanClaim struct {
	APIVersion     string           `json:"apiVersion"`
	Kind           string           `json:"kind"`
	ClaimID        string           `json:"claimId"`
	ClaimDigest    string           `json:"claimDigest"`
	PlanID         string           `json:"planId"`
	PlanDigest     string           `json:"planDigest"`
	ApprovalID     string           `json:"approvalId"`
	ApprovalDigest string           `json:"approvalDigest"`
	EnvironmentID  string           `json:"environmentId"`
	Action         string           `json:"action"`
	Scope          string           `json:"scope"`
	Basis          plan.Basis       `json:"basis"`
	OperationID    string           `json:"operationId"`
	ClaimedAt      string           `json:"claimedAt"`
	Claimer        ActorObservation `json:"claimer"`
	AdmissionBasis AdmissionBasis   `json:"admissionBasis"`
	LockFencing    *LockFencing     `json:"lockFencing,omitempty"`
}

type semanticClaim struct {
	APIVersion     string           `json:"apiVersion"`
	Kind           string           `json:"kind"`
	PlanID         string           `json:"planId"`
	PlanDigest     string           `json:"planDigest"`
	ApprovalID     string           `json:"approvalId"`
	ApprovalDigest string           `json:"approvalDigest"`
	EnvironmentID  string           `json:"environmentId"`
	Action         string           `json:"action"`
	Scope          string           `json:"scope"`
	Basis          plan.Basis       `json:"basis"`
	OperationID    string           `json:"operationId"`
	ClaimedAt      string           `json:"claimedAt"`
	Claimer        ActorObservation `json:"claimer"`
	AdmissionBasis AdmissionBasis   `json:"admissionBasis"`
	LockFencing    *LockFencing     `json:"lockFencing,omitempty"`
}

func (c PlanClaim) semantic() semanticClaim {
	return semanticClaim{
		APIVersion: c.APIVersion, Kind: c.Kind, PlanID: c.PlanID,
		PlanDigest: c.PlanDigest, ApprovalID: c.ApprovalID, ApprovalDigest: c.ApprovalDigest,
		EnvironmentID: c.EnvironmentID, Action: c.Action, Scope: c.Scope, Basis: c.Basis,
		OperationID: c.OperationID, ClaimedAt: c.ClaimedAt, Claimer: c.Claimer,
		AdmissionBasis: c.AdmissionBasis, LockFencing: c.LockFencing,
	}
}

func (c PlanClaim) ExpectedDigest() (string, error) {
	return digest.Sum(c.semantic())
}

func (c PlanClaim) ExpectedID() (string, error) {
	value, err := c.ExpectedDigest()
	if err != nil {
		return "", err
	}
	return "claim-" + strings.TrimPrefix(value, "sha256:"), nil
}

// DeriveOperationID deterministically names the sole operation allowed to
// consume an exact Plan/Approval admission tuple.
func DeriveOperationID(planID, approvalID, environmentID, action, scope string) (string, error) {
	if !planIDPattern.MatchString(planID) {
		return "", fmt.Errorf("planId is invalid")
	}
	if !approvalIDPattern.MatchString(approvalID) {
		return "", fmt.Errorf("approvalId is invalid")
	}
	for name, value := range map[string]string{
		"planId": planID, "approvalId": approvalID, "environmentId": environmentID,
		"action": action, "scope": scope,
	} {
		if strings.TrimSpace(value) == "" {
			return "", fmt.Errorf("%s is required", name)
		}
	}
	value, err := digest.Sum(struct {
		PlanID        string `json:"planId"`
		ApprovalID    string `json:"approvalId"`
		EnvironmentID string `json:"environmentId"`
		Action        string `json:"action"`
		Scope         string `json:"scope"`
	}{planID, approvalID, environmentID, action, scope})
	if err != nil {
		return "", err
	}
	return "operation-" + strings.TrimPrefix(value, "sha256:"), nil
}

// NewPlanClaim builds the immutable, one-shot claim for a valid Plan and
// Approval pair. Store-level create-if-absent semantics provide the permanent
// anti-replay guarantee; this model binds every value relevant to admission.
func NewPlanClaim(p plan.Plan, a approval.Approval, claimer ActorObservation, basis AdmissionBasis, fencing *LockFencing, now time.Time) (PlanClaim, error) {
	var result PlanClaim
	if now.IsZero() {
		return result, fmt.Errorf("claim time is required")
	}
	operationID, err := DeriveOperationID(p.PlanID, a.ApprovalID, p.EnvironmentID, p.Action, p.ApprovalScope)
	if err != nil {
		return result, err
	}
	result = PlanClaim{
		APIVersion: APIVersion, Kind: KindPlanClaim,
		PlanID: p.PlanID, PlanDigest: p.PlanDigest,
		ApprovalID: a.ApprovalID, ApprovalDigest: a.ApprovalDigest,
		EnvironmentID: p.EnvironmentID, Action: p.Action, Scope: p.ApprovalScope,
		Basis: p.Basis, OperationID: operationID, ClaimedAt: now.UTC().Format(time.RFC3339Nano),
		Claimer: claimer, AdmissionBasis: basis, LockFencing: cloneFencing(fencing),
	}
	result.Claimer.ObservedAt = result.ClaimedAt
	d, err := result.ExpectedDigest()
	if err != nil {
		return PlanClaim{}, err
	}
	result.ClaimDigest = d
	result.ClaimID, err = result.ExpectedID()
	if err != nil {
		return PlanClaim{}, err
	}
	if err := ValidatePlanClaim(result, a, p); err != nil {
		return PlanClaim{}, err
	}
	return result, nil
}

func cloneFencing(value *LockFencing) *LockFencing {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
