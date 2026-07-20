package vm

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	upmconfig "github.com/upmio/kubespray-upm/upmctl/internal/config"
	upmcontext "github.com/upmio/kubespray-upm/upmctl/internal/context"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
)

type Machine struct {
	Name            string            `json:"name"`
	Index           int               `json:"index,omitempty"`
	Expected        bool              `json:"expected"`
	Managed         bool              `json:"managed"`
	Role            string            `json:"role"`
	Health          string            `json:"health"`
	Consistency     string            `json:"consistency"`
	VagrantState    string            `json:"vagrantState"`
	LibvirtID       string            `json:"libvirtId,omitempty"`
	LibvirtState    string            `json:"libvirtState"`
	KubernetesState string            `json:"kubernetesState"`
	Identity        Identity          `json:"identity"`
	Power           Power             `json:"power"`
	Network         Network           `json:"network"`
	Kubernetes      Kubernetes        `json:"kubernetes"`
	Resources       Resources         `json:"resources"`
	Sources         map[string]string `json:"sources"`
	Findings        []Finding         `json:"findings,omitempty"`
}

type Identity struct {
	VagrantMachine    string `json:"vagrantMachine,omitempty"`
	LibvirtUUID       string `json:"libvirtUUID,omitempty"`
	DomainName        string `json:"domainName,omitempty"`
	KubernetesNode    string `json:"kubernetesNodeName,omitempty"`
	KubernetesNodeUID string `json:"kubernetesNodeUID,omitempty"`
}

type Power struct {
	Desired string `json:"desired"`
	Vagrant string `json:"vagrant"`
	Libvirt string `json:"libvirt"`
}

type Network struct {
	Addresses  []string `json:"addresses"`
	InternalIP string   `json:"internalIP,omitempty"`
	SSHHost    string   `json:"sshHost,omitempty"`
	SSHPort    int      `json:"sshPort,omitempty"`
	SSHState   string   `json:"sshState"`
}

type Kubernetes struct {
	Present       bool   `json:"present"`
	Ready         bool   `json:"ready"`
	Unschedulable bool   `json:"unschedulable"`
	State         string `json:"state"`
	UID           string `json:"-"`
	InternalIP    string `json:"-"`
}

type Resources struct {
	CPU           int    `json:"cpu"`
	MemoryMiB     int    `json:"memoryMiB"`
	DataDisks     int    `json:"dataDisks"`
	DataDiskSize  string `json:"dataDiskSize,omitempty"`
	ObservedDisks []Disk `json:"observedDisks"`
}

type Disk struct {
	Type   string `json:"type"`
	Device string `json:"device"`
	Target string `json:"target"`
	Source string `json:"source,omitempty"`
}

type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Resource string `json:"resource,omitempty"`
	Message  string `json:"message"`
}

type List struct {
	Workspace string      `json:"workspace"`
	Machines  []Machine   `json:"machines"`
	Sources   ListSources `json:"sources"`
	Findings  []Finding   `json:"findings,omitempty"`
}

type ListSources struct {
	Vagrant       string `json:"vagrant"`
	Libvirt       string `json:"libvirt"`
	Kubernetes    string `json:"kubernetes"`
	KubernetesAPI string `json:"kubernetesAPI"`
}

type domainObservation struct {
	Name string
	UUID string
}

type Service struct {
	runner runner.Runner
}

