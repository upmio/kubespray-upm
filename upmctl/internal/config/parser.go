package config

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxConfigSize            = 1 << 20
	maxLineSize              = 16 << 10
	serviceCIDR              = "10.233.0.0/18"
	podCIDR                  = "10.233.64.0/18"
	allowedKubernetesVersion = "1.36.1"
)

type valueKind int

const (
	kindString valueKind = iota
	kindInt
	kindBool
	kindEmptyMap
	kindHomePath
)

type fieldSpec struct {
	kind      valueKind
	required  bool
	sensitive bool
}

type parsedValue struct {
	stringValue string
	intValue    int
	boolValue   bool
	kind        valueKind
	line        int
}

var (
	assignmentPattern = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.+)$`)
	homePathPattern   = regexp.MustCompile(`^ENV\[(?:'HOME'|"HOME")\]\s*\+\s*("(?:[^"\\]|\\["\\])*")$`)
	sizePattern       = regexp.MustCompile(`^[1-9][0-9]*(?:G|T)$`)
	versionPattern    = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	interfacePattern  = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,64}$`)
	namePattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)
	vgPattern         = regexp.MustCompile(`^[A-Za-z0-9_+][A-Za-z0-9_.+-]{0,126}$`)
	timeZonePattern   = regexp.MustCompile(`^[A-Za-z0-9_+.-]+(?:/[A-Za-z0-9_+.-]+)+$`)
	noProxyPattern    = regexp.MustCompile(`^[A-Za-z0-9.,:/_\[\]-]*$`)
)

var fields = map[string]fieldSpec{
	"instance_name_prefix":                  {kind: kindString, required: true},
	"vm_cpus":                               {kind: kindInt, required: true},
	"vm_memory":                             {kind: kindInt, required: true},
	"kube_master_vm_cpus":                   {kind: kindInt, required: true},
	"kube_master_vm_memory":                 {kind: kindInt, required: true},
	"upm_control_plane_vm_cpus":             {kind: kindInt, required: true},
	"upm_control_plane_vm_memory":           {kind: kindInt, required: true},
	"kube_node_instances_with_disks":        {kind: kindBool, required: true},
	"kube_node_instances_with_disks_size":   {kind: kindString, required: true},
	"kube_node_instances_with_disks_number": {kind: kindInt, required: true},
	"kube_node_instances_with_disk_dir":     {kind: kindHomePath, required: true},
	"kube_node_instances_with_disk_suffix":  {kind: kindString, required: true},
	"kube_node_instances_volume_group":      {kind: kindString, required: true},
	"kube_node_instances_create_vg":         {kind: kindBool, required: true},
	"num_instances":                         {kind: kindInt, required: true},
	"etcd_instances":                        {kind: kindInt, required: true},
	"kube_master_instances":                 {kind: kindInt, required: true},
	"upm_ctl_instances":                     {kind: kindInt, required: true},
	"provider":                              {kind: kindString},
	"time_zone":                             {kind: kindString, required: true},
	"ntp_enabled":                           {kind: kindString, required: true},
	"ntp_manage_config":                     {kind: kindString, required: true},
	"os":                                    {kind: kindString, required: true},
	"vm_network":                            {kind: kindString, required: true},
	"subnet_split4":                         {kind: kindInt, required: true},
	"subnet":                                {kind: kindString},
	"dns_server":                            {kind: kindString, required: true},
	"netmask":                               {kind: kindString},
	"gateway":                               {kind: kindString},
	"bridge_nic":                            {kind: kindString},
	"bridge_host_interface":                 {kind: kindString, required: true},
	"network_plugin":                        {kind: kindString, required: true},
	"cilium_kube_proxy_replacement":         {kind: kindBool, required: true},
	"cilium_load_balancer_enabled":          {kind: kindBool, required: true},
	"cilium_load_balancer_pool_name":        {kind: kindString, required: true},
	"cilium_load_balancer_start":            {kind: kindString, required: true},
	"cilium_load_balancer_stop":             {kind: kindString, required: true},
	"cilium_l2_announcement_interface":      {kind: kindString, required: true},
	"cert_manager_enabled":                  {kind: kindString, required: true},
	"local_path_provisioner_enabled":        {kind: kindString, required: true},
	"local_path_provisioner_claim_root":     {kind: kindString, required: true},
	"inventory":                             {kind: kindString, required: true},
	"shared_folders":                        {kind: kindEmptyMap, required: true},
	"kube_version":                          {kind: kindString, required: true},
	"ansible_verbosity":                     {kind: kindString},
	"http_proxy":                            {kind: kindString, sensitive: true},
	"https_proxy":                           {kind: kindString, sensitive: true},
	"no_proxy":                              {kind: kindString, sensitive: true},
	"additional_no_proxy":                   {kind: kindString, sensitive: true},
}

