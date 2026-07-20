package vm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
)

type fakeRunner struct {
	commands []runner.Command
	results  map[string]runner.Result
	errors   map[string]error
}

func (f *fakeRunner) Run(_ context.Context, command runner.Command) (runner.Result, error) {
	f.commands = append(f.commands, command)
	key := command.Executable + " " + fmt.Sprint(command.Args)
	return f.results[key], f.errors[key]
}

func TestServiceListAggregatesVagrantLibvirtAndKubernetes(t *testing.T) {
	workspace := t.TempDir()
	mustWriteID(t, workspace, "k8s-1", "11111111-1111-4111-8111-111111111111")
	mustWriteID(t, workspace, "k8s-2", "22222222-2222-4222-8222-222222222222")
	kubeconfig := filepath.Join(workspace, "inventory", "sample", "artifacts", "admin.conf")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kubeconfig, []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}

	fake := &fakeRunner{results: map[string]runner.Result{
		"vagrant [status --machine-readable]": {
			Stdout: "1,k8s-2,state,poweroff\n2,k8s-1,state,running\n",
		},
		"virsh [list --all --name]":                                         {Stdout: "fixture_k8s-1\nfixture_k8s-2\nfixture_k8s-3\nfixture_k8s-9\n"},
		"virsh [domuuid fixture_k8s-1]":                                     {Stdout: "11111111-1111-4111-8111-111111111111\n"},
		"virsh [domuuid fixture_k8s-2]":                                     {Stdout: "22222222-2222-4222-8222-222222222222\n"},
		"virsh [domuuid fixture_k8s-3]":                                     {Stdout: "33333333-3333-4333-8333-333333333333\n"},
		"virsh [domstate 11111111-1111-4111-8111-111111111111]":             {Stdout: "running\n"},
		"virsh [domstate 22222222-2222-4222-8222-222222222222]":             {Stdout: "shut off\n"},
		"virsh [domstate 33333333-3333-4333-8333-333333333333]":             {Stdout: "shut off\n"},
		"virsh [dominfo 11111111-1111-4111-8111-111111111111]":              {Stdout: "Name: k8s-1-domain\nCPU(s): 4\nMax memory: 4194304 KiB\n"},
		"virsh [dominfo 22222222-2222-4222-8222-222222222222]":              {Stdout: "Name: k8s-2-domain\nCPU(s): 4\nMax memory: 4194304 KiB\n"},
		"virsh [dominfo 33333333-3333-4333-8333-333333333333]":              {Stdout: "Name: k8s-3-domain\nCPU(s): 6\nMax memory: 8388608 KiB\n"},
		"virsh [domblklist 11111111-1111-4111-8111-111111111111 --details]": {Stdout: "Type Device Target Source\n------------------------------------------------\nfile disk vda /var/lib/libvirt/images/k8s-1.img\n"},
		"virsh [domblklist 22222222-2222-4222-8222-222222222222 --details]": {Stdout: "Type Device Target Source\n------------------------------------------------\nfile disk vda /var/lib/libvirt/images/k8s-2.img\nfile disk vdb /data/k8s-2-1.img\n"},
		"vagrant [ssh-config k8s-1]":                                        {Stdout: "HostName 192.168.200.101\nPort 22\n"},
		"vagrant [ssh-config k8s-2]":                                        {Stdout: "HostName 192.168.200.102\nPort 22\n"},
		"vagrant [ssh k8s-1 -c true]":                                       {},
		"kubectl [--kubeconfig " + kubeconfig + " get nodes -o json]": {
			Stdout: `{"items":[{"metadata":{"name":"k8s-1","uid":"node-uid-1"},"spec":{"unschedulable":false},"status":{"conditions":[{"type":"Ready","status":"True"}],"addresses":[{"type":"InternalIP","address":"192.168.200.101"}]}},{"metadata":{"name":"k8s-9","uid":"extra-node"},"status":{"conditions":[{"type":"Ready","status":"True"}]}}]}`,
		},
	}, errors: map[string]error{}}

	list, err := NewService(fake).List(context.Background(), upmcontext.Deployment{
		Workspace:  workspace,
		Kubeconfig: kubeconfig,
		Managed:    true,
		MachineIDs: map[string]string{
			"k8s-1": "11111111-1111-4111-8111-111111111111",
			"k8s-2": "22222222-2222-4222-8222-222222222222",
		},
	}, upmconfig.Config{
		Prefix:            "k8s",
		NodeCount:         2,
		ControlPlaneCount: 1,
		EtcdCount:         1,
		UPMCount:          1,
		Resources: upmconfig.ResourceProfile{
			ControlPlaneCPU: 4, ControlPlaneMiB: 4096,
			UPMCPU: 4, UPMMemoryMiB: 4096,
			WorkerCPU: 6, WorkerMemoryMiB: 8192,
		},
	})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(list.Machines) != 3 {
		t.Fatalf("machine count = %d, want 3", len(list.Machines))
	}
	if list.Machines[0].Name != "k8s-1" || list.Machines[0].Health != "RUNNING_HEALTHY" {
		t.Fatalf("k8s-1 = %#v", list.Machines[0])
	}
	if list.Machines[0].Identity.DomainName != "k8s-1-domain" || list.Machines[0].Identity.KubernetesNodeUID != "node-uid-1" || list.Machines[0].Network.InternalIP != "192.168.200.101" || list.Machines[0].Network.SSHState != "reachable" {
		t.Fatalf("k8s-1 inspection fields = %#v", list.Machines[0])
	}
	if list.Machines[1].Name != "k8s-2" || list.Machines[1].Health != "STOPPED" {
		t.Fatalf("k8s-2 = %#v", list.Machines[1])
	}
	if len(list.Machines[1].Resources.ObservedDisks) != 2 {
		t.Fatalf("k8s-2 disks = %#v", list.Machines[1].Resources.ObservedDisks)
	}
	if list.Machines[2].Name != "k8s-3" || list.Machines[2].Health != "ORPHANED" {
		t.Fatalf("k8s-3 = %#v", list.Machines[2])
	}
	if !containsFindingCode(list.Findings, "UNEXPECTED_KUBERNETES_NODE") || !containsFindingCode(list.Findings, "UNEXPECTED_LIBVIRT_DOMAIN") {
		t.Fatalf("list findings = %#v", list.Findings)
	}
	wantFirst := runner.Command{Executable: "vagrant", Args: []string{"status", "--machine-readable"}, Dir: workspace}
	if !reflect.DeepEqual(fake.commands[0], wantFirst) {
		t.Fatalf("first command = %#v, want %#v", fake.commands[0], wantFirst)
	}
}

