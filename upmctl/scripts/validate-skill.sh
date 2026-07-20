#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	echo "usage: $0 SKILL_DIRECTORY" >&2
	exit 2
fi

skill_dir=$1
[ -d "$skill_dir" ] || { echo "skill directory not found: $skill_dir" >&2; exit 1; }

if [ -n "${SKILL_QUICK_VALIDATE:-}" ]; then
	validator=$SKILL_QUICK_VALIDATE
else
	codex_home=${CODEX_HOME:-$HOME/.codex}
	validator=$codex_home/skills/.system/skill-creator/scripts/quick_validate.py
fi

[ -f "$validator" ] || {
	echo "skill-creator quick_validate.py not found: $validator" >&2
	echo "set SKILL_QUICK_VALIDATE to the official validator path" >&2
	exit 1
}
command -v python3 >/dev/null 2>&1 || { echo "python3 is required for Skill validation" >&2; exit 1; }

python3 "$validator" "$skill_dir"
