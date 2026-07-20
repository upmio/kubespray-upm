package plan

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/upmio/kubespray-upm/upmctl/internal/digest"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

type VMStartInput struct {
	EnvironmentID       string
	ConfigDigest        string
	ManagedStateDigest  string
	ObservedStateDigest string
	Observed            vm.List
	Node                string
	Now                 time.Time
	TTL                 time.Duration
}

func ObservedDigest(observed vm.List) (string, error) {
	semantic := struct {
		Machines []vm.Machine   `json:"machines"`
		Sources  vm.ListSources `json:"sources"`
		Findings []vm.Finding   `json:"findings"`
	}{
		Machines: append([]vm.Machine(nil), observed.Machines...),
		Sources:  observed.Sources,
		Findings: append([]vm.Finding(nil), observed.Findings...),
	}
	sort.Slice(semantic.Machines, func(i, j int) bool {
		if semantic.Machines[i].Index == semantic.Machines[j].Index {
			return semantic.Machines[i].Name < semantic.Machines[j].Name
		}
		return semantic.Machines[i].Index < semantic.Machines[j].Index
	})
	for index := range semantic.Machines {
		machine := &semantic.Machines[index]
		machine.Network.Addresses = append([]string(nil), machine.Network.Addresses...)
		sort.Strings(machine.Network.Addresses)
		machine.Resources.ObservedDisks = append([]vm.Disk(nil), machine.Resources.ObservedDisks...)
		sort.Slice(machine.Resources.ObservedDisks, func(i, j int) bool {
			left, right := machine.Resources.ObservedDisks[i], machine.Resources.ObservedDisks[j]
			return left.Target+"\x00"+left.Source < right.Target+"\x00"+right.Source
		})
		machine.Findings = append([]vm.Finding(nil), machine.Findings...)
		sortFindings(machine.Findings)
	}
	sortFindings(semantic.Findings)
	return digest.Sum(semantic)
}

