#!/usr/bin/env bash

set -euo pipefail

TEST_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SETUP_SCRIPT="${TEST_DIR}/../libvirt_kubespray_setup.sh"

# shellcheck source=../libvirt_kubespray_setup.sh
source "$SETUP_SCRIPT"
trap - EXIT INT TERM
ALLOW_NON_TTY_INPUT=true

tests_run=0

run_test() {
    local name="$1"
    shift
    if "$@"; then
        printf 'ok - %s\n' "$name"
    else
        printf 'not ok - %s\n' "$name" >&2
        return 1
    fi
    tests_run=$((tests_run + 1))
}

test_interactive_vm_network_defaults_to_nat() (
    NETWORK_TYPE=""
    NETWORK_TYPE_EXPLICIT=false
    AUTO_CONFIRM=false
    configure_vm_networking_interactive <<<"" >/dev/null
    [[ $NETWORK_TYPE == "nat" ]]
)

test_interactive_vm_network_can_select_bridge() (
    NETWORK_TYPE=""
    NETWORK_TYPE_EXPLICIT=false
    AUTO_CONFIRM=false
    configure_vm_networking_interactive <<<"2" >/dev/null
    [[ $NETWORK_TYPE == "bridge" ]]
)

test_auto_confirm_uses_safe_vm_network_default() (
    NETWORK_TYPE=""
    NETWORK_TYPE_EXPLICIT=false
    AUTO_CONFIRM=true
    configure_vm_networking_interactive >/dev/null
    [[ $NETWORK_TYPE == "nat" ]]
)

test_cli_network_preset_is_preserved() (
    NETWORK_TYPE="bridge"
    NETWORK_TYPE_EXPLICIT=true
    AUTO_CONFIRM=false
    configure_vm_networking_interactive >/dev/null
    [[ $NETWORK_TYPE == "bridge" ]]
)

test_interactive_cni_defaults_to_calico() (
    NETWORK_PLUGIN_EXPLICIT=false
    AUTO_CONFIRM=false
    K8S_NETWORK_PLUGIN="calico"
    CILIUM_KUBE_PROXY_REPLACEMENT=false
    CILIUM_LOAD_BALANCER_ENABLED=false
    CILIUM_L2_INTERFACE_EXPLICIT=false
    configure_kubernetes_networking_interactive <<<"" >/dev/null
    [[ $K8S_NETWORK_PLUGIN == "calico" ]]
)

test_interactive_cilium_load_balancer_plan() (
    NETWORK_TYPE="bridge"
    G_NUM_INSTANCES=8
    G_SUBNET="192.168.29"
    G_NETMASK="255.255.240.0"
    G_GATEWAY="192.168.21.1"
    G_SUBNET_SPLIT4=50
    NETWORK_PLUGIN_EXPLICIT=false
    AUTO_CONFIRM=false
    K8S_NETWORK_PLUGIN="calico"
    CILIUM_KUBE_PROXY_REPLACEMENT=false
    CILIUM_KUBE_PROXY_REPLACEMENT_EXPLICIT=false
    CILIUM_LOAD_BALANCER_ENABLED=false
    CILIUM_LOAD_BALANCER_EXPLICIT=false
    CILIUM_LB_RANGE_EXPLICIT=false
    CILIUM_LOAD_BALANCER_START=""
    CILIUM_LOAD_BALANCER_STOP=""
    CILIUM_L2_ANNOUNCEMENT_INTERFACE="eth1"
    CILIUM_L2_INTERFACE_EXPLICIT=false

    configure_kubernetes_networking_interactive <<'EOF' >/dev/null
2
yes
yes
192.168.29.120-192.168.29.200
eth1
EOF

    [[ $K8S_NETWORK_PLUGIN == "cilium" ]]
    [[ $CILIUM_KUBE_PROXY_REPLACEMENT == "true" ]]
    [[ $CILIUM_LOAD_BALANCER_ENABLED == "true" ]]
    [[ $CILIUM_LOAD_BALANCER_START == "192.168.29.120" ]]
    [[ $CILIUM_LOAD_BALANCER_STOP == "192.168.29.200" ]]
    [[ $CILIUM_L2_ANNOUNCEMENT_INTERFACE == "eth1" ]]
)