func ParseFile(path string) Result {
	result := Result{Path: path, Status: "UNSAFE", Findings: []Finding{}}
	info, err := os.Lstat(path)
	if err != nil {
		result.Findings = append(result.Findings, errorFinding("CONFIG_NOT_FOUND", "", 0, "config.rb was not found"))
		return result
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Size() > maxConfigSize {
		result.Findings = append(result.Findings, errorFinding("UNSAFE_CONFIG_FILE", "", 0, "config.rb must be a regular non-symlink file no larger than 1 MiB"))
		return result
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		result.Findings = append(result.Findings, errorFinding("CONFIG_READ_FAILED", "", 0, "config.rb could not be read"))
		return result
	}
	digest := sha256.Sum256(raw)
	result.Digest = "sha256:" + hex.EncodeToString(digest[:])
	if bytes.IndexByte(raw, 0) >= 0 || !utf8.Valid(raw) {
		result.Findings = append(result.Findings, errorFinding("UNSAFE_CONFIG_ENCODING", "", 0, "config.rb must be valid UTF-8 without NUL bytes"))
		return result
	}
	raw = bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF})

	values := map[string]parsedValue{}
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 1024), maxLineSize)
	lineNumber := 0
	unsafe := false
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(strings.TrimSuffix(scanner.Text(), "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		activity, stripErr := stripComment(line)
		if stripErr != nil {
			result.Findings = append(result.Findings, errorFinding("UNSAFE_RUBY_SYNTAX", "", lineNumber, stripErr.Error()))
			unsafe = true
			continue
		}
		activity = strings.TrimSpace(activity)
		if activity == "" {
			continue
		}
		match := assignmentPattern.FindStringSubmatch(activity)
		if match == nil {
			result.Findings = append(result.Findings, errorFinding("UNSAFE_RUBY_SYNTAX", "", lineNumber, "active line is outside the supported assignment subset"))
			unsafe = true
			continue
		}
		name, expression := match[1], strings.TrimSpace(match[2])
		spec, known := fields[name]
		if !known {
			result.Findings = append(result.Findings, errorFinding("UNSUPPORTED_ASSIGNMENT", name, lineNumber, "variable is not in the V1 configuration allowlist"))
			unsafe = true
			continue
		}
		if _, duplicate := values[name]; duplicate {
			result.Findings = append(result.Findings, errorFinding("DUPLICATE_FIELD", name, lineNumber, "field is assigned more than once"))
			unsafe = true
			continue
		}
		value, parseErr := parseValue(spec.kind, expression, lineNumber)
		if parseErr != nil {
			message := parseErr.Error()
			if spec.sensitive {
				message = "sensitive field uses unsupported or unsafe syntax"
			}
			result.Findings = append(result.Findings, errorFinding("UNSAFE_FIELD_VALUE", name, lineNumber, message))
			unsafe = true
			continue
		}
		values[name] = value
	}
	if err := scanner.Err(); err != nil {
		result.Findings = append(result.Findings, errorFinding("UNSAFE_CONFIG_LINE", "", lineNumber, "config.rb contains a line longer than 16 KiB"))
		unsafe = true
	}
	for name, spec := range fields {
		if spec.required {
			if _, ok := values[name]; !ok {
				result.Findings = append(result.Findings, errorFinding("MISSING_REQUIRED_FIELD", name, 0, "required field is missing"))
			}
		}
	}
	if unsafe {
		result.Status = "UNSAFE"
		return result
	}

	result.Config = buildConfig(values)
	result.Findings = append(result.Findings, validate(result.Config, values)...)
	hasError := false
	hasIncomplete := false
	for _, finding := range result.Findings {
		if finding.Severity == "error" {
			hasError = true
		}
		if finding.Code == "INCOMPLETE_CONFIG" {
			hasIncomplete = true
		}
	}
	result.Safe = true
	result.Complete = !hasIncomplete && !hasError
	result.Valid = !hasError
	switch {
	case hasError:
		result.Status = "INVALID"
	case hasIncomplete:
		result.Status = "SAFE_INCOMPLETE"
	default:
		result.Status = "SAFE_COMPLETE"
	}
	return result
}

