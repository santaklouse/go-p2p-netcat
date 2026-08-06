# Installing go-p2p-netcat

[Русская версия](INSTALLATION.RU.md)

## Requirements

- Go with `GOTOOLCHAIN=auto`; the module selects its required toolchain;
- macOS, Linux, or Windows;
- outbound TCP/WSS and UDP for discovery, signaling, QUIC, and WebRTC.

## Verified deploy script

Install the latest Linux, macOS, or Android release:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  bash
```

The script detects the release target, downloads the archive and
`SHA256SUMS`, verifies SHA-256 before extraction, and installs all three command
names. It does not start a listener, background service, shell, cron job, or
login item.

Pin a version:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_VERSION=v0.5.1 bash
```

Install without elevated privileges:

```bash
install -d -m 0755 ~/.local/bin
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_INSTALL_DIR="$HOME/.local/bin" P2PNC_NO_SUDO=1 bash
```

Uninstall from the same directory:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_INSTALL_DIR="$HOME/.local/bin" P2PNC_UNINSTALL=1 bash
```

For audit-sensitive environments, download and inspect the script before
running it:

```bash
curl -fsSLo /tmp/p2p-nc-deploy.sh \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh
less /tmp/p2p-nc-deploy.sh
bash /tmp/p2p-nc-deploy.sh
```

The complete variable reference is at the top of
[`deploy/deploy.sh`](../deploy/deploy.sh).

## Docker and GitHub Packages

Release images are published to GitHub Container Registry as
`ghcr.io/santaklouse/go-p2p-netcat` for Linux `amd64` and `arm64`.

Pull and verify the latest stable image:

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker run --rm ghcr.io/santaklouse/go-p2p-netcat:latest --version
```

The image uses these tags:

| Git ref | Container tags |
|---|---|
| `main` | `main`, for example `sha-0123456789ab` |
| `v1.2.3` | `1.2.3`, `1.2`, `1`, `latest`, for example `sha-0123456789ab` |

Use a version tag or digest instead of `latest` for reproducible deployments:

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker inspect \
  --format='{{index .RepoDigests 0}}' \
  ghcr.io/santaklouse/go-p2p-netcat:latest
```

The process runs as UID/GID `65532`, includes CA certificates and `/bin/sh`,
and uses `/config/p2p-netcat/identity.key` as its default persistent identity.
Its writable cache, including listener lock files, is kept below
`/config/p2p-netcat/cache`. Keep `/config` in a named volume:

```bash
docker volume create p2p-netcat-config
docker run --rm \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  id
```

On Linux, host networking gives libp2p and forwarded services the same network
namespace as the host. This is the most direct configuration for a listener:

```bash
docker run --rm --init \
  --name p2p-netcat \
  --network host \
  --volume p2p-netcat-config:/config \
  --env XDG_CACHE_HOME=/config/p2p-netcat/cache \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -l -k --transport-port 4001 31337
```

It also lets a UDP listener reach a WireGuard endpoint bound to host loopback:

```bash
docker run --rm --init \
  --name p2p-netcat-wireguard \
  --network host \
  --volume p2p-netcat-config:/config \
  --env XDG_CACHE_HOME=/config/p2p-netcat/cache \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -u -l -k --transport-port 4001 \
  -d 127.0.0.1 -p 51820 35182
```

Docker Desktop does not provide Linux host networking with identical behavior.
Use bridge networking there, publish the required TCP/UDP ports, and provide an
explicit relay or public `--announce` address. Docker Desktop bridge networks
do not expose a suitable multicast interface to libp2p mDNS, so disable mDNS;
DHT, PubSub, native WebRTC signaling, and explicit relay discovery remain
available:

```bash
docker run --rm --init \
  --name p2p-netcat \
  --publish 4001:4001/tcp \
  --publish 4001:4001/udp \
  --publish 127.0.0.1:15182:15182/udp \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -u --no-mdns --bind 0.0.0.0 -p 15182 --transport-port 4001 \
  12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 35182
```

The target PeerId in that command is a syntactically valid example; an actual
connection must use the listener's PeerId. Add the same explicit `--relay`
multiaddr to both peers when direct discovery or reachability is insufficient.
The Docker-published UDP port remains host-loopback-only even though
p2p-netcat binds all interfaces inside the isolated container.
A quic-go receive-buffer warning may still appear because Docker Desktop
controls the Linux VM socket limits; it is non-fatal and does not require
disabling QUIC.

Build and test the container locally:

```bash
docker build --build-arg VERSION=local -t p2p-netcat:local .
docker run --rm p2p-netcat:local --version
bash scripts/docker_test.sh
```

The GitHub Actions release workflow publishes multi-platform OCI images with
SBOM and provenance attestations using the repository `GITHUB_TOKEN`; no
long-lived registry token is required. GitHub creates a new container package
as private by default. After its first publication, a package administrator
must explicitly select **Public** in the package settings if anonymous pulls
are desired. Public container packages can then be pulled without login.

## Install with Go

Install both command names:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
```

Verify the installation:

```bash
p2p-nc --version
pnc --version
```

## First connection

Start the listener:

```bash
p2p-nc -l -v 31337
```

Use the printed PeerId from another machine:

```bash
printf 'hello\n' | p2p-nc -v -w 90 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

The PeerId above is syntactically valid, but a real connection must use the
value printed by the listener. For deterministic routing through a relay, pass
the same explicit `--relay` multiaddr on both sides.

## Interactive PTY

```bash
# listener
p2p-nc -l -i -v 31337

# client
p2p-nc -i -v 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

PTY uses a native pseudoterminal on Unix and ConPTY on Windows. Exit the client
with `Ctrl-E`, then `Q`.

## Browser PWA

The static browser client is published from `web/`:

- English: <https://santaklouse.github.io/go-p2p-netcat/>
- Russian: <https://santaklouse.github.io/go-p2p-netcat/?lang=ru>

Browsers cannot dial ordinary TCP or QUIC multiaddrs. They use native WebRTC,
WebTransport, or secure WebSocket relay routes.

## Persistent identity and private pairing

The listener creates `~/.config/p2p-netcat/identity.key` by default. Select a
different file with `--identity`. Back it up: replacing it changes the PeerId.

Create a private token:

```bash
p2p-nc token --identity ~/.config/p2p-netcat/identity.key 31337
```

To protect the token during storage or transfer, write a password-encrypted
file and unlock it once into a local `0600` token file:

```bash
p2p-nc token 31337 \
  --identity ~/.config/p2p-netcat/identity.key \
  --encrypt-to ~/.config/p2p-netcat/pairing.token.enc
p2p-nc token unlock \
  ~/.config/p2p-netcat/pairing.token.enc \
  --output ~/.config/p2p-netcat/pairing.token
```

Both password prompts read from a terminal without echo. For non-interactive
automation, `--password-file` accepts a regular file that has no group or other
permissions. The unlocked file is a bearer credential and no password is
requested when it is later passed through `--pairing-token-file`.

Pass it via `--pairing-token`, `--pairing-token-file`, or
`P2P_NETCAT_TOKEN`. Private mode derives secret DHT/signaling rendezvous,
encrypts native signaling, and performs the mutual admission handshake before
application bytes.

## Building and testing

```bash
GOTOOLCHAIN=auto go build ./cmd/p2p-nc ./cmd/pnc
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
```

The browser packages require Node 22.12 or newer:

```bash
cd packages/core
npm ci
npm test
cd ../../web
npm ci
npm test
npm run lint
```
