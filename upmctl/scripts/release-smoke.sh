#!/bin/sh
set -eu

if [ "$#" -ne 4 ]; then
	echo "usage: $0 DIST_DIR VERSION COMMIT BUILD_DATE" >&2
	exit 2
fi

dist_dir=$1
version=$2
commit=$3
build_date=$4
script_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
project_dir=$(CDPATH= cd -P "$script_dir/.." && pwd)
case "$dist_dir" in
	/*) dist_path=$dist_dir ;;
	*) dist_path=$project_dir/$dist_dir ;;
esac
dist_dir=$(CDPATH= cd -P "$dist_path" && pwd)

verify_checksums() {
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$dist_dir" && sha256sum -c SHA256SUMS)
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$dist_dir" && shasum -a 256 -c SHA256SUMS)
	else
		echo "sha256sum or shasum is required" >&2
		exit 2
	fi
}

verify_checksums

host_os=$(uname -s | tr '[:upper:]' '[:lower:]')
host_arch=$(uname -m)
[ "$host_arch" != x86_64 ] || host_arch=amd64
[ "$host_arch" != aarch64 ] || host_arch=arm64

for target in darwin_arm64 linux_amd64 linux_arm64; do
	os=${target%_*}
	arch=${target#*_}
	if [ "$os/$arch" = linux/amd64 ]; then
		validation_tier=rocky9-e2e-candidate
	else
		validation_tier=experimental-build-only
	fi
	package="upmctl_${version}_${os}_${arch}"
	archive="$dist_dir/$package.tar.gz"
	[ -f "$archive" ] || { echo "missing archive: $archive" >&2; exit 1; }

	if tar -tzf "$archive" | awk '/^\// || /(^|\/)\.\.($|\/)/ { bad=1 } END { exit bad ? 0 : 1 }'; then
		echo "unsafe archive path in $archive" >&2
		exit 1
	fi

	temp=$(mktemp -d "${TMPDIR:-/tmp}/upmctl-release-smoke.XXXXXX")
	trap 'rm -rf "$temp"' EXIT HUP INT TERM
	tar -xzf "$archive" -C "$temp"
	root="$temp/$package"
	for required in upmctl install.sh README.md LICENSE SHA256SUMS release-manifest.json \
		docs/upmctl/deployment-guide.md \
		docs/upmctl/user-guide.md \
		docs/upmctl/operations-troubleshooting.md \
		docs/upmctl/admin-operations-test-plan.md \
		docs/upmctl/admin-operations-test-cases.md \
		docs/upmctl/cli-coverage-audit.md \
		docs/upmctl/cli-coverage-matrix.yaml \
		docs/upmctl/security-negative-test-plan.md \
		docs/upmctl/test-environment-validation-report.md \
		scripts/validate-test-environment.sh \
		scripts/audit-cli-contract.sh \
		scripts/host-safe-cli-coverage.sh \
		skills/upmctl-environment/SKILL.md; do
		[ -f "$root/$required" ] || { echo "missing $required in $archive" >&2; exit 1; }
	done
	cmp "$project_dir/scripts/validate-test-environment.sh" "$root/scripts/validate-test-environment.sh" >/dev/null || {
		echo "packaged validation script differs from source in $archive" >&2
		exit 1
	}
	cmp "$project_dir/scripts/tests/audit-cli-contract.sh" "$root/scripts/audit-cli-contract.sh" >/dev/null || {
		echo "packaged CLI audit differs from source in $archive" >&2
		exit 1
	}
	cmp "$project_dir/scripts/tests/host-safe-cli-coverage.sh" "$root/scripts/host-safe-cli-coverage.sh" >/dev/null || {
		echo "packaged host-safe coverage script differs from source in $archive" >&2
		exit 1
	}
	"$root/scripts/validate-test-environment.sh" --help | grep -F 'Usage: validate-test-environment.sh' >/dev/null
	"$root/scripts/audit-cli-contract.sh" --help | grep -F 'Usage:' >/dev/null
	"$root/scripts/host-safe-cli-coverage.sh" --help | grep -F 'Usage: host-safe-cli-coverage.sh' >/dev/null
	source_skill_manifest="$temp/source-skill-files"
	archive_skill_manifest="$temp/archive-skill-files"
	(
		cd "$project_dir/skills/upmctl-environment"
		find . -type f -print | LC_ALL=C sort
	) >"$source_skill_manifest"
	(
		cd "$root/skills/upmctl-environment"
		find . -type f -print | LC_ALL=C sort
	) >"$archive_skill_manifest"
	diff -u "$source_skill_manifest" "$archive_skill_manifest" >/dev/null || {
		echo "packaged Skill directory is incomplete in $archive" >&2
		exit 1
	}
	(
		cd "$project_dir"
		go run ./scripts/releasemanifest \
			-mode verify -source "$root" \
			-version "$version" -commit "$commit" -build-date "$build_date" \
			-os "$os" -arch "$arch" -validation-tier "$validation_tier" \
			-archive "$package.tar.gz"
	)
	[ -x "$root/upmctl" ] && [ -x "$root/install.sh" ] && \
		[ -x "$root/scripts/validate-test-environment.sh" ] && \
		[ -x "$root/scripts/audit-cli-contract.sh" ] && \
		[ -x "$root/scripts/host-safe-cli-coverage.sh" ] || {
		echo "release executables have invalid mode" >&2
		exit 1
	}
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$root" && sha256sum -c SHA256SUMS >/dev/null)
	else
		(cd "$root" && shasum -a 256 -c SHA256SUMS >/dev/null)
	fi
	manifest_files="$temp/checksum-manifest-files"
	archive_files="$temp/archive-regular-files"
	awk '{ print substr($0, 67) }' "$root/SHA256SUMS" | LC_ALL=C sort >"$manifest_files"
	(
		cd "$root"
		find . -type f ! -name SHA256SUMS -print | sed 's|^\./||' | LC_ALL=C sort
	) >"$archive_files"
	diff -u "$archive_files" "$manifest_files" >/dev/null || {
		echo "internal SHA256SUMS does not cover every packaged file in $archive" >&2
		exit 1
	}

	go version -m "$root/upmctl" | grep -F "GOOS=$os" >/dev/null
	go version -m "$root/upmctl" | grep -F "GOARCH=$arch" >/dev/null

	if [ "$os" = "$host_os" ] && [ "$arch" = "$host_arch" ]; then
		version_output=$($root/upmctl version)
		printf '%s\n' "$version_output" | grep -F "Version:  $version" >/dev/null
		printf '%s\n' "$version_output" | grep -F "Commit:   $commit" >/dev/null
		printf '%s\n' "$version_output" | grep -F "Built:    $build_date" >/dev/null
		prefix="$temp/install-prefix"
		"$root/install.sh" --prefix "$prefix" >/dev/null
		"$prefix/bin/upmctl" version | grep -F "Version:  $version" >/dev/null
		[ ! -e "$prefix/docs" ] && [ ! -e "$prefix/skills" ] && [ ! -e "$prefix/scripts" ] || {
			echo "installer copied delivery support content outside bin/" >&2
			exit 1
		}
		"$root/install.sh" --prefix "$prefix" | grep -F "already installed" >/dev/null

		printf 'previous binary\n' >"$prefix/bin/upmctl"
		chmod 0755 "$prefix/bin/upmctl"
		if "$root/install.sh" --prefix "$prefix" >/dev/null 2>&1; then
			echo "installer overwrote a different binary without --replace" >&2
			exit 1
		fi
		"$root/install.sh" --prefix "$prefix" --replace >/dev/null
		set -- "$prefix/bin/upmctl.backup."*
		[ "$#" -eq 1 ] && [ -f "$1" ] || { echo "installer did not preserve exactly one backup" >&2; exit 1; }
		"$prefix/bin/upmctl" version | grep -F "Version:  $version" >/dev/null

		mkdir "$temp/real-prefix"
		ln -s "$temp/real-prefix" "$temp/symlink-prefix"
		if "$root/install.sh" --prefix "$temp/symlink-prefix" >/dev/null 2>&1; then
			echo "installer accepted a symlinked prefix" >&2
			exit 1
		fi
	fi

	rm -rf "$temp"
	trap - EXIT HUP INT TERM
done

echo "release smoke passed for upmctl $version"
