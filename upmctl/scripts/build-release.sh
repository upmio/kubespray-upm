#!/bin/sh
set -eu

if [ "$#" -ne 7 ]; then
	echo "usage: $0 GOOS GOARCH VERSION COMMIT BUILD_DATE SOURCE_DATE_EPOCH DIST_DIR" >&2
	exit 2
fi

goos=$1
goarch=$2
version=$3
commit=$4
build_date=$5
source_date_epoch=$6
dist_dir=$7

case "$goos/$goarch" in
	linux/amd64) validation_tier=rocky9-e2e-candidate ;;
	darwin/arm64|linux/arm64) validation_tier=experimental-build-only ;;
	*)
		echo "unsupported release platform: $goos/$goarch" >&2
		exit 2
		;;
esac

case "$version" in
	''|*[!0-9A-Za-z._+-]*) echo "invalid release version: $version" >&2; exit 2 ;;
esac
case "$commit" in
	''|*[!0-9A-Za-z._-]*) echo "invalid release commit: $commit" >&2; exit 2 ;;
esac
case "$build_date" in
	''|*[!0-9TZ:.-]*) echo "invalid release build date: $build_date" >&2; exit 2 ;;
esac
case "$source_date_epoch" in
	''|*[!0-9]*) echo "invalid SOURCE_DATE_EPOCH: $source_date_epoch" >&2; exit 2 ;;
esac

script_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
project_dir=$(CDPATH= cd -P "$script_dir/.." && pwd)
case "$dist_dir" in
	/*) dist_path=$dist_dir ;;
	*) dist_path=$project_dir/$dist_dir ;;
esac
dist_dir_abs=$(mkdir -p "$dist_path" && CDPATH= cd -P "$dist_path" && pwd)
package_name="upmctl_${version}_${goos}_${goarch}"
stage_parent="$dist_dir_abs/.stage"
stage="$stage_parent/$package_name"
archive="$dist_dir_abs/$package_name.tar.gz"
buildinfo_package=github.com/upmio/kubespray-upm/upmctl/internal/buildinfo

rm -rf "$stage"
mkdir -p "$stage"

(
	cd "$project_dir"
	CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
		-trimpath -buildvcs=false \
		-ldflags "-s -w -X $buildinfo_package.Version=$version -X $buildinfo_package.GitCommit=$commit -X $buildinfo_package.BuildDate=$build_date" \
		-o "$stage/upmctl" ./cmd/upmctl
)

cp "$project_dir/README.md" "$stage/README.md"
cp "$project_dir/../LICENSE" "$stage/LICENSE"
cp "$script_dir/install.sh" "$stage/install.sh"
mkdir -p "$stage/docs/upmctl" "$stage/skills" "$stage/scripts"
for manual in \
	deployment-guide.md \
	user-guide.md \
	operations-troubleshooting.md \
	admin-operations-test-plan.md \
	admin-operations-test-cases.md \
	cli-coverage-audit.md \
	cli-coverage-matrix.yaml \
	security-negative-test-plan.md; do
	[ -f "$project_dir/docs/$manual" ] || {
		echo "required delivery manual is missing: upmctl/docs/$manual" >&2
		exit 1
	}
	cp "$project_dir/docs/$manual" "$stage/docs/upmctl/$manual"
done
validation_report="$project_dir/docs/test-environment-validation-report.md"
[ -f "$validation_report" ] || {
	echo "required validation report template is missing: upmctl/docs/test-environment-validation-report.md" >&2
	exit 1
}
cp "$validation_report" "$stage/docs/upmctl/test-environment-validation-report.md"
validation_script="$project_dir/scripts/validate-test-environment.sh"
[ -f "$validation_script" ] && [ -x "$validation_script" ] || {
	echo "required executable validation script is missing: upmctl/scripts/validate-test-environment.sh" >&2
	exit 1
}
cp "$validation_script" "$stage/scripts/validate-test-environment.sh"
for validation_support in audit-cli-contract.sh host-safe-cli-coverage.sh; do
	source_support="$project_dir/scripts/tests/$validation_support"
	[ -f "$source_support" ] && [ -x "$source_support" ] || {
		echo "required executable validation support is missing: upmctl/scripts/tests/$validation_support" >&2
		exit 1
	}
	cp "$source_support" "$stage/scripts/$validation_support"
done
[ -d "$project_dir/skills/upmctl-environment" ] || {
	echo "required Codex Skill is missing: upmctl/skills/upmctl-environment" >&2
	exit 1
}
cp -R "$project_dir/skills/upmctl-environment" "$stage/skills/upmctl-environment"
chmod 0755 "$stage/upmctl" "$stage/install.sh" "$stage/scripts/validate-test-environment.sh" \
	"$stage/scripts/audit-cli-contract.sh" "$stage/scripts/host-safe-cli-coverage.sh"
find "$stage/docs" "$stage/skills" -type f -exec chmod 0644 {} \;
find "$stage/docs" "$stage/skills" -type d -exec chmod 0755 {} \;
chmod 0644 "$stage/README.md" "$stage/LICENSE"

(
	cd "$project_dir"
	go run ./scripts/releasemanifest \
		-mode generate -source "$stage" -output "$stage/release-manifest.json" \
		-version "$version" -commit "$commit" -build-date "$build_date" \
		-os "$goos" -arch "$goarch" -validation-tier "$validation_tier" \
		-archive "$package_name.tar.gz"
)

"$script_dir/write-checksums.sh" "$stage" "$stage/SHA256SUMS"
(
	cd "$project_dir"
	go run ./scripts/package-release.go -source "$stage" -output "$archive" -epoch "$source_date_epoch"
)
rm -rf "$stage"
rmdir "$stage_parent" 2>/dev/null || true
