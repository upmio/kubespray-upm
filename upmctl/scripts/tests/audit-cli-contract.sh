#!/bin/sh
set -eu

usage() {
	printf '%s\n' 'Usage: UPMCTL_BIN=/absolute/path/to/upmctl audit-cli-contract.sh'
}

case "${1:-}" in
-h|--help)
	usage
	exit 0
	;;
"") ;;
*)
	usage >&2
	exit 2
	;;
esac

binary=${UPMCTL_BIN:-upmctl}
case "$binary" in
*/*)
	[ -f "$binary" ] && [ ! -L "$binary" ] && [ -x "$binary" ] || {
		printf 'audit-cli-contract: unsafe or non-executable UPMCTL_BIN: %s\n' "$binary" >&2
		exit 2
	}
	;;
*)
	binary=$(command -v "$binary" 2>/dev/null || true)
	[ -n "$binary" ] || {
		printf '%s\n' 'audit-cli-contract: upmctl was not found' >&2
		exit 2
	}
	;;
esac

temp=$(mktemp -d "${TMPDIR:-/tmp}/upmctl-cli-audit.XXXXXX")
trap 'rm -rf "$temp"' EXIT HUP INT TERM
results=$temp/results.tsv
printf 'case\texpectedExit\tactualExit\tstatus\tdetail\n' >"$results"
pass=0
fail=0

capture() {
	label=$1
	shift
	stdout=$temp/$label.stdout
	stderr=$temp/$label.stderr
	set +e
	"$binary" "$@" >"$stdout" 2>"$stderr"
	rc=$?
	set -e
}

record() {
	label=$1
	expected=$2
	status=$3
	detail=$4
	printf '%s\t%s\t%s\t%s\t%s\n' "$label" "$expected" "$rc" "$status" "$detail" >>"$results"
	case "$status" in
	PASS) pass=$((pass + 1)) ;;
	*) fail=$((fail + 1)) ;;
	esac
}

expect_kind() {
	label=$1
	kind=$2
	shift 2
	capture "$label" "$@"
	if [ "$rc" -eq 0 ] && grep -E '"kind"[[:space:]]*:[[:space:]]*"'"$kind"'"' "$stdout" >/dev/null; then
		record "$label" 0 PASS "kind=$kind"
	else
		record "$label" 0 FAIL "expected kind=$kind"
	fi
}

expect_error() {
	label=$1
	expected_exit=$2
	code=$3
	shift 3
	capture "$label" "$@"
	if [ "$rc" -eq "$expected_exit" ] && grep -F "$code" "$stderr" >/dev/null; then
		record "$label" "$expected_exit" PASS "code=$code"
	else
		record "$label" "$expected_exit" FAIL "expected code=$code"
	fi
}

expect_help() {
	label=$1
	shift
	capture "$label" "$@"
	if [ "$rc" -eq 0 ] && [ -s "$stdout" ] && [ ! -s "$stderr" ]; then
		record "$label" 0 PASS plain-text
	else
		record "$label" 0 FAIL "help output contract"
	fi
}

expect_help help-root help
expect_help help-long --help
expect_help help-short -h
for topic in environment vm plan approval; do
	expect_help "help-$topic" help "$topic"
done

expect_kind version-json Version version --output json --request-id audit-version
expect_kind version-jsonl Version version --output jsonl --request-id audit-version-jsonl
expect_kind capabilities-json Capabilities capabilities --output json --request-id audit-capabilities

expect_error help-unknown 2 UPMCTL_USAGE help cluster --output json
expect_error output-invalid 2 UPMCTL_USAGE version --output yaml
expect_error timeout-zero 2 UPMCTL_USAGE version --timeout 0s --output json
expect_error version-extra 2 UPMCTL_USAGE version unexpected --output json
expect_error version-option 2 UPMCTL_USAGE version --typo --output json
expect_error capabilities-extra 2 UPMCTL_USAGE capabilities unexpected --output json
expect_error capabilities-option 2 UPMCTL_USAGE capabilities --typo --output json
expect_error leading-option 2 UPMCTL_USAGE --typo capabilities --output json
expect_error duplicate-output 2 UPMCTL_USAGE version --output json --output=json
expect_error duplicate-request-id 2 UPMCTL_USAGE version --request-id one --request-id=two --output json
expect_error duplicate-timeout 2 UPMCTL_USAGE version --timeout 1s --timeout=2s --output json
expect_error empty-workspace 2 UPMCTL_USAGE context discover --workspace= --output json
expect_error empty-request-id 2 UPMCTL_USAGE version --request-id= --output json

plan_id=plan-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
expect_error apply-closed 3 UPMCTL_NOT_IMPLEMENTED apply --plan-id "$plan_id" --output json
expect_error vm-stop-closed 3 UPMCTL_NOT_IMPLEMENTED vm stop k8s-3 --output json
expect_error plan-vm-stop-closed 3 UPMCTL_NOT_IMPLEMENTED plan vm stop --node k8s-3 --output json
expect_error node-add-closed 3 UPMCTL_NOT_IMPLEMENTED node add --output json
expect_error operation-closed 3 UPMCTL_NOT_IMPLEMENTED operation get operation-audit --output json
expect_error verify-closed 3 UPMCTL_NOT_IMPLEMENTED verify --output json
expect_error report-closed 3 UPMCTL_NOT_IMPLEMENTED report generate --output json

log=$temp/runtime.jsonl
capture runtime-log version --output json --request-id audit-log --log-file "$log"
if [ "$rc" -eq 0 ] && [ "$(wc -l <"$log" | tr -d ' ')" -eq 2 ] &&
	grep -F '"command":"version"' "$log" >/dev/null &&
	grep -F '"requestId":"audit-log"' "$log" >/dev/null; then
	record runtime-log 0 PASS "two lifecycle events"
else
	record runtime-log 0 FAIL "runtime log lifecycle contract"
fi

printf 'upmctl CLI offline audit: PASS=%s FAIL=%s\n' "$pass" "$fail"
printf 'details: %s\n' "$results"
if [ "$fail" -gt 0 ]; then
	cat "$results"
	exit 1
fi
exit 0
