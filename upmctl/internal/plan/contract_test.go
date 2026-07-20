package plan

import (
	"testing"
	"time"
)

func TestValidateActionContractAcceptsGeneratedPlan(t *testing.T) {
	created, err := NewVMStart(VMStartInput{
		EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest,
		Observed: observedWithStoppedWorker(), Node: "k8s-3", Now: time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateActionContract(created); err != nil {
		t.Fatalf("ValidateActionContract() error = %v", err)
	}
}

func TestValidateActionContractRejectsRecomputedUnknownStep(t *testing.T) {
	created, err := NewVMStart(VMStartInput{
		EnvironmentID: "env", ConfigDigest: testDigest, ManagedStateDigest: testDigest, ObservedStateDigest: testDigest,
		Observed: observedWithStoppedWorker(), Node: "k8s-3", Now: time.Date(2026, 7, 17, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	created.Steps[1].Code = "ARBITRARY_ACTION"
	created.PlanDigest, err = created.ExpectedDigest()
	if err != nil {
		t.Fatal(err)
	}
	// A caller that also recomputes the instance ID can make the artifact
	// internally self-consistent, but cannot expand the binary's action allowlist.
	created.PlanID, err = created.ExpectedID()
	if err != nil {
		t.Fatal(err)
	}
	if err := Validate(created); err != nil {
		t.Fatalf("artifact should remain structurally self-consistent: %v", err)
	}
	if err := ValidateActionContract(created); err == nil {
		t.Fatal("ValidateActionContract() accepted an unknown step code")
	}
}