test_lb_range_preset_enables_load_balancer() (
    NETWORK_PLUGIN_EXPLICIT=false
    K8S_NETWORK_PLUGIN="calico"
    CILIUM_LOAD_BALANCER_ENABLED=false
    CILIUM_LOAD_BALANCER_EXPLICIT=false
    CILIUM_LB_RANGE_EXPLICIT=false
    parse_arguments --cilium-lb-range 192.168.200.201-192.168.200.220 >/dev/null
    [[ $K8S_NETWORK_PLUGIN == "cilium" ]]
    [[ $CILIUM_LOAD_BALANCER_ENABLED == "true" ]]
    [[ $CILIUM_LOAD_BALANCER_EXPLICIT == "true" ]]
    [[ $CILIUM_LB_RANGE_EXPLICIT == "true" ]]
)

test_auto_confirm_cilium_lb_preset_is_complete() (
    AUTO_CONFIRM=false
    NETWORK_TYPE=""
    NETWORK_TYPE_EXPLICIT=false
    NETWORK_PLUGIN_EXPLICIT=false
    K8S_NETWORK_PLUGIN="calico"
    CILIUM_KUBE_PROXY_REPLACEMENT=false
    CILIUM_KUBE_PROXY_REPLACEMENT_EXPLICIT=false
    CILIUM_LOAD_BALANCER_ENABLED=false
    CILIUM_LOAD_BALANCER_EXPLICIT=false
    CILIUM_LB_RANGE_EXPLICIT=false
    CILIUM_L2_INTERFACE_EXPLICIT=false
    G_NUM_INSTANCES=5
    G_SUBNET_SPLIT4=100

    parse_arguments -y -n nat --cilium-lb-range 192.168.200.201-192.168.200.220 --cilium-lb-interface eth1 >/dev/null
    configure_vm_networking_interactive >/dev/null
    configure_kubernetes_networking_interactive >/dev/null

    [[ $NETWORK_TYPE == nat ]]
    [[ $K8S_NETWORK_PLUGIN == cilium ]]
    [[ $CILIUM_KUBE_PROXY_REPLACEMENT == true ]]
    [[ $CILIUM_LOAD_BALANCER_ENABLED == true ]]
)

test_locked_cluster_reuses_persisted_vm_count() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    mkdir -p "$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cat >"$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant/config.rb" <<'EOF'
$instance_name_prefix = "k8s"
$num_instances = 8
$kube_master_instances = 1
$upm_ctl_instances = 1
$vm_cpus = 6
$vm_memory = 8192
$kube_master_vm_cpus = 4
$kube_master_vm_memory = 4096
$upm_control_plane_vm_cpus = 4
$upm_control_plane_vm_memory = 4096
$vm_network = "nat"
$dns_server = "192.168.21.2"
$network_plugin = "calico"
EOF

    TEST_ROOT="$temp_root" bash <<'EOF'
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
find_existing_libvirt_cluster_vms() { printf '%s\n' 'kubespray_k8s-1'; }
G_NUM_INSTANCES=5
NUM_INSTANCES_EXPLICIT=false
NETWORK_TYPE=""
NETWORK_TYPE_EXPLICIT=false
NETWORK_PLUGIN_EXPLICIT=false
load_existing_kubernetes_network_configuration >/dev/null
[[ $G_NUM_INSTANCES == 8 ]]
[[ $G_VM_CPUS == 6 ]]
[[ $G_VM_MEMORY == 8192 ]]
[[ $NETWORK_TYPE == nat ]]
[[ $K8S_NETWORK_PLUGIN == calico ]]
EOF
)

