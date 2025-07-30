#!/usr/bin/env bash
#
# Kubespray Libvirt Environment Setup Script
#
# Description:
#   Automated setup script for Kubespray Kubernetes cluster with libvirt virtualization
#   on RHEL-based distributions (RHEL/Rocky/AlmaLinux 8/9).
#
# Requirements:
#   - Hardware: CPU 12+ cores, Memory 32GB+, Storage 200GB+
#   - Access: sudo privileges, Internet connectivity
#   - Virtualization: Intel VT-x/AMD-V enabled
#
# Features:
#   - libvirt/KVM virtualization setup
#   - Python environment via pyenv
#   - Kubernetes cluster deployment
#   - Network configuration (nat/bridge)
#   - Proxy support for enterprise environments
#
# Environment Variables:
#   HTTP_PROXY/HTTPS_PROXY - Proxy server URLs
#   NO_PROXY               - Hosts to bypass proxy
#   PYTHON_VERSION         - Python version (default: 3.12.11)
#
# Key Paths:
#   KUBESPRAY_DIR - ./kubespray-upm
#   KUBECONFIG    - $HOME/.kube/config
#   LOG_FILE      - ./libvirt_kubespray_setup.log
#
# License: Apache License 2.0
# Author: Kubespray UPM Team
# Repository: https://github.com/upmio/kubespray-upm
#

set -eE

#######################################
# Cleanup and Signal Handling
#######################################
cleanup() {
    local exit_code=$?
    log_info "Cleaning up temporary resources..."
    exit $exit_code
}

# Set up signal handlers
trap cleanup EXIT INT TERM

#######################################
# Constants and Configuration
#######################################

# Script metadata
readonly SCRIPT_VERSION="1.0"
readonly SCRIPT_NAME="Kubespray Libvirt Environment Setup Script"
readonly SCRIPT_AUTHOR="Kubespray UPM Team"
readonly SCRIPT_LICENSE="Apache License 2.0"
readonly SCRIPT_REPOSITORY="https://github.com/upmio/kubespray-upm"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
readonly SCRIPT_DIR
readonly KUBESPRAY_DIR="${SCRIPT_DIR}/kubespray-upm"
readonly KUBESPRAY_REPO_URL="https://github.com/upmio/kubespray-upm.git"
readonly PYTHON_VERSION="3.12.11"
readonly VAGRANT_CONF_DIR="${KUBESPRAY_DIR}/vagrant"
readonly VAGRANT_CONF_FILE="${VAGRANT_CONF_DIR}/config.rb"
readonly VAGRANTFILE_PATH="${KUBESPRAY_DIR}/Vagrantfile"
readonly LOCAL_BIN_DIR="${HOME}/bin"
readonly KUBECTL="${LOCAL_BIN_DIR}/kubectl"
readonly KUBE_DIR="${HOME}/.kube"
export KUBECONFIG="${KUBE_DIR}/config"

# Network configuration constants
readonly BRIDGE_NAME="br0"

# Package lists
readonly SYSTEM_PACKAGES="curl git rsync yum-utils"
readonly LIBVIRT_PACKAGES="qemu-kvm libvirt libvirt-python3 libvirt-client virt-install virt-viewer virt-manager"
readonly PLUGIN_DEPENDENCIES="pkgconf-pkg-config libvirt-libs libvirt-devel libxml2-devel libxslt-devel ruby-devel gcc gcc-c++ make krb5-devel zlib-devel bridge-utils"
readonly PYENV_DEPENDENCIES="gcc make patch zlib-devel bzip2 bzip2-devel readline-devel sqlite sqlite-devel openssl-devel tk-devel libffi-devel xz-devel"

# Global configuration variables (initialized from environment)
declare HTTP_PROXY="${HTTP_PROXY:-${http_proxy:-""}}"
declare HTTPS_PROXY="${HTTPS_PROXY:-${https_proxy:-$HTTP_PROXY}}"
declare NO_PROXY="${NO_PROXY:-${no_proxy:-"localhost,127.0.0.1,192.168.0.0/16,10.0.0.0/8"}}"
declare PIP_PROXY="${PIP_PROXY:-${HTTP_PROXY:-""}}"
declare GIT_PROXY="${GIT_PROXY:-${HTTP_PROXY:-""}}"
declare BRIDGE_INTERFACE=""
declare NETWORK_TYPE="nat"

# Global variable for prompt function results
declare PROMPT_RESULT=""

# Global variable for auto-confirm mode (-y parameter)
declare AUTO_CONFIRM=false

# Global variable for bridge network configuration (pipe-separated string)
declare BRIDGE_NETWORK_CONFIG=""

# Global variables for Vagrant configuration (extracted from config.rb)
declare G_NUM_INSTANCES="5"
declare G_KUBE_MASTER_INSTANCES="1"
declare G_UPM_CTL_INSTANCES="1"
declare G_VM_CPUS="6"
declare G_VM_MEMORY="8192"
declare G_KUBE_MASTER_VM_CPUS="4"
declare G_KUBE_MASTER_VM_MEMORY="4096"
declare G_UPM_CONTROL_PLANE_VM_CPUS="12"
declare G_UPM_CONTROL_PLANE_VM_MEMORY="24576"
declare G_KUBE_VERSION="1.32.7"
declare G_OS="rockylinux9"
declare G_NETWORK_PLUGIN="calico"
declare G_INSTANCE_NAME_PREFIX="k8s"
declare G_WORKER_NODES
declare G_SUBNET_SPLIT4="100"
declare G_SUBNET
declare G_NETMASK
declare G_GATEWAY
declare G_DNS_SERVER="8.8.8.8"
declare SYS_MEMORY_MB
declare SYS_CPU_CORES

# Log file configuration
LOG_FILE="${SCRIPT_DIR}/libvirt_kubespray_setup.log"

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

# Function execution timing wrapper
# Usage: time_function "function_name" [args...]
time_function() {
    local func_name="$1"
    shift

    # Simple performance monitoring display with timestamps
    local start_timestamp
    start_timestamp=$(date '+%H:%M:%S')
    echo -e "${YELLOW}⏱️  Starting: ${BOLD}$func_name${NC} ${BLUE}[$start_timestamp]${NC}"

    log_info "Starting function: $func_name"
    local start_time
    start_time=$(date +%s)

    # Execute the function
    "$func_name" "$@"
    local exit_code=$?

    local end_time
    end_time=$(date +%s)
    local duration=$((end_time - start_time))
    local end_timestamp
    end_timestamp=$(date '+%H:%M:%S')

    # Simple completion display with timestamps
    if [[ $exit_code -eq 0 ]]; then
        log_info "Function $func_name completed successfully (Duration: ${duration}s)"
        echo -e "${GREEN}✅ Completed: ${BOLD}$func_name${NC} ${BLUE}[$end_timestamp]${NC} ${MAGENTA}(${duration}s)${NC}"
    else
        log_error "Function $func_name failed (Duration: ${duration}s, Exit code: $exit_code)"
        echo -e "${RED}❌ Failed: ${BOLD}$func_name${NC} ${BLUE}[$end_timestamp]${NC} ${MAGENTA}(${duration}s, exit: $exit_code)${NC}"
    fi
    echo

    return $exit_code
}

