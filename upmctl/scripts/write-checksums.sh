#!/bin/sh
set -eu

if [ "$#" -ne 2 ]; then
	echo "usage: $0 INPUT_PATH OUTPUT_FILE" >&2
	exit 2
fi

input=$1
output=$2
output_dir=$(dirname "$output")
mkdir -p "$output_dir"
temp=$(mktemp "$output_dir/.$(basename "$output").XXXXXX")
trap 'rm -f "$temp"' EXIT HUP INT TERM

checksum() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	else
		echo "sha256sum or shasum is required" >&2
		exit 2
	fi
}

if [ -d "$input" ]; then
	(
		cd "$input"
		temp_name=./$(basename "$temp")
		output_name=./$(basename "$output")
		find . -type f -print | LC_ALL=C sort | while IFS= read -r file; do
			[ "$file" = "$output_name" ] && continue
			[ "$file" = "$temp_name" ] && continue
			name=${file#./}
			printf '%s  %s\n' "$(checksum "$file")" "$name"
		done
	) >"$temp"
else
	printf '%s  %s\n' "$(checksum "$input")" "$(basename "$input")" >"$temp"
fi

chmod 0644 "$temp"
mv "$temp" "$output"
trap - EXIT HUP INT TERM