test_stale_config_does_not_override_safe_defaults() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    mkdir -p "$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cat >"$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant/config.rb" <<'EOF'
$instance_name_prefix = "k8s"
$num_instances = 8
$vm_network = "bridge"
$bridge_host_interface = "ens4f1"
$subnet = "192.168.29"
$netmask = "255.255.240.0"
$gateway = "192.168.21.1"
$dns_server = "192.168.21.2"
$subnet_split4 = 50
$network_plugin = "cilium"
$cilium_kube_proxy_replacement = true
$cilium_load_balancer_enabled = true
EOF

    TEST_ROOT="$temp_root" bash <<'EOF'
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
find_existing_libvirt_cluster_vms() { return 1; }
AUTO_CONFIRM=true
NETWORK_TYPE=""
NETWORK_TYPE_EXPLICIT=false
NETWORK_PLUGIN_EXPLICIT=false
K8S_NETWORK_PLUGIN="calico"
CILIUM_KUBE_PROXY_REPLACEMENT=false
CILIUM_LOAD_BALANCER_ENABLED=false
load_existing_kubernetes_network_configuration >/dev/null
configure_vm_networking_interactive >/dev/null
configure_kubernetes_networking_interactive >/dev/null
[[ $NETWORK_TYPE == nat ]]
[[ $K8S_NETWORK_PLUGIN == calico ]]
[[ $CILIUM_KUBE_PROXY_REPLACEMENT == false ]]
[[ $CILIUM_LOAD_BALANCER_ENABLED == false ]]
EOF
)

test_locked_cluster_rejects_vm_count_change() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    mkdir -p "$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cat >"$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant/config.rb" <<'EOF'
$instance_name_prefix = "k8s"
$num_instances = 8
$kube_master_instances = 1
$upm_ctl_instances = 1
$vm_cpus = 6
$vm_memory = 8192
$kube_master_vm_cpus = 4
$kube_master_vm_memory = 4096
$upm_control_plane_vm_cpus = 4
$upm_control_plane_vm_memory = 4096
$vm_network = "nat"
$dns_server = "192.168.21.2"
$network_plugin = "calico"
EOF

    if TEST_ROOT="$temp_root" bash <<'EOF' >/dev/null 2>&1
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
find_existing_libvirt_cluster_vms() { printf '%s\n' 'kubespray_k8s-1'; }
G_NUM_INSTANCES=5
NUM_INSTANCES_EXPLICIT=true
load_existing_kubernetes_network_configuration
EOF
    then
        return 1
    fi
)

test_active_bridge_interface_is_detected() (
    ip() {
        case "$*" in
        "link show br0") printf '%s\n' '3: br0: <BROADCAST,MULTICAST,UP> state UP' ;;
        "-o link show master br0") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        "-o link show ens4f1") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        *) return 1 ;;
        esac
    }
    nmcli() {
        case "$*" in
        "-t -f NAME,TYPE,DEVICE connection show --active") printf '%s\n' 'br0:bridge:br0' 'bridge-slave-ens4f1:802-3-ethernet:ens4f1' ;;
        "connection show bridge-slave-ens4f1" | "connection show br0") return 0 ;;
        "-g connection.interface-name connection show bridge-slave-ens4f1") printf '%s\n' ens4f1 ;;
        "-g connection.master connection show bridge-slave-ens4f1") printf '%s\n' br0 ;;
        "-g connection.slave-type connection show bridge-slave-ens4f1") printf '%s\n' bridge ;;
        "-g connection.type connection show br0") printf '%s\n' bridge ;;
        "-g connection.interface-name connection show br0") printf '%s\n' br0 ;;
        "-g connection.uuid connection show br0") printf '%s\n' bridge-uuid ;;
        "-g ipv4.method connection show br0" | "-g ipv6.method connection show br0") printf '%s\n' disabled ;;
        *) return 1 ;;
        esac
    }

    [[ $(detect_active_bridge_host_interface) == "ens4f1" ]]
    bridge_interface_matches_active_configuration "ens4f1"
)

