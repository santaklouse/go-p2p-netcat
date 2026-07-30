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
  P2PNC_VERSION=v0.2.0 bash
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