func stripComment(line string) (string, error) {
	var quote byte
	escaped := false
	for index := 0; index < len(line); index++ {
		character := line[index]
		if escaped {
			escaped = false
			continue
		}
		if quote != 0 && character == '\\' {
			escaped = true
			continue
		}
		if character == '\'' || character == '"' {
			if quote == 0 {
				quote = character
			} else if quote == character {
				quote = 0
			}
			continue
		}
		if quote == 0 && character == '#' {
			return line[:index], nil
		}
		if quote == 0 && character == ';' {
			return "", fmt.Errorf("semicolon is not allowed")
		}
	}
	if quote != 0 || escaped {
		return "", fmt.Errorf("unterminated quoted value")
	}
	return line, nil
}

func parseValue(kind valueKind, expression string, line int) (parsedValue, error) {
	value := parsedValue{kind: kind, line: line}
	switch kind {
	case kindInt:
		if expression == "" || strings.HasPrefix(expression, "+") || strings.HasPrefix(expression, "-") {
			return value, fmt.Errorf("expected a non-negative decimal integer")
		}
		parsed, err := strconv.Atoi(expression)
		if err != nil {
			return value, fmt.Errorf("expected a decimal integer")
		}
		value.intValue = parsed
	case kindBool:
		if expression != "true" && expression != "false" {
			return value, fmt.Errorf("expected true or false")
		}
		value.boolValue = expression == "true"
	case kindEmptyMap:
		if expression != "{}" {
			return value, fmt.Errorf("only an empty map is supported")
		}
	case kindHomePath:
		match := homePathPattern.FindStringSubmatch(expression)
		if match == nil {
			return value, fmt.Errorf("expected the restricted ENV['HOME'] plus literal path expression")
		}
		path, err := parseQuotedString(match[1])
		if err != nil || !strings.HasPrefix(path, "/") || containsParentTraversal(path) {
			return value, fmt.Errorf("HOME path must be an absolute clean suffix without parent traversal")
		}
		value.stringValue = "$HOME" + path
	case kindString:
		parsed, err := parseQuotedString(expression)
		if err != nil {
			return value, err
		}
		value.stringValue = parsed
	}
	return value, nil
}

func parseQuotedString(expression string) (string, error) {
	if len(expression) < 2 || expression[0] != '"' || expression[len(expression)-1] != '"' {
		return "", fmt.Errorf("expected a double-quoted literal string")
	}
	if strings.Contains(expression, "#{") || strings.Contains(expression, "`") {
		return "", fmt.Errorf("Ruby interpolation and command syntax are not allowed")
	}
	parsed, err := strconv.Unquote(expression)
	if err != nil {
		return "", fmt.Errorf("string contains unsupported escaping")
	}
	for _, character := range parsed {
		if character < 0x20 && character != '\t' {
			return "", fmt.Errorf("string contains control characters")
		}
	}
	return parsed, nil
}

