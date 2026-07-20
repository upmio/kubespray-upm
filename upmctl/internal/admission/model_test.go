package admission

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestApprovalRevocationBindsApprovalAndPlan(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	revokedAt := approvedAt.Add(time.Minute)
	r, err := NewApprovalRevocation(a, p, testHumanActor(), approval.Presence{Terminal: "/dev/tty", ChallengeDigest: testDigest}, "maintenance cancelled", revokedAt)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindApprovalRevocation || r.Disposition != DispositionRevoked || r.Actor.Source != approval.SourceHumanCLI || r.HumanPresence.ConfirmedAt != r.RevokedAt {
		t.Fatalf("revocation = %#v", r)
	}
	if err := ValidateApprovalRevocation(r, a, p); err != nil {
		t.Fatalf("ValidateApprovalRevocation() error = %v", err)
	}

	tampered := r
	tampered.Reason = "different reason"
	if err := ValidateApprovalRevocationIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "revocationDigest") {
		t.Fatalf("tampered validation error = %v", err)
	}
	tampered = r
	tampered.ApprovalID = "approval-" + strings.Repeat("f", 64)
	tampered.RevocationDigest, _ = tampered.ExpectedDigest()
	tampered.RevocationID, _ = tampered.ExpectedID()
	if err := ValidateApprovalRevocation(tampered, a, p); err == nil || !strings.Contains(err.Error(), "supplied approval") {
		t.Fatalf("binding validation error = %v", err)
	}
	tampered = r
	tampered.Actor.Source = "agent"
	tampered.RevocationDigest, _ = tampered.ExpectedDigest()
	tampered.RevocationID, _ = tampered.ExpectedID()
	if err := ValidateApprovalRevocationIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "source") {
		t.Fatalf("non-human revocation error = %v", err)
	}
}

func TestApprovalRevocationRejectsExpiredOrBackdatedApproval(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	for _, at := range []time.Time{approvedAt.Add(-time.Nanosecond), approvedAt.Add(approval.DefaultTTL)} {
		_, err := NewApprovalRevocation(a, p, testHumanActor(), approval.Presence{Terminal: "/dev/tty", ChallengeDigest: testDigest}, "cancel", at)
		if err == nil {
			t.Fatalf("NewApprovalRevocation(%s) succeeded", at)
		}
	}
}

func TestPlanClaimDeterministicOperationAndIntegrity(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	claimedAt := approvedAt.Add(time.Minute)
	basis := validAdmissionBasis(claimedAt.Add(-time.Second))
	claimer := testClaimer()
	first, err := NewPlanClaim(p, a, claimer, basis, &LockFencing{LockID: "environment-lock:env-test", Token: 7}, claimedAt)
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewPlanClaim(p, a, claimer, basis, &LockFencing{LockID: "environment-lock:env-test", Token: 7}, claimedAt)
	if err != nil {
		t.Fatal(err)
	}
	if first.OperationID != second.OperationID || first.ClaimDigest != second.ClaimDigest || first.ClaimID != second.ClaimID {
		t.Fatalf("same admission tuple was not deterministic: %#v %#v", first, second)
	}
	wantOperation, err := DeriveOperationID(p.PlanID, a.ApprovalID, p.EnvironmentID, p.Action, p.ApprovalScope)
	if err != nil || first.OperationID != wantOperation {
		t.Fatalf("operationId = %q, %v; want %q", first.OperationID, err, wantOperation)
	}
	if err := ValidatePlanClaim(first, a, p); err != nil {
		t.Fatalf("ValidatePlanClaim() error = %v", err)
	}

	tampered := first
	tampered.OperationID = "operation-" + strings.Repeat("f", 64)
	tampered.ClaimDigest, _ = tampered.ExpectedDigest()
	tampered.ClaimID, _ = tampered.ExpectedID()
	if err := ValidatePlanClaimIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "operationId") {
		t.Fatalf("operation tampering error = %v", err)
	}
	tampered = first
	tampered.Action = "cluster.stop"
	tampered.Scope = "cluster.stop"
	tampered.OperationID, _ = DeriveOperationID(tampered.PlanID, tampered.ApprovalID, tampered.EnvironmentID, tampered.Action, tampered.Scope)
	tampered.ClaimDigest, _ = tampered.ExpectedDigest()
	tampered.ClaimID, _ = tampered.ExpectedID()
	if err := ValidatePlanClaimIntegrity(tampered); err == nil || !strings.Contains(err.Error(), "action or scope") {
		t.Fatalf("unsupported action error = %v", err)
	}
}

func TestPlanClaimOptionalLockFencingAndAdmissionChecks(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	claimedAt := approvedAt.Add(time.Minute)
	claim, err := NewPlanClaim(p, a, testClaimer(), validAdmissionBasis(claimedAt), nil, claimedAt)
	if err != nil {
		t.Fatal(err)
	}
	if claim.LockFencing != nil {
		t.Fatalf("LockFencing = %#v, want nil", claim.LockFencing)
	}

	invalidBasis := validAdmissionBasis(claimedAt)
	invalidBasis.DriftValidation = "SKIPPED"
	if _, err := NewPlanClaim(p, a, testClaimer(), invalidBasis, nil, claimedAt); err == nil || !strings.Contains(err.Error(), "admissionBasis") {
		t.Fatalf("invalid admission basis error = %v", err)
	}
	if _, err := NewPlanClaim(p, a, testClaimer(), validAdmissionBasis(claimedAt), &LockFencing{LockID: "lock", Token: 0}, claimedAt); err == nil || !strings.Contains(err.Error(), "positive") {
		t.Fatalf("invalid fencing error = %v", err)
	}
}