#######################################
# Error Handling
#######################################
error_exit() {
    log_error "$1"
    exit 1
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

# Safe sudo execution function
# Usage: safe_sudo command [args...]
# Returns: exit code of the command
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
    if [[ $AUTO_CONFIRM == "true" && $force_interactive != "true" ]]; then
        echo -e "${CYAN}❓ $question${NC} ${GREEN}(auto-confirmed: yes)${NC}"
        return 0
    fi

    while true; do
        if [[ -n $default ]]; then
            printf "${CYAN}❓ %s [%s]: ${NC}" "$question" "$default"
        else
            printf "${CYAN}❓ %s (yes/no): ${NC}" "$question"
        fi
        # Improved output buffer flushing
        printf "" >&1
        read -r response

        # Use default if response is empty
        if [[ -z $response && -n $default ]]; then
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

# Unified IP address validation function
# Usage: validate_ip_address "ip_address"
# Returns: 0 if valid, 1 if invalid
validate_ip_address() {
    local ip="$1"
    local ip_regex='^([0-9]{1,3}\.){3}[0-9]{1,3}$'

    [[ $ip =~ $ip_regex ]] || return 1

    # Check each octet with proper IFS handling
    local IFS='.'
    local -a octets
    read -ra octets <<<"$ip"

    local octet
    for octet in "${octets[@]}"; do
        # Validate octet range (0-255) and ensure no leading zeros (except for "0")
        if [[ $octet -lt 0 || $octet -gt 255 ]] || [[ $octet =~ ^0[0-9] ]]; then
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

    while true; do
        printf "${CYAN}🌐 %s: ${NC}" "$prompt"
        # Improved output buffer flushing
        printf "" >&1
        read -r ip_input

        if [[ -z $ip_input ]]; then
            echo -e "${RED}❌ IP address cannot be empty${NC}"
            continue
        fi

        if $validation_func "$ip_input"; then
            PROMPT_RESULT="$ip_input"
            return 0
        else
            if [[ $validation_func == "validate_vm_ip_range" ]]; then
                # Special error message for VM IP range validation
                local fourth_octet
                fourth_octet=$(echo "$ip_input" | cut -d'.' -f4)
                if validate_ip_address "$ip_input"; then
                    echo -e "${RED}❌ Fourth octet ($fourth_octet) must be between 1 and $((254 - G_NUM_INSTANCES)) for VM allocation${NC}"
                    echo -e "${YELLOW}💡 Please enter an IP with fourth octet between 1 and $((254 - G_NUM_INSTANCES)) (e.g., 192.168.1.10)${NC}"
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

        if [[ -z $text_input ]]; then
            if [[ $allow_empty == "true" ]]; then
                PROMPT_RESULT="$text_input"
                return 0
            else
                echo -e "${RED}❌ Input cannot be empty${NC}"
                continue
            fi
        fi

        if [[ -n $validation_func ]]; then
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

        if [[ $user_input == "$expected_value" ]]; then
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

    # Extract the fourth octet with proper IFS handling
    local IFS='.'
    local -a octets
    local fourth_octet
    read -ra octets <<<"$ip"
    fourth_octet="${octets[3]}"

    # Check if the fourth octet is in valid range for VM allocation
    if [[ $fourth_octet -lt 1 || $fourth_octet -gt $((254 - G_NUM_INSTANCES)) ]]; then
        # Don't echo here as it interferes with prompt_ip_input return value
        return 1
    fi

    return 0
}

# Function to list and select network interface
# Usage: select_network_interface
# Returns: selected interface name via global variable SELECTED_INTERFACE
select_network_interface() {
    local interfaces=()
    local interface_data=()
    local choice
    local interface_name
    local interface_state
    local interface_mac
    local interface_ip
    local interface_speed

    log_info "Detecting available network interfaces..."

    # Get all non-bridge, non-loopback interfaces using ip -br link
    while IFS= read -r line; do
        # Parse ip -br link output: interface_name state mac_address flags
        if [[ $line =~ ^([^[:space:]]+)[[:space:]]+([^[:space:]]+)[[:space:]]+([^[:space:]]+)[[:space:]]*(.*)$ ]]; then
            interface_name="${BASH_REMATCH[1]}"
            interface_state="${BASH_REMATCH[2]}"
            interface_mac="${BASH_REMATCH[3]}"

            # Skip loopback, bridge, and virtual interfaces
            if [[ $interface_name != "lo" &&
                $interface_name != *"br"* &&
                $interface_name != *"virbr"* &&
                $interface_name != *"docker"* &&
                $interface_name != *"vnet"* &&
                $interface_name != *"veth"* ]]; then

                # Get IP address if available
                interface_ip=$(ip addr show "$interface_name" 2>/dev/null | grep -oP 'inet \K[0-9.]+' | head -1)
                if [[ -z $interface_ip ]]; then
                    interface_ip="No IP"
                fi

                # Normalize state (UNKNOWN -> DOWN for display purposes)
                if [[ $interface_state == "UNKNOWN" ]]; then
                    interface_state="DOWN"
                fi

                # Get interface speed
                if [[ -r "/sys/class/net/$interface_name/speed" ]]; then
                    local speed_value
                    speed_value=$(cat "/sys/class/net/$interface_name/speed" 2>/dev/null)
                    if [[ $speed_value =~ ^[0-9]+$ ]] && [[ $speed_value -gt 0 ]]; then
                        if [[ $speed_value -ge 1000 ]]; then
                            interface_speed="$((speed_value / 1000))Gbps"
                        else
                            interface_speed="${speed_value}Mbps"
                        fi
                    else
                        interface_speed="Unknown"
                    fi
                else
                    interface_speed="Unknown"
                fi

                interfaces+=("$interface_name")
                interface_data+=("$interface_name|$interface_ip|$interface_state|$interface_mac|$interface_speed")
            fi
        fi
    done < <(ip -br link)

    if [[ ${#interfaces[@]} -eq 0 ]]; then
        log_error "No suitable network interfaces found"
        return 1
    fi

    echo -e "\n${YELLOW}🌐 Available Network Interfaces:${NC}"
    printf "  ${CYAN}%-3s %-12s %-15s %-7s %-20s %-10s${NC}\n" "No." "Interface" "IP Address" "State" "MAC Address" "Speed"
    printf "  ${CYAN}%-3s %-12s %-15s %-7s %-20s %-10s${NC}\n" "---" "----------" "-----------" "-----" "-----------" "-----"

    for i in "${!interface_data[@]}"; do
        IFS='|' read -r name ip state mac speed <<<"${interface_data[i]}"

        # Apply color to state
        if [[ $state == "UP" ]]; then
            colored_state="${GREEN}UP${NC}\t  "
        else
            colored_state="${RED}DOWN${NC}\t  "
        fi

        printf "  ${CYAN}%-3s${NC} %-12s %-15s %b %-20s %-10s\n" "$((i + 1))." "$name" "$ip" "$colored_state" "$mac" "$speed"
    done
    echo

    while true; do
        printf "${CYAN}🔗 Select network interface (1-%s): ${NC}" "${#interfaces[@]}"
        read -r choice

        if [[ $choice =~ ^[0-9]+$ ]] && [[ $choice -ge 1 ]] && [[ $choice -le ${#interfaces[@]} ]]; then
            SELECTED_INTERFACE="${interfaces[$((choice - 1))]}"
            echo -e "${GREEN}✅ Selected interface: ${CYAN}$SELECTED_INTERFACE${NC}\n"
            return 0
        else
            echo -e "${RED}❌ Invalid choice. Please enter a number between 1 and ${#interfaces[@]}${NC}"
        fi
    done
}

# Function to interactively configure bridge network settings
# Usage: configure_bridge_network_interactive
# Sets global variables: BRIDGE_INTERFACE, subnet, netmask, gateway, dns_server, subnet_split4
# Bridge network configuration structure
# Returns: "bridge_interface|subnet|netmask|gateway|dns_server|subnet_split4"
configure_bridge_network_interactive() {
    log_info "Starting interactive bridge network configuration..."
    echo
    echo -e "${YELLOW}🌐 Bridge Network Configuration${NC}"
    echo -e "${WHITE}Please provide the network configuration for bridge network:${NC}"
    echo

    # Step 1: Select bridge interface
    if ! select_network_interface; then
        error_exit "Failed to select network interface"
    fi
    local bridge_interface="$SELECTED_INTERFACE"

    # Step 1.5: Bridge configuration confirmation
    log_info "Validating bridge interface configuration..."

    # Get and validate current IP address of the interface for warning
    local current_ip
    current_ip=$(ip addr show "$bridge_interface" 2>/dev/null | grep 'inet ' | grep -v '127.0.0.1' | awk '{print $2}' | cut -d'/' -f1 | head -1 | tr -d '[:space:]')

    # Debug: log the current IP for troubleshooting
    log_info "Detected IP address for interface '$bridge_interface': '${current_ip:-<empty>}'"

    # Interactive confirmation for bridge setup
    if [[ -n $current_ip ]]; then
        echo -e "\n${RED}⚠️  Bridge Configuration Warning${NC}"
        echo -e "${YELLOW}🔧 Interface:${NC} ${WHITE}$bridge_interface${NC}"
        echo -e "${YELLOW}🌐 Current IP:${NC} ${WHITE}$current_ip${NC}"
        echo -e "${RED}⚠️  WARNING:${NC} Configuring bridge will remove this IP address and may disconnect existing connections!\n"

        if ! prompt_yes_no "Continue with bridge configuration?" "" true; then
            echo -e "\n${YELLOW}⏸️  Bridge configuration cancelled by user.${NC}\n"
            exit 0
        fi

        # Second confirmation: require user to input the current IP address
        echo -e "\n${RED}🔐 Second Confirmation Required${NC}"
        echo -e "${YELLOW}🔒 Security Check:${NC} To proceed with bridge configuration"
        echo -e "${WHITE}   Please enter the current IP address of '$bridge_interface'${NC}"
        echo -e "${RED}⚠️  This confirms you understand that IP '$current_ip' will be permanently removed${NC}\n"

        if ! prompt_confirmation_with_retry "Enter current IP address to confirm deletion" "$current_ip" 3; then
            echo -e "\n${RED}🚫 Bridge configuration cancelled for safety.${NC}\n"
            exit 0
        fi
        echo -e "\n${GREEN}✅ IP address confirmed. Proceeding with bridge configuration...${NC}\n"
    fi

    # Step 2: Get starting IP with CIDR notation
    local starting_ip_cidr
    local starting_ip
    local cidr_prefix

    while true; do
        read -r -p "$(echo -e "${WHITE}Enter starting IP for VM allocation with CIDR (e.g., 192.168.1.90/24): ${NC}")" starting_ip_cidr

        # Validate CIDR format
        if [[ $starting_ip_cidr =~ ^([0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3}\.[0-9]{1,3})/([0-9]{1,2})$ ]]; then
            starting_ip="${BASH_REMATCH[1]}"
            cidr_prefix="${BASH_REMATCH[2]}"

            # Validate IP address
            if validate_vm_ip_range "$starting_ip"; then
                # Validate CIDR prefix (8-30 for practical use)
                if [[ $cidr_prefix -ge 8 && $cidr_prefix -le 30 ]]; then
                    break
                else
                    echo -e "${RED}Invalid CIDR prefix. Please use a value between 8 and 30.${NC}"
                fi
            else
                echo -e "${RED}Invalid IP address format.${NC}"
            fi
        else
            echo -e "${RED}Invalid CIDR format. Please use format like 192.168.1.90/24${NC}"
        fi
    done

    # Extract subnet and fourth octet from IP
    local octets
    IFS='.' read -ra octets <<<"$starting_ip"
    local subnet="${octets[0]}.${octets[1]}.${octets[2]}"
    local subnet_split4="${octets[3]}"

    # Convert CIDR prefix to netmask using lookup table
    local netmask
    if [[ $cidr_prefix -ge 8 && $cidr_prefix -le 30 ]]; then
        local netmasks=(
            [8]="255.0.0.0" [9]="255.128.0.0" [10]="255.192.0.0" [11]="255.224.0.0"
            [12]="255.240.0.0" [13]="255.248.0.0" [14]="255.252.0.0" [15]="255.254.0.0"
            [16]="255.255.0.0" [17]="255.255.128.0" [18]="255.255.192.0" [19]="255.255.224.0"
            [20]="255.255.240.0" [21]="255.255.248.0" [22]="255.255.252.0" [23]="255.255.254.0"
            [24]="255.255.255.0" [25]="255.255.255.128" [26]="255.255.255.192" [27]="255.255.255.224"
            [28]="255.255.255.240" [29]="255.255.255.248" [30]="255.255.255.252"
        )
        netmask="${netmasks[$cidr_prefix]}"
    else
        log_error "Invalid CIDR prefix: $cidr_prefix. Must be between /8 and /30."
        return 1
    fi

    echo -e "${GREEN}✅ Parsed network configuration:${NC}"
    echo -e "   ${GREEN}•${NC} Starting IP: ${CYAN}$starting_ip${NC}"
    echo -e "   ${GREEN}•${NC} Subnet: ${CYAN}$subnet.0${NC}"
    echo -e "   ${GREEN}•${NC} Netmask: ${CYAN}$netmask${NC}"
    echo -e "   ${GREEN}•${NC} CIDR: ${CYAN}/$cidr_prefix${NC}\n"

    # Step 3: Get gateway using unified function
    prompt_ip_input "Enter gateway IP (e.g., $subnet.1)"
    local gateway="$PROMPT_RESULT"

    # Step 4: Get DNS server using unified function
    prompt_ip_input "Enter DNS server IP (e.g., 8.8.8.8 or $gateway)"
    local dns_server="$PROMPT_RESULT"

    # Display final configuration
    echo -e "\n${GREEN}🎯 Final Bridge Network Configuration:${NC}"
    echo -e "   ${GREEN}•${NC} Bridge Interface: ${CYAN}$bridge_interface${NC}"
    echo -e "   ${GREEN}•${NC} Subnet: ${CYAN}$subnet${NC}"
    echo -e "   ${GREEN}•${NC} Netmask: ${CYAN}$netmask${NC}"
    echo -e "   ${GREEN}•${NC} Gateway: ${CYAN}$gateway${NC}"
    echo -e "   ${GREEN}•${NC} DNS Server: ${CYAN}$dns_server${NC}"
    echo -e "   ${GREEN}•${NC} Starting IP: ${CYAN}$subnet.$subnet_split4${NC}\n"

    log_info "Bridge network configuration completed successfully"

    # Set global variables for backward compatibility
    BRIDGE_INTERFACE="$bridge_interface"

    # Set configuration as global variable instead of echo output
    BRIDGE_NETWORK_CONFIG="$bridge_interface|$subnet|$netmask|$gateway|$dns_server|$subnet_split4"
    return 0
}

install_packages() {
    local packages="$1"
    local max_retries="${2:-3}"
    local retry_delay="${3:-5}"

    log_info "Installing packages: $packages (max retries: $max_retries)"

    local package_array
    read -ra package_array <<<"$packages"

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

    # Initialize log file with proper permissions
    touch "$LOG_FILE" 2>/dev/null || {
        echo "Warning: Cannot create log file $LOG_FILE, logging to stdout only"
        LOG_FILE="/dev/stdout"
    }
    chmod 666 "$LOG_FILE" 2>/dev/null || true

    # Validate kubespray directory permissions
    if [[ -d $KUBESPRAY_DIR ]]; then
        if [[ ! -w $KUBESPRAY_DIR ]]; then
            error_exit "No write permission for kubespray directory parent: $KUBESPRAY_DIR"
        fi
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
        if command -v subscription-manager &>/dev/null; then
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
    if [[ $rhel_version != "9" ]]; then
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
    local required_disk=209715200 # 200GB in KB
    if [ "$available_disk" -lt "$required_disk" ]; then
        error_exit "Insufficient disk space. At least 200GB required, but only $((available_disk / 1024 / 1024))GB available."
    else
        log_info "Disk space check passed: $((available_disk / 1024 / 1024))GB available"
    fi

    # Check available memory (at least 32GB recommended)
    SYS_MEMORY_MB=$(free -m | awk 'NR==2{print $7}')
    local required_memory=32768 # 32GB in MB
    if [ "$SYS_MEMORY_MB" -lt "$required_memory" ]; then
        log_warn "Insufficient memory. At least 32GB recommended, but only ${SYS_MEMORY_MB}MB available. Performance may be affected."
    else
        log_info "Memory check passed: ${SYS_MEMORY_MB}MB available"
    fi

    # Check CPU cores (at least 16 cores recommended)
    SYS_CPU_CORES=$(nproc)
    local required_cores=12
    if [ "$SYS_CPU_CORES" -lt "$required_cores" ]; then
        error_exit "Insufficient CPU cores. At least 12 cores required, but only $SYS_CPU_CORES available."
    else
        log_info "CPU cores check passed: $SYS_CPU_CORES cores available"
    fi

    # Check CPU hardware virtualization extensions (Intel VT-x or AMD-V)
    log_info "Checking CPU hardware virtualization support..."
    local vt_support=false
    local cpu_flags=""
    local os_type
    os_type="$(uname -s)"

    # Only perform this check on Linux systems
    if [ "$os_type" = "Linux" ]; then
        if [ -r "/proc/cpuinfo" ]; then
            cpu_flags=$(grep -m1 "^flags" /proc/cpuinfo | cut -d: -f2 2>/dev/null || echo "")

            # Check for Intel VT-x (vmx flag)
            if echo "$cpu_flags" | grep -q "vmx"; then
                log_info "Intel VT-x (vmx) support detected"
                vt_support=true
            # Check for AMD-V (svm flag)
            elif echo "$cpu_flags" | grep -q "svm"; then
                log_info "AMD-V (svm) support detected"
                vt_support=true
            fi
        fi

        if [ "$vt_support" = false ]; then
            log_error "CPU hardware virtualization extensions not found or not enabled"
            log_error "Libvirt requires Intel VT-x or AMD-V support for hardware virtualization"
            log_error "Please check:"
            log_error "  1. CPU supports hardware virtualization (Intel VT-x or AMD-V)"
            log_error "  2. Virtualization is enabled in BIOS/UEFI settings"
            log_error "  3. Nested virtualization is enabled if running in a VM"
            error_exit "Hardware virtualization support check failed"
        else
            log_info "CPU hardware virtualization support check passed"
        fi

        # Additional check for KVM module availability (Linux only)
        if [ -e "/dev/kvm" ]; then
            log_info "KVM device (/dev/kvm) is available"
        else
            log_warn "KVM device (/dev/kvm) not found - KVM modules may not be loaded"
            log_warn "This will be addressed during libvirt installation"
        fi
    else
        error_exit "Only supported on Linux systems"
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
        sleep 3 # Wait for chronyd to start
    }

    # Check synchronization status using chronyc
    if command -v chronyc >/dev/null 2>&1; then
        local tracking_output
        tracking_output=$(chronyc tracking 2>/dev/null)
        local sync_status
        sync_status=$(echo "$tracking_output" | grep "Leap status" | awk '{print $4}')
        local time_offset
        time_offset=$(echo "$tracking_output" | grep "System time" | awk '{print $4}' | sed 's/seconds//')

        if [[ $sync_status == "Normal" ]]; then
            log_info "NTP synchronization status: Normal"
            if [[ -n $time_offset ]]; then
                local abs_offset=${time_offset#-}
                if (($(echo "$abs_offset < 5" | bc -l 2>/dev/null || echo "1"))); then
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

        if [[ $ntp_status == "active" && $sync_status_alt == "yes" ]]; then
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

# Combined network connectivity and proxy validation function
validate_network_and_proxy() {
    log_info "Validating network connectivity and proxy configuration..."

    local test_urls=("http://www.google.com" "https://github.com" "https://pypi.org")
    local connectivity_passed=false

    # Test HTTP proxy if configured
    if [[ -n $HTTP_PROXY ]]; then
        log_info "Testing HTTP proxy: $HTTP_PROXY"
        if curl -s --proxy "$HTTP_PROXY" --connect-timeout 10 "http://www.google.com" >/dev/null; then
            log_info "HTTP proxy validation passed"
            connectivity_passed=true
        else
            log_warn "HTTP proxy $HTTP_PROXY may not be working correctly"
        fi
    fi

    # Test HTTPS proxy if configured and different from HTTP proxy
    if [[ -n $HTTPS_PROXY && $HTTPS_PROXY != "$HTTP_PROXY" ]]; then
        log_info "Testing HTTPS proxy: $HTTPS_PROXY"
        if curl -s --proxy "$HTTPS_PROXY" --connect-timeout 10 "https://www.google.com" >/dev/null; then
            log_info "HTTPS proxy validation passed"
            connectivity_passed=true
        else
            log_warn "HTTPS proxy $HTTPS_PROXY may not be working correctly"
        fi
    fi

    # Test general network connectivity
    if [[ $connectivity_passed == "false" ]]; then
        log_info "Testing general network connectivity..."
        local proxy_set=""
        if [ -n "$HTTP_PROXY" ]; then
            proxy_set="$HTTP_PROXY"
        fi

        for url in "${test_urls[@]}"; do
            if curl -s --proxy "$proxy_set" --connect-timeout 10 "$url" >/dev/null; then
                log_info "Network connectivity test passed for $url"
                connectivity_passed=true
                break
            fi
        done
    fi

    if [ -n "$HTTP_PROXY" ]; then
        export http_proxy="$HTTP_PROXY"
        export https_proxy="$HTTPS_PROXY"
        export no_proxy="$NO_PROXY"
    fi

    if [[ $connectivity_passed == "false" ]]; then
        log_warn "Network connectivity test failed. Please check your internet connection and proxy settings."
        return 1
    fi

    log_info "Network connectivity and proxy validation completed successfully"
    return 0
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
install_libvirt() {
    log_info "Installing Libvirt..."

    # Install Development Tools
    if ! dnf group list --installed | grep -q "Development Tools"; then
        log_info "Installing Development Tools..."
        safe_sudo dnf groupinstall -y "Development Tools"
    else
        log_info "Development Tools are already installed"
    fi
    # Install basic packages
    log_info "Installing basic packages..."
    install_packages "$SYSTEM_PACKAGES"

    # Install Libvirt packages
    log_info "Installing Libvirt packages..."
    install_packages "$LIBVIRT_PACKAGES"

    log_info "Libvirt installation completed"
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
    local slave_name="bridge-slave-$BRIDGE_INTERFACE"

    # Create bridge connection if it doesn't exist
    if ! safe_sudo nmcli con show "$BRIDGE_NAME" &>/dev/null; then
        log_info "Creating transparent bridge $BRIDGE_NAME..."
        safe_sudo nmcli con add type bridge con-name "$BRIDGE_NAME" ifname "$BRIDGE_NAME" ||
            error_exit "Failed to create bridge connection $BRIDGE_NAME"

        # Configure bridge (disable IP, disable STP)
        safe_sudo nmcli con mod "$BRIDGE_NAME" ipv4.method disabled ipv6.method disabled ||
            error_exit "Failed to disable IP on bridge $BRIDGE_NAME"
        safe_sudo nmcli con mod "$BRIDGE_NAME" bridge.stp no ||
            log_warn "Failed to disable STP on bridge $BRIDGE_NAME"
    fi

    # Create bridge slave connection if it doesn't exist
    if ! safe_sudo nmcli con show "$slave_name" &>/dev/null; then
        log_info "Adding $BRIDGE_INTERFACE to bridge $BRIDGE_NAME..."
        safe_sudo nmcli con add type bridge-slave ifname "$BRIDGE_INTERFACE" master "$BRIDGE_NAME" con-name "$slave_name" ||
            error_exit "Failed to add $BRIDGE_INTERFACE to bridge $BRIDGE_NAME"
    fi

    # Activate connections
    if ! safe_sudo nmcli con up "$BRIDGE_NAME" || ! safe_sudo nmcli con up "$slave_name"; then
        error_exit "Failed to activate bridge connections"
    fi

    # Wait and verify bridge is ready
    sleep 2
    if ! ip link show "$BRIDGE_NAME" | grep -q "state UP"; then
        error_exit "Bridge $BRIDGE_NAME is not in UP state"
    fi
}

# Setup libvirt with bridge networking
setup_libvirt() {
    # Enable and start libvirtd service
    manage_service "libvirtd" "enable"
    manage_service "libvirtd" "start"

    # Add current user to libvirt group
    add_user_to_group "$USER" "libvirt"

    if [[ -n ${BRIDGE_INTERFACE:-} ]]; then
        log_info "Setting up Libvirt with bridge networking..."
        echo -e "${GREEN}🚀 Starting bridge configuration...${NC}"
        local network_name="bridge-network"
        local xml_file="/tmp/$network_name.xml"

        # First create the bridge connections if interface is provided
        log_info "Setting up bridge connections for network '$network_name'..."
        create_bridge_connections

        if safe_sudo virsh net-list --all | grep -q "$network_name"; then
            # Ensure existing network is active and set to autostart
            safe_sudo virsh net-list | grep -q "$network_name.*active" ||
                safe_sudo virsh net-start "$network_name" 2>/dev/null
            safe_sudo virsh net-list --autostart | grep -q "$network_name" ||
                safe_sudo virsh net-autostart "$network_name" 2>/dev/null
            log_info "Bridge network '$network_name' is already configured and active"
        else
            # Create new bridge network
            log_info "Creating libvirt bridge network '$network_name'..."
            cat >"$xml_file" <<EOF
<network>
  <name>$network_name</name>
  <forward mode='bridge'/>
  <bridge name='$BRIDGE_NAME'/>
</network>
EOF

            if ! safe_sudo virsh net-define "$xml_file" ||
                ! safe_sudo virsh net-autostart "$network_name" ||
                ! safe_sudo virsh net-start "$network_name"; then
                error_exit "Failed to create bridge network '$network_name'"
            fi

            rm -f "$xml_file"
            log_info "Bridge network '$network_name' created and configured"
        fi
    else
        log_info "Skipping bridge network configuration (BRIDGE_INTERFACE not set)"
    fi

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
        if ! pyenv update; then
            log_error "Failed to update pyenv"
            exit 1
        fi
        if ! pyenv install "$PYTHON_VERSION"; then
            log_error "Failed to install Python $PYTHON_VERSION using pyenv"
            exit 1
        fi
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
        log_error "  NO_PROXY: ${NO_PROXY:-Not set}"
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
    "/bin/zsh" | */usr/bin/zsh)
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
        } >>"$shell_rc"
    else
        log_info "pyenv environment variables already configured in $shell_rc"
    fi

    # Export variables for current script
    export PYENV_ROOT="$HOME/.pyenv"
    export PATH="$PYENV_ROOT/bin:$PATH"
    eval "$(pyenv init --path)"
}

setup_virtual_environment() {
    local venv_name="kubespray-venv"

    log_info "Setting up Python virtual environment using pyenv..."

    # Change to kubespray directory
    cd "$KUBESPRAY_DIR" || {
        log_error "Failed to change directory to $KUBESPRAY_DIR"
        exit 1
    }

    # Check if virtual environment already exists
    if pyenv versions --bare | grep -q "$venv_name"; then
        log_info "Virtual environment '$venv_name' already exists"
    else
        log_info "Creating virtual environment: $venv_name"
        if ! pyenv virtualenv "$PYTHON_VERSION" "$venv_name"; then
            log_error "Failed to create virtual environment '$venv_name' with Python $PYTHON_VERSION"
            exit 1
        fi
        log_info "Virtual environment '$venv_name' created successfully"
    fi

    # Set local Python version to use the virtual environment
    log_info "Activating virtual environment..."
    if ! pyenv local "$venv_name"; then
        log_error "Failed to set local Python environment to '$venv_name'"
        exit 1
    fi

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

# Function to configure bridge network settings interactively

configure_vagrant_config() {
    log_info "Configuring Vagrant config.rb ..."
    # Create vagrant directory if it doesn't exist
    if [ ! -d "$VAGRANT_CONF_DIR" ]; then
        log_info "Creating vagrant directory: $VAGRANT_CONF_DIR"
        mkdir -p "$VAGRANT_CONF_DIR"
    fi

    # Declare local variables
    local temp_file="${VAGRANT_CONF_FILE}.tmp"
    local template_file
    template_file="$KUBESPRAY_DIR/vagrant_setup_scripts/vagrant-config/${NETWORK_TYPE}_network-config.rb"

    # Copy template to config.rb
    log_info "Copying template to $VAGRANT_CONF_FILE"
    [[ -f $template_file ]] || {
        log_error "Template file not found: $template_file"
        error_exit "Template file not found"
    }
    cp "$template_file" "$VAGRANT_CONF_FILE" || {
        log_error "Failed to copy template file to $VAGRANT_CONF_FILE"
        error_exit "Template file copy failed"
    }

    # Configure bridge network settings if using bridge network
    if [[ $NETWORK_TYPE == "bridge" ]]; then
        if [[ -n $BRIDGE_NETWORK_CONFIG ]]; then
            # Parse the bridge network configuration
            IFS='|' read -r bridge_interface subnet netmask gateway dns_server subnet_split4 <<<"$BRIDGE_NETWORK_CONFIG"

            # Validate that all parameters are non-empty
            if [[ -z $bridge_interface || -z $subnet || -z $netmask ||
                -z $gateway || -z $dns_server || -z $subnet_split4 ]]; then
                log_error "All network configuration parameters must be non-empty"
                log_error "Received: bridge_interface='$bridge_interface', subnet='$subnet', netmask='$netmask'"
                log_error "          gateway='$gateway', dns_server='$dns_server', subnet_split4='$subnet_split4'"
                error_exit "Invalid bridge network configuration parameters"
            fi

            log_info "Applying bridge network settings to configuration..."
            log_info "Bridge Interface: $bridge_interface, Subnet: $subnet, Gateway: $gateway, DNS: $dns_server"

            # Apply the configuration to the config file
            awk -v subnet="$subnet" \
                -v netmask="$netmask" \
                -v gateway="$gateway" \
                -v dns_server="$dns_server" \
                -v subnet_split4="$subnet_split4" \
                -v bridge_name="$BRIDGE_NAME" \
                '{
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
            }' "$VAGRANT_CONF_FILE" >"$temp_file"

            # Replace the original file
            if mv "$temp_file" "$VAGRANT_CONF_FILE"; then
                log_info "Bridge network configuration applied successfully to $VAGRANT_CONF_FILE"
            else
                log_error "Failed to apply configuration to $VAGRANT_CONF_FILE"
                error_exit "Failed to update Vagrant configuration file"
            fi
        else
            error_exit "Bridge network configuration not available. Please run configure_bridge_network_interactive first."
        fi
    elif [[ $NETWORK_TYPE == "nat" ]]; then
        # Update DNS server configuration in config.rb
        awk -v dns_server="$DNS_SERVER" '
        {
            if ($0 ~ /^$dns_server = /) {
                print "$dns_server = \"" dns_server "\""
            } else {
                print $0
            }
        }' "$VAGRANT_CONF_FILE" >"$temp_file"

        # Replace the original file
        if mv "$temp_file" "$VAGRANT_CONF_FILE"; then
            log_info "DNS server configuration updated in config.rb"
        else
            log_error "Failed to update DNS server configuration in $VAGRANT_CONF_FILE"
            error_exit "Failed to update DNS server configuration"
        fi
    fi

    # Configure proxy settings if HTTP_PROXY is set
    if [ -n "$HTTP_PROXY" ]; then
        log_info "Configuring proxy settings in config.rb"

        # Use awk for safer text replacement
        awk -v http_proxy="$HTTP_PROXY" \
            -v https_proxy="$HTTPS_PROXY" \
            -v no_proxy="$NO_PROXY" \
            -v additional_no_proxy="$NO_PROXY" \
            '{
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
        }' "$VAGRANT_CONF_FILE" >"$temp_file"

        # Replace the original file
        if mv "$temp_file" "$VAGRANT_CONF_FILE"; then
            log_info "Proxy configuration completed:"
        else
            log_error "Failed to update proxy configuration in $VAGRANT_CONF_FILE"
            error_exit "Failed to update proxy configuration"
        fi
        log_info "  HTTP_PROXY: $HTTP_PROXY"
        log_info "  HTTPS_PROXY: $HTTPS_PROXY"
        log_info "  NO_PROXY: $NO_PROXY"
        log_info "  ADDITIONAL_NO_PROXY: $NO_PROXY"
    else
        log_info "No HTTP_PROXY set, keeping proxy settings commented out"
    fi

    # Configure VM resources based on system capacity
    log_info "Configuring VM resources based on system capacity..."
    # Set vm_cpus based on system CPU count
    if [[ $SYS_CPU_CORES -le 12 ]]; then
        G_VM_CPUS=6
    elif [[ $SYS_CPU_CORES -le 16 ]]; then
        G_VM_CPUS=8
    elif [[ $SYS_CPU_CORES -le 24 ]]; then
        G_VM_CPUS=12
    elif [[ $SYS_CPU_CORES -le 32 ]]; then
        G_VM_CPUS=16
    else
        G_VM_CPUS=24 # Default for systems with more than 32 CPUs
    fi

    # Set vm_memory based on system memory
    if [[ $SYS_MEMORY_MB -le 32768 ]]; then
        G_VM_MEMORY=8192 # 8GB
    elif [[ $SYS_MEMORY_MB -le 65536 ]]; then
        G_VM_MEMORY=16384 # 16GB
    elif [[ $SYS_MEMORY_MB -le 131072 ]]; then
        G_VM_MEMORY=32768 # 32GB
    else
        G_VM_MEMORY=49152 # 48GB
    fi

    # Add VM resource and instance count configuration to config.rb
    awk -v vm_cpus="$G_VM_CPUS" \
        -v vm_memory="$G_VM_MEMORY" \
        -v num_instances="$G_NUM_INSTANCES" \
        '{
        if ($0 ~ /^# \$vm_cpus = ""/) {
            print "$vm_cpus = " vm_cpus
        } else if ($0 ~ /^# \$vm_memory = ""/) {
            print "$vm_memory = " vm_memory
        } else if ($0 ~ /^\$num_instances = /) {
            print "$num_instances = " num_instances
        } else {
            print $0
        }
    }' "$VAGRANT_CONF_FILE" >"$temp_file"

    # Replace the original file
    if mv "$temp_file" "$VAGRANT_CONF_FILE"; then
        log_info "VM configuration added to config.rb: Instances=$G_NUM_INSTANCES, CPUs=$G_VM_CPUS, Memory=${G_VM_MEMORY}MB"
        log_info "Vagrant config.rb configuration completed: $VAGRANT_CONF_FILE"
    else
        log_error "Failed to update VM configuration in $VAGRANT_CONF_FILE"
        error_exit "Failed to update VM configuration"
    fi
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

    if [[ -f $source_vagrantfile ]]; then
        if cp "$source_vagrantfile" "$VAGRANTFILE_PATH"; then
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
# Extract Vagrant configuration variables and set as globals
#######################################
extract_vagrant_config_variables() {
    if [[ ! -f $VAGRANT_CONF_FILE ]]; then
        error_exit "Config file not found: $VAGRANT_CONF_FILE"
    fi

    log_info "Extracting Vagrant configuration variables from: $VAGRANT_CONF_FILE"

    # Extract configuration values from config.rb and set as global variables
    G_NUM_INSTANCES=$(grep "^\$num_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_KUBE_MASTER_INSTANCES=$(grep "^\$kube_master_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_UPM_CTL_INSTANCES=$(grep "^\$upm_ctl_instances\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_KUBE_MASTER_VM_CPUS=$(grep "^\$kube_master_vm_cpus\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_KUBE_MASTER_VM_MEMORY=$(grep "^\$kube_master_vm_memory\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_UPM_CONTROL_PLANE_VM_CPUS=$(grep "^\$upm_control_plane_vm_cpus\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_UPM_CONTROL_PLANE_VM_MEMORY=$(grep "^\$upm_control_plane_vm_memory\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_KUBE_VERSION=$(grep "^\$kube_version\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')
    G_OS=$(grep "^\$os\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')
    G_NETWORK_PLUGIN=$(grep "^\$network_plugin\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')
    G_INSTANCE_NAME_PREFIX=$(grep "^\$instance_name_prefix\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')
    G_SUBNET_SPLIT4=$(grep "^\$subnet_split4\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*([0-9]+).*/\1/')
    G_DNS_SERVER=$(grep "^\$dns_server\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/')

    # Extract network-specific variables based on network type
    if [[ $NETWORK_TYPE == "bridge" ]]; then
        G_SUBNET=$(grep "^\$subnet\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/' || echo "")
        G_NETMASK=$(grep "^\$netmask\s*=" "$VAGRANT_CONF_FILE" | sed -E 's/.*[[:space:]]*=[[:space:]]*"([^"]*)".*/\1/' || echo "")
    else
        G_SUBNET="192.168.200"
        G_NETMASK="255.255.255.0"
    fi

    # Calculate derived values
    G_WORKER_NODES=$((G_NUM_INSTANCES - G_KUBE_MASTER_INSTANCES - G_UPM_CTL_INSTANCES))
    if [[ $G_WORKER_NODES -lt 0 ]]; then
        G_WORKER_NODES=0
    fi

    log_info "Vagrant configuration variables extracted successfully"
    return 0
}

#######################################
# Parse and display Vagrant configuration
#######################################
parse_vagrant_config() {
    log_info "Parsing Vagrant configuration"

    # Calculate total resources
    local total_cpus
    total_cpus=$((G_WORKER_NODES * G_VM_CPUS + G_KUBE_MASTER_INSTANCES * G_KUBE_MASTER_VM_CPUS + G_UPM_CTL_INSTANCES * G_UPM_CONTROL_PLANE_VM_CPUS))
    local total_memory_mb
    total_memory_mb=$((G_WORKER_NODES * G_VM_MEMORY + G_KUBE_MASTER_INSTANCES * G_KUBE_MASTER_VM_MEMORY + G_UPM_CTL_INSTANCES * G_UPM_CONTROL_PLANE_VM_MEMORY))
    local total_memory_gb
    total_memory_gb=$((total_memory_mb / 1024))

    echo -e "\n${GREEN}🎯 Kubernetes Cluster Configuration${NC}\n"

    echo -e "${WHITE}📋 Cluster:${NC}"
    echo -e "   ${GREEN}•${NC} Kubernetes: ${CYAN}$G_KUBE_VERSION${NC}"
    echo -e "   ${GREEN}•${NC} OS: ${CYAN}$G_OS${NC}"
    echo -e "   ${GREEN}•${NC} Network Plugin: ${CYAN}$G_NETWORK_PLUGIN${NC}"
    echo -e "   ${GREEN}•${NC} Prefix: ${CYAN}$G_INSTANCE_NAME_PREFIX${NC}\n"

    echo -e "${WHITE}🖥️  Nodes:${NC}"
    echo -e "   ${GREEN}•${NC} Masters: ${WHITE}$G_KUBE_MASTER_INSTANCES${NC} × ${CYAN}${G_KUBE_MASTER_VM_CPUS}C/$((G_KUBE_MASTER_VM_MEMORY / 1024))GB${NC}"
    echo -e "   ${GREEN}•${NC} Workers: ${WHITE}$G_WORKER_NODES${NC} × ${CYAN}${G_VM_CPUS}C/$((G_VM_MEMORY / 1024))GB${NC}"
    echo -e "   ${GREEN}•${NC} UPM Control: ${WHITE}$G_UPM_CTL_INSTANCES${NC} × ${CYAN}${G_UPM_CONTROL_PLANE_VM_CPUS}C/$((G_UPM_CONTROL_PLANE_VM_MEMORY / 1024))GB${NC}\n"

    # Display network configuration
    echo -e "${WHITE}🌐 Network Configuration:${NC}"
    echo -e "   ${GREEN}•${NC} Type: ${CYAN}$NETWORK_TYPE${NC}"

    if [[ $NETWORK_TYPE == "bridge" ]]; then
        # BRIDGE_NETWORK information:
        echo -e "   ${GREEN}•${NC} Mode: ${CYAN}Bridge Network${NC}"
        echo -e "${GREEN}✅ Network configuration summary:${NC}"
        echo -e "${GREEN}   ├─ Starting IP:${NC} ${CYAN}$G_SUBNET.${G_SUBNET_SPLIT4}+${NC}"
        echo -e "${GREEN}   ├─ Netmask:${NC} ${WHITE}$G_NETMASK${NC}"
        echo -e "${GREEN}   ├─ Gateway:${NC} ${WHITE}$G_GATEWAY${NC}"
        echo -e "${GREEN}   ├─ DNS Server:${NC} ${WHITE}$G_DNS_SERVER${NC}"
        echo -e "${GREEN}   └─ Bridge Interface:${NC} ${WHITE}$BRIDGE_INTERFACE${NC}"
    else
        # NAT_NETWORK information:
        echo -e "   ${GREEN}•${NC} Mode: ${CYAN}NAT Network${NC}"
        echo -e "   ${GREEN}•${NC} Subnet: ${CYAN}192.168.200.0${NC}"
        echo -e "   ${GREEN}•${NC} Netmask: ${CYAN}255.255.255.0${NC}"
        echo -e "   ${GREEN}•${NC} DNS: ${CYAN}$G_DNS_SERVER${NC}"
    fi

    # Show VM IP address preview
    echo -e "\n${YELLOW}🖥️  Virtual Machine IP Address Preview${NC}"
    echo -e "${WHITE}The following VMs will be created with these IP addresses:${NC}\n"

    # Display VM preview
    for ((i = 1; i <= G_NUM_INSTANCES; i++)); do
        local vm_ip
        vm_ip="$G_SUBNET.$((G_SUBNET_SPLIT4 + i))"
        local vm_name="${G_INSTANCE_NAME_PREFIX}-${i}"
        if [[ $i -eq 1 ]]; then
            echo -e "${GREEN}   ├─ VM $i:${NC} ${WHITE}$vm_name${NC} → ${CYAN}$vm_ip${NC} ${YELLOW}(Master Node)${NC}"
        else
            echo -e "${GREEN}   ├─ VM $i:${NC} ${WHITE}$vm_name${NC} → ${CYAN}$vm_ip${NC} (Worker Node)"
        fi
    done
    echo -e "${GREEN}   └─ Total:${NC} ${WHITE}$G_NUM_INSTANCES VMs${NC} from ${CYAN}$G_SUBNET.$((G_SUBNET_SPLIT4 + 1))${NC} to ${CYAN}$G_SUBNET.$((G_SUBNET_SPLIT4 + G_NUM_INSTANCES))${NC}"

    echo -e "\n${WHITE}📊 Total Resources:${NC}"
    echo -e "   ${GREEN}•${NC} Nodes: ${WHITE}$G_NUM_INSTANCES${NC}"
    echo -e "   ${GREEN}•${NC} CPUs: ${RED}$total_cpus${NC} cores"
    echo -e "   ${GREEN}•${NC} Memory: ${RED}${total_memory_gb}GB${NC}\n"

    echo -e "${WHITE}⚙️  Config: ${CYAN}$VAGRANT_CONF_FILE${NC}"

    return 0
}

#######################################
# Show setup confirmation and proceed with installation
#######################################
show_setup_confirmation() {
    # Installation Preview Mode
    echo -e "\n${GREEN}🚀 Kubespray Libvirt Environment Setup${NC}\n"

    # Key Components
    echo -e "${WHITE}📦 Will Install:${NC}"
    echo -e "   ${GREEN}•${NC} Virtualization: ${CYAN}libvirt + QEMU/KVM${NC}"
    echo -e "   ${GREEN}•${NC} Container: ${CYAN}Vagrant 2.4.7 + libvirt plugin${NC}"
    echo -e "   ${GREEN}•${NC} Python: ${CYAN}pyenv + Python $PYTHON_VERSION${NC}\n"

    # Network Configuration
    echo -e "${WHITE}🌐 Network Setup:${NC}"
    if [[ -n ${BRIDGE_INTERFACE:-} ]]; then
        echo -e "   ${GREEN}•${NC} Bridge: ${CYAN}$BRIDGE_NAME (using interface: ${YELLOW}$BRIDGE_INTERFACE${NC})"
    else
        echo -e "   ${YELLOW}•${NC} Bridge: ${YELLOW}Not configured${NC}"
        echo -e "   ${GREEN}•${NC} NAT: ${CYAN}192.168.200.0/24${NC} (DHCP: Enabled)"
        echo -e "   ${GREEN}•${NC} DNS: ${CYAN}${DNS_SERVER}${NC}"
    fi

    # Proxy Configuration
    if [[ -n ${HTTP_PROXY:-} ]]; then
        echo -e "   ${GREEN}•${NC} Proxy: ${CYAN}${HTTP_PROXY}${NC}"
        if [[ -n ${HTTPS_PROXY:-} && ${HTTPS_PROXY} != "${HTTP_PROXY}" ]]; then
            echo -e "   ${GREEN}•${NC} HTTPS Proxy: ${CYAN}${HTTPS_PROXY}${NC}"
        fi
        if [[ -n ${NO_PROXY:-} ]]; then
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
}

#######################################
# Configure containerd registries and authentication
#######################################
configure_containerd_registries() {
    log_info "Configuring containerd registries and authentication..."

    local containerd_config_file
    containerd_config_file="${SCRIPT_DIR}/containerd.yml"
    local target_containerd_file="${KUBESPRAY_DIR}/inventory/sample/group_vars/all/containerd.yml"

    # Check if local containerd config file exists
    if [[ ! -f $containerd_config_file ]]; then
        log_info "Local containerd config file not found: $containerd_config_file"
        log_info "Skipping containerd registry configuration"
        return 0
    fi

    echo -e "${YELLOW}🔧 Found local containerd configuration file: ${CYAN}$containerd_config_file${NC}"

    # Check if target containerd.yml exists
    if [[ ! -f $target_containerd_file ]]; then
        log_error "Target containerd.yml not found: $target_containerd_file"
        return 1
    fi

    # Create backup of original containerd.yml
    local backup_file
    backup_file="${SCRIPT_DIR}/containerd.yml.backup.$(date +%Y%m%d_%H%M%S)"
    if cp "$target_containerd_file" "$backup_file"; then
        log_info "Created backup: $backup_file"
    else
        log_error "Failed to create backup of containerd.yml"
        return 1
    fi
    echo -e "${YELLOW}📝 Overwriting containerd configuration...${NC}"

    # Overwrite target file with local configuration
    if cp "$containerd_config_file" "$target_containerd_file"; then
        echo -e "${GREEN}✅ Successfully overwritten containerd configuration${NC}"
        log_info "Containerd configuration overwritten from $containerd_config_file to $target_containerd_file"
    else
        log_error "Failed to overwrite containerd configuration"
        return 1
    fi

    return 0
}

#######################################
# Create VM And Deploy Kubernetes Cluster
#######################################
vagrant_and_run_kubespray() {
    log_info "Starting VM creation and Kubespray deployment"
    # Use global variables extracted from config
    extract_vagrant_config_variables

    # Parse and display configuration
    if ! parse_vagrant_config; then
        log_error "Failed to parse Vagrant configuration"
        return 1
    fi

    echo -e "\n${GREEN}🚀 Ready to Deploy Kubernetes Cluster${NC}"

    # Deployment Commands
    echo -e "\n${WHITE}📋 Commands to Execute:${NC}"
    echo -e "   ${GREEN}1.${NC} ${CYAN}cd $KUBESPRAY_DIR${NC}"
    echo -e "   ${GREEN}3.${NC} ${CYAN}vagrant up --provider=libvirt --no-parallel${NC}"

    # Time Estimate
    echo -e "\n${YELLOW}⏱️  Estimated time: 20-30 minutes${NC}"

    # Confirmation prompt for deployment
    if prompt_yes_no "Continue with deployment?"; then
        echo -e "\n${GREEN}🚀 Starting Kubernetes cluster deployment...${NC}"
        echo -e "\n${YELLOW}⚙️  Starting Vagrant deployment (this may take 15-30 minutes)...${NC}\n"

        # Change to kubespray directory
        echo -e "${YELLOW}📁 Changing to kubespray directory...${NC}"
        cd "$KUBESPRAY_DIR" || error_exit "Failed to change to kubespray directory: $KUBESPRAY_DIR"

        # Configure containerd registries before deployment
        configure_containerd_registries

        # Check vagrant box existence
        # Get Box Name
        local box_name
        # Try to extract box name from Vagrantfile SUPPORTED_OS configuration
        if [[ -f ${VAGRANTFILE_PATH} ]]; then
            # Extract the SUPPORTED_OS hash and find the box name for the given OS
            local supported_os_section
            supported_os_section=$(awk '/SUPPORTED_OS\s*=\s*{/,/^}/' "${VAGRANTFILE_PATH}" 2>/dev/null)

            if [[ -n $supported_os_section ]]; then
                # Look for the OS entry and extract the box name
                # Format: "rockylinux9" => {box: "rockylinux/9", user: "vagrant"},
                box_name=$(echo "$supported_os_section" | grep "\"$G_OS\"" | grep -o 'box: "[^"]*"' | cut -d'"' -f2 | head -n1)
            else
                log_error "Could not extract box name for OS '$G_OS' from $VAGRANTFILE_PATH"
                error_exit "Failed to extract box name"
            fi
        else
            log_error "Could not find $VAGRANTFILE_PATH"
            error_exit "Failed to find Vagrant configuration file"
        fi

        # Check vagrant box existence with retry mechanism
        echo -e "${YELLOW}🔍 Checking for Vagrant box...${NC}"
        if ! vagrant box list | grep -q "$box_name"; then
            echo -e "${RED}❌ Box not found.${NC}"
            echo -e "${YELLOW}🔄 Adding box with retry mechanism...${NC}"

            local max_retries=3
            local retry_delay=30
            local attempt=1
            local success=false

            while [[ $attempt -le $max_retries ]]; do
                echo -e "${YELLOW}📦 Attempt $attempt/$max_retries: Adding Vagrant box '$box_name'...${NC}"

                if vagrant box add --name "$box_name" "$box_name" --provider libvirt; then
                    echo -e "${GREEN}✅ Successfully added Vagrant box '$box_name'${NC}"
                    success=true
                    break
                else
                    echo -e "${RED}❌ Failed to add Vagrant box (attempt $attempt/$max_retries)${NC}"

                    if [[ $attempt -lt $max_retries ]]; then
                        echo -e "${YELLOW}⏳ Waiting ${retry_delay}s before retry...${NC}"
                        sleep $retry_delay
                        # Increase delay for next attempt (exponential backoff)
                        retry_delay=$((retry_delay * 2))
                    fi
                fi

                ((attempt++))
            done

            if [[ $success != "true" ]]; then
                echo -e "${RED}💥 Failed to add Vagrant box '$box_name' after $max_retries attempts${NC}"
                echo -e "${YELLOW}💡 Troubleshooting suggestions:${NC}"
                echo -e "   ${YELLOW}•${NC} Check internet connection"
                echo -e "   ${YELLOW}•${NC} Verify box name: $box_name"
                echo -e "   ${YELLOW}•${NC} Try manual addition: ${CYAN}vagrant box add $box_name${NC}"
                echo -e "   ${YELLOW}•${NC} Check Vagrant Cloud status: ${CYAN}https://app.vagrantup.com/bento${NC}"
                error_exit "Failed to add Vagrant box: $box_name"
            fi
        else
            echo -e "${GREEN}✅ Box found.${NC}"
        fi

        # Check for existing VMs and handle them intelligently
        echo -e "${YELLOW}🔍 Checking for existing virtual machines...${NC}"
        local vm_status
        # Check for VMs matching kubespray+k8s+<number> pattern using virsh
        vm_status=$(sudo virsh list --all 2>/dev/null | grep -E "kubespray${G_INSTANCE_NAME_PREFIX}.*[0-9]+" || true)

        if [[ -n $vm_status ]]; then
            echo -e "${YELLOW}⚠️  Found existing kubespray virtual machines:${NC}"
            echo "$vm_status"

            # Count existing VMs
            local existing_vm_count
            existing_vm_count=$(echo "$vm_status" | wc -l)

            # Calculate expected total VM count
            echo -e "\n${WHITE}VM Count Analysis:${NC}"
            echo -e "   ${GREEN}•${NC} Found VMs: ${CYAN}$existing_vm_count${NC}"
            echo -e "   ${GREEN}•${NC} Expected VMs: ${CYAN}$G_NUM_INSTANCES${NC}"

            if [[ $existing_vm_count -eq $G_NUM_INSTANCES ]]; then
                echo -e "\n${GREEN}✅ VM count matches expected configuration!${NC}"
                echo -e "${WHITE}You have the following options:${NC}"
                echo -e "   ${GREEN}1.${NC} Keep existing VMs and run ${CYAN}vagrant up${NC} (recommended for updates)"
                echo -e "   ${GREEN}2.${NC} Keep existing VMs and run ${CYAN}vagrant provision${NC} (re-provision only)"
                echo -e "   ${RED}3.${NC} Delete all VMs and create fresh ones"
                echo -e "   ${YELLOW}4.${NC} Cancel deployment\n"

                local choice
                while true; do
                    read -r -p "Please select an option (1-4): " choice
                    case $choice in
                    1)
                        echo -e "${GREEN}✅ Proceeding with existing VMs using vagrant up...${NC}"
                        break
                        ;;
                    2)
                        echo -e "${GREEN}✅ Proceeding with existing VMs using vagrant provision...${NC}"
                        if vagrant provision --provision-with ansible; then
                            echo -e "\n${GREEN}🎉 Provisioning Completed Successfully!${NC}\n"
                            # Configure kubectl for local access
                            echo -e "${YELLOW}🔧 Configuring kubectl for local access...${NC}"
                            if configure_kubectl_access; then
                                echo -e "${GREEN}✅ kubectl configured successfully${NC}\n"
                                display_cluster_info
                            else
                                echo -e "${YELLOW}⚠️  kubectl configuration failed or artifacts not found${NC}\n"
                                error_exit "kubectl configuration failed or artifacts not found"
                            fi
                            return 0
                        else
                            echo -e "\n${RED}❌ Provisioning failed! Check logs above.${NC}\n"
                            return 1
                        fi
                        ;;
                    3)
                        echo -e "${RED}🗑️  Deleting existing VMs...${NC}"
                        # Extract VM names and delete them
                        local vm_names
                        vm_names=$(echo "$vm_status" | awk '{print $2}' | grep -E "kubespray${G_INSTANCE_NAME_PREFIX}.*[0-9]+")

                        local cleanup_success=true
                        while IFS= read -r vm_name; do
                            if [[ -n $vm_name ]]; then
                                echo -e "${YELLOW}  Destroying VM: $vm_name${NC}"

                                # First try to destroy (shutdown) the VM if it's running
                                if sudo virsh destroy "$vm_name" 2>/dev/null; then
                                    echo -e "${GREEN}    ✓ VM $vm_name destroyed${NC}"
                                else
                                    echo -e "${YELLOW}    ⚠ VM $vm_name was not running${NC}"
                                fi

                                # Then undefine (remove) the VM completely
                                if sudo virsh undefine "$vm_name" --remove-all-storage 2>/dev/null; then
                                    echo -e "${GREEN}    ✓ VM $vm_name undefined and storage removed${NC}"
                                else
                                    echo -e "${RED}    ✗ Failed to undefine VM $vm_name${NC}"
                                    cleanup_success=false
                                fi
                            fi
                        done <<<"$vm_names"

                        if $cleanup_success; then
                            echo -e "${GREEN}✅ VMs cleaned up successfully${NC}"
                            break
                        else
                            echo -e "${RED}❌ Failed to clean up some VMs automatically${NC}"
                            echo -e "${YELLOW}Please clean up manually and run the script again${NC}"
                            return 1
                        fi
                        ;;
                    4)
                        echo -e "${YELLOW}⏸️  Deployment cancelled by user${NC}"
                        return 0
                        ;;
                    *)
                        echo -e "${RED}❌ Invalid option. Please select 1, 2, 3, or 4.${NC}"
                        ;;
                    esac
                done
            else
                echo -e "\n${RED}❌ VM count mismatch detected!${NC}"
                echo -e "${YELLOW}The existing VMs don't match the expected configuration.${NC}"
                echo -e "${WHITE}You have the following options:${NC}"
                echo -e "   ${RED}1.${NC} Delete all existing VMs and create fresh ones"
                echo -e "   ${YELLOW}2.${NC} Cancel deployment and check configuration\n"

                local choice
                while true; do
                    read -r -p "Please select an option (1-2): " choice
                    case $choice in
                    1)
                        echo -e "${RED}🗑️  Deleting existing VMs...${NC}"
                        # Extract VM names and delete them
                        local vm_names
                        vm_names=$(echo "$vm_status" | awk '{print $2}' | grep -E "kubespray${G_INSTANCE_NAME_PREFIX}.*[0-9]+")

                        local cleanup_success=true
                        while IFS= read -r vm_name; do
                            if [[ -n $vm_name ]]; then
                                echo -e "${YELLOW}  Destroying VM: $vm_name${NC}"

                                # First try to destroy (shutdown) the VM if it's running
                                if sudo virsh destroy "$vm_name" 2>/dev/null; then
                                    echo -e "${GREEN}    ✓ VM $vm_name destroyed${NC}"
                                else
                                    echo -e "${YELLOW}    ⚠ VM $vm_name was not running${NC}"
                                fi

                                # Then undefine (remove) the VM completely
                                if sudo virsh undefine "$vm_name" --remove-all-storage 2>/dev/null; then
                                    echo -e "${GREEN}    ✓ VM $vm_name undefined and storage removed${NC}"
                                else
                                    echo -e "${RED}    ✗ Failed to undefine VM $vm_name${NC}"
                                    cleanup_success=false
                                fi
                            fi
                        done <<<"$vm_names"

                        if $cleanup_success; then
                            echo -e "${GREEN}✅ VMs cleaned up successfully${NC}"
                            break
                        else
                            echo -e "${RED}❌ Failed to clean up some VMs automatically${NC}"
                            echo -e "${YELLOW}Please clean up manually and run the script again${NC}"
                            return 1
                        fi
                        ;;
                    2)
                        echo -e "${YELLOW}⏸️  Deployment cancelled by user${NC}"
                        echo -e "${WHITE}Manual cleanup commands:${NC}"
                        echo -e "   ${CYAN}sudo virsh destroy <vm_name>${NC}     # Shutdown VM"
                        echo -e "   ${CYAN}sudo virsh undefine <vm_name> --remove-all-storage${NC}  # Remove VM"
                        echo -e "   ${CYAN}sudo virsh list --all${NC}            # Check VM status"
                        echo -e "\n${WHITE}After cleanup, run this script again.${NC}"
                        return 0
                        ;;
                    *)
                        echo -e "${RED}❌ Invalid option. Please select 1 or 2.${NC}"
                        ;;
                    esac
                done
            fi
        else
            echo -e "${GREEN}✅ No existing kubespray VMs found${NC}"
        fi

        if vagrant up --provider=libvirt --no-parallel; then
            echo -e "\n${GREEN}🎉 Deployment Completed Successfully!${NC}\n"
            # Configure kubectl for local access
            echo -e "${YELLOW}🔧 Configuring kubectl for local access...${NC}"
            if configure_kubectl_access; then
                echo -e "${GREEN}✅ kubectl configured successfully${NC}\n"

                display_cluster_info
            else
                echo -e "${YELLOW}⚠️  kubectl configuration failed or artifacts not found${NC}\n"
                error_exit "kubectl configuration failed or artifacts not found"
            fi

            echo -e "${WHITE}🎯 Cluster Access Options:${NC}"
            echo -e "   ${GREEN}Local:${NC}"
            echo -e "     ${GREEN}•${NC} ${CYAN}kubectl get nodes${NC} (if configured above)"
            echo -e "     ${GREEN}•${NC} ${CYAN}kubectl get pods --all-namespaces${NC}\n"

            echo -e "${WHITE}⚙️  Management:${NC}"
            echo -e "   ${RED}•${NC} Stop: ${CYAN}vagrant halt${NC}"
            echo -e "   ${GREEN}•${NC} Start: ${CYAN}vagrant up${NC}"
            echo -e "   ${YELLOW}•${NC} Destroy: ${CYAN}sudo virsh destroy <vm_name> && sudo virsh undefine <vm_name> --remove-all-storage${NC}\n"
        else
            echo -e "\n${RED}❌ Deployment failed! Check logs above.${NC}\n"
            echo -e "${YELLOW}🔄 Retry: ${CYAN}cd $KUBESPRAY_DIR && vagrant up --provider=libvirt --no-parallel${NC}\n"
            return 1
        fi
    else
        echo -e "\n${YELLOW}⏸️  Deployment cancelled.${NC}\n"
        echo -e "${WHITE}📝 Config: ${CYAN}$VAGRANT_CONF_FILE${NC}\n"
        echo -e "${WHITE}🚀 Manual deploy: ${CYAN}cd $KUBESPRAY_DIR && vagrant up --provider=libvirt --no-parallel${NC}\n"
        return 0
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
    local success_count=0
    local total_steps=2

    # Check if artifacts directory exists
    if [[ ! -d $artifacts_dir ]]; then
        log_warn "Artifacts directory not found: $artifacts_dir"
        return 1
    fi

    # Create directories
    mkdir -p "$LOCAL_BIN_DIR" "$KUBE_DIR" || {
        log_error "Failed to create directories"
        return 1
    }

    # Copy kubectl binary
    if [[ -f $kubectl_binary ]]; then
        if cp "$kubectl_binary" "$LOCAL_BIN_DIR/kubectl" && chmod +x "$LOCAL_BIN_DIR/kubectl"; then
            ((success_count++))
            log_info "kubectl binary configured successfully"
        else
            log_error "Failed to configure kubectl binary"
        fi
    else
        log_warn "kubectl binary not found: $kubectl_binary"
    fi

    # Copy kubeconfig file
    if [[ -f $kubeconfig_file ]]; then
        if cp "$kubeconfig_file" "$KUBECONFIG" && chmod 600 "$KUBECONFIG"; then
            ((success_count++))
            log_info "kubeconfig configured successfully"
        else
            log_error "Failed to configure kubeconfig"
        fi
    else
        log_warn "kubeconfig file not found: $kubeconfig_file"
        return 1
    fi

    # Verify and test configuration
    if [[ $success_count -eq $total_steps ]]; then
        log_info "kubectl configuration completed successfully ($success_count/$total_steps steps)"

        # Test kubectl connection
        if [[ -x $KUBECTL && -f $KUBECONFIG ]]; then
            log_info "Testing kubectl connection..."
            for attempt in {1..4}; do
                log_info "Attempt $attempt/4: Testing kubectl connection..."
                if timeout 10 "$KUBECTL" --kubeconfig="$KUBECONFIG" cluster-info &>/dev/null; then
                    log_info "kubectl connection test successful"
                    break
                elif [[ $attempt -eq 4 ]]; then
                    log_warn "kubectl connection test failed after 4 attempts - cluster may still be starting"
                else
                    log_info "Connection failed, waiting 30s before retry..."
                    sleep 30
                fi
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

    echo -e "\n${GREEN}🎯 Kubernetes Cluster Information${NC}\n"

    # Display cluster info
    echo -e "${WHITE}📊 Cluster Status:${NC}"
    if timeout 30 "$KUBECTL" cluster-info 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to connect to cluster${NC}"
        return 1
    fi

    # Display nodes
    echo -e "${WHITE}🖥️  Nodes:${NC}"
    if timeout 30 "$KUBECTL" get nodes -o wide 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve node information${NC}\n"
    fi

    # Display namespaces
    echo -e "${WHITE}📦 Namespaces:${NC}"
    if timeout 30 "$KUBECTL" get namespaces 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve namespace information${NC}\n"
    fi

    # Display pods in kube-system
    echo -e "${WHITE}🔧 System Pods (kube-system):${NC}"
    if timeout 30 "$KUBECTL" get pods -n kube-system 2>/dev/null; then
        echo
    else
        echo -e "   ${RED}•${NC} ${RED}Unable to retrieve pod information${NC}\n"
    fi

    # Display kubectl usage instructions
    echo -e "${WHITE}💡 kubectl Usage:${NC}"
    echo -e "   ${GREEN}•${NC} Config: ${CYAN}$KUBECONFIG${NC}"
    echo -e "   ${GREEN}•${NC} Binary: ${CYAN}$KUBECTL${NC}"
    echo -e "   ${GREEN}•${NC} Get nodes: ${CYAN}kubectl get nodes${NC}"
    echo -e "   ${GREEN}•${NC} Get pods: ${CYAN}kubectl get pods --all-namespaces${NC}"
    echo -e "   ${GREEN}•${NC} Get services: ${CYAN}kubectl get services --all-namespaces${NC}\n"

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
  Repository:  ${SCRIPT_REPOSITORY}

System Information:
  Script Path: ${BASH_SOURCE[0]}
  Working Dir: ${SCRIPT_DIR}
  Shell:       ${BASH_VERSION}
  Platform:    $(uname -s) $(uname -r) $(uname -m)
  User:        $(whoami)
  Date:        $(date '+%Y-%m-%d %H:%M:%S %Z')

Component Versions:
  Python:      ${PYTHON_VERSION}
EOF
}

#######################################
# Help Function
#######################################
show_help() {
    cat <<EOF
Kubespray Setup Script v${SCRIPT_VERSION}
Usage: $0 [OPTIONS]

OPTIONS:
  -h, --help                    Show this help message
  -v, --version                 Show version information
  -y                            Auto-confirm all yes/no prompts (except network bridge configuration)
  -c <count>                    Set number of virtual machines (default: 5)
  -n <network_type>             Set network type (nat|bridge, default: nat)
                                When set to 'bridge', interactive configuration will be required

DESCRIPTION:
  This script sets up a complete Kubespray Kubernetes cluster environment with libvirt
  virtualization for RHEL-based distributions. It provides automated installation and
  configuration of all necessary components including libvirt, Vagrant, Python environment,
  and Kubespray project setup.

  SUDO SESSION MANAGEMENT:
  The script automatically manages sudo sessions for long-running operations.
  You will be prompted for your sudo password once at the beginning, and the
  session will be maintained throughout the entire execution with automatic
  background refresh every 4 minutes. If you encounter sudo timeout errors:
  
  1. Ensure you have sudo privileges: sudo -v
  2. Check sudo timeout settings: sudo -l | grep timestamp_timeout
  3. For very long operations, consider running: sudo visudo
     and adding: Defaults timestamp_timeout=60
  
  The script uses a 60-second timeout for password input to prevent hanging.

REQUIREMENTS:
  - Operating System: RHEL/Rocky/AlmaLinux 8/9 (x86_64 architecture)
  - Hardware: CPU 12+ cores, Memory 32GB+, Storage 200GB+ available
  - Access: sudo privileges required, Internet connectivity essential
  - Network: Virtualization support (Intel VT-x/AMD-V) enabled in BIOS

EOF
}

#######################################
# Parse Command Line Arguments
#######################################
parse_arguments() {
    # Process arguments in a single pass
    while [[ $# -gt 0 ]]; do
        case $1 in
        -h | --help)
            show_help
            exit 0
            ;;
        -v | --version)
            show_version
            exit 0
            ;;
        -y)
            AUTO_CONFIRM=true
            shift
            ;;
        -c)
            [[ -z $2 || $2 == -* ]] && {
                log_error "Option -c requires a number argument (virtual machine count)"
                show_help
                exit 1
            }
            if [[ $2 -ge 3 ]] && [[ $2 -le 50 ]]; then
                G_NUM_INSTANCES="$2"
                log_info "Virtual machine count set to: $G_NUM_INSTANCES"
                shift 2
            else
                log_error "Invalid virtual machine count: $2. Must be a positive integer between 3 and 50"
                exit 1
            fi
            ;;
        -n)
            [[ -z $2 || $2 == -* ]] && {
                log_error "Option -n requires a network type argument (nat|bridge)"
                show_help
                exit 1
            }
            case "$2" in
            nat | bridge)
                NETWORK_TYPE="$2"
                log_info "Network type set to: $NETWORK_TYPE"
                shift 2
                ;;
            *)
                log_error "Invalid network type: $2. Valid options: nat, bridge"
                exit 1
                ;;
            esac
            ;;
        *)
            log_error "Unknown argument: $1. This script is dedicated to Kubernetes cluster deployment."
            log_error "For UPM components installation, please use upm_setup.sh script."
            show_help
            exit 1
            ;;
        esac
    done
}

#######################################
# Main Function
#######################################
main() {
    # Display script version and basic info at startup
    echo -e "${CYAN}🚀 ${SCRIPT_NAME} v${SCRIPT_VERSION}${NC}"
    echo -e "${WHITE}🔗 Repository: ${SCRIPT_REPOSITORY}${NC}"

    # Variable validation
    validate_required_variables

    # Parse command line arguments
    parse_arguments "$@"

    log_info "Starting environment setup process..."

    # Network and proxy validation
    validate_network_and_proxy

    [[ -n $NETWORK_TYPE ]] || error_exit "NETWORK_TYPE is not set. Please use -n option to set it."

    # Configure bridge network if needed
    if [[ $NETWORK_TYPE == "bridge" ]]; then
        log_info "Configuring bridge network settings..."
        # Execute interactive function directly in main shell
        # The function displays interaction to user and sets BRIDGE_NETWORK_CONFIG global variable
        if ! configure_bridge_network_interactive || [[ -z $BRIDGE_NETWORK_CONFIG ]]; then
            error_exit "Failed to configure bridge network settings"
        fi
        log_info "Bridge network configuration captured: $BRIDGE_NETWORK_CONFIG"
    elif [[ $NETWORK_TYPE == "nat" ]]; then
        log_info "Configuring NAT network settings..."
        DNS_SERVER=$(grep nameserver /etc/resolv.conf | awk '{print $2}' | head -1)
        if [[ -z $DNS_SERVER ]]; then
            log_warn "No DNS server detected from system configuration. Using default: 8.8.8.8"
            DNS_SERVER="8.8.8.8"
        fi
        log_info "Detected DNS server: $DNS_SERVER"
    fi

    # System validation
    check_system_requirements
    check_ntp_synchronization
    # Pre-installation confirmation
    show_setup_confirmation
    # Installation steps with performance monitoring
    time_function configure_system_security
    time_function install_libvirt
    time_function setup_libvirt
    time_function install_vagrant
    time_function install_vagrant_libvirt_plugin
    time_function setup_python_environment
    time_function setup_kubespray_project
    # Post-installation confirmation with performance monitoring
    time_function vagrant_and_run_kubespray

    log_info "Environment setup completed successfully!"
}

#######################################
# Script Execution Entry Point
#######################################
if [[ ${BASH_SOURCE[0]} == "${0}" ]] || [[ -z ${BASH_SOURCE[*]} ]]; then
    main "$@"
fi