func buildConfig(values map[string]parsedValue) Config {
	stringValue := func(name string) string { return values[name].stringValue }
	intValue := func(name string) int { return values[name].intValue }
	boolValue := func(name string) bool { return values[name].boolValue }
	stringBool := func(name string) bool { return strings.EqualFold(stringValue(name), "true") }
	config := Config{
		Prefix:            stringValue("instance_name_prefix"),
		NodeCount:         intValue("num_instances"),
		EtcdCount:         intValue("etcd_instances"),
		ControlPlaneCount: intValue("kube_master_instances"),
		UPMCount:          intValue("upm_ctl_instances"),
		GuestOS:           stringValue("os"),
		KubernetesVersion: stringValue("kube_version"),
		NetworkPlugin:     stringValue("network_plugin"),
		TimeZone:          stringValue("time_zone"),
		Network: Network{
			Mode:                stringValue("vm_network"),
			SubnetPrefix:        stringValue("subnet"),
			SubnetSplit4:        intValue("subnet_split4"),
			Netmask:             stringValue("netmask"),
			Gateway:             stringValue("gateway"),
			DNS:                 stringValue("dns_server"),
			BridgeNIC:           stringValue("bridge_nic"),
			BridgeHostInterface: stringValue("bridge_host_interface"),
		},
		Cilium: Cilium{
			KubeProxyReplacement: boolValue("cilium_kube_proxy_replacement"),
			LoadBalancerEnabled:  boolValue("cilium_load_balancer_enabled"),
			PoolName:             stringValue("cilium_load_balancer_pool_name"),
			Start:                stringValue("cilium_load_balancer_start"),
			Stop:                 stringValue("cilium_load_balancer_stop"),
			L2Interface:          stringValue("cilium_l2_announcement_interface"),
		},
		Resources: ResourceProfile{
			WorkerCPU:       intValue("vm_cpus"),
			WorkerMemoryMiB: intValue("vm_memory"),
			ControlPlaneCPU: intValue("kube_master_vm_cpus"),
			ControlPlaneMiB: intValue("kube_master_vm_memory"),
			UPMCPU:          intValue("upm_control_plane_vm_cpus"),
			UPMMemoryMiB:    intValue("upm_control_plane_vm_memory"),
		},
		Storage: Storage{
			Enabled:      boolValue("kube_node_instances_with_disks"),
			DiskSize:     stringValue("kube_node_instances_with_disks_size"),
			DisksPerNode: intValue("kube_node_instances_with_disks_number"),
			Directory:    stringValue("kube_node_instances_with_disk_dir"),
			Suffix:       stringValue("kube_node_instances_with_disk_suffix"),
			VolumeGroup:  stringValue("kube_node_instances_volume_group"),
			CreateVG:     boolValue("kube_node_instances_create_vg"),
		},
		Inventory:          stringValue("inventory"),
		CertManagerEnabled: stringBool("cert_manager_enabled"),
		LocalPathEnabled:   stringBool("local_path_provisioner_enabled"),
	}
	for _, name := range []string{"http_proxy", "https_proxy", "no_proxy", "additional_no_proxy"} {
		if value, ok := values[name]; ok && value.stringValue != "" {
			config.ProxyConfigured = true
		}
	}
	workers := config.NodeCount - config.ControlPlaneCount - config.UPMCount
	config.Resources.TotalCPU = workers*config.Resources.WorkerCPU + config.ControlPlaneCount*config.Resources.ControlPlaneCPU + config.UPMCount*config.Resources.UPMCPU
	config.Resources.TotalMemoryMiB = workers*config.Resources.WorkerMemoryMiB + config.ControlPlaneCount*config.Resources.ControlPlaneMiB + config.UPMCount*config.Resources.UPMMemoryMiB
	return config
}