test_bridge_interface_drift_is_rejected() (
    ip() {
        case "$*" in
        "link show br0") printf '%s\n' '3: br0: <BROADCAST,MULTICAST,UP> state UP' ;;
        "-o link show master br0") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        "-o link show ens4f1") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        *) return 1 ;;
        esac
    }
    nmcli() {
        case "$*" in
        "-t -f NAME,TYPE,DEVICE connection show --active") printf '%s\n' 'br0:bridge:br0' 'bridge-slave-ens4f1:802-3-ethernet:ens4f1' ;;
        "connection show bridge-slave-ens4f1" | "connection show br0") return 0 ;;
        "-g connection.interface-name connection show bridge-slave-ens4f1") printf '%s\n' ens4f1 ;;
        "-g connection.master connection show bridge-slave-ens4f1") printf '%s\n' br0 ;;
        "-g connection.slave-type connection show bridge-slave-ens4f1") printf '%s\n' bridge ;;
        "-g connection.type connection show br0") printf '%s\n' bridge ;;
        "-g connection.interface-name connection show br0") printf '%s\n' br0 ;;
        "-g connection.uuid connection show br0") printf '%s\n' bridge-uuid ;;
        "-g ipv4.method connection show br0" | "-g ipv6.method connection show br0") printf '%s\n' disabled ;;
        *) return 1 ;;
        esac
    }

    ! bridge_interface_matches_active_configuration "ens5f0"
)

test_bridge_networkmanager_profile_drift_is_rejected() (
    ip() {
        case "$*" in
        "link show br0") printf '%s\n' '3: br0: <BROADCAST,MULTICAST,UP> state UP' ;;
        "-o link show master br0") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        "-o link show ens4f1") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        *) return 1 ;;
        esac
    }
    nmcli() {
        case "$*" in
        "-t -f NAME,TYPE,DEVICE connection show --active") printf '%s\n' 'br0:bridge:br0' 'bridge-slave-ens4f1:802-3-ethernet:ens4f1' ;;
        "connection show bridge-slave-ens4f1" | "connection show br0") return 0 ;;
        "-g connection.interface-name connection show bridge-slave-ens4f1") printf '%s\n' ens4f1 ;;
        "-g connection.master connection show bridge-slave-ens4f1") printf '%s\n' wrong-bridge ;;
        "-g connection.slave-type connection show bridge-slave-ens4f1") printf '%s\n' bridge ;;
        "-g connection.type connection show br0") printf '%s\n' bridge ;;
        "-g connection.interface-name connection show br0") printf '%s\n' br0 ;;
        "-g connection.uuid connection show br0") printf '%s\n' bridge-uuid ;;
        "-g ipv4.method connection show br0" | "-g ipv6.method connection show br0") printf '%s\n' disabled ;;
        *) return 1 ;;
        esac
    }

    ! bridge_interface_matches_active_configuration "ens4f1"
)

test_other_bridge_profiles_do_not_affect_br0_detection() (
    ip() {
        case "$*" in
        "link show br0") printf '%s\n' '3: br0: <BROADCAST,MULTICAST,UP> state UP' ;;
        "-o link show master br0") printf '%s\n' '4: ens4f1: <BROADCAST,MULTICAST,UP> master br0 state UP' ;;
        *) return 1 ;;
        esac
    }
    nmcli() {
        case "$*" in
        "connection show bridge-slave-ens4f1" | "connection show br0") return 0 ;;
        "-g connection.interface-name connection show bridge-slave-ens4f1") printf '%s\n' ens4f1 ;;
        "-g connection.master connection show bridge-slave-ens4f1") printf '%s\n' br0 ;;
        "-g connection.slave-type connection show bridge-slave-ens4f1") printf '%s\n' bridge ;;
        "-g connection.type connection show br0") printf '%s\n' bridge ;;
        "-g connection.interface-name connection show br0") printf '%s\n' br0 ;;
        "-g connection.uuid connection show br0") printf '%s\n' bridge-uuid ;;
        "-g ipv4.method connection show br0" | "-g ipv6.method connection show br0") printf '%s\n' disabled ;;
        "connection show bridge-slave-ens5f0") return 0 ;;
        *) return 1 ;;
        esac
    }

    [[ $(detect_active_bridge_host_interface) == "ens4f1" ]]
)

