#!/usr/bin/env bash

set -euo pipefail

p2pnc_test_root="$(mktemp -d "${TMPDIR:-/tmp}/p2p-netcat-homebrew-test.XXXXXXXX")"
trap 'rm -rf "$p2pnc_test_root"' EXIT

p2pnc_formula="$p2pnc_test_root/p2p-netcat.rb"
bash packaging/generate-homebrew-formula.sh \
	v9.8.7 \
	0000000000000000000000000000000000000000000000000000000000000001 \
	0000000000000000000000000000000000000000000000000000000000000002 \
	0000000000000000000000000000000000000000000000000000000000000003 \
	0000000000000000000000000000000000000000000000000000000000000004 \
	"$p2pnc_formula"

ruby -c "$p2pnc_formula" | grep -Fqx 'Syntax OK'
grep -Fqx '  version "9.8.7"' "$p2pnc_formula"
grep -Fq '/v9.8.7/p2p-nc-darwin-arm64.tar.gz"' "$p2pnc_formula"
grep -Fq '/v9.8.7/p2p-nc-linux-amd64.tar.gz"' "$p2pnc_formula"
grep -Fqx '    bin.install_symlink "p2p-nc" => "pnc"' "$p2pnc_formula"
grep -Fqx '    bin.install_symlink "p2p-nc" => "p2p-netcat"' "$p2pnc_formula"

printf 'Homebrew formula tests passed\n'