var (
	machineNamePattern = regexp.MustCompile(`^k8s-[1-8]$`)
	libvirtUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	domainNamePattern  = regexp.MustCompile(`^[A-Za-z0-9_.:+-]{1,128}$`)
	endpointPattern    = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,255}$`)
	diskTargetPattern  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	domainNodePattern  = regexp.MustCompile(`(^|[_-])(k8s-[0-9]+)$`)
)

func NewService(commandRunner runner.Runner) *Service {
	return &Service{runner: commandRunner}
}

func (s *Service) List(ctx context.Context, deployment upmcontext.Deployment, declared upmconfig.Config) (List, error) {
	if deployment.Workspace == "" {
		return List{}, fmt.Errorf("deployment workspace is not available")
	}

	if err := ctx.Err(); err != nil {
		return List{}, err
	}
	statusResult, err := s.runner.Run(ctx, runner.Command{
		Executable: "vagrant",
		Args:       []string{"status", "--machine-readable"},
		Dir:        deployment.Workspace,
	})
	vagrantAvailable := err == nil
	states := map[string]string{}
	list := List{Workspace: deployment.Workspace, Sources: ListSources{Vagrant: "unavailable", Libvirt: "unavailable", Kubernetes: "unavailable", KubernetesAPI: "unknown"}}
	if err != nil {
		if ctx.Err() != nil {
			return List{}, ctx.Err()
		}
		list.Findings = append(list.Findings, newFinding("VAGRANT_STATUS_UNAVAILABLE", "vagrant", "", "Vagrant status is unavailable"))
	} else {
		list.Sources.Vagrant = "observed"
		states, err = parseVagrantStatus(statusResult.Stdout)
		if err != nil {
			vagrantAvailable = false
			list.Findings = append(list.Findings, newFinding("VAGRANT_STATUS_INVALID", "vagrant", "", err.Error()))
		}
	}
	nodeStates, kubernetesFinding, kubernetesAvailable, err := s.readKubernetesNodes(ctx, deployment)
	if err != nil {
		return List{}, err
	}
	metadataNames, metadataFindings := listMetadataMachines(deployment.Workspace)
	domains, domainFindings, libvirtAvailable, err := s.readLibvirtInventory(ctx)
	if err != nil {
		return List{}, err
	}
	if libvirtAvailable {
		list.Sources.Libvirt = "observed"
	}
	if kubernetesAvailable {
		list.Sources.Kubernetes = "observed"
		list.Sources.KubernetesAPI = "reachable"
	} else if deployment.Kubeconfig == "" {
		list.Sources.Kubernetes = "absent"
	} else {
		list.Sources.KubernetesAPI = "unavailable"
	}

	nameSet := map[string]struct{}{}
	expected := map[string]upmconfig.ExpectedNode{}
	for _, node := range declared.ExpectedNodes() {
		expected[node.Name] = node
		nameSet[node.Name] = struct{}{}
	}
	for name := range states {
		if machineNamePattern.MatchString(name) {
			nameSet[name] = struct{}{}
		} else {
			list.Findings = append(list.Findings, newFinding("UNEXPECTED_VAGRANT_MACHINE", "vagrant", name, "Vagrant reported a machine outside the supported managed topology"))
		}
	}
	for _, name := range metadataNames {
		nameSet[name] = struct{}{}
	}
	for name := range deployment.MachineIDs {
		if machineNamePattern.MatchString(name) {
			nameSet[name] = struct{}{}
		}
	}
	for name := range domains {
		nameSet[name] = struct{}{}
	}
	for name := range nodeStates {
		if machineNamePattern.MatchString(name) {
			nameSet[name] = struct{}{}
		} else {
			list.Findings = append(list.Findings, newFinding("UNEXPECTED_KUBERNETES_NODE", "kubernetes", name, "Kubernetes reported a Node outside the supported managed topology"))
		}
	}
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		left, right := nodeIndex(names[i]), nodeIndex(names[j])
		if left == right {
			return names[i] < names[j]
		}
		return left < right
	})

	list.Findings = append(list.Findings, metadataFindings...)
	list.Findings = append(list.Findings, domainFindings...)
	if kubernetesFinding != nil {
		list.Findings = append(list.Findings, *kubernetesFinding)
	}
	for _, name := range names {
		expectedNode, isExpected := expected[name]
		vagrantState := states[name]
		if vagrantState == "" {
			vagrantState = "unknown"
		}
		machine := Machine{
			Name:            name,
			Index:           nodeIndex(name),
			Expected:        isExpected,
			Managed:         deployment.Managed && isExpected,
			Role:            expectedNode.Role,
			VagrantState:    normalize(vagrantState),
			LibvirtState:    "unknown",
			KubernetesState: nodeStates[name].State,
			Power:           Power{Desired: "unknown", Vagrant: normalize(vagrantState), Libvirt: "unknown"},
			Network:         Network{Addresses: []string{}, SSHState: "unknown"},
			Kubernetes:      nodeStates[name],
			Resources: Resources{
				CPU:           expectedNode.CPU,
				MemoryMiB:     expectedNode.MemoryMiB,
				DataDisks:     expectedNode.DataDisks,
				DataDiskSize:  expectedNode.DataDiskSize,
				ObservedDisks: []Disk{},
			},
			Sources: map[string]string{
				"config":           sourceState(isExpected),
				"vagrant":          sourceAvailability(vagrantAvailable, states[name] != ""),
				"libvirt":          "unknown",
				"libvirtInventory": sourceAvailability(libvirtAvailable, domains[name].UUID != ""),
				"libvirtInfo":      "not-applicable",
				"blockDevices":     "not-applicable",
				"kubernetes":       sourceAvailability(kubernetesAvailable, nodeStates[name].Present),
				"ssh":              "not-applicable",
			},
		}
		if machine.Role == "" {
			machine.Role = "unknown"
		}
		if machine.KubernetesState == "" {
			machine.KubernetesState = "unknown"
			machine.Kubernetes.State = "unknown"
		}
		if machine.Kubernetes.Present {
			machine.Identity.KubernetesNode = name
			machine.Identity.KubernetesNodeUID = machine.Kubernetes.UID
			machine.Network.InternalIP = machine.Kubernetes.InternalIP
			if machine.Network.InternalIP != "" {
				machine.Network.Addresses = append(machine.Network.Addresses, machine.Network.InternalIP)
			}
		}
		if states[name] != "" {
			machine.Identity.VagrantMachine = name
		}

		metadataID, identityFinding := readLibvirtID(deployment.Workspace, name)
		stateID := deployment.MachineIDs[name]
		inventoryID := domains[name].UUID
		identityConflict := identitiesConflict(metadataID, stateID, inventoryID)
		observedCreated := states[name] != "" && states[name] != "not_created" && states[name] != "not created" && states[name] != "unknown"
		if isExpected && stateID == "" && (metadataID != "" || inventoryID != "" || machine.Kubernetes.Present || observedCreated) {
			identityConflict = true
			machine.Findings = append(machine.Findings, newFinding("MANAGED_UUID_MISSING", "managed-state", name, "managed state UUID is missing for an observed expected machine"))
		}
		if isExpected && metadataID == "" && (stateID != "" || inventoryID != "" || observedCreated) {
			identityConflict = true
		}
		switch {
		case identityConflict:
			machine.LibvirtID = firstIdentity(stateID, metadataID, inventoryID)
			machine.Findings = append(machine.Findings, newFinding("VM_IDENTITY_INCONSISTENT", "identity", name, "managed state, Vagrant metadata and libvirt inventory identities are incomplete or inconsistent"))
		case stateID != "":
			machine.LibvirtID = stateID
		case metadataID != "":
			machine.LibvirtID = metadataID
		case inventoryID != "":
			machine.LibvirtID = inventoryID
		}
		if identityFinding != "" {
			if stateID == "" {
				machine.Findings = append(machine.Findings, newFinding("VAGRANT_METADATA_ID_MISSING", "vagrant-metadata", name, identityFinding))
			} else {
				machine.Findings = append(machine.Findings, newFinding("VAGRANT_METADATA_ID_MISSING", "vagrant-metadata", name, "Vagrant metadata identity is missing; using the managed state UUID"))
			}
		}
		if machine.LibvirtID != "" {
			if domains[name].Name != "" {
				machine.Identity.DomainName = domains[name].Name
			}
			machine.Identity.LibvirtUUID = machine.LibvirtID
			result, runErr := s.runner.Run(ctx, runner.Command{
				Executable: "virsh",
				Args:       []string{"domstate", machine.LibvirtID},
			})
			if runErr != nil {
				if ctx.Err() != nil {
					return List{}, ctx.Err()
				}
				machine.Findings = append(machine.Findings, newFinding("LIBVIRT_DOMAIN_STATE_UNAVAILABLE", "libvirt", name, "libvirt domain state is unavailable"))
				machine.Sources["libvirt"] = "unavailable"
			} else {
				machine.LibvirtState = normalize(result.Stdout)
				machine.Power.Libvirt = machine.LibvirtState
				machine.Sources["libvirt"] = "observed"
				domInfo, infoErr := s.runner.Run(ctx, runner.Command{Executable: "virsh", Args: []string{"dominfo", machine.LibvirtID}})
				if infoErr == nil {
					machine.Sources["libvirtInfo"] = "observed"
					name, cpu, memory := parseDomInfo(domInfo.Stdout)
					machine.Identity.DomainName = name
					if cpu > 0 {
						machine.Resources.CPU = cpu
					}
					if memory > 0 {
						machine.Resources.MemoryMiB = memory
					}
				} else {
					machine.Sources["libvirtInfo"] = "unavailable"
				}
				blockInfo, blockErr := s.runner.Run(ctx, runner.Command{Executable: "virsh", Args: []string{"domblklist", machine.LibvirtID, "--details"}})
				if blockErr == nil {
					machine.Resources.ObservedDisks = parseBlockDevices(blockInfo.Stdout)
					machine.Sources["blockDevices"] = "observed"
				} else {
					machine.Sources["blockDevices"] = "unavailable"
				}
			}
		} else if libvirtAvailable {
			machine.Sources["libvirt"] = "absent"
		}
		if states[name] != "" && machine.VagrantState != "not_created" && machine.VagrantState != "not created" && machine.VagrantState != "unknown" {
			machine.Sources["ssh"] = "unavailable"
			sshResult, sshErr := s.runner.Run(ctx, runner.Command{Executable: "vagrant", Args: []string{"ssh-config", name}, Dir: deployment.Workspace})
			if sshErr == nil {
				machine.Network.SSHHost, machine.Network.SSHPort = parseSSHConfig(sshResult.Stdout)
				if machine.Network.SSHHost != "" && machine.Network.SSHPort > 0 {
					machine.Network.SSHState = "endpoint-configured"
					machine.Sources["ssh"] = "endpoint-configured"
					if !contains(machine.Network.Addresses, machine.Network.SSHHost) {
						machine.Network.Addresses = append(machine.Network.Addresses, machine.Network.SSHHost)
					}
					if machine.VagrantState == "running" && machine.LibvirtState == "running" {
						_, probeErr := s.runner.Run(ctx, runner.Command{
							Executable: "vagrant",
							Args:       []string{"ssh", name, "-c", "true"},
							Dir:        deployment.Workspace,
						})
						if probeErr == nil {
							machine.Network.SSHState = "reachable"
							machine.Sources["ssh"] = "observed"
						} else if ctx.Err() != nil {
							return List{}, ctx.Err()
						} else {
							machine.Network.SSHState = "unavailable"
							machine.Sources["ssh"] = "unavailable"
							machine.Findings = append(machine.Findings, newFinding("SSH_PROBE_FAILED", "ssh", name, "fixed read-only SSH reachability probe failed"))
						}
					}
				}
			}
		}
		machine.Health = deriveHealth(machine, vagrantAvailable, kubernetesAvailable)
		if identityConflict {
			machine.Health = "INCONSISTENT"
		}
		machine.Consistency = consistencyFor(machine.Health)
		list.Machines = append(list.Machines, machine)
	}
	markDuplicateUUIDs(&list)

	return list, nil
}

func (s *Service) readKubernetesNodes(ctx context.Context, deployment upmcontext.Deployment) (map[string]Kubernetes, *Finding, bool, error) {
	states := map[string]Kubernetes{}
	if deployment.Kubeconfig == "" {
		finding := newFinding("KUBECONFIG_NOT_FOUND", "kubernetes", "", "kubeconfig was not found; Kubernetes state is unknown")
		return states, &finding, false, nil
	}
	result, err := s.runner.Run(ctx, runner.Command{
		Executable: "kubectl",
		Args:       []string{"--kubeconfig", deployment.Kubeconfig, "get", "nodes", "-o", "json"},
	})
	if err != nil {
		if ctx.Err() != nil {
			return states, nil, false, ctx.Err()
		}
		finding := newFinding("KUBERNETES_API_UNAVAILABLE", "kubernetes", "", "Kubernetes API state is unavailable")
		return states, &finding, false, nil
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
				UID  string `json:"uid"`
			} `json:"metadata"`
			Spec struct {
				Unschedulable bool `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				Addresses []struct {
					Type    string `json:"type"`
					Address string `json:"address"`
				} `json:"addresses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &payload); err != nil {
		finding := newFinding("KUBERNETES_NODE_OUTPUT_INVALID", "kubernetes", "", "Kubernetes Node output could not be parsed")
		return states, &finding, false, nil
	}
	for _, item := range payload.Items {
		node := Kubernetes{Present: true, State: "not-ready", Unschedulable: item.Spec.Unschedulable, UID: item.Metadata.UID}
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" && condition.Status == "True" {
				node.Ready = true
				node.State = "ready"
				break
			}
		}
		for _, address := range item.Status.Addresses {
			if address.Type == "InternalIP" {
				node.InternalIP = address.Address
				break
			}
		}
		states[item.Metadata.Name] = node
	}
	return states, nil, true, nil
}

func (s *Service) readLibvirtInventory(ctx context.Context) (map[string]domainObservation, []Finding, bool, error) {
	domains := map[string]domainObservation{}
	result, err := s.runner.Run(ctx, runner.Command{Executable: "virsh", Args: []string{"list", "--all", "--name"}})
	if err != nil {
		if ctx.Err() != nil {
			return nil, nil, false, ctx.Err()
		}
		return domains, []Finding{newFinding("LIBVIRT_INVENTORY_UNAVAILABLE", "libvirt", "", "libvirt domain inventory is unavailable")}, false, nil
	}
	var findings []Finding
	for _, rawName := range strings.Split(result.Stdout, "\n") {
		domainName := strings.TrimSpace(rawName)
		if domainName == "" || !domainNamePattern.MatchString(domainName) {
			continue
		}
		match := domainNodePattern.FindStringSubmatch(domainName)
		if match == nil {
			continue
		}
		nodeName := match[2]
		if !machineNamePattern.MatchString(nodeName) {
			findings = append(findings, newFinding("UNEXPECTED_LIBVIRT_DOMAIN", "libvirt", domainName, "libvirt reported a domain outside the supported managed topology"))
			continue
		}
		uuidResult, uuidErr := s.runner.Run(ctx, runner.Command{Executable: "virsh", Args: []string{"domuuid", domainName}})
		if uuidErr != nil || !libvirtUUIDPattern.MatchString(strings.TrimSpace(uuidResult.Stdout)) {
			findings = append(findings, newFinding("LIBVIRT_DOMAIN_UUID_UNAVAILABLE", "libvirt", nodeName, "libvirt domain UUID is unavailable for the node candidate"))
			continue
		}
		if _, duplicate := domains[nodeName]; duplicate {
			findings = append(findings, newFinding("DUPLICATE_LIBVIRT_DOMAIN", "libvirt", nodeName, "multiple libvirt domains map to the managed node candidate"))
			continue
		}
		domains[nodeName] = domainObservation{Name: domainName, UUID: strings.TrimSpace(uuidResult.Stdout)}
	}
	return domains, findings, true, nil
}

func parseVagrantStatus(raw string) (map[string]string, error) {
	states := map[string]string{}
	reader := csv.NewReader(strings.NewReader(raw))
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("parse Vagrant machine-readable status: %w", err)
		}
		if len(record) < 4 || record[2] != "state" || record[1] == "" {
			continue
		}
		states[record[1]] = record[3]
	}
	if len(states) == 0 {
		return nil, fmt.Errorf("Vagrant returned no machine states")
	}
	return states, nil
}

func listMetadataMachines(workspace string) ([]string, []Finding) {
	root := filepath.Join(workspace, ".vagrant", "machines")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, []Finding{newFinding("VAGRANT_METADATA_UNAVAILABLE", "vagrant-metadata", "", "Vagrant machine metadata is unavailable")}
	}
	var names []string
	var findings []Finding
	for _, entry := range entries {
		if !entry.IsDir() || !machineNamePattern.MatchString(entry.Name()) {
			if entry.Name() != ".DS_Store" {
				findings = append(findings, newFinding("UNEXPECTED_VAGRANT_METADATA", "vagrant-metadata", entry.Name(), "Vagrant metadata entry is outside the supported managed topology"))
			}
			continue
		}
		names = append(names, entry.Name())
	}
	return names, findings
}

func readLibvirtID(workspace, name string) (string, string) {
	if !machineNamePattern.MatchString(name) {
		return "", "machine name is outside the supported k8s-1..k8s-8 topology"
	}
	path := filepath.Join(workspace, ".vagrant", "machines", name, "libvirt", "id")
	info, err := os.Lstat(path)
	if err != nil {
		return "", "libvirt identity is missing from Vagrant metadata"
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > 128 {
		return "", "libvirt identity metadata is not a safe regular file"
	}
	workspaceRoot, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		return "", "workspace path cannot be resolved safely"
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "libvirt identity path cannot be resolved safely"
	}
	allowedRoot := filepath.Join(workspaceRoot, ".vagrant", "machines") + string(os.PathSeparator)
	if !strings.HasPrefix(resolvedPath, allowedRoot) {
		return "", "libvirt identity path escapes the deployment workspace"
	}
	contents, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", "libvirt identity cannot be read"
	}
	identity := strings.TrimSpace(string(contents))
	if !libvirtUUIDPattern.MatchString(identity) {
		return "", "libvirt identity is not a valid UUID"
	}
	return identity, ""
}

func nodeIndex(name string) int {
	separator := strings.LastIndex(name, "-")
	if separator < 0 || separator == len(name)-1 {
		return 0
	}
	value, err := strconv.Atoi(name[separator+1:])
	if err != nil {
		return 0
	}
	return value
}

func normalize(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func deriveHealth(machine Machine, vagrantAvailable, kubernetesAvailable bool) string {
	if !machine.Expected && (machine.VagrantState != "unknown" || machine.LibvirtID != "" || machine.Kubernetes.Present) {
		return "ORPHANED"
	}
	switch machine.VagrantState {
	case "poweroff", "shutoff", "shut off":
		if machine.LibvirtState == "running" || machine.KubernetesState == "ready" {
			return "INCONSISTENT"
		}
		if (machine.LibvirtState == "shut off" || machine.LibvirtState == "shutoff") && kubernetesAvailable && !machine.Kubernetes.Ready {
			return "STOPPED"
		}
		return "UNKNOWN"
	case "saved":
		return "INCONSISTENT"
	case "not_created", "not created":
		if machine.LibvirtID != "" || machine.KubernetesState != "unknown" {
			if machine.Expected {
				return "INCONSISTENT"
			}
			return "ORPHANED"
		}
		if vagrantAvailable && kubernetesAvailable {
			return "MISSING"
		}
		return "UNKNOWN"
	case "running":
		if machine.LibvirtState == "running" && machine.KubernetesState == "ready" && machine.Network.SSHState == "reachable" {
			return "RUNNING_HEALTHY"
		}
		return "RUNNING_DEGRADED"
	case "unknown":
		if machine.Expected {
			if machine.LibvirtState == "running" || machine.Kubernetes.Present {
				return "RUNNING_DEGRADED"
			}
			if vagrantAvailable && kubernetesAvailable {
				return "MISSING"
			}
			return "UNKNOWN"
		}
		if machine.LibvirtID != "" || machine.KubernetesState != "unknown" {
			return "ORPHANED"
		}
		return "UNKNOWN"
	default:
		return "UNKNOWN"
	}
}

func markDuplicateUUIDs(list *List) {
	seen := map[string]int{}
	for index := range list.Machines {
		uuid := list.Machines[index].LibvirtID
		if uuid == "" {
			continue
		}
		if previous, duplicate := seen[uuid]; duplicate {
			message := "libvirt UUID is duplicated across Vagrant machine metadata"
			list.Machines[previous].Findings = append(list.Machines[previous].Findings, newFinding("DUPLICATE_LIBVIRT_UUID", "identity", list.Machines[previous].Name, message))
			list.Machines[previous].Health = "INCONSISTENT"
			list.Machines[previous].Consistency = "inconsistent"
			list.Machines[index].Findings = append(list.Machines[index].Findings, newFinding("DUPLICATE_LIBVIRT_UUID", "identity", list.Machines[index].Name, message))
			list.Machines[index].Health = "INCONSISTENT"
			list.Machines[index].Consistency = "inconsistent"
			continue
		}
		seen[uuid] = index
	}
}

func parseDomInfo(raw string) (string, int, int) {
	var name string
	var cpu int
	var memoryMiB int
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		switch key {
		case "Name":
			if domainNamePattern.MatchString(value) {
				name = value
			}
		case "CPU(s)":
			cpu, _ = strconv.Atoi(value)
		case "Max memory":
			fields := strings.Fields(value)
			if len(fields) > 0 {
				kib, _ := strconv.Atoi(fields[0])
				memoryMiB = kib / 1024
			}
		}
	}
	return name, cpu, memoryMiB
}

func parseSSHConfig(raw string) (string, int) {
	var host string
	var port int
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "HostName":
			if endpointPattern.MatchString(fields[1]) {
				host = fields[1]
			}
		case "Port":
			port, _ = strconv.Atoi(fields[1])
			if port < 1 || port > 65535 {
				port = 0
			}
		}
	}
	return host, port
}

func parseBlockDevices(raw string) []Disk {
	var disks []Disk
	for _, line := range strings.Split(raw, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[0] == "Type" || strings.HasPrefix(fields[0], "-") {
			continue
		}
		if fields[1] != "disk" || !diskTargetPattern.MatchString(fields[2]) || len(fields[3]) > 4096 || hasControl(fields[3]) {
			continue
		}
		disks = append(disks, Disk{Type: fields[0], Device: fields[1], Target: fields[2], Source: fields[3]})
	}
	return disks
}

func hasControl(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func consistencyFor(health string) string {
	switch health {
	case "RUNNING_HEALTHY", "STOPPED":
		return "consistent"
	case "INCONSISTENT", "ORPHANED":
		return "inconsistent"
	case "RUNNING_DEGRADED":
		return "degraded"
	default:
		return "unknown"
	}
}

func sourceState(present bool) string {
	if present {
		return "declared"
	}
	return "absent"
}

func sourceAvailability(available, present bool) string {
	if !available {
		return "unavailable"
	}
	if present {
		return "observed"
	}
	return "absent"
}

func contains(values []string, candidate string) bool {
	for _, value := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func identitiesConflict(identities ...string) bool {
	var first string
	for _, identity := range identities {
		if identity == "" {
			continue
		}
		if first == "" {
			first = identity
			continue
		}
		if first != identity {
			return true
		}
	}
	return false
}

func firstIdentity(identities ...string) string {
	for _, identity := range identities {
		if identity != "" {
			return identity
		}
	}
	return ""
}

func newFinding(code, source, resource, message string) Finding {
	return Finding{Code: code, Severity: "warning", Source: source, Resource: resource, Message: message}
}
