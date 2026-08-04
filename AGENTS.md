# go-p2p-netcat contributor context

This file is operational context for coding agents and human contributors.
Read it before changing behavior, wire formats, networking, documentation, CI,
or release files.

## Repository and scope

- Canonical Go repository: `/Users/alexnevpryaga/projects/santaklouse/go-p2p-netcat`.
- Deprecated JavaScript predecessor: `/Users/alexnevpryaga/projects/santaklouse/p2p-netcat`.
- Module: `github.com/santaklouse/go-p2p-netcat`.
- Commands: `cmd/p2p-nc` and the identical `cmd/pnc` alias.
- The browser PWA remains TypeScript under `web/`; browser code cannot expose
  local TCP or UDP sockets.
- Preserve unrelated worktree changes and generated local files. In
  particular, do not delete `web/dev-dist/` merely because it is untracked.

## Compatibility invariants

Do not change these without an explicit protocol-version migration:

- Byte-stream protocol: `/p2p-netcat/1.0.0/<logical-port>`.
- Datagram protocol: `/p2p-netcat/udp/1.0.0/<logical-port>`.
- Logical ports are decimal integers in `1..65535`.
- PTY frame layout and constants in `protocol/pty` and `packages/core/src/index.js`.
- Pairing, admission, signed route records, PeerId validation, and the frozen
  authentication-domain strings used by compatible JavaScript/browser peers.
- UDP datagram boundaries: one application datagram per length-prefixed frame.

PeerId is an identity, not a route. A connection still needs a direct
multiaddr, DHT/provider discovery, mDNS, PubSub discovery, native WebRTC
signaling, or Circuit Relay. Native Nostr/WebTorrent signaling can cross many
NAT combinations without a user-operated media relay, but symmetric NAT or
blocked UDP cannot be promised to work without TURN, a reachable TCP/WSS
route, port mapping, or Circuit Relay.

## Main packages

| Path | Responsibility |
|---|---|
| `internal/cli` | Cobra options, validation, listener/client orchestration, route racing |
| `internal/app` | Process entry point, signals, streams, Tor re-exec, user-facing diagnostics |
| `p2p` | libp2p transports, DHT, mDNS, PubSub discovery, relay routing |
| `nativewebrtc` | Pion endpoint, Nostr/WebTorrent signaling, authenticated reconnectable stream |
| `session` | raw bridge, exec, TCP/UDP forwarding, SOCKS, Unix PTY, Windows ConPTY |
| `protocol` | Admission, pairing, datagram, PTY, and route-record wire codecs |
| `relay` | Embeddable Circuit Relay v2 server |
| `packages/core` | Browser-safe JavaScript wire compatibility package |
| `web` | Static browser PWA |
| `deploy` | Installer and Linux WireGuard full-tunnel wrapper |
| `scripts` | Docker, Wiki, and privileged network-namespace integration tests |

## WireGuard full-tunnel design

The WireGuard client endpoint is the local p2p-nc UDP socket, normally
`127.0.0.1:15182`. The remote listener forwards framed UDP to the real
WireGuard server at `127.0.0.1:51820`.

Two protections are required for `AllowedIPs = 0.0.0.0/0`:

1. `internal/cli` establishes the first P2P UDP carrier before announcing the
   local UDP listener. This prevents the first WireGuard handshake from being
   responsible for starting route discovery.
2. `deploy/wireguard-full-tunnel.sh` runs the whole p2p-nc client under an
   unused UID and installs IPv4/IPv6 `uidrange ... lookup main` policy rules.
   This covers every outer socket, including DNS, DHT, signaling, STUN, ICE,
   libp2p transports, and reconnects. A following `prohibit` rule prevents
   fallthrough into WireGuard when the physical table has no matching route.
   Do not replace it with a partial socket
   mark unless every current and future outbound socket is demonstrably marked.

The WireGuard gateway must independently enable IP forwarding and egress NAT.
The exact client and gateway examples are maintained in both
`docs/USE_CASES.md` and `docs/USE_CASES.RU.md`.

The deterministic privileged test is `scripts/full_tunnel_netns_test.sh`. It
creates five uniquely named Linux network namespaces, two NATs, a TCP port map,
a WireGuard client and gateway, UDP-over-TCP p2p forwarding, and a simulated
HTTP Internet endpoint. It cleans up only objects created by that invocation.

## PTY shutdown semantics

On Unix, reading the PTY master returns `EIO` after the last slave closes. This
is normal EOF after shell `exit` or Ctrl-D, not a session failure. `PTYServer`
must send a graceful `CloseWrite` after the last output so the client consumes
`logout` and receives EOF. In the native WebRTC adapter, `Close` is graceful
and `Reset` is the abort operation. Keep tests for both behaviors.

## CLI mode rules

- Raw stream is the default mode.
- `-l` selects listener mode; `-k`, `-e`, and `-S` are listener-only.
- `-z` is client-only.
- `-p` means a server-side destination port with `-l` and a local forwarding
  listener in client mode.
- `-u` requires `-p` and selects framed fixed-destination UDP forwarding.
- `-i`, `-e`, `-S`, TCP/UDP forwarding, and `-z` are mutually exclusive modes.
- Only one local listener may own a logical port at a time, regardless of
  listener mode or byte-stream/datagram protocol. `internal/listenerlock`
  enforces this across processes with an OS file lock; persistent lock files
  are harmless because ownership is released when the process exits.
- `--quit-delay` applies only to raw streams.
- Tor requires an explicit TCP/WS/WSS relay and disables direct UDP transports
  and public discovery.
- Keep `internal/cli/root_test.go` table-driven mode coverage synchronized with
  validation changes.

## Required toolchains

- Go version is defined by `go.mod`; use a modern Go installation and
  `GOTOOLCHAIN=auto` when appropriate.
- Node must be at least 22.12 for `packages/core` and `web`.
- On this macOS workstation, prefer `/opt/homebrew/opt/go/libexec/bin/go`,
  `/opt/homebrew/bin/node`, and `/usr/bin/ssh`; older `/usr/local` binaries may
  be incompatible.
- Docker Desktop may be unavailable. `limactl` can provide Linux; check its
  state before use. Never delete or reset a user-owned Lima instance.

## Verification commands

Run the smallest relevant tests while editing, then the complete set:

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go test -race -count=1 -timeout=25m ./...
GOTOOLCHAIN=auto go vet ./...
bash deploy/deploy_test.sh
bash deploy/wireguard-full-tunnel_test.sh
bash scripts/sync-wiki_test.sh
bash scripts/docker_test.sh
```

Browser packages:

```bash
(cd packages/core && npm ci && npm test && npm run lint && npm pack --dry-run)
(cd web && npm ci && npm test && npm run lint && npm pack --dry-run)
```

Privileged Linux full-tunnel test:

```bash
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o /tmp/p2p-nc-linux ./cmd/p2p-nc
sudo P2PNC_BINARY=/tmp/p2p-nc-linux scripts/full_tunnel_netns_test.sh
```

Use the architecture matching the Linux host. The test supports
`P2PNC_IPTABLES=iptables-legacy` where the nftables compatibility backend is
not available.

## Documentation, messages, and releases

- All non-web runtime and script messages must be English. The web PWA retains
  explicit English/Russian localization.
- Keep English and Russian documents synchronized.
- Wiki content is generated by `scripts/sync-wiki.sh`; test it after docs edits.
- Update version references consistently when releasing, then validate Go,
  JavaScript, web, Docker, archives, checksums, tags, and GitHub workflows.
- GitHub Actions use pinned action commit SHAs. Preserve least-privilege job
  permissions and keep shell syntax checks synchronized with added scripts.
