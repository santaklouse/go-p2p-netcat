#!/usr/bin/env bash

set -euo pipefail

p2pnc_usage() {
	printf 'usage: %s VERSION ARCH BINARY_DIR OUTPUT_DIR\n' "$0" >&2
	exit 2
}

[[ $# -eq 4 ]] || p2pnc_usage

p2pnc_version="${1#v}"
p2pnc_arch="$2"
p2pnc_binary_dir="$3"
p2pnc_output_dir="$4"

if [[ ! "$p2pnc_version" =~ ^[0-9][0-9A-Za-z.+:~-]*$ ]]; then
	printf 'invalid Debian version: %s\n' "$p2pnc_version" >&2
	exit 2
fi

case "$p2pnc_arch" in
amd64 | arm64) ;;
*)
	printf 'unsupported Debian architecture: %s\n' "$p2pnc_arch" >&2
	exit 2
	;;
esac

command -v dpkg-deb >/dev/null 2>&1 || {
	printf 'dpkg-deb is required to build the Debian package\n' >&2
	exit 1
}

[[ -x "$p2pnc_binary_dir/p2p-nc" ]] || {
	printf 'missing executable: %s/p2p-nc\n' "$p2pnc_binary_dir" >&2
	exit 1
}

mkdir -p "$p2pnc_output_dir"
p2pnc_temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/p2p-netcat-deb.XXXXXXXX")"
trap 'rm -rf "$p2pnc_temp_dir"' EXIT

p2pnc_root="$p2pnc_temp_dir/root"
install -d -m 0755 \
	"$p2pnc_root/DEBIAN" \
	"$p2pnc_root/usr/bin" \
	"$p2pnc_root/usr/share/doc/p2p-netcat"

install -m 0755 "$p2pnc_binary_dir/p2p-nc" "$p2pnc_root/usr/bin/p2p-nc"
ln -s p2p-nc "$p2pnc_root/usr/bin/pnc"
ln -s p2p-nc "$p2pnc_root/usr/bin/p2p-netcat"
install -m 0644 LICENSE "$p2pnc_root/usr/share/doc/p2p-netcat/copyright"
install -m 0644 README.md README.RU.md "$p2pnc_root/usr/share/doc/p2p-netcat/"

p2pnc_installed_size="$(du -sk "$p2pnc_root/usr" | awk '{print $1}')"
printf '%s\n' \
	'Package: p2p-netcat' \
	"Version: $p2pnc_version" \
	'Section: net' \
	'Priority: optional' \
	"Architecture: $p2pnc_arch" \
	'Maintainer: go-p2p-netcat maintainers <santaklouse@users.noreply.github.com>' \
	"Installed-Size: $p2pnc_installed_size" \
	'Depends: ca-certificates' \
	'Homepage: https://github.com/santaklouse/go-p2p-netcat' \
	'Description: PeerId-addressed netcat-compatible networking utility' \
	' p2p-netcat provides bidirectional streams, TCP and UDP forwarding,' \
	' SOCKS proxying, PTY sessions, relay routing, and native WebRTC signaling.' \
	>"$p2pnc_root/DEBIAN/control"
chmod 0644 "$p2pnc_root/DEBIAN/control"

p2pnc_package="$p2pnc_output_dir/p2p-netcat_${p2pnc_version}_${p2pnc_arch}.deb"
dpkg-deb --root-owner-group --build "$p2pnc_root" "$p2pnc_package"
dpkg-deb --info "$p2pnc_package" >/dev/null
dpkg-deb --contents "$p2pnc_package" >/dev/null
printf 'built %s\n' "$p2pnc_package"
