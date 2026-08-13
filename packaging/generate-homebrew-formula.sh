#!/usr/bin/env bash

set -euo pipefail

p2pnc_usage() {
	printf 'usage: %s VERSION DARWIN_AMD64_SHA256 DARWIN_ARM64_SHA256 LINUX_AMD64_SHA256 LINUX_ARM64_SHA256 OUTPUT\n' "$0" >&2
	exit 2
}

[[ $# -eq 6 ]] || p2pnc_usage

p2pnc_version="${1#v}"
p2pnc_darwin_amd64_sha256="$2"
p2pnc_darwin_arm64_sha256="$3"
p2pnc_linux_amd64_sha256="$4"
p2pnc_linux_arm64_sha256="$5"
p2pnc_output="$6"

if [[ ! "$p2pnc_version" =~ ^[0-9][0-9A-Za-z.+_-]*$ ]]; then
	printf 'invalid Homebrew version: %s\n' "$p2pnc_version" >&2
	exit 2
fi

for p2pnc_checksum in \
	"$p2pnc_darwin_amd64_sha256" \
	"$p2pnc_darwin_arm64_sha256" \
	"$p2pnc_linux_amd64_sha256" \
	"$p2pnc_linux_arm64_sha256"; do
	if [[ ! "$p2pnc_checksum" =~ ^[0-9a-fA-F]{64}$ ]]; then
		printf 'invalid SHA-256 value: %s\n' "$p2pnc_checksum" >&2
		exit 2
	fi
	done

mkdir -p "$(dirname "$p2pnc_output")"
{
	printf '%s\n' \
		'class P2pNetcat < Formula' \
		'  desc "PeerId-addressed netcat-compatible networking utility"' \
		'  homepage "https://github.com/santaklouse/go-p2p-netcat"' \
		"  version \"$p2pnc_version\"" \
		'  license "MIT"' \
		'' \
		'  on_macos do' \
		'    if Hardware::CPU.arm?' \
		"      url \"https://github.com/santaklouse/go-p2p-netcat/releases/download/v$p2pnc_version/p2p-nc-darwin-arm64.tar.gz\"" \
		"      sha256 \"$p2pnc_darwin_arm64_sha256\"" \
		'    else' \
		"      url \"https://github.com/santaklouse/go-p2p-netcat/releases/download/v$p2pnc_version/p2p-nc-darwin-amd64.tar.gz\"" \
		"      sha256 \"$p2pnc_darwin_amd64_sha256\"" \
		'    end' \
		'  end' \
		'' \
		'  on_linux do' \
		'    if Hardware::CPU.arm?' \
		"      url \"https://github.com/santaklouse/go-p2p-netcat/releases/download/v$p2pnc_version/p2p-nc-linux-arm64.tar.gz\"" \
		"      sha256 \"$p2pnc_linux_arm64_sha256\"" \
		'    else' \
		"      url \"https://github.com/santaklouse/go-p2p-netcat/releases/download/v$p2pnc_version/p2p-nc-linux-amd64.tar.gz\"" \
		"      sha256 \"$p2pnc_linux_amd64_sha256\"" \
		'    end' \
		'  end' \
		'' \
		'  def install' \
		'    bin.install "p2p-nc"' \
		'    bin.install_symlink "p2p-nc" => "pnc"' \
		'    bin.install_symlink "p2p-nc" => "p2p-netcat"' \
		'  end' \
		'' \
		'  test do' \
		'    assert_match "p2p-nc version #{version}", shell_output("#{bin}/p2p-nc --version")' \
		'  end' \
		'end'
} >"$p2pnc_output"

if command -v ruby >/dev/null 2>&1; then
	ruby -c "$p2pnc_output" >/dev/null
fi

printf 'generated %s\n' "$p2pnc_output"
