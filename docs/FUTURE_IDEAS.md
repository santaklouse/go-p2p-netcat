# Future ideas: relay-only and WSS gateway/agent

[Русская версия](FUTURE_IDEAS.RU.md)

Document status: **architecture proposal; implementation has not started**.
It defines sufficiently precise boundaries for a later implementation, but it
does not change the current CLI, wire protocols, or compatibility guarantees.

## Goal

Add two related but fundamentally different modes:

1. strict `--relay-only`, where the regular `p2p-nc` keeps its local PeerId and
   end-to-end libp2p security but makes external connections only to explicitly
   configured Circuit Relay v2 servers;
2. optional `p2p-nc-lite` + `p2p-nc gateway` thin-client mode, where the local
   computer keeps an outbound WSS connection on TCP 443 while a trusted
   Internet server owns the full P2P host and its network connections.

Do not allocate dynamic public TCP ports for clients. Carry control and data
over one versioned WSS protocol at a fixed endpoint.

## Why these are separate modes

They address similar reachability problems but have different trust boundaries.

| Property | `--relay-only` | WSS gateway/agent |
|---|---|---|
| Private identity key location | Local host | Gateway, separately for every device |
| P2P endpoint | Local host | Gateway |
| Relay/gateway access to application plaintext | Circuit Relay cannot read plaintext | Trusted gateway can potentially read plaintext |
| Local implementation size | Full `p2p-nc` | Smaller `p2p-nc-lite` |
| Local host external connections | Configured relays only | WSS to gateway only |
| Compatibility with a regular peer | Full | Full on the P2P side because the gateway runs the regular protocol handler |

`--relay-only` is the recommended first phase. The WSS gateway is justified
only when a minimal agent, centralized egress, or browser-friendly transport is
more important and the gateway is inside the user's trust boundary.

## Invariants that must not change

The implementation must not change:

- byte-stream protocol `/p2p-netcat/1.0.0/<logical-port>`;
- datagram protocol `/p2p-netcat/udp/1.0.0/<logical-port>`;
- logical port range `1..65535`;
- PTY frame layout and constants;
- pairing token, admission handshake, signed route records, or frozen
  authentication-domain strings;
- the one-application-datagram-per-length-prefixed-frame rule;
- the existing meaning of `-i`: interactive PTY, not a gateway address;
- the distinction between a PeerId and a route.

The WSS gateway protocol is a new internal transport between the lite agent and
gateway. It does not replace or rename existing P2P protocols.

## Non-goals

The first implementation must not:

- promise anonymity or resistance to traffic analysis;
- turn the gateway into an unauthenticated public proxy;
- upload an existing local identity private key to the gateway;
- start one `p2p-nc` process per session;
- change the browser PWA as part of the first Go MVP;
- implement multi-gateway migration, billing, or a public SaaS control plane;
- present the trusted gateway as an end-to-end opaque relay;
- put secrets in URL query parameters;
- use random public TCP ports as the data plane.

## Phase 1: strict `--relay-only`

### User-visible semantics

Add a boolean flag:

```text
--relay-only    use only explicitly configured Circuit Relay v2 routes; never dial or accept a direct peer route
```

Listener example:

```bash
export P2PNC_RELAY=/dns4/relay.example.com/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
p2p-nc -l -k --relay-only --relay "${P2PNC_RELAY}" 34000
```

Client example:

```bash
export P2PNC_RELAY=/dns4/relay.example.com/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
p2p-nc --relay-only --relay "${P2PNC_RELAY}" 12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW 34000
```

The example domains and PeerIds are illustrative.

### Validation rules

`--relay-only`:

- requires at least one explicit `--relay` or a relay hint from a valid pairing
  token;
- rejects `--announce` because a direct announced address contradicts the
  policy;
- automatically disables DHT, mDNS, PubSub discovery, direct QUIC, WebRTC
  Direct, and native Nostr/WebTorrent WebRTC;
- works with `-T` only when every relay route uses TCP/WS/WSS rather than
  UDP/QUIC;
- permits multiple explicit relays for availability, but no other Internet peer
  may become an outer connection;
- rejects a direct multiaddr even when the target is supplied as a full direct
  address;