func TestPlanClaimRejectsReplayTupleChangesAndExpiry(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	first, err := DeriveOperationID(p.PlanID, a.ApprovalID, p.EnvironmentID, p.Action, p.ApprovalScope)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := DeriveOperationID(p.PlanID, a.ApprovalID, p.EnvironmentID, p.Action, p.ApprovalScope+":changed")
	if err != nil {
		t.Fatal(err)
	}
	if first == changed {
		t.Fatal("scope change did not change operation identity")
	}
	_, err = NewPlanClaim(p, a, testClaimer(), validAdmissionBasis(approvedAt.Add(approval.DefaultTTL)), nil, approvedAt.Add(approval.DefaultTTL))
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired approval claim error = %v", err)
	}
}

func TestArtifactUnionRoundTripsAllKinds(t *testing.T) {
	p, a, approvedAt := testPlanAndApproval(t)
	r, err := NewApprovalRevocation(a, p, testHumanActor(), approval.Presence{Terminal: "/dev/tty", ChallengeDigest: testDigest}, "cancel", approvedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewPlanClaim(p, a, testClaimer(), validAdmissionBasis(approvedAt), nil, approvedAt.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range []Artifact{ApprovalArtifact(a), RevocationArtifact(r), ClaimArtifact(c)} {
		encoded, err := EncodeArtifact(artifact)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeArtifact(encoded)
		if err != nil {
			t.Fatal(err)
		}
		kind, _ := artifact.Kind()
		decodedKind, _ := decoded.Kind()
		if decodedKind != kind {
			t.Fatalf("decoded kind = %q, want %q", decodedKind, kind)
		}
		planID, _ := decoded.PlanID()
		if planID != p.PlanID {
			t.Fatalf("decoded planId = %q, want %q", planID, p.PlanID)
		}
	}
}

func TestArtifactStrictDecoderRejectsInvalidJSONContracts(t *testing.T) {
	_, a, _ := testPlanAndApproval(t)
	valid, err := EncodeArtifact(ApprovalArtifact(a))
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]any
	if err := json.Unmarshal(valid, &object); err != nil {
		t.Fatal(err)
	}
	object["unknown"] = true
	unknown, _ := json.Marshal(object)

	duplicate := strings.Replace(string(valid), `"kind":"Approval"`, `"kind":"Approval","kind":"Approval"`, 1)
	badKind := strings.Replace(string(valid), `"kind":"Approval"`, `"kind":"Unknown"`, 1)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "unknown field", data: unknown},
		{name: "duplicate key", data: []byte(duplicate)},
		{name: "trailing value", data: append(append([]byte{}, valid...), []byte(` {}`)...)},
		{name: "unsupported kind", data: []byte(badKind)},
		{name: "missing kind", data: []byte(`{"apiVersion":"upmctl.upm.io/v1alpha1"}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := DecodeArtifact(test.data); err == nil {
				t.Fatal("DecodeArtifact() succeeded")
			}
		})
	}
}

func testPlanAndApproval(t *testing.T) (plan.Plan, approval.Approval, time.Time) {
	t.Helper()
	createdAt := time.Date(2026, 7, 17, 2, 0, 0, 0, time.UTC)
	p, err := plan.NewVMStart(plan.VMStartInput{
		EnvironmentID: "env-test", ConfigDigest: testDigest, ManagedStateDigest: testDigest,
		ObservedStateDigest: testDigest, Observed: testObserved(), Node: "k8s-3", Now: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	approvedAt := createdAt.Add(time.Minute)
	a, err := approval.New(p, testHumanActor(), approval.Presence{Terminal: "/dev/tty", ChallengeDigest: testDigest}, "approved for test", "request-1", "upmctl-test", approvedAt)
	if err != nil {
		t.Fatal(err)
	}
	return p, a, approvedAt
}

func testHumanActor() approval.Actor {
	return approval.Actor{Subject: "uid:1000", UID: "1000", Username: "operator", Hostname: "lab-host"}
}

func testClaimer() ActorObservation {
	return ActorObservation{
		Subject: "uid:1000", UID: "1000", Username: "operator", Hostname: "lab-host",
		Source: "upmctl-apply", AuthMethod: "local-process",
	}
}

func validAdmissionBasis(checkedAt time.Time) AdmissionBasis {
	return AdmissionBasis{
		PlanValidation: AdmissionPlanValid, ApprovalValidation: AdmissionApprovalApproved,
		EnvironmentValidation: AdmissionEnvironmentMatch, DriftValidation: AdmissionDriftMatch,
		CheckedAt: checkedAt.UTC().Format(time.RFC3339Nano),
	}
}

func testObserved() vm.List {
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
