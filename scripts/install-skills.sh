#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd -P)"
SOURCE_SKILLS_DIR="${REPO_ROOT}/skill"
AGENT_SKILLS_DIR="${HOME}/.agent/skills"
CLAUDE_SKILLS_DIR="${HOME}/.claude/skills"

usage() {
	cat <<EOF
Usage: $0

Copy project skills from:
  ${SOURCE_SKILLS_DIR}

To:
  ${AGENT_SKILLS_DIR}

Then create symlinks in:
  ${CLAUDE_SKILLS_DIR}

Existing ~/.agent/skills/<skill-name> directories with the same project skill
name are replaced. Existing ~/.claude/skills/<skill-name> non-symlink paths are
not overwritten.
EOF
}

info() {
	printf '==> %s\n' "$*"
}

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

require_command() {
	command -v "$1" >/dev/null 2>&1 || fail "missing command: $1"
}

copy_skill() {
	local source_dir="$1"
	local skill_name
	skill_name="$(basename -- "${source_dir}")"

	[[ -f "${source_dir}/SKILL.md" ]] || fail "missing SKILL.md: ${source_dir}"

	local target_dir="${AGENT_SKILLS_DIR}/${skill_name}"
	local tmp_dir="${AGENT_SKILLS_DIR}/.${skill_name}.tmp.$$"

	rm -rf -- "${tmp_dir}"
	mkdir -p -- "${tmp_dir}"
	cp -a -- "${source_dir}/." "${tmp_dir}/"

	rm -rf -- "${target_dir}"
	mv -- "${tmp_dir}" "${target_dir}"

	info "installed skill: ${target_dir}"
}

link_skill_for_claude() {
	local skill_name="$1"
	local target_dir="${AGENT_SKILLS_DIR}/${skill_name}"
	local link_path="${CLAUDE_SKILLS_DIR}/${skill_name}"

	[[ -d "${target_dir}" ]] || fail "agent skill does not exist: ${target_dir}"

	if [[ -L "${link_path}" ]]; then
		ln -sfn -- "${target_dir}" "${link_path}"
	elif [[ -e "${link_path}" ]]; then
		fail "refusing to overwrite non-symlink path: ${link_path}"
	else
		ln -s -- "${target_dir}" "${link_path}"
	fi

	info "linked Claude skill: ${link_path} -> ${target_dir}"
}

main() {
	if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
		usage
		return
	fi
	if [[ $# -ne 0 ]]; then
		usage >&2
		exit 2
	fi

	require_command cp
	require_command find
	require_command ln

	[[ -d "${SOURCE_SKILLS_DIR}" ]] || fail "source skills directory not found: ${SOURCE_SKILLS_DIR}"

	mkdir -p -- "${AGENT_SKILLS_DIR}" "${CLAUDE_SKILLS_DIR}"

	local found=0
	while IFS= read -r -d '' source_dir; do
		found=1
		copy_skill "${source_dir}"
		link_skill_for_claude "$(basename -- "${source_dir}")"
	done < <(find "${SOURCE_SKILLS_DIR}" -mindepth 1 -maxdepth 1 -type d -print0 | sort -z)

	if [[ "${found}" -eq 0 ]]; then
		fail "no skills found under: ${SOURCE_SKILLS_DIR}"
	fi

	info "done"
}

main "$@"