- returns a clear error when the relay is unavailable; direct fallback is
  forbidden.

Validation happens in two stages: Cobra validation checks explicitly supplied
flags, then final validation checks the effective relay list after `loadToken`.
Without the second stage, a command whose only relay hint is in a pairing token
would be rejected before reading that token.

Pairing mode must not implicitly enable private DHT or native WebRTC in this
mode. Relay hints from the token are allowed, but all other route hints must be
discarded.

### Route enforcement

Changing only candidate ordering is insufficient. Enforce the policy below the
route race:

1. parse and freeze the allowed relay PeerId set and base multiaddrs;
2. connect directly only to those relay PeerIds;
3. normalize each relay address so it ends in `/p2p/<relay-id>`, then construct
   a target route only by appending `/p2p-circuit/p2p/<target-id>`;
4. remove or ignore direct target addresses from the peerstore while opening
   an application stream;
5. reject direct inbound connections; an accepted application connection must
   have a relayed remote multiaddr containing `p2p-circuit`;
6. publish only circuit addresses for a listener, never transport listen
   addresses;
7. do not race native WebRTC or an unrestricted `Node.OpenStream` against the
   circuit-only opener.

Prefer creating a relay-only host without physical listen addresses: its
outbound relay connection and reservation should be sufficient to receive
virtual relayed streams. Prove this with an integration test against the
current go-libp2p version. If an internal transport listener is still required,
never announce its address and block direct inbound traffic with the gater.

If a libp2p `ConnectionGater` is used, do not blindly reject the target in
`InterceptPeerDial`: the target PeerId is also present in a virtual relayed
connection. The check must distinguish direct multiaddrs from `p2p-circuit`
routes. Constructing only circuit candidates and filtering address-level dial
and inbound paths is the primary enforcement; a gater is defense in depth.

### Proposed source changes

- `internal/cli/root.go`: flag, validation, diagnostics, and mode tests;
- `internal/cli/node_config.go`: propagate a typed route policy;
- `p2p/node.go`: `RoutePolicy` or `RelayOnly bool`, address filtering,
  circuit-only announcements, and inbound enforcement;
- P2P stream open helpers: a dedicated circuit-only opener without direct
  racing;
- tests: validation units, address construction, and real integration hosts.

Prefer an enum instead of accumulating booleans:

```go
type RoutePolicy uint8

const (
	RoutePolicyAny RoutePolicy = iota
	RoutePolicyRelayOnly
)
```

`RoutePolicyAny` must preserve current behavior without changes.

### Phase 1 acceptance criteria

- target and relay are both directly reachable: the connection uses only
  `p2p-circuit`;
- target is directly reachable but the relay is down: the command fails and
  does not connect directly;
- listener neither announces a direct address nor accepts a direct stream;
- a pairing token containing direct and relay hints uses only relay hints;
- two explicit relays can provide fallback without opening other outer
  connections;
- raw, PTY, TCP forwarding, and framed UDP work through the relay;
- existing tests without `--relay-only` retain their behavior;
- verbose diagnostics print `route policy: relay-only`.

## Phase 2: trusted WSS gateway and `p2p-nc-lite`

### Trust model

This is deliberately a **trusted gateway**:

- TLS/WSS protects the local-to-gateway hop;
- libp2p Noise/TLS protects the gateway-to-peer hop;
- the gateway sits between these hops and can potentially observe application
  bytes, metadata, and pairing material;
- this is not equivalent to Circuit Relay v2 with end-to-end libp2p encryption;
- users who do not trust the server must select `--relay-only`.

The CLI and documentation must display this warning before enabling the mode.

### Identity model

Every registered device receives a separate persistent Ed25519 identity on the
gateway. Never use one shared PeerId for all tenants: doing so creates logical
port collisions and mixes authorization boundaries.

- generate and store the private key on the gateway with mode `0600` or in an
  encrypted secret store;
- return the PeerId, but never the private key, to the agent;
- one device may listen on multiple logical ports at once;
- different devices may use the same logical port because their PeerIds differ;
- a regular remote `p2p-nc` sees the hosted device PeerId;
- bind pairing tokens to the hosted PeerId and logical port;
- revoking a device must revoke its gateway credential, close sessions, and
  safely delete or archive its hosted identity according to an explicit policy.

