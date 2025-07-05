#!/bin/bash
#
# Kubespray Libvirt Environment Setup Script v3.0
#
# Description:
#   Automated Kubespray environment setup with libvirt virtualization for RHEL-based
#   distributions. Configures complete Kubernetes development environment with
#   networking, virtualization, and interactive deployment capabilities.
#
# Key Features:
#   System validation (OS, resources, NTP sync)
#   Libvirt/KVM setup with security configuration
#   Multi-network support (Bridge, NAT, Host-only)
#   Python environment (pyenv 3.11.10 + venv)
#   Vagrant + libvirt plugin installation
#   Kubespray project setup with proxy support
#   VM IP preview and interactive deployment
#   Comprehensive logging and error handling
#
# Environment Variables:
#   BRIDGE_INTERFACE   - Network interface for bridge (optional)
#   HTTP_PROXY         - HTTP proxy URL
#   HTTPS_PROXY        - HTTPS proxy URL (defaults to HTTP_PROXY)
#   NO_PROXY           - No-proxy addresses
#
# Fixed Paths:
#   KUBESPRAY_DIR      - Fixed to $(pwd)/kubespray (not configurable)
#
# Network Setup:
#   Bridge:    br0 (uses BRIDGE_INTERFACE if set)
#   NAT:       192.168.121.0/24 (DHCP: Enabled)
#   Host-only: 192.168.200.0/24 (DHCP: Disabled)
#
# Requirements:
#   - RHEL-based Linux (x86_64)
#   - sudo privileges
#   - Internet connectivity
#   - 200GB+ disk space
#   - 32GB+ memory
#   - 16+ CPU cores
#
# Software Installed:
#   - Python: 3.11.10 (pyenv)
#   - Vagrant: 2.4.7
#   - Libvirt/QEMU: Latest
#   - Kubespray: Latest
#

set -eE

#######################################
# Constants and Configuration
#######################################

# Script metadata
readonly SCRIPT_VERSION="3.0"
readonly KUBESPRAY_DIR="$(pwd)/kubespray"

# Default values
readonly DEFAULT_PYTHON_VERSION="3.12.11"
readonly KUBESPRAY_REPO_URL="https://github.com/upmio/kubespray.git"

# Network configuration constants
readonly DEFAULT_BRIDGE_NAME="br0"
readonly PRIVATE_NETWORK_TYPE="private"
readonly PUBLIC_NETWORK_TYPE="public"
readonly DEFAULT_VM_INSTANCES="5"
readonly DEFAULT_INSTANCE_PREFIX="k8s"

# Package lists
readonly SYSTEM_PACKAGES="curl git rsync yum-utils"
readonly LIBVIRT_PACKAGES="qemu-kvm libvirt libvirt-python3 libvirt-client virt-install virt-viewer virt-manager"
readonly PLUGIN_DEPENDENCIES="pkgconf-pkg-config libvirt-libs libvirt-devel libxml2-devel libxslt-devel ruby-devel gcc gcc-c++ make krb5-devel zlib-devel bridge-utils"
readonly PYENV_DEPENDENCIES="gcc make patch zlib-devel bzip2 bzip2-devel readline-devel sqlite sqlite-devel openssl-devel tk-devel libffi-devel xz-devel"

# Global configuration variables (initialized from environment)
declare PYTHON_VERSION="${PYTHON_VERSION:-$DEFAULT_PYTHON_VERSION}"
declare HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-""}}"
declare HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-$HTTP_PROXY}}"
declare PIP_PROXY="${PIP_PROXY:-$HTTP_PROXY}"
declare GIT_PROXY="${GIT_PROXY:-$HTTP_PROXY}"
declare BRIDGE_INTERFACE="${BRIDGE_INTERFACE:-""}"

# Global variable for prompt function results
declare PROMPT_RESULT=""

# Log file configuration
LOG_FILE="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)/libvirt_kubespray_setup.log"

#######################################
# Color Definitions (Global)
#######################################
readonly GREEN='\033[0;32m'
readonly YELLOW='\033[1;33m'
readonly RED='\033[0;31m'
readonly CYAN='\033[0;36m'
readonly WHITE='\033[1;37m'
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

# Backward compatibility
log() { log_info "$@"; }

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
# Utility Functions
#######################################
command_exists() {
    command -v "$1" >/dev/null 2>&1 || { log_error "Command not found: $1"; return 1; }
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
# Usage: prompt_yes_no "question" [default_answer]
# Returns: 0 for yes, 1 for no
prompt_yes_no() {
    local question="$1"
    local default="${2:-}"
    local response
    
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
            [Yy][Ee][Ss]|[Yy])
                return 0
                ;;
            [Nn][Oo]|[Nn])
                return 1
                ;;
            *)
                echo -e "${RED}❌ Please enter 'yes' or 'no'${NC}"
                ;;
        esac
    done
}

# Unified IP address validation function
# Usage: validate_ip_address "ip_address"
# Returns: 0 if valid, 1 if invalid
validate_ip_address() {
    local ip="$1"
    local ip_regex='^([0-9]{1,3}\.){3}[0-9]{1,3}$'
    
    if [[ ! $ip =~ $ip_regex ]]; then
        return 1
    fi
    
    # Check each octet
    local IFS='.'
    IFS='.' read -ra octets <<< "$ip"
    for octet in "${octets[@]}"; do
        if [[ $octet -lt 0 || $octet -gt 255 ]]; then
            return 1
        fi
    done
    
    return 0
}

