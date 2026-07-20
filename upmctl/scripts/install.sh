#!/bin/sh
set -eu

usage() {
	cat <<'EOF'
Usage: ./install.sh [--prefix /usr/local] [--replace]

Install upmctl into PREFIX/bin. Existing installations are never overwritten
unless --replace is supplied; replacement first creates a timestamped backup.
EOF
}

prefix=/usr/local
replace=false
while [ "$#" -gt 0 ]; do
	case "$1" in
		--prefix)
			[ "$#" -ge 2 ] || { echo "--prefix requires a value" >&2; exit 2; }
			prefix=$2
			shift 2
			;;
		--replace)
			replace=true
			shift
			;;
		-h|--help)
			usage
			exit 0
			;;
		*)
			echo "unknown argument: $1" >&2
			usage >&2
			exit 2
			;;
	esac
done

case "$prefix" in
	/*) ;;
	*) echo "prefix must be an absolute path" >&2; exit 2 ;;
esac
case "$prefix" in
	/|*'
'*) echo "unsafe prefix: $prefix" >&2; exit 2 ;;
esac

script_dir=$(CDPATH= cd -P "$(dirname "$0")" && pwd)
source_binary="$script_dir/upmctl"
source_checksums="$script_dir/SHA256SUMS"
bin_dir="$prefix/bin"
target="$bin_dir/upmctl"

[ -f "$source_binary" ] && [ ! -L "$source_binary" ] && [ -x "$source_binary" ] || {
	echo "release binary is missing, non-regular, or not executable: $source_binary" >&2
	exit 1
}
[ -f "$source_checksums" ] && [ ! -L "$source_checksums" ] || {
	echo "release checksum manifest is missing or unsafe: $source_checksums" >&2
	exit 1
}

expected=$(awk '$2 == "upmctl" { print $1 }' "$source_checksums")
case "$expected" in
	''|*[!0-9a-fA-F]*) echo "release checksum manifest does not contain a valid upmctl digest" >&2; exit 1 ;;
esac
[ "${#expected}" -eq 64 ] || { echo "release upmctl digest is not SHA-256" >&2; exit 1; }

checksum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		echo "sha256sum or shasum is required" >&2
		exit 1
	fi
}

actual=$(checksum "$source_binary")
[ "$actual" = "$expected" ] || {
	echo "release binary checksum mismatch" >&2
	exit 1
}

if [ -L "$prefix" ] || { [ -e "$bin_dir" ] && [ -L "$bin_dir" ]; }; then
	echo "refusing to install through a symlinked prefix or bin directory" >&2
	exit 1
fi
mkdir -p "$bin_dir"
[ -d "$bin_dir" ] || { echo "not a directory: $bin_dir" >&2; exit 1; }

if [ -L "$target" ]; then
	echo "refusing to replace symlink: $target" >&2
	exit 1
fi
if [ -e "$target" ]; then
	if [ "$(checksum "$target")" = "$actual" ]; then
		echo "upmctl is already installed at $target"
		exit 0
	fi
	if [ "$replace" != true ]; then
		echo "upmctl already exists at $target; use --replace to preserve it as a backup and upgrade" >&2
		exit 1
	fi
	backup=$(mktemp "$target.backup.$(date -u '+%Y%m%dT%H%M%SZ').XXXXXX")
	cp -p "$target" "$backup"
	echo "Preserved existing binary as $backup"
fi

umask 022
temp=$(mktemp "$bin_dir/.upmctl.install.XXXXXX")
trap 'rm -f "$temp"' EXIT HUP INT TERM
cp "$source_binary" "$temp"
chmod 0755 "$temp"
[ "$(checksum "$temp")" = "$actual" ] || { echo "installed copy checksum mismatch" >&2; exit 1; }
mv "$temp" "$target"
trap - EXIT HUP INT TERM

echo "Installed upmctl to $target"
echo "Run: $target version"
