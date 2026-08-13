#!/usr/bin/env bash

set -euo pipefail

command -v dpkg-deb >/dev/null 2>&1 || {
	printf 'dpkg-deb is required to test Debian packaging\n' >&2
	exit 1
}

p2pnc_test_root="$(mktemp -d "${TMPDIR:-/tmp}/p2p-netcat-package-test.XXXXXXXX")"
trap 'rm -rf "$p2pnc_test_root"' EXIT

install -d -m 0755 "$p2pnc_test_root/bin" "$p2pnc_test_root/dist" "$p2pnc_test_root/unpacked"
printf '#!/bin/sh\nprintf "p2p-nc version 9.8.7\\n"\n' >"$p2pnc_test_root/bin/p2p-nc"
chmod 0755 "$p2pnc_test_root/bin/p2p-nc"

bash packaging/build-deb.sh \
	v9.8.7 \
	amd64 \
	"$p2pnc_test_root/bin" \
	"$p2pnc_test_root/dist"

p2pnc_package="$p2pnc_test_root/dist/p2p-netcat_9.8.7_amd64.deb"
[[ -f "$p2pnc_package" ]]

dpkg-deb --field "$p2pnc_package" Package | grep -Fqx 'p2p-netcat'
dpkg-deb --field "$p2pnc_package" Version | grep -Fqx '9.8.7'
dpkg-deb --field "$p2pnc_package" Architecture | grep -Fqx 'amd64'
dpkg-deb --field "$p2pnc_package" Depends | grep -Fqx 'ca-certificates'

dpkg-deb --extract "$p2pnc_package" "$p2pnc_test_root/unpacked"
"$p2pnc_test_root/unpacked/usr/bin/p2p-nc" --version | grep -Fqx 'p2p-nc version 9.8.7'
[[ "$(readlink "$p2pnc_test_root/unpacked/usr/bin/pnc")" == 'p2p-nc' ]]
[[ "$(readlink "$p2pnc_test_root/unpacked/usr/bin/p2p-netcat")" == 'p2p-nc' ]]
[[ -f "$p2pnc_test_root/unpacked/usr/share/doc/p2p-netcat/README.md" ]]
[[ -f "$p2pnc_test_root/unpacked/usr/share/doc/p2p-netcat/README.RU.md" ]]
[[ -f "$p2pnc_test_root/unpacked/usr/share/doc/p2p-netcat/copyright" ]]

printf 'Debian package tests passed\n'
