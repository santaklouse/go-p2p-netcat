#!/usr/bin/env bash

set -Eeuo pipefail

test_root="$(mktemp -d "${TMPDIR:-/tmp}/p2p-nc-deploy-test.XXXXXXXX")"
trap 'rm -rf -- "${test_root}"' EXIT

release_dir="${test_root}/release"
package_dir="${release_dir}/p2p-nc-linux-amd64"
install_dir="${test_root}/bin"
mkdir -p "${package_dir}"

printf '%s\n' '#!/usr/bin/env sh' 'printf "p2p-nc version test\n"' >"${package_dir}/p2p-nc"
printf '%s\n' '#!/usr/bin/env sh' 'printf "pnc version test\n"' >"${package_dir}/pnc"
chmod 0755 "${package_dir}/p2p-nc" "${package_dir}/pnc"

(
	cd "${release_dir}"
	tar -czf p2p-nc-linux-amd64.tar.gz p2p-nc-linux-amd64
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum p2p-nc-linux-amd64.tar.gz >SHA256SUMS
	else
		shasum -a 256 p2p-nc-linux-amd64.tar.gz >SHA256SUMS
	fi
)

P2PNC_RELEASE_BASE="file://${release_dir}" \
	P2PNC_INSTALL_DIR="${install_dir}" \
	P2PNC_OS=linux \
	P2PNC_ARCH=amd64 \
	P2PNC_NO_SUDO=1 \
	bash "$(dirname "$0")/deploy.sh"

[[ "$("${install_dir}/p2p-nc" --version)" == "p2p-nc version test" ]]
[[ "$("${install_dir}/pnc" --version)" == "pnc version test" ]]
[[ -L "${install_dir}/p2p-netcat" ]]
[[ "$("${install_dir}/p2p-netcat" --version)" == "p2p-nc version test" ]]

P2PNC_INSTALL_DIR="${install_dir}" \
	P2PNC_UNINSTALL=1 \
	P2PNC_NO_SUDO=1 \
	bash "$(dirname "$0")/deploy.sh"

[[ ! -e "${install_dir}/p2p-nc" ]]
[[ ! -e "${install_dir}/pnc" ]]
[[ ! -e "${install_dir}/p2p-netcat" ]]

printf '%064d  %s\n' 0 p2p-nc-linux-amd64.tar.gz >"${release_dir}/SHA256SUMS"
if P2PNC_RELEASE_BASE="file://${release_dir}" \
	P2PNC_INSTALL_DIR="${install_dir}" \
	P2PNC_OS=linux \
	P2PNC_ARCH=amd64 \
	P2PNC_NO_SUDO=1 \
	bash "$(dirname "$0")/deploy.sh" >/dev/null 2>&1; then
	printf 'tampered checksum unexpectedly passed\n' >&2
	exit 1
fi

printf 'deploy tests passed\n'
