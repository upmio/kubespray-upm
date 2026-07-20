package approval

import (
	"strings"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

var (
	testActor    = Actor{Subject: "os-user:501", UID: "501", Username: "operator", Hostname: "admin.example.test"}
	testPresence = Presence{Terminal: "/dev/ttys001", ChallengeDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
)

func TestNewBuildsPlanBoundHumanApproval(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 123, time.UTC)
	p := mustWorkerStartPlan(t, createdAt)
	now := createdAt.Add(2 * time.Minute)

	got, err := New(p, testActor, testPresence, "Start the declared worker after maintenance", "req-approval-1", "upmctl-test", now)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := Validate(got, p); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if err := ValidateIntegrity(got); err != nil {
		t.Fatalf("ValidateIntegrity() error = %v", err)
	}
	if got.APIVersion != APIVersion || got.Kind != Kind || got.Decision != DecisionApproved || got.PolicyVersion != PolicyVersion {
		t.Fatalf("approval identity/policy = %#v", got)
	}
	if got.PlanID != p.PlanID || got.PlanDigest != p.PlanDigest || got.EnvironmentID != p.EnvironmentID || got.Target != p.Target || got.Basis != p.Basis {
		t.Fatalf("approval is not bound to plan: %#v", got)
	}
	if got.Approver.Source != SourceHumanCLI || got.Approver.AuthMethod != AuthMethodInteractiveTTY {
		t.Fatalf("approver policy = %#v", got.Approver)
	}
	if got.HumanPresence.Method != PresenceMethodTyped || got.HumanPresence.ConfirmedAt != got.ApprovedAt {
		t.Fatalf("human presence = %#v", got.HumanPresence)
	}
	if got.ExpiresAt != now.Add(DefaultTTL).Format(time.RFC3339Nano) {
		t.Fatalf("expiresAt = %q", got.ExpiresAt)
	}
	wantID := "approval-" + strings.TrimPrefix(got.ApprovalDigest, "sha256:")
	if got.ApprovalID != wantID {
		t.Fatalf("approvalId = %q, want %q", got.ApprovalID, wantID)
	}

	again, err := New(p, testActor, testPresence, got.Reason, got.RequestID, got.CLIVersion, now)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("same semantic input was not deterministic:\nfirst=%#v\nsecond=%#v", got, again)
	}
}

