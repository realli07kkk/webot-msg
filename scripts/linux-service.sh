#!/usr/bin/env bash
set -Eeuo pipefail

SERVICE_NAME="webot-msg"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd -- "${SCRIPT_DIR}/.." && pwd -P)"
BIN_DIR="${REPO_ROOT}/bin"
BINARY_PATH="${BIN_DIR}/${SERVICE_NAME}"

DEPLOY_USER=""
DEPLOY_GROUP=""
DEPLOY_HOME=""
WEBOT_DIR=""
CONFIG_DIR=""
LOG_DIR=""
CONFIG_PATH=""

usage() {
	cat <<EOF
Usage: $0 <command>

Commands:
  install   Build bin/${SERVICE_NAME}, create ~/.webot-msg, and install systemd service
  upgrade   Stop running service if active, replace bin/${SERVICE_NAME}, and restart only if it was active
  start     Run systemctl start ${SERVICE_NAME}
  stop      Run systemctl stop ${SERVICE_NAME}
  restart   Run systemctl restart ${SERVICE_NAME}
  status    Run systemctl status ${SERVICE_NAME}
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

require_linux_systemd() {
	if [[ "$(uname -s)" != "Linux" ]]; then
		fail "Linux systemd environment required"
	fi
	if [[ ! -d /run/systemd/system ]]; then
		fail "systemd is not running or /run/systemd/system is missing"
	fi
	require_command systemctl
}

sudo_cmd() {
	if [[ "$(id -u)" -eq 0 ]]; then
		"$@"
	else
		sudo "$@"
	fi
}

require_sudo() {
	if [[ "$(id -u)" -eq 0 ]]; then
		return
	fi
	require_command sudo
	sudo -v || fail "sudo privilege is required"
}

resolve_deploy_identity() {
	require_command getent

	if [[ -n "${SUDO_USER:-}" && "${SUDO_USER}" != "root" ]]; then
		DEPLOY_USER="${SUDO_USER}"
	else
		DEPLOY_USER="$(id -un)"
	fi
	[[ -n "${DEPLOY_USER}" ]] || fail "deploy user is empty"

	DEPLOY_GROUP="$(id -gn "${DEPLOY_USER}")" || fail "cannot resolve group for ${DEPLOY_USER}"
	DEPLOY_HOME="$(getent passwd "${DEPLOY_USER}" | cut -d: -f6)"
	[[ -n "${DEPLOY_HOME}" ]] || fail "cannot resolve home for ${DEPLOY_USER}"

	WEBOT_DIR="${DEPLOY_HOME}/.webot-msg"
	CONFIG_DIR="${WEBOT_DIR}/config"
	LOG_DIR="${WEBOT_DIR}/logs"
	CONFIG_PATH="${CONFIG_DIR}/${SERVICE_NAME}.toml"
}

print_paths() {
	info "repo root: ${REPO_ROOT}"
	info "deploy user: ${DEPLOY_USER}:${DEPLOY_GROUP}"
	info "deploy home: ${DEPLOY_HOME}"
	info "runtime dir: ${WEBOT_DIR}"
	info "binary path: ${BINARY_PATH}"
	info "config path: ${CONFIG_PATH}"
	info "service file: ${SERVICE_FILE}"
}

prepare_common_context() {
	require_linux_systemd
	require_command go
	resolve_deploy_identity
	print_paths
}

chown_if_root() {
	if [[ "$(id -u)" -eq 0 ]]; then
		chown "$1" "$2"
	fi
}

ensure_systemd_path() {
	if [[ "$1" == *[[:space:]]* ]]; then
		fail "systemd service path contains whitespace and is not supported: $1"
	fi
}

build_binary() {
	info "building ${BINARY_PATH}"
	mkdir -p "${BIN_DIR}"
	chown_if_root "${DEPLOY_USER}:${DEPLOY_GROUP}" "${BIN_DIR}" || fail "cannot chown ${BIN_DIR}"

	local tmp_binary
	tmp_binary="$(mktemp "${BIN_DIR}/.${SERVICE_NAME}.tmp.XXXXXX")" || fail "cannot create temporary binary path"

	if ! (cd "${REPO_ROOT}" && go build -o "${tmp_binary}" ./cmd/webot-msg); then
		rm -f "${tmp_binary}"
		fail "go build failed"
	fi

	chmod 0755 "${tmp_binary}" || {
		rm -f "${tmp_binary}"
		fail "cannot chmod temporary binary"
	}
	chown_if_root "${DEPLOY_USER}:${DEPLOY_GROUP}" "${tmp_binary}" || {
		rm -f "${tmp_binary}"
		fail "cannot chown temporary binary"
	}

	mv -f "${tmp_binary}" "${BINARY_PATH}" || {
		rm -f "${tmp_binary}"
		fail "cannot replace ${BINARY_PATH}"
	}
	info "binary updated: ${BINARY_PATH}"
}

create_runtime_dir() {
	local path="$1"
	local mode="$2"

	mkdir -p "${path}" || fail "cannot create directory: ${path}"
	chmod "${mode}" "${path}" || fail "cannot chmod ${path}"
	chown_if_root "${DEPLOY_USER}:${DEPLOY_GROUP}" "${path}" || fail "cannot chown ${path}"
}

prepare_runtime_dirs() {
	info "creating runtime directories"
	create_runtime_dir "${WEBOT_DIR}" 0700
	create_runtime_dir "${CONFIG_DIR}" 0700
	create_runtime_dir "${LOG_DIR}" 0755
}

write_default_config() {
	if [[ -e "${CONFIG_PATH}" ]]; then
		info "config exists, keeping: ${CONFIG_PATH}"
		return
	fi

	info "writing default config: ${CONFIG_PATH}"
	local tmp_config
	tmp_config="$(mktemp "${CONFIG_DIR}/.${SERVICE_NAME}.toml.tmp.XXXXXX")" || fail "cannot create temporary config"

	cat >"${tmp_config}" <<'EOF'
[api]
port = 26322

[storage]
auth_path = "~/.webot-msg/config/auth.json"

[ilink]
base_url = "https://ilinkai.weixin.qq.com"

[log]
file_path = "~/.webot-msg/logs/webot-msg.log"
max_size = "100MB"
EOF

	chmod 0600 "${tmp_config}" || {
		rm -f "${tmp_config}"
		fail "cannot chmod temporary config"
	}
	chown_if_root "${DEPLOY_USER}:${DEPLOY_GROUP}" "${tmp_config}" || {
		rm -f "${tmp_config}"
		fail "cannot chown temporary config"
	}
	mv -f "${tmp_config}" "${CONFIG_PATH}" || {
		rm -f "${tmp_config}"
		fail "cannot write ${CONFIG_PATH}"
	}
}

write_service_unit() {
	ensure_systemd_path "${REPO_ROOT}"
	ensure_systemd_path "${BINARY_PATH}"
	ensure_systemd_path "${CONFIG_PATH}"

	info "writing systemd service: ${SERVICE_FILE}"
	local tmp_service
	tmp_service="$(mktemp)" || fail "cannot create temporary service file"

	cat >"${tmp_service}" <<EOF
[Unit]
Description=webot-msg local bot message bridge
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${DEPLOY_USER}
Group=${DEPLOY_GROUP}
WorkingDirectory=${REPO_ROOT}
ExecStart=${BINARY_PATH} -c ${CONFIG_PATH}
Restart=on-failure
RestartSec=5s

[Install]
WantedBy=multi-user.target
EOF

	sudo_cmd cp "${tmp_service}" "${SERVICE_FILE}" || {
		rm -f "${tmp_service}"
		fail "cannot write ${SERVICE_FILE}"
	}
	rm -f "${tmp_service}"
	sudo_cmd chmod 0644 "${SERVICE_FILE}" || fail "cannot chmod ${SERVICE_FILE}"
}

daemon_reload() {
	info "reloading systemd"
	sudo_cmd systemctl daemon-reload || fail "systemctl daemon-reload failed"
}

service_is_active() {
	systemctl is-active --quiet "${SERVICE_NAME}"
}

stop_service() {
	info "stopping ${SERVICE_NAME}"
	sudo_cmd systemctl stop "${SERVICE_NAME}" || fail "systemctl stop ${SERVICE_NAME} failed"
}

start_service() {
	info "starting ${SERVICE_NAME}"
	sudo_cmd systemctl start "${SERVICE_NAME}" || fail "systemctl start ${SERVICE_NAME} failed"
}

cmd_install() {
	prepare_common_context
	require_sudo
	build_binary
	prepare_runtime_dirs
	write_default_config
	write_service_unit
	daemon_reload
	info "install complete"
}

cmd_upgrade() {
	require_linux_systemd
	local was_active=0
	if service_is_active; then
		was_active=1
	fi

	require_command go
	resolve_deploy_identity
	print_paths

	if [[ "${was_active}" -eq 1 ]]; then
		require_sudo
		stop_service
	else
		info "${SERVICE_NAME} is not active; upgrade will not start it"
	fi

	build_binary

	if [[ "${was_active}" -eq 1 ]]; then
		start_service
	fi
	info "upgrade complete"
}

cmd_systemctl() {
	require_linux_systemd
	require_sudo
	sudo_cmd systemctl "$1" "${SERVICE_NAME}"
}

main() {
	local command="${1:-}"
	case "${command}" in
		install)
			cmd_install
			;;
		upgrade)
			cmd_upgrade
			;;
		start|stop|restart|status)
			cmd_systemctl "${command}"
			;;
		""|-h|--help|help)
			usage
			;;
		*)
			usage >&2
			fail "unknown command: ${command}"
			;;
	esac
}

main "$@"
