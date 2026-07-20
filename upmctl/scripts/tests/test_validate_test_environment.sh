#!/bin/sh
set -eu

script_dir=$(CDPATH='' cd -P "$(dirname "$0")" && pwd)
validator=$script_dir/../validate-test-environment.sh

fail() {
	echo "test_validate_test_environment: $*" >&2
	exit 1
}

sh -n "$validator"

temp=$(mktemp -d "${TMPDIR:-/tmp}/upmctl-validation-test.XXXXXX")
trap 'rm -rf "$temp"' EXIT HUP INT TERM
fake_bin=$temp/bin
workspace=$temp/workspace
mkdir "$fake_bin" "$workspace"
printf 'fixture vagrantfile\n' >"$workspace/Vagrantfile"
mkdir "$workspace/vagrant" "$workspace/artifacts"
printf 'fixture config\n' >"$workspace/vagrant/config.rb"
printf 'DO-NOT-COLLECT-KUBECONFIG-CONTENT\n' >"$workspace/artifacts/admin.conf"
calls=$temp/calls.log
: >"$calls"

cat >"$fake_bin/upmctl" <<'EOF'
#!/bin/sh
set -eu
request=
log_file=
command=
separator=
while [ "$#" -gt 0 ]; do
	case "$1" in
	--request-id)
		request=$2
		shift 2
		;;
	--log-file)
		log_file=$2
		shift 2
		;;
	--workspace|--output|--timeout)
		shift 2
		;;
	--request-id=*|--log-file=*|--workspace=*|--output=*|--timeout=*)
		case "$1" in
		--request-id=*) request=${1#*=} ;;
		--log-file=*) log_file=${1#*=} ;;
		esac
		shift
		;;
	*)
		command=$command$separator$1
		separator=' '
		shift
		;;
	esac
done

printf 'upmctl:%s\n' "$command" >>"$FAKE_CALLS"
case "$command" in
version) kind=Version; data='{"version":"0.1.0-test","gitCommit":"fixture","platform":"linux/amd64"}'; canonical=version; rc=0 ;;
capabilities) kind=Capabilities; data='{"phase":"2b2a","capabilities":[]}'; canonical=capabilities; rc=0 ;;
'context discover') kind=DeploymentContext; data='{"workspace":"fixture","managed":true,"trust":"MANAGED_VALID"}'; canonical='context discover'; rc=0 ;;
'config validate') kind=ConfigValidation; data='{"validation":{"safe":true,"valid":true,"complete":true}}'; canonical='config validate'; rc=0 ;;
status)
	kind=EnvironmentStatus
	data='{"health":"READY"}'
	canonical=status
	rc=${FAKE_STATUS_EXIT:-0}
	;;
'vm list') kind=VMList; data='{"machines":[{"name":"k8s-3","health":"READY"}]}'; canonical='vm list'; rc=0 ;;
'vm inspect k8s-3') kind=VMInspection; data='{"name":"k8s-3","health":"READY"}'; canonical='vm inspect'; rc=0 ;;
'plan vm start --node k8s-3') kind=Plan; data='{"planId":"plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","disposition":"ACTION_REQUIRED","executionAvailable":false}'; canonical='plan vm start'; rc=0 ;;
'plan get plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa') kind=PlanInspection; data='{"plan":{"planId":"plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},"executionAvailable":false}'; canonical='plan get'; rc=0 ;;
'plan validate plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa') kind=PlanValidation; data='{"planId":"plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","blockers":[],"executionAvailable":false}'; canonical='plan validate'; rc=0 ;;
'preflight --plan-id plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa')
	kind=PreflightResult
	data='{"planId":"plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","preflightStatus":"PASSED","applyDecision":"BLOCKED","executionAvailable":false}'
	canonical=preflight
	rc=3
	if [ "${FAKE_PREFLIGHT_ERROR:-0}" = 1 ]; then
		kind=Error
		data='{"code":"UPMCTL_PLAN_NOT_FOUND"}'
	fi
	;;
*) kind=Error; data='{}'; canonical=unknown; rc=2 ;;
esac

printf '{"logVersion":"upmctl.runtime/v1","requestId":"%s","command":"%s","event":"start","exitCode":null,"errorCode":null}\n' "$request" "$canonical" >>"$log_file"
output_request=$request
[ "${FAKE_BAD_REQUEST:-0}" != 1 ] || output_request=fixture-wrong-request
printf '{\n  "apiVersion": "upmctl.upm.io/v1alpha1",\n  "kind": "%s",\n  "requestId": "%s",\n  "data": %s\n}\n' "$kind" "$output_request" "$data"
printf '{"logVersion":"upmctl.runtime/v1","requestId":"%s","command":"%s","event":"complete","exitCode":%s,"errorCode":null}\n' "$request" "$canonical" "$rc" >>"$log_file"
exit "$rc"
EOF
chmod 0755 "$fake_bin/upmctl"

