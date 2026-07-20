#!/bin/sh
set -eu

usage() {
	cat <<'EOF'
Usage: host-safe-cli-coverage.sh --workspace ABSOLUTE_PATH --report-dir ABSOLUTE_PATH [--node k8s-N]

Runs host-safe, non-TTY CLI coverage. It never calls adopt, approval grant/revoke,
plan generation, apply, or a target mutation command. UPMCTL_BIN selects the
tested executable (default: upmctl from PATH).
EOF
}

die() {
	printf 'host-safe coverage error: %s\n' "$*" >&2
	exit 2
}

workspace=
report_dir=
node=
while [ "$#" -gt 0 ]; do
	case "$1" in
	--workspace) [ "$#" -ge 2 ] || die "--workspace requires a value"; workspace=$2; shift 2 ;;
	--report-dir) [ "$#" -ge 2 ] || die "--report-dir requires a value"; report_dir=$2; shift 2 ;;
	--node) [ "$#" -ge 2 ] || die "--node requires a value"; node=$2; shift 2 ;;
	-h|--help) usage; exit 0 ;;
	*) die "unknown argument: $1" ;;
	esac
done

[ -n "$workspace" ] || die "--workspace is required"
[ -n "$report_dir" ] || die "--report-dir is required"
case "$workspace" in /*) ;; *) die "--workspace must be absolute" ;; esac
case "$report_dir" in /*) ;; *) die "--report-dir must be absolute" ;; esac
[ -d "$workspace" ] && [ ! -L "$workspace" ] || die "workspace must be a real directory"
case "$node" in ""|k8s-[1-8]) ;; *) die "--node must be k8s-1 through k8s-8" ;; esac
[ ! -e "$report_dir" ] && [ ! -L "$report_dir" ] || die "report directory already exists"
[ -d "$(dirname "$report_dir")" ] || die "report parent does not exist"

binary=${UPMCTL_BIN:-upmctl}
case "$binary" in
*/*) [ -f "$binary" ] && [ ! -L "$binary" ] && [ -x "$binary" ] || die "unsafe UPMCTL_BIN" ;;
*) binary=$(command -v "$binary" 2>/dev/null || true); [ -n "$binary" ] || die "upmctl not found" ;;
esac
command -v sha256sum >/dev/null 2>&1 || die "sha256sum is required"

umask 077
mkdir "$report_dir"
mkdir "$report_dir/commands" "$report_dir/snapshots"
results=$report_dir/results.tsv
runtime_log=$report_dir/runtime.jsonl
printf 'case\texpectedExit\tactualExit\tstatus\tstdout\tstderr\n' >"$results"
pass=0
fail=0
blocked=0
sequence=0

record() {
	label=$1 expected=$2 actual=$3 status=$4 stdout_file=$5 stderr_file=$6
	printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$label" "$expected" "$actual" "$status" "$stdout_file" "$stderr_file" >>"$results"
	case "$status" in PASS) pass=$((pass + 1)) ;; BLOCKED) blocked=$((blocked + 1)) ;; *) fail=$((fail + 1)) ;; esac
}

run_cli() {
	label=$1 expected_exit=$2 expected_token=$3
	shift 3
	sequence=$((sequence + 1))
	request=host-safe-$$-$sequence
	stdout_file=commands/$label.stdout
	stderr_file=commands/$label.stderr
	set +e
	"$binary" --request-id "$request" --log-file "$runtime_log" "$@" >"$report_dir/$stdout_file" 2>"$report_dir/$stderr_file"
	rc=$?
	set -e
	status=PASS
	[ "$rc" -eq "$expected_exit" ] || status=FAIL
	if [ -n "$expected_token" ] && ! grep -F "$expected_token" "$report_dir/$stdout_file" "$report_dir/$stderr_file" >/dev/null 2>&1; then
		status=FAIL
	fi
	if ! grep -F '"requestId":"'"$request"'"' "$runtime_log" | grep -F '"exitCode":'"$rc" >/dev/null 2>&1; then
		status=FAIL
	fi
	record "$label" "$expected_exit" "$rc" "$status" "$stdout_file" "$stderr_file"
}

snapshot_control_state() {
	destination=$1
	if [ ! -e "$workspace/.upmctl" ]; then
		printf '%s\n' ABSENT >"$destination"
		return
	fi
	find "$workspace/.upmctl" -type f -print | LC_ALL=C sort | while IFS= read -r path; do
		printf '%s  %s\n' "$(sha256sum "$path" | awk '{print $1}')" "${path#"$workspace"/}"
	done >"$destination"
}

snapshot_vagrant() {
	destination=$1
	if ! command -v vagrant >/dev/null 2>&1; then
		printf '%s\n' UNAVAILABLE >"$destination"
		return
	fi
	set +e
	(cd "$workspace" && vagrant status --machine-readable 2>&1) | cut -d, -f2- | LC_ALL=C sort >"$destination"
	rc=$?
	set -e
	printf 'exitCode=%s\n' "$rc" >>"$destination"
}

