package status

import (
	"testing"

	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

func TestBuildAggregatesInconsistentEnvironment(t *testing.T) {
	validation := upmconfig.Result{
		Path: "/workspace/vagrant/config.rb", Digest: "sha256:test", Status: "SAFE_COMPLETE",
		Safe: true, Complete: true, Valid: true,
		Config: upmconfig.Config{NodeCount: 2},
	}
	machines := vm.List{Machines: []vm.Machine{
		{Name: "k8s-1", Expected: true, Health: "RUNNING_HEALTHY", VagrantState: "running", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true}, Sources: map[string]string{"kubernetes": "observed"}},
		{Name: "k8s-2", Expected: true, Health: "INCONSISTENT", VagrantState: "poweroff", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true}, Sources: map[string]string{"kubernetes": "observed"}},
	}}
	result := Build(upmcontext.Deployment{Managed: true, Trust: upmcontext.TrustManagedValid, Workspace: "/workspace", Kubeconfig: "/workspace/admin.conf"}, validation, &machines)
	if result.Health != "INCONSISTENT" || result.VMSummary.Healthy != 1 || result.VMSummary.Inconsistent != 1 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildLegacyIsPassiveUnknown(t *testing.T) {
	result := Build(upmcontext.Deployment{Trust: upmcontext.TrustLegacyReadOnly, Workspace: "/workspace"}, upmconfig.Result{Status: "SAFE_COMPLETE", Valid: true, Safe: true, Complete: true}, nil)
	if result.Mode != "legacy-read-only" || result.Health != "UNKNOWN" || result.ObservationComplete {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildUnexpectedManagedMachineMakesEnvironmentInconsistent(t *testing.T) {
	validation := upmconfig.Result{Status: "SAFE_COMPLETE", Safe: true, Complete: true, Valid: true, Config: upmconfig.Config{NodeCount: 1}}
	machines := vm.List{
		Findings: []vm.Finding{{Code: "UNEXPECTED_VAGRANT_MACHINE", Severity: "warning", Source: "vagrant", Resource: "k8s-9", Message: "unexpected"}},
		Machines: []vm.Machine{{Name: "k8s-1", Expected: true, Health: "RUNNING_HEALTHY", VagrantState: "running", LibvirtState: "running", Kubernetes: vm.Kubernetes{Present: true}, Sources: map[string]string{"kubernetes": "observed"}}},
	}
	result := Build(upmcontext.Deployment{Managed: true, Trust: upmcontext.TrustManagedValid}, validation, &machines)
	if result.Health != "INCONSISTENT" {
		t.Fatalf("health = %q, want INCONSISTENT", result.Health)
	}
}