Moving a hosted identity between gateway instances is outside the first MVP.

### Gateway P2P network topology

One libp2p host cannot own multiple PeerIds. Every active device therefore gets
a separate `p2p.Node`, started lazily with its hosted private key after a
successful `HELLO`. Never attempt to share one host across device identities.

To avoid random public ports for per-device hosts, they do not listen on
Internet transports directly. Each reserves a slot on one shared Circuit Relay
v2 running on the same VPS or inside trusted gateway infrastructure:

```text
local agent ── WSS 443 ──> gateway control/data service
                              ├── device host A / PeerId A ──┐
                              ├── device host B / PeerId B ──┼──> shared Circuit Relay v2 / WSS 443
                              └── device host C / PeerId C ──┘
remote peer ─────────────────────────────────────────────────> device relay circuit address
```

Gateway WSS and Circuit Relay WSS are separate logical endpoints. A reverse
proxy may publish them on two DNS names, such as `gateway.example.com:443` and
`relay.example.com:443`, on one VPS. This keeps fixed TCP 443 and requires no
per-device ports.

- a hosted listener announces only its circuit address through the shared
  relay;
- a hosted client may use normal route selection to dial the remote peer, while
  every outer socket still originates on the VPS;
- the relay reservation must exist before returning `LISTEN_OK`;
- after agent disconnect, remove stream handlers, close the hosted node, and
  release the reservation after a bounded drain period;
- one active identity means one hosted node; `--max-devices` must account for
  memory, file descriptors, and relay reservation capacity;
- never combine the default 128-reservation relay limit with a larger
  `--max-devices` value without explicitly increasing the limit and load
  testing it.

This internal Circuit Relay hop does not make the gateway opaque: the hosted
libp2p Noise/TLS session still terminates in the per-device node on the VPS.

### Process model

The gateway is one long-running service and calls Go packages directly. Never
start a `p2p-nc` subprocess per session. Proposed layout:

```text
cmd/p2p-nc-lite/        separate small binary
gateway/                embeddable trusted gateway service
protocol/gateway/       WSS framing, CBOR control messages, validation
internal/gatewaycli/    gateway Cobra command and administration
internal/lite/          agent lifecycle and local session adapters
```

Reuse the existing `protocol/admission`, `protocol/datagram`, `protocol/pty`,
and `session` packages instead of copying them.

### Public endpoint

Use one endpoint on TCP 443:

```text
wss://relay.example.com/v1/agent
```

WebSocket subprotocol:

```text
p2p-netcat-gateway-v1
```

TLS may terminate in the gateway or in a trusted reverse proxy. When a reverse
proxy is used, configure body/frame limits, idle timeout, and real client IP
forwarding explicitly. Permit plain `ws://` only in loopback tests.

REST is optional administration only. Listener/client sessions are not created
by unauthenticated `curl`; create them with control frames inside the
authenticated WSS connection.

### Gateway authentication

Keep the gateway credential separate from the P2P pairing token.

Proposed provisioning command:

```bash
p2p-nc gateway token create \
  --data-dir /var/lib/p2p-netcat-gateway \
  --device alex-laptop \
  --expires 720h \
  --output /var/lib/p2p-netcat-gateway/alex-laptop.token
```

The credential uses a separate `pncg1_` prefix, contains at least 256 random
bits, and is stored server-side only as a password hash/KDF result. The agent
sends it in the HTTP header:

```text
Authorization: Bearer pncg1_...
```

Never put the credential in a URL, query string, WebSocket subprotocol, or log.
Validate the agent token file as a private regular file like pairing token
files.

The first MVP may use bearer authentication. mTLS and OIDC are later mutually
exclusive authentication backends.

### WSS multiplexing protocol v1

One WSS connection carries control and multiple data channels. One binary
WebSocket message contains exactly one gateway frame.

The fixed header is 16 bytes:

```text
0               4 5 6     8            12            16
+----------------+-+-+-+-+--------------+--------------+
| magic "PNGW"  |v| type | flags (u16) | channel(u32) |
+----------------+-+-+-+-+--------------+--------------+
| payload length (u32)    | payload ...                |
+-------------------------+----------------------------+
```

