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
$upm_control_plane_vm_cpus = 12      # CPU cores for UPM control plane
$upm_control_plane_vm_memory = 24576 # Memory in MB for UPM control plane (24GB)

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
# 3. Auto-detection of installed providers (parallels > virtualbox > libvirt)
# 
# Supported options: "virtualbox", "libvirt", "parallels"
# 
# Provider recommendations:
# - virtualbox: Best for development and testing (free, cross-platform)
# - libvirt: Good for Linux production environments (KVM-based)
# - parallels: Good for macOS users with Parallels Desktop
# 
# Leave commented for auto-detection, or uncomment and set to force a specific provider
# $provider = "virtualbox"

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
# Network type: "private_network" or "public_network"
# 
# private_network: Auto-detect provider network and assign IPs (recommended)
#   - Automatically detects provider's network settings (subnet, gateway, netmask)
#   - Assigns IPs starting from $subnet_split4 (100) within detected subnet
#   - VirtualBox: Uses VBoxManage to detect NAT network (default: 10.0.2.0/24)
#   - libvirt: Uses default NAT network (192.168.121.0/24)
#   - Parallels: Uses prlsrvctl to detect shared network (default: 10.211.55.0/24)
#   - Falls back to provider default NAT if detection fails
#
# public_network: Use bridge network with manual IP configuration
#   - Requires manual IP, subnet, gateway, and DNS configuration
#   - VMs will be accessible from external network
$vm_network = "public_network"

# Starting IP for the 4th octet (VMs will get IPs starting from this number)
# Used in both private_network (with auto-detected subnet) and public_network modes
$subnet_split4 = 100

# The following network settings are only used when $vm_network = "public_network"
# For private_network, subnet/gateway/netmask are auto-detected from provider

# Network subnet (first 3 octets) - public_network only
$subnet = "192.168.29"

# Network configuration - public_network only
$netmask = "255.255.240.0"      # Subnet mask
$gateway = "192.168.21.1"        # Default gateway
$dns_server = "192.168.21.1"         # DNS server

# Bridge network interface (required when using "public_network")
# Example: On linux, libvirt bridge interface name: br0
# Example: On linux, vitrulbox bridge interface name: eth1
$bridge_nic = "br0"

# =============================================================================
# KUBERNETES CONFIGURATION
# =============================================================================
# Container Network Interface (CNI) plugin
# Options: "calico", "flannel", "weave", "cilium", "kube-ovn", etc.
$network_plugin = "calico"

# Enable multi-networking support
$multi_networking = "False"

# Cert-Manager Configuration
$cert_manager_enabled = "False"             # Enable cert-manager

# Local Path Provisioner Configuration
$local_path_provisioner_enabled = "False"    # Enable local path provisioner
$local_path_provisioner_claim_root = "/opt/local-path-provisioner/"  # Local path root

# Ansible inventory directory
$inventory = "inventory/sample"

# Shared folders between host and VMs (empty by default)
$shared_folders = {}

# Kubernetes version to install
$kube_version = "1.33.2"