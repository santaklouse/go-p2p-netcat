#!/usr/bin/env bash
#
# Install or uninstall a verified go-p2p-netcat release.
#
# Typical usage:
#   curl -fsSL https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh | bash
#
# Configuration:
#   P2PNC_VERSION=v0.2.0       Install a specific release (default: latest).
#   P2PNC_INSTALL_DIR=DIR      Override the installation directory.
#   P2PNC_UNINSTALL=1          Remove p2p-nc, pnc, and p2p-netcat.
#   P2PNC_NO_SUDO=1            Never invoke sudo.
#   P2PNC_DEBUG=1              Enable shell tracing.
#   P2PNC_REPOSITORY=OWNER/REPO
#                              Override the GitHub repository.
#   P2PNC_RELEASE_BASE=URL     Override the release directory URL.
#   P2PNC_OS=linux|darwin|android
#   P2PNC_ARCH=amd64|arm64|armv7
#
# The script installs binaries only. It never creates a background service,
# listener, shell, cron job, or login item.

set -Eeuo pipefail

if [[ -n "${P2PNC_DEBUG:-}" ]]; then
	set -x
fi

p2pnc_repository="${P2PNC_REPOSITORY:-santaklouse/go-p2p-netcat}"
p2pnc_version="${P2PNC_VERSION:-latest}"
p2pnc_temp_dir=""
p2pnc_sudo=()

p2pnc_log() {
	printf '[p2p-nc] %s\n' "$*"
}

p2pnc_die() {
	printf '[p2p-nc] error: %s\n' "$*" >&2
	exit 1
}

p2pnc_cleanup() {
	if [[ -n "${p2pnc_temp_dir}" && -d "${p2pnc_temp_dir}" ]]; then
		rm -rf -- "${p2pnc_temp_dir}"
	fi
}

trap p2pnc_cleanup EXIT

p2pnc_require() {
	command -v "$1" >/dev/null 2>&1 || p2pnc_die "required command not found: $1"
}

p2pnc_detect_os() {
	if [[ -n "${P2PNC_OS:-}" ]]; then
		printf '%s\n' "${P2PNC_OS}"
		return
	fi

	case "$(uname -s)" in
	Linux)
		if [[ -n "${ANDROID_ROOT:-}" ]] || [[ "$(uname -o 2>/dev/null || true)" == "Android" ]]; then
			printf 'android\n'
		else
			printf 'linux\n'
		fi
		;;
	Darwin)
		printf 'darwin\n'
		;;
	*)
		p2pnc_die "unsupported operating system: $(uname -s)"
		;;
	esac
}

p2pnc_detect_arch() {
	if [[ -n "${P2PNC_ARCH:-}" ]]; then
		printf '%s\n' "${P2PNC_ARCH}"
		return
	fi
	if [[ "$(uname -s)" == "Darwin" ]] &&
		command -v sysctl >/dev/null 2>&1 &&
		[[ "$(sysctl -in hw.optional.arm64 2>/dev/null || true)" == "1" ]]; then
		printf 'arm64\n'
		return
	fi

	case "$(uname -m)" in
	x86_64 | amd64)
		printf 'amd64\n'
		;;
	arm64 | aarch64)
		printf 'arm64\n'
		;;
	armv7l | armv7)
		printf 'armv7\n'
		;;
	*)
		p2pnc_die "unsupported architecture: $(uname -m)"
		;;
	esac
}

p2pnc_default_install_dir() {
	if [[ -n "${P2PNC_INSTALL_DIR:-}" ]]; then
		printf '%s\n' "${P2PNC_INSTALL_DIR}"
		return
	fi
	if [[ -n "${PREFIX:-}" && -d "${PREFIX}/bin" ]]; then
		printf '%s\n' "${PREFIX}/bin"
		return
	fi
	if [[ "$(id -u)" -eq 0 || -w /usr/local/bin ]]; then
		printf '/usr/local/bin\n'
		return
	fi
	printf '%s/.local/bin\n' "${HOME}"
}

p2pnc_prepare_privileges() {
	local destination="$1"
	local parent
	parent="$(dirname "${destination}")"

	if [[ -d "${destination}" && -w "${destination}" ]]; then
		return
	fi
	if [[ ! -d "${destination}" && -d "${parent}" && -w "${parent}" ]]; then
		return
	fi
	if [[ "$(id -u)" -eq 0 ]]; then
		return
	fi
	if [[ -n "${P2PNC_NO_SUDO:-}" ]]; then
		p2pnc_die "${destination} is not writable and P2PNC_NO_SUDO is set"
	fi
	command -v sudo >/dev/null 2>&1 || p2pnc_die "${destination} is not writable and sudo is unavailable"
	p2pnc_sudo=(sudo)
}

p2pnc_download() {
	local url="$1"
	local destination="$2"

	if command -v curl >/dev/null 2>&1; then
		curl --fail --silent --show-error --location --retry 3 \
			--output "${destination}" "${url}"
	elif command -v wget >/dev/null 2>&1; then
		wget --quiet --tries=3 --output-document="${destination}" "${url}"
	else
		p2pnc_die "curl or wget is required"
	fi
}

p2pnc_sha256() {
	local file="$1"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "${file}" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "${file}" | awk '{print $1}'
	else
		p2pnc_die "sha256sum or shasum is required"
	fi
}

p2pnc_verify() {
	local archive="$1"
	local checksum_file="$2"
	local archive_name
	local expected
	local actual

	archive_name="$(basename "${archive}")"
	expected="$(
		awk -v name="${archive_name}" '
			$2 == name || $2 == "*" name { print tolower($1); exit }
		' "${checksum_file}"
	)"
	[[ -n "${expected}" ]] || p2pnc_die "SHA256SUMS has no entry for ${archive_name}"

	actual="$(p2pnc_sha256 "${archive}")"
	[[ "${actual}" == "${expected}" ]] ||
		p2pnc_die "SHA-256 verification failed for ${archive_name}"
	p2pnc_log "verified ${archive_name}: ${actual}"
}

p2pnc_uninstall() {
	local install_dir="$1"
	p2pnc_prepare_privileges "${install_dir}"
	for name in p2p-nc pnc p2p-netcat; do
		if [[ -e "${install_dir}/${name}" || -L "${install_dir}/${name}" ]]; then
			"${p2pnc_sudo[@]}" rm -f -- "${install_dir:?}/${name}"
			p2pnc_log "removed ${install_dir}/${name}"
		fi
	done
}

p2pnc_install() {
	local os_name="$1"
	local arch="$2"
	local install_dir="$3"
	local target="p2p-nc-${os_name}-${arch}"
	local archive_name="${target}.tar.gz"
	local release_base
	local archive
	local checksums
	local source_dir

	case "${os_name}-${arch}" in
	linux-amd64 | linux-arm64 | darwin-amd64 | darwin-arm64 | android-arm64 | android-armv7) ;;
	*) p2pnc_die "no release artifact for ${os_name}/${arch}" ;;
	esac

	if [[ -n "${P2PNC_RELEASE_BASE:-}" ]]; then
		release_base="${P2PNC_RELEASE_BASE%/}"
	elif [[ "${p2pnc_version}" == "latest" ]]; then
		release_base="https://github.com/${p2pnc_repository}/releases/latest/download"
	else
		release_base="https://github.com/${p2pnc_repository}/releases/download/${p2pnc_version}"
	fi

	p2pnc_require tar
	p2pnc_require awk
	p2pnc_temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/p2p-nc-install.XXXXXXXX")"
	archive="${p2pnc_temp_dir}/${archive_name}"
	checksums="${p2pnc_temp_dir}/SHA256SUMS"

	p2pnc_log "downloading ${release_base}/${archive_name}"
	p2pnc_download "${release_base}/${archive_name}" "${archive}"
	p2pnc_download "${release_base}/SHA256SUMS" "${checksums}"
	p2pnc_verify "${archive}" "${checksums}"

	tar -xzf "${archive}" -C "${p2pnc_temp_dir}"
	source_dir="${p2pnc_temp_dir}/${target}"
	[[ -f "${source_dir}/p2p-nc" ]] || p2pnc_die "archive does not contain ${target}/p2p-nc"
	[[ -f "${source_dir}/pnc" ]] || p2pnc_die "archive does not contain ${target}/pnc"

	p2pnc_prepare_privileges "${install_dir}"
	"${p2pnc_sudo[@]}" mkdir -p -- "${install_dir}"
	"${p2pnc_sudo[@]}" install -m 0755 "${source_dir}/p2p-nc" "${install_dir}/p2p-nc"
	"${p2pnc_sudo[@]}" install -m 0755 "${source_dir}/pnc" "${install_dir}/pnc"
	"${p2pnc_sudo[@]}" ln -sfn p2p-nc "${install_dir}/p2p-netcat"

	p2pnc_log "installed ${install_dir}/p2p-nc"
	p2pnc_log "installed ${install_dir}/pnc"
	p2pnc_log "installed ${install_dir}/p2p-netcat"
	case ":${PATH}:" in
	*":${install_dir}:"*) ;;
	*) p2pnc_log "add ${install_dir} to PATH before invoking p2p-nc" ;;
	esac
}

p2pnc_main() {
	local install_dir
	local os_name
	local arch

	install_dir="$(p2pnc_default_install_dir)"
	if [[ -n "${P2PNC_UNINSTALL:-}" ]]; then
		p2pnc_uninstall "${install_dir}"
		return
	fi

	os_name="$(p2pnc_detect_os)"
	arch="$(p2pnc_detect_arch)"
	p2pnc_install "${os_name}" "${arch}" "${install_dir}"
	"${install_dir}/p2p-nc" --version
}

p2pnc_main "$@"
