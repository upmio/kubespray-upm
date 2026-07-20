#!/bin/sh
# shellcheck disable=SC2016 # Backticks below are literal Markdown delimiters.
set -eu

usage() {
	cat <<'EOF'
Usage: validate-test-environment.sh --workspace ABSOLUTE_PATH --report-dir ABSOLUTE_PATH [--node k8s-N] [--include-plan]

Run the delivery validation suite against an upmctl test environment. The
default suite is read-only. --include-plan additionally creates an immutable,
non-executable vm.start Plan and validates it; it never grants/revokes approval
or applies a Plan.

Environment:
  UPMCTL_BIN  upmctl executable to validate (default: upmctl from PATH)
  UPMCTL_TIMEOUT  timeout passed to observed-state commands (default: 2m)
EOF
}

die() {
	echo "validation error: $*" >&2
	exit 2
}

contains_control() {
	case "$1" in
	*'
'*|*'	'*) return 0 ;;
	*) return 1 ;;
	esac
}

workspace=
report_dir=
node=
include_plan=false

while [ "$#" -gt 0 ]; do
	case "$1" in
	--workspace)
		[ "$#" -ge 2 ] || die "--workspace requires a value"
		workspace=$2
		shift 2
		;;
	--report-dir)
		[ "$#" -ge 2 ] || die "--report-dir requires a value"
		report_dir=$2
		shift 2
		;;
	--node)
		[ "$#" -ge 2 ] || die "--node requires a value"
		node=$2
		shift 2
		;;
	--include-plan)
		include_plan=true
		shift
		;;
	-h|--help)
		usage
		exit 0
		;;
	*)
		die "unknown argument: $1"
		;;
	esac
done

[ -n "$workspace" ] || die "--workspace is required"
[ -n "$report_dir" ] || die "--report-dir is required"
case "$workspace" in /*) ;; *) die "--workspace must be an absolute path" ;; esac
case "$report_dir" in /*) ;; *) die "--report-dir must be an absolute path" ;; esac
contains_control "$workspace" && die "--workspace must not contain newline or tab characters"
contains_control "$report_dir" && die "--report-dir must not contain newline or tab characters"
[ -d "$workspace" ] || die "workspace is not a directory: $workspace"
[ ! -L "$workspace" ] || die "workspace must not be a symlink: $workspace"
if [ -n "$node" ]; then
	case "$node" in k8s-[1-8]) ;; *) die "--node must be one of k8s-1 through k8s-8" ;; esac
fi

report_parent=$(dirname "$report_dir")
[ -d "$report_parent" ] || die "report parent directory does not exist: $report_parent"
[ ! -L "$report_parent" ] || die "report parent directory must not be a symlink: $report_parent"
[ ! -e "$report_dir" ] && [ ! -L "$report_dir" ] || die "report directory already exists: $report_dir"

upmctl_name=${UPMCTL_BIN:-upmctl}
case "$upmctl_name" in
*/*)
		[ -f "$upmctl_name" ] && [ ! -L "$upmctl_name" ] && [ -x "$upmctl_name" ] || die "UPMCTL_BIN is not a real executable file: $upmctl_name"
		upmctl_parent=$(CDPATH='' cd -P "$(dirname "$upmctl_name")" && pwd)
		upmctl_bin=$upmctl_parent/$(basename "$upmctl_name")
		;;
