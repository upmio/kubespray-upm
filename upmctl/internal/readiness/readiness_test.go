package readiness

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

const (
	testConfigDigest   = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testManagedDigest  = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testObservedDigest = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestBuildInspectionHonorsExpiryBoundary(t *testing.T) {
	candidate := validPlan(t)
	expiresAt := mustTime(t, candidate.ExpiresAt)

	before, err := BuildInspection(candidate, expiresAt.Add(-time.Nanosecond))
	if err != nil {
		t.Fatal(err)
	}
	if before.Expired || before.ExecutionAvailable {
		t.Fatalf("inspection before expiry = %#v", before)
	}
	if before.APIVersion != APIVersion || before.Kind != KindPlanInspection || !reflect.DeepEqual(before.Plan, candidate) {
		t.Fatalf("inspection identity or embedded plan is wrong: %#v", before)
	}

	atBoundary, err := BuildInspection(candidate, expiresAt)
	if err != nil {
		t.Fatal(err)
	}
	if !atBoundary.Expired || atBoundary.CheckedAt != candidate.ExpiresAt {
		t.Fatalf("inspection at expiry = %#v", atBoundary)
	}
}

func TestBuildInspectionRejectsInvalidArtifactAndZeroTime(t *testing.T) {
	candidate := validPlan(t)
	if _, err := BuildInspection(candidate, time.Time{}); err == nil {
		t.Fatal("BuildInspection accepted zero checkedAt")
	}
	candidate.PlanDigest = testConfigDigest
	if _, err := BuildInspection(candidate, time.Now()); err == nil || !strings.Contains(err.Error(), "planDigest") {
		t.Fatalf("BuildInspection invalid plan error = %v", err)
	}
}

func TestBuildValidationMatchesFrozenContract(t *testing.T) {
	candidate := validPlan(t)
	got := BuildValidation(ValidationInput{
		Plan: candidate, Now: mustTime(t, "2026-07-17T00:10:00Z"), Current: matchingCurrent(candidate),
	})

	if got.APIVersion != APIVersion || got.Kind != KindPlanValidation || got.ArtifactStatus != StateValid {
		t.Fatalf("validation identity = %#v", got)
	}
	if got.FreshnessStatus != FreshnessCurrent || got.EnvironmentBinding != BindingMatch || got.ConfigBinding != BindingMatch || got.ManagedStateBinding != BindingMatch {
		t.Fatalf("validation statuses = %#v", got)
	}
	if got.ObservedStateBinding != BindingNotChecked || got.ExecutionAvailable {
		t.Fatalf("validation exceeded read-only boundary: %#v", got)
	}
	want := []string{}
	if !reflect.DeepEqual(got.Blockers, want) {
		t.Fatalf("Blockers = %v, want %v", got.Blockers, want)
	}
}

func TestBuildValidationReportsExpiredMismatchInvalidAndUnavailableInStableOrder(t *testing.T) {
	candidate := validPlan(t)
	current := CurrentState{
		EnvironmentID:      "other",
		ConfigDigest:       "not-a-digest",
		ConfigStatus:       StateInvalid,
		ManagedStateStatus: StateUnavailable,
	}
	got := BuildValidation(ValidationInput{Plan: candidate, Now: mustTime(t, candidate.ExpiresAt), Current: current})
	if got.FreshnessStatus != FreshnessExpired || got.EnvironmentBinding != BindingMismatch || got.ConfigBinding != BindingInvalid || got.ManagedStateBinding != BindingUnknown {
		t.Fatalf("validation statuses = %#v", got)
	}
	want := []string{
		CodePlanExpired, CodeEnvironmentMismatch, CodeConfigInvalid, CodeManagedStateUnavailable,
	}
	if !reflect.DeepEqual(got.Blockers, want) {
		t.Fatalf("Blockers = %v, want %v", got.Blockers, want)
	}
	for attempt := 0; attempt < 10; attempt++ {
		again := BuildValidation(ValidationInput{Plan: candidate, Now: mustTime(t, candidate.ExpiresAt), Current: current})
		if !reflect.DeepEqual(again.Blockers, want) {
			t.Fatalf("attempt %d Blockers = %v", attempt, again.Blockers)
		}
	}
}

func TestBuildValidationTreatsFuturePlanAndUnknownEnvironmentAsInvalid(t *testing.T) {
	candidate := validPlan(t)
	current := matchingCurrent(candidate)
	current.EnvironmentID = ""
	got := BuildValidation(ValidationInput{Plan: candidate, Now: mustTime(t, "2026-07-16T23:59:59Z"), Current: current})
	if got.FreshnessStatus != FreshnessInvalid || got.EnvironmentBinding != BindingUnknown {
		t.Fatalf("validation = %#v", got)
	}
}

func TestBuildPreflightPassesReadOnlyChecksButAlwaysBlocksApply(t *testing.T) {
	candidate := validPlan(t)
	got := BuildPreflight(PreflightInput{
		Plan: candidate, Now: mustTime(t, "2026-07-17T00:10:00Z"), Current: matchingCurrent(candidate),
	})

	if got.PreflightStatus != PreflightPassed || got.ApplyDecision != ApplyDecisionBlocked || got.ExecutionAvailable || got.ApprovalStatus != ApprovalMissing {
		t.Fatalf("preflight capability boundary = %#v", got)
	}
	if got.Basis.Config.Status != BasisMatch || got.Basis.ManagedState.Status != BasisMatch || got.Basis.ObservedState.Status != BasisMatch {
		t.Fatalf("Basis = %#v", got.Basis)
	}
	wantIDs := []string{
		CheckPlanIntegrity, CheckPlanTimeValid, CheckEnvironmentMatch, CheckConfigMatch,
		CheckManagedStateMatch, CheckObservationSafe, CheckObservedStateMatch,
		CheckExecutorCapability, CheckConcurrencyControl, CheckApprovalSubsystem,
	}
	if len(got.Checks) != 10 {
		t.Fatalf("len(Checks) = %d", len(got.Checks))
	}
	for index, id := range wantIDs {
		if got.Checks[index].ID != id {
			t.Fatalf("Checks[%d].ID = %q, want %q", index, got.Checks[index].ID, id)
		}
		wantStatus := CheckPass
		if index >= 7 {
			wantStatus = CheckFail
		}
		if got.Checks[index].Status != wantStatus || got.Checks[index].Code == "" || got.Checks[index].Message == "" {
			t.Fatalf("Checks[%d] = %#v, want status %s and populated code/message", index, got.Checks[index], wantStatus)
		}
	}
	wantBlockers := []string{CodeExecutorUnavailable, CodeConcurrencyUnavailable, CodeApprovalMissing}
	if !reflect.DeepEqual(got.Blockers, wantBlockers) {
		t.Fatalf("Blockers = %v, want %v", got.Blockers, wantBlockers)
	}
}

func TestBuildPreflightBasisStatusesAndBlockerOrder(t *testing.T) {
	candidate := validPlan(t)
	current := CurrentState{
		EnvironmentID:       "other",
		ConfigDigest:        testObservedDigest,
		ConfigStatus:        StateValid,
		ManagedStateDigest:  "invalid",
		ManagedStateStatus:  StateInvalid,
		ObservedStateStatus: StateUnavailable,
		ObservedStateSafe:   false,
	}
	got := BuildPreflight(PreflightInput{Plan: candidate, Now: mustTime(t, candidate.ExpiresAt), Current: current})
	if got.PreflightStatus != PreflightBlocked {
		t.Fatalf("PreflightStatus = %q", got.PreflightStatus)
	}
	if got.Basis.Config.Status != BasisDrift || got.Basis.ManagedState.Status != BasisInvalid || got.Basis.ObservedState.Status != BasisUnavailable || got.Basis.ObservedState.Current != nil {
		t.Fatalf("Basis = %#v", got.Basis)
	}
	want := []string{
		CodePlanExpired, CodeEnvironmentMismatch, CodeConfigDrift, CodeManagedStateInvalid,
		CodeObservedStateUnavailable, CodeExecutorUnavailable,
		CodeConcurrencyUnavailable, CodeApprovalMissing,
	}
	if !reflect.DeepEqual(got.Blockers, want) {
		t.Fatalf("Blockers = %v, want %v", got.Blockers, want)
	}
}

func TestBuildPreflightInvalidArtifactSkipsDependentChecks(t *testing.T) {
	candidate := validPlan(t)
	candidate.PlanDigest = testConfigDigest
	got := BuildPreflight(PreflightInput{Plan: candidate, Now: mustTime(t, "2026-07-17T00:10:00Z"), Current: matchingCurrent(candidate)})
	if got.PreflightStatus != PreflightBlocked || len(got.Checks) != 10 {
		t.Fatalf("invalid artifact preflight = %#v", got)
	}
	if got.Checks[0].Status != CheckFail {
		t.Fatalf("integrity check = %#v", got.Checks[0])
	}
	for index := 1; index < 7; index++ {
		if got.Checks[index].Status != CheckSkipped {
			t.Fatalf("Checks[%d] = %#v, want SKIPPED", index, got.Checks[index])
		}
	}
	want := []string{CodePlanTampered, CodeExecutorUnavailable, CodeConcurrencyUnavailable, CodeApprovalInvalid}
	if !reflect.DeepEqual(got.Blockers, want) {
		t.Fatalf("Blockers = %v, want %v", got.Blockers, want)
	}
}

func TestBuildPreflightReportsApprovalStatesWithoutEnablingApply(t *testing.T) {
	candidate := validPlan(t)
	for _, status := range []string{ApprovalMissing, ApprovalApproved, ApprovalRevoked, ApprovalExpired, ApprovalInvalid} {
		t.Run(status, func(t *testing.T) {
			got := BuildPreflight(PreflightInput{
				Plan: candidate, Now: mustTime(t, "2026-07-17T00:10:00Z"),
				Current: matchingCurrent(candidate), ApprovalStatus: status,
			})
			if got.ApprovalStatus != status || got.ApplyDecision != ApplyDecisionBlocked || got.ExecutionAvailable {
				t.Fatalf("approval boundary = %#v", got)
			}
			approvalCheck := got.Checks[len(got.Checks)-1]
			if status == ApprovalApproved {
				if approvalCheck.Status != CheckPass || approvalCheck.Code != CodeCheckPassed {
					t.Fatalf("approved check = %#v", approvalCheck)
				}
			} else if approvalCheck.Status != CheckFail {
				t.Fatalf("%s check = %#v", status, approvalCheck)
			}
		})
	}
}

func TestUnavailableBasisMarshalsCurrentAsNull(t *testing.T) {
	candidate := validPlan(t)
	got := BuildPreflight(PreflightInput{Plan: candidate, Now: mustTime(t, "2026-07-17T00:10:00Z"), Current: CurrentState{}})
	encoded, err := json.Marshal(got.Basis.Config)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"current":null`) {
		t.Fatalf("unavailable basis JSON = %s", encoded)
	}
}

func validPlan(t *testing.T) plan.Plan {
	t.Helper()
	createdAt := mustTime(t, "2026-07-17T00:00:00Z")
	observed := vm.List{
		Sources: vm.ListSources{Vagrant: "observed", Libvirt: "observed", Kubernetes: "observed", KubernetesAPI: "reachable"},
		Machines: []vm.Machine{
			{Name: "k8s-1", Index: 1, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true}, Sources: map[string]string{"vagrant": "observed", "libvirt": "observed"}},
			{Name: "k8s-3", Index: 3, Expected: true, Managed: true, Health: "STOPPED", LibvirtID: "33333333-3333-4333-8333-333333333333", LibvirtState: "shut off", Identity: vm.Identity{VagrantMachine: "k8s-3", DomainName: "fixture_k8s-3"}, Sources: map[string]string{"vagrant": "observed", "libvirt": "observed"}},
		},
	}
	candidate, err := plan.NewVMStart(plan.VMStartInput{EnvironmentID: "env-1", ConfigDigest: testConfigDigest, ManagedStateDigest: testManagedDigest, ObservedStateDigest: testObservedDigest, Observed: observed, Node: "k8s-3", Now: createdAt})
	if err != nil {
		t.Fatal(err)
	}
	return candidate
}

func matchingCurrent(candidate plan.Plan) CurrentState {
	return CurrentState{
		EnvironmentID: candidate.EnvironmentID,
		ConfigDigest:  candidate.Basis.ConfigDigest, ConfigStatus: StateValid,
		ManagedStateDigest: candidate.Basis.ManagedStateDigest, ManagedStateStatus: StateValid,
		ObservedStateDigest: candidate.Basis.ObservedStateDigest, ObservedStateStatus: StateValid,
		ObservedStateSafe: true,
	}
}

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}
