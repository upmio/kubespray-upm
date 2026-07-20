package plan

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/digest"
)

const (
	APIVersion = "upmctl.upm.io/v1alpha1"
	Kind       = "Plan"

	ActionVMStart = "vm.start"

	DispositionActionRequired = "ACTION_REQUIRED"
	DispositionNoop           = "NOOP"
	DispositionBlocked        = "BLOCKED"

	DefaultTTL = 30 * time.Minute
)

var (
	digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	planIDPattern = regexp.MustCompile(`^plan-[0-9a-f]{64}$`)
	nodePattern   = regexp.MustCompile(`^k8s-[1-8]$`)
)

type Plan struct {
	APIVersion          string   `json:"apiVersion"`
	Kind                string   `json:"kind"`
	PlanID              string   `json:"planId"`
	PlanDigest          string   `json:"planDigest"`
	EnvironmentID       string   `json:"environmentId"`
	Action              string   `json:"action"`
	Disposition         string   `json:"disposition"`
	CreatedAt           string   `json:"createdAt"`
	ExpiresAt           string   `json:"expiresAt"`
	RiskLevel           string   `json:"riskLevel"`
	Basis               Basis    `json:"basis"`
	Target              Target   `json:"target"`
	AffectedResources   []string `json:"affectedResources"`
	Preconditions       []string `json:"preconditions"`
	Blockers            []string `json:"blockers"`
	RejectionConditions []string `json:"rejectionConditions"`
	IrreversibleActions []string `json:"irreversibleActions"`
	DataImpact          []string `json:"dataImpact"`
	ExpectedDisruption  []string `json:"expectedDisruption"`
	ApprovalScope       string   `json:"approvalScope"`
	AcceptanceRefs      []string `json:"acceptanceRefs"`
	Steps               []Step   `json:"steps"`
}

type Basis struct {
	ConfigDigest        string `json:"configDigest"`
	ManagedStateDigest  string `json:"managedStateDigest"`
	ObservedStateDigest string `json:"observedStateDigest"`
}

type Target struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type Step struct {
	ID             string   `json:"id"`
	Code           string   `json:"code"`
	Resource       string   `json:"resource"`
	Postconditions []string `json:"postconditions"`
	AcceptanceRefs []string `json:"acceptanceRefs"`
}

type semanticPlan struct {
	APIVersion          string   `json:"apiVersion"`
	Kind                string   `json:"kind"`
	EnvironmentID       string   `json:"environmentId"`
	Action              string   `json:"action"`
	Disposition         string   `json:"disposition"`
	RiskLevel           string   `json:"riskLevel"`
	Basis               Basis    `json:"basis"`
	Target              Target   `json:"target"`
	AffectedResources   []string `json:"affectedResources"`
	Preconditions       []string `json:"preconditions"`
	Blockers            []string `json:"blockers"`
	RejectionConditions []string `json:"rejectionConditions"`
	IrreversibleActions []string `json:"irreversibleActions"`
	DataImpact          []string `json:"dataImpact"`
	ExpectedDisruption  []string `json:"expectedDisruption"`
	ApprovalScope       string   `json:"approvalScope"`
	AcceptanceRefs      []string `json:"acceptanceRefs"`
	Steps               []Step   `json:"steps"`
}

func (p Plan) semantic() semanticPlan {
	return semanticPlan{
		APIVersion: p.APIVersion, Kind: p.Kind, EnvironmentID: p.EnvironmentID,
		Action: p.Action, Disposition: p.Disposition, RiskLevel: p.RiskLevel,
		Basis: p.Basis, Target: p.Target, AffectedResources: nonNil(p.AffectedResources),
		Preconditions: nonNil(p.Preconditions), Blockers: nonNil(p.Blockers),
		RejectionConditions: nonNil(p.RejectionConditions), IrreversibleActions: nonNil(p.IrreversibleActions),
		DataImpact: nonNil(p.DataImpact), ExpectedDisruption: nonNil(p.ExpectedDisruption),
		ApprovalScope: p.ApprovalScope, AcceptanceRefs: nonNil(p.AcceptanceRefs), Steps: nonNilSteps(p.Steps),
	}
}

func (p Plan) ExpectedDigest() (string, error) {
	return digest.Sum(p.semantic())
}