*)
		upmctl_bin=$(command -v "$upmctl_name" 2>/dev/null || true)
		[ -n "$upmctl_bin" ] || die "upmctl executable was not found in PATH"
		case "$upmctl_bin" in /*) ;; *) die "resolved upmctl path is not absolute: $upmctl_bin" ;; esac
		[ -f "$upmctl_bin" ] && [ ! -L "$upmctl_bin" ] && [ -x "$upmctl_bin" ] || die "resolved upmctl is not a real executable file: $upmctl_bin"
		;;
esac

timeout=${UPMCTL_TIMEOUT:-2m}
contains_control "$timeout" && die "UPMCTL_TIMEOUT must not contain newline or tab characters"

umask 077
mkdir "$report_dir"
chmod 0700 "$report_dir"
commands_dir=$report_dir/commands
mkdir "$commands_dir"
chmod 0700 "$commands_dir"
runtime_log=$report_dir/runtime.jsonl
results=$report_dir/command-results.tsv
dependencies=$report_dir/dependencies.txt
host_info=$report_dir/host.txt
artifact_hashes=$report_dir/artifact-sha256.txt
report=$report_dir/validation-report.md

printf 'command\trequestId\texitCode\tstatus\tstdout\tstderr\n' >"$results"

sha256_file() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		return 1
	fi
}

artifact_hash=$(sha256_file "$upmctl_bin" || true)
[ -n "$artifact_hash" ] || die "sha256sum or shasum is required to identify the tested artifact"
printf '%s  %s\n' "$artifact_hash" "$upmctl_bin" >"$artifact_hashes"

started_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
host_name=$(hostname 2>/dev/null || uname -n)
{
	printf 'startedAt=%s\n' "$started_at"
	printf 'hostname=%s\n' "$host_name"
	printf 'uname=%s\n' "$(uname -a)"
	printf 'user=%s\n' "$(id -un 2>/dev/null || printf unknown)"
	printf 'uid=%s\n' "$(id -u 2>/dev/null || printf unknown)"
	printf 'workspace=%s\n' "$workspace"
	printf 'upmctl=%s\n' "$upmctl_bin"
	printf 'upmctlSha256=%s\n' "$artifact_hash"
} >"$host_info"

pass_count=0
fail_count=0
blocked_count=0
sequence=0
LAST_STATUS=
LAST_STDOUT=

record_result() {
	label=$1
	request=$2
	rc=$3
	status=$4
	stdout_file=$5
	stderr_file=$6
	printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$label" "$request" "$rc" "$status" "$stdout_file" "$stderr_file" >>"$results"
	case "$status" in
	PASS) pass_count=$((pass_count + 1)) ;;
	FAIL) fail_count=$((fail_count + 1)) ;;
	BLOCKED) blocked_count=$((blocked_count + 1)) ;;
	esac
}

run_cli() {
	label=$1
	pass_codes=$2
	blocked_codes=$3
	expected_kind=$4
	shift 4
	sequence=$((sequence + 1))
	request="validation-$(date -u '+%Y%m%dT%H%M%SZ')-$$-$sequence"
	stdout_file="commands/$label.json"
	stderr_file="commands/$label.stderr.json"
	set +e
	"$upmctl_bin" "$@" \
		--workspace "$workspace" \
		--output json \
		--request-id "$request" \
		--log-file "$runtime_log" \
		>"$report_dir/$stdout_file" 2>"$report_dir/$stderr_file"
	rc=$?
	set -e
	case ",$pass_codes," in
	*,"$rc",*) status=PASS ;;
	*)
		case ",$blocked_codes," in
		*,"$rc",*) status=BLOCKED ;;
		*) status=FAIL ;;
		esac
		;;
	esac
	if ! grep -F '"requestId": "'"$request"'"' "$report_dir/$stdout_file" "$report_dir/$stderr_file" >/dev/null 2>&1; then
		status=FAIL
	fi
	if ! grep -F '"requestId":"'"$request"'"' "$runtime_log" 2>/dev/null | grep -F '"exitCode":'"$rc" >/dev/null 2>&1; then
		status=FAIL
	fi
	if [ "$status" = PASS ] && ! grep -F '"kind": "'"$expected_kind"'"' "$report_dir/$stdout_file" >/dev/null 2>&1; then
		status=FAIL
	fi
	record_result "$label" "$request" "$rc" "$status" "$stdout_file" "$stderr_file"
	LAST_STATUS=$status
	LAST_STDOUT=$report_dir/$stdout_file
}

probe_dependency() {
	name=$1
	shift
	path=$(command -v "$name" 2>/dev/null || true)
	if [ -z "$path" ]; then
		printf '%s\tMISSING\t-\t-\n' "$name" >>"$dependencies"
		blocked_count=$((blocked_count + 1))
		return
	fi
	set +e
	version_output=$("$path" "$@" 2>&1)
	rc=$?
	set -e
	version_line=$(printf '%s\n' "$version_output" | sed -n '1p' | tr '\t' ' ')
	if [ "$rc" -eq 0 ]; then
		printf '%s\tAVAILABLE\t%s\t%s\n' "$name" "$path" "$version_line" >>"$dependencies"
	else
		printf '%s\tERROR(%s)\t%s\t%s\n' "$name" "$rc" "$path" "$version_line" >>"$dependencies"
		blocked_count=$((blocked_count + 1))
	fi
}

printf 'dependency\tstatus\tpath\tversion\n' >"$dependencies"
probe_dependency vagrant --version
probe_dependency virsh --version
probe_dependency kubectl version --client --output=json

if command -v vagrant >/dev/null 2>&1; then
	set +e
	vagrant_plugins=$(cd "$workspace" && vagrant plugin list 2>&1)
	vagrant_plugins_rc=$?
	set -e
	printf '\n[vagrant plugins exitCode=%s]\n%s\n' "$vagrant_plugins_rc" "$vagrant_plugins" >>"$dependencies"
	if [ "$vagrant_plugins_rc" -ne 0 ]; then
		blocked_count=$((blocked_count + 1))
	fi
fi
if command -v virsh >/dev/null 2>&1; then
	set +e
	virsh_uri=$(virsh uri 2>&1)
	virsh_uri_rc=$?
	set -e
	printf '\n[virsh uri exitCode=%s]\n%s\n' "$virsh_uri_rc" "$virsh_uri" >>"$dependencies"
	if [ "$virsh_uri_rc" -ne 0 ]; then
		blocked_count=$((blocked_count + 1))
	fi
fi

run_cli version 0 '' Version version
run_cli capabilities 0 '' Capabilities capabilities
run_cli context-discover 0 '3,4,6' DeploymentContext context discover
run_cli config-validate 0 '3,4,6' ConfigValidation config validate
run_cli status 0 '3,4,6' EnvironmentStatus --timeout "$timeout" status
run_cli vm-list 0 '3,4,6' VMList --timeout "$timeout" vm list
vm_list_stdout=$LAST_STDOUT

inspect_node=$node
if [ -z "$inspect_node" ] && [ -s "$vm_list_stdout" ]; then
	inspect_node=$(sed -n 's/.*"name"[[:space:]]*:[[:space:]]*"\(k8s-[1-8]\)".*/\1/p' "$vm_list_stdout" | sed -n '1p')
