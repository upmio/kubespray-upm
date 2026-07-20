#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
repo_dir=$(CDPATH= cd -P "$script_dir/../.." && pwd)

git_value() {
	git -C "$repo_dir" "$@" 2>/dev/null || true
}

source_epoch() {
	if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
		case "$SOURCE_DATE_EPOCH" in
			*[!0-9]*|'')
				echo "SOURCE_DATE_EPOCH must be a non-negative integer" >&2
				exit 2
				;;
		esac
		printf '%s\n' "$SOURCE_DATE_EPOCH"
		return
	fi

	epoch=$(git_value log -1 --format=%ct)
	if [ -n "$epoch" ]; then
		printf '%s\n' "$epoch"
	else
		printf '0\n'
	fi
}

format_epoch() {
	epoch=$1
	if date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ' >/dev/null 2>&1; then
		date -u -d "@$epoch" '+%Y-%m-%dT%H:%M:%SZ'
	elif date -u -r "$epoch" '+%Y-%m-%dT%H:%M:%SZ' >/dev/null 2>&1; then
		date -u -r "$epoch" '+%Y-%m-%dT%H:%M:%SZ'
	else
		echo "cannot convert SOURCE_DATE_EPOCH using the installed date command" >&2
		exit 2
	fi
}

case "${1:-}" in
	version)
		tag=$(git_value describe --tags --exact-match --match 'upmctl-v*')
		if [ -n "$tag" ]; then
			printf '%s\n' "${tag#upmctl-v}"
		else
			printf '0.1.0-dev\n'
		fi
		;;
	commit)
		commit=$(git_value rev-parse HEAD)
		if [ -n "$commit" ] && [ -n "$(git_value status --porcelain -- upmctl LICENSE)" ]; then
			commit=$commit-dirty
		fi
		printf '%s\n' "${commit:-unknown}"
		;;
	epoch)
		source_epoch
		;;
	date)
		format_epoch "$(source_epoch)"
		;;
	*)
		echo "usage: $0 {version|commit|epoch|date}" >&2
		exit 2
		;;
esac
