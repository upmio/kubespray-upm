#!/usr/bin/env bash
#
# UPM Setup Script for Kubernetes
#
# Description:
#   Automated installation script for UPM (Unified Platform Management) components
#   on existing Kubernetes clusters. Supports modular deployment of storage,
#   monitoring, database, and platform management components.
#
# Requirements:
#   - Kubernetes cluster (v1.29+) with kubectl configured
#   - RHEL/Rocky/AlmaLinux 8/9 (x86_64)
#   - Hardware: 8+ CPU cores, 16GB+ RAM, 100GB+ storage
#   - sudo privileges and internet connectivity
#
# Components:
#   - OpenEBS LVM LocalPV (persistent storage)
#   - Prometheus (monitoring stack)
#   - CloudNative-PG (PostgreSQL operator)
#   - UPM Engine & Platform (management platform)
#   - Nginx (reverse proxy configuration)
#
# Usage: ./upm_setup.sh [OPTIONS] [INSTALL_OPTIONS]
# Example: ./upm_setup.sh --all
#
# License: Apache License 2.0
# Repository: https://github.com/upmio/kubespray-upm
#

set -eE

#######################################
# Global Variables for Cleanup
#######################################
declare -a TEMP_FILES=()
declare -a TEMP_DIRS=()

#######################################

#######################################
# Cleanup and Signal Handling
#######################################
cleanup() {
    local exit_code=$?
    log_info "Cleaning up temporary resources..."
    
    # Clean up temporary files
    if [[ ${#TEMP_FILES[@]} -gt 0 ]]; then
        for temp_file in "${TEMP_FILES[@]}"; do
            [[ -f "$temp_file" ]] && rm -f "$temp_file" 2>/dev/null && log_info "Removed temporary file: $temp_file"
        done
    fi
    
    # Clean up temporary directories
    if [[ ${#TEMP_DIRS[@]} -gt 0 ]]; then
        for temp_dir in "${TEMP_DIRS[@]}"; do
            [[ -d "$temp_dir" ]] && rm -rf "$temp_dir" 2>/dev/null && log_info "Removed temporary directory: $temp_dir"
        done
    fi
    
    exit $exit_code
}

# Set up signal handlers
trap cleanup EXIT INT TERM

#######################################
# Constants and Configuration
#######################################

# Script metadata
readonly SCRIPT_VERSION="1.0"
readonly SCRIPT_NAME="UPM Setup Script"
readonly SCRIPT_AUTHOR="UPM Team"
readonly SCRIPT_LICENSE="Apache License 2.0"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
readonly SCRIPT_DIR
readonly VAGRANT_CONF_FILE="${SCRIPT_DIR}/../vagrant/config.rb"
export KUBECONFIG="${HOME}/.kube/config"

readonly LVM_LOCALPV_NAMESPACE="openebs"
readonly LVM_LOCALPV_CHART_VERSION="${LVM_LOCALPV_CHART_VERSION:-"1.8.0"}"
readonly LVM_LOCALPV_STORAGECLASS_NAME="lvm-localpv"
readonly CNPG_NAMESPACE="cnpg-system"
readonly CNPG_CHART_VERSION="${CNPG_CHART_VERSION:-"0.24.0"}"
readonly UPM_NAMESPACE="upm-system"
readonly UPM_CHART_VERSION="${UPM_CHART_VERSION:-"1.2.4"}"
readonly UPM_PWD="${UPM_PWD:-"Upm@2024!"}"
readonly PROMETHEUS_CHART_VERSION="${PROMETHEUS_CHART_VERSION:-"80.13.3"}"
readonly PROMETHEUS_NAMESPACE="prometheus"

# Global variable for auto-confirm mode (-y parameter)
declare AUTO_CONFIRM=false

# Global array for installation options
declare -a INSTALLATION_OPTIONS=()

# Global variables for Vagrant configuration (extracted from config.rb)
declare G_NUM_INSTANCES=""
declare G_KUBE_MASTER_INSTANCES=""
declare G_UPM_CTL_INSTANCES=""
declare G_INSTANCE_NAME_PREFIX=""

# Log file configuration
LOG_FILE="${SCRIPT_DIR}/upm_setup.log"

#######################################
# Color Definitions (Global)
#######################################
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly RED='\033[0;31m'
readonly CYAN='\033[0;36m'
readonly WHITE='\033[1;37m'
readonly BLUE='\033[0;34m'
readonly MAGENTA='\033[0;35m'
readonly BOLD='\033[1m'
readonly NC='\033[0m'

#######################################
# Logging Functions
#######################################
log_with_level() {
    local level="$1"
    shift
    local message="$*"
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')

    case "$level" in
    "INFO")
        echo "[$timestamp] [INFO] $message" | tee -a "$LOG_FILE"
        ;;
    "WARN")
        echo "[$timestamp] [WARN] $message" | tee -a "$LOG_FILE" >&2
        ;;
    "ERROR")
        echo "[$timestamp] [ERROR] $message" | tee -a "$LOG_FILE" >&2
        ;;
    esac
}

log_info() { log_with_level "INFO" "$@"; }
log_warn() { log_with_level "WARN" "$@"; }
log_error() { log_with_level "ERROR" "$@"; }

# Enhanced structured logging function
# Usage: log_structured "LEVEL" "COMPONENT" "MESSAGE" ["DETAILS"]
log_structured() {
    local level="$1"
    local component="$2"
    local message="$3"
    local details="${4:-}"
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    
    local log_entry="[$timestamp] [$level] [$component] $message"
    [[ -n "$details" ]] && log_entry="$log_entry - $details"
    
    case "$level" in
    "INFO")
        echo "$log_entry" | tee -a "$LOG_FILE"
        ;;
    "WARN"|"ERROR")
        echo "$log_entry" | tee -a "$LOG_FILE" >&2
        ;;
    esac
}

# Function execution timing wrapper
# Usage: time_function "function_name" [args...]
# Examples:
#   time_function "custom_function" "arg1" "arg2"
time_function() {
    local func_name="$1"
    shift
    local start_time end_time duration
    
    # Simple performance monitoring display with timestamps
    local start_timestamp
    start_timestamp=$(date '+%H:%M:%S')
    echo -e "${YELLOW}⏱️  Starting: ${BOLD}$func_name${NC} ${BLUE}[$start_timestamp]${NC}"
    
    log_structured "INFO" "PERF" "Starting function: $func_name"
    local start_time
    start_time=$(date +%s)
    local start_timestamp
    start_timestamp=$(format_timestamp_from_epoch "$start_time" '+%H:%M:%S')
    
    # Execute the function
    "$func_name" "$@"
    local exit_code=$?
    
    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    local end_timestamp
    end_timestamp=$(format_timestamp_from_epoch "$end_time" '+%H:%M:%S')
    
    # Format duration for human readability using cross-platform function
    local formatted_duration
    formatted_duration=$(format_duration_human "$duration")
    
    # Simple completion display with timestamps
    if [[ $exit_code -eq 0 ]]; then
        log_structured "INFO" "PERF" "Function $func_name completed successfully" "Duration: ${duration}s"
        echo -e "${GREEN}✅ Completed: ${BOLD}$func_name${NC} ${BLUE}[$end_timestamp]${NC} ${MAGENTA}(${formatted_duration})${NC}"
    else
        log_structured "ERROR" "PERF" "Function $func_name failed" "Duration: ${duration}s, Exit code: $exit_code"
        echo -e "${RED}❌ Failed: ${BOLD}$func_name${NC} ${BLUE}[$end_timestamp]${NC} ${MAGENTA}(${formatted_duration}, exit: $exit_code)${NC}"
    fi
    echo
    
    return $exit_code
}

#######################################
# Error Handling
#######################################
handle_error() {
    local exit_code=$?
    local line_number=$1
    log_error "Command failed at line $line_number with exit code $exit_code"
    log_error "$2"

    exit $exit_code
}

error_exit() {
    log_error "$1"

    exit 1
}

trap "handle_error \$LINENO \"Unexpected error occurred\"" ERR

#######################################
# Cross-platform Compatibility Functions
#######################################

# Detect operating system
# Usage: detect_os
# Returns: "macos" or "linux"
detect_os() {
    case "$(uname -s)" in
        Darwin*) echo "macos" ;;
        Linux*)  echo "linux" ;;
        *)       echo "unknown" ;;
    esac
}

# Check if current system is Linux
# Usage: check_linux_system
# Returns: 0 if Linux, exits with error if not Linux
check_linux_system() {
    local os_type
    os_type=$(detect_os)
    
    if [[ "$os_type" != "linux" ]]; then
        log_error "This operation is only supported on Linux systems"
        log_error "Current operating system: $(uname -s)"
        log_error "Supported systems: Linux (RHEL, CentOS, Rocky, AlmaLinux)"
        error_exit "Unsupported operating system for this operation"
    fi
    
    log_info "Operating system check passed: Linux system detected"
}