# Unified IP input function with validation
# Usage: prompt_ip_input "prompt_message" [validation_function]
# Returns: validated IP address via global variable PROMPT_RESULT
prompt_ip_input() {
    local prompt="$1"
    local validation_func="${2:-validate_ip_address}"
    local ip_input
    
    # Parameter validation
    if [[ $# -lt 1 ]]; then
        log_error "Usage: prompt_ip_input 'prompt_message' [validation_function]"
        return 1
    fi
    
    # Check required constants for VM range validation
    if [[ "$validation_func" == "validate_vm_ip_range" ]]; then
        if [[ -z "${DEFAULT_VM_INSTANCES:-}" ]]; then
            log_error "Required constant DEFAULT_VM_INSTANCES not defined for VM IP range validation"
            return 1
        fi
    fi
        
    while true; do
        printf "${CYAN}🌐 %s: ${NC}" "$prompt"
        # Improved output buffer flushing
        printf "" >&1
        read -r ip_input
        
        if [[ -z "$ip_input" ]]; then
            echo -e "${RED}❌ IP address cannot be empty${NC}"
            continue
        fi
        
        if $validation_func "$ip_input"; then
            PROMPT_RESULT="$ip_input"
            return 0
        else
            if [[ "$validation_func" == "validate_vm_ip_range" ]]; then
                # Special error message for VM IP range validation
                local fourth_octet
                fourth_octet=$(echo "$ip_input" | cut -d'.' -f4)
                if validate_ip_address "$ip_input"; then
                    echo -e "${RED}❌ Fourth octet ($fourth_octet) must be between 1 and $((254 - DEFAULT_VM_INSTANCES)) for VM allocation${NC}"
                    echo -e "${YELLOW}💡 Please enter an IP with fourth octet between 1 and $((254 - DEFAULT_VM_INSTANCES)) (e.g., 192.168.1.10)${NC}"
                else
                    echo -e "${RED}❌ Invalid IP address format: $ip_input${NC}"
                    echo -e "${YELLOW}💡 Please enter a valid IP address (e.g., 192.168.1.10)${NC}"
                fi
            else
                echo -e "${RED}❌ Invalid IP address format: $ip_input${NC}"
                echo -e "${YELLOW}💡 Please enter a valid IP address (e.g., 192.168.1.100)${NC}"
            fi
        fi
    done
}

# Unified text input function with validation
# Usage: prompt_text_input "prompt_message" [validation_function] [allow_empty]
# Returns: validated text via global variable PROMPT_RESULT
prompt_text_input() {
    local prompt="$1"
    local validation_func="${2:-}"
    local allow_empty="${3:-false}"
    local text_input
    
    while true; do
        printf "${CYAN}📝 %s: ${NC}" "$prompt"
        # Improved output buffer flushing
        printf "" >&1
        read -r text_input
        
        if [[ -z "$text_input" ]]; then
            if [[ "$allow_empty" == "true" ]]; then
                PROMPT_RESULT="$text_input"
                return 0
            else
                echo -e "${RED}❌ Input cannot be empty${NC}"
                continue
            fi
        fi
        
        if [[ -n "$validation_func" ]]; then
            if $validation_func "$text_input"; then
                PROMPT_RESULT="$text_input"
                return 0
            else
                echo -e "${RED}❌ Invalid input: $text_input${NC}"
                continue
            fi
        else
            PROMPT_RESULT="$text_input"
            return 0
        fi
    done
}

# Unified confirmation with retry function
# Usage: prompt_confirmation_with_retry "prompt" "expected_value" [max_attempts]
# Returns: 0 if confirmed, 1 if failed
prompt_confirmation_with_retry() {
    local prompt="$1"
    local expected_value="$2"
    local max_attempts="${3:-3}"
    local user_input
    local attempt=1
    
    while [[ $attempt -le $max_attempts ]]; do
        printf "${CYAN}🔑 %s: ${NC}" "$prompt"
        # Improved output buffer flushing
        printf "" >&1
        read -r user_input
        
        if [[ "$user_input" == "$expected_value" ]]; then
            echo -e "${GREEN}✅ Confirmation successful${NC}"
            return 0
        else
            echo -e "${RED}❌ Entered value '$user_input' does not match expected value '$expected_value'${NC}"
            if [[ $attempt -eq $max_attempts ]]; then
                echo -e "${RED}🚫 Maximum attempts reached. Operation cancelled for safety.${NC}"
                return 1
            else
                echo -e "${YELLOW}⚠️  Attempt $attempt of $max_attempts. Please try again.${NC}"
                ((attempt++))
            fi
        fi
    done
    
    return 1
}

# IP address range validation for VM allocation
# Usage: validate_vm_ip_range "ip_address"
# Returns: 0 if valid for VM allocation, 1 if invalid
validate_vm_ip_range() {
    local ip="$1"
    
    if ! validate_ip_address "$ip"; then
        return 1
    fi
    
    # Extract the fourth octet
    local fourth_octet
    IFS='.' read -ra octets <<< "$ip"
    fourth_octet="${octets[3]}"
    
    # Check if the fourth octet is in valid range for VM allocation
    if [[ $fourth_octet -lt 1 || $fourth_octet -gt $((254 - DEFAULT_VM_INSTANCES)) ]]; then
        # Don't echo here as it interferes with prompt_ip_input return value
        return 1
    fi
    
    return 0
}

install_packages() {
    local packages="$1"
    local max_retries="${2:-3}"
    local retry_delay="${3:-5}"
    
    log_info "Installing packages: $packages (max retries: $max_retries)"
    
    local package_array
    read -ra package_array <<< "$packages"
    
    local failed_packages=()
    local installed_count=0
    local skipped_count=0
    local retry_count
    
    for package in "${package_array[@]}"; do
        if ! rpm -q "$package" &>/dev/null; then
            retry_count=0
            while [[ $retry_count -lt $max_retries ]]; do
                log_info "Installing $package... (attempt $((retry_count + 1))/$max_retries)"
                
                if safe_sudo dnf install -y "$package"; then
                    log_info "$package installed successfully"
                    installed_count=$((installed_count + 1))
                    break
                else
                    retry_count=$((retry_count + 1))
                    if [[ $retry_count -lt $max_retries ]]; then
                        log_warn "Failed to install $package, retrying in ${retry_delay}s... ($retry_count/$max_retries)"
                        sleep "$retry_delay"
                        
                        # Try to clean dnf cache before retry
                        log_info "Cleaning dnf cache before retry..."
                        safe_sudo dnf clean all &>/dev/null || true
                    else
                        log_error "Failed to install $package after $max_retries attempts"
                        failed_packages+=("$package")
                    fi
                fi
            done
        else
            log_info "Package $package is already installed"
            skipped_count=$((skipped_count + 1))
        fi
    done
    
    if [[ ${#failed_packages[@]} -gt 0 ]]; then
        log_error "Failed to install packages after retries: ${failed_packages[*]}"
        log_error "Troubleshooting suggestions:"
        log_error "  1. Check network connectivity: ping 8.8.8.8"
        log_error "  2. Verify repository configuration: dnf repolist"
        log_error "  3. Try manual installation: dnf install ${failed_packages[*]}"
        log_error "  4. Check disk space: df -h"
        error_exit "Package installation failed"
    fi
    
    log_info "Package installation summary: $installed_count installed, $skipped_count already present"
}

manage_service() {
    local service_name="$1"
    local action="$2"
    local timeout="${3:-30}"
    local retry_count=0
    local max_retries=3
    
    # Verify service exists
    if ! systemctl list-unit-files "$service_name.service" &>/dev/null; then
        log_error "Service $service_name does not exist"
        return 1
    fi
    
    case "$action" in
        "enable")
            if ! systemctl is-enabled "$service_name" &>/dev/null; then
                log_info "Enabling $service_name service..."
                if safe_sudo systemctl enable "$service_name"; then
                    log_info "Service $service_name enabled successfully"
                else
                    log_error "Failed to enable service $service_name"
                    return 1
                fi
            else
                log_info "Service $service_name is already enabled"
            fi
            ;;
        "start")
            if ! systemctl is-active "$service_name" &>/dev/null; then
                log_info "Starting $service_name service..."
                if safe_sudo systemctl start "$service_name"; then
                    # Wait for service to start with timeout
                    local wait_time=0
                    while [[ $wait_time -lt $timeout ]]; do
                        if systemctl is-active "$service_name" &>/dev/null; then
                            log_info "Service $service_name started successfully"
                            return 0
                        fi
                        sleep 2
                        wait_time=$((wait_time + 2))
                    done
                    log_error "Service $service_name failed to start within ${timeout}s timeout"
                    # Show service status for debugging
                    log_error "Service status: $(systemctl status "$service_name" --no-pager -l)"
                    return 1
                else
                    log_error "Failed to start service $service_name"
                    return 1
                fi
            else
                log_info "Service $service_name is already running"
            fi
            ;;
        "stop")
            if systemctl is-active "$service_name" &>/dev/null; then
                log_info "Stopping $service_name service..."
                if safe_sudo systemctl stop "$service_name"; then
                    # Wait for service to stop with timeout
                    local wait_time=0
                    while [[ $wait_time -lt $timeout ]]; do
                        if ! systemctl is-active "$service_name" &>/dev/null; then
                            log_info "Service $service_name stopped successfully"
                            return 0
                        fi
                        sleep 2
                        wait_time=$((wait_time + 2))
                    done
                    log_warn "Service $service_name did not stop within ${timeout}s, forcing stop..."
                    safe_sudo systemctl kill "$service_name" || true
                else
                    log_error "Failed to stop service $service_name"
                    return 1
                fi
            else
                log_info "Service $service_name is already stopped"
            fi
            ;;
        "disable")
            if systemctl is-enabled "$service_name" &>/dev/null; then
                log_info "Disabling $service_name service..."
                if safe_sudo systemctl disable "$service_name"; then
                    log_info "Service $service_name disabled successfully"
                else
                    log_error "Failed to disable service $service_name"
                    return 1
                fi
            else
                log_info "Service $service_name is already disabled or not found"
            fi
            ;;
        *)
            log_error "Invalid action: $action. Valid actions: enable, start, stop, disable, restart"
            return 1
            ;;
    esac
    
    return 0
}

add_user_to_group() {
    local user="$1"
    local group="$2"
    
    if ! groups "$user" | grep -q "$group"; then
        log_info "Adding user '$user' to '$group' group..."
        safe_sudo usermod -aG "$group" "$user"
        log_info "User added to $group group. Please log out and back in for changes to take effect."
    else
        log_info "User $user is already in group $group"
    fi
}