test_non_tty_guided_input_fails_clearly() (
    if (
        ALLOW_NON_TTY_INPUT=false
        NETWORK_TYPE=""
        AUTO_CONFIRM=false
        configure_vm_networking_interactive </dev/null
    ) >/dev/null 2>&1; then
        return 1
    fi
)

test_bridge_cidr_allocation_validation() (
    G_NUM_INSTANCES=8
    validate_bridge_vm_allocation "192.168.29.50" "255.255.240.0"
    ! validate_bridge_vm_allocation "192.168.1.120" "255.255.255.128" >/dev/null 2>&1

    G_NUM_INSTANCES=3
    ! validate_bridge_vm_allocation "192.168.1.1" "255.255.255.252" >/dev/null 2>&1
)

test_bridge_gateway_validation() (
    G_NUM_INSTANCES=8
    validate_bridge_gateway "192.168.21.1" "192.168.29.50" "255.255.240.0"
    ! validate_bridge_gateway "192.168.40.1" "192.168.29.50" "255.255.240.0" >/dev/null 2>&1
    ! validate_bridge_gateway "192.168.29.55" "192.168.29.50" "255.255.240.0" >/dev/null 2>&1
)

test_ip_with_leading_zero_is_rejected_cleanly() (
    ! validate_ip_address "192.168.008.1" 2>/dev/null
)

test_invalid_vm_count_is_rejected_cleanly() (
    local err_file
    err_file=$(mktemp)
    trap 'rm -f "$err_file"' EXIT
    if bash <<EOF >/dev/null 2>"$err_file"
set -e
source "$SETUP_SCRIPT"
trap - EXIT INT TERM
parse_arguments -c 3x
EOF
    then
        return 1
    fi
    ! grep -Eq 'value too great for base|syntax error' "$err_file"
)

test_invalid_bridge_connection_profile_is_rejected() (
    nmcli() {
        case "$*" in
        "connection show br0") return 0 ;;
        "-g connection.type connection show br0") printf '%s\n' ethernet ;;
        "-g connection.interface-name connection show br0") printf '%s\n' br0 ;;
        "-g ipv4.method connection show br0" | "-g ipv6.method connection show br0") printf '%s\n' disabled ;;
        *) return 1 ;;
        esac
    }

    ! networkmanager_bridge_connection_matches
)

test_cilium_lb_rejects_service_and_pod_cidrs() (
    NETWORK_TYPE="bridge"
    G_NUM_INSTANCES=3
    G_NETMASK="255.255.192.0"
    G_SUBNET_SPLIT4=100
    CILIUM_LOAD_BALANCER_ENABLED=true
    CILIUM_L2_ANNOUNCEMENT_INTERFACE="eth1"

    G_SUBNET="10.233.0"
    G_GATEWAY="10.233.0.1"
    CILIUM_LOAD_BALANCER_START="10.233.1.10"
    CILIUM_LOAD_BALANCER_STOP="10.233.1.20"
    ! validate_cilium_load_balancer_configuration >/dev/null 2>&1

    G_SUBNET="10.233.64"
    G_GATEWAY="10.233.64.1"
    CILIUM_LOAD_BALANCER_START="10.233.65.10"
    CILIUM_LOAD_BALANCER_STOP="10.233.65.20"
    ! validate_cilium_load_balancer_configuration >/dev/null 2>&1
)

test_eight_node_resource_plan_is_validated() (
    EXISTING_CLUSTER_CONFIGURATION_LOCKED=false
    G_NUM_INSTANCES=8
    G_KUBE_MASTER_INSTANCES=1
    G_UPM_CTL_INSTANCES=1
    G_VM_CPUS=6
    G_VM_MEMORY=8192
    G_KUBE_MASTER_VM_CPUS=4
    G_KUBE_MASTER_VM_MEMORY=4096
    G_UPM_CONTROL_PLANE_VM_CPUS=4
    G_UPM_CONTROL_PLANE_VM_MEMORY=4096
    SYS_CPU_CORES=24
    SYS_MEMORY_MB=106228

    validate_cluster_resource_capacity >/dev/null
    [[ $PLANNED_TOTAL_CPUS == 44 ]]
    [[ $PLANNED_TOTAL_MEMORY_MB == 57344 ]]
)