Exact layout:

- bytes `0..3`: ASCII `PNGW`;
- byte `4`: version, `1` for this protocol;
- byte `5`: frame type;
- bytes `6..7`: big-endian flags, which must be zero in v1;
- bytes `8..11`: big-endian channel ID; zero is the control channel;
- bytes `12..15`: big-endian payload length;
- remaining bytes: payload of exactly that length.

Limits:

- control payload: at most 64 KiB;
- `DATA`/`DATAGRAM`: at most 64 KiB per frame;
- WebSocket message: at most header plus 64 KiB;
- unknown version, non-zero reserved flags, wrong length, or unknown required
  frame type closes the connection with a protocol error;
- the agent assigns even channel IDs and the gateway assigns odd channel IDs;
  zero is never a data channel.

Encode control payloads as deterministic CBOR maps with integer keys. Codecs
must have golden vectors and reject duplicate keys, indefinite lengths,
unknown required fields, and oversized strings or arrays.

Frame types v1:

| Hex | Name | Direction | Channel | Purpose |
|---:|---|---|---:|---|
| `01` | `HELLO` | agent → gateway | 0 | Version, device name, capabilities |
| `02` | `HELLO_OK` | gateway → agent | 0 | Hosted PeerId, limits, connection ID |
| `03` | `LISTEN` | agent → gateway | 0 | Register a logical service |
| `04` | `LISTEN_OK` | gateway → agent | 0 | Listener is ready and announced |
| `05` | `UNLISTEN` | agent → gateway | 0 | Remove a listener |
| `06` | `DIAL` | agent → gateway | 0 | Dial an exact PeerId/service |
| `07` | `DIAL_OK` | gateway → agent | data ID | P2P stream is ready |
| `08` | `INCOMING` | gateway → agent | data ID | New incoming P2P stream |
| `09` | `ACCEPT` | agent → gateway | data ID | Agent accepts the incoming stream |
| `0a` | `REJECT` | agent → gateway | data ID | Agent rejects before application data |
| `10` | `DATA` | both | data ID | Byte-stream bytes |
| `11` | `DATAGRAM` | both | data ID | Exactly one application datagram |
| `12` | `EOF` | both | data ID | Graceful half-close/write EOF |
| `13` | `CLOSE` | both | data ID | Graceful full close after drain |
| `14` | `RESET` | both | data ID | Abort without drain |
| `15` | `WINDOW_UPDATE` | both | data ID | Credit-based flow control |
| `16` | `PING` | both | 0 | Liveness nonce |
| `17` | `PONG` | both | 0 | Exact liveness nonce response |
| `18` | `ERROR` | gateway → agent | 0 or data ID | Structured bounded error |

`HELLO`, `LISTEN`, and `DIAL` carry monotonically increasing `request_id`
values. The corresponding response repeats it so concurrent control operations
can be matched safely.

Minimum semantic fields:

- `HELLO`: protocol version `1`, device label, supported stream kinds,
  supported session modes, and agent version;
- `HELLO_OK`: hosted PeerId, gateway connection ID, initial window, maximum
  channels, maximum frame size, and idle timeout;
- `LISTEN`: request ID, logical port, `byte`/`datagram` stream kind, keep-open,
  optional pairing token, and explicit relay hints;
- `DIAL`: request ID, exact target PeerId, logical port, stream kind, optional
  pairing token, explicit relay hints, and connect timeout;
- `INCOMING`: logical port, stream kind, and authenticated remote PeerId when
  known;
- `ERROR`: stable machine code plus a bounded English message.

Freeze exact CBOR integer keys and error codes in a separate
`docs/GATEWAY_PROTOCOL.md` when implementation begins. This future document
defines semantics but is not a wire specification until golden vectors exist.

### Admission and pairing

The gateway creates or accepts the libp2p stream, while the lite agent owns the
session mode. Pairing admission bytes pass through the WSS channel to the agent
before application data. The agent reuses `protocol/admission` as client or
server.

The gateway needs the pairing token when private discovery or signaling is
required. Trusted mode may therefore send the token inside the encrypted WSS
`LISTEN`/`DIAL` payload, but:

- keep the token only in session memory;
- never expose the token, rendezvous, or derived keys in logs, metrics, or
  errors;
- clear redundant buffers after route setup where practical;
- still execute admission at the agent rather than considering it complete
  merely because the gateway received the token.

This reduces accidental exposure but does not make the gateway untrusted.

### Session ownership

`p2p-nc-lite`, not the gateway, performs local actions:

- `-i`: create a PTY/ConPTY on the local computer;
- `-e`: run the local command;
- listener `-p`: connect to a local destination;
- client `-p`: open the local forwarding listener;
- `-S`: run the SOCKS session relative to the agent host;
- raw mode: attach the WSS channel to stdin/stdout;
- UDP: preserve datagram boundaries.

The gateway must never interpret `-i` as permission to start a shell on the
VPS. It carries protocol bytes and metadata between the P2P stream and agent
channel.

### `p2p-nc-lite` CLI

Preserve the existing netcat-style semantics as closely as possible. Select
the gateway with a dedicated long option; never reuse `-i` as an address.

Listener example:

```bash
p2p-nc-lite -l -k -i \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token \
  --pairing-token-file /home/alex/.config/p2p-netcat/shell.token \
  34000
```

Client example:

```bash
p2p-nc-lite -i \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token \
  --pairing-token-file /home/alex/.config/p2p-netcat/shell.token \
  12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW \
  34000
```

Identity query:

```bash
p2p-nc-lite id \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token
```

The first MVP supports raw, PTY, and TCP forwarding. Add UDP, SOCKS, and `-e`
after close and flow-control tests are stable, while reserving them in the wire
design from the start.

### Gateway CLI

Proposed startup behind Caddy or nginx:

```bash
p2p-nc gateway \
  --listen 127.0.0.1:8080 \
  --public-url wss://relay.example.com/v1/agent \
  --data-dir /var/lib/p2p-netcat-gateway \
  --max-devices 100 \
  --max-channels-per-device 64 \
  --idle-timeout 2m
```

Runtime messages remain English under repository policy. The server must
handle SIGINT/SIGTERM cleanly: stop upgrades, remove listeners, send a bounded
shutdown error, allow a short frame drain window, then reset streams.

### Flow control and backpressure

A plain `io.Copy` between WebSocket and multiple P2P streams is insufficient.

- initial per-channel send window: 256 KiB;
- `DATA` and `DATAGRAM` consume credit equal to payload length;
- send `WINDOW_UPDATE` only after forwarding bytes to the next consumer;
- zero credit blocks that channel, never the control channel or unrelated data
  channels;
- cap queued unsent data at 256 KiB per channel;
- enforce a separate hard aggregate queue limit per device;
- limit overflow resets the channel, while repeated abuse closes the WSS;
- control frames have priority over data frames;
- one serialized writer goroutine performs WebSocket writes to satisfy the
  selected library's concurrency rules.

For `DATAGRAM`, credit covers the complete payload and every frame contains
exactly one datagram. Never merge or split an application datagram.

### Close semantics

- `EOF` maps to graceful `CloseWrite` and leaves the reverse direction open;
- send `CLOSE` after draining both directions;
- `RESET` is an abort and resets the stream;
- normal Unix PTY `EIO` after the final slave closes is EOF;
- a gateway disconnect resets all channels and local sessions in the MVP;
- automatic resume is outside the MVP; a later version may add a 120-second
  grace period only with a versioned resume protocol and bounded replay buffer.

### Security requirements

Before merge, require:

- TLS 1.2 minimum, preferably TLS 1.3; plain WS only in loopback tests;
- constant-time credential verification;
- per-IP and per-device rate limits for upgrade, authentication, LISTEN, and
  DIAL;
- deadlines for HTTP headers, WebSocket handshake, HELLO, route setup, idle,
  and shutdown;
- limits on devices, listeners, channels, frames, queues, bytes, and session
  TTL;
- exact PeerId, logical port, and stream-kind validation;
- no arbitrary gateway-side TCP destination: destinations belong to the local
  agent or identify a P2P PeerId, never an SSRF proxy from the VPS;
- secret redaction from all logs, errors, traces, and metrics;
- an Origin allowlist for browser use; non-browser agents still require bearer
  authentication;