func NewVMStart(input VMStartInput) (Plan, error) {
	if input.TTL == 0 {
		input.TTL = DefaultTTL
	}
	if input.TTL != DefaultTTL {
		return Plan{}, fmt.Errorf("Phase 2a plan TTL must be exactly %s", DefaultTTL)
	}
	if input.Now.IsZero() {
		return Plan{}, fmt.Errorf("plan creation time is required")
	}
	if !digestPattern.MatchString(input.ConfigDigest) || !digestPattern.MatchString(input.ManagedStateDigest) || !digestPattern.MatchString(input.ObservedStateDigest) {
		return Plan{}, fmt.Errorf("plan basis contains an invalid digest")
	}

	createdAt := input.Now.UTC()
	result := Plan{
		APIVersion: APIVersion, Kind: Kind, EnvironmentID: input.EnvironmentID,
		Action: ActionVMStart, Disposition: DispositionBlocked,
		CreatedAt: createdAt.Format(time.RFC3339Nano), ExpiresAt: createdAt.Add(input.TTL).Format(time.RFC3339Nano),
		RiskLevel:         "R0",
		Basis:             Basis{ConfigDigest: input.ConfigDigest, ManagedStateDigest: input.ManagedStateDigest, ObservedStateDigest: input.ObservedStateDigest},
		Target:            Target{Kind: "VirtualMachine", Name: input.Node},
		AffectedResources: []string{input.Node},
		Preconditions: []string{
			"MANAGED_ENVIRONMENT_VALID", "CONFIG_SAFE_COMPLETE", "VAGRANTFILE_DIGEST_MATCHED",
			"EXPECTED_MACHINE_IDENTITIES_BOUND", "NO_ORPHANED_RESOURCES", "NO_INCONSISTENT_IDENTITIES",
			"PLAN_DIGESTS_UNCHANGED_AT_APPLY",
		},
		Blockers: []string{},
		RejectionConditions: []string{
			"TARGET_NOT_EXPECTED", "TARGET_NOT_MANAGED", "TARGET_IDENTITY_INCOMPLETE",
			"TARGET_STATE_NOT_STOPPED", "TARGET_RUNNING_DEGRADED", "CONCURRENT_OPERATION",
		},
		IrreversibleActions: []string{}, DataImpact: []string{},
		ExpectedDisruption: []string{"target node remains unavailable until future readiness checks complete"},
		ApprovalScope:      "none", AcceptanceRefs: []string{"AC-PLAN-001", "AC-PLAN-004", "AC-PLAN-006", "AC-PLAN-007"},
		Steps: []Step{},
	}

	target, found := findMachine(input.Observed.Machines, input.Node)
	if !found {
		result.Blockers = append(result.Blockers, "TARGET_NOT_FOUND")
	} else {
		result.Blockers = append(result.Blockers, globalBlockers(input.Observed)...)
		result.Blockers = append(result.Blockers, targetBlockers(target, input.Observed)...)
		switch {
		case target.Health == "RUNNING_HEALTHY" && len(result.Blockers) == 0:
			result.Disposition = DispositionNoop
			result.AcceptanceRefs = []string{"AC-VM-002", "AC-PLAN-003", "AC-PLAN-006", "AC-PLAN-007"}
			result.AffectedResources = []string{}
			result.ExpectedDisruption = []string{}
		case target.Health == "STOPPED" && len(result.Blockers) == 0:
			result.Disposition = DispositionActionRequired
			result.RiskLevel = riskForStart(target.Name)
			result.ApprovalScope = "vm.start:" + target.Name
			result.AcceptanceRefs = []string{"AC-PLAN-002", "AC-PLAN-005", "AC-PLAN-006", "AC-PLAN-007"}
			result.Steps = startSteps(target)
		default:
			if len(result.Blockers) == 0 {
				result.Blockers = append(result.Blockers, blockerForHealth(target.Health))
			}
		}
	}
	result.Blockers = uniqueSorted(result.Blockers)

	planDigest, err := result.ExpectedDigest()
	if err != nil {
		return Plan{}, err
	}
	result.PlanDigest = planDigest
	planID, err := result.ExpectedID()
	if err != nil {
		return Plan{}, err
	}
	result.PlanID = planID
	if err := Validate(result); err != nil {
		return Plan{}, err
	}
	return result, nil
}

func globalBlockers(observed vm.List) []string {
	blockers := []string{}
	if observed.Sources.Vagrant != "observed" {
		blockers = append(blockers, "VAGRANT_OBSERVATION_INCOMPLETE")
	}
	if observed.Sources.Libvirt != "observed" {
		blockers = append(blockers, "LIBVIRT_OBSERVATION_INCOMPLETE")
	}
	for _, finding := range observed.Findings {
		if strings.Contains(finding.Code, "UNEXPECTED") || strings.Contains(finding.Code, "DUPLICATE") {
			blockers = append(blockers, finding.Code)
		}
	}
	for _, machine := range observed.Machines {
		if machine.Health == "ORPHANED" || machine.Health == "INCONSISTENT" {
			blockers = append(blockers, "ENVIRONMENT_"+machine.Health+":"+machine.Name)
		}
	}
	return blockers
}

func targetBlockers(target vm.Machine, observed vm.List) []string {
	blockers := []string{}
	if !target.Expected {
		blockers = append(blockers, "TARGET_NOT_EXPECTED")
	}
	if !target.Managed {
		blockers = append(blockers, "TARGET_NOT_MANAGED")
	}
	if target.Identity.VagrantMachine == "" || target.LibvirtID == "" || target.Identity.DomainName == "" {
		blockers = append(blockers, "TARGET_IDENTITY_INCOMPLETE")
	}
	if target.Sources["vagrant"] != "observed" || target.Sources["libvirt"] != "observed" {
		blockers = append(blockers, "TARGET_OBSERVATION_INCOMPLETE")
	}
	if target.Index > 1 {
		control, found := findMachine(observed.Machines, "k8s-1")
		if !found || control.LibvirtState != "running" || !control.Kubernetes.Ready || observed.Sources.KubernetesAPI != "reachable" {
			blockers = append(blockers, "CONTROL_PLANE_NOT_READY")
		}
	}
	return blockers
}