fi
if [ -n "$inspect_node" ]; then
	run_cli vm-inspect 0 '3,4,6' VMInspection --timeout "$timeout" vm inspect "$inspect_node"
else
	: >"$report_dir/commands/vm-inspect.json"
	printf '%s\n' 'No k8s-N machine was available in vm list output; pass --node explicitly after correcting the environment.' >"$report_dir/commands/vm-inspect.stderr.json"
	record_result vm-inspect - - BLOCKED commands/vm-inspect.json commands/vm-inspect.stderr.json
fi

plan_id=
if [ "$include_plan" = true ]; then
	plan_node=$node
	[ -n "$plan_node" ] || plan_node=$inspect_node
	if [ -z "$plan_node" ]; then
		: >"$report_dir/commands/plan-vm-start.json"
		printf '%s\n' 'Plan validation requires --node or a k8s-N machine discoverable from vm list.' >"$report_dir/commands/plan-vm-start.stderr.json"
		record_result plan-vm-start - - BLOCKED commands/plan-vm-start.json commands/plan-vm-start.stderr.json
	else
		run_cli plan-vm-start 0 '3,4,6' Plan --timeout "$timeout" plan vm start --node "$plan_node"
		plan_output=$LAST_STDOUT
		disposition=$(sed -n 's/.*"disposition"[[:space:]]*:[[:space:]]*"\([A-Z_]*\)".*/\1/p' "$plan_output" | sed -n '1p')
		plan_id=$(sed -n 's/.*"planId"[[:space:]]*:[[:space:]]*"\(plan-[0-9a-f]*\)".*/\1/p' "$plan_output" | sed -n '1p')
		plan_suffix=${plan_id#plan-}
		case "$plan_suffix" in ''|*[!0-9a-f]*) plan_id= ;; esac
		[ "${#plan_id}" -eq 69 ] || plan_id=
		if [ "$LAST_STATUS" = PASS ] && [ "$disposition" = ACTION_REQUIRED ] && [ -n "$plan_id" ]; then
			run_cli plan-get 0 '3,4,6' PlanInspection plan get "$plan_id"
			run_cli plan-validate 0 '3,4,6' PlanValidation plan validate "$plan_id"
			# Phase 2b2a intentionally returns exit 3 even when all read-only
			# checks pass because execution remains unavailable.
			run_cli preflight 3 '4,6' PreflightResult --timeout "$timeout" preflight --plan-id "$plan_id"
		else
			: >"$report_dir/commands/plan-readiness.json"
			printf 'No persisted ACTION_REQUIRED Plan was produced (disposition=%s); get/validate/preflight were not called.\n' "${disposition:-unknown}" >"$report_dir/commands/plan-readiness.stderr.json"
			record_result plan-readiness - - BLOCKED commands/plan-readiness.json commands/plan-readiness.stderr.json
		fi
	fi
