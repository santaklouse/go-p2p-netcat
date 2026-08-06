#!/usr/bin/env bash

set -euo pipefail

P2PNC_VERSION="v0.6.0"
P2PNC_ANDROID_ABI="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
case "$P2PNC_ANDROID_ABI" in
  arm64-v8a) P2PNC_ANDROID_ARCH="arm64" ;;
  armeabi-v7a|armeabi) P2PNC_ANDROID_ARCH="armv7" ;;
  *) echo "Unsupported Android ABI: $P2PNC_ANDROID_ABI" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-android-${P2PNC_ANDROID_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then
  grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | sha256sum --check -
else
  grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | shasum -a 256 --check
fi
tar -xzf "$P2PNC_ARCHIVE"
adb push "p2p-nc-android-${P2PNC_ANDROID_ARCH}/p2p-nc" /data/local/tmp/p2p-nc
adb shell chmod 755 /data/local/tmp/p2p-nc
adb shell /data/local/tmp/p2p-nc --version
