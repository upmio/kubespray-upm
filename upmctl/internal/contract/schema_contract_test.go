package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/upmio/kubespray-upm/upmctl/internal/admission"
	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	upmlogging "github.com/upmio/kubespray-upm/upmctl/internal/logging"
	"github.com/upmio/kubespray-upm/upmctl/internal/managedenv"
	upmplan "github.com/upmio/kubespray-upm/upmctl/internal/plan"
	upmreadiness "github.com/upmio/kubespray-upm/upmctl/internal/readiness"
	upmstatus "github.com/upmio/kubespray-upm/upmctl/internal/status"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

func TestManagedEnvironmentAdoptionConformsToSchema(t *testing.T) {
	state := managedenv.State{
		APIVersion: managedenv.APIVersion, Kind: managedenv.Kind, EnvironmentID: "env-contract", Workspace: "/workspace",
		Files: map[string]string{
			"Vagrantfile":       "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			"vagrant/config.rb": "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		},
		Machines: map[string]string{
			"k8s-1": "11111111-1111-4111-8111-111111111111",
			"k8s-2": "22222222-2222-4222-8222-222222222222",
			"k8s-3": "33333333-3333-4333-8333-333333333333",
		},
		Adoption: managedenv.Adoption{
			AdoptedAt:     "2026-07-17T12:00:00Z",
			Actor:         managedenv.Actor{Subject: "os-user:1000", UID: "1000", Username: "operator", Hostname: "host", Source: "human-cli", AuthMethod: "interactive-tty"},
			HumanPresence: managedenv.HumanPresence{Method: "typed-challenge", Terminal: "/dev/tty", ChallengeDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", ConfirmedAt: "2026-07-17T12:00:00Z"},
			Reason:        "verified legacy identities", RequestID: "req-contract", CLIVersion: "0.1.0-test",
		},
	}
	validateDocument(t, "managed-environment.schema.json", state)
}

func TestEnvironmentStatusConformsToSchema(t *testing.T) {
	value := upmstatus.Environment{
		Mode: "managed", Health: "DEGRADED", ObservationComplete: false,
		RepositoryRoot: "/repo", Workspace: "/workspace",
		Config:       upmstatus.ConfigStatus{Present: true, Safe: true, Complete: true, Valid: true, Digest: "sha256:test", Status: "SAFE_COMPLETE"},
		ManagedState: upmstatus.ManagedStatus{Present: true, Valid: true, Trust: upmcontext.TrustManagedValid},
		Cluster:      upmstatus.ClusterStatus{APIState: "reachable", KubeconfigPresent: true, ExpectedNodes: 1, VagrantMachines: 1, LibvirtDomains: 1, KubernetesNodes: 1},
		VMSummary:    upmstatus.VMSummary{Expected: 1, Degraded: 1},
		Findings:     []upmstatus.Finding{{Code: "SSH_NOT_CHECKED", Severity: "warning", Scope: "ssh", Resource: "k8s-1", Message: "SSH reachability is not checked in Phase 1b"}},
	}
	validateDocument(t, "observed-state.schema.json", value)
}

func TestVMInspectionConformsToSchema(t *testing.T) {
	value := vm.Machine{
		Name: "k8s-1", Index: 1, Expected: true, Managed: true,
		Role: "control-plane-etcd-worker", Health: "RUNNING_DEGRADED", Consistency: "degraded",
		VagrantState: "running", LibvirtID: "11111111-1111-4111-8111-111111111111", LibvirtState: "running", KubernetesState: "ready",
		Identity:   vm.Identity{VagrantMachine: "k8s-1", LibvirtUUID: "11111111-1111-4111-8111-111111111111", DomainName: "env_k8s-1", KubernetesNode: "k8s-1", KubernetesNodeUID: "uid-1"},
		Power:      vm.Power{Desired: "unknown", Vagrant: "running", Libvirt: "running"},
		Network:    vm.Network{Addresses: []string{"192.168.200.101"}, InternalIP: "192.168.200.101", SSHHost: "192.168.200.101", SSHPort: 22, SSHState: "endpoint-configured"},
		Kubernetes: vm.Kubernetes{Present: true, Ready: true, State: "ready"},
		Resources:  vm.Resources{CPU: 4, MemoryMiB: 4096, DataDisks: 0, ObservedDisks: []vm.Disk{{Type: "file", Device: "disk", Target: "vda", Source: "/var/lib/libvirt/images/node.img"}}},
		Sources:    map[string]string{"config": "declared", "vagrant": "observed", "libvirt": "observed", "libvirtInventory": "observed", "libvirtInfo": "observed", "blockDevices": "observed", "kubernetes": "observed", "ssh": "endpoint-configured"},
		Findings:   []vm.Finding{{Code: "SSH_REACHABILITY_NOT_CHECKED", Severity: "warning", Source: "ssh", Resource: "k8s-1", Message: "only endpoint metadata is observed"}},
	}
	validateDocument(t, "vm-inspection.schema.json", value)
}

func TestRuntimeLogEventsConformToSchema(t *testing.T) {
	exitCode := 3
	errorCode := "UPMCTL_USAGE"
	validateDocument(t, "runtime-log-event.schema.json", upmlogging.Event{
		LogVersion: "upmctl.runtime/v1", Timestamp: "2026-07-17T01:02:03Z",
		RequestID: "req-runtime-log", Command: "approval grant", Event: "start",
	})
	validateDocument(t, "runtime-log-event.schema.json", upmlogging.Event{
		LogVersion: "upmctl.runtime/v1", Timestamp: "2026-07-17T01:02:04Z",
		RequestID: "req-runtime-log", Command: "approval grant", Event: "error",
		ExitCode: &exitCode, ErrorCode: &errorCode,
	})

	invalid := asJSONDocument(t, upmlogging.Event{
		LogVersion: "upmctl.runtime/v1", Timestamp: "2026-07-17T01:02:03Z",
		RequestID: "req-runtime-log", Command: "approval grant secret", Event: "start",
	})
	if err := validateSchemaDocument(t, "runtime-log-event.schema.json", invalid); err == nil {
		t.Fatal("runtime log schema accepted a command containing an argument value")
	}
}

func TestPhase2PlanConformsToSchema(t *testing.T) {
	validateDocument(t, "plan.schema.json", representativePlan())
}

func TestPhase2bReadinessArtifactsConformToSchemas(t *testing.T) {
	candidate := validRepresentativePlan(t)
	now := time.Date(2026, 7, 17, 0, 10, 0, 0, time.UTC)
	current := upmreadiness.CurrentState{
		EnvironmentID:       candidate.EnvironmentID,
		ConfigDigest:        candidate.Basis.ConfigDigest,
		ConfigStatus:        upmreadiness.CurrentStateValid,
		ManagedStateDigest:  candidate.Basis.ManagedStateDigest,
		ManagedStateStatus:  upmreadiness.CurrentStateValid,
		ObservedStateDigest: candidate.Basis.ObservedStateDigest,
		ObservedStateStatus: upmreadiness.CurrentStateValid,
		ObservedStateSafe:   true,
	}

	inspection, err := upmreadiness.BuildInspection(candidate, now)
	if err != nil {
		t.Fatal(err)
	}
	validateDocument(t, "plan-inspection.schema.json", inspection)
	validateDocument(t, "plan-validation.schema.json", upmreadiness.BuildValidation(upmreadiness.ValidationInput{
		Plan: candidate, Now: now, Current: current,
	}))
	validateDocument(t, "preflight-result.schema.json", upmreadiness.BuildPreflight(upmreadiness.PreflightInput{
		Plan: candidate, Now: now, Current: current,
	}))
}

func TestPhase2b2aApprovalArtifactsConformToSchemas(t *testing.T) {
	candidate := validRepresentativePlan(t)
	approvedAt := time.Date(2026, 7, 17, 0, 10, 0, 0, time.UTC)
	value, err := approval.New(candidate, approval.Actor{
		Subject: "os-user:501", UID: "501", Username: "operator", Hostname: "control-host",
	}, approval.Presence{
		Terminal: "/dev/tty", ChallengeDigest: "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
	}, "reviewed immutable plan impact", "req-contract", "0.1.0-test", approvedAt)
	if err != nil {
		t.Fatal(err)
	}
	validateDocument(t, "approval.schema.json", value)

	revokedAt := approvedAt.Add(time.Minute)
	revocation, err := admission.NewApprovalRevocation(value, candidate, approval.Actor{
		Subject: "os-user:501", UID: "501", Username: "operator", Hostname: "control-host",
	}, approval.Presence{
		Terminal:        "/dev/tty",
		ChallengeDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}, "approval no longer intended", revokedAt)
	if err != nil {
		t.Fatal(err)
	}
	validateDocument(t, "approval-revocation.schema.json", revocation)
	validateDocument(t, "admission.schema.json", revocation)

	claimedAt := approvedAt.Add(2 * time.Minute)
	claim, err := admission.NewPlanClaim(candidate, value, admission.ActorObservation{
		Subject: "upmctl-apply", UID: "501", Username: "operator", Hostname: "control-host",
		Source: "application-service", AuthMethod: "internal-admission",
	}, admission.AdmissionBasis{
		PlanValidation: admission.AdmissionPlanValid, ApprovalValidation: admission.AdmissionApprovalApproved,
		EnvironmentValidation: admission.AdmissionEnvironmentMatch, DriftValidation: admission.AdmissionDriftMatch,
		CheckedAt: approvedAt.Add(90 * time.Second).Format(time.RFC3339Nano),
	}, nil, claimedAt)
	if err != nil {
		t.Fatal(err)
	}
	validateDocument(t, "plan-claim.schema.json", claim)
	validateDocument(t, "admission.schema.json", claim)

	inspection := app.ApprovalInspection{
		APIVersion: approval.APIVersion, Kind: "ApprovalInspection",
		CheckedAt: approvedAt.Add(30 * time.Second).Format(time.RFC3339Nano),
		Approval:  value, Status: upmreadiness.ApprovalApproved, ExecutionAvailable: false,
	}
	validateDocument(t, "approval-status.schema.json", inspection)
	validateDocument(t, "approval-status.schema.json", struct {
		APIVersion         string                   `json:"apiVersion"`
		Kind               string                   `json:"kind"`
		CheckedAt          string                   `json:"checkedAt"`
		Items              []app.ApprovalInspection `json:"items"`
		ExecutionAvailable bool                     `json:"executionAvailable"`
	}{
		APIVersion: approval.APIVersion, Kind: "ApprovalInspectionList",
		CheckedAt: approvedAt.Add(30 * time.Second).Format(time.RFC3339Nano),
		Items:     []app.ApprovalInspection{inspection}, ExecutionAvailable: false,
	})
}

func TestCodexSkillEnforcesPhase2b2aHumanBoundary(t *testing.T) {
	root := repositoryRoot(t)
	skillPath := filepath.Join(root, "upmctl", "skills", "upmctl-environment", "SKILL.md")
	contents, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	text := string(contents)
	for _, required := range []string{
		"name: upmctl-environment",
		"capabilities -> context discover -> status -> plan -> preflight -> explain",
		"Never execute or automate either command",
		"Never execute or automate environment adoption",
		"upmctl environment adopt --environment-id ENV_ID --workspace PATH",
		"upmctl approval grant --plan-id PLAN_ID",
		"Do not simulate a TTY",
		"applyDecision=BLOCKED",
		"Never call `vagrant`, `virsh`, `ssh`, `kubectl`, `helm`, `ansible-playbook`",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("Skill is missing required Phase 2b2a guardrail %q", required)
		}
	}
	metadata, err := os.ReadFile(filepath.Join(root, "upmctl", "skills", "upmctl-environment", "agents", "openai.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(metadata, []byte("$upmctl-environment")) {
		t.Fatal("Skill UI metadata default prompt does not invoke $upmctl-environment")
	}
}

func TestValidatorEnforcesPhase2bSchemaComposition(t *testing.T) {
	t.Run("plan allOf if then", func(t *testing.T) {
		value := asJSONDocument(t, representativePlan())
		value.(map[string]any)["target"].(map[string]any)["kind"] = "Cluster"
		if err := validateSchemaDocument(t, "plan.schema.json", value); err == nil {
			t.Fatal("validator accepted a VM action with a Cluster target")
		}
	})

	t.Run("external ref", func(t *testing.T) {
		candidate := validRepresentativePlan(t)
		inspection, err := upmreadiness.BuildInspection(candidate, time.Date(2026, 7, 17, 0, 10, 0, 0, time.UTC))
		if err != nil {
			t.Fatal(err)
		}
		value := asJSONDocument(t, inspection)
		value.(map[string]any)["plan"].(map[string]any)["kind"] = "NotAPlan"
		if err := validateSchemaDocument(t, "plan-inspection.schema.json", value); err == nil {
			t.Fatal("validator accepted an embedded plan rejected by the external plan.schema.json ref")
		}
	})

	t.Run("local ref allOf and prefixItems", func(t *testing.T) {
		candidate := validRepresentativePlan(t)
		current := upmreadiness.CurrentState{
			EnvironmentID: candidate.EnvironmentID, ConfigDigest: candidate.Basis.ConfigDigest,
			ConfigStatus: upmreadiness.CurrentStateValid, ManagedStateDigest: candidate.Basis.ManagedStateDigest,
			ManagedStateStatus: upmreadiness.CurrentStateValid, ObservedStateDigest: candidate.Basis.ObservedStateDigest,
			ObservedStateStatus: upmreadiness.CurrentStateValid, ObservedStateSafe: true,
		}
		preflight := upmreadiness.BuildPreflight(upmreadiness.PreflightInput{
			Plan: candidate, Now: time.Date(2026, 7, 17, 0, 10, 0, 0, time.UTC), Current: current,
		})
		value := asJSONDocument(t, preflight)
		checks := value.(map[string]any)["checks"].([]any)
		checks[0].(map[string]any)["id"] = upmreadiness.CheckPlanTimeValid
		if err := validateSchemaDocument(t, "preflight-result.schema.json", value); err == nil {
			t.Fatal("validator accepted a check that violates the local-ref prefixItems contract")
		}
	})

	t.Run("items false", func(t *testing.T) {
		schema := map[string]any{
			"type":        "array",
			"prefixItems": []any{map[string]any{"const": "first"}},
			"items":       false,
		}
		if err := validate(schema, []any{"first", "extra"}, "$data"); err == nil {
			t.Fatal("validator accepted an array item forbidden by items:false")
		}
	})

	t.Run("oneOf", func(t *testing.T) {
		schema := map[string]any{
			"oneOf": []any{
				map[string]any{"type": "string"},
				map[string]any{"const": "duplicate-match"},
			},
		}
		if err := validate(schema, "duplicate-match", "$data"); err == nil {
			t.Fatal("validator accepted a value matching more than one oneOf branch")
		}
		if err := validate(schema, float64(1), "$data"); err == nil {
			t.Fatal("validator accepted a value matching no oneOf branch")
		}
	})

	t.Run("date-time format", func(t *testing.T) {
		if err := validate(map[string]any{"type": "string", "format": "date-time"}, "not-a-date", "$data"); err == nil {
			t.Fatal("validator accepted an invalid date-time")
		}
	})
}

func TestPhase2PlanSchemaRejectsExecutableFields(t *testing.T) {
	encoded, err := json.Marshal(representativePlan())
	if err != nil {
		t.Fatal(err)
	}
	var value map[string]any
	if err := json.Unmarshal(encoded, &value); err != nil {
		t.Fatal(err)
	}
	steps := value["steps"].([]any)
	step := steps[0].(map[string]any)
	step["command"] = "vagrant up k8s-3"

	schema := loadJSON(t, filepath.Join(repositoryRoot(t), "upmctl", "specs", "v1", "schemas", "plan.schema.json"))
	encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	if err := validate(schema, document, "$data"); err == nil {
		t.Fatal("plan schema accepted forbidden executable command field")
	}
}

func TestValidatorEnforcesMaximumBounds(t *testing.T) {
	t.Run("maxLength", func(t *testing.T) {
		err := validate(map[string]any{"type": "string", "maxLength": float64(3)}, "four", "$data")
		if err == nil {
			t.Fatal("validator accepted a string longer than maxLength")
		}
	})

	t.Run("maxItems", func(t *testing.T) {
		err := validate(map[string]any{"type": "array", "maxItems": float64(1)}, []any{"one", "two"}, "$data")
		if err == nil {
			t.Fatal("validator accepted an array longer than maxItems")
		}
	})
}

func representativePlan() upmplan.Plan {
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	return upmplan.Plan{
		APIVersion: upmplan.APIVersion, Kind: upmplan.Kind,
		PlanID: "plan-0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", PlanDigest: digest,
		EnvironmentID: "env-test", Action: upmplan.ActionVMStart, Disposition: upmplan.DispositionActionRequired,
		CreatedAt: "2026-07-17T00:00:00Z", ExpiresAt: "2026-07-17T00:30:00Z", RiskLevel: "R1",
		Basis:             upmplan.Basis{ConfigDigest: digest, ManagedStateDigest: digest, ObservedStateDigest: digest},
		Target:            upmplan.Target{Kind: "VirtualMachine", Name: "k8s-3"},
		AffectedResources: []string{"vm/k8s-3", "node/k8s-3"}, Preconditions: []string{"MANAGED_ENVIRONMENT_VALID", "TARGET_STOPPED"},
		Blockers: []string{}, RejectionConditions: []string{"TARGET_IDENTITY_INCONSISTENT"}, IrreversibleActions: []string{}, DataImpact: []string{},
		ExpectedDisruption: []string{"target node is unavailable while starting"}, ApprovalScope: "vm.start:k8s-3",
		AcceptanceRefs: []string{"AC-PLAN-001", "AC-PLAN-002"},
		Steps:          []upmplan.Step{{ID: "vm-start-01", Code: "VM_START_NO_PROVISION", Resource: "vm/k8s-3", Postconditions: []string{"libvirt domain is running"}, AcceptanceRefs: []string{"AC-PLAN-002"}}},
	}
}

func validRepresentativePlan(t *testing.T) upmplan.Plan {
	t.Helper()
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	observed := vm.List{
		Sources: vm.ListSources{Vagrant: "observed", Libvirt: "observed", Kubernetes: "observed", KubernetesAPI: "reachable"},
		Machines: []vm.Machine{
			{Name: "k8s-1", Index: 1, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true}, Sources: map[string]string{"vagrant": "observed", "libvirt": "observed"}},
			{Name: "k8s-3", Index: 3, Expected: true, Managed: true, Health: "STOPPED", LibvirtID: "33333333-3333-4333-8333-333333333333", LibvirtState: "shut off", Identity: vm.Identity{VagrantMachine: "k8s-3", DomainName: "fixture_k8s-3"}, Sources: map[string]string{"vagrant": "observed", "libvirt": "observed"}},
		},
	}
	candidate, err := upmplan.NewVMStart(upmplan.VMStartInput{EnvironmentID: "env-test", ConfigDigest: digest, ManagedStateDigest: digest, ObservedStateDigest: digest, Observed: observed, Node: "k8s-3", Now: time.Date(2026, 7, 17, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func asJSONDocument(t *testing.T, value any) any {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func validateSchemaDocument(t *testing.T, schemaName string, document any) error {
	t.Helper()
	schemaPath := filepath.Join(repositoryRoot(t), "upmctl", "specs", "v1", "schemas", schemaName)
	schema := loadJSON(t, schemaPath)
	return newSchemaValidator(filepath.Dir(schemaPath), schema).validate(schema, schema, document, "$data")
}

func validateDocument(t *testing.T, schemaName string, value any) {
	t.Helper()
	schemaPath := filepath.Join(repositoryRoot(t), "upmctl", "specs", "v1", "schemas", schemaName)
	schema := loadJSON(t, schemaPath)
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatal(err)
	}
	validator := newSchemaValidator(filepath.Dir(schemaPath), schema)
	if err := validator.validate(schema, schema, document, "$data"); err != nil {
		t.Fatalf("%s: %v\nJSON: %s", schemaName, err, encoded)
	}
}

func loadJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func repositoryRoot(t *testing.T) string {
	t.Helper()
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
}

type schemaValidator struct {
	schemaDir string
	cache     map[string]map[string]any
}

func newSchemaValidator(schemaDir string, root map[string]any) *schemaValidator {
	validator := &schemaValidator{
		schemaDir: schemaDir,
		cache:     make(map[string]map[string]any),
	}
	if schemaDir != "" {
		validator.cache[filepath.Clean(schemaDir)] = root
	}
	return validator
}

func validate(schema map[string]any, value any, path string) error {
	validator := newSchemaValidator("", schema)
	return validator.validate(schema, schema, value, path)
}

func (validator *schemaValidator) validate(schema, root map[string]any, value any, path string) error {
	if reference, ok := schema["$ref"].(string); ok {
		resolved, resolvedRoot, err := validator.resolveReference(reference, root)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if err := validator.validateSchema(resolved, resolvedRoot, value, path); err != nil {
			return err
		}
	}

	if branches, ok := schema["allOf"].([]any); ok {
		for index, branch := range branches {
			if err := validator.validateSchema(branch, root, value, path); err != nil {
				return fmt.Errorf("%s allOf[%d]: %w", path, index, err)
			}
		}
	}
	if branches, ok := schema["oneOf"].([]any); ok {
		matches := 0
		var lastErr error
		for _, branch := range branches {
			if err := validator.validateSchema(branch, root, value, path); err == nil {
				matches++
			} else {
				lastErr = err
			}
		}
		if matches != 1 {
			return fmt.Errorf("%s must match exactly one oneOf branch (matched %d; last error: %v)", path, matches, lastErr)
		}
	}
	if condition, ok := schema["if"]; ok {
		if validator.validateSchema(condition, root, value, path) == nil {
			if thenSchema, exists := schema["then"]; exists {
				if err := validator.validateSchema(thenSchema, root, value, path); err != nil {
					return fmt.Errorf("%s then: %w", path, err)
				}
			}
		} else if elseSchema, exists := schema["else"]; exists {
			if err := validator.validateSchema(elseSchema, root, value, path); err != nil {
				return fmt.Errorf("%s else: %w", path, err)
			}
		}
	}

	if constant, ok := schema["const"]; ok && !reflect.DeepEqual(constant, value) {
		return fmt.Errorf("%s does not equal const %v", path, constant)
	}
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			if reflect.DeepEqual(candidate, value) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s value %v is not in enum", path, value)
		}
	}
	if typeName, ok := schema["type"].(string); ok && !matchesType(typeName, value) {
		return fmt.Errorf("%s must be %s", path, typeName)
	}

	if object, ok := value.(map[string]any); ok {
		properties, _ := schema["properties"].(map[string]any)
		if required, ok := schema["required"].([]any); ok {
			for _, item := range required {
				name, ok := item.(string)
				if !ok {
					return fmt.Errorf("%s has non-string required property name", path)
				}
				if _, exists := object[name]; !exists {
					return fmt.Errorf("%s.%s is required", path, name)
				}
			}
		}
		for name, child := range object {
			if childSchema, exists := properties[name]; exists {
				if err := validator.validateSchema(childSchema, root, child, path+"."+name); err != nil {
					return err
				}
				continue
			}
			if additional, exists := schema["additionalProperties"]; exists {
				switch additional := additional.(type) {
				case bool:
					if !additional {
						return fmt.Errorf("%s.%s is not allowed", path, name)
					}
				default:
					if err := validator.validateSchema(additional, root, child, path+"."+name); err != nil {
						return err
					}
				}
			}
		}
	}

	if array, ok := value.([]any); ok {
		if minimum, ok := schema["minItems"].(float64); ok && len(array) < int(minimum) {
			return fmt.Errorf("%s has too few items", path)
		}
		if maximum, ok := schema["maxItems"].(float64); ok && len(array) > int(maximum) {
			return fmt.Errorf("%s has too many items", path)
		}

		prefixLength := 0
		if prefix, ok := schema["prefixItems"].([]any); ok {
			prefixLength = len(prefix)
			limit := len(array)
			if limit > prefixLength {
				limit = prefixLength
			}
			for index := 0; index < limit; index++ {
				if err := validator.validateSchema(prefix[index], root, array[index], fmt.Sprintf("%s[%d]", path, index)); err != nil {
					return err
				}
			}
		}
		if itemSchema, exists := schema["items"]; exists {
			start := 0
			if _, hasPrefix := schema["prefixItems"]; hasPrefix {
				start = prefixLength
			}
			if allowed, ok := itemSchema.(bool); ok && !allowed && len(array) > start {
				return fmt.Errorf("%s[%d] is not allowed", path, start)
			}
			if _, ok := itemSchema.(bool); !ok {
				for index := start; index < len(array); index++ {
					if err := validator.validateSchema(itemSchema, root, array[index], fmt.Sprintf("%s[%d]", path, index)); err != nil {
						return err
					}
				}
			}
		}
	}

	if text, ok := value.(string); ok {
		length := utf8.RuneCountInString(text)
		if minimum, ok := schema["minLength"].(float64); ok && length < int(minimum) {
			return fmt.Errorf("%s is too short", path)
		}
		if maximum, ok := schema["maxLength"].(float64); ok && length > int(maximum) {
			return fmt.Errorf("%s is too long", path)
		}
		if pattern, ok := schema["pattern"].(string); ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("%s has invalid schema pattern %q: %w", path, pattern, err)
			}
			if !compiled.MatchString(text) {
				return fmt.Errorf("%s does not match %s", path, pattern)
			}
		}
		if format, ok := schema["format"].(string); ok && format == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
				return fmt.Errorf("%s is not a valid date-time: %w", path, err)
			}
		}
	}

	if number, ok := value.(float64); ok {
		if minimum, ok := schema["minimum"].(float64); ok && number < minimum {
			return fmt.Errorf("%s is below minimum", path)
		}
		if maximum, ok := schema["maximum"].(float64); ok && number > maximum {
			return fmt.Errorf("%s is above maximum", path)
		}
	}
	return nil
}