func validate(config Config, values map[string]parsedValue) []Finding {
	var findings []Finding
	addError := func(code, field, message string) {
		findings = append(findings, errorFinding(code, field, values[field].line, message))
	}
	addIncomplete := func(field, message string) {
		findings = append(findings, Finding{Code: "INCOMPLETE_CONFIG", Severity: "warning", Field: field, Line: values[field].line, Message: message})
	}

	if config.Prefix != "k8s" {
		addError("INVALID_PREFIX", "instance_name_prefix", "V1 requires the k8s node prefix")
	}
	if config.NodeCount < 3 || config.NodeCount > 8 {
		addError("INVALID_NODE_COUNT", "num_instances", "node count must be between 3 and 8")
	}
	if config.EtcdCount != 1 {
		addError("INVALID_ETCD_TOPOLOGY", "etcd_instances", "V1 requires exactly one etcd node")
	}
	if config.ControlPlaneCount != 1 {
		addError("INVALID_CONTROL_PLANE_TOPOLOGY", "kube_master_instances", "V1 requires exactly one control-plane node")
	}
	if config.UPMCount != 1 {
		addError("INVALID_UPM_TOPOLOGY", "upm_ctl_instances", "V1 requires exactly one UPM service node")
	}
	for _, field := range []string{"vm_cpus", "kube_master_vm_cpus", "upm_control_plane_vm_cpus"} {
		value := values[field].intValue
		if value < 1 || value > 64 {
			addError("INVALID_CPU", field, "CPU must be between 1 and 64")
		}
	}
	for _, field := range []string{"vm_memory", "kube_master_vm_memory", "upm_control_plane_vm_memory"} {
		value := values[field].intValue
		if value < 2048 || value > 262144 {
			addError("INVALID_MEMORY", field, "memory must be between 2048 and 262144 MiB")
		}
	}
	if config.GuestOS != "rockylinux9" {
		addError("UNSUPPORTED_GUEST_OS", "os", "V1 supports the rockylinux9 guest profile")
	}
	if !timeZonePattern.MatchString(config.TimeZone) {
		addError("INVALID_TIME_ZONE", "time_zone", "time zone contains unsupported characters or is not an IANA-style name")
	}
	for _, field := range []string{"ntp_enabled", "ntp_manage_config", "cert_manager_enabled", "local_path_provisioner_enabled"} {
		value := values[field].stringValue
		if value != "True" && value != "False" {
			addError("INVALID_STRING_BOOLEAN", field, "field must be the string True or False")
		}
	}
	if !versionPattern.MatchString(config.KubernetesVersion) || config.KubernetesVersion != allowedKubernetesVersion {
		addError("INVALID_KUBERNETES_VERSION", "kube_version", "Kubernetes version is not the pinned V1 release version")
	}
	if config.Inventory != "inventory/sample" || filepath.IsAbs(config.Inventory) || strings.Contains(filepath.Clean(config.Inventory), "..") {
		addError("INVALID_INVENTORY", "inventory", "V1 requires inventory/sample")
	}
	claimRoot := values["local_path_provisioner_claim_root"].stringValue
	if !filepath.IsAbs(claimRoot) || containsParentTraversal(claimRoot) || hasUnsafeText(claimRoot) {
		addError("INVALID_LOCAL_PATH_ROOT", "local_path_provisioner_claim_root", "local path root must be a clean absolute path")
	}
	if config.LocalPathEnabled {
		addError("UNSUPPORTED_LOCAL_PATH", "local_path_provisioner_enabled", "V1 requires Local Path Provisioner to remain disabled")
	}
	if config.Network.Mode != "nat" && config.Network.Mode != "bridge" {
		addError("INVALID_NETWORK_MODE", "vm_network", "network mode must be nat or bridge")
	}
	if net.ParseIP(config.Network.DNS).To4() == nil {
		addError("INVALID_DNS", "dns_server", "DNS server must be a valid IPv4 address")
	}
	if config.Network.SubnetSplit4 < 1 || config.Network.SubnetSplit4+config.NodeCount > 254 {
		addError("INVALID_VM_ADDRESS_RANGE", "subnet_split4", "VM address range exceeds valid host addresses")
	}
	if config.Network.Mode == "nat" && config.Network.SubnetSplit4 < 100 {
		addError("NAT_DHCP_OVERLAP", "subnet_split4", "NAT VM addresses must start after the reserved DHCP range")
	}
	if config.Network.Mode == "bridge" {
		for _, field := range []string{"subnet", "netmask", "gateway", "bridge_nic"} {
			if values[field].stringValue == "" {
				addError("MISSING_BRIDGE_FIELD", field, "bridge mode requires this field")
			}
		}
		if config.Network.BridgeHostInterface == "" {
			addIncomplete("bridge_host_interface", "generated bridge environments must bind the host physical interface")
		}
		if config.Network.BridgeNIC != "" && config.Network.BridgeNIC != "br0" {
			addError("INVALID_BRIDGE_NIC", "bridge_nic", "V1 requires bridge interface br0")
		}
		if config.Network.BridgeHostInterface != "" && !interfacePattern.MatchString(config.Network.BridgeHostInterface) {
			addError("INVALID_INTERFACE", "bridge_host_interface", "host interface contains unsupported characters")
		}
		if !validBridgeNetwork(config.Network, config.NodeCount) {
			addError("INVALID_BRIDGE_ADDRESSING", "subnet", "gateway and VM addresses must share a valid subnet without network or broadcast collisions")
		}
	}
	if config.NetworkPlugin != "calico" && config.NetworkPlugin != "cilium" {
		addError("INVALID_CNI", "network_plugin", "network plugin must be calico or cilium")
	}
	if config.NetworkPlugin == "calico" && (config.Cilium.KubeProxyReplacement || config.Cilium.LoadBalancerEnabled) {
		addError("CILIUM_OPTIONS_WITH_CALICO", "network_plugin", "Cilium options cannot be enabled with Calico")
	}
	if config.Cilium.LoadBalancerEnabled {
		if !config.Cilium.KubeProxyReplacement {
			addError("CILIUM_LB_REQUIRES_KPR", "cilium_load_balancer_enabled", "Cilium LoadBalancer requires kube-proxy replacement")
		}
		if net.ParseIP(config.Cilium.Start).To4() == nil || net.ParseIP(config.Cilium.Stop).To4() == nil {
			addError("INVALID_CILIUM_LB_RANGE", "cilium_load_balancer_start", "Cilium LoadBalancer start and stop must be valid IPv4 addresses")
		} else if ipv4Number(config.Cilium.Start) > ipv4Number(config.Cilium.Stop) {
			addError("INVALID_CILIUM_LB_RANGE", "cilium_load_balancer_start", "Cilium LoadBalancer range is reversed")
		}
		if !namePattern.MatchString(config.Cilium.PoolName) {
			addError("INVALID_CILIUM_POOL", "cilium_load_balancer_pool_name", "pool name contains unsupported characters")
		}
		if !interfacePattern.MatchString(config.Cilium.L2Interface) {
			addError("INVALID_INTERFACE", "cilium_l2_announcement_interface", "L2 interface contains unsupported characters")
		}
		if net.ParseIP(config.Cilium.Start).To4() != nil && net.ParseIP(config.Cilium.Stop).To4() != nil {
			validateLoadBalancerRange(config, values, &findings)
		}
	} else if config.Cilium.Start != "" || config.Cilium.Stop != "" {
		addError("DISABLED_CILIUM_LB_HAS_RANGE", "cilium_load_balancer_start", "disabled Cilium LoadBalancer must not retain an address range")
	}
	if config.Storage.Enabled {
		if !sizePattern.MatchString(config.Storage.DiskSize) {
			addError("INVALID_DISK_SIZE", "kube_node_instances_with_disks_size", "disk size must use a positive G or T suffix")
		}
		if config.Storage.DisksPerNode < 1 || config.Storage.DisksPerNode > 8 {
			addError("INVALID_DISK_COUNT", "kube_node_instances_with_disks_number", "disk count must be between 1 and 8")
		}
		if config.Storage.Suffix == "" || strings.ContainsAny(config.Storage.Suffix, `/\\ \t`) {
			addError("INVALID_DISK_SUFFIX", "kube_node_instances_with_disk_suffix", "disk suffix contains unsupported characters")
		}
		if !vgPattern.MatchString(config.Storage.VolumeGroup) {
			addError("INVALID_VOLUME_GROUP", "kube_node_instances_volume_group", "volume group name contains unsupported characters")
		}
	}
	if provider, ok := values["provider"]; ok && provider.stringValue != "libvirt" {
		addError("UNSUPPORTED_PROVIDER", "provider", "V1 only supports libvirt")
	}
	if verbosity, ok := values["ansible_verbosity"]; ok {
		if verbosity.stringValue != "v" && verbosity.stringValue != "vv" && verbosity.stringValue != "vvv" && verbosity.stringValue != "vvvv" {
			addError("INVALID_ANSIBLE_VERBOSITY", "ansible_verbosity", "verbosity must be v, vv, vvv or vvvv")
		}
	}
	for _, field := range []string{"http_proxy", "https_proxy"} {
		if proxy, ok := values[field]; ok && proxy.stringValue != "" {
			parsed, err := url.Parse(proxy.stringValue)
			if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || hasControlText(proxy.stringValue) {
				addError("INVALID_PROXY", field, "proxy must be a valid HTTP or HTTPS URL")
			}
		}
	}
	for _, field := range []string{"no_proxy", "additional_no_proxy"} {
		if value, ok := values[field]; ok && (len(value.stringValue) > 8192 || !noProxyPattern.MatchString(value.stringValue)) {
			addError("INVALID_NO_PROXY", field, "no_proxy contains unsupported characters or is too long")
		}
	}
	return findings
}