# Cross-platform timestamp formatting from epoch
# Usage: format_timestamp_from_epoch "epoch_seconds" "format"
# Returns: formatted timestamp string
format_timestamp_from_epoch() {
    local epoch="$1"
    local format="$2"
    local os_type
    os_type=$(detect_os)
    
    case "$os_type" in
        "macos")
            # macOS date command uses -r for epoch time
            date -r "$epoch" "$format" 2>/dev/null || echo "N/A"
            ;;
        "linux")
            # Linux date command uses -d @epoch
            date -d @"$epoch" "$format" 2>/dev/null || echo "N/A"
            ;;
        *)
            # Fallback for unknown systems
            echo "N/A"
            ;;
    esac
}

# Cross-platform duration formatting
# Usage: format_duration_human "duration_seconds"
# Returns: human-readable duration string
format_duration_human() {
    local duration="$1"
    local os_type
    os_type=$(detect_os)
    
    if (( duration >= 3600 )); then
        # Hours and minutes for durations >= 1 hour
        case "$os_type" in
            "macos")
                # macOS doesn't support %-H format, use alternative calculation
                local hours=$((duration / 3600))
                local minutes=$(((duration % 3600) / 60))
                echo "${hours}h ${minutes}m"
                ;;
            "linux")
                date -u -d @"$duration" +"%-Hh %-Mm" 2>/dev/null || echo "${duration}s"
                ;;
            *)
                echo "${duration}s"
                ;;
        esac
    elif (( duration >= 60 )); then
        # Minutes and seconds for durations >= 1 minute
        case "$os_type" in
            "macos")
                # macOS doesn't support %-M format, use alternative calculation
                local minutes=$((duration / 60))
                local seconds=$((duration % 60))
                echo "${minutes}m ${seconds}s"
                ;;
            "linux")
                date -u -d @"$duration" +"%-Mm %-Ss" 2>/dev/null || echo "${duration}s"
                ;;
            *)
                echo "${duration}s"
                ;;
        esac
    else
        # Seconds only for durations < 1 minute
        echo "${duration}s"
    fi
}

#######################################
# Utility Functions
#######################################
command_exists() {
    command -v "$1" >/dev/null 2>&1 || {
        log_error "Command not found: $1"
        return 1
    }
}

safe_sudo() {
    if [ "$EUID" -eq 0 ]; then
        "$@"
    else
        sudo "$@"
    fi
}

#######################################
# Unified Input Validation Functions
#######################################

# Unified yes/no confirmation function
# Usage: prompt_yes_no "question" [default_answer] [force_interactive]
# Returns: 0 for yes, 1 for no
# force_interactive: if true, ignores AUTO_CONFIRM mode (for network bridge inputs)
prompt_yes_no() {
    local question="$1"
    local default="${2:-}"
    local force_interactive="${3:-false}"
    local response

    # Auto-confirm mode: automatically return 'yes' unless force_interactive is true
    if [[ "$AUTO_CONFIRM" == "true" && "$force_interactive" != "true" ]]; then
        echo -e "${CYAN}❓ $question${NC} ${GREEN}(auto-confirmed: yes)${NC}"
        return 0
    fi

    while true; do
        if [[ -n "$default" ]]; then
            printf "${CYAN}❓ %s [%s]: ${NC}" "$question" "$default"
        else
            printf "${CYAN}❓ %s (yes/no): ${NC}" "$question"
        fi
        # Improved output buffer flushing
        printf "" >&1
        read -r response

        # Use default if response is empty
        if [[ -z "$response" && -n "$default" ]]; then
            response="$default"
        fi

        case "$response" in
        [Yy][Ee][Ss] | [Yy])
            return 0
            ;;
        [Nn][Oo] | [Nn])
            return 1
            ;;
        *)
            echo -e "${RED}❌ Please enter 'yes' or 'no'${NC}"
            ;;
        esac
    done
}

#######################################
# Cluster Connectivity Validation Function
#######################################
validate_cluster_connectivity() {
    log_info "Validating Kubernetes cluster connectivity..."
    if ! kubectl cluster-info >/dev/null 2>&1; then
        log_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig and cluster status."
        exit 1
    fi

    # Display cluster information
    log_info "Kubernetes cluster information:"
    echo -e "${GREEN}=== Cluster Overview ===${NC}"
    echo -e "   ${GREEN}•${NC} Cluster Address: ${CYAN}$(kubectl cluster-info | grep 'Kubernetes control plane' | awk '{print $NF}' 2>/dev/null || echo 'N/A')${NC}"
    echo -e "   ${GREEN}•${NC} Cluster Version: ${CYAN}$(kubectl version -o json 2>/dev/null | jq -r '.serverVersion.gitVersion' 2>/dev/null || echo 'N/A')${NC}"
    echo
    
    echo -e "${GREEN}=== Node Status ===${NC}"
    kubectl get nodes --no-headers 2>/dev/null | while read -r node status role age version; do
        echo -e "   ${GREEN}•${NC} Node: ${WHITE}$node${NC} | Status: ${CYAN}$status${NC} | Version: ${CYAN}$version${NC}"
    done || echo -e "   ${RED}•${NC} Unable to retrieve node information"
    echo
    
    echo -e "${GREEN}=== Namespaces ===${NC}"
    echo -e "   ${GREEN}•${NC} Total: ${WHITE}$(kubectl get namespaces --no-headers 2>/dev/null | wc -l | tr -d ' ')${NC} namespaces"
    echo

    if ! prompt_yes_no "Do you want to proceed with the installation on this cluster?"; then
        log_info "Installation cancelled by user."
        exit 0
    fi
}

#######################################
# Variable Validation Functions
#######################################
validate_required_variables() {
    log_info "Validating required variables..."

    # Initialize log file with proper permissions
    touch "$LOG_FILE" 2>/dev/null || {
        echo "Warning: Cannot create log file $LOG_FILE, logging to stdout only"
        LOG_FILE="/dev/stdout"
    }
    chmod 666 "$LOG_FILE" 2>/dev/null || true

    # Validate kubectl command availability
    log_info "Checking kubectl command availability..."
    if ! command -v kubectl >/dev/null 2>&1; then
        log_error "kubectl command not found. Please install kubectl and ensure it's in your PATH."
        exit 1
    fi
    log_info "kubectl command found: $(which kubectl)"

    log_info "Variable validation passed"
}

#######################################
# Install Helm Function
#######################################
install_helm() {
    # Check if helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Installing Helm..."
        curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
        chmod 700 get_helm.sh
        ./get_helm.sh
        rm -f get_helm.sh
    else
        log_info "Helm is already installed"
    fi
}

