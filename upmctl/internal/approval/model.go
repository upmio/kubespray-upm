// Package approval defines the immutable human-approval evidence bound to a
// closed, action-required Plan. It deliberately contains no filesystem, CLI,
// authentication, locking, or execution behavior.
package approval

import (
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/upmio/kubespray-upm/upmctl/internal/digest"
	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
)

const (
	APIVersion = plan.APIVersion
	Kind       = "Approval"

	DecisionApproved = "APPROVED"
	PolicyVersion    = "human-approval-v1"

	SourceHumanCLI           = "human-cli"
	AuthMethodInteractiveTTY = "interactive-tty"
	PresenceMethodTyped      = "typed-challenge"

	DefaultTTL = 10 * time.Minute
)

var (
	digestPattern     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	approvalIDPattern = regexp.MustCompile(`^approval-[0-9a-f]{64}$`)
	planIDPattern     = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)
	nodePattern       = regexp.MustCompile(`^k8s-[1-8]$`)
)

// Actor is the local operating-system identity reported by the CLI. It is
// audit context, not proof of an independently authenticated real-world user.
// Source and the policy-labelled authMethod are constants, not caller input.
type Actor struct {
	Subject  string
	UID      string
	Username string
	Hostname string
}

// Presence is the synchronous, locally observed terminal challenge evidence
// supplied to New. It does not establish an external identity. New records the
// confirmation at the exact approval timestamp.
type Presence struct {
	Terminal        string
	ChallengeDigest string
}

type Approver struct {
	Subject    string `json:"subject"`
	UID        string `json:"uid"`
	Username   string `json:"username"`
	Hostname   string `json:"hostname"`
	Source     string `json:"source"`
	AuthMethod string `json:"authMethod"`
}

type HumanPresence struct {
	Method          string `json:"method"`
	Terminal        string `json:"terminal"`
	ChallengeDigest string `json:"challengeDigest"`
	ConfirmedAt     string `json:"confirmedAt"`
}

type Approval struct {
	APIVersion     string        `json:"apiVersion"`
	Kind           string        `json:"kind"`
	ApprovalID     string        `json:"approvalId"`
	ApprovalDigest string        `json:"approvalDigest"`
	Decision       string        `json:"decision"`
	PlanID         string        `json:"planId"`
	PlanDigest     string        `json:"planDigest"`
	EnvironmentID  string        `json:"environmentId"`
	Action         string        `json:"action"`
	Target         plan.Target   `json:"target"`
	RiskLevel      string        `json:"riskLevel"`
	ApprovalScope  string        `json:"approvalScope"`
	Basis          plan.Basis    `json:"basis"`
	PolicyVersion  string        `json:"policyVersion"`
	ApprovedAt     string        `json:"approvedAt"`
	ExpiresAt      string        `json:"expiresAt"`
	Approver       Approver      `json:"approver"`
	HumanPresence  HumanPresence `json:"humanPresence"`
	Reason         string        `json:"reason"`
	RequestID      string        `json:"requestId"`
	CLIVersion     string        `json:"cliVersion"`
}

type semanticApproval struct {
	APIVersion    string        `json:"apiVersion"`
	Kind          string        `json:"kind"`
	Decision      string        `json:"decision"`
	PlanID        string        `json:"planId"`
	PlanDigest    string        `json:"planDigest"`
	EnvironmentID string        `json:"environmentId"`
	Action        string        `json:"action"`
	Target        plan.Target   `json:"target"`
	RiskLevel     string        `json:"riskLevel"`
	ApprovalScope string        `json:"approvalScope"`
	Basis         plan.Basis    `json:"basis"`
	PolicyVersion string        `json:"policyVersion"`
	ApprovedAt    string        `json:"approvedAt"`
	ExpiresAt     string        `json:"expiresAt"`
	Approver      Approver      `json:"approver"`
	HumanPresence HumanPresence `json:"humanPresence"`
	Reason        string        `json:"reason"`
	RequestID     string        `json:"requestId"`
	CLIVersion    string        `json:"cliVersion"`
}