func validateLoadBalancerRange(config Config, values map[string]parsedValue, findings *[]Finding) {
	add := func(code, message string) {
		*findings = append(*findings, errorFinding(code, "cilium_load_balancer_start", values["cilium_load_balancer_start"].line, message))
	}
	start, stop := ipv4Number(config.Cilium.Start), ipv4Number(config.Cilium.Stop)
	if !rangeWithinVMNetwork(config, start, stop) {
		add("CILIUM_LB_OUTSIDE_L2_NETWORK", "Cilium LoadBalancer range must stay inside the VM L2 network and exclude network/broadcast addresses")
		return
	}
	for _, cidr := range []string{serviceCIDR, podCIDR} {
		if rangeOverlapsCIDR(start, stop, cidr) {
			add("CILIUM_LB_CLUSTER_CIDR_OVERLAP", "Cilium LoadBalancer range overlaps the fixed V1 Service or Pod CIDR")
			return
		}
	}
	prefix, gateway := "192.168.200", "192.168.200.1"
	if config.Network.Mode == "bridge" {
		prefix, gateway = config.Network.SubnetPrefix, config.Network.Gateway
	}
	for index := 1; index <= config.NodeCount; index++ {
		address := ipv4Number(fmt.Sprintf("%s.%d", prefix, config.Network.SubnetSplit4+index))
		if address >= start && address <= stop {
			add("CILIUM_LB_NODE_OVERLAP", "Cilium LoadBalancer range overlaps a VM node address")
			return
		}
	}
	gatewayNumber := ipv4Number(gateway)
	if gatewayNumber >= start && gatewayNumber <= stop {
		add("CILIUM_LB_GATEWAY_OVERLAP", "Cilium LoadBalancer range contains the VM gateway")
		return
	}
	if config.Network.Mode == "nat" {
		dhcpStart, dhcpStop := ipv4Number("192.168.200.2"), ipv4Number("192.168.200.100")
		if start <= dhcpStop && stop >= dhcpStart {
			add("CILIUM_LB_DHCP_OVERLAP", "Cilium LoadBalancer range overlaps the reserved NAT DHCP range")
		}
	}
}