func TestNewCapsTTLAtPlanExpiry(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	p := mustWorkerStartPlan(t, createdAt)
	now := createdAt.Add(25 * time.Minute)

	got, err := New(p, testActor, testPresence, "Approve near plan expiry", "req-expiry-cap", "dev", now)
	if err != nil {
		t.Fatal(err)
	}
	if got.ExpiresAt != p.ExpiresAt {
		t.Fatalf("expiresAt = %q, want plan expiry %q", got.ExpiresAt, p.ExpiresAt)
	}
	if err := Validate(got, p); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestNewRejectsUnapprovablePlans(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	valid := mustWorkerStartPlan(t, createdAt)
	tests := []struct {
		name string
		p    plan.Plan
		now  time.Time
		want string
	}{
		{name: "expired", p: valid, now: createdAt.Add(plan.DefaultTTL), want: "expired"},
		{name: "future", p: valid, now: createdAt.Add(-time.Second), want: "future"},
		{name: "not action required", p: mutatePlan(t, valid, func(p *plan.Plan) { p.Disposition = plan.DispositionNoop }), now: createdAt, want: "ACTION_REQUIRED"},
		{name: "risk zero", p: mutatePlan(t, valid, func(p *plan.Plan) { p.RiskLevel = "R0" }), now: createdAt, want: "R1-R3"},
		{name: "tampered contract", p: mutatePlan(t, valid, func(p *plan.Plan) { p.ApprovalScope = "vm.start:k8s-4" }), now: createdAt, want: "action contract"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(test.p, testActor, testPresence, "Approved for test", "req-test", "dev", test.now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestNewRejectsInvalidHumanEvidence(t *testing.T) {
	now := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	p := mustWorkerStartPlan(t, now)
	tests := []struct {
		name      string
		actor     Actor
		presence  Presence
		reason    string
		requestID string
		version   string
		now       time.Time
		want      string
	}{
		{name: "zero time", actor: testActor, presence: testPresence, reason: "reason", requestID: "req", version: "dev", want: "time"},
		{name: "missing subject", actor: Actor{UID: "501", Username: "operator", Hostname: "host"}, presence: testPresence, reason: "reason", requestID: "req", version: "dev", now: now, want: "subject"},
		{name: "control username", actor: Actor{Subject: "subject", UID: "501", Username: "bad\nname", Hostname: "host"}, presence: testPresence, reason: "reason", requestID: "req", version: "dev", now: now, want: "username"},
		{name: "bad challenge", actor: testActor, presence: Presence{Terminal: "/dev/tty", ChallengeDigest: "bad"}, reason: "reason", requestID: "req", version: "dev", now: now, want: "challengeDigest"},
		{name: "missing terminal", actor: testActor, presence: Presence{ChallengeDigest: testPresence.ChallengeDigest}, reason: "reason", requestID: "req", version: "dev", now: now, want: "terminal"},
		{name: "trimmed reason", actor: testActor, presence: testPresence, reason: " reason", requestID: "req", version: "dev", now: now, want: "reason"},
		{name: "long reason", actor: testActor, presence: testPresence, reason: strings.Repeat("x", 1025), requestID: "req", version: "dev", now: now, want: "reason"},
		{name: "control request", actor: testActor, presence: testPresence, reason: "reason", requestID: "req\tbad", version: "dev", now: now, want: "requestId"},
		{name: "missing version", actor: testActor, presence: testPresence, reason: "reason", requestID: "req", now: now, want: "cliVersion"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(p, test.actor, test.presence, test.reason, test.requestID, test.version, test.now)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("New() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestValidateRejectsWrongPlanAndTampering(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	p := mustWorkerStartPlan(t, createdAt)
	a := mustApproval(t, p, createdAt.Add(time.Minute))

	other := mustWorkerStartPlanFor(t, createdAt, "env-other", testDigest)
	if err := Validate(a, other); err == nil || !strings.Contains(err.Error(), "bound plan") {
		t.Fatalf("Validate(wrong plan) error = %v", err)
	}

	tampered := a
	tampered.Reason = "Changed after approval"
	if err := ValidateIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "approvalDigest") {
		t.Fatalf("ValidateIntegrity(tampered) error = %v", err)
	}
	tampered = a
	tampered.ApprovalID = "approval-" + strings.Repeat("f", 64)
	if err := ValidateIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "approvalId") {
		t.Fatalf("ValidateIntegrity(id tamper) error = %v", err)
	}
}

func TestValidateIntegrityRejectsInvalidPolicyTimeAndText(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	p := mustWorkerStartPlan(t, createdAt)
	valid := mustApproval(t, p, createdAt.Add(time.Minute))
	tests := []struct {
		name string
		edit func(*Approval)
		want string
	}{
		{name: "source", edit: func(a *Approval) { a.Approver.Source = "api" }, want: "source"},
		{name: "auth method", edit: func(a *Approval) { a.Approver.AuthMethod = "token" }, want: "authentication"},
		{name: "presence method", edit: func(a *Approval) { a.HumanPresence.Method = "checkbox" }, want: "presence method"},
		{name: "confirmation mismatch", edit: func(a *Approval) { a.HumanPresence.ConfirmedAt = createdAt.Format(time.RFC3339Nano) }, want: "confirmedAt"},
		{name: "non UTC time", edit: func(a *Approval) {
			a.ApprovedAt = "2026-07-17T16:01:00+08:00"
			a.HumanPresence.ConfirmedAt = a.ApprovedAt
		}, want: "canonical UTC"},
		{name: "excess TTL", edit: func(a *Approval) {
			at, _ := time.Parse(time.RFC3339Nano, a.ApprovedAt)
			a.ExpiresAt = at.Add(DefaultTTL + time.Nanosecond).Format(time.RFC3339Nano)
		}, want: "exceeds"},
		{name: "bad basis", edit: func(a *Approval) { a.Basis.ConfigDigest = "bad" }, want: "configDigest"},
		{name: "bad scope", edit: func(a *Approval) { a.ApprovalScope = "vm.start:k8s-4" }, want: "approvalScope"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.edit(&candidate)
			resealApproval(t, &candidate)
			if err := ValidateIntegrity(candidate); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ValidateIntegrity() error = %v, want containing %q", err, test.want)
			}
		})
	}
}

func TestExpiredUsesExclusiveExpiryBoundary(t *testing.T) {
	createdAt := time.Date(2026, 7, 17, 8, 0, 0, 0, time.UTC)
	p := mustWorkerStartPlan(t, createdAt)
	a := mustApproval(t, p, createdAt.Add(time.Minute))
	expiresAt, _ := time.Parse(time.RFC3339Nano, a.ExpiresAt)

	if expired, err := a.Expired(expiresAt.Add(-time.Nanosecond)); err != nil || expired {
		t.Fatalf("Expired(before) = %t, %v", expired, err)
	}
	if expired, err := a.Expired(expiresAt); err != nil || !expired {
		t.Fatalf("Expired(at boundary) = %t, %v", expired, err)
	}
	tampered := a
	tampered.ExpiresAt = "not-a-time"
	if _, err := tampered.Expired(expiresAt); err == nil {
		t.Fatal("Expired() accepted invalid expiresAt")
	}
}

func mustApproval(t *testing.T, p plan.Plan, now time.Time) Approval {
	t.Helper()
	a, err := New(p, testActor, testPresence, "Approved by an interactive operator", "req-approval", "dev", now)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func mustWorkerStartPlan(t *testing.T, now time.Time) plan.Plan {
	t.Helper()
	return mustWorkerStartPlanFor(t, now, "env-approval-test", testDigest)
}

func mustWorkerStartPlanFor(t *testing.T, now time.Time, environmentID, basisDigest string) plan.Plan {
	t.Helper()
	p, err := plan.NewVMStart(plan.VMStartInput{
		EnvironmentID: environmentID, ConfigDigest: basisDigest,
		ManagedStateDigest: basisDigest, ObservedStateDigest: basisDigest,
		Observed: stoppedWorkerObservation(), Node: "k8s-3", Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func stoppedWorkerObservation() vm.List {
	sources := map[string]string{"vagrant": "observed", "libvirt": "observed"}
	return vm.List{
		Sources: vm.ListSources{Vagrant: "observed", Libvirt: "observed", Kubernetes: "observed", KubernetesAPI: "reachable"},
		Machines: []vm.Machine{
			{Name: "k8s-1", Index: 1, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true, State: "ready"}, Sources: sources},
			{Name: "k8s-2", Index: 2, Expected: true, Managed: true, Health: "RUNNING_DEGRADED", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true, Ready: true, State: "ready"}, Sources: sources},
			{Name: "k8s-3", Index: 3, Expected: true, Managed: true, Health: "STOPPED", VagrantState: "poweroff", LibvirtID: "33333333-3333-4333-8333-333333333333", LibvirtState: "shut off", Identity: vm.Identity{VagrantMachine: "k8s-3", DomainName: "env_k8s-3", LibvirtUUID: "33333333-3333-4333-8333-333333333333"}, Sources: sources},
		},
	}
}

func mutatePlan(t *testing.T, source plan.Plan, edit func(*plan.Plan)) plan.Plan {
	t.Helper()
	edit(&source)
	var err error
	source.PlanDigest, err = source.ExpectedDigest()
	if err != nil {
		t.Fatal(err)
	}
	source.PlanID, err = source.ExpectedID()
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func resealApproval(t *testing.T, a *Approval) {
	t.Helper()
	var err error
	a.ApprovalDigest, err = a.ExpectedDigest()
	if err != nil {
		t.Fatal(err)
	}
	a.ApprovalID, err = a.ExpectedID()
	if err != nil {
		t.Fatal(err)
	}
}