func startSteps(target vm.Machine) []Step {
	refs := []string{"AC-PLAN-005", "AC-PLAN-007"}
	steps := []Step{
		{ID: "vm-start-01-revalidate", Code: "REVALIDATE_PLAN_BASIS", Resource: target.Name, Postconditions: []string{"config, managed state, observed state and environment identity still match"}, AcceptanceRefs: refs},
		{ID: "vm-start-02-start", Code: "VAGRANT_UP_NO_PROVISION", Resource: target.Name, Postconditions: []string{"future executor starts only the declared VM without provisioning"}, AcceptanceRefs: refs},
		{ID: "vm-start-03-domain", Code: "WAIT_LIBVIRT_RUNNING", Resource: target.Name, Postconditions: []string{"libvirt domain is running with the bound UUID"}, AcceptanceRefs: refs},
		{ID: "vm-start-04-guest", Code: "WAIT_SSH_REACHABLE", Resource: target.Name, Postconditions: []string{"guest SSH is reachable through the observed endpoint"}, AcceptanceRefs: refs},
	}
	switch target.Index {
	case 1:
		steps = append(steps, Step{ID: "vm-start-05-control-plane", Code: "WAIT_CONTROL_PLANE_READY", Resource: target.Name, Postconditions: []string{"etcd, Kubernetes API and control-plane components are healthy"}, AcceptanceRefs: refs})
	case 2:
		steps = append(steps, Step{ID: "vm-start-05-service-node", Code: "WAIT_SERVICE_NODE_READY", Resource: target.Name, Postconditions: []string{"Kubernetes Node, storage and required UPM services are healthy"}, AcceptanceRefs: refs})
	default:
		steps = append(steps, Step{ID: "vm-start-05-kubernetes", Code: "WAIT_KUBERNETES_NODE_READY", Resource: target.Name, Postconditions: []string{"Kubernetes Node is Ready and CNI is healthy"}, AcceptanceRefs: refs})
	}
	return append(steps, Step{ID: "vm-start-06-verify", Code: "VERIFY_VM_START_POSTCONDITIONS", Resource: target.Name, Postconditions: []string{"target and cluster postconditions pass without implicit provisioning"}, AcceptanceRefs: refs})
}

func blockerForHealth(health string) string {
	switch health {
	case "RUNNING_DEGRADED":
		return "TARGET_RUNNING_DEGRADED"
	case "MISSING":
		return "TARGET_MISSING"
	case "ORPHANED":
		return "TARGET_ORPHANED"
	case "INCONSISTENT":
		return "TARGET_INCONSISTENT"
	case "UNKNOWN":
		return "TARGET_STATE_UNKNOWN"
	default:
		return "TARGET_STATE_NOT_STOPPED"
	}
}

func riskForStart(name string) string {
	if name == "k8s-1" || name == "k8s-2" {
		return "R2"
	}
	return "R1"
}

func findMachine(machines []vm.Machine, name string) (vm.Machine, bool) {
	for _, machine := range machines {
		if machine.Name == name {
			return machine, true
		}
	}
	return vm.Machine{}, false
}

func sortFindings(findings []vm.Finding) {
	sort.Slice(findings, func(i, j int) bool {
		left := findings[i].Code + "\x00" + findings[i].Source + "\x00" + findings[i].Resource + "\x00" + findings[i].Message
		right := findings[j].Code + "\x00" + findings[j].Source + "\x00" + findings[j].Resource + "\x00" + findings[j].Message
		return left < right
	})
}

func uniqueSorted(values []string) []string {
	seen := map[string]struct{}{}
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