func (validator *schemaValidator) validateSchema(schema any, root map[string]any, value any, path string) error {
	switch schema := schema.(type) {
	case bool:
		if !schema {
			return fmt.Errorf("%s is rejected by false schema", path)
		}
		return nil
	case map[string]any:
		return validator.validate(schema, root, value, path)
	default:
		return fmt.Errorf("%s has invalid schema node %T", path, schema)
	}
}

func (validator *schemaValidator) resolveReference(reference string, root map[string]any) (any, map[string]any, error) {
	filePart, fragment, hasFragment := strings.Cut(reference, "#")
	resolvedRoot := root
	if filePart != "" {
		if validator.schemaDir == "" || filepath.IsAbs(filePart) || filepath.Base(filePart) != filePart {
			return nil, nil, fmt.Errorf("unsupported schema reference %q", reference)
		}
		path := filepath.Join(validator.schemaDir, filePart)
		var ok bool
		resolvedRoot, ok = validator.cache[path]
		if !ok {
			contents, err := os.ReadFile(path)
			if err != nil {
				return nil, nil, fmt.Errorf("read schema reference %q: %w", reference, err)
			}
			if err := json.Unmarshal(contents, &resolvedRoot); err != nil {
				return nil, nil, fmt.Errorf("decode schema reference %q: %w", reference, err)
			}
			validator.cache[path] = resolvedRoot
		}
	}
	if !hasFragment || fragment == "" {
		return resolvedRoot, resolvedRoot, nil
	}
	if !strings.HasPrefix(fragment, "/") {
		return nil, nil, fmt.Errorf("unsupported schema fragment #%s", fragment)
	}
	var current any = resolvedRoot
	for _, token := range strings.Split(strings.TrimPrefix(fragment, "/"), "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		object, ok := current.(map[string]any)
		if !ok {
			return nil, nil, fmt.Errorf("schema reference %q traverses non-object", reference)
		}
		current, ok = object[token]
		if !ok {
			return nil, nil, fmt.Errorf("schema reference %q does not exist", reference)
		}
	}
	return current, resolvedRoot, nil
}

func matchesType(typeName string, value any) bool {
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == float64(int64(number))
	case "number":
		_, ok := value.(float64)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}