func New(p plan.Plan, actor Actor, presence Presence, reason, requestID, cliVersion string, now time.Time) (Approval, error) {
	if now.IsZero() {
		return Approval{}, fmt.Errorf("approval time is required")
	}
	approvedAt := now.UTC()
	if err := validateApprovablePlan(p, approvedAt); err != nil {
		return Approval{}, err
	}
	if err := validateActor(actor); err != nil {
		return Approval{}, err
	}
	if err := validateText("terminal", presence.Terminal, 256); err != nil {
		return Approval{}, err
	}
	if !digestPattern.MatchString(presence.ChallengeDigest) {
		return Approval{}, fmt.Errorf("challengeDigest is invalid")
	}
	if err := validateText("reason", reason, 1024); err != nil {
		return Approval{}, err
	}
	if err := validateText("requestId", requestID, 128); err != nil {
		return Approval{}, err
	}
	if err := validateText("cliVersion", cliVersion, 128); err != nil {
		return Approval{}, err
	}

	planExpiresAt, _ := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	expiresAt := approvedAt.Add(DefaultTTL)
	if planExpiresAt.Before(expiresAt) {
		expiresAt = planExpiresAt
	}
	approvedAtText := approvedAt.Format(time.RFC3339Nano)
	result := Approval{
		APIVersion: APIVersion, Kind: Kind, Decision: DecisionApproved,
		PlanID: p.PlanID, PlanDigest: p.PlanDigest, EnvironmentID: p.EnvironmentID,
		Action: p.Action, Target: p.Target, RiskLevel: p.RiskLevel,
		ApprovalScope: p.ApprovalScope, Basis: p.Basis, PolicyVersion: PolicyVersion,
		ApprovedAt: approvedAtText, ExpiresAt: expiresAt.UTC().Format(time.RFC3339Nano),
		Approver: Approver{
			Subject: actor.Subject, UID: actor.UID, Username: actor.Username,
			Hostname: actor.Hostname, Source: SourceHumanCLI,
			AuthMethod: AuthMethodInteractiveTTY,
		},
		HumanPresence: HumanPresence{
			Method: PresenceMethodTyped, Terminal: presence.Terminal,
			ChallengeDigest: presence.ChallengeDigest, ConfirmedAt: approvedAtText,
		},
		Reason: reason, RequestID: requestID, CLIVersion: cliVersion,
	}

	approvalDigest, err := result.ExpectedDigest()
	if err != nil {
		return Approval{}, err
	}
	result.ApprovalDigest = approvalDigest
	result.ApprovalID, err = result.ExpectedID()
	if err != nil {
		return Approval{}, err
	}
	if err := Validate(result, p); err != nil {
		return Approval{}, err
	}
	return result, nil
}

func (a Approval) semantic() semanticApproval {
	return semanticApproval{
		APIVersion: a.APIVersion, Kind: a.Kind, Decision: a.Decision,
		PlanID: a.PlanID, PlanDigest: a.PlanDigest, EnvironmentID: a.EnvironmentID,
		Action: a.Action, Target: a.Target, RiskLevel: a.RiskLevel,
		ApprovalScope: a.ApprovalScope, Basis: a.Basis, PolicyVersion: a.PolicyVersion,
		ApprovedAt: a.ApprovedAt, ExpiresAt: a.ExpiresAt, Approver: a.Approver,
		HumanPresence: a.HumanPresence, Reason: a.Reason, RequestID: a.RequestID,
		CLIVersion: a.CLIVersion,
	}
}

func (a Approval) ExpectedDigest() (string, error) {
	return digest.Sum(a.semantic())
}

func (a Approval) ExpectedID() (string, error) {
	if !digestPattern.MatchString(a.ApprovalDigest) {
		return "", fmt.Errorf("approvalDigest is invalid")
	}
	return "approval-" + strings.TrimPrefix(a.ApprovalDigest, "sha256:"), nil
}

// Validate verifies both the approval artifact and its immutable binding to p.
// It intentionally remains valid for historical audit after expiration.
func Validate(a Approval, p plan.Plan) error {
	if err := ValidateIntegrity(a); err != nil {
		return err
	}
	approvedAt, _ := time.Parse(time.RFC3339Nano, a.ApprovedAt)
	if err := validateApprovablePlan(p, approvedAt); err != nil {
		return err
	}
	planExpiresAt, _ := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	wantExpiresAt := approvedAt.Add(DefaultTTL)
	if planExpiresAt.Before(wantExpiresAt) {
		wantExpiresAt = planExpiresAt
	}
	expiresAt, _ := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if !expiresAt.Equal(wantExpiresAt) {
		return fmt.Errorf("approval validity window must be min(%s, plan expiry)", DefaultTTL)
	}
	if a.PlanID != p.PlanID || a.PlanDigest != p.PlanDigest ||
		a.EnvironmentID != p.EnvironmentID || a.Action != p.Action ||
		a.Target != p.Target || a.RiskLevel != p.RiskLevel ||
		a.ApprovalScope != p.ApprovalScope || a.Basis != p.Basis {
		return fmt.Errorf("approval does not match bound plan")
	}
	return nil
}