- CSRF protection on cookie-based administration endpoints;
- no compression (`permessage-deflate` disabled) in the MVP to reduce memory
  and side-channel surface;
- payload-free audit events: device, operation, result, byte counts, duration;
- active WSS and listeners close when a credential is revoked;
- frame/CBOR decoder fuzz tests and race-enabled lifecycle tests.

The gateway binds to loopback by default when it does not terminate TLS. A
public plaintext bind requires a separate explicitly unsafe override and must
not appear in documentation examples.

### Observability

Metrics never contain a full PeerId, token, logical payload, or command
argument. Allowed metrics include:

- active devices/listeners/channels;
- authentication failures by bounded reason;
- route setup latency;
- bytes/frames by stream kind;
- resets, graceful closes, and protocol errors;
- queue utilization and backpressure events.

Replace PeerIds in metrics with keyed, rotating pseudonymous labels. A full
PeerId may appear only in an access-controlled audit log under an explicit
policy.

### WSS MVP acceptance criteria

The end-to-end integration matrix must prove:

1. regular client ↔ hosted listener/agent transfers bidirectional raw bytes;
2. lite client/gateway ↔ regular listener transfers bidirectional raw bytes;
3. pairing succeeds, wrong tokens fail, and no application bytes precede
   admission;
4. PTY resize, shell `exit`, final output, graceful EOF, and abort work;
5. TCP forwarding works in both directions;
6. two devices using the same logical port and different hosted PeerIds do not
   conflict;
7. one device cannot receive another device's stream;
8. WSS disconnect cleans listeners, P2P streams, goroutines, and local sockets;
9. a slow consumer is bounded by the flow window without unbounded memory;
10. malformed, oversized, and unknown frames fail deterministically;
11. revoked or expired gateway credentials cannot open or preserve a session;
12. gateway logs contain neither gateway credentials nor pairing tokens;
13. `go test -race` finds no race during concurrent open/close/reset;
14. a 64-channel soak runs for at least one hour without goroutine or memory
    leaks.

After raw/PTY/TCP MVP, add separate acceptance tests for UDP packet boundaries,
idle associations, SOCKS authorization, and local `-e` execution.

## Implementation order

Each item should be a separate reviewable change:

1. add typed `RoutePolicy` and unit tests without changing the CLI;
2. add `--relay-only`, validation, and circuit-only integration tests;
3. update English/Russian CLI documentation for relay-only;
4. freeze `GATEWAY_PROTOCOL.md`, constants, codecs, golden vectors, and fuzz
   tests;
5. implement the gateway credential store and hosted device identities;
6. implement authenticated HELLO and an empty multiplexed WSS connection;
7. implement LISTEN/DIAL/INCOMING and raw byte channels;
8. add admission/pairing and exact PeerId checks;
9. add flow control, EOF/CLOSE/RESET, and cleanup stress tests;
10. implement `cmd/p2p-nc-lite` raw mode;
11. reuse local PTY and TCP forwarding adapters;
12. complete a security review before UDP/SOCKS/exec;
13. add UDP with packet-boundary tests;
14. add reconnect/resume only as protocol v2 or a negotiated v1 extension,
    never by silently changing v1 semantics;
15. consider a browser PWA adapter after the Go agent is stable.

## Verification

Run targeted tests while implementing, then the full project suite:

```bash
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go test ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go test -race -count=1 -timeout=25m ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go vet ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go run ./cmd/webrtc-soak --profile smoke
bash scripts/sync-wiki_test.sh
```

Gateway integration tests should use a loopback TLS server with a disposable
test CA; production tests must not depend on an external domain.

## Decisions to freeze before coding

Before WSS implementation, record a separate design review for:

- the concrete WebSocket library and its concurrency/close semantics;
- the deterministic CBOR library and configuration;
- the encrypted-at-rest backend for hosted identities;
- reverse-proxy trust and trusted proxy CIDRs;
- whether the MVP sends pairing tokens to the gateway for private discovery or
  limits itself to explicit relay routes;
- `p2p-nc-lite` packaging and acceptable binary size;
- hosted identity deletion policy after device revocation.

These decisions may not weaken the documented trust warnings, framing limits,
route guarantees, or compatibility invariants.