test_resource_plan_rejects_insufficient_memory() (
    error_exit() { return 1; }
    EXISTING_CLUSTER_CONFIGURATION_LOCKED=false
    G_NUM_INSTANCES=5
    SYS_CPU_CORES=24
    SYS_MEMORY_MB=32768
    ! validate_cluster_resource_capacity >/dev/null 2>&1
)

test_resource_plan_rejects_excessive_cpu() (
    error_exit() { return 1; }
    EXISTING_CLUSTER_CONFIGURATION_LOCKED=false
    G_NUM_INSTANCES=20
    SYS_CPU_CORES=12
    SYS_MEMORY_MB=524288
    ! validate_cluster_resource_capacity >/dev/null 2>&1
)

test_resource_templates_are_consistent() (
    local template
    for template in \
        "$TEST_DIR/../vagrant-config/nat_network-config.rb" \
        "$TEST_DIR/../vagrant-config/bridge_network-config.rb"; do
        grep -Eq '^\$vm_cpus = 6([[:space:]]|$)' "$template"
        grep -Eq '^\$vm_memory = 8192([[:space:]]|$)' "$template"
        grep -Eq '^\$upm_control_plane_vm_memory = 4096([[:space:]]|$)' "$template"
    done
)

test_generated_resource_profile_round_trip() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    local nested="$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant_setup_scripts/vagrant-config"
    mkdir -p "$nested"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cp "$TEST_DIR/../vagrant-config/nat_network-config.rb" "$nested/"

    TEST_ROOT="$temp_root" bash <<'EOF' >/dev/null
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
NETWORK_TYPE="nat"
DNS_SERVER="192.168.21.2"
HTTP_PROXY=""
HTTPS_PROXY=""
G_NUM_INSTANCES=8
G_VM_CPUS=6
G_VM_MEMORY=8192
configure_vagrant_config
extract_vagrant_config_variables
[[ $G_NUM_INSTANCES == 8 ]]
[[ $G_VM_CPUS == 6 ]]
[[ $G_VM_MEMORY == 8192 ]]
EOF
)

test_existing_cluster_resource_config_is_preserved() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    mkdir -p "$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cat >"$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant/config.rb" <<'EOF'
$num_instances = 8
$vm_cpus = 5
$vm_memory = 6144
EOF

    TEST_ROOT="$temp_root" bash <<'EOF' >/dev/null
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
EXISTING_CLUSTER_CONFIGURATION_LOCKED=true
before=$(sha256sum "$VAGRANT_CONF_FILE")
configure_vagrant_config
after=$(sha256sum "$VAGRANT_CONF_FILE")
[[ $before == "$after" ]]
EOF
)

test_legacy_bridge_host_interface_is_persisted() (
    local temp_root
    temp_root=$(mktemp -d)
    trap 'rm -rf "$temp_root"' EXIT
    mkdir -p "$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant"
    cp "$SETUP_SCRIPT" "$temp_root/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
    cat >"$temp_root/vagrant_setup_scripts/kubespray-upm/vagrant/config.rb" <<'EOF'
$instance_name_prefix = "k8s"
$num_instances = 5
$kube_master_instances = 1
$upm_ctl_instances = 1
$vm_cpus = 6
$vm_memory = 8192
$kube_master_vm_cpus = 4
$kube_master_vm_memory = 4096
$upm_control_plane_vm_cpus = 4
$upm_control_plane_vm_memory = 4096
$vm_network = "bridge"
$subnet = "192.168.29"
$netmask = "255.255.240.0"
$gateway = "192.168.21.1"
$dns_server = "192.168.21.2"
$subnet_split4 = 50
$network_plugin = "calico"
EOF

    TEST_ROOT="$temp_root" bash <<'EOF' >/dev/null
set -euo pipefail
source "$TEST_ROOT/vagrant_setup_scripts/libvirt_kubespray_setup.sh"
trap - EXIT INT TERM
find_existing_libvirt_cluster_vms() { printf '%s\n' 'kubespray_k8s-1'; }
detect_active_bridge_host_interface() { printf '%s\n' 'ens4f1'; }
bridge_interface_matches_active_configuration() { [[ $1 == ens4f1 ]]; }
load_existing_kubernetes_network_configuration
load_existing_kubernetes_network_configuration
[[ $(grep -Fc '$bridge_host_interface = "ens4f1"' "$VAGRANT_CONF_FILE") -eq 1 ]]
EOF
)