// ExpectedID derives the immutable plan instance identity from the semantic
// digest and the exact validity window. This binds timestamps that are
// deliberately excluded from PlanDigest to PlanID instead.
func (p Plan) ExpectedID() (string, error) {
	instanceDigest, err := digest.Sum(struct {
		PlanDigest string `json:"planDigest"`
		CreatedAt  string `json:"createdAt"`
		ExpiresAt  string `json:"expiresAt"`
	}{PlanDigest: p.PlanDigest, CreatedAt: p.CreatedAt, ExpiresAt: p.ExpiresAt})
	if err != nil {
		return "", err
	}
	return "plan-" + strings.TrimPrefix(instanceDigest, "sha256:"), nil
}

func (p Plan) Expired(now time.Time) (bool, error) {
	expiresAt, err := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if err != nil {
		return false, fmt.Errorf("expiresAt is invalid: %w", err)
	}
	return !now.UTC().Before(expiresAt), nil
}

func Validate(p Plan) error {
	if p.APIVersion != APIVersion || p.Kind != Kind {
		return fmt.Errorf("plan identity is invalid")
	}
	if !planIDPattern.MatchString(p.PlanID) {
		return fmt.Errorf("planId is invalid")
	}
	if strings.TrimSpace(p.EnvironmentID) == "" || len(p.EnvironmentID) > 128 {
		return fmt.Errorf("environmentId is required")
	}
	if p.Action != ActionVMStart {
		return fmt.Errorf("unsupported plan action %q", p.Action)
	}
	if p.Target.Kind != "VirtualMachine" || !nodePattern.MatchString(p.Target.Name) {
		return fmt.Errorf("plan target is invalid")
	}
	if p.RiskLevel != "R0" && p.RiskLevel != "R1" && p.RiskLevel != "R2" && p.RiskLevel != "R3" {
		return fmt.Errorf("riskLevel is invalid")
	}
	for name, value := range map[string]string{
		"planDigest":          p.PlanDigest,
		"configDigest":        p.Basis.ConfigDigest,
		"managedStateDigest":  p.Basis.ManagedStateDigest,
		"observedStateDigest": p.Basis.ObservedStateDigest,
	} {
		if !digestPattern.MatchString(value) {
			return fmt.Errorf("%s is invalid", name)
		}
	}
	createdAt, err := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if err != nil {
		return fmt.Errorf("createdAt is invalid: %w", err)
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, p.ExpiresAt)
	if err != nil || !expiresAt.After(createdAt) {
		return fmt.Errorf("expiresAt is invalid")
	}
	if expiresAt.Sub(createdAt) != DefaultTTL {
		return fmt.Errorf("plan validity window must be exactly %s", DefaultTTL)
	}
	switch p.Disposition {
	case DispositionActionRequired:
		if len(p.Steps) == 0 || len(p.Blockers) != 0 || p.RiskLevel == "R0" {
			return fmt.Errorf("ACTION_REQUIRED plan has invalid steps, blockers, or risk")
		}
	case DispositionNoop:
		if len(p.Steps) != 0 || len(p.Blockers) != 0 || p.RiskLevel != "R0" {
			return fmt.Errorf("NOOP plan has invalid steps, blockers, or risk")
		}
	case DispositionBlocked:
		if len(p.Steps) != 0 || len(p.Blockers) == 0 || p.RiskLevel != "R0" {
			return fmt.Errorf("BLOCKED plan has invalid steps, blockers, or risk")
		}
	default:
		return fmt.Errorf("disposition is invalid")
	}
	expected, err := p.ExpectedDigest()
	if err != nil {
		return err
	}
	if p.PlanDigest != expected {
		return fmt.Errorf("planDigest does not match plan semantics")
	}
	expectedID, err := p.ExpectedID()
	if err != nil {
		return err
	}
	if p.PlanID != expectedID {
		return fmt.Errorf("planId does not match plan digest and validity window")
	}
	return nil
}

// ValidateTiming applies checks relative to a caller-supplied clock. Store
// reads intentionally use Validate only, so an immutable historical plan
// remains readable for audit after it expires.
func ValidateTiming(p Plan, now time.Time) error {
	if err := Validate(p); err != nil {
		return err
	}
	if now.IsZero() {
		return fmt.Errorf("validation time is required")
	}
	createdAt, _ := time.Parse(time.RFC3339Nano, p.CreatedAt)
	if createdAt.After(now.UTC()) {
		return fmt.Errorf("createdAt is in the future")
	}
	expired, err := p.Expired(now)
	if err != nil {
		return err
	}
	if expired {
		return fmt.Errorf("plan is expired")
	}
	return nil
}

func nonNil(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func nonNilSteps(values []Step) []Step {
	if values == nil {
		return []Step{}
	}
	return values
}
