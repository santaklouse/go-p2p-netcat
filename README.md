# go-p2p-netcat

**English** | [Русский](README.RU.md)

A Go implementation of `p2p-netcat`: a bidirectional netcat-like stream
addressed by a libp2p `PeerId` instead of an IP address. The
`/p2p-netcat/1.0.0/<logical-port>` wire protocol, identity files, pairing
tokens, admission handshake, and PTY frames are compatible with the original
JavaScript implementation.

## Porting status

Implemented:

- persistent Ed25519 identities in the compatible protobuf format;
- TCP, QUIC v1, WebSocket, and standard libp2p WebRTC Direct;
- Noise/TLS, Yamux, and Circuit Relay v2;
- mDNS and the IPFS Amino DHT, including provider records;
- private rotating DHT rendezvous identifiers derived from `pnc1_` tokens;
- a mutual admission handshake before application bytes are exposed;
- canonical CBOR, HKDF-SHA-256, AES-256-GCM, and signed RouteRecords;
- raw stdin/stdout, `-e`, TCP forwarding, SOCKS4/4a/5, and PTY sessions;
- `-l`, `-k`, `-w`, `-d`, `-p`, `-q`, `-S`, `-T`, `-i`, `-z`, `-e`,
  `-4`, `-6`, relay, id, and token commands.

Two JavaScript-specific mechanisms have not been ported yet:

- custom WebRTC signaling over public Nostr relays and WebTorrent trackers
  with 120-second session resumption;
- GossipSub `pubsub-peer-discovery` announcements. The `--no-pubsub` flag is
  retained for CLI compatibility but currently has no effect.

The Go implementation uses the standard go-libp2p `webrtc-direct` transport.
TCP, QUIC, Noise, Yamux, identities, pairing, and application protocols are
compatible with the JavaScript CLI. PTY mode works on macOS and Linux. Windows
builds return an explicit error for `-i`, while the other modes remain
available. The static browser PWA remains in the original repository.

## Requirements and build

The module pins Go 1.25.7 in `go.mod`. A recent Go installation with
`GOTOOLCHAIN=auto` downloads the required toolchain automatically.

```bash
cd /Users/alexnevpryaga/projects/santaklouse/go-p2p-netcat
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o p2p-nc ./cmd/p2p-nc
./p2p-nc --version
```

Install the command into `GOBIN`:

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/p2p-nc
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