test_containerd_save_uses_proxy_environment() (
    local save_task
    save_task=$(awk '
        /^    - name: Download_container \| Save and compress image$/ { in_save_task = 1 }
        in_save_task && /^    - name:/ && $0 !~ /Save and compress image$/ { exit }
        in_save_task { print }
    ' "$TEST_DIR/../../roles/download/tasks/download_container.yml")
    grep -Fq 'environment: "{{ proxy_env if container_manager == '\''containerd'\'' else omit }}"' <<<"$save_task"
)

run_test "interactive VM network defaults to NAT" test_interactive_vm_network_defaults_to_nat
run_test "interactive VM network can select Bridge" test_interactive_vm_network_can_select_bridge
run_test "-y uses safe NAT default" test_auto_confirm_uses_safe_vm_network_default
run_test "CLI VM network preset is preserved" test_cli_network_preset_is_preserved
run_test "interactive CNI defaults to Calico" test_interactive_cni_defaults_to_calico
run_test "interactive Cilium LB plan is collected and validated" test_interactive_cilium_load_balancer_plan
run_test "LB range preset enables Cilium LoadBalancer" test_lb_range_preset_enables_load_balancer
run_test "-y Cilium LB presets form a complete noninteractive plan" test_auto_confirm_cilium_lb_preset_is_complete
run_test "locked cluster reuses persisted VM count" test_locked_cluster_reuses_persisted_vm_count
run_test "stale config does not override -y safe defaults" test_stale_config_does_not_override_safe_defaults
run_test "locked cluster rejects VM count changes" test_locked_cluster_rejects_vm_count_change
run_test "active Bridge host interface is detected" test_active_bridge_interface_is_detected
run_test "Bridge host-interface drift is rejected" test_bridge_interface_drift_is_rejected
run_test "Bridge NetworkManager profile drift is rejected" test_bridge_networkmanager_profile_drift_is_rejected
run_test "other Bridge profiles do not affect br0 detection" test_other_bridge_profiles_do_not_affect_br0_detection
run_test "guided input without a TTY fails clearly" test_non_tty_guided_input_fails_clearly
run_test "Bridge VM allocation respects CIDR boundaries" test_bridge_cidr_allocation_validation
run_test "Bridge gateway stays in subnet and outside VM range" test_bridge_gateway_validation
run_test "IPv4 leading zeros are rejected without arithmetic errors" test_ip_with_leading_zero_is_rejected_cleanly
run_test "invalid VM counts are rejected without arithmetic errors" test_invalid_vm_count_is_rejected_cleanly
run_test "invalid existing Bridge profiles are rejected" test_invalid_bridge_connection_profile_is_rejected
run_test "Cilium LB rejects Kubernetes Service and Pod CIDRs" test_cilium_lb_rejects_service_and_pod_cidrs
run_test "8-node resource plan fits the validated host" test_eight_node_resource_plan_is_validated
run_test "insufficient memory fails before VM creation" test_resource_plan_rejects_insufficient_memory
run_test "excessive CPU overcommit fails before VM creation" test_resource_plan_rejects_excessive_cpu
run_test "NAT and Bridge templates use the same worker resources" test_resource_templates_are_consistent
run_test "generated worker resources round-trip through config.rb" test_generated_resource_profile_round_trip
run_test "existing cluster resources are preserved" test_existing_cluster_resource_config_is_preserved
run_test "legacy Bridge host interface is persisted" test_legacy_bridge_host_interface_is_persisted
run_test "containerd image save receives proxy_env" test_containerd_save_uses_proxy_environment

printf '1..%d\n' "$tests_run"
