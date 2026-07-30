# go-p2p-netcat

**English** | [Русский](README.RU.md)

## Quick install

Install the latest tagged version:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  bash
```

The deploy script detects Linux, macOS, or Android and the processor
architecture, downloads the matching release, verifies it against
`SHA256SUMS`, and installs `p2p-nc`, `pnc`, and `p2p-netcat`. See
[installation](docs/INSTALLATION.md#verified-deploy-script) for version
pinning, a custom destination, and uninstalling.

The Linux `amd64`/`arm64` container is published to
[GitHub Packages](https://github.com/santaklouse/go-p2p-netcat/pkgs/container/go-p2p-netcat):

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker run --rm ghcr.io/santaklouse/go-p2p-netcat:latest --version
```

The container runs as a non-root user and stores its persistent identity under
`/config`. See [Docker and GitHub Packages](docs/INSTALLATION.md#docker-and-github-packages)
for listener, UDP, networking, version-tag, and local-build examples.

Alternatively, install with Go:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
```

`CGO_ENABLED=0` is intentional: the project does not require CGO, and this
avoids macOS DWARF linking failures when Homebrew LLVM takes precedence over
Apple's toolchain. If CGO is explicitly required, select Apple Clang:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ \
  go install github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
```

On macOS and Linux, install it and immediately create the `p2p-netcat`
command as a symbolic link next to `p2p-nc`:

```bash
set -e

GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
P2PNC_BIN_DIR="$(go env GOBIN)"
if [ -z "$P2PNC_BIN_DIR" ]; then
  P2PNC_BIN_DIR="$(go env GOPATH)/bin"
fi
ln -sf "$P2PNC_BIN_DIR/p2p-nc" "$P2PNC_BIN_DIR/p2p-netcat"
export PATH="$P2PNC_BIN_DIR:$PATH"
p2p-netcat --version
```

A Go implementation of `p2p-netcat`: a bidirectional netcat-like stream
addressed by a libp2p `PeerId` instead of an IP address. The existing
`/p2p-netcat/1.0.0/<logical-port>` stream protocol, identity files, pairing
tokens, admission handshake, and PTY frames are compatible with the original
JavaScript implementation. Go peers additionally support framed UDP forwarding
through `/p2p-netcat/udp/1.0.0/<logical-port>`.

## Porting status

Implemented:

- persistent Ed25519 identities in the compatible protobuf format;
- TCP, QUIC v1, WebSocket, and standard libp2p WebRTC Direct;
- Noise/TLS, Yamux, and Circuit Relay v2;
- mDNS, GossipSub peer discovery, and the IPFS Amino DHT, including provider records;
- private rotating DHT rendezvous identifiers derived from `pnc1_` tokens;
- a mutual admission handshake before application bytes are exposed;
- canonical CBOR, HKDF-SHA-256, AES-256-GCM, and signed RouteRecords;
- native WebRTC v2 with Nostr/WebTorrent signaling, PeerId authentication,
  pairing-token encryption, flow control, 120-second stream resumption, and
  libp2p route racing;
- raw stdin/stdout, `-e`, TCP and packet-preserving UDP forwarding,
  SOCKS4/4a/5, and PTY sessions, including Windows ConPTY;
- `-l`, `-k`, `-w`, `-d`, `-p`, `-u`, `-q`, `-S`, `-T`, `-i`, `-z`, `-e`,
  `-4`, `-6`, relay, id, and token commands;
- the `p2p-nc` and short `pnc` command names;
- the browser-safe core and static English/Russian PWA in `packages/core` and
  `web`.

The migration from the JavaScript repository is complete. The Go CLI keeps the
same application protocols and can interoperate with the old CLI and browser
client. Browser code remains TypeScript because it runs directly in browser
Web APIs; it is now versioned and deployed from this repository.

## Installation

The current stable version is `v0.3.0`. Release archives contain the
`p2p-nc` and `pnc` executables, the MIT license, and both README files. Verify the
downloaded archive against `SHA256SUMS` before installing it.

### Linux

The following command selects `amd64` or `arm64`, verifies the archive, and
installs the executable into `/usr/local/bin`:

```bash
set -euo pipefail

P2PNC_VERSION="v0.3.0"
case "$(uname -m)" in
  x86_64|amd64) P2PNC_ARCH="amd64" ;;
  aarch64|arm64) P2PNC_ARCH="arm64" ;;
  *) echo "Unsupported Linux architecture: $(uname -m)" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-linux-${P2PNC_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | sha256sum --check -
tar -xzf "$P2PNC_ARCHIVE"
sudo install -m 0755 "p2p-nc-linux-${P2PNC_ARCH}/p2p-nc" /usr/local/bin/p2p-nc
p2p-nc --version
```

### macOS

The macOS archives support Intel and Apple Silicon. This command detects the
processor architecture automatically:

```bash
set -euo pipefail

P2PNC_VERSION="v0.3.0"
case "$(uname -m)" in
  x86_64|amd64) P2PNC_ARCH="amd64" ;;
  arm64|aarch64) P2PNC_ARCH="arm64" ;;
  *) echo "Unsupported macOS architecture: $(uname -m)" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-darwin-${P2PNC_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | shasum -a 256 --check
tar -xzf "$P2PNC_ARCHIVE"
sudo mkdir -p /usr/local/bin
sudo install -m 0755 "p2p-nc-darwin-${P2PNC_ARCH}/p2p-nc" /usr/local/bin/p2p-nc
p2p-nc --version
```

The release binary is not Apple-notarized. If Gatekeeper quarantines it after
a browser download, inspect the file first and then remove only its quarantine
attribute:

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/p2p-nc
```

### Windows

Open PowerShell and run:

```powershell
$ErrorActionPreference = 'Stop'
$Version = 'v0.3.0'
$Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    'X64' { 'amd64' }
    'Arm64' { 'arm64' }
    default { throw "Unsupported Windows architecture: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}
$Archive = "p2p-nc-windows-$Architecture.zip"
$ReleaseUrl = "https://github.com/santaklouse/go-p2p-netcat/releases/download/$Version"

Invoke-WebRequest "$ReleaseUrl/$Archive" -OutFile $Archive
Invoke-WebRequest "$ReleaseUrl/SHA256SUMS" -OutFile 'SHA256SUMS'
$ChecksumLine = Get-Content 'SHA256SUMS' | Where-Object { $_ -match ([regex]::Escape($Archive) + '$') }
if (-not $ChecksumLine) { throw "Checksum for $Archive was not found" }
$ExpectedHash = ($ChecksumLine -split '\s+')[0].ToLowerInvariant()
$ActualHash = (Get-FileHash $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($ActualHash -ne $ExpectedHash) { throw "SHA-256 verification failed for $Archive" }

Expand-Archive $Archive -DestinationPath . -Force
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\p2p-netcat'
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Copy-Item "p2p-nc-windows-$Architecture\p2p-nc.exe" "$InstallDir\p2p-nc.exe" -Force

$CurrentPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($CurrentPath -split ';') -notcontains $InstallDir) {
    $NewPath = if ([string]::IsNullOrWhiteSpace($CurrentPath)) { $InstallDir } else { "$CurrentPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}
& "$InstallDir\p2p-nc.exe" --version
```

The updated user `PATH` is available in newly opened terminals. Windows
SmartScreen can warn because the release executable is not code-signed.

### Android phones

Android builds are standalone shell executables, not APK files. They require
Android 7.0 or newer and do not require root when launched through `adb shell`.
Enable USB debugging, connect the phone, install Android Platform Tools, and
run:

```bash
set -euo pipefail

P2PNC_VERSION="v0.3.0"
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
```

Run the command with an explicit writable identity path when persistent
identity is required:

```bash
adb shell /data/local/tmp/p2p-nc id --identity /data/local/tmp/p2p-nc-identity.key
```

### Install with Go

With a recent Go installation and automatic toolchain downloads enabled:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@v0.3.0
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@v0.3.0
"$(go env GOPATH)/bin/p2p-nc" --version
```

## Build from source

The module pins Go 1.25.7 in `go.mod`. A recent Go installation with
`GOTOOLCHAIN=auto` downloads the required toolchain automatically.

```bash
cd /Users/alexnevpryaga/projects/santaklouse/go-p2p-netcat
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o p2p-nc ./cmd/p2p-nc
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o pnc ./cmd/pnc
./p2p-nc --version
```

Install the command into `GOBIN`:

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/p2p-nc
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/pnc
```

On this development machine, `/usr/local/bin/go` is an obsolete Intel build of
Go 1.13. Use `/opt/homebrew/bin/go` or correct `PATH`:

```bash
export PATH="/opt/homebrew/bin:$PATH"
go version
```

## Quick start

Start a listener:

```bash
./p2p-nc -l 8080
```

The command prints its PeerId and available multiaddrs to `stderr`. Connect
from the client:

```bash
./p2p-nc 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 8080
```

Use the actual PeerId printed by the listener. The following complete example
performs a local check without DHT discovery:

```bash
DEMO_DIR="$(mktemp -d)"
DEMO_KEY="$DEMO_DIR/listener.key"
DEMO_ID="$(./p2p-nc id --identity "$DEMO_KEY")"
./p2p-nc -l 8080 \
  --transport-port 43127 \
  --no-dht \
  --no-mdns \
  --no-quic \
  --no-webrtc \
  --identity "$DEMO_KEY" \
  >"$DEMO_DIR/received.txt" &
DEMO_PID=$!
sleep 2
printf 'hello over go-libp2p\n' | ./p2p-nc \
  --no-dht \
  --no-mdns \
  --no-quic \
  --no-webrtc \
  "/ip4/127.0.0.1/tcp/43127/p2p/$DEMO_ID" \
  8080
wait "$DEMO_PID"
cat "$DEMO_DIR/received.txt"
```

## Private pairing

Create a token from the listener's persistent identity:

```bash
./p2p-nc token 31337 \
  --identity "$HOME/.config/p2p-netcat/identity.key" \
  >"$HOME/.config/p2p-netcat/pairing.token"
chmod 600 "$HOME/.config/p2p-netcat/pairing.token"
```

Start the listener:

```bash
./p2p-nc -l -i \
  --identity "$HOME/.config/p2p-netcat/identity.key" \
  --pairing-token-file "$HOME/.config/p2p-netcat/pairing.token"
```

After securely transferring the token file, start the client:

```bash
./p2p-nc -i \
  --pairing-token-file "$HOME/.config/p2p-netcat/pairing.token"
```

The token contains the PeerId and logical port, so private mode does not
require separate positional arguments. Keep the token file private and set its
permissions to `0600`.

## Forwarding, SOCKS, and PTY

Forward a local TCP port to `127.0.0.1:5432` on the remote peer:

```bash
./p2p-nc -l 15432 -p 5432
./p2p-nc -p 15432 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 15432
```

Forward a local UDP endpoint to WireGuard on the remote peer:

```bash
# WireGuard host
./p2p-nc -u -l -d 127.0.0.1 -p 51820 35182

# WireGuard client host
./p2p-nc -u -p 15182 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 35182
```

Set the WireGuard peer endpoint on the client to `127.0.0.1:15182`.
`p2p-nc` keeps UDP packet boundaries while carrying the packets through the
selected libp2p stream, including TCP/WSS and Circuit Relay routes.

Run a SOCKS server on the remote peer:

```bash
./p2p-nc -l -S 1080
./p2p-nc -p 1080 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 1080
curl --socks5-hostname 127.0.0.1:1080 https://example.com/
```

Start an interactive login shell:

```bash
./p2p-nc -l -i 2222
./p2p-nc -i 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 2222
```

In the PTY client, press `Ctrl-E` followed by `q` to close the session.

## Running a relay

Run a local Circuit Relay v2 instance:

```bash
./p2p-nc relay -4 -p 9090 --websocket-port 9091
```

On a public VPS, add the server's actual public multiaddrs with `--announce`.
TCP and UDP port 9090 must be reachable from the internet. Open TCP port 9091
as well when WebSocket access is required.

## Verification

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go fmt ./...
GOTOOLCHAIN=auto /opt/homebrew/bin/go vet ./...
GOTOOLCHAIN=auto /opt/homebrew/bin/go test ./...
```

The test suite includes the published JavaScript interoperability vectors for
tokens, all four HKDF keys, rendezvous IDs, provider CIDs, AES-GCM envelopes,
and both admission frames.

## Automated releases

Every push to `main`, including merged pull requests, and every semantic
`v*.*.*` tag runs `.github/workflows/release-main.yml`. After tests and static
analysis pass, the workflow publishes a GitHub Release with these builds:

- Linux: `amd64`, `arm64`;
- macOS: `amd64`, `arm64`;
- Windows: `amd64`, `arm64`;
- Android 7.0 (API 24) and newer: `arm64`, `armv7`.

Linux and macOS builds are distributed as `.tar.gz` archives. Windows builds
are distributed as `.zip` archives, and Android builds as `.tar.gz` archives.
Every release also contains `SHA256SUMS`. Semantic tags such as `v0.3.0`
produce stable releases. Builds from `main` are marked as prereleases and use a
deterministic tag that starts with `main-` and ends with the first 12
characters of the commit SHA. Rerunning a workflow updates the same release
instead of creating a duplicate.

The Android artifacts are command-line executables for Android's shell, not
APK files. Use `android-arm64` for almost all current physical phones and
tablets. `android-armv7` supports older 32-bit ARM devices. For example:

```bash
tar -xzf p2p-nc-android-arm64.tar.gz
adb push p2p-nc-android-arm64/p2p-nc /data/local/tmp/p2p-nc
adb shell chmod 755 /data/local/tmp/p2p-nc
adb shell /data/local/tmp/p2p-nc --version
```

## Documentation

- [Practical usage cookbook](docs/USE_CASES.md): OpenSSH, OpenVPN,
  TCP/UDP forwarding, SOCKS, WireGuard, file transfer, relay, and
  systemd examples.
- [Installation](docs/INSTALLATION.md)
- [Architecture](docs/ARCHITECTURE.md)
- [Datagram forwarding protocol](docs/DATAGRAM_PROTOCOL.md)
- [gs-netcat compatibility](docs/GS_NETCAT_COMPAT.md)
- [Pairing protocol](docs/PAIRING_PROTOCOL.md)
- [Relay API](docs/RELAY_API.md)
- [Native WebRTC migration](docs/WEBRTC_MIGRATION.md)
- [GitHub Wiki](https://github.com/santaklouse/go-p2p-netcat/wiki)

## Project structure

```text
cmd/p2p-nc/             CLI entry point
internal/cli/           parsing, validation, and lifecycle
internal/identity/      compatible persistent Ed25519 keys
p2p/                    host, transports, DHT, mDNS, and relay
protocol/pairing/       token, HKDF, rendezvous, and AEAD
protocol/admission/     mutual fixed-frame handshake
protocol/routerecord/   deterministic CBOR and identity signatures
protocol/pty/           binary PTY frames
session/                raw, exec, forwarding, SOCKS, and PTY
```

## License

MIT.
