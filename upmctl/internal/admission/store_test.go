package admission

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/approval"
	"github.com/upmio/kubespray-upm/upmctl/internal/plan"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

const admissionStoreDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestAdmissionStoreSavesAndReadsRevocation(t *testing.T) {
	workspace := t.TempDir()
	p, a := admissionStorePlanAndApproval(t)
	value := admissionStoreRevocation(t, p, a)
	store := NewStore(workspace)

	path, err := store.Save(RevocationArtifact(value))
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	wantPath := filepath.Join(workspace, ".upmctl", "admissions", p.PlanID+".json")
	if path != wantPath {
		t.Fatalf("Save() path = %q, want %q", path, wantPath)
	}
	admissionAssertMode(t, filepath.Join(workspace, ".upmctl"), 0o700)
	admissionAssertMode(t, filepath.Join(workspace, ".upmctl", "admissions"), 0o700)
	admissionAssertMode(t, path, 0o600)

	got, err := store.Read(p.PlanID)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if got.Revocation == nil || !reflect.DeepEqual(*got.Revocation, value) {
		t.Fatalf("Read() = %#v, want revocation %#v", got, value)
	}
}

func TestAdmissionStoreRevocationAndClaimCompeteForOneSlot(t *testing.T) {
	workspace := t.TempDir()
	p, a := admissionStorePlanAndApproval(t)
	revocation := RevocationArtifact(admissionStoreRevocation(t, p, a))
	claim := ClaimArtifact(admissionStoreClaim(t, p, a))
	store := NewStore(workspace)

	start := make(chan struct{})
	values := []Artifact{revocation, claim}
	errorsByCall := make([]error, len(values))
	var wait sync.WaitGroup
	for index := range values {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			<-start
			_, errorsByCall[index] = store.Save(values[index])
		}(index)
	}
	close(start)
	wait.Wait()

	succeeded, collided := 0, 0
	for _, err := range errorsByCall {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrAdmissionExists):
			collided++
		default:
			t.Fatalf("concurrent Save() unexpected error = %v", err)
		}
	}
	if succeeded != 1 || collided != 1 {
		t.Fatalf("concurrent admission results succeeded=%d collided=%d", succeeded, collided)
	}
	got, err := store.Read(p.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	kind, err := got.Kind()
	if err != nil || (kind != KindApprovalRevocation && kind != KindPlanClaim) {
		t.Fatalf("stored admission kind = %q, %v", kind, err)
	}
}

func TestAdmissionStoreRejectsApprovalArtifact(t *testing.T) {
	workspace := t.TempDir()
	p, a := admissionStorePlanAndApproval(t)
	store := NewStore(workspace)
	if _, err := store.Save(ApprovalArtifact(a)); !errors.Is(err, ErrUnsupportedArtifact) {
		t.Fatalf("Save(Approval) error = %v, want ErrUnsupportedArtifact", err)
	}
	if _, err := os.Lstat(filepath.Join(workspace, ".upmctl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Save(Approval) created control state: %v", err)
	}

	contents, err := EncodeArtifact(ApprovalArtifact(a))
	if err != nil {
		t.Fatal(err)
	}
	admissionWriteStored(t, workspace, p.PlanID, contents)
	if _, err := store.Read(p.PlanID); !errors.Is(err, ErrUnsupportedArtifact) {
		t.Fatalf("Read(stored Approval) error = %v, want ErrUnsupportedArtifact", err)
	}
}