#######################################
# Variable Validation Functions
#######################################
validate_required_variables() {
    log_info "Validating required variables..."
    
    local required_vars=("PYTHON_VERSION" "KUBESPRAY_REPO_URL" "KUBESPRAY_DIR")
    local missing_vars=()
    
    for var in "${required_vars[@]}"; do
        if [[ -z "${!var}" ]]; then
            missing_vars+=("$var")
        fi
    done
    
    if [[ ${#missing_vars[@]} -gt 0 ]]; then
        log_error "Required variables are not set: ${missing_vars[*]}"
        error_exit "Variable validation failed"
    fi
    
    # Validate Python version format
    if ! echo "$PYTHON_VERSION" | grep -qE '^[0-9]+\.[0-9]+\.[0-9]+$'; then
        error_exit "Invalid Python version format: $PYTHON_VERSION (expected: X.Y.Z)"
    fi
    
    # Validate kubespray directory permissions
    local parent_dir
    parent_dir="$(dirname "$KUBESPRAY_DIR")"
    if [[ ! -w "$parent_dir" ]]; then
        error_exit "No write permission for kubespray directory parent: $parent_dir"
    fi
    
    log_info "Variable validation passed"
}

#######################################
# System Validation Functions
#######################################
# Helper function to detect RHEL system more accurately
is_rhel_system() {    
    # Check for RHEL-specific files AND subscription-manager (both required for RHEL)
    if [[ -f /etc/redhat-release ]] && grep -q "Red Hat Enterprise Linux" /etc/redhat-release; then
        # Also verify subscription-manager is available and shows Red Hat identity
        if command -v subscription-manager &>/dev/null ; then
            return 0
        fi
    fi
    
    return 1
}

check_rhel_repositories() {
    log_info "Checking RHEL repository configuration..."
    
    # Check if this is a RHEL system using improved detection
    if ! is_rhel_system; then
        log_info "Not a RHEL system, skipping RHEL repository checks"
        return 0
    fi
    
    local rhel_version
    rhel_version=$(grep "VERSION_ID" /etc/os-release | cut -d'"' -f2 | cut -d'.' -f1)
    if [[ "$rhel_version" != "9" ]]; then
        error_exit "This script only supports RHEL 9. Current version: $rhel_version"
    fi
    
    log_info "Detected RHEL version: $rhel_version"
    
    # Define required repositories based on detected RHEL version
    local required_repos=(
        "rhel-${rhel_version}-for-$(arch)-baseos-rpms"
        "rhel-${rhel_version}-for-$(arch)-appstream-rpms"
        "codeready-builder-for-rhel-${rhel_version}-$(arch)-rpms"
    )
    
    log_info "Required repositories for RHEL ${rhel_version}: ${required_repos[*]}"
    
    # Check and enable required repositories
    local missing_repos=()
    local enabled_repos
    enabled_repos=$(safe_sudo subscription-manager repos --list-enabled 2>/dev/null | grep "Repo ID" | awk '{print $3}' || true)
    
    for repo in "${required_repos[@]}"; do
        if ! echo "$enabled_repos" | grep -q "^$repo$"; then
            missing_repos+=("$repo")
        else
            log_info "Repository already enabled: $repo"
        fi
    done
    
    # Enable missing repositories
    if [[ ${#missing_repos[@]} -gt 0 ]]; then
        log_info "Enabling missing RHEL repositories: ${missing_repos[*]}"
        
        for repo in "${missing_repos[@]}"; do
            log_info "Enabling repository: $repo"
            if safe_sudo subscription-manager repos --enable="$repo" 2>/dev/null; then
                log_info "Successfully enabled repository: $repo"
            else
                log_error "Failed to enable repository: $repo"
                error_exit "Required RHEL repository unavailable: $repo"
            fi
        done
        
        # Verify repositories are now enabled
        log_info "Verifying enabled repositories..."
        enabled_repos=$(safe_sudo subscription-manager repos --list-enabled 2>/dev/null | grep "Repo ID" | awk '{print $3}' || true)
        
        for repo in "${required_repos[@]}"; do
            if echo "$enabled_repos" | grep -q "^$repo$"; then
                log_info "✅ Repository verified: $repo"
            else
                log_error "❌ Repository verification failed: $repo"
                error_exit "Failed to enable required RHEL repository: $repo"
            fi
        done
        
        # Clean and update repository cache
        log_info "Updating repository cache..."
        safe_sudo dnf clean all >/dev/null 2>&1 || true
        safe_sudo dnf makecache >/dev/null 2>&1 || log_warn "Failed to update repository cache"
        
        log_info "RHEL repositories successfully configured"
    else
        log_info "All required RHEL repositories are already enabled"
    fi
    
    log_info "RHEL repository check completed"
}

check_system_requirements() {
    log_info "Checking system requirements..."
    
    # Check operating system
    if ! grep -q "Red Hat\|CentOS\|Rocky\|AlmaLinux" /etc/os-release; then
        error_exit "This script is designed for RHEL-based distributions"
    fi
    
    # Check and enable RHEL repositories if needed (uses improved RHEL detection)
    check_rhel_repositories
    
    # Check system architecture
    if [ "$(uname -m)" != "x86_64" ]; then
        log_warn "This script is optimized for x86_64 architecture"
    fi
    
    # Check disk space (at least 200GB free)
    local available_disk
    available_disk=$(df / | awk 'NR==2 {print $4}')
    local required_disk=209715200  # 200GB in KB
    if [ "$available_disk" -lt "$required_disk" ]; then
        error_exit "Insufficient disk space. At least 200GB required, but only $((available_disk/1024/1024))GB available."
    else
        log_info "Disk space check passed: $((available_disk/1024/1024))GB available"
    fi
    
    # Check available memory (at least 32GB recommended)
    local available_memory
    available_memory=$(free -m | awk 'NR==2{print $7}')
    local required_memory=32768  # 32GB in MB
    if [ "$available_memory" -lt "$required_memory" ]; then
        log_warn "Insufficient memory. At least 32GB recommended, but only ${available_memory}MB available. Performance may be affected."
    else
        log_info "Memory check passed: ${available_memory}MB available"
    fi
    
    # Check CPU cores (at least 16 cores recommended)
    local cpu_cores
    cpu_cores=$(nproc)
    local required_cores=16
    if [ "$cpu_cores" -lt "$required_cores" ]; then
        error_exit "Insufficient CPU cores. At least 16 cores required, but only $cpu_cores available."
    else
        log_info "CPU cores check passed: $cpu_cores cores available"
    fi
    
    log_info "System requirements check passed"
}

check_ntp_synchronization() {
    log_info "Checking NTP time synchronization..."
    
    # Install and configure chronyd if needed
    if ! command -v chronyd >/dev/null 2>&1; then
        log_warn "chrony is not installed. Installing chrony for time synchronization..."
        install_packages "chrony"
    fi
    
    # Ensure chronyd service is enabled and running
    systemctl is-enabled chronyd &>/dev/null || {
        log_info "Enabling chronyd service..."
        manage_service "chronyd" "enable"
    }
    
    systemctl is-active chronyd &>/dev/null || {
        log_info "Starting chronyd service..."
        manage_service "chronyd" "start"
        sleep 3  # Wait for chronyd to start
    }
    
    # Check synchronization status using chronyc
    if command -v chronyc >/dev/null 2>&1; then
        local tracking_output
        tracking_output=$(chronyc tracking 2>/dev/null)
        local sync_status
        sync_status=$(echo "$tracking_output" | grep "Leap status" | awk '{print $4}')
        local time_offset
        time_offset=$(echo "$tracking_output" | grep "System time" | awk '{print $4}' | sed 's/seconds//')
        
        if [[ "$sync_status" == "Normal" ]]; then
            log_info "NTP synchronization status: Normal"
            if [[ -n "$time_offset" ]]; then
                local abs_offset=${time_offset#-}
                if (( $(echo "$abs_offset < 5" | bc -l 2>/dev/null || echo "1") )); then
                    log_info "Time offset: ${time_offset} seconds (within acceptable range)"
                else
                    log_warn "Time offset: ${time_offset} seconds (exceeds acceptable range of ±5s)"
                    log_warn "Consider manual time synchronization if issues persist"
                fi
            fi
        else
            log_warn "NTP synchronization status: ${sync_status:-unknown}"
            log_warn "Time synchronization may not be working properly"
        fi
    else
        log_warn "chronyc command not available, cannot verify detailed sync status"
    fi
    
    # Check using timedatectl as alternative
    if command -v timedatectl >/dev/null 2>&1; then
        local timedatectl_output
        timedatectl_output=$(timedatectl status)
        local ntp_status
        ntp_status=$(echo "$timedatectl_output" | grep "NTP service" | awk '{print $3}')
        local sync_status_alt
        sync_status_alt=$(echo "$timedatectl_output" | grep "synchronized" | awk '{print $3}')
        
        log_info "System time status: NTP service: ${ntp_status:-unknown}, Time synchronized: ${sync_status_alt:-unknown}"
        
        if [[ "$ntp_status" == "active" && "$sync_status_alt" == "yes" ]]; then
            log_info "System time is properly synchronized"
        else
            log_warn "System time synchronization may have issues - this could cause SSL certificate validation problems"
        fi
    fi
    
    log_info "Current system time: $(date)"
    
    # Test NTP server connectivity
    local ntp_servers=("pool.ntp.org" "time.nist.gov" "time.google.com")
    for server in "${ntp_servers[@]}"; do
        if timeout 5 bash -c "</dev/tcp/$server/123" 2>/dev/null; then
            log_info "NTP synchronization check completed"
            return 0
        fi
    done
    
    log_warn "Cannot reach any NTP servers. This may cause time synchronization issues."
    log_warn "Please check network connectivity and firewall settings."
    log_info "NTP synchronization check completed"
}

validate_configuration() {
    log_info "Validating proxy configuration..."
    
    # Validate HTTP proxy configuration
    if [[ -n "$HTTP_PROXY" ]]; then

        if ! curl -s --proxy "$HTTP_PROXY" --connect-timeout 10 http://www.google.com > /dev/null; then
            log_warn "HTTP proxy $HTTP_PROXY may not be working correctly"
        fi
    fi
    
    # Validate HTTPS proxy configuration
    if [[ -n "$HTTPS_PROXY" && "$HTTPS_PROXY" != "$HTTP_PROXY" ]]; then

        if ! curl -s --proxy "$HTTPS_PROXY" --connect-timeout 10 https://www.google.com > /dev/null; then
            log_warn "HTTPS proxy $HTTPS_PROXY may not be working correctly"
        fi
    fi
    
    log_info "Proxy configuration validation completed"
}

check_sudo_privileges() {
    log_info "Checking sudo privileges..."
    if [ "$EUID" -ne 0 ] && ! sudo -n true 2>/dev/null; then
        log_info "This script requires sudo privileges. Please enter your password when prompted."
        if ! sudo true; then
            error_exit "Failed to obtain sudo privileges"
        fi
    fi
    log_info "Sudo privileges confirmed"
}

#######################################
# Network and Proxy Functions
#######################################
test_network_connectivity() {
    log_info "Testing network connectivity..."
    
    local test_urls=("http://www.google.com" "https://github.com" "https://pypi.org")
    local proxy_set=""
    
    if [ -n "$HTTP_PROXY" ]; then
        proxy_set="$HTTP_PROXY"
    fi
    
    for url in "${test_urls[@]}"; do
        if curl -s --proxy "$proxy_set" --connect-timeout 10 "$url" > /dev/null; then
            log_info "Network connectivity test passed for $url"
            return 0
        fi
    done
    
    log_warn "Network connectivity test failed. Please check your internet connection and proxy settings."
    return 1
}

configure_git_proxy() {
    if [ -n "$GIT_PROXY" ]; then
        log_info "Configuring git proxy: $GIT_PROXY"
        git config --global http.proxy "$GIT_PROXY"
        git config --global https.proxy "$GIT_PROXY"
    else
        log_info "No git proxy configured"
    fi
}

#######################################
# Installation Functions
#######################################
install_system_dependencies() {
    log_info "Installing system dependencies..."
    
    # Install basic packages
    install_packages "$SYSTEM_PACKAGES"
    
    # Install Development Tools
    if ! dnf group list --installed | grep -q "Development Tools"; then
        log_info "Installing Development Tools..."
        safe_sudo dnf groupinstall -y "Development Tools"
    else
        log_info "Development Tools are already installed"
    fi
    
    # Install Libvirt packages
    install_packages "$LIBVIRT_PACKAGES"
    
    log_info "System dependencies installation completed"
}

configure_system_security() {
    log_info "Configuring system security..."
    
    # Configure firewall
    if systemctl is-active --quiet firewalld 2>/dev/null; then
        log_info "Stopping firewalld..."
        manage_service "firewalld" "stop"
    fi
    
    manage_service "firewalld" "disable"
    
    # Configure SELinux
    log_info "Configuring SELinux..."
    if [ "$(getenforce)" != "Disabled" ]; then
        log_info "Temporarily disabling SELinux"
        safe_sudo setenforce 0
        
        log_info "Permanently disabling SELinux"
        safe_sudo sed -i 's/^SELINUX=enforcing$/SELINUX=disabled/' "/etc/selinux/config"
        safe_sudo sed -i 's/^SELINUX=permissive$/SELINUX=disabled/' "/etc/selinux/config"
        
        log_info "SELinux disabled. Changes will be permanent after reboot."
    else
        log_info "SELinux is already disabled"
    fi
    
    log_info "System security configuration completed"
}

# Create and configure bridge network connections
create_bridge_connections() {
    local bridge_name="$1"
    local interface="$2"
    local slave_name="bridge-slave-$interface"
    
    # Create bridge connection if it doesn't exist
    if ! safe_sudo nmcli con show "$bridge_name" &>/dev/null; then
        log_info "Creating transparent bridge $bridge_name..."
        safe_sudo nmcli con add type bridge con-name "$bridge_name" ifname "$bridge_name" || \
            error_exit "Failed to create bridge connection $bridge_name"
        
        # Configure bridge (disable IP, disable STP)
        safe_sudo nmcli con mod "$bridge_name" ipv4.method disabled ipv6.method disabled || \
            error_exit "Failed to disable IP on bridge $bridge_name"
        safe_sudo nmcli con mod "$bridge_name" bridge.stp no || \
            log_warn "Failed to disable STP on bridge $bridge_name"
    fi
    
    # Create bridge slave connection if it doesn't exist
    if ! safe_sudo nmcli con show "$slave_name" &>/dev/null; then
        log_info "Adding $interface to bridge $bridge_name..."
        safe_sudo nmcli con add type bridge-slave ifname "$interface" master "$bridge_name" con-name "$slave_name" || \
            error_exit "Failed to add $interface to bridge $bridge_name"
    fi
    
    # Activate connections
    if ! safe_sudo nmcli con up "$bridge_name" || ! safe_sudo nmcli con up "$slave_name"; then
        error_exit "Failed to activate bridge connections"
    fi
    
    # Wait and verify bridge is ready
    sleep 2
    if ! ip link show "$bridge_name" | grep -q "state UP"; then
        error_exit "Bridge $bridge_name is not in UP state"
    fi
}

# Configure libvirt bridge network
configure_bridge_network() {
    local network_name="$1"
    local bridge_name="$2"
    local interface="$3"
    local xml_file="/tmp/$network_name.xml"
    
    # First create the bridge connections if interface is provided
    if [[ -n "$interface" ]]; then
        log_info "Setting up bridge connections for network '$network_name'..."
        create_bridge_connections "$bridge_name" "$interface"
    fi
    
    if safe_sudo virsh net-list --all | grep -q "$network_name"; then
        # Ensure existing network is active and set to autostart
        safe_sudo virsh net-list | grep -q "$network_name.*active" || \
            safe_sudo virsh net-start "$network_name" 2>/dev/null
        safe_sudo virsh net-list --autostart | grep -q "$network_name" || \
            safe_sudo virsh net-autostart "$network_name" 2>/dev/null
        log_info "Bridge network '$network_name' is already configured and active"
    else
        # Create new bridge network
        log_info "Creating libvirt bridge network '$network_name'..."
        cat > "$xml_file" << EOF
<network>
  <name>$network_name</name>
  <forward mode='bridge'/>
  <bridge name='$bridge_name'/>
</network>
EOF
        
        if ! safe_sudo virsh net-define "$xml_file" || \
           ! safe_sudo virsh net-autostart "$network_name" || \
           ! safe_sudo virsh net-start "$network_name"; then
            error_exit "Failed to create bridge network '$network_name'"
        fi
        
        rm -f "$xml_file"
        log_info "Bridge network '$network_name' created and configured"
    fi
}

# Configure Host-only network with DHCP disabled
configure_hostonly_network() {
    # Declare local variables
    local network_name
    local xml_file
    
    log_info "Configuring Host-only network without DHCP..."
    
    network_name="hostonly-network"
    xml_file="/tmp/$network_name.xml"
    
    # Check if Host-only network already exists
    if safe_sudo virsh net-list --all | grep -q "$network_name"; then
        log_info "Host-only network '$network_name' already exists"
        # Ensure it's active and set to autostart
        safe_sudo virsh net-list | grep -q "$network_name.*active" || \
            safe_sudo virsh net-start "$network_name" 2>/dev/null
        safe_sudo virsh net-list --autostart | grep -q "$network_name" || \
            safe_sudo virsh net-autostart "$network_name" 2>/dev/null
        return 0
    fi
    
    # Create Host-only network XML configuration
    log_info "Creating Host-only network '$network_name'..."
    cat > "$xml_file" << EOF
<network>
  <name>$network_name</name>
  <uuid>$(uuidgen)</uuid>
  <bridge name='virbr2' stp='on' delay='0'/>
  <mac address='52:54:00:12:34:57'/>
  <ip address='192.168.200.1' netmask='255.255.255.0'/>
</network>
EOF
    
    # Define, autostart and start the network
    if safe_sudo virsh net-define "$xml_file" && \
       safe_sudo virsh net-autostart "$network_name" && \
       safe_sudo virsh net-start "$network_name"; then
        log_info "Host-only network '$network_name' created successfully"
        log_info "  - Network: 192.168.200.0/24"
        log_info "  - Gateway: 192.168.200.1"
        log_info "  - DHCP: Disabled (static IP configuration required)"
    else
        error_exit "Failed to create Host-only network '$network_name'"
    fi
    
    # Clean up temporary file
    rm -f "$xml_file"
}

# Disable default libvirt network
disable_default_network() {
    # Declare local variables
    local libvirt_config_dir

    libvirt_config_dir="/etc/libvirt/qemu/networks"
    
    # Disable default network
    if safe_sudo virsh net-list --all | grep -q "default"; then
        safe_sudo virsh net-destroy default 2>/dev/null || true
        safe_sudo virsh net-autostart default --disable 2>/dev/null || true
    fi

    log_info "Default network disabled"
}

# Setup libvirt with bridge networking
setup_libvirt() {
    # Declare local variables
    local current_ip
    
    if [[ -n "${BRIDGE_INTERFACE:-}" ]]; then
        log_info "Setting up Libvirt with bridge networking..."
        
        # Verify bridge interface exists
        ip link show "$BRIDGE_INTERFACE" &>/dev/null || \
            error_exit "Network interface '$BRIDGE_INTERFACE' does not exist. Available: $(ip link show | grep -E '^[0-9]+:' | grep -v 'lo:' | cut -d: -f2 | tr -d ' ' | tr '\n' ' ')"
    else
        log_info "Setting up Libvirt without bridge networking..."
    fi
    
    # Enable and start libvirtd service
    manage_service "libvirtd" "enable"
    manage_service "libvirtd" "start"
    
    # Add current user to libvirt group
    add_user_to_group "$USER" "libvirt"
    
    # Configure libvirt networks
    log_info "Configuring libvirt networks..."
    
    # Configure bridge network only if BRIDGE_INTERFACE is set
    if [[ -n "${BRIDGE_INTERFACE:-}" ]]; then
        # Get current IP address of the interface for warning
        current_ip=$(ip addr show "$BRIDGE_INTERFACE" 2>/dev/null | grep -oP 'inet \K[^/]+' | head -1)
        
        # Interactive confirmation for bridge setup
        if [[ -n "$current_ip" ]]; then
            echo
            echo -e "${RED}⚠️  Bridge Configuration Warning${NC}"
        echo -e "${YELLOW}🔧 Interface:${NC} ${WHITE}$BRIDGE_INTERFACE${NC}"
        echo -e "${YELLOW}🌐 Current IP:${NC} ${WHITE}$current_ip${NC}"
        echo -e "${RED}⚠️  WARNING:${NC} Configuring bridge will remove this IP address and may disconnect existing connections!"
            echo
            
            if ! prompt_yes_no "Continue with bridge configuration?"; then
                echo
                echo -e "${YELLOW}⏸️  Bridge configuration cancelled by user.${NC}"
                echo
                exit 0
            fi
            
            # Second confirmation: require user to input the current IP address
            echo
            echo -e "${RED}🔐 Second Confirmation Required${NC}"
            echo -e "${YELLOW}🔒 Security Check:${NC} To proceed with bridge configuration"
            echo -e "${WHITE}   Please enter the current IP address of '$BRIDGE_INTERFACE'${NC}"
            echo -e "${RED}⚠️  This confirms you understand that IP '$current_ip' will be permanently removed${NC}"
            echo
            
            if ! prompt_confirmation_with_retry "Enter current IP address to confirm deletion" "$current_ip" 3; then
                echo
                echo -e "${RED}🚫 Bridge configuration cancelled for safety.${NC}"
                echo
                exit 0
            fi
            echo
            echo -e "${GREEN}✅ IP address confirmed. Proceeding with bridge configuration...${NC}"
            echo
        else
            echo
            echo -e "${RED}⚠️  Bridge Configuration Warning${NC}"
        echo -e "${YELLOW}🔧 Interface:${NC} ${WHITE}$BRIDGE_INTERFACE${NC}"
        echo -e "${RED}⚠️  WARNING:${NC} Configuring bridge will modify interface configuration and may affect network connectivity!"
            echo
            
            if ! prompt_yes_no "Continue with bridge configuration?"; then
                echo
                echo -e "${YELLOW}⏸️  Bridge configuration cancelled by user.${NC}"
                echo
                exit 0
            fi
        fi
        
        echo -e "${GREEN}🚀 Starting bridge configuration...${NC}"
    echo -e "${YELLOW}🌉 Configuring bridge network...${NC}"
        local bridge_name="br0"
        local network_name="bridge-network"
        configure_bridge_network "$network_name" "$bridge_name" "$BRIDGE_INTERFACE"

        log_info "Bridge network configured as default"
    else
        log_info "Skipping bridge network configuration (BRIDGE_INTERFACE not set)"
    fi
    
    # Configure additional networks
    configure_hostonly_network

    # disable default network
    disable_default_network

    log_info "Libvirt setup completed"
}

install_vagrant() {
    log_info "Installing Vagrant..."
    
    if command_exists vagrant; then
        log_info "Vagrant is already installed"
        return 0
    fi
    
    # Add HashiCorp repository
    if [ ! -f "/etc/yum.repos.d/hashicorp.repo" ]; then
        log_info "Adding HashiCorp YUM repository..."
        safe_sudo yum-config-manager --add-repo https://rpm.releases.hashicorp.com/RHEL/hashicorp.repo
    else
        log_info "HashiCorp repository is already configured"
    fi
    
    # Install Vagrant
    install_packages "vagrant"
    
    log_info "Vagrant installation completed"
}

install_vagrant_libvirt_plugin() {
    log_info "Installing Vagrant libvirt plugin..."

    # Enable EPEL repository
    log_info "Installing EPEL repository from network..."
    safe_sudo dnf install -y https://dl.fedoraproject.org/pub/epel/epel-release-latest-9.noarch.rpm || {
        log_error "Failed to install EPEL repository"
        error_exit "Failed to install EPEL repository"
    }

    # Enable CRB repository (skip for RHEL as it's handled in check_rhel_repositories)
    if ! is_rhel_system; then
        log_info "Enabling CRB repository..."
        safe_sudo dnf config-manager --set-enabled crb 2>/dev/null || {
            log_error "Failed to enable CRB repository"
            error_exit "Failed to enable CRB repository"
        }
    else
        log_info "Skipping CRB repository configuration for RHEL (handled in system requirements check)"
    fi
    
    if vagrant plugin list | grep -q "vagrant-libvirt"; then
        log_info "vagrant-libvirt plugin is already installed"
        vagrant plugin list
        return 0
    fi
    
    # Install plugin dependencies
    install_packages "$PLUGIN_DEPENDENCIES"
    
    # Configure proxy for plugin installation
    if [ -n "$HTTP_PROXY" ] || [ -n "$HTTPS_PROXY" ]; then
        log_info "Configuring proxy for vagrant plugin installation..."
        export http_proxy="$HTTP_PROXY"
        export https_proxy="$HTTPS_PROXY"
        export HTTP_PROXY="$HTTP_PROXY"
        export HTTPS_PROXY="$HTTPS_PROXY"
        export no_proxy="localhost,127.0.0.1,::1"
        export NO_PROXY="localhost,127.0.0.1,::1"
        export BUNDLE_HTTP_PROXY="$HTTP_PROXY"
        export BUNDLE_HTTPS_PROXY="$HTTPS_PROXY"
        export GEM_HTTP_PROXY="$HTTP_PROXY"
        export GEM_HTTPS_PROXY="$HTTPS_PROXY"
    else
        log_info "No proxy configuration needed for vagrant plugin installation"
    fi
    
    # Install plugin
    log_info "Installing vagrant-libvirt plugin..."
    vagrant plugin install vagrant-libvirt
    
    log_info "Vagrant libvirt plugin installation completed"
}

setup_python_environment() {
    log_info "Setting up Python environment..."
    
    # Install pyenv if not present
    if ! command_exists pyenv; then
        install_pyenv
    else
        log_info "pyenv is already installed"
        setup_pyenv_environment
    fi
    
    # Install Python version
    if ! pyenv versions --bare | grep -q "$PYTHON_VERSION"; then
        log_info "Installing Python $PYTHON_VERSION using pyenv..."
        pyenv install "$PYTHON_VERSION"
    else
        log_info "Python $PYTHON_VERSION is already installed"
    fi
    
    log_info "Python environment setup completed"
}

install_pyenv() {
    # Declare local variables
    local curl_cmd
    local curl_options
    local temp_script
    
    log_info "Installing pyenv..."
    
    # Install pyenv dependencies
    install_packages "$PYENV_DEPENDENCIES"
    
    # Remove existing pyenv installation
    if [ -d "$HOME/.pyenv" ]; then
        log_info "Removing existing pyenv installation..."
        rm -rf "$HOME/.pyenv"
    fi
    
    # Install pyenv with error handling and proxy support
    log_info "Downloading and installing pyenv..."
    
    # Prepare curl command with proxy support
    curl_cmd="curl"
    curl_options="--fail --location --show-error --silent"
    
    # Add proxy configuration if available
    if [ -n "$HTTP_PROXY" ]; then
        log_info "Using HTTP proxy: $HTTP_PROXY"
        curl_options="$curl_options --proxy $HTTP_PROXY"
    fi
    
    # Add timeout and retry options
    curl_options="$curl_options --connect-timeout 30 --max-time 300 --retry 3 --retry-delay 5"
    
    # Download and execute pyenv installer with error handling
    # First download the script to a temporary file
    temp_script="$(mktemp)"
    if ! eval "$curl_cmd $curl_options https://pyenv.run -o '$temp_script'"; then
        rm -f "$temp_script"
        log_error "Failed to download pyenv installer script"
        log_error "This could be due to:"
        log_error "  1. Network connectivity issues"
        log_error "  2. Proxy configuration problems"
        log_error "  3. GitHub/raw.githubusercontent.com access restrictions"
        log_error "  4. Firewall blocking the connection"
        log_error ""
        log_error "Current proxy settings:"
        log_error "  HTTP_PROXY: ${HTTP_PROXY:-Not set}"
        log_error ""
        log_error "Troubleshooting steps:"
        log_error "  1. Check network connectivity: curl -I https://raw.githubusercontent.com"
        log_error "  2. Verify proxy settings if behind corporate firewall"
        log_error "  3. Try manual installation: git clone https://github.com/pyenv/pyenv.git ~/.pyenv"
        error_exit "pyenv installer download failed"
    fi
    
    # Execute the downloaded script with proxy environment variables
    log_info "Executing pyenv installer script..."
    if [ -n "$HTTP_PROXY" ]; then
        export HTTP_PROXY
        export HTTPS_PROXY="$HTTP_PROXY"
        export http_proxy="$HTTP_PROXY"
        export https_proxy="$HTTP_PROXY"
    fi
    
    if ! bash "$temp_script"; then
        rm -f "$temp_script"
        log_error "Failed to execute pyenv installer script"
        log_error "This could be due to:"
        log_error "  1. Network connectivity issues during script execution"
        log_error "  2. Proxy configuration problems for git/curl operations in script"
        log_error "  3. Missing dependencies for pyenv installation"
        log_error "  4. Insufficient permissions"
        log_error ""
        log_error "Current proxy settings:"
        log_error "  HTTP_PROXY: ${HTTP_PROXY:-Not set}"
        log_error "  HTTPS_PROXY: ${HTTPS_PROXY:-Not set}"
        log_error ""
        log_error "Troubleshooting steps:"
        log_error "  1. Check network connectivity: curl -I https://raw.githubusercontent.com"
        log_error "  2. Verify proxy settings if behind corporate firewall"
        log_error "  3. Try manual installation: git clone https://github.com/pyenv/pyenv.git ~/.pyenv"
        log_error "  4. Check system dependencies: git, curl, build tools"
        error_exit "pyenv installation failed"
    fi
    
    # Clean up temporary script file
    rm -f "$temp_script"
    
    # Verify pyenv installation
    if [ ! -d "$HOME/.pyenv" ]; then
        log_error "pyenv installation directory not found after installation"
        error_exit "pyenv installation verification failed"
    fi
    
    # Configure shell environment
    setup_pyenv_environment
    
    log_info "pyenv installation completed successfully"
}

setup_pyenv_environment() {
    # Declare local variables
    local shell_rc
    
    # Determine shell configuration file
    case "$SHELL" in
        "/bin/bash")
            shell_rc="$HOME/.bashrc"
            ;;
        "/bin/zsh"|*/usr/bin/zsh)
            shell_rc="$HOME/.zshrc"
            ;;
        *)
            shell_rc="$HOME/.profile"
            ;;
    esac
    
    # Create shell config file if it doesn't exist
    if [ ! -f "$shell_rc" ]; then
        log_info "Creating shell config file: $shell_rc"
        touch "$shell_rc"
    fi
    
    # Configure pyenv environment variables
    if ! grep -q "PYENV_ROOT" "$shell_rc" 2>/dev/null; then
        log_info "Configuring pyenv environment variables in $shell_rc..."
        {
            echo "export PYENV_ROOT=\"\$HOME/.pyenv\""
            echo "export PATH=\"\$PYENV_ROOT/bin:\$PATH\""
            echo "eval \"\$(pyenv init --path)\""
        } >> "$shell_rc"
    else
        log_info "pyenv environment variables already configured in $shell_rc"
    fi
    
    # Export variables for current script
    export PYENV_ROOT="$HOME/.pyenv"
    export PATH="$PYENV_ROOT/bin:$PATH"
    eval "$(pyenv init --path)"
}

setup_virtual_environment() {
    local venv_dir="$KUBESPRAY_DIR/venv"
    
    log_info "Setting up Python virtual environment..."
    
    # Create virtual environment
    if [ ! -d "$venv_dir" ]; then
        log_info "Creating virtual environment: $venv_dir"
        python3 -m venv "$venv_dir"
    else
        log_info "Virtual environment already exists: $venv_dir"
    fi
    
    # Activate virtual environment
    log_info "Activating virtual environment..."
    # shellcheck disable=SC1091
    source "${venv_dir}"/bin/activate
    
    # Upgrade pip
    pip_install_cmd="pip install"
    if [ -n "$PIP_PROXY" ]; then
        pip_install_cmd="$pip_install_cmd --proxy=$PIP_PROXY"
    fi
    
    log_info "Upgrading pip..."
    $pip_install_cmd --upgrade pip
    
    # Install dependencies
    if [ -f "$KUBESPRAY_DIR/requirements.txt" ]; then
        log_info "Installing Kubespray dependencies..."
        $pip_install_cmd -r "$KUBESPRAY_DIR/requirements.txt" || {
            log_error "Failed to install Kubespray dependencies"
            error_exit "requirements.txt installation failed"
        }
    else
        log_error "requirements.txt not found in $KUBESPRAY_DIR"
        error_exit "requirements.txt not found"
    fi
    
    log_info "Virtual environment setup completed"
}

# Function to configure public network settings interactively
configure_public_network_settings() {
    # Declare local variables
    local config_file
    local temp_file
    local subnet
    local subnet_split4
    local netmask
    local gateway
    local dns_server

    config_file="$1"
    temp_file="${config_file}.tmp"
    
    log_info "Configuring public network settings..."
    echo
    echo -e "${YELLOW}🌐 Public Network Configuration${NC}"
    echo -e "${WHITE}Please provide the network configuration for public network:${NC}"
    echo
    
    # Get starting IP for VM allocation using unified function
    local starting_ip
    prompt_ip_input "Enter starting IP for VM allocation (e.g., 192.168.1.10)" "validate_vm_ip_range"
    starting_ip="$PROMPT_RESULT"
    
    # Extract subnet and fourth octet
    IFS='.' read -ra octets <<< "$starting_ip"
    subnet="${octets[0]}.${octets[1]}.${octets[2]}"
    subnet_split4="${octets[3]}"
    
    # Get netmask using unified function
    prompt_ip_input "Enter netmask (e.g., 255.255.255.0)"
    netmask="$PROMPT_RESULT"
    
    # Get gateway using unified function
    prompt_ip_input "Enter gateway IP (e.g., $subnet.1)"
    gateway="$PROMPT_RESULT"
    
    # Get DNS server using unified function
    prompt_ip_input "Enter DNS server IP (e.g., 8.8.8.8 or $gateway)"
    dns_server="$PROMPT_RESULT"
    
    echo
    echo -e "${GREEN}✅ Network configuration summary:${NC}"
    echo -e "${GREEN}   ├─ Starting IP:${NC} ${WHITE}$starting_ip${NC}"
    echo -e "${GREEN}   ├─ Netmask:${NC} ${WHITE}$netmask${NC}"
    echo -e "${GREEN}   ├─ Gateway:${NC} ${WHITE}$gateway${NC}"
    echo -e "${GREEN}   ├─ DNS Server:${NC} ${WHITE}$dns_server${NC}"
    echo -e "${GREEN}   └─ Bridge NIC:${NC} ${WHITE}br0${NC}"
    echo
    
    # Apply the configuration to the config file
    awk -v subnet="$subnet" \
        -v netmask="$netmask" \
        -v gateway="$gateway" \
        -v dns_server="$dns_server" \
        -v subnet_split4="$subnet_split4" \
        -v bridge_name="$DEFAULT_BRIDGE_NAME" '
    {
        if ($0 ~ /^\$subnet = /) {
            print "$subnet = \"" subnet "\""
        } else if ($0 ~ /^\$netmask = /) {
            print "$netmask = \"" netmask "\""
        } else if ($0 ~ /^\$gateway = /) {
            print "$gateway = \"" gateway "\""
        } else if ($0 ~ /^\$dns_server = /) {
            print "$dns_server = \"" dns_server "\""
        } else if ($0 ~ /^\$subnet_split4 = /) {
            print "$subnet_split4 = " subnet_split4
        } else if ($0 ~ /^\$bridge_nic = /) {
            print "$bridge_nic = \"" bridge_name "\""
        } else {
            print $0
        }
    }' "$config_file" > "$temp_file"
    
    # Replace the original file
    mv "$temp_file" "$config_file"
    
    # Show VM IP address preview
    echo -e "\n${YELLOW}🖥️  Virtual Machine IP Address Preview${NC}"
    echo -e "${WHITE}The following VMs will be created with these IP addresses:${NC}\n"
    
    # Get number of instances and instance name prefix from config file
    local num_instances
    num_instances=$(grep "^\$num_instances\s*=" "$config_file" | sed 's/.*=\s*\([0-9]*\).*/\1/' || echo "$DEFAULT_VM_INSTANCES")
    local instance_name_prefix
    instance_name_prefix=$(grep "^\$instance_name_prefix\s*=" "$config_file" | sed 's/.*=\s*"\([^"]*\)".*/\1/' || echo "$DEFAULT_INSTANCE_PREFIX")
    
    # Display VM preview
    for ((i=1; i<=num_instances; i++)); do
        local vm_ip
        vm_ip="$subnet.$((subnet_split4 + i))"
        local vm_name="${instance_name_prefix}-${i}"
        if [[ $i -eq 1 ]]; then
            echo -e "${GREEN}   ├─ VM $i:${NC} ${WHITE}$vm_name${NC} → ${CYAN}$vm_ip${NC} ${YELLOW}(Master Node)${NC}"
        else
            echo -e "${GREEN}   ├─ VM $i:${NC} ${WHITE}$vm_name${NC} → ${CYAN}$vm_ip${NC} (Worker Node)"
        fi
    done
    echo -e "${GREEN}   └─ Total:${NC} ${WHITE}$num_instances VMs${NC} from ${CYAN}$subnet.$((subnet_split4 + 1))${NC} to ${CYAN}$subnet.$((subnet_split4 + num_instances))${NC}"
    echo
    
    log_info "Public network configuration applied to $config_file"
}

configure_vagrant_config() {
    # Declare local variables
    local vagrant_dir
    local config_file
    local script_dir
    local network_type
    local template_file
    local temp_file
    
    vagrant_dir="$KUBESPRAY_DIR/vagrant"
    config_file="$vagrant_dir/config.rb"
    network_type="$PRIVATE_NETWORK_TYPE"
    
    script_dir="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
    template_file="$script_dir/vagrant_setup_scripts/vagrant-config/private_network-config.rb"
    
    if [[ -n "${BRIDGE_INTERFACE:-}" ]]; then
        network_type="$PUBLIC_NETWORK_TYPE"
        template_file="$script_dir/vagrant_setup_scripts/vagrant-config/public_network-config.rb"
        log_info "BRIDGE_INTERFACE detected: $BRIDGE_INTERFACE - Using public network configuration"
    else
        log_info "BRIDGE_INTERFACE not set - Using private network configuration"
    fi
    
    log_info "Configuring Vagrant config.rb with $network_type network..."
    
    # Create vagrant directory if it doesn't exist
    if [ ! -d "$vagrant_dir" ]; then
        log_info "Creating vagrant directory: $vagrant_dir"
        mkdir -p "$vagrant_dir"
    fi
    
    # Check if template file exists
    if [ ! -f "$template_file" ]; then
        log_warn "Template file not found: $template_file"
        log_warn "Skipping Vagrant config.rb configuration"
        return 0
    fi
    
    # Copy template to config.rb
    log_info "Copying template to $config_file"
    cp "$template_file" "$config_file"
    
    # Configure public network settings if using public network
    if [[ "$network_type" == "$PUBLIC_NETWORK_TYPE" ]]; then
        configure_public_network_settings "$config_file"
    fi
    
    # Configure proxy settings if HTTP_PROXY is set
    if [ -n "$HTTP_PROXY" ]; then
        log_info "Configuring proxy settings in config.rb"
        
        # Create a temporary file for safe editing
        temp_file="${config_file}.tmp"
        
        # Set $https_proxy (use HTTP_PROXY if HTTPS_PROXY is not set)
        local https_proxy_value="${HTTPS_PROXY:-$HTTP_PROXY}"
        
        # Set $no_proxy with common local addresses
        local no_proxy_value="localhost,127.0.0.1,10.0.0.0/8,192.168.0.0/16,172.16.0.0/12,.local"
        
        # Use awk for safer text replacement
        awk -v http_proxy="$HTTP_PROXY" \
            -v https_proxy="$https_proxy_value" \
            -v no_proxy="$no_proxy_value" \
            -v additional_no_proxy="${NO_PROXY:-}" '
        {
            if ($0 ~ /^# \$http_proxy = ""/) {
                print "$http_proxy = \"" http_proxy "\""
            } else if ($0 ~ /^# \$https_proxy = ""/) {
                print "$https_proxy = \"" https_proxy "\""
            } else if ($0 ~ /^# \$no_proxy = ""/) {
                print "$no_proxy = \"" no_proxy "\""
            } else if ($0 ~ /^# \$additional_no_proxy = ""/ && additional_no_proxy != "") {
                print "$additional_no_proxy = \"" additional_no_proxy "\""
            } else {
                print $0
            }
        }' "$config_file" > "$temp_file"
        
        # Replace the original file
        mv "$temp_file" "$config_file"
        
        log_info "Proxy configuration completed:"
        log_info "  HTTP_PROXY: $HTTP_PROXY"
        log_info "  HTTPS_PROXY: $https_proxy_value"
        log_info "  NO_PROXY: $no_proxy_value"
        if [ -n "${NO_PROXY:-}" ]; then
            log_info "  ADDITIONAL_NO_PROXY: $NO_PROXY"
        fi
    else
        log_info "No HTTP_PROXY set, keeping proxy settings commented out"
    fi
    
    log_info "Vagrant config.rb configuration completed: $config_file"
}

setup_kubespray_project() {
    log_info "Setting up Kubespray project..."
        
    # Check if KUBESPRAY_DIR already exists
    if [ -d "$KUBESPRAY_DIR" ]; then
        log_info "Kubespray directory already exists, updating..."
        cd "$KUBESPRAY_DIR"
        if ! git pull; then
            log_error "Failed to update Kubespray repository"
            error_exit "Kubespray repository update failed"
        fi
    else
        # Configure git proxy and clone repository
        configure_git_proxy
        
        log_info "Cloning Kubespray repository..."
        if ! git clone "$KUBESPRAY_REPO_URL" "$KUBESPRAY_DIR"; then
            log_error "Failed to clone Kubespray repository"
            error_exit "Kubespray repository clone failed"
        fi
    fi
    
    # Set up Python environment for project
    cd "$KUBESPRAY_DIR"
    pyenv local "$PYTHON_VERSION"
    
    # Create and activate virtual environment
    setup_virtual_environment
    
    # Configure Vagrant config.rb
    configure_vagrant_config
    
    # Replace project Vagrantfile with vagrant_setup_scripts version
    log_info "Replacing project Vagrantfile with vagrant_setup_scripts version..."
    local source_vagrantfile="$KUBESPRAY_DIR/vagrant_setup_scripts/Vagrantfile"
    local target_vagrantfile="$KUBESPRAY_DIR/Vagrantfile"
    
    if [[ -f "$source_vagrantfile" ]]; then
        if cp "$source_vagrantfile" "$target_vagrantfile"; then
            log_info "Successfully replaced Vagrantfile"
        else
            log_error "Failed to replace Vagrantfile"
            error_exit "Vagrantfile replacement failed"
        fi
    else
        log_error "Source Vagrantfile not found: $source_vagrantfile"
        error_exit "Source Vagrantfile not found"
    fi
    
    log_info "Kubespray project setup completed"
}

#######################################
# Main Function
#######################################
main() {
    # Initialize log file with proper permissions
    touch "$LOG_FILE" 2>/dev/null || {
        echo "Warning: Cannot create log file $LOG_FILE, logging to stdout only"
        LOG_FILE="/dev/stdout"
    }
    chmod 666 "$LOG_FILE" 2>/dev/null || true
    
    log_info "Starting Kubespray Libvirt Environment Setup v$SCRIPT_VERSION"
    log_info "Log file: $LOG_FILE"
    
    # Validate optional environment variables
    if [[ -n "${BRIDGE_INTERFACE:-}" ]]; then
        log_info "Using bridge interface: $BRIDGE_INTERFACE"
        
        # Verify the specified interface exists
        if ! ip link show "$BRIDGE_INTERFACE" &>/dev/null; then
            error_exit "Network interface '$BRIDGE_INTERFACE' does not exist. Available interfaces: $(ip link show | grep -E '^[0-9]+:' | grep -v 'lo:' | cut -d: -f2 | tr -d ' ' | tr '\n' ' ')"
        fi
    else
        log_info "BRIDGE_INTERFACE not set - bridge network will be skipped"
    fi
    
    # Variable validation
    validate_required_variables
    # System validation
    check_sudo_privileges
    check_system_requirements
    check_ntp_synchronization
    validate_configuration
    test_network_connectivity
    # Pre-installation confirmation
    show_interactive_confirmation "setup"
    # Installation steps
    install_system_dependencies
    configure_system_security
    setup_libvirt
    install_vagrant
    install_vagrant_libvirt_plugin
    setup_python_environment
    setup_kubespray_project
    
    echo -e "\n${GREEN}🎉 Environment Setup Completed Successfully!${NC}\n"

    show_interactive_confirmation "deployment"
    
    log_info "Kubespray environment setup completed successfully!"
}

#######################################
# Parse and display Vagrant configuration
#######################################
parse_vagrant_config() {
    # Declare local variables
    local config_file
    local num_instances
    local kube_master_instances
    local upm_ctl_instances
    local vm_cpus
    local vm_memory
    local kube_master_vm_cpus
    local kube_master_vm_memory
    local upm_control_plane_vm_cpus
    local upm_control_plane_vm_memory
    local kube_version
    local os
    local network_plugin
    local instance_name_prefix
    local worker_nodes
    local vm_memory_gb
    local kube_master_vm_memory_gb
    local upm_control_plane_vm_memory_gb
    
    config_file="$1"
    
    if [[ ! -f "$config_file" ]]; then
        log_warn "Config file not found: $config_file"
        return 1
    fi
    
    # Extract configuration values from config.rb
    
    num_instances=$(grep "^\$num_instances\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "$DEFAULT_VM_INSTANCES")
    kube_master_instances=$(grep "^\$kube_master_instances\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "1")
    upm_ctl_instances=$(grep "^\$upm_ctl_instances\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "1")
    vm_cpus=$(grep "^\$vm_cpus\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "8")
    vm_memory=$(grep "^\$vm_memory\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "16384")
    kube_master_vm_cpus=$(grep "^\$kube_master_vm_cpus\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "4")
    kube_master_vm_memory=$(grep "^\$kube_master_vm_memory\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "4096")
    upm_control_plane_vm_cpus=$(grep "^\$upm_control_plane_vm_cpus\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "12")
    upm_control_plane_vm_memory=$(grep "^\$upm_control_plane_vm_memory\s*=" "$config_file" | sed 's/.*=\s*\([0-9]\+\).*/\1/' || echo "24576")
    kube_version=$(grep "^\$kube_version\s*=" "$config_file" | sed 's/.*=\s*"\([^"]*\)".*/\1/' || echo "1.33.2")
    os=$(grep "^\$os\s*=" "$config_file" | sed 's/.*=\s*"\([^"]*\)".*/\1/' || echo "rockylinux9")
    network_plugin=$(grep "^\$network_plugin\s*=" "$config_file" | sed 's/.*=\s*"\([^"]*\)".*/\1/' || echo "calico")
    instance_name_prefix=$(grep "^\$instance_name_prefix\s*=" "$config_file" | sed 's/.*=\s*"\([^"]*\)".*/\1/' || echo "$DEFAULT_INSTANCE_PREFIX")

    # Calculate worker nodes
    worker_nodes=$((num_instances - kube_master_instances - upm_ctl_instances))
    if [[ $worker_nodes -lt 0 ]]; then
        worker_nodes=0
    fi
    
    # Convert memory from MB to GB for display
    vm_memory_gb=$((vm_memory / 1024))
    kube_master_vm_memory_gb=$((kube_master_vm_memory / 1024))
    upm_control_plane_vm_memory_gb=$((upm_control_plane_vm_memory / 1024))
    
    # Calculate total resources
    local total_cpus
    total_cpus=$((worker_nodes * vm_cpus + kube_master_instances * kube_master_vm_cpus + upm_ctl_instances * upm_control_plane_vm_cpus))
    local total_memory_mb
    total_memory_mb=$((worker_nodes * vm_memory + kube_master_instances * kube_master_vm_memory + upm_ctl_instances * upm_control_plane_vm_memory))
    local total_memory_gb
    total_memory_gb=$((total_memory_mb / 1024))
    
    echo -e "\n${GREEN}🎯 Kubernetes Cluster Configuration${NC}\n"
    
    echo -e "${WHITE}📋 Cluster:${NC}"
    echo -e "   ${GREEN}•${NC} Kubernetes: ${CYAN}$kube_version${NC}"
    echo -e "   ${GREEN}•${NC} OS: ${CYAN}$os${NC}"
    echo -e "   ${GREEN}•${NC} Network Plugin: ${CYAN}$network_plugin${NC}"
    echo -e "   ${GREEN}•${NC} Prefix: ${CYAN}$instance_name_prefix${NC}\n"
    
    echo -e "${WHITE}🖥️  Nodes:${NC}"
    echo -e "   ${GREEN}•${NC} Masters: ${WHITE}$kube_master_instances${NC} × ${CYAN}${kube_master_vm_cpus}C/${kube_master_vm_memory_gb}GB${NC}"
    echo -e "   ${GREEN}•${NC} Workers: ${WHITE}$worker_nodes${NC} × ${CYAN}${vm_cpus}C/${vm_memory_gb}GB${NC}"
    echo -e "   ${GREEN}•${NC} UPM Control: ${WHITE}$upm_ctl_instances${NC} × ${CYAN}${upm_control_plane_vm_cpus}C/${upm_control_plane_vm_memory_gb}GB${NC}\n"
    
    echo -e "${WHITE}📊 Total Resources:${NC}"
    echo -e "   ${GREEN}•${NC} Nodes: ${WHITE}$num_instances${NC}"
    echo -e "   ${GREEN}•${NC} CPUs: ${RED}$total_cpus${NC} cores"
    echo -e "   ${GREEN}•${NC} Memory: ${RED}${total_memory_gb}GB${NC}\n"
    
    echo -e "${WHITE}⚙️  Config: ${CYAN}$config_file${NC}"
    
    # Resource Warning if needed
    if [[ $total_cpus -gt 32 ]] || [[ $total_memory_gb -gt 128 ]]; then
        echo -e "${RED}⚠️  Warning: High resource requirements (${total_cpus}C/${total_memory_gb}GB)${NC}"
    fi
    
    return 0
}

#######################################
# Generic interactive confirmation function
#######################################
show_interactive_confirmation() {
    local mode="$1"  # "setup" or "deployment"
    
    if [[ "$mode" == "setup" ]]; then
        # Installation Preview Mode
        echo -e "\n${GREEN}🚀 Kubespray Libvirt Environment Setup${NC}\n"
        
        # Key Components
        echo -e "${WHITE}📦 Will Install:${NC}"
        echo -e "   ${GREEN}•${NC} Virtualization: ${CYAN}libvirt + QEMU/KVM${NC}"
        echo -e "   ${GREEN}•${NC} Container: ${CYAN}Vagrant 2.4.7 + libvirt plugin${NC}"
        echo -e "   ${GREEN}•${NC} Python: ${CYAN}pyenv + Python $PYTHON_VERSION${NC}\n"
        
        # Network Configuration
        echo -e "${WHITE}🌐 Network Setup:${NC}"
        if [[ -n "${BRIDGE_INTERFACE:-}" ]]; then
            echo -e "   ${GREEN}•${NC} Bridge: ${CYAN}br0${NC} (using interface: ${YELLOW}$BRIDGE_INTERFACE${NC})"
        else
            echo -e "   ${YELLOW}•${NC} Bridge: ${YELLOW}Not configured${NC}"
        fi
        echo -e "   ${GREEN}•${NC} NAT: ${CYAN}192.168.121.0/24${NC} (DHCP: Enabled)"
        echo -e "   ${GREEN}•${NC} Host-only: ${CYAN}192.168.200.0/24${NC} (DHCP: Disabled)"
        
        # Proxy Configuration
        if [[ -n "${HTTP_PROXY:-}" ]]; then
            echo -e "   ${GREEN}•${NC} Proxy: ${CYAN}${HTTP_PROXY}${NC}"
            if [[ -n "${HTTPS_PROXY:-}" && "${HTTPS_PROXY}" != "${HTTP_PROXY}" ]]; then
                echo -e "   ${GREEN}•${NC} HTTPS Proxy: ${CYAN}${HTTPS_PROXY}${NC}"
            fi
            if [[ -n "${NO_PROXY:-}" ]]; then
                echo -e "   ${GREEN}•${NC} No Proxy: ${CYAN}${NO_PROXY}${NC}"
            fi
        else
            echo -e "   ${YELLOW}•${NC} Proxy: ${YELLOW}Not configured${NC}"
        fi
        echo
        
        # Critical Changes
        echo -e "${YELLOW}⚠️  System Changes:${NC}"
        echo -e "   ${RED}•${NC} Security: ${RED}Firewall & SELinux disabled${NC}"
        echo -e "   ${GREEN}•${NC} Services: ${GREEN}libvirtd enabled${NC}"
        echo -e "   ${GREEN}•${NC} User: ${GREEN}Added to libvirt group${NC}\n"
        
        # Time & Resources
        echo -e "${CYAN}⏱️  Estimates: ${YELLOW}15-25 min, ~1GB download, ~5GB disk${NC}"
        # Important Notes
        echo -e "${RED}⚠️  Requirements: sudo access, stable internet${NC}\n"
        
        # Confirmation prompt for setup
        if prompt_yes_no "Do you want to proceed with the installation?"; then
            echo -e "\n${GREEN}✅ Installation confirmed. Proceeding...${NC}\n"
            log_info "User confirmed installation. Starting setup process..."
            return 0
        else
            echo -e "\n${RED}❌ Installation cancelled by user.${NC}\n"
            log_info "Installation cancelled by user"
            exit 0
        fi
        
    elif [[ "$mode" == "deployment" ]]; then
        # Deployment Confirmation Mode
        local config_file="$KUBESPRAY_DIR/vagrant/config.rb"
        
        # Parse and display configuration
        if ! parse_vagrant_config "$config_file"; then
            log_error "Failed to parse Vagrant configuration"
            return 1
        fi
        
        echo -e "\n${GREEN}🚀 Ready to Deploy Kubernetes Cluster${NC}\n"
        
        # Deployment Commands
        echo -e "${WHITE}📋 Commands to Execute:${NC}"
        echo -e "   ${GREEN}1.${NC} ${CYAN}cd $KUBESPRAY_DIR${NC}"
        echo -e "   ${GREEN}2.${NC} ${CYAN}source venv/bin/activate${NC}"
        echo -e "   ${GREEN}3.${NC} ${CYAN}vagrant up --provider=libvirt --no-parallel${NC}\n"
        
        # Time Estimate
        echo -e "${YELLOW}⏱️  Estimated time: 20-30 minutes${NC}\n"
        
        # Confirmation prompt for deployment
        if prompt_yes_no "Continue with deployment?"; then
            echo
            echo -e "${GREEN}🚀 Starting Kubernetes cluster deployment...${NC}"
            echo
            
            # Change to kubespray directory
            echo -e "${YELLOW}📁 Changing to kubespray directory...${NC}"
            cd "$KUBESPRAY_DIR" || {
                error_exit "Failed to change to kubespray directory: $KUBESPRAY_DIR"
            }
            
            # Activate virtual environment
            echo -e "${YELLOW}🐍 Activating Python virtual environment...${NC}"
            # shellcheck disable=SC1091
            source venv/bin/activate || {
                error_exit "Failed to activate virtual environment"
            }
            
            # Start vagrant deployment
            echo -e "${YELLOW}⚙️  Starting Vagrant deployment (this may take 15-30 minutes)...${NC}\n"
            if vagrant up --provider=libvirt --no-parallel; then
                echo -e "\n${GREEN}🎉 Deployment Completed Successfully!${NC}\n"

                # Configure kubectl for local access
                echo -e "${YELLOW}🔧 Configuring kubectl for local access...${NC}"
                if configure_kubectl_access; then
                    echo -e "${GREEN}✅ kubectl configured successfully${NC}\n"
                    
                    display_cluster_info
                else
                    echo -e "${YELLOW}⚠️  kubectl configuration failed or artifacts not found${NC}\n"
                fi
                
                echo -e "${WHITE}🎯 Cluster Access Options:${NC}"
                echo -e "   ${GREEN}Local:${NC}"
                echo -e "     ${GREEN}•${NC} ${CYAN}kubectl get nodes${NC} (if configured above)"
                echo -e "     ${GREEN}•${NC} ${CYAN}kubectl get pods --all-namespaces${NC}\n"
                
                echo -e "${WHITE}⚙️  Management:${NC}"
                echo -e "   ${RED}•${NC} Stop: ${CYAN}vagrant halt${NC}"
                echo -e "   ${GREEN}•${NC} Start: ${CYAN}vagrant up${NC}"
                echo -e "   ${YELLOW}•${NC} Destroy: ${CYAN}vagrant destroy -f${NC}\n"
            else
                echo -e "\n${RED}❌ Deployment failed! Check logs above.${NC}\n"
                echo -e "${YELLOW}🔄 Retry: ${CYAN}cd $KUBESPRAY_DIR && source venv/bin/activate && vagrant up --provider=libvirt --no-parallel${NC}\n"
                return 1
            fi
            return 0
        else
            echo -e "\n${YELLOW}⏸️  Deployment cancelled.${NC}\n"
            echo -e "${WHITE}📝 Config: ${CYAN}$config_file${NC}\n"
            echo -e "${WHITE}🚀 Manual deploy: ${CYAN}cd $KUBESPRAY_DIR && source venv/bin/activate && vagrant up --provider=libvirt --no-parallel${NC}\n"
            return 0
        fi
    else
        log_error "Invalid mode: $mode. Use 'setup' or 'deployment'."
        return 1
    fi
}

#######################################
# Configure kubectl for local access
#######################################
configure_kubectl_access() {
    log_info "Configuring kubectl for local access..."
    
    local artifacts_dir="$KUBESPRAY_DIR/inventory/sample/artifacts"
    local kubectl_binary="$artifacts_dir/kubectl"
    local kubeconfig_file="$artifacts_dir/admin.conf"
    local local_bin_dir="$HOME/.local/bin"
    local kube_dir="$HOME/.kube"
    local shell_config="$HOME/.bashrc"
    local success_count=0
    local total_steps=2
    
    # Check if artifacts directory exists
    if [[ ! -d "$artifacts_dir" ]]; then
        log_warn "Artifacts directory not found: $artifacts_dir"
        log_warn "kubectl configuration skipped. You may need to configure it manually."
        log_warn "Try running: vagrant ssh -c 'sudo cp /usr/local/bin/kubectl /vagrant/kubectl && sudo cp /etc/kubernetes/admin.conf /vagrant/admin.conf'"
        return 1
    fi
    
    # Create directories with error handling
    for dir in "$local_bin_dir" "$kube_dir"; do
        if [[ ! -d "$dir" ]]; then
            log_info "Creating directory: $dir"
            if ! mkdir -p "$dir"; then
                log_error "Failed to create directory: $dir"
                return 1
            fi
        fi
    done
    
    # Handle kubectl binary
    if [[ -f "$kubectl_binary" ]]; then
        log_info "Copying kubectl binary to $local_bin_dir/kubectl"
        if cp "$kubectl_binary" "$local_bin_dir/kubectl" && chmod +x "$local_bin_dir/kubectl"; then
            success_count=$((success_count + 1))
            log_info "kubectl binary configured successfully"
        else
            log_error "Failed to copy or set permissions for kubectl binary"
        fi
    else
        log_warn "kubectl binary not found: $kubectl_binary"
        log_warn "You may need to install kubectl manually or copy it from the cluster"
    fi
    
    # Handle kubeconfig file
    if [[ -f "$kubeconfig_file" ]]; then
        log_info "Copying kubeconfig to $kube_dir/config"
        if cp "$kubeconfig_file" "$kube_dir/config" && chmod 600 "$kube_dir/config"; then
            success_count=$((success_count + 1))
            log_info "kubeconfig configured successfully"
        else
            log_error "Failed to copy or set permissions for kubeconfig"
        fi
    else
        log_warn "kubeconfig file not found: $kubeconfig_file"
        log_warn "You may need to copy it manually from the cluster"
    fi
    
    # Verify configuration
    if [[ $success_count -eq $total_steps ]]; then
        log_info "kubectl configuration completed successfully ($success_count/$total_steps steps)"
        log_info "Please run 'source ~/.bashrc' or restart your terminal to apply changes"
        
        # Test kubectl with retry mechanism
        if [[ -x "$local_bin_dir/kubectl" && -f "$kube_dir/config" ]]; then
            log_info "Testing kubectl connection with retry..."
            local max_attempts=4
            local wait_seconds=30
            local attempt=1
            
            while [[ $attempt -le $max_attempts ]]; do
                log_info "Attempt $attempt/$max_attempts: Testing kubectl connection..."
                if timeout 10 "$local_bin_dir/kubectl" --kubeconfig="$kube_dir/config" cluster-info &>/dev/null; then
                    log_info "kubectl connection test successful"
                    break
                else
                    if [[ $attempt -eq $max_attempts ]]; then
                        log_warn "kubectl connection test failed after $max_attempts attempts - cluster may still be starting"
                    else
                        log_info "Connection failed, waiting ${wait_seconds}s before retry..."
                        sleep $wait_seconds
                    fi
                fi
                ((attempt++))
            done
        fi
        return 0
    else
        log_warn "kubectl configuration partially completed ($success_count/$total_steps steps)"
        return 1
    fi
}

#######################################
# Display cluster information
#######################################
display_cluster_info() {
    log_info "Displaying Kubernetes cluster information..."
    
    local kubectl_cmd="$HOME/.local/bin/kubectl"
    local kubeconfig="$HOME/.kube/config"
    
    # Check if kubectl and kubeconfig are available
    if [[ ! -f "$kubectl_cmd" ]] || [[ ! -f "$kubeconfig" ]]; then
        log_warn "kubectl or kubeconfig not found. Skipping cluster info display."
        return 1
    fi
    
    # Set KUBECONFIG for this session
    export KUBECONFIG="$kubeconfig"
    
    echo -e "\n${GREEN}🎯 Kubernetes Cluster Information${NC}\n"
    
    # Display cluster info
    echo -e "${WHITE}📊 Cluster Status:${NC}"
    if timeout 30 "$kubectl_cmd" cluster-info 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to connect to cluster${NC}"
        echo -e "   ${YELLOW}•${NC} ${YELLOW}The cluster may still be initializing${NC}\n"
        return 1
    fi
    
    # Display nodes
    echo -e "${WHITE}🖥️  Nodes:${NC}"
    if timeout 30 "$kubectl_cmd" get nodes -o wide 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve node information${NC}\n"
    fi
    
    # Display namespaces
    echo -e "${WHITE}📦 Namespaces:${NC}"
    if timeout 30 "$kubectl_cmd" get namespaces 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve namespace information${NC}\n"
    fi
    
    # Display pods in kube-system
    echo -e "${WHITE}🔧 System Pods (kube-system):${NC}"
    if timeout 30 "$kubectl_cmd" get pods -n kube-system 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve pod information${NC}\n"
    fi
    
    # Display kubectl usage instructions
    echo -e "${WHITE}💡 kubectl Usage:${NC}"
    echo -e "   ${GREEN}•${NC} Config: ${CYAN}$kubeconfig${NC}"
    echo -e "   ${GREEN}•${NC} Binary: ${CYAN}$kubectl_cmd${NC}"
    echo -e "   ${GREEN}•${NC} Get nodes: ${CYAN}kubectl get nodes${NC}"
    echo -e "   ${GREEN}•${NC} Get pods: ${CYAN}kubectl get pods --all-namespaces${NC}"
    echo -e "   ${GREEN}•${NC} Get services: ${CYAN}kubectl get services --all-namespaces${NC}\n"
    
    echo -e "${YELLOW}💡 Note: You may need to source ~/.bashrc or restart your terminal for PATH changes to take effect${NC}\n"
    
    return 0
}

#######################################
# Script Execution Entry Point
#######################################
# Support both direct execution and curl | bash execution
if [[ "${BASH_SOURCE[0]}" == "${0}" ]] || [[ "${BASH_SOURCE[0]}" == "bash" ]] || [[ -z "${BASH_SOURCE[0]}" ]] || [[ "${0}" == "bash" ]]; then
    main "$@"
fi