fi

if [ ! -f "$runtime_log" ]; then
	: >"$runtime_log"
	chmod 0600 "$runtime_log"
	fail_count=$((fail_count + 1))
fi

if grep -E '"command":"(approval grant|approval revoke|apply)"' "$runtime_log" >/dev/null 2>&1; then
	fail_count=$((fail_count + 1))
fi

if [ "$fail_count" -gt 0 ]; then
	delivery_verdict=FAIL
elif [ "$blocked_count" -gt 0 ]; then
	delivery_verdict=BLOCKED
else
	delivery_verdict=PASS
fi

completed_at=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
{
	printf '# upmctl 测试环境验收报告\n\n'
	printf '## 交付判定\n\n'
	printf -- '- Verdict: **%s**\n' "$delivery_verdict"
	printf -- '- Pass: %s\n' "$pass_count"
	printf -- '- Fail: %s\n' "$fail_count"
	printf -- '- Blocked: %s\n' "$blocked_count"
	printf -- '- Started (UTC): `%s`\n' "$started_at"
	printf -- '- Completed (UTC): `%s`\n\n' "$completed_at"
	printf 'PASS 表示所有请求的检查均按当前CLI契约完成；BLOCKED表示依赖、环境状态或可持久化Plan不足，需修复后重新验收；FAIL表示命令、requestId、退出码或运行日志契约不符合预期。\n\n'
	printf '## 主机和制品\n\n'
	printf -- '- Host: `%s`\n' "$host_name"
	printf -- '- Workspace: `%s`\n' "$workspace"
	printf -- '- Binary: `%s`\n' "$upmctl_bin"
	printf -- '- Binary SHA-256: `%s`\n' "$artifact_hash"
	printf -- '- Inspected node: `%s`\n' "${inspect_node:-not-selected}"
	printf -- '- Plan ID: `%s`\n\n' "${plan_id:-not-generated}"
	printf '## 证据索引\n\n'
	printf -- '- `command-results.tsv`: 每个命令的requestId、退出码和PASS/FAIL/BLOCKED判定。\n'
	printf -- '- `commands/*.json`: CLI stdout envelope；`commands/*.stderr.json`: CLI stderr envelope。\n'
	printf -- '- `runtime.jsonl`: 隐私最小化运行生命周期日志。\n'
	printf -- '- `dependencies.txt`: Vagrant、libvirt、kubectl版本、Vagrant插件和libvirt URI。\n'
	printf -- '- `host.txt`: 主机、运行身份、工作区和制品摘要。\n'
	printf -- '- `artifact-sha256.txt`: 被测upmctl二进制摘要。\n\n'
	printf '本验收未读取或复制kubeconfig内容、私钥或环境变量，也未调用人工Approval、Apply或任何底层变更命令。`--include-plan`仅允许生成不可执行Plan并调用get/validate/preflight；当前Preflight预期exit 3且执行仍被阻塞。\n'
} >"$report"

evidence_hashes=$report_dir/evidence-sha256.txt
{
	for evidence in "$host_info" "$dependencies" "$results" "$runtime_log" "$report" "$commands_dir"/*; do
		[ -f "$evidence" ] || continue
		digest=$(sha256_file "$evidence")
		printf '%s  %s\n' "$digest" "${evidence#"$report_dir"/}"
	done
} >"$evidence_hashes"

printf 'upmctl test-environment validation: %s\n' "$delivery_verdict"
printf 'report: %s\n' "$report"
printf 'evidence: %s\n' "$report_dir"

case "$delivery_verdict" in
PASS) exit 0 ;;
BLOCKED) exit 3 ;;
*) exit 1 ;;
esac
