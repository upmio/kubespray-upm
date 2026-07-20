package plan

import (
	"fmt"
	"reflect"

	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

// ValidateActionContract verifies that a persisted Plan uses the exact closed
// semantic template implemented by this binary. A caller must never interpret
// an arbitrary schema-valid step code as an executable capability.
func ValidateActionContract(p Plan) error {
	if p.Action != ActionVMStart || p.Disposition != DispositionActionRequired {
		return fmt.Errorf("action %q with disposition %q is not supported by the current action contract", p.Action, p.Disposition)
	}
	index := 0
	for _, candidate := range []struct {
		name  string
		index int
	}{{"k8s-1", 1}, {"k8s-2", 2}, {"k8s-3", 3}, {"k8s-4", 4}, {"k8s-5", 5}, {"k8s-6", 6}, {"k8s-7", 7}, {"k8s-8", 8}} {
		if p.Target.Name == candidate.name {
			index = candidate.index
			break
		}
	}
	if index == 0 || p.Target.Kind != "VirtualMachine" {
		return fmt.Errorf("VM start target is not supported")
	}
	wantRisk := riskForStart(p.Target.Name)
	wantApproval := "vm.start:" + p.Target.Name
	wantAffected := []string{p.Target.Name}
	wantPreconditions := []string{
		"MANAGED_ENVIRONMENT_VALID", "CONFIG_SAFE_COMPLETE", "VAGRANTFILE_DIGEST_MATCHED",
		"EXPECTED_MACHINE_IDENTITIES_BOUND", "NO_ORPHANED_RESOURCES", "NO_INCONSISTENT_IDENTITIES",
		"PLAN_DIGESTS_UNCHANGED_AT_APPLY",
	}
	wantRejections := []string{
		"TARGET_NOT_EXPECTED", "TARGET_NOT_MANAGED", "TARGET_IDENTITY_INCOMPLETE",
		"TARGET_STATE_NOT_STOPPED", "TARGET_RUNNING_DEGRADED", "CONCURRENT_OPERATION",
	}
	wantDisruption := []string{"target node remains unavailable until future readiness checks complete"}
	wantAcceptance := []string{"AC-PLAN-002", "AC-PLAN-005", "AC-PLAN-006", "AC-PLAN-007"}
	wantSteps := startSteps(vm.Machine{Name: p.Target.Name, Index: index})

	if p.RiskLevel != wantRisk || p.ApprovalScope != wantApproval ||
		!reflect.DeepEqual(p.AffectedResources, wantAffected) ||
		!reflect.DeepEqual(p.Preconditions, wantPreconditions) ||
		!reflect.DeepEqual(p.RejectionConditions, wantRejections) ||
		!reflect.DeepEqual(p.IrreversibleActions, []string{}) ||
		!reflect.DeepEqual(p.DataImpact, []string{}) ||
		!reflect.DeepEqual(p.ExpectedDisruption, wantDisruption) ||
		!reflect.DeepEqual(p.AcceptanceRefs, wantAcceptance) ||
		!reflect.DeepEqual(p.Blockers, []string{}) ||
		!reflect.DeepEqual(p.Steps, wantSteps) {
		return fmt.Errorf("Plan does not match the current closed vm.start action template")
	}
	return nil
}