func TestAdmissionStoreReadUsesStrictJSONAndStorageBinding(t *testing.T) {
	p, a := admissionStorePlanAndApproval(t)
	value := admissionStoreRevocation(t, p, a)
	encoded, err := EncodeArtifact(RevocationArtifact(value))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		key    string
		mutate func([]byte) []byte
	}{
		{
			name: "unknown field", key: p.PlanID,
			mutate: func(source []byte) []byte {
				return append(bytes.TrimSuffix(source, []byte("}")), []byte(`,"unknown":true}`)...)
			},
		},
		{
			name: "duplicate field", key: p.PlanID,
			mutate: func(source []byte) []byte {
				needle := []byte(`"planId":"` + p.PlanID + `"`)
				replacement := append(append([]byte{}, needle...), append([]byte(","), needle...)...)
				return bytes.Replace(source, needle, replacement, 1)
			},
		},
		{name: "trailing value", key: p.PlanID, mutate: func(source []byte) []byte { return append(source, []byte(`{}`)...) }},
		{name: "storage key mismatch", key: "plan-" + strings.Repeat("f", 64), mutate: func(source []byte) []byte { return source }},
		{
			name: "tampered digest", key: p.PlanID,
			mutate: func(source []byte) []byte {
				return bytes.Replace(source, []byte(value.Reason), []byte("tampered revocation reason"), 1)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			admissionWriteStored(t, workspace, test.key, test.mutate(append([]byte{}, encoded...)))
			if _, err := NewStore(workspace).Read(test.key); err == nil {
				t.Fatal("Read() error = nil, want strict rejection")
			}
		})
	}
}

func TestAdmissionStoreMissingAndInvalidPlanID(t *testing.T) {
	workspace := t.TempDir()
	store := NewStore(workspace)
	planID := "plan-" + strings.Repeat("a", 64)
	if _, err := store.Read(planID); !errors.Is(err, ErrAdmissionNotFound) {
		t.Fatalf("Read(missing) error = %v, want ErrAdmissionNotFound", err)
	}
	if _, err := store.Read("../bad"); !errors.Is(err, ErrInvalidPlanID) {
		t.Fatalf("Read(invalid) error = %v, want ErrInvalidPlanID", err)
	}
}

func admissionStorePlanAndApproval(t *testing.T) (plan.Plan, approval.Approval) {
	t.Helper()
	createdAt := time.Date(2026, 7, 17, 6, 0, 0, 0, time.UTC)
	p, err := plan.NewVMStart(plan.VMStartInput{
		EnvironmentID: "env-admission-store", ConfigDigest: admissionStoreDigest,
		ManagedStateDigest: admissionStoreDigest, ObservedStateDigest: admissionStoreDigest,
		Observed: admissionStoreStoppedWorkers(), Node: "k8s-3", Now: createdAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	a, err := approval.New(p,
		approval.Actor{Subject: "operator", UID: "501", Username: "operator", Hostname: "host.example.test"},
		approval.Presence{Terminal: "/dev/ttys001", ChallengeDigest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		"Approve the declared VM start", "req-admission-store", "test", createdAt.Add(time.Minute),
	)
	if err != nil {
		t.Fatal(err)
	}
	return p, a
}

func admissionStoreRevocation(t *testing.T, p plan.Plan, a approval.Approval) ApprovalRevocation {
	t.Helper()
	revokedAt := time.Date(2026, 7, 17, 6, 2, 0, 0, time.UTC)
	value, err := NewApprovalRevocation(a, p,
		approval.Actor{Subject: "operator", UID: "501", Username: "operator", Hostname: "host.example.test"},
		approval.Presence{Terminal: "/dev/ttys001", ChallengeDigest: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"},
		"Revoke before execution", revokedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func admissionStoreClaim(t *testing.T, p plan.Plan, a approval.Approval) PlanClaim {
	t.Helper()
	claimedAt := time.Date(2026, 7, 17, 6, 2, 0, 0, time.UTC)
	value, err := NewPlanClaim(p, a,
		ActorObservation{Subject: "upmctl", UID: "501", Username: "operator", Hostname: "host.example.test", Source: "internal", AuthMethod: "local-process"},
		AdmissionBasis{PlanValidation: AdmissionPlanValid, ApprovalValidation: AdmissionApprovalApproved, EnvironmentValidation: AdmissionEnvironmentMatch, DriftValidation: AdmissionDriftMatch, CheckedAt: claimedAt.Add(-time.Second).Format(time.RFC3339Nano)},
		nil, claimedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func admissionStoreStoppedWorkers() vm.List {
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

func admissionWriteStored(t *testing.T, workspace, planID string, contents []byte) {
	t.Helper()
	directory := filepath.Join(workspace, ".upmctl", "admissions")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	for path := directory; path != workspace; path = filepath.Dir(path) {
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, planID+".json"), contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func admissionAssertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %04o, want %04o", path, got, want)
	}
}