#######################################
# Extract Vagrant configuration variables and set as globals
#######################################
extract_vagrant_config_variables() {
    if [[ ! -f "$VAGRANT_CONF_FILE" ]]; then
        error_exit "Config file not found: $VAGRANT_CONF_FILE"
    fi

    log_info "Extracting Vagrant configuration variables from: $VAGRANT_CONF_FILE"

    # Extract configuration values from config.rb and set as global variables
    G_NUM_INSTANCES=$(grep "^\$num_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_KUBE_MASTER_INSTANCES=$(grep "^\$kube_master_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_UPM_CTL_INSTANCES=$(grep "^\$upm_ctl_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_INSTANCE_NAME_PREFIX=$(grep "^\$instance_name_prefix\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')

    # Check if required variables are set
    if [[ -z "$G_NUM_INSTANCES" || -z "$G_KUBE_MASTER_INSTANCES" || -z "$G_UPM_CTL_INSTANCES" || -z "$G_INSTANCE_NAME_PREFIX" ]]; then
        error_exit "Required Vagrant configuration variables are missing or empty"
    fi

    log_info "Vagrant configuration variables extracted successfully"
    return 0
}

#######################################
# Install and configure OpenEBS LVM LocalPV
#######################################
install_lvm_localpv() {
    log_info "Installing OpenEBS LVM LocalPV..."

    # Ensure Helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Helm not found, installing..."
        install_helm
    fi

    echo -e "${YELLOW}🔧 Installing OpenEBS LVM LocalPV...${NC}"
    local lvm_localpv_chart_repo="https://openebs.github.io/lvm-localpv"
    local lvm_localpv_repo_name="openebs-lvmlocalpv"
    local lvm_localpv_release_name="lvm-localpv"
    local lvm_localpv_chart_name="$lvm_localpv_repo_name/lvm-localpv"

    # Get volume group name from Vagrant configuration
    local vg_name="local_vg_dev" # default fallback
    if [[ -f "$VAGRANT_CONF_FILE" ]]; then
        local extracted_vg
        extracted_vg=$(grep "\$kube_node_instances_volume_group" "$VAGRANT_CONF_FILE" 2>/dev/null | sed -E 's/.*= *"([^"]*)".*/\1/' | head -n1)
        if [[ -n "$extracted_vg" ]]; then
            vg_name="$extracted_vg"
            log_info "Found volume group name '$vg_name' in $VAGRANT_CONF_FILE"
        else
            log_info "Could not find volume group name in $VAGRANT_CONF_FILE, using default: $vg_name"
        fi
    fi

    # Interactive confirmation for OpenEBS installation
    echo -e "\n${YELLOW}📦 OpenEBS LVM LocalPV Installation${NC}\n"
    echo -e "${WHITE}This will install OpenEBS LVM LocalPV with the following components:${NC}"
    echo -e "   ${GREEN}•${NC} OpenEBS LVM LocalPV Helm chart"
    echo -e "   ${GREEN}•${NC} LVM LocalPV StorageClass"
    echo -e "   ${GREEN}•${NC} Node labels for OpenEBS scheduling"
    echo -e "   ${GREEN}•${NC} Helm repository configuration\n"

    echo -e "${WHITE}Installation details:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$LVM_LOCALPV_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart: ${CYAN}$lvm_localpv_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart version: ${CYAN}$LVM_LOCALPV_CHART_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} StorageClass: ${CYAN}$LVM_LOCALPV_STORAGECLASS_NAME${NC}"
    echo -e "   ${GREEN}•${NC} VolumeGroup: ${CYAN}$vg_name${NC}"
    echo -e "   ${GREEN}•${NC} Installation timeout: ${CYAN}15 minutes${NC}\n"

    echo -e "${YELLOW}⚠️  Note: This installation will:${NC}"
    echo -e "   ${YELLOW}•${NC} Add node labels to control plane and worker nodes"
    echo -e "   ${YELLOW}•${NC} Install Helm if not already present\n"

    if ! prompt_yes_no "Do you want to proceed with OpenEBS LVM LocalPV installation?"; then
        echo -e "${YELLOW}⏸️  OpenEBS LVM LocalPV installation skipped.${NC}\n"
        log_info "OpenEBS LVM LocalPV installation skipped by user"
        return 0
    fi

    echo -e "${GREEN}✅ Proceeding with OpenEBS LVM LocalPV installation...${NC}\n"

    # Label openebs control plane nodes (openebs.io/control-plane=enable)
    log_info "Labeling openebs control plane nodes..."
    # Use global variables extracted from config
    extract_vagrant_config_variables

    local ctl_start_index=$((G_KUBE_MASTER_INSTANCES + 1))
    local ctl_end_index=$((G_KUBE_MASTER_INSTANCES + G_UPM_CTL_INSTANCES))

    local nodes
    nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$ctl_start_index" ]] && [[ "$node_num" -le "$ctl_end_index" ]]; then
                log_info "Labeling control plane node: $node (node number: $node_num)"
                kubectl label node "$node" "openebs.io/control-plane=enable" --overwrite || {
                    error_exit "Failed to label OpenEBS LVM LocalPV control plane node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    # Label data nodes (openebs.io/node=enable)
    log_info "Labeling data nodes..."
    local worker_start_index=$ctl_start_index
    local worker_end_index=$((G_NUM_INSTANCES))

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$worker_start_index" ]] && [[ "$node_num" -le "$worker_end_index" ]]; then
                log_info "Labeling worker node: $node"
                kubectl label node "$node" "openebs.io/node=enable" --overwrite || {
                    error_exit "Failed to label worker node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    # Add OpenEBS Helm repository
    log_info "Adding OpenEBS Helm repository..."
    helm repo add "$lvm_localpv_repo_name" "$lvm_localpv_chart_repo" || {
        error_exit "Failed to add OpenEBS Helm repository: $lvm_localpv_repo_name, $lvm_localpv_chart_repo"
    }
    helm repo update "$lvm_localpv_repo_name"

    local values_file="/tmp/lvm_localpv_values.yaml"
    cat <<EOF >"$values_file"
lvmPlugin:
  allowedTopologies: "kubernetes.io/hostname,openebs.io/node,"
lvmController:
  nodeSelector:
    "openebs.io/control-plane": "enable"
lvmNode:
  nodeSelector:
    "openebs.io/node": "enable"
analytics:
  enabled: false
EOF

    # Install OpenEBS LVM LocalPV
    log_info "Installing OpenEBS LVM LocalPV with Helm..."
    helm upgrade --install "$lvm_localpv_release_name" "$lvm_localpv_chart_name" \
        --version "$LVM_LOCALPV_CHART_VERSION" \
        --namespace "$LVM_LOCALPV_NAMESPACE" \
        --create-namespace \
        --values "$values_file" \
        --wait --timeout=15m || {
        error_exit "Failed to install OpenEBS LVM LocalPV"
    }

    # Clean up values file
    rm -f "$values_file"

    # Wait for pods to be ready
    log_info "Waiting for OpenEBS pods to be ready..."
    kubectl wait --for=condition=ready pod -l release="$lvm_localpv_release_name" -n "$LVM_LOCALPV_NAMESPACE" --timeout=900s || {
        error_exit "OpenEBS pods failed to become ready"
    }
    log_info "OpenEBS LVM LocalPV installed successfully"

    # Create StorageClass
    log_info "Creating OpenEBS LVM LocalPV StorageClass..."
    if kubectl apply -f - <<EOF
apiVersion: storage.k8s.io/v1
kind: StorageClass
metadata:
  name: $LVM_LOCALPV_STORAGECLASS_NAME
allowVolumeExpansion: true
volumeBindingMode: WaitForFirstConsumer
parameters:
  shared: "yes"
  storage: "lvm"
  volgroup: "$vg_name"
  fsType: ext4
provisioner: local.csi.openebs.io
allowedTopologies:
  - matchLabelExpressions:
      - key: openebs.io/node
        values: ["enable"]
EOF
    then
        log_info "OpenEBS LVM LocalPV StorageClass created successfully"
    else
        error_exit "Failed to create OpenEBS LVM LocalPV StorageClass"
    fi

    # Display installation status
    echo -e "\n${GREEN}🎉 OpenEBS LVM LocalPV Installation Completed!${NC}\n"
    echo -e "${WHITE}📦 Components:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$LVM_LOCALPV_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} StorageClass: ${CYAN}$LVM_LOCALPV_STORAGECLASS_NAME${NC}"
    echo -e "   ${GREEN}•${NC} Volume Group: ${CYAN}$vg_name${NC}\n"

    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check pods: ${CYAN}kubectl get pods -n $LVM_LOCALPV_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check StorageClass: ${CYAN}kubectl get storageclass $LVM_LOCALPV_STORAGECLASS_NAME${NC}"
    echo -e "   ${GREEN}•${NC} Check node labels: ${CYAN}kubectl get nodes --show-labels${NC}"
    echo -e "${GREEN}✅ OpenEBS LVM LocalPV installed successfully${NC}\n"



    return 0
}

#######################################
# Install Prometheus Function
#######################################
install_prometheus() {
    log_info "Starting Prometheus installation..."

    # Ensure Helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Helm not found, installing..."
        install_helm
    fi

    # Prometheus configuration
    local prometheus_repo_name="prometheus-community"
    local prometheus_chart_repo="https://prometheus-community.github.io/helm-charts"
    local prometheus_release_name="prometheus"
    local prometheus_chart_name="$prometheus_repo_name/kube-prometheus-stack"

    # Interactive confirmation for Prometheus installation
    echo -e "\n${YELLOW}📊 Prometheus Installation${NC}\n"
    echo -e "${WHITE}This will install Prometheus with the following components:${NC}"
    echo -e "   ${GREEN}•${NC} Prometheus Operator"
    echo -e "   ${GREEN}•${NC} Prometheus Server"
    echo -e "   ${GREEN}•${NC} Alertmanager"
    echo -e "   ${GREEN}•${NC} Grafana"
    echo -e "   ${GREEN}•${NC} kube-state-metrics"
    echo -e "   ${GREEN}•${NC} Node labels for Prometheus scheduling\n"

    echo -e "${WHITE}Installation details:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$PROMETHEUS_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart: ${CYAN}$prometheus_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart version: ${CYAN}$PROMETHEUS_CHART_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} Installation timeout: ${CYAN}15 minutes${NC}\n"

    echo -e "${YELLOW}⚠️  Note: This installation will:${NC}"
    echo -e "   ${YELLOW}•${NC} Add node labels to worker nodes"
    echo -e "   ${YELLOW}•${NC} Install Helm if not already present"
    echo -e "   ${YELLOW}•${NC} Create persistent storage for Prometheus data\n"

    if ! prompt_yes_no "Do you want to proceed with Prometheus installation?"; then
        echo -e "${YELLOW}⏸️  Prometheus installation skipped.${NC}\n"
        log_info "Prometheus installation skipped by user"
        return 0
    fi

    echo -e "${GREEN}✅ Proceeding with Prometheus installation...${NC}\n"

    # Add Prometheus Helm repository with retry mechanism
    log_info "Adding Prometheus Helm repository..."
    local max_retries=3
    local retry_count=0
    
    while [ $retry_count -lt $max_retries ]; do
        if helm repo add "$prometheus_repo_name" "$prometheus_chart_repo"; then
            log_info "Prometheus Helm repository added successfully"
            break
        else
            retry_count=$((retry_count + 1))
            log_info "Failed to add Prometheus Helm repository (attempt $retry_count/$max_retries)"
            if [ $retry_count -lt $max_retries ]; then
                log_info "Retrying in 10 seconds..."
                sleep 10
            else
                error_exit "Failed to add Prometheus Helm repository after $max_retries attempts"
            fi
        fi
    done
    
    # Update repository with retry mechanism
    retry_count=0
    while [ $retry_count -lt $max_retries ]; do
        if helm repo update "$prometheus_repo_name"; then
            log_info "Prometheus Helm repository updated successfully"
            break
        else
            retry_count=$((retry_count + 1))
            log_info "Failed to update Prometheus Helm repository (attempt $retry_count/$max_retries)"
            if [ $retry_count -lt $max_retries ]; then
                log_info "Retrying in 10 seconds..."
                sleep 10
            else
                error_exit "Failed to update Prometheus Helm repository after $max_retries attempts"
            fi
        fi
    done

    log_info "Labeling Prometheus worker nodes..."
    # Use global variables extracted from config
    extract_vagrant_config_variables

    local ctl_start_index=$((G_KUBE_MASTER_INSTANCES + 1))
    local ctl_end_index=$((G_KUBE_MASTER_INSTANCES + G_UPM_CTL_INSTANCES))

    local nodes
    nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$ctl_start_index" ]] && [[ "$node_num" -le "$ctl_end_index" ]]; then
                log_info "Labeling Prometheus control plane node: $node"
                kubectl label node "$node" "prometheus.node=true" --overwrite || {
                    error_exit "Failed to label Prometheus control plane node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    # Create values file
    local values_file="/tmp/prometheus_values.yaml"
    cat >"$values_file" <<EOF
prometheusOperator:
  admissionWebhooks:
    patch:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: prometheus.node
                    operator: Exists
    deployment:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
              - matchExpressions:
                  - key: prometheus.node
                    operator: Exists
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: prometheus.node
                operator: Exists

prometheus:
  prometheusSpec:
    podMonitorSelectorNilUsesHelmValues: false
    serviceMonitorSelectorNilUsesHelmValues: false
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - key: prometheus.node
                  operator: Exists
    storageSpec:
      volumeClaimTemplate:
        spec:
          storageClassName: "${LVM_LOCALPV_STORAGECLASS_NAME}"
          accessModes:
            - ReadWriteOnce
          resources:
            requests:
              storage: 30Gi

alertmanager:
  alertmanagerSpec:
    affinity:
      nodeAffinity:
        requiredDuringSchedulingIgnoredDuringExecution:
          nodeSelectorTerms:
            - matchExpressions:
                - key: prometheus.node
                  operator: Exists

grafana:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: prometheus.node
                operator: Exists

kube-state-metrics:
  affinity:
    nodeAffinity:
      requiredDuringSchedulingIgnoredDuringExecution:
        nodeSelectorTerms:
          - matchExpressions:
              - key: prometheus.node
                operator: Exists
EOF

    log_info "Installing Prometheus via Helm..."
    # Install with retry mechanism for network issues
    retry_count=0
    while [ $retry_count -lt $max_retries ]; do
        if helm upgrade --install "$prometheus_release_name" "$prometheus_chart_name" \
            --namespace "$PROMETHEUS_NAMESPACE" \
            --create-namespace \
            --version "$PROMETHEUS_CHART_VERSION" \
            --values "$values_file" \
            --wait --timeout=15m; then
            log_info "Prometheus installed successfully"
            break
        else
            retry_count=$((retry_count + 1))
            log_info "Failed to install Prometheus (attempt $retry_count/$max_retries)"
            if [ $retry_count -lt $max_retries ]; then
                log_info "Retrying in 30 seconds..."
                sleep 30
                # Try to update repo again before retry
                helm repo update "$prometheus_repo_name" || true
            else
                error_exit "Failed to install Prometheus after $max_retries attempts. Please check network connectivity and try again."
            fi
        fi
    done

    # Clean up values file
    rm -f "$values_file"

    # Wait for Prometheus to be ready
    log_info "Waiting for Prometheus to be ready..."
    # Wait for Prometheus operator to be ready
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/name=kube-prometheus-stack-prometheus-operator" -n "$PROMETHEUS_NAMESPACE" --timeout=900s || {
        error_exit "Prometheus operator failed to become ready"
    }
    
    # Wait for Grafana to be ready
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/name=grafana" -n "$PROMETHEUS_NAMESPACE" --timeout=900s || {
        error_exit "Grafana failed to become ready"
    }
    
    # Wait for AlertManager to be ready
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/name=alertmanager" -n "$PROMETHEUS_NAMESPACE" --timeout=900s || {
        error_exit "AlertManager failed to become ready"
    }
    
    # Wait for Kube State Metrics to be ready
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/name=kube-state-metrics" -n "$PROMETHEUS_NAMESPACE" --timeout=900s || {
        error_exit "Kube State Metrics failed to become ready"
    }
    
    # Wait for Prometheus Node Exporter to be ready
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/name=prometheus-node-exporter" -n "$PROMETHEUS_NAMESPACE" --timeout=900s || {
        error_exit "Prometheus Node Exporter failed to become ready"
    }

    # Display installation status
    echo -e "\n${GREEN}🎉 Prometheus Installation Completed!${NC}\n"
    echo -e "${WHITE}📊 Components:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$PROMETHEUS_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Chart: ${CYAN}$prometheus_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Chart Version: ${CYAN}$PROMETHEUS_CHART_VERSION${NC}\n"

    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check pods: ${CYAN}kubectl get pods -n $PROMETHEUS_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check Prometheus: ${CYAN}kubectl get prometheus -n $PROMETHEUS_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check services: ${CYAN}kubectl get svc -n $PROMETHEUS_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check Helm release: ${CYAN}helm list -n $PROMETHEUS_NAMESPACE${NC}"
    echo -e "${GREEN}✅ Prometheus installed successfully${NC}\n"

    # Get service information for access
    local prometheus_svc
    local grafana_svc
    prometheus_svc=$(kubectl get svc -n "$PROMETHEUS_NAMESPACE" -l "app.kubernetes.io/name=prometheus" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    grafana_svc=$(kubectl get svc -n "$PROMETHEUS_NAMESPACE" -l "app.kubernetes.io/name=grafana" -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    # Display Prometheus access information
    echo -e "${WHITE}🌐 Prometheus Access Information:${NC}"
    if [[ -n "$prometheus_svc" ]]; then
        echo -e "   ${GREEN}•${NC} Prometheus UI: ${CYAN}kubectl port-forward -n $PROMETHEUS_NAMESPACE svc/$prometheus_svc 9090:9090${NC}"
        echo -e "   ${GREEN}•${NC} Then access: ${CYAN}http://localhost:9090${NC}"
    fi
    if [[ -n "$grafana_svc" ]]; then
        echo -e "   ${GREEN}•${NC} Grafana UI: ${CYAN}kubectl port-forward -n $PROMETHEUS_NAMESPACE svc/$grafana_svc 3000:80${NC}"
        echo -e "   ${GREEN}•${NC} Then access: ${CYAN}http://localhost:3000${NC}"
        echo -e "   ${GREEN}•${NC} Grafana default credentials: ${CYAN}admin/prom-operator${NC}"
    fi
    echo


    log_info "Prometheus installation completed successfully!"
    return 0
}

#######################################
# Install CloudNative-PG
#######################################
install_cnpg() {
    log_info "Starting CloudNative-PG installation..."

    # Ensure Helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Helm not found, installing..."
        install_helm
    fi

    # Configuration variables
    local cnpg_chart_repo="https://cloudnative-pg.github.io/charts"
    local cnpg_repo_name="cnpg"
    local cnpg_release_name="cloudnative-pg"
    local cnpg_chart_name="$cnpg_repo_name/cloudnative-pg"

    # Interactive confirmation for CloudNative-PG installation
    echo -e "\n${YELLOW}📦 CloudNative-PG Installation${NC}\n"
    echo -e "${WHITE}This will install CloudNative-PG with the following components:${NC}"
    echo -e "   ${GREEN}•${NC} CloudNative-PG Helm chart"
    echo -e "   ${GREEN}•${NC} Node labels for CloudNative-PG scheduling"
    echo -e "   ${GREEN}•${NC} Helm repository configuration\n"

    echo -e "${WHITE}Installation details:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$CNPG_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart: ${CYAN}$cnpg_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart version: ${CYAN}$CNPG_CHART_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} Installation timeout: ${CYAN}5 minutes${NC}\n"

    echo -e "${YELLOW}⚠️  Note: This installation will:${NC}"
    echo -e "   ${YELLOW}•${NC} Add node labels to control plane"
    echo -e "   ${YELLOW}•${NC} Install Helm if not already present\n"

    if ! prompt_yes_no "Do you want to proceed with CloudNative-PG installation?"; then
        echo -e "${YELLOW}⏸️  CloudNative-PG installation skipped.${NC}\n"
        log_info "CloudNative-PG installation skipped by user"
        return 0
    fi

    echo -e "${GREEN}✅ Proceeding with CloudNative-PG installation...${NC}\n"

    # Add CloudNative-PG Helm repository
    log_info "Adding CloudNative-PG Helm repository..."
    helm repo add "$cnpg_repo_name" "$cnpg_chart_repo" || {
        error_exit "Failed to add CloudNative-PG Helm repository"
    }
    helm repo update "$cnpg_repo_name"

    log_info "Labeling CloudNative-PG control plane nodes..."
    # Use global variables extracted from config
    extract_vagrant_config_variables

    local ctl_start_index=$((G_KUBE_MASTER_INSTANCES + 1))
    local ctl_end_index=$((G_KUBE_MASTER_INSTANCES + G_UPM_CTL_INSTANCES))

    local nodes
    nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$ctl_start_index" ]] && [[ "$node_num" -le "$ctl_end_index" ]]; then
                log_info "Labeling CloudNative-PG control plane node: $node"
                kubectl label node "$node" "cnpg.io/control-plane=enable" --overwrite || {
                    error_exit "Failed to label CloudNative-PG control plane node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    # Create values file
    local values_file="/tmp/cnpg_values.yaml"
    cat >"$values_file" <<EOF
# Operator configuration.
config:
  data:
    ENABLE_INSTANCE_MANAGER_INPLACE_UPDATES: "true"
    INHERITED_ANNOTATIONS: "categories"
    INHERITED_LABELS: "upm.api/service-group.name, upm.api/service-group.type, upm.api/service.type, upm.io/owner, upm.api/pod.main-container"
# -- Affinity for the operator to be installed.
affinity: 
  nodeAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      nodeSelectorTerms:
      - matchExpressions:
        - key: cnpg.io/control-plane
          operator: In
          values:
          - enable
EOF

    log_info "Installing CloudNative-PG operator via Helm..."
    helm upgrade --install "$cnpg_release_name" "$cnpg_chart_name" \
        --namespace "$CNPG_NAMESPACE" \
        --create-namespace \
        --version "$CNPG_CHART_VERSION" \
        --values "$values_file" \
        --wait --timeout=5m || {
        error_exit "Failed to upgrade CloudNative-PG"
    }

    # Clean up values file
    rm -f "$values_file"

    # Wait for operator to be ready
    log_info "Waiting for CloudNative-PG operator to be ready..."
    kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance="$cnpg_release_name" -n "$CNPG_NAMESPACE" --timeout=300s || {
        error_exit "CloudNative-PG operator failed to become ready"
    }

    # Display installation status
    echo -e "\n${GREEN}🎉 CloudNative-PG Installation Completed!${NC}\n"
    echo -e "${WHITE}📦 Components:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$CNPG_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Chart: ${CYAN}$cnpg_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Chart Version: ${CYAN}$CNPG_CHART_VERSION${NC}\n"

    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check pods: ${CYAN}kubectl get pods -n $CNPG_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check operator logs: ${CYAN}kubectl logs -n $CNPG_NAMESPACE deployment/cnpg-controller-manager${NC}"
    echo -e "   ${GREEN}•${NC} Check CRDs: ${CYAN}kubectl get crd | grep cnpg${NC}"
    echo -e "   ${GREEN}•${NC} Check Helm release: ${CYAN}helm list -n $CNPG_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check deployment config: ${CYAN}kubectl get deployment cnpg-controller-manager -n $CNPG_NAMESPACE -o yaml${NC}"
    echo -e "${GREEN}✅ CloudNative-PG installed successfully${NC}\n"

    return 0
}

#######################################
# Install UPM Engine
#######################################
install_upm_engine() {
    log_info "Starting UPM Engine installation..."

    # Ensure Helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Helm not found, installing..."
        install_helm
    fi

    # Configuration variables
    local upm_chart_repo="https://upmio.github.io/helm-charts"
    local upm_repo_name="upm-charts"
    local upm_engine_release_name="upm-engine"
    local upm_engine_chart_name="$upm_repo_name/upm-engine"

    # Interactive confirmation for UPM Engine installation
    echo -e "\n${YELLOW}📦 UPM Engine Installation${NC}\n"
    echo -e "${WHITE}This will install UPM Engine with the following components:${NC}"
    echo -e "   ${GREEN}•${NC} UPM Engine Helm chart"
    echo -e "   ${GREEN}•${NC} Node labels for UPM Engine scheduling"
    echo -e "   ${GREEN}•${NC} Helm repository configuration\n"

    echo -e "${WHITE}Installation details:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart: ${CYAN}$upm_engine_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart version: ${CYAN}$UPM_CHART_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} Installation timeout: ${CYAN}5 minutes${NC}\n"

    echo -e "${YELLOW}⚠️  Note: This installation will:${NC}"
    echo -e "   ${YELLOW}•${NC} Add node labels to control plane"
    echo -e "   ${YELLOW}•${NC} Install Helm if not already present\n"

    if ! prompt_yes_no "Do you want to proceed with UPM Engine installation?"; then
        echo -e "${YELLOW}⏸️  UPM Engine installation skipped.${NC}\n"
        log_info "UPM Engine installation skipped by user"
        return 0
    fi

    echo -e "${GREEN}✅ Proceeding with UPM Engine installation...${NC}\n"

    # Add UPM Engine Helm repository
    log_info "Adding UPM Engine Helm repository..."
    helm repo add "$upm_repo_name" "$upm_chart_repo" || {
        error_exit "Failed to add UPM Engine Helm repository"
    }
    helm repo update "$upm_repo_name"

    log_info "Labeling UPM Engine control plane nodes..."
    # Use global variables extracted from config
    extract_vagrant_config_variables

    local ctl_start_index=$((G_KUBE_MASTER_INSTANCES + 1))
    local ctl_end_index=$((G_KUBE_MASTER_INSTANCES + G_UPM_CTL_INSTANCES))

    local nodes
    nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$ctl_start_index" ]] && [[ "$node_num" -le "$ctl_end_index" ]]; then
                log_info "Labeling UPM Engine control plane node: $node"
                kubectl label node "$node" "upm.engine.node=enable" --overwrite || {
                    error_exit "Failed to label UPM Engine control plane node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    log_info "Installing UPM Engine via Helm..."
    helm upgrade --install "$upm_engine_release_name" "$upm_engine_chart_name" \
        --namespace "$UPM_NAMESPACE" \
        --create-namespace \
        --version "$UPM_CHART_VERSION" \
        --wait --timeout=5m || {
        error_exit "Failed to upgrade UPM Engine"
    }

    # Wait for operator to be ready
    log_info "Waiting for UPM Engine to be ready..."
    kubectl wait --for=condition=ready pod -l "app.kubernetes.io/instance=$upm_engine_release_name" -n "$UPM_NAMESPACE" --timeout=300s || {
        error_exit "UPM Engine failed to become ready"
    }

    # Display installation status
    echo -e "\n${GREEN}🎉 UPM Engine Installation Completed!${NC}\n"
    echo -e "${WHITE}📦 Components:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Chart: ${CYAN}$upm_engine_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Chart Version: ${CYAN}$UPM_CHART_VERSION${NC}\n"

    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check pods: ${CYAN}kubectl get pods -n $UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check operator logs: ${CYAN}kubectl logs -n $UPM_NAMESPACE deployment/upm-engine-controller-manager${NC}"
    echo -e "   ${GREEN}•${NC} Check CRDs: ${CYAN}kubectl get crd | grep upm${NC}"
    echo -e "   ${GREEN}•${NC} Check Helm release: ${CYAN}helm list -n $UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check deployment config: ${CYAN}kubectl get deployment upm-engine-controller-manager -n $UPM_NAMESPACE -o yaml${NC}"
    echo -e "${GREEN}✅ UPM Engine installed successfully${NC}\n"

    return 0
}

#######################################
# Install UPM Platform
#######################################
install_upm_platform() {
    log_info "Starting UPM Platform installation..."

    # Ensure Helm is installed
    if ! command -v helm >/dev/null 2>&1; then
        log_info "Helm not found, installing..."
        install_helm
    fi

    # Check LVM LocalPV prerequisites
    log_info "Checking LVM LocalPV prerequisites..."
    
    # Check if LVM LocalPV Helm release exists
    if ! helm list -n openebs | grep -q "lvm-localpv"; then
        log_error "LVM LocalPV Helm release not found. LVM LocalPV is required for UPM Platform."
        echo -e "${RED}❌ LVM LocalPV is not installed.${NC}"
        echo -e "${WHITE}To install LVM LocalPV, run:${NC}"
        echo -e "   ${CYAN}./upm_setup.sh --lvmlocalpv${NC}"
        echo -e "${WHITE}Or use the interactive menu option.${NC}\n"
        error_exit "UPM Platform installation cancelled due to missing LVM LocalPV"
    fi
    log_info "✅ LVM LocalPV Helm release found"
    
    # Check if the required StorageClass exists
    if ! kubectl get storageclass "$LVM_LOCALPV_STORAGECLASS_NAME" >/dev/null 2>&1; then
        log_error "Required StorageClass '$LVM_LOCALPV_STORAGECLASS_NAME' not found."
        echo -e "${RED}❌ LVM LocalPV StorageClass is missing.${NC}"
        echo -e "${WHITE}Available StorageClasses:${NC}"
        kubectl get storageclass || true
        echo -e "${WHITE}\nTo fix this issue:${NC}"
        echo -e "   ${CYAN}1. Check LVM LocalPV Helm release: helm list -n openebs${NC}"
        echo -e "   ${CYAN}2. Verify StorageClass configuration${NC}"
        echo -e "   ${CYAN}3. Re-run LVM LocalPV installation if needed${NC}\n"
        error_exit "UPM Platform installation cancelled due to missing StorageClass"
    fi
    log_info "✅ Required StorageClass '$LVM_LOCALPV_STORAGECLASS_NAME' found"
    
    # Configuration variables
    local upm_chart_repo="https://upmio.github.io/helm-charts"
    local upm_repo_name="upm-charts"
    local upm_platform_release_name="upm-platform"
    local upm_platform_chart_name="$upm_repo_name/upm-platform"

    # Interactive confirmation for UPM Platform installation
    echo -e "\n${YELLOW}📦 UPM Platform Installation${NC}\n"
    echo -e "${WHITE}This will install UPM Platform with the following components:${NC}"
    echo -e "   ${GREEN}•${NC} UPM Platform Helm chart"
    echo -e "   ${GREEN}•${NC} Node labels for UPM Platform scheduling"
    echo -e "   ${GREEN}•${NC} Helm repository configuration\n"

    echo -e "${WHITE}Installation details:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart: ${CYAN}$upm_platform_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Helm chart version: ${CYAN}$UPM_CHART_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} Installation timeout: ${CYAN}15 minutes${NC}\n"

    echo -e "${YELLOW}⚠️  Note: This installation will:${NC}"
    echo -e "   ${YELLOW}•${NC} Add node labels to worker nodes"
    echo -e "   ${YELLOW}•${NC} Install Helm if not already present\n"

    if ! prompt_yes_no "Do you want to proceed with UPM Platform installation?"; then
        echo -e "${YELLOW}⏸️  UPM Platform installation skipped.${NC}\n"
        log_info "UPM Platform installation skipped by user"
        return 0
    fi

    echo -e "${GREEN}✅ Proceeding with UPM Platform installation...${NC}\n"

    # Add UPM Platform Helm repository
    log_info "Adding UPM Platform Helm repository..."
    helm repo add "$upm_repo_name" "$upm_chart_repo" || {
        error_exit "Failed to add UPM Platform Helm repository"
    }
    helm repo update "$upm_repo_name"

    log_info "Labeling UPM Platform worker nodes..."
    # Use global variables extracted from config
    extract_vagrant_config_variables

    local ctl_start_index=$((G_KUBE_MASTER_INSTANCES + 1))
    local ctl_end_index=$((G_KUBE_MASTER_INSTANCES + G_UPM_CTL_INSTANCES))

    local nodes
    nodes=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name")

    while IFS= read -r node; do
        # Extract node number from node name (assuming format: prefix-number)
        if [[ "$node" =~ ^${G_INSTANCE_NAME_PREFIX}-([0-9]+)$ ]]; then
            local node_num="${BASH_REMATCH[1]}"
            if [[ "$node_num" -ge "$ctl_start_index" ]] && [[ "$node_num" -le "$ctl_end_index" ]]; then
                log_info "Labeling UPM Platform control plane node: $node"
                kubectl label node "$node" "upm.platform.node=enable" --overwrite || {
                    error_exit "Failed to label UPM Platform control plane node: $node"
                }
                kubectl label node "$node" 'nacos.io/control-plane=enable' --overwrite || {
                    error_exit "Failed to label UPM Platform nacos node: $node"
                }
                kubectl label node "$node" 'mysql.standalone.node=enable' --overwrite || {
                    error_exit "Failed to label UPM Platform database node: $node"
                }
                kubectl label node "$node" 'redis.standalone.node=enable' --overwrite || {
                    error_exit "Failed to label UPM Platform cache node: $node"
                }
            fi
        fi
    done <<<"$nodes"

    # Create values file
    local values_file="/tmp/upm_platform_values.yaml"
    cat >"$values_file" <<EOF
nginx:
  service:
    type: NodePort
    ports:
      http: 80
    nodePorts:
      http: 32010

apiserver:
  upm:
    mysqlUser:
      name: "upm"
      password: "${UPM_PWD}"
    serviceGroup:
      elasticsearch:
        enabled: true
      kafka:
        enabled: true
      mysql:
        enabled: true
      postgresql:
        enabled: true
      redis:
        enabled: true
      redis-cluster:
        enabled: true
      zookeeper:
        enabled: true
      cnpg:
        enabled: true
      innodb-cluster:
        enabled: true
  mysql:
    auth:
      rootPassword: "${UPM_PWD}"
    primary:
      persistence:
        storageClass: "${LVM_LOCALPV_STORAGECLASS_NAME}"
      resourcesPreset: "large"
    resources: {}
  redis:
    master:
      persistence:
        enabled: true
        storageClass: "${LVM_LOCALPV_STORAGECLASS_NAME}"
      resources: {}
    auth:
      password: "${UPM_PWD}"
  nacos:
    service:
      type: NodePort
      loadBalancerIP: ""
    persistence:
      storageClass: "${LVM_LOCALPV_STORAGECLASS_NAME}"
    mysql:
      external:
        mysqlMasterHost: "${upm_platform_release_name}-mysql"
        mysqlMasterPassword: "${UPM_PWD}"
EOF

    log_info "Installing UPM Platform via Helm..."
    helm upgrade --install "$upm_platform_release_name" "$upm_platform_chart_name" \
        --namespace "$UPM_NAMESPACE" \
        --create-namespace \
        --version "$UPM_CHART_VERSION" \
        --values "$values_file" \
        --wait --timeout=15m || {
        error_exit "Failed to upgrade UPM Platform"
    }

    # Clean up values file
    rm -f "$values_file"

    # Wait for platform to be ready
    log_info "Waiting for UPM Platform to be ready..."
    if ! kubectl wait --for=condition=ready pod -l "app.kubernetes.io/instance=$upm_platform_release_name,!job-name" -n "$UPM_NAMESPACE" --timeout=900s; then
        log_error "UPM Platform pods failed to become ready. Checking pod status..."
        kubectl get pods -n "$UPM_NAMESPACE" -l "app.kubernetes.io/instance=$upm_platform_release_name,!job-name" || true
        kubectl describe pods -n "$UPM_NAMESPACE" -l "app.kubernetes.io/instance=$upm_platform_release_name,!job-name" || true
        kubectl get events -n "$UPM_NAMESPACE" --sort-by='.lastTimestamp' | tail -20 || true
        error_exit "UPM Platform failed to become ready"
    fi

    # Display installation status
    echo -e "\n${GREEN}🎉 UPM Platform Installation Completed!${NC}\n"
    echo -e "${WHITE}📦 Components:${NC}"
    echo -e "   ${GREEN}•${NC} Namespace: ${CYAN}$UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Chart: ${CYAN}$upm_platform_chart_name${NC}"
    echo -e "   ${GREEN}•${NC} Chart Version: ${CYAN}$UPM_CHART_VERSION${NC}\n"

    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check pods: ${CYAN}kubectl get pods -n $UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check platform logs: ${CYAN}kubectl logs -n $UPM_NAMESPACE deployment/upm-platform-controller-manager${NC}"
    echo -e "   ${GREEN}•${NC} Check CRDs: ${CYAN}kubectl get crd | grep upm${NC}"
    echo -e "   ${GREEN}•${NC} Check Helm release: ${CYAN}helm list -n $UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Check deployment config: ${CYAN}kubectl get deployment upm-platform-controller-manager -n $UPM_NAMESPACE -o yaml${NC}"
    echo -e "${GREEN}✅ UPM Platform installed successfully${NC}\n"

    # Get worker node IP for login URL (prioritize nodes with upm.platform.node label)
    local worker_node_ip
    worker_node_ip=$(kubectl get nodes -l upm.platform.node=enable -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)
    if [[ -n "$worker_node_ip" ]]; then
        echo "Worker node IP: $worker_node_ip"
    else
        error_exit "Failed to get worker node IP"
    fi
    
    # Display UPM Platform login information
    echo -e "${WHITE}🌐 UPM Platform Access Information:${NC}"
    if [[ -n "$worker_node_ip" ]]; then
        echo -e "   ${GREEN}•${NC} Login URL: ${CYAN}http://$worker_node_ip:32010/upm-ui/#/login${NC}"
    else
        echo -e "   ${GREEN}•${NC} Login URL: ${CYAN}http://<node-ip>:32010/upm-ui/#/login${NC}"
        echo -e "   ${GREEN}•${NC} Note: Replace <node-ip> with any worker node's IP address${NC}"
    fi
    echo -e "   ${GREEN}•${NC} Username: ${CYAN}super_root${NC}"
    echo -e "   ${GREEN}•${NC} Default Password: ${CYAN}Upm@2024!${NC}\n"

    # Create ClusterRoleBinding for upm-system default service account
    log_info "Creating ClusterRoleBinding for upm-system default service account..."
    kubectl apply -f - <<EOF || error_exit "Failed to create ClusterRoleBinding for upm-system default service account"
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: upm-system-admin-default-account
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: default
  namespace: upm-system
EOF

    echo -e "${GREEN}✅ ClusterRoleBinding created successfully for upm-system default service account${NC}\n"
    log_info "ClusterRoleBinding 'upm-system-admin-default-account' created successfully"

    return 0
}

#######################################
# Configure Nginx for UPM Platform
#######################################
configure_nginx_for_upm() {
    log_info "Starting Nginx configuration for UPM Platform..."
    
    # Check if kubectl is available
    if ! command_exists kubectl; then
        error_exit "kubectl not found. Please ensure Kubernetes cluster is set up."
    fi
    
    # Check if UPM namespace exists
    if ! kubectl get namespace "$UPM_NAMESPACE" >/dev/null 2>&1; then
        error_exit "UPM namespace '$UPM_NAMESPACE' not found. Please install UPM Platform first."
    fi
    
    echo -e "\n${YELLOW}🌐 Nginx Configuration for UPM Platform${NC}\n"
    echo -e "${WHITE}This will configure:${NC}"
    echo -e "   ${GREEN}•${NC} Change upm-platform-gateway service to NodePort (port 31404)"
    echo -e "   ${GREEN}•${NC} Change upm-platform-ui service to NodePort (port 31405)"
    echo -e "   ${GREEN}•${NC} Install and configure Nginx with proxy forwarding"
    echo -e "   ${GREEN}•${NC} Set up access via port 80\n"
    
    if ! prompt_yes_no "Do you want to proceed with Nginx configuration?"; then
        echo -e "${YELLOW}⏸️  Nginx configuration skipped.${NC}\n"
        log_info "Nginx configuration skipped by user"
        return 0
    fi
    
    echo -e "${GREEN}✅ Proceeding with Nginx configuration...${NC}\n"
    
    # Step 1: Configure upm-platform-gateway service to NodePort
    log_info "Configuring upm-platform-gateway service to NodePort..."
    if kubectl get service upm-platform-gateway -n "$UPM_NAMESPACE" >/dev/null 2>&1; then
        # Check if service is already NodePort with correct port
        local current_type
        current_type=$(kubectl get service upm-platform-gateway -n "$UPM_NAMESPACE" -o jsonpath='{.spec.type}')
        local current_nodeport
        current_nodeport=$(kubectl get service upm-platform-gateway -n "$UPM_NAMESPACE" -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo "")
        
        if [[ "$current_type" == "NodePort" && "$current_nodeport" == "31404" ]]; then
            log_info "✅ upm-platform-gateway service already configured as NodePort with port 31404"
        else
            log_info "Patching upm-platform-gateway service to NodePort with port 31404..."
            kubectl patch service upm-platform-gateway -n "$UPM_NAMESPACE" -p '{
                "spec": {
                    "type": "NodePort",
                    "ports": [{
                        "name": "http",
                        "port": 8080,
                        "targetPort": 8080,
                        "nodePort": 31404,
                        "protocol": "TCP"
                    }]
                }
            }' || error_exit "Failed to patch upm-platform-gateway service"
            log_info "✅ upm-platform-gateway service configured successfully"
        fi
    else
        log_warn "upm-platform-gateway service not found in namespace $UPM_NAMESPACE"
    fi
    
    # Step 2: Configure upm-platform-ui service to NodePort
    log_info "Configuring upm-platform-ui service to NodePort..."
    if kubectl get service upm-platform-ui -n "$UPM_NAMESPACE" >/dev/null 2>&1; then
        # Check if service is already NodePort with correct port
        local current_type
        current_type=$(kubectl get service upm-platform-ui -n "$UPM_NAMESPACE" -o jsonpath='{.spec.type}')
        local current_nodeport
        current_nodeport=$(kubectl get service upm-platform-ui -n "$UPM_NAMESPACE" -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null || echo "")
        
        if [[ "$current_type" == "NodePort" && "$current_nodeport" == "31405" ]]; then
            log_info "✅ upm-platform-ui service already configured as NodePort with port 31405"
        else
            log_info "Patching upm-platform-ui service to NodePort with port 31405..."
            kubectl patch service upm-platform-ui -n "$UPM_NAMESPACE" -p '{
                "spec": {
                    "type": "NodePort",
                    "ports": [{
                        "name": "http",
                        "port": 80,
                        "targetPort": 80,
                        "nodePort": 31405,
                        "protocol": "TCP"
                    }]
                }
            }' || error_exit "Failed to patch upm-platform-ui service"
            log_info "✅ upm-platform-ui service configured successfully"
        fi
    else
        log_warn "upm-platform-ui service not found in namespace $UPM_NAMESPACE"
    fi
    
    # Step 3: Install Nginx if not already installed
    log_info "Checking Nginx installation..."
    if ! command_exists nginx; then
        log_info "Installing Nginx..."
        safe_sudo dnf install -y nginx || error_exit "Failed to install Nginx"
        log_info "✅ Nginx installed successfully"
    else
        log_info "✅ Nginx is already installed"
    fi
    
    # Step 4: Get worker node IP for Nginx configuration
    log_info "Getting worker node IP for Nginx configuration..."
    local worker_node_ip
    worker_node_ip=$(kubectl get nodes -l upm.platform.node=enable -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null)
    if [[ -z "$worker_node_ip" ]]; then
        # Fallback to any worker node
        worker_node_ip=$(kubectl get nodes --no-headers -o custom-columns=":metadata.name" | head -1 | xargs kubectl get node -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')
    fi
    
    if [[ -z "$worker_node_ip" ]]; then
        error_exit "Failed to get worker node IP for Nginx configuration"
    fi
    
    log_info "Using worker node IP: $worker_node_ip"
    
    # Step 5: Create Nginx configuration
    log_info "Creating Nginx configuration..."
    local nginx_conf="/etc/nginx/nginx.conf"
    local nginx_conf_backup
    nginx_conf_backup="/etc/nginx/nginx.conf.backup.$(date +%Y%m%d_%H%M%S)"
    
    # Backup existing configuration
    if [[ -f "$nginx_conf" ]]; then
        safe_sudo cp "$nginx_conf" "$nginx_conf_backup"
        log_info "✅ Nginx configuration backed up to $nginx_conf_backup"
    fi
    
    # Create new Nginx configuration
    safe_sudo tee "$nginx_conf" > /dev/null <<EOF
# For more information on configuration, see:
#   * Official English Documentation: http://nginx.org/en/docs/

user nginx;
worker_processes 2;
error_log /var/log/nginx/error.log;
pid /run/nginx.pid;

# Load dynamic modules. See /usr/share/doc/nginx/README.dynamic.
include /usr/share/nginx/modules/*.conf;

events {
    worker_connections 1024;
}

http {
    log_format  main  '\$remote_addr - \$remote_user [\$time_local] "\$request" '
                      '\$status \$body_bytes_sent "\$http_referer" '
                      '"\$http_user_agent" "\$http_x_forwarded_for"';
    include             /etc/nginx/mime.types;
    default_type        application/octet-stream;

    proxy_buffer_size 512k;
    proxy_buffers 8 1024k;
    proxy_busy_buffers_size 1024k;
    keepalive_timeout 65;

    upstream api {
        server $worker_node_ip:31404;
    }

    upstream ui {
        server $worker_node_ip:31405;
    }

    upstream license {
        server localhost:8080;
    }

    server {
        keepalive_requests 120;
        listen       80;
        listen       [::]:80;

        location  /upm-ui/ {
            proxy_pass  http://ui/upm-ui/;
        }

        location  /api/ {
            proxy_pass  http://api/;
        }

        location  /license/ {
            proxy_pass  http://license/upm/license/;
        }

        location  /license-ui/ {
            root /tmp/license-ui;
            index index.html;
            try_files \$uri \$uri/ /index.html;
        }
    }
}
EOF
    
    log_info "✅ Nginx configuration created successfully"
    
    # Step 6: Test Nginx configuration
    log_info "Testing Nginx configuration..."
    if safe_sudo nginx -t; then
        log_info "✅ Nginx configuration test passed"
    else
        error_exit "Nginx configuration test failed"
    fi
    
    # Step 7: Enable and start Nginx service
    log_info "Enabling and starting Nginx service..."
    
    # Check if Nginx is already running
    if systemctl is-active --quiet nginx; then
        log_info "Nginx is already running, reloading configuration..."
        safe_sudo systemctl reload nginx || error_exit "Failed to reload Nginx"
        log_info "✅ Nginx configuration reloaded successfully"
    else
        # Enable Nginx service
        safe_sudo systemctl enable nginx || error_exit "Failed to enable Nginx service"
        log_info "✅ Nginx service enabled"
        
        # Start Nginx service
        safe_sudo systemctl start nginx || error_exit "Failed to start Nginx service"
        log_info "✅ Nginx service started successfully"
    fi
    
    # Step 8: Configure firewall if firewalld is running
    if systemctl is-active --quiet firewalld; then
        log_info "Configuring firewall for HTTP traffic..."
        if safe_sudo firewall-cmd --list-services | grep -q http; then
            log_info "✅ HTTP service already allowed in firewall"
        else
            safe_sudo firewall-cmd --permanent --add-service=http || log_warn "Failed to add HTTP service to firewall"
            safe_sudo firewall-cmd --reload || log_warn "Failed to reload firewall"
            log_info "✅ Firewall configured for HTTP traffic"
        fi
    else
        log_info "Firewalld is not running, skipping firewall configuration"
    fi
    
    # Step 9: Verify Nginx status
    log_info "Verifying Nginx status..."
    if systemctl is-active --quiet nginx; then
        log_info "✅ Nginx is running successfully"
    else
        error_exit "Nginx is not running"
    fi
    
    # Step 10: Display access information
    echo -e "\n${GREEN}🎉 Nginx Configuration Completed!${NC}\n"
    echo -e "${WHITE}📦 Configuration Summary:${NC}"
    echo -e "   ${GREEN}•${NC} upm-platform-gateway: ${CYAN}NodePort 31404${NC}"
    echo -e "   ${GREEN}•${NC} upm-platform-ui: ${CYAN}NodePort 31405${NC}"
    echo -e "   ${GREEN}•${NC} Nginx proxy: ${CYAN}Port 80${NC}"
    echo -e "   ${GREEN}•${NC} Worker node IP: ${CYAN}$worker_node_ip${NC}\n"
    
    echo -e "${WHITE}🌐 Access Information:${NC}"
    local host_ip
    host_ip=$(hostname -I | awk '{print $1}')
    if [[ -n "$host_ip" ]]; then
        echo -e "   ${GREEN}•${NC} UPM Platform URL: ${CYAN}http://$host_ip/upm-ui/${NC}"
        echo -e "   ${GREEN}•${NC} API Endpoint: ${CYAN}http://$host_ip/api/${NC}"
    else
        echo -e "   ${GREEN}•${NC} UPM Platform URL: ${CYAN}http://<host-ip>/upm-ui/${NC}"
        echo -e "   ${GREEN}•${NC} API Endpoint: ${CYAN}http://<host-ip>/api/${NC}"
        echo -e "   ${GREEN}•${NC} Note: Replace <host-ip> with this server's IP address${NC}"
    fi
    echo -e "   ${GREEN}•${NC} Username: ${CYAN}super_root${NC}"
    echo -e "   ${GREEN}•${NC} Default Password: ${CYAN}Upm@2024!${NC}\n"
    
    echo -e "${WHITE}🔍 Verification Commands:${NC}"
    echo -e "   ${GREEN}•${NC} Check Nginx status: ${CYAN}systemctl status nginx${NC}"
    echo -e "   ${GREEN}•${NC} Check Nginx config: ${CYAN}nginx -t${NC}"
    echo -e "   ${GREEN}•${NC} Check services: ${CYAN}kubectl get svc -n $UPM_NAMESPACE${NC}"
    echo -e "   ${GREEN}•${NC} Test connectivity: ${CYAN}curl -I http://localhost/upm-ui/${NC}"
    echo -e "${GREEN}✅ Nginx configuration completed successfully${NC}\n"
    
    # Step 11: Config

    return 0
}

#######################################
# Version Functions
#######################################

# Display version information
show_version() {
    cat <<EOF
${SCRIPT_NAME} v${SCRIPT_VERSION}

Build Information:
  Version:     ${SCRIPT_VERSION}
  Author:      ${SCRIPT_AUTHOR}
  License:     ${SCRIPT_LICENSE}

System Information:
  Script Path: ${BASH_SOURCE[0]}
  Working Dir: ${SCRIPT_DIR}
  Shell:       ${BASH_VERSION}
  Platform:    $(uname -s) $(uname -r) $(uname -m)
  User:        $(whoami)
  Date:        $(date '+%Y-%m-%d %H:%M:%S %Z')

Component Versions:
  LVM LocalPV: ${LVM_LOCALPV_CHART_VERSION}
  CNPG:        ${CNPG_CHART_VERSION}
  UPM:         ${UPM_CHART_VERSION}
  Prometheus:  ${PROMETHEUS_CHART_VERSION}
EOF
}

#######################################
# Help Function
#######################################
show_help() {
    echo -e "${GREEN}UPM Setup Script v${SCRIPT_VERSION}${NC}"
    echo
    echo -e "${WHITE}USAGE:${NC}"
    echo "    $0 [OPTIONS] [INSTALL_OPTIONS]"
    echo
    echo -e "${WHITE}OPTIONS:${NC}"
    echo "    -h, --help              Show this help message and exit"
    echo "    -v, --version           Show version information and exit"
    echo "    -y                      Automatic yes to prompts (non-interactive mode)"
    echo
    echo -e "${WHITE}INSTALL OPTIONS:${NC}"
    echo "    --lvmlocalpv            Install OpenEBS LVM LocalPV for persistent storage"
    echo "    --prometheus            Install Prometheus monitoring stack"
    echo "    --cnpg                  Install CloudNative-PG PostgreSQL operator"
    echo "    --upm-engine            Install UPM Engine (requires LVM LocalPV)"
    echo "    --upm-platform          Install UPM Platform (requires LVM LocalPV)"
    echo "    --config_nginx          Configure Nginx for UPM Platform access"
    echo "    --all                   Install all components"
    echo
    echo -e "${WHITE}PREREQUISITES:${NC}"
    echo "    • Kubernetes cluster (v1.28+) with kubectl configured"
    echo "    • Root/sudo privileges for system operations"
    echo "    • Internet connectivity for downloads"
    echo "    • Minimum: 8+ CPU cores, 16GB+ RAM, 100GB+ storage"
    echo
    echo -e "${WHITE}DESCRIPTION:${NC}"
    echo "    Automates UPM (Unified Platform Management) component installation"
    echo "    on existing Kubernetes clusters with modular installation options."
    echo
    echo -e "${WHITE}COMPONENTS:${NC}"
    echo -e "    ${YELLOW}LVM LocalPV:${NC}     Persistent storage using LVM (namespace: openebs)"
    echo -e "    ${YELLOW}Prometheus:${NC}      Monitoring stack (ports: 30090, 30091)"
    echo -e "    ${YELLOW}CNPG:${NC}            PostgreSQL operator (namespace: cnpg-system)"
    echo -e "    ${YELLOW}UPM Engine:${NC}      Core management engine (namespace: upm-system)"
    echo -e "    ${YELLOW}UPM Platform:${NC}    Web interface (port: 32010, user: super_root/Upm@2024!)"
    echo -e "    ${YELLOW}Nginx:${NC}           Reverse proxy configuration"
    echo
    echo -e "${WHITE}EXAMPLES:${NC}"
    echo "    $0 --all                                    # Install all components"
    echo "    $0 --lvmlocalpv --prometheus                # Storage + monitoring"
    echo "    $0 -y --lvmlocalpv --upm-platform          # Platform (non-interactive)"
    echo "    $0 --config_nginx                           # Configure Nginx only"
    echo
}

#######################################
# Parse Command Line Arguments
#######################################
parse_arguments() {
    # Use a global array to store installation options
    INSTALLATION_OPTIONS=()
    
    # Process arguments in a single pass
    while [[ $# -gt 0 ]]; do
        case $1 in
        -h|--help)
            show_help
            exit 0
            ;;
        -v|--version)
            show_version
            exit 0
            ;;
        -y)
            AUTO_CONFIRM=true
            shift
            ;;
        --*)
            # Collect installation options
            INSTALLATION_OPTIONS+=("$1")
            shift
            ;;
        *)
            log_error "Unknown argument: $1"
            show_help
            exit 1
            ;;
        esac
    done
    
    # Show help if no installation options provided
    if [[ ${#INSTALLATION_OPTIONS[@]} -eq 0 ]]; then
        log_info "No installation options specified. Showing help..."
        show_help
        exit 0
    fi
}

#######################################
# Main Function
#######################################
main() {
    # Display script version and basic info at startup
    echo -e "${CYAN}🚀 ${SCRIPT_NAME} v${SCRIPT_VERSION}${NC}"
    
    # Variable validation
    validate_required_variables
    
    # Parse command line arguments (sets global INSTALLATION_OPTIONS array)
    parse_arguments "$@"

    # Validate cluster connectivity
    validate_cluster_connectivity
    
    # Validate that only one installation option is provided
    if [[ ${#INSTALLATION_OPTIONS[@]} -ne 1 ]]; then
        log_error "Exactly one installation option must be specified"
        show_help
        exit 1
    fi
    
    local selected_option="${INSTALLATION_OPTIONS[0]}"
    
    # Execute the selected installation function with performance monitoring
    case "$selected_option" in
        "--lvmlocalpv")
            log_info "Executing: install_lvm_localpv"
            time_function install_lvm_localpv
            ;;
        "--prometheus")
            log_info "Executing: install_prometheus"
            time_function install_prometheus
            ;;
        "--cnpg")
            log_info "Executing: install_cnpg"
            time_function install_cnpg
            ;;
        "--upm-engine")
            log_info "Executing: install_upm_engine"
            time_function install_upm_engine
            ;;
        "--upm-platform")
            log_info "Executing: install_upm_platform"
            time_function install_upm_platform
            ;;
        "--config_nginx")
            log_info "Checking system compatibility for Nginx configuration..."
            check_linux_system
            log_info "Executing: configure_nginx_for_upm"
            time_function configure_nginx_for_upm
            ;;
        "--all")
            log_info "Checking system compatibility for complete installation..."
            log_info "Executing: complete installation sequence"
            time_function install_lvm_localpv
            time_function install_prometheus
            time_function install_cnpg
            time_function install_upm_engine
            time_function install_upm_platform
            ;;
        *)
            log_error "Unknown installation option: $selected_option"
            show_help
            exit 1
            ;;
    esac
    
    exit 0
}

#######################################
# Script Execution Entry Point
#######################################
if [[ "${BASH_SOURCE[0]}" == "${0}" ]] || [[ -z "${BASH_SOURCE[*]}" ]]; then
    main "$@"
fi
