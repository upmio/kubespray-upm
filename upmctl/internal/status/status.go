package status

import (
	"strings"

	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	"github.com/upmio/kubespray-upm/upmctl/internal/vm"
)

type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Scope    string `json:"scope"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
	Evidence string `json:"evidence,omitempty"`
}

type Environment struct {
	Mode                string        `json:"mode"`
	Health              string        `json:"health"`
	ObservationComplete bool          `json:"observationComplete"`
	RepositoryRoot      string        `json:"repositoryRoot,omitempty"`
	Workspace           string        `json:"workspace,omitempty"`
	Config              ConfigStatus  `json:"config"`
	ManagedState        ManagedStatus `json:"managedState"`
	Cluster             ClusterStatus `json:"cluster"`
	VMSummary           VMSummary     `json:"vmSummary"`
	Findings            []Finding     `json:"findings"`
}

type ConfigStatus struct {
	Present  bool   `json:"present"`
	Safe     bool   `json:"safe"`
	Complete bool   `json:"complete"`
	Valid    bool   `json:"valid"`
	Digest   string `json:"digest,omitempty"`
	Status   string `json:"status"`
}

type ManagedStatus struct {
	Present bool             `json:"present"`
	Valid   bool             `json:"valid"`
	Trust   upmcontext.Trust `json:"trust"`
}

type ClusterStatus struct {
	APIState          string `json:"apiState"`
	KubeconfigPresent bool   `json:"kubeconfigPresent"`
	ExpectedNodes     int    `json:"expectedNodes"`
	VagrantMachines   int    `json:"vagrantMachines"`
	LibvirtDomains    int    `json:"libvirtDomains"`
	KubernetesNodes   int    `json:"kubernetesNodes"`
}

type VMSummary struct {
	Expected     int `json:"expected"`
	Healthy      int `json:"healthy"`
	Degraded     int `json:"degraded"`
	Stopped      int `json:"stopped"`
	Missing      int `json:"missing"`
	Orphaned     int `json:"orphaned"`
	Inconsistent int `json:"inconsistent"`
	Unknown      int `json:"unknown"`
}

func Build(deployment upmcontext.Deployment, validation upmconfig.Result, machines *vm.List) Environment {
	environment := Environment{
		Mode:           modeFor(deployment),
		Health:         "UNKNOWN",
		RepositoryRoot: deployment.RepositoryRoot,
		Workspace:      deployment.Workspace,
		Config: ConfigStatus{
			Present:  validation.Path != "" && validation.Digest != "",
			Safe:     validation.Safe,
			Complete: validation.Complete,
			Valid:    validation.Valid,
			Digest:   validation.Digest,
			Status:   validation.Status,
		},
		ManagedState: ManagedStatus{
			Present: deployment.StateFile != "" && deployment.Trust != upmcontext.TrustLegacyReadOnly,
			Valid:   deployment.Managed,
			Trust:   deployment.Trust,
		},
		Cluster: ClusterStatus{
			APIState:          "unknown",
			KubeconfigPresent: deployment.Kubeconfig != "",
			ExpectedNodes:     validation.Config.NodeCount,
		},
		VMSummary: VMSummary{Expected: validation.Config.NodeCount},
		Findings:  []Finding{},
	}
	for _, message := range deployment.Findings {
		environment.Findings = append(environment.Findings, Finding{Code: "CONTEXT_FINDING", Severity: "warning", Scope: "context", Message: message})
	}
	for _, finding := range validation.Findings {
		environment.Findings = append(environment.Findings, Finding{
			Code:     finding.Code,
			Severity: finding.Severity,
			Scope:    "config",
			Resource: finding.Field,
			Message:  finding.Message,
		})
	}
	if machines == nil {
		if deployment.Trust == upmcontext.TrustInvalid || (validation.Digest != "" && !validation.Valid) {
			environment.Health = "INCONSISTENT"
		}
		return environment
	}

	complete := machines.Sources.Vagrant == "observed" && machines.Sources.Libvirt == "observed" && machines.Sources.Kubernetes == "observed"
	identityConflict := false
	for _, finding := range machines.Findings {
		environment.Findings = append(environment.Findings, Finding{Code: finding.Code, Severity: finding.Severity, Scope: finding.Source, Resource: finding.Resource, Message: finding.Message})
		if strings.Contains(finding.Code, "UNAVAILABLE") || strings.Contains(finding.Code, "NOT_FOUND") {
			complete = false
		}
		if strings.Contains(finding.Code, "UNEXPECTED") || strings.Contains(finding.Code, "DUPLICATE") {
			identityConflict = true
		}
	}
	for _, machine := range machines.Machines {
		if machine.VagrantState != "unknown" {
			environment.Cluster.VagrantMachines++
		}
		if machine.LibvirtState != "unknown" {
			environment.Cluster.LibvirtDomains++
		}
		if machine.Kubernetes.Present {
			environment.Cluster.KubernetesNodes++
		}
		for _, sourceState := range machine.Sources {
			if sourceState == "unavailable" || sourceState == "unknown" || sourceState == "not-checked" {
				complete = false
			}
		}
		for _, finding := range machine.Findings {
			environment.Findings = append(environment.Findings, Finding{Code: finding.Code, Severity: finding.Severity, Scope: finding.Source, Resource: machine.Name, Message: finding.Message})
			complete = false
		}
		switch machine.Health {
		case "RUNNING_HEALTHY":
			environment.VMSummary.Healthy++
		case "RUNNING_DEGRADED":
			environment.VMSummary.Degraded++
		case "STOPPED":
			environment.VMSummary.Stopped++
		case "MISSING":
			environment.VMSummary.Missing++
		case "ORPHANED":
			environment.VMSummary.Orphaned++
		case "INCONSISTENT":
			environment.VMSummary.Inconsistent++
		default:
			environment.VMSummary.Unknown++
		}
	}
	environment.Cluster.APIState = machines.Sources.KubernetesAPI
	if environment.Cluster.APIState == "" {
		environment.Cluster.APIState = "unknown"
	}
	environment.ObservationComplete = complete
	environment.Health = aggregateHealth(environment.VMSummary)
	if identityConflict {
		environment.Health = "INCONSISTENT"
	}
	return environment
}

func modeFor(deployment upmcontext.Deployment) string {
	switch deployment.Trust {
	case upmcontext.TrustManagedValid:
		return "managed"
	case upmcontext.TrustInvalid:
		return "invalid-managed"
	case upmcontext.TrustLegacyReadOnly:
		return "legacy-read-only"
	default:
		return "unknown"
	}
}

func aggregateHealth(summary VMSummary) string {
	if summary.Inconsistent > 0 || summary.Orphaned > 0 {
		return "INCONSISTENT"
	}
	if summary.Expected > 0 && summary.Healthy == summary.Expected {
		return "HEALTHY"
	}
	if summary.Expected > 0 && summary.Stopped == summary.Expected {
		return "STOPPED"
	}
	if summary.Degraded > 0 || summary.Stopped > 0 || summary.Missing > 0 {
		return "DEGRADED"
	}
	return "UNKNOWN"
}