func TestDeriveHealthDetectsCrossSourceDrift(t *testing.T) {
	tests := []struct {
		name    string
		machine Machine
		want    string
	}{
		{
			name: "poweroff domain still running",
			machine: Machine{
				Expected:        true,
				VagrantState:    "poweroff",
				LibvirtState:    "running",
				KubernetesState: "not-ready",
			},
			want: "INCONSISTENT",
		},
		{
			name: "poweroff without libvirt evidence is unknown",
			machine: Machine{
				Expected:        true,
				VagrantState:    "poweroff",
				LibvirtState:    "unknown",
				KubernetesState: "not-ready",
			},
			want: "UNKNOWN",
		},
		{
			name: "metadata only orphan",
			machine: Machine{
				VagrantState:    "unknown",
				LibvirtID:       "11111111-1111-4111-8111-111111111111",
				LibvirtState:    "shut off",
				KubernetesState: "unknown",
			},
			want: "ORPHANED",
		},
		{
			name: "saved is unsupported",
			machine: Machine{
				Expected:        true,
				VagrantState:    "saved",
				LibvirtState:    "paused",
				KubernetesState: "unknown",
			},
			want: "INCONSISTENT",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := deriveHealth(test.machine, true, true); got != test.want {
				t.Fatalf("deriveHealth() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestKubernetesAPIReachableWithZeroNodes(t *testing.T) {
	fake := &fakeRunner{results: map[string]runner.Result{
		"kubectl [--kubeconfig /tmp/admin.conf get nodes -o json]": {Stdout: `{"items":[]}`},
	}, errors: map[string]error{}}
	states, finding, available, err := NewService(fake).readKubernetesNodes(context.Background(), upmcontext.Deployment{Kubeconfig: "/tmp/admin.conf"})
	if err != nil || !available || finding != nil || len(states) != 0 {
		t.Fatalf("states=%#v finding=%#v available=%t err=%v", states, finding, available, err)
	}
}

func TestMarkDuplicateUUIDsMakesBothMachinesInconsistent(t *testing.T) {
	list := List{Machines: []Machine{
		{Name: "k8s-1", LibvirtID: "11111111-1111-4111-8111-111111111111", Health: "RUNNING_HEALTHY", Consistency: "consistent"},
		{Name: "k8s-2", LibvirtID: "11111111-1111-4111-8111-111111111111", Health: "STOPPED", Consistency: "consistent"},
	}}
	markDuplicateUUIDs(&list)
	for _, machine := range list.Machines {
		if machine.Health != "INCONSISTENT" || machine.Consistency != "inconsistent" {
			t.Fatalf("machine = %#v", machine)
		}
	}
}

func TestParseVagrantStatusRequiresMachineStates(t *testing.T) {
	_, err := parseVagrantStatus("1,,ui,hello\n")
	if err == nil {
		t.Fatal("parseVagrantStatus() error = nil, want error")
	}
}

func mustWriteID(t *testing.T, workspace, name, id string) {
	t.Helper()
	path := filepath.Join(workspace, ".vagrant", "machines", name, "libvirt", "id")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsFindingCode(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