snapshot_libvirt() {
	destination=$1
	if ! command -v virsh >/dev/null 2>&1; then
		printf '%s\n' UNAVAILABLE >"$destination"
		return
	fi
	{
		virsh uri 2>&1
		virsh list --all --name 2>/dev/null | sed '/^$/d' | LC_ALL=C sort | while IFS= read -r domain; do
			printf 'domain=%s uuid=%s state=%s\n' "$domain" "$(virsh domuuid "$domain" 2>/dev/null || printf unavailable)" "$(virsh domstate "$domain" 2>/dev/null | tr '\n' ' ' || printf unavailable)"
		done
	} >"$destination"
}

snapshot_kubernetes() {
	destination=$1
	kubeconfig=
	for candidate in \
		"$workspace/inventory/sample/artifacts/admin.conf" \
		"$workspace/artifacts/admin.conf" \
		"$workspace/inventory/artifacts/admin.conf"; do
		if [ -f "$candidate" ] && [ ! -L "$candidate" ]; then kubeconfig=$candidate; break; fi
	done
	if [ -z "$kubeconfig" ] || ! command -v kubectl >/dev/null 2>&1; then
		printf '%s\n' UNAVAILABLE >"$destination"
		return
	fi
	set +e
	kubectl --kubeconfig "$kubeconfig" get nodes -o custom-columns='NAME:.metadata.name,UID:.metadata.uid,INTERNAL_IP:.status.addresses[?(@.type=="InternalIP")].address' --no-headers 2>&1 | LC_ALL=C sort >"$destination"
	rc=$?
	set -e
	printf 'exitCode=%s\n' "$rc" >>"$destination"
}

snapshot_all() {
	prefix=$1
	snapshot_control_state "$report_dir/snapshots/$prefix-control-state.sha256"
	snapshot_vagrant "$report_dir/snapshots/$prefix-vagrant.txt"
	snapshot_libvirt "$report_dir/snapshots/$prefix-libvirt.txt"
	snapshot_kubernetes "$report_dir/snapshots/$prefix-kubernetes.txt"
}

# Establish trust using only passive discovery before direct host snapshots.
run_cli context-discover 0 '"kind": "DeploymentContext"' context discover --workspace "$workspace" --output json
if ! grep -F '"trust": "MANAGED_VALID"' "$report_dir/commands/context-discover.stdout" >/dev/null; then
	printf '%s\n' 'Workspace is not MANAGED_VALID; host-safe external coverage is blocked.' >"$report_dir/checkpoints.md"
	record managed-valid 0 3 BLOCKED - commands/context-discover.stdout
	printf 'host-safe CLI coverage: PASS=%s FAIL=%s BLOCKED=%s\n' "$pass" "$fail" "$blocked"
	exit 3
fi

snapshot_all before

for topic in root environment vm plan approval; do
	case "$topic" in root) run_cli help-root 0 'upmctl - Kubespray UPM' help ;; *) run_cli "help-$topic" 0 'Usage:' help "$topic" ;; esac
done
run_cli help-long 0 'upmctl - Kubespray UPM' --help
run_cli help-short 0 'upmctl - Kubespray UPM' -h
run_cli version 0 '"kind": "Version"' version --output json
run_cli capabilities 0 '"kind": "Capabilities"' capabilities --output json
run_cli config-validate 0 '"kind": "ConfigValidation"' config validate --workspace "$workspace" --output json
run_cli status 0 '"kind": "EnvironmentStatus"' status --workspace "$workspace" --timeout 2m --output json
run_cli vm-list 0 '"kind": "VMList"' vm list --workspace "$workspace" --timeout 2m --output json
run_cli vm-status-all 0 '"kind": "VMList"' vm status --workspace "$workspace" --timeout 2m --output json

if [ -z "$node" ]; then
	node=$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\(k8s-[1-8]\)".*/\1/p' "$report_dir/commands/vm-status-all.stdout" | sed -n '1p')
fi
if [ -n "$node" ]; then
	run_cli vm-status-node 0 '"kind": "VMStatus"' vm status "$node" --workspace "$workspace" --timeout 2m --output json
	run_cli vm-inspect-node 0 '"kind": "VMInspection"' vm inspect "$node" --workspace "$workspace" --timeout 2m --output json
	run_cli plan-vm-start-noop 0 '"disposition": "NOOP"' plan vm start --node "$node" --workspace "$workspace" --timeout 2m --output json
else
	record vm-status-node 0 - BLOCKED - commands/vm-status-all.stdout
	record vm-inspect-node 0 - BLOCKED - commands/vm-status-all.stdout
	record plan-vm-start-noop 0 - BLOCKED - commands/vm-status-all.stdout
fi

run_cli approval-list 0 '"kind": "ApprovalInspectionList"' approval list --workspace "$workspace" --output json

