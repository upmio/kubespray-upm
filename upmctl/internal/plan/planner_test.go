package plan

import (
	"strings"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestNewVMStartActionRequiredIsDeterministic(t *testing.T) {
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	input := VMStartInput{
		EnvironmentID: "env-test", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest,
		Observed: observedWithStoppedWorker(), Node: "k8s-3", Now: now,
	}
	first, err := NewVMStart(input)
	if err != nil {
		t.Fatal(err)
	}
	input.Now = now.Add(time.Minute)
	second, err := NewVMStart(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.Disposition != DispositionActionRequired || first.RiskLevel != "R1" || len(first.Steps) == 0 {
		t.Fatalf("plan = %#v", first)
	}
	if first.PlanDigest != second.PlanDigest {
		t.Fatalf("semantic digest changed with time: %s != %s", first.PlanDigest, second.PlanDigest)
	}
	if first.PlanID == second.PlanID {
		t.Fatal("plan IDs should identify separate immutable instances")
	}
	if first.ExpiresAt != now.Add(DefaultTTL).Format(time.RFC3339Nano) {
		t.Fatalf("expiresAt = %s", first.ExpiresAt)
	}
}

func TestNewVMStartNoopAndBlockedHaveNoSteps(t *testing.T) {
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	observed := observedWithStoppedWorker()
	observed.Machines[2].Health = "RUNNING_HEALTHY"
	observed.Machines[2].VagrantState = "running"
	observed.Machines[2].LibvirtState = "running"
	plan, err := NewVMStart(VMStartInput{EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest, Observed: observed, Node: "k8s-3", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Disposition != DispositionNoop || len(plan.Steps) != 0 || len(plan.Blockers) != 0 {
		t.Fatalf("NOOP plan = %#v", plan)
	}

	observed.Machines[2].Health = "INCONSISTENT"
	blocked, err := NewVMStart(VMStartInput{EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest, Observed: observed, Node: "k8s-3", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Disposition != DispositionBlocked || len(blocked.Steps) != 0 || len(blocked.Blockers) == 0 {
		t.Fatalf("BLOCKED plan = %#v", blocked)
	}
}

func TestObservedDigestNormalizesUnorderedCollections(t *testing.T) {
	first := observedWithStoppedWorker()
	second := observedWithStoppedWorker()
	second.Machines[2].Network.Addresses = []string{"10.0.0.2", "10.0.0.1"}
	first.Machines[2].Network.Addresses = []string{"10.0.0.1", "10.0.0.2"}
	second.Machines[2].Findings = []vm.Finding{{Code: "B", Source: "x", Message: "b"}, {Code: "A", Source: "x", Message: "a"}}
	first.Machines[2].Findings = []vm.Finding{{Code: "A", Source: "x", Message: "a"}, {Code: "B", Source: "x", Message: "b"}}
	left, err := ObservedDigest(first)
	if err != nil {
		t.Fatal(err)
	}
	right, err := ObservedDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("digests differ: %s != %s", left, right)
	}
	second.Machines[2].LibvirtState = "running"
	changed, err := ObservedDigest(second)
	if err != nil {
		t.Fatal(err)
	}
	if changed == left {
		t.Fatal("state change did not change observed digest")
	}
}

func TestPlanExpiryBoundary(t *testing.T) {
	now := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	created, err := NewVMStart(VMStartInput{EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest, Observed: observedWithStoppedWorker(), Node: "k8s-3", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		now  time.Time
		want bool
	}{
		{name: "before", now: now.Add(DefaultTTL - time.Nanosecond), want: false},
		{name: "at boundary", now: now.Add(DefaultTTL), want: true},
		{name: "after", now: now.Add(DefaultTTL + time.Nanosecond), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := created.Expired(test.now)
			if err != nil || got != test.want {
				t.Fatalf("Expired() = %t, %v; want %t", got, err, test.want)
			}
		})
	}
}

func TestValidateRejectsPlanIDTampering(t *testing.T) {
	plan := mustNewWorkerStartPlan(t, time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC))
	plan.PlanID = "plan-" + strings.Repeat("f", 64)
	if err := Validate(plan); err == nil || !strings.Contains(err.Error(), "planId does not match") {
		t.Fatalf("Validate() error = %v, want PlanID content-binding rejection", err)
	}
}

func TestValidateRejectsStalePlanIDAfterTimeChange(t *testing.T) {
	plan := mustNewWorkerStartPlan(t, time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC))
	createdAt, err := time.Parse(time.RFC3339Nano, plan.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	plan.CreatedAt = createdAt.Add(time.Minute).Format(time.RFC3339Nano)
	plan.ExpiresAt = createdAt.Add(time.Minute + DefaultTTL).Format(time.RFC3339Nano)
	plan.PlanDigest, err = plan.ExpectedDigest()
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(plan); err == nil || !strings.Contains(err.Error(), "planId does not match") {
		t.Fatalf("Validate() error = %v, want stale PlanID rejection", err)
	}
}

func TestValidateRequiresExactDefaultTTL(t *testing.T) {
	plan := mustNewWorkerStartPlan(t, time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC))
	createdAt, err := time.Parse(time.RFC3339Nano, plan.CreatedAt)
	if err != nil {
		t.Fatal(err)
	}
	plan.ExpiresAt = createdAt.Add(DefaultTTL + time.Nanosecond).Format(time.RFC3339Nano)
	plan.PlanID, err = plan.ExpectedID()
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(plan); err == nil || !strings.Contains(err.Error(), "exactly 30m0s") {
		t.Fatalf("Validate() error = %v, want exact TTL rejection", err)
	}
}

func TestValidateTimingRejectsFutureAndExpiredPlans(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	plan := mustNewWorkerStartPlan(t, createdAt)
	if err := ValidateTiming(plan, createdAt.Add(-time.Nanosecond)); err == nil || !strings.Contains(err.Error(), "future") {
		t.Fatalf("ValidateTiming() error = %v, want future-created rejection", err)
	}
	if err := ValidateTiming(plan, createdAt); err != nil {
		t.Fatalf("ValidateTiming() at creation error = %v", err)
	}
	if err := ValidateTiming(plan, createdAt.Add(DefaultTTL)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("ValidateTiming() error = %v, want expiry rejection", err)
	}
	if err := Validate(plan); err != nil {
		t.Fatalf("Validate() rejected historical plan needed for audit: %v", err)
	}
}

func mustNewWorkerStartPlan(t *testing.T, now time.Time) Plan {
	t.Helper()
	plan, err := NewVMStart(VMStartInput{
		EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest,
		ObservedStateDigest: testDigest, Observed: observedWithStoppedWorker(), Node: "k8s-3", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan
}

func observedWithStoppedWorker() vm.List {
	baseSources := map[string]string{"vagrant": "observed", "libvirt": "observed"}
	return vm.List{
		Sources: vm.ListSources{Vagrant: "observed", Libvirt: "observed", Kubernetes: "observed", KubernetesAPI: "reachable"},
		Machines: []vm.Machine{
			{Name: "k8s-1", Index: 1, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true, State: "ready"}, Sources: baseSources},
			{Name: "k8s-2", Index: 2, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true, State: "ready"}, Sources: baseSources},
			{Name: "k8s-3", Index: 3, Expected: true, Managed: true, Health: "STOPPED", VagrantState: "poweroff", LibvirtID: "33333333-3333-4333-8333-333333333333", LibvirtState: "shut off", Identity: vm.Identity{VagrantMachine: "k8s-3", DomainName: "env_k8s-3", LibvirtUUID: "33333333-3333-4333-8333-333333333333"}, Sources: baseSources},
		},
	}
}
