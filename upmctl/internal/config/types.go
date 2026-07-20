package config

type Finding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Field    string `json:"field,omitempty"`
	Line     int    `json:"line,omitempty"`
	Message  string `json:"message"`
}

type Result struct {
	Path     string    `json:"path"`
	Digest   string    `json:"digest"`
	Status   string    `json:"status"`
	Safe     bool      `json:"safe"`
	Complete bool      `json:"complete"`
	Valid    bool      `json:"valid"`
	Config   Config    `json:"config"`
	Findings []Finding `json:"findings"`
}

type Config struct {
	Prefix             string          `json:"prefix"`
	NodeCount          int             `json:"nodeCount"`
	EtcdCount          int             `json:"etcdCount"`
	ControlPlaneCount  int             `json:"controlPlaneCount"`
	UPMCount           int             `json:"upmCount"`
	GuestOS            string          `json:"guestOS"`
	KubernetesVersion  string          `json:"kubernetesVersion"`
	NetworkPlugin      string          `json:"networkPlugin"`
	TimeZone           string          `json:"timeZone"`
	Network            Network         `json:"network"`
	Cilium             Cilium          `json:"cilium"`
	Resources          ResourceProfile `json:"resources"`
	Storage            Storage         `json:"storage"`
	Inventory          string          `json:"inventory"`
	CertManagerEnabled bool            `json:"certManagerEnabled"`
	LocalPathEnabled   bool            `json:"localPathEnabled"`
	ProxyConfigured    bool            `json:"proxyConfigured"`
}

type Network struct {
	Mode                string `json:"mode"`
	SubnetPrefix        string `json:"subnetPrefix,omitempty"`
	SubnetSplit4        int    `json:"subnetSplit4"`
	Netmask             string `json:"netmask,omitempty"`
	Gateway             string `json:"gateway,omitempty"`
	DNS                 string `json:"dns"`
	BridgeNIC           string `json:"bridgeNIC,omitempty"`
	BridgeHostInterface string `json:"bridgeHostInterface,omitempty"`
}

type Cilium struct {
	KubeProxyReplacement bool   `json:"kubeProxyReplacement"`
	LoadBalancerEnabled  bool   `json:"loadBalancerEnabled"`
	PoolName             string `json:"poolName,omitempty"`
	Start                string `json:"start,omitempty"`
	Stop                 string `json:"stop,omitempty"`
	L2Interface          string `json:"l2Interface,omitempty"`
}

type ResourceProfile struct {
	WorkerCPU       int `json:"workerCPU"`
	WorkerMemoryMiB int `json:"workerMemoryMiB"`
	ControlPlaneCPU int `json:"controlPlaneCPU"`
	ControlPlaneMiB int `json:"controlPlaneMemoryMiB"`
	UPMCPU          int `json:"upmCPU"`
	UPMMemoryMiB    int `json:"upmMemoryMiB"`
	TotalCPU        int `json:"totalCPU"`
	TotalMemoryMiB  int `json:"totalMemoryMiB"`
}

type Storage struct {
	Enabled      bool   `json:"enabled"`
	DiskSize     string `json:"diskSize,omitempty"`
	DisksPerNode int    `json:"disksPerNode"`
	Directory    string `json:"directory,omitempty"`
	Suffix       string `json:"suffix,omitempty"`
	VolumeGroup  string `json:"volumeGroup,omitempty"`
	CreateVG     bool   `json:"createVG"`
}

type ExpectedNode struct {
	Name         string `json:"name"`
	Index        int    `json:"index"`
	Role         string `json:"role"`
	CPU          int    `json:"cpu"`
	MemoryMiB    int    `json:"memoryMiB"`
	DataDisks    int    `json:"dataDisks"`
	DataDiskSize string `json:"dataDiskSize,omitempty"`
}

func (c Config) ExpectedNodes() []ExpectedNode {
	nodes := make([]ExpectedNode, 0, c.NodeCount)
	controlBoundary := c.ControlPlaneCount
	if c.EtcdCount > controlBoundary {
		controlBoundary = c.EtcdCount
	}
	for index := 1; index <= c.NodeCount; index++ {
		node := ExpectedNode{Name: c.Prefix + "-" + itoa(index), Index: index, Role: "worker"}
		switch {
		case index <= controlBoundary:
			node.Role = "control-plane-etcd-worker"
			node.CPU = c.Resources.ControlPlaneCPU
			node.MemoryMiB = c.Resources.ControlPlaneMiB
		case index <= controlBoundary+c.UPMCount:
			node.Role = "upm-service-worker"
			node.CPU = c.Resources.UPMCPU
			node.MemoryMiB = c.Resources.UPMMemoryMiB
		default:
			node.CPU = c.Resources.WorkerCPU
			node.MemoryMiB = c.Resources.WorkerMemoryMiB
		}
		if c.Storage.Enabled && index > controlBoundary {
			node.DataDisks = c.Storage.DisksPerNode
			node.DataDiskSize = c.Storage.DiskSize
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var buffer [20]byte
	position := len(buffer)
	for value > 0 {
		position--
		buffer[position] = byte('0' + value%10)
		value /= 10
	}
	return string(buffer[position:])
}