missing_plan=plan-cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
missing_approval=approval-dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd
run_cli plan-get-missing 3 UPMCTL_PLAN_NOT_FOUND plan get "$missing_plan" --workspace "$workspace" --output json
run_cli plan-validate-missing 3 UPMCTL_PLAN_NOT_FOUND plan validate "$missing_plan" --workspace "$workspace" --output json
run_cli preflight-missing 3 UPMCTL_PLAN_NOT_FOUND preflight --plan-id "$missing_plan" --workspace "$workspace" --output json
run_cli approval-get-missing 3 UPMCTL_APPROVAL_NOT_FOUND approval get "$missing_approval" --workspace "$workspace" --output json
run_cli adopt-nontty 3 UPMCTL_HUMAN_TTY_REQUIRED environment adopt --environment-id env-host-safe-reject --workspace "$workspace" --output json
run_cli approval-grant-nontty 3 UPMCTL_HUMAN_TTY_REQUIRED approval grant --plan-id "$missing_plan" --workspace "$workspace" --output json
run_cli approval-revoke-nontty 3 UPMCTL_HUMAN_TTY_REQUIRED approval revoke "$missing_approval" --workspace "$workspace" --output json

run_cli version-extra 2 UPMCTL_USAGE version unexpected --output json
run_cli capabilities-option 2 UPMCTL_USAGE capabilities --typo --output json
run_cli status-option 2 UPMCTL_USAGE status --typo --workspace "$workspace" --output json
run_cli invalid-timeout 2 UPMCTL_USAGE status --timeout 0s --workspace "$workspace" --output json
run_cli invalid-output 2 UPMCTL_USAGE version --output yaml

plan_id=plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
run_cli apply-closed 3 UPMCTL_NOT_IMPLEMENTED apply --plan-id "$plan_id" --output json
run_cli vm-stop-closed 3 UPMCTL_NOT_IMPLEMENTED vm stop k8s-3 --output json
run_cli vm-restart-closed 3 UPMCTL_NOT_IMPLEMENTED vm restart k8s-3 --output json
run_cli vm-ssh-closed 3 UPMCTL_NOT_IMPLEMENTED vm ssh k8s-3 --output json
run_cli plan-vm-stop-closed 3 UPMCTL_NOT_IMPLEMENTED plan vm stop --node k8s-3 --output json
run_cli plan-vm-restart-closed 3 UPMCTL_NOT_IMPLEMENTED plan vm restart --node k8s-3 --output json
for action in deploy start stop restart destroy; do
	run_cli "plan-cluster-$action-closed" 3 UPMCTL_NOT_IMPLEMENTED plan cluster "$action" --output json
done
run_cli plan-node-add-closed 3 UPMCTL_NOT_IMPLEMENTED plan node add --output json
run_cli plan-node-remove-closed 3 UPMCTL_NOT_IMPLEMENTED plan node remove --node k8s-5 --output json
run_cli node-list-closed 3 UPMCTL_NOT_IMPLEMENTED node list --output json
run_cli node-status-closed 3 UPMCTL_NOT_IMPLEMENTED node status k8s-5 --output json
run_cli plan-addon-install-closed 3 UPMCTL_NOT_IMPLEMENTED plan addon install --name prometheus --output json
run_cli operation-closed 3 UPMCTL_NOT_IMPLEMENTED operation get operation-audit --output json
run_cli operation-cancel-closed 3 UPMCTL_NOT_IMPLEMENTED operation cancel operation-audit --output json
run_cli operation-resume-closed 3 UPMCTL_NOT_IMPLEMENTED operation resume operation-audit --output json
run_cli verify-closed 3 UPMCTL_NOT_IMPLEMENTED verify --output json
run_cli report-closed 3 UPMCTL_NOT_IMPLEMENTED report generate --output json

snapshot_all after
for name in control-state.sha256 vagrant.txt libvirt.txt kubernetes.txt; do
	if cmp -s "$report_dir/snapshots/before-$name" "$report_dir/snapshots/after-$name"; then
		record "snapshot-$name" identical identical PASS "snapshots/before-$name" "snapshots/after-$name"
	else
		record "snapshot-$name" identical changed FAIL "snapshots/before-$name" "snapshots/after-$name"
	fi
done

cat >"$report_dir/checkpoints.md" <<'EOF'
# Manual and stateful coverage checkpoints

- [ ] A human TTY adopted a disposable clean legacy workspace and verified the only write was `.upmctl/state.json`.
- [ ] An authorized isolated ordinary Worker was stopped outside this script, or a fixture began with one stopped Worker.
- [ ] `plan vm start` produced ACTION_REQUIRED without changing the Worker.
- [ ] `plan get`, `plan validate`, and `preflight` completed; Preflight returned a business result with exit 3 and Apply BLOCKED.
- [ ] A human TTY completed approval grant, get/list, revoke, and post-revocation get/list.
- [ ] The fixture Worker was restored and Kubernetes Ready was independently verified.

This script intentionally does not perform any of these stateful or human-only steps.
EOF

printf 'host-safe CLI coverage: PASS=%s FAIL=%s BLOCKED=%s\n' "$pass" "$fail" "$blocked"
printf 'report: %s\n' "$report_dir"
if [ "$fail" -gt 0 ]; then exit 1; fi
if [ "$blocked" -gt 0 ]; then exit 3; fi
exit 0
