# Vagrant configuration file for Kubespray
# Kubespray Vagrant Configuration Sample
# This file allows you to customize various settings for your Vagrant environment
# Copy this file to vagrant/config.rb and modify the values according to your needs

# =============================================================================
# PROXY CONFIGURATION
# =============================================================================
# Configure proxy settings for the cluster if you're behind a corporate firewall
# Leave empty or comment out if no proxy is needed

# HTTP proxy URL - used for HTTP traffic
# Example: "http://proxy.company.com:8080"
# $http_proxy = ""

# HTTPS proxy URL - used for HTTPS traffic
# Example: "https://proxy.company.com:8080"
# $https_proxy = ""

# No proxy list - comma-separated list of hosts/domains that should bypass proxy
# Common entries: localhost, 127.0.0.1, local domains, cluster subnets
# Example: "localhost,127.0.0.1,.local,.company.com,10.0.0.0/8,192.168.0.0/16"
# $no_proxy = ""

# Additional no proxy entries - will be added to the default no_proxy list
# Use this to add extra domains without overriding the defaults
# Example: ".internal,.corp,.k8s.local"
# $additional_no_proxy = ""

# =============================================================================
# ANSIBLE CONFIGURATION
# =============================================================================
# Ansible verbosity level for debugging (uncomment to enable)
# Options: "v" (verbose), "vv" (more verbose), "vvv" (debug), "vvvv" (connection debug)
#$ansible_verbosity = "vvv"

# =============================================================================
# VIRTUAL MACHINE CONFIGURATION
# =============================================================================
# Prefix for VM instance names (will be followed by node number)
$instance_name_prefix = "k8s"

# Default CPU and memory settings for worker nodes
$vm_cpus = 8                    # Number of CPU cores per worker node
$vm_memory = 16384              # Memory in MB per worker node (16GB)

# Master/Control plane node resources
$kube_master_vm_cpus = 4        # CPU cores for Kubernetes master nodes
$kube_master_vm_memory = 4096   # Memory in MB for Kubernetes master nodes (4GB)

# UPM Control plane node resources (if using UPM)
$upm_control_plane_vm_cpus = 4      # CPU cores for UPM control plane
$upm_control_plane_vm_memory = 4096 # Memory in MB for UPM control plane (24GB)

# =============================================================================
# STORAGE CONFIGURATION
# =============================================================================
# Enable additional disks for worker nodes (useful for storage testing)
$kube_node_instances_with_disks = true

# Size of additional disks in GB (200GB in this example)
$kube_node_instances_with_disks_size = "200G"

# Number of additional disks per node
$kube_node_instances_with_disks_number = 1

# Directory to store additional disk files
$kube_node_instances_with_disk_dir = ENV['HOME'] + "/kubespray_vm_disk/upm_disks"

# Suffix for disk file names
$kube_node_instances_with_disk_suffix = "upm"

# VolumeGroup configuration for additional disks
# Name of the VolumeGroup to create for additional disks
$kube_node_instances_volume_group = "local_vg_dev"

# Enable automatic VolumeGroup creation for additional disks
$kube_node_instances_create_vg = true

# =============================================================================
# CLUSTER TOPOLOGY
# =============================================================================
# Total number of nodes in the cluster (masters + workers)
$num_instances = 5

# Number of etcd instances (should be odd number: 1, 3, 5, etc.)
$etcd_instances = 1

# Number of Kubernetes master/control plane instances
$kube_master_instances = 1

# Number of UPM control instances (if using UPM)
$upm_ctl_instances = 1

# =============================================================================
# SYSTEM CONFIGURATION
# =============================================================================
# Vagrant Provider Configuration
# Specify the Vagrant provider to use for virtual machines
# If not set, Vagrant will auto-detect available providers in this order:
# 1. Command line --provider argument (highest priority)
# 2. VAGRANT_DEFAULT_PROVIDER environment variable
# 3. Auto-detection of installed providers (libvirt only)
# 
# Supported options: "libvirt"
# 
# Provider recommendations:
# - libvirt: Good for Linux production environments (KVM-based)
# 
# Leave commented for auto-detection, or uncomment and set to force libvirt provider
# $provider = "libvirt"

# Timezone for all VMs
$time_zone = "Asia/Shanghai"

# Ntp Sever Configuration
$ntp_enabled = "True"
$ntp_manage_config = "True"

# Operating system for VMs
# Supported options: "ubuntu2004", "ubuntu2204", "centos7", "centos8", "rockylinux8", "rockylinux9", etc.
$os = "rockylinux9"

# =============================================================================
# NETWORK CONFIGURATION
# =============================================================================
# Network type: "nat" or "bridge"
#
# nat: Auto-detect provider network and assign IPs (recommended)
#   - Automatically detects provider default network (usually 192.168.x.0/24)
#   - Uses NAT networking for VM internet access
#   - VMs can communicate with each other and host
#   - Simpler setup, no bridge configuration required
#   - Recommended for development and testing
#
# bridge: Use bridge network with manual IP configuration
#   - Requires manual bridge interface setup on host
#   - VMs get IPs from same subnet as host network
#   - Direct network access, VMs appear as separate devices on network
#   - More complex setup, requires bridge configuration
#   - Recommended for production-like environments
$vm_network = "bridge"

# Starting IP for the 4th octet (VMs will get IPs starting from this number)
# Used in both nat (with auto-detected subnet) and bridge modes
$subnet_split4 = 100

# The following network settings are only used when $vm_network = "bridge"
# For nat, subnet/gateway/netmask are auto-detected from provider

# Network subnet (first 3 octets) - bridge only
$subnet = "192.168.29"

$dns_server = "8.8.8.8"         # DNS server

# Network configuration - bridge only
$netmask = "255.255.240.0"      # Subnet mask
$gateway = "192.168.21.1"       # Default gateway

# Bridge network interface (required when using "bridge")
# Example: On linux, libvirt bridge interface name: br0
$bridge_nic = "br0"

# =============================================================================
# KUBERNETES CONFIGURATION
# =============================================================================
# Container Network Interface (CNI) plugin
# Options: "calico", "flannel", "weave", "cilium", "kube-ovn", etc.
$network_plugin = "calico"

# Cert-Manager Configuration
$cert_manager_enabled = "True"             # Enable cert-manager

# Local Path Provisioner Configuration
$local_path_provisioner_enabled = "False"    # Enable local path provisioner
$local_path_provisioner_claim_root = "/opt/local-path-provisioner/"  # Local path root

# Ansible inventory directory
$inventory = "inventory/sample"

# Shared folders between host and VMs (empty by default)
$shared_folders = {}

# Kubernetes version to install
$kube_version = "1.33.4"

# =============================================================================
# METALLB LOAD BALANCER CONFIGURATION
# =============================================================================
# Enable MetalLB load balancer
$metallb_enabled = "False"

# MetalLB protocol (layer2 or bgp)
$metallb_protocol = "layer2"