// ValidateIntegrity validates a standalone Approval artifact without needing
// its Plan. Validate must additionally be used before consuming the approval
// for a particular Plan.
func ValidateIntegrity(a Approval) error {
	if a.APIVersion != APIVersion || a.Kind != Kind {
		return fmt.Errorf("approval identity is invalid")
	}
	if !approvalIDPattern.MatchString(a.ApprovalID) {
		return fmt.Errorf("approvalId is invalid")
	}
	if !digestPattern.MatchString(a.ApprovalDigest) {
		return fmt.Errorf("approvalDigest is invalid")
	}
	if !planIDPattern.MatchString(a.PlanID) || !digestPattern.MatchString(a.PlanDigest) {
		return fmt.Errorf("bound plan identity is invalid")
	}
	if a.Decision != DecisionApproved || a.PolicyVersion != PolicyVersion {
		return fmt.Errorf("approval decision or policy is invalid")
	}
	if err := validateText("environmentId", a.EnvironmentID, 128); err != nil {
		return err
	}
	if a.Action != plan.ActionVMStart || a.Target.Kind != "VirtualMachine" || !nodePattern.MatchString(a.Target.Name) {
		return fmt.Errorf("approval action or target is invalid")
	}
	if a.RiskLevel != "R1" && a.RiskLevel != "R2" && a.RiskLevel != "R3" {
		return fmt.Errorf("approval riskLevel is invalid")
	}
	if a.ApprovalScope != a.Action+":"+a.Target.Name {
		return fmt.Errorf("approvalScope is invalid")
	}
	for name, value := range map[string]string{
		"configDigest":        a.Basis.ConfigDigest,
		"managedStateDigest":  a.Basis.ManagedStateDigest,
		"observedStateDigest": a.Basis.ObservedStateDigest,
	} {
		if !digestPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}

	approvedAt, err := parseCanonicalTime("approvedAt", a.ApprovedAt)
	if err != nil {
		return err
	}
	expiresAt, err := parseCanonicalTime("expiresAt", a.ExpiresAt)
	if err != nil || !expiresAt.After(approvedAt) {
		return fmt.Errorf("expiresAt is invalid")
	}
	confirmedAt, err := parseCanonicalTime("confirmedAt", a.HumanPresence.ConfirmedAt)
	if err != nil {
		return err
	}
	if !confirmedAt.Equal(approvedAt) {
		return fmt.Errorf("confirmedAt must equal approvedAt")
	}
	if expiresAt.Sub(approvedAt) > DefaultTTL {
		return fmt.Errorf("approval validity window exceeds %s", DefaultTTL)
	}
	if err := validateApprover(a.Approver); err != nil {
		return err
	}
	if a.HumanPresence.Method != PresenceMethodTyped {
		return fmt.Errorf("human presence method is invalid")
	}
	if err := validateText("terminal", a.HumanPresence.Terminal, 256); err != nil {
		return err
	}
	if !digestPattern.MatchString(a.HumanPresence.ChallengeDigest) {
		return fmt.Errorf("challengeDigest is invalid")
	}
	if err := validateText("reason", a.Reason, 1024); err != nil {
		return err
	}
	if err := validateText("requestId", a.RequestID, 128); err != nil {
		return err
	}
	if err := validateText("cliVersion", a.CLIVersion, 128); err != nil {
		return err
	}

	expectedDigest, err := a.ExpectedDigest()
	if err != nil {
		return err
	}
	if a.ApprovalDigest != expectedDigest {
		return fmt.Errorf("approvalDigest does not match approval semantics")
	}
	expectedID, err := a.ExpectedID()
	if err != nil {
		return err
	}
	if a.ApprovalID != expectedID {
		return fmt.Errorf("approvalId does not match approval digest")
	}
	return nil
}

func (a Approval) Expired(now time.Time) (bool, error) {
	expiresAt, err := time.Parse(time.RFC3339Nano, a.ExpiresAt)
	if err != nil {
		return false, fmt.Errorf("expiresAt is invalid: %w", err)
	}
	return !now.UTC().Before(expiresAt), nil
}

func validateApprovablePlan(p plan.Plan, at time.Time) error {
	if p.Disposition != plan.DispositionActionRequired {
		return fmt.Errorf("only ACTION_REQUIRED plans can be approved")
	}
	if p.RiskLevel != "R1" && p.RiskLevel != "R2" && p.RiskLevel != "R3" {
		return fmt.Errorf("only R1-R3 plans can be approved")
	}
	if err := plan.ValidateTiming(p, at); err != nil {
		return fmt.Errorf("plan is not approvable: %w", err)
	}
	if err := plan.ValidateActionContract(p); err != nil {
		return fmt.Errorf("plan action contract is not approvable: %w", err)
	}
	return nil
}

func validateActor(actor Actor) error {
	for _, field := range []struct {
		name  string
		value string
		max   int
	}{
		{"subject", actor.Subject, 256},
		{"uid", actor.UID, 128},
		{"username", actor.Username, 128},
		{"hostname", actor.Hostname, 255},
	} {
		if err := validateText(field.name, field.value, field.max); err != nil {
			return err
		}
	}
	return nil
}

func validateApprover(approver Approver) error {
	if err := validateActor(Actor{
		Subject: approver.Subject, UID: approver.UID,
		Username: approver.Username, Hostname: approver.Hostname,
	}); err != nil {
		return err
	}
	if approver.Source != SourceHumanCLI || approver.AuthMethod != AuthMethodInteractiveTTY {
		return fmt.Errorf("approver source or authentication method is invalid")
	}
	return nil
}

func validateText(name, value string, max int) error {
	if value == "" || strings.TrimSpace(value) != value {
		return fmt.Errorf("%s is required and must not have surrounding whitespace", name)
	}
	if len(value) > max {
		return fmt.Errorf("%s exceeds maximum length %d", name, max)
	}
	for _, r := range value {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s contains a control character", name)
		}
	}
	return nil
}

func parseCanonicalTime(name, value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s is invalid: %w", name, err)
	}
	if value != parsed.UTC().Format(time.RFC3339Nano) {
		return time.Time{}, fmt.Errorf("%s must use canonical UTC RFC3339Nano", name)
	}
	return parsed, nil
}