func rangeWithinVMNetwork(config Config, start, stop uint32) bool {
	base := net.ParseIP("192.168.200.0").To4()
	mask := net.CIDRMask(24, 32)
	if config.Network.Mode == "bridge" {
		base = net.ParseIP(config.Network.SubnetPrefix + ".0").To4()
		maskIP := net.ParseIP(config.Network.Netmask).To4()
		if base == nil || maskIP == nil {
			return false
		}
		mask = net.IPMask(maskIP)
		if ones, bits := mask.Size(); bits != 32 || ones == 0 {
			return false
		}
	}
	network := base.Mask(mask)
	networkNumber := ipv4Number(network.String())
	ones, _ := mask.Size()
	size := uint32(1) << uint32(32-ones)
	broadcast := networkNumber + size - 1
	return start > networkNumber && stop < broadcast
}

func rangeOverlapsCIDR(start, stop uint32, cidr string) bool {
	ip, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	first := ipv4Number(ip.String())
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return false
	}
	size := uint32(1) << uint32(32-ones)
	last := first + size - 1
	return start <= last && stop >= first
}

func validBridgeNetwork(networkConfig Network, nodes int) bool {
	parts := strings.Split(networkConfig.SubnetPrefix, ".")
	if len(parts) != 3 {
		return false
	}
	base := net.ParseIP(networkConfig.SubnetPrefix + ".0").To4()
	gateway := net.ParseIP(networkConfig.Gateway).To4()
	maskIP := net.ParseIP(networkConfig.Netmask).To4()
	if base == nil || gateway == nil || maskIP == nil {
		return false
	}
	mask := net.IPMask(maskIP)
	ones, bits := mask.Size()
	if bits != 32 || ones == 0 {
		return false
	}
	networkAddress := base.Mask(mask)
	broadcast := make(net.IP, 4)
	for index := range networkAddress {
		broadcast[index] = networkAddress[index] | ^mask[index]
	}
	if !gateway.Mask(mask).Equal(networkAddress) || gateway.Equal(networkAddress) || gateway.Equal(broadcast) {
		return false
	}
	for index := 1; index <= nodes; index++ {
		address := net.ParseIP(fmt.Sprintf("%s.%d", networkConfig.SubnetPrefix, networkConfig.SubnetSplit4+index)).To4()
		if address == nil || !address.Mask(mask).Equal(networkAddress) || address.Equal(networkAddress) || address.Equal(broadcast) || address.Equal(gateway) {
			return false
		}
	}
	return true
}

func ipv4Number(value string) uint32 {
	ip := net.ParseIP(value).To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func containsParentTraversal(path string) bool {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func hasUnsafeText(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f || strings.ContainsRune(";`$|&<>\\", character) {
			return true
		}
	}
	return false
}

func hasControlText(value string) bool {
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return true
		}
	}
	return false
}

func errorFinding(code, field string, line int, message string) Finding {
	return Finding{Code: code, Severity: "error", Field: field, Line: line, Message: message}
}