cat >"$fake_bin/vagrant" <<'EOF'
#!/bin/sh
set -eu
printf 'vagrant:%s\n' "$*" >>"$FAKE_CALLS"
case "$*" in
'--version') echo 'Vagrant 2.4.1' ;;
'plugin list') echo 'vagrant-libvirt (0.12.2, global)' ;;
*) exit 2 ;;
esac
EOF
cat >"$fake_bin/virsh" <<'EOF'
#!/bin/sh
set -eu
printf 'virsh:%s\n' "$*" >>"$FAKE_CALLS"
case "$*" in
'--version') echo '10.5.0' ;;
'uri') echo 'qemu:///system' ;;
*) exit 2 ;;
esac
EOF
cat >"$fake_bin/kubectl" <<'EOF'
#!/bin/sh
set -eu
printf 'kubectl:%s\n' "$*" >>"$FAKE_CALLS"
[ "$*" = 'version --client --output=json' ] || exit 2
echo '{"clientVersion":{"gitVersion":"v1.31.0"}}'
EOF
chmod 0755 "$fake_bin/vagrant" "$fake_bin/virsh" "$fake_bin/kubectl"

run_validator() {
	PATH="$fake_bin:$PATH" \
		FAKE_CALLS="$calls" \
		FAKE_STATUS_EXIT="${FAKE_STATUS_EXIT:-}" \
		FAKE_BAD_REQUEST="${FAKE_BAD_REQUEST:-}" \
		FAKE_PREFLIGHT_ERROR="${FAKE_PREFLIGHT_ERROR:-}" \
		UPMCTL_BIN="$fake_bin/upmctl" \
		"$validator" "$@"
}

readonly_report=$temp/reports/readonly
mkdir "$temp/reports"
run_validator --workspace "$workspace" --report-dir "$readonly_report" >"$temp/readonly.out"
grep -F 'validation: PASS' "$temp/readonly.out" >/dev/null || fail "read-only validation did not pass"
grep -F 'Verdict: **PASS**' "$readonly_report/validation-report.md" >/dev/null || fail "read-only report verdict is missing"
grep -F 'vm-inspect' "$readonly_report/command-results.tsv" >/dev/null || fail "vm inspect evidence is missing"
if grep -F 'plan vm start' "$calls" >/dev/null; then
	fail "default validation invoked Plan generation"
fi
if grep -R -F 'DO-NOT-COLLECT-KUBECONFIG-CONTENT' "$readonly_report" >/dev/null 2>&1; then
	fail "validation report copied kubeconfig content"
fi

: >"$calls"
plan_report=$temp/reports/plan
run_validator --workspace "$workspace" --report-dir "$plan_report" --node k8s-3 --include-plan >"$temp/plan.out"
grep -F 'validation: PASS' "$temp/plan.out" >/dev/null || fail "Plan validation did not pass"
awk -F '\t' '$1 == "preflight" && $3 == "3" && $4 == "PASS" { found=1 } END { exit found ? 0 : 1 }' "$plan_report/command-results.tsv" || fail "preflight exit 3 was not accepted"
grep -F 'upmctl:plan get plan-' "$calls" >/dev/null || fail "plan get was not invoked"
grep -F 'upmctl:plan validate plan-' "$calls" >/dev/null || fail "plan validate was not invoked"
grep -F 'upmctl:preflight --plan-id plan-' "$calls" >/dev/null || fail "preflight was not invoked"
if grep -E 'upmctl:(approval grant|approval revoke|apply)|vagrant:(up|halt|destroy)|virsh:(start|shutdown|destroy)|kubectl:(apply|delete|drain)' "$calls" >/dev/null; then
	fail "validation invoked a forbidden mutating command"
fi
grep -F '"command":"preflight"' "$plan_report/runtime.jsonl" | grep -F '"exitCode":3' >/dev/null || fail "runtime log lacks preflight exit code evidence"

blocked_report=$temp/reports/blocked
set +e
FAKE_STATUS_EXIT=4 run_validator --workspace "$workspace" --report-dir "$blocked_report" --node k8s-3 >"$temp/blocked.out"
blocked_rc=$?
set -e
[ "$blocked_rc" -eq 3 ] || fail "dependency-policy outcome did not return script exit 3"
grep -F 'Verdict: **BLOCKED**' "$blocked_report/validation-report.md" >/dev/null || fail "BLOCKED report verdict is missing"
awk -F '\t' '$1 == "status" && $3 == "4" && $4 == "BLOCKED" { found=1 } END { exit found ? 0 : 1 }' "$blocked_report/command-results.tsv" || fail "CLI exit 4 was not classified as BLOCKED"

failed_report=$temp/reports/failed
set +e
FAKE_PREFLIGHT_ERROR=1 run_validator --workspace "$workspace" --report-dir "$failed_report" --node k8s-3 --include-plan >"$temp/failed.out"
failed_rc=$?
set -e
[ "$failed_rc" -eq 1 ] || fail "preflight error envelope with exit 3 did not return script exit 1"
grep -F 'Verdict: **FAIL**' "$failed_report/validation-report.md" >/dev/null || fail "FAIL report verdict is missing"
awk -F '\t' '$1 == "preflight" && $3 == "3" && $4 == "FAIL" { found=1 } END { exit found ? 0 : 1 }' "$failed_report/command-results.tsv" || fail "preflight Error kind was not classified as FAIL"

before=$(wc -l <"$calls" | tr -d ' ')
set +e
run_validator --workspace relative/workspace --report-dir "$temp/reports/invalid" >/dev/null 2>&1
relative_rc=$?
set -e
[ "$relative_rc" -eq 2 ] || fail "relative workspace was not rejected with exit 2"
after=$(wc -l <"$calls" | tr -d ' ')
[ "$before" = "$after" ] || fail "CLI was invoked after unsafe argument rejection"

echo "validate-test-environment fixture tests passed"
