# p2p-netcat-core

**English** | [Русский](README.RU.md)

The interaction between the library, CLI, and browser Worker is documented in
[`docs/ARCHITECTURE.md`](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/ARCHITECTURE.md).

The browser-safe shared core of `p2p-netcat`. The package does not use Node.js
APIs and can be imported by a Web Worker and other JavaScript clients. The Go
CLI implements the same documented wire formats.

The package owns:

- logical ports and protocol IDs;
- PeerId and multiaddr normalization;
- relay address validation;
- Circuit Relay dial-plan construction;
- browser-compatible address detection;
- a shared transport preference order;
- the application PubSub discovery topic and interval;
- the common WebRTC STUN pool;
- browser-safe PTY data/resize framing and incremental decoding;
- project-owned WebRTC binary framing and `RTCPeerConnection` lifecycle;
- signed Nostr and WebTorrent tracker signaling adapters;
- canonical pairing tokens, rotating rendezvous keys, and AES-GCM envelopes;
- mutual stream admission and signed RouteRecord codecs;
- signaling-independent WebRTC action mapping;
- negotiated WebRTC byte-window flow control, acknowledgements, keepalive, EOF,
  abort, and reconnect semantics.

Creating a libp2p node, querying the DHT, Web Worker RPC, and stdin/stdout remain
in platform-specific packages.

## Exported API

| Function | Purpose |
|---|---|
| `validateService(value)` | Validates a logical port in the `1..65535` range |
| `protocolForService(service)` | Builds `/p2p-netcat/1.0.0/{service}` |
| `encodePtyData(value)` | Frames keyboard or terminal bytes for an interactive session |
| `encodePtyResize(columns, rows)` | Frames terminal dimensions |
| `decodePtyResize(value)` | Decodes and validates terminal dimensions |
| `PtyFrameDecoder` | Incrementally decodes PTY frames split across transport chunks |
| `normalizePeerId(value)` | Validates and canonicalizes a PeerId |
| `normalizeMultiaddr(value)` | Validates and canonicalizes a multiaddr |
| `normalizeRelayAddress(value, options)` | Applies relay, WS/WSS, and secure-context checks |
| `relayedTargetAddress(relay, peerId, options)` | Returns the target Circuit Relay multiaddr |
| `createRelayDialPlan(input)` | Returns an immutable dial plan |
| `browserDialableAddress(address, options)` | Checks whether a browser can dial an address |
| `addressRank(address)` | Returns a numeric transport rank |
| `preferDialAddresses(a, b)` | Comparator for sorting multiaddrs |
| `PUBSUB_DISCOVERY_TOPIC` | App-specific GossipSub discovery topic |
| `PUBSUB_DISCOVERY_INTERVAL_MS` | Announcement repeat interval |
| `DEFAULT_STUN_URLS` | Immutable shared STUN URL list |
| `defaultRtcConfiguration()` | Returns a fresh WebRTC configuration using the shared STUN pool |
| `webRtcRoomId(peerId, service)` | Builds the deterministic WebRTC room |
| `webRtcAuthPayload(...)` | Builds a domain-separated signed challenge |
| `encodeWebRtcAuthResponse(...)` | Encodes the public key and signature |
| `decodeWebRtcAuthResponse(...)` | Validates and decodes the response |
| `signWebRtcAuthResponse(...)` | Signs a challenge with a libp2p private key |
| `verifyWebRtcAuthResponse(...)` | Verifies the signature and exact requested PeerId |
| `WebRtcStream` | Adapts an action channel to backpressure, recovery, and EOF semantics |
| `createWebRtcActionHub(room, options)` | Maps the data/control actions and peer lifecycle to shared streams |
| `NativeWebRtcPeer` | Owns one ordered reliable data channel and its binary frame protocol |
| `createNostrSignalingSession(options)` | Opens signed, room-scoped Nostr signaling |
| `createTorrentSignalingSession(options)` | Opens WebTorrent WebSocket tracker signaling |
| `startNativeWebRtcListener(options)` | Answers offers and exposes authenticated `WebRtcStream` instances |
| `connectNativeWebRtc(options)` | Races signaling sessions, authenticates the PeerId, and reconnects the stream |
| `createPairingToken(input)` / `decodePairingToken(value)` | Creates or validates a canonical `pnc1_` bearer token |
| `pairingProviderCids(token, options)` | Derives previous/current/next private IPFS provider CIDs |
| `sealPairingPayload(...)` / `openPairingPayload(...)` | Protects a binary payload with a domain-separated AES-256-GCM key |
| `authenticateClientStream(...)` / `authenticateServerStream(...)` | Performs mutual token admission without exposing frames to the application |
| `signRouteRecord(...)` / `verifyRouteRecord(...)` | Signs and validates versioned route metadata against an exact PeerId |

The pairing formats are specified independently of JavaScript and include
fixed interoperability vectors for the future Go implementation. See the
[private pairing and wire protocol](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/PAIRING_PROTOCOL.md).

The order is WebRTC Direct, QUIC v1, WebTransport, WSS, WS, TCP, other
addresses, and Circuit Relay. A transport appearing in the common ranking does
not imply that every runtime implements it.

`defaultRtcConfiguration()` returns a fresh object on every call because WebRTC
implementations may normalize or mutate their configuration. The current pool
contains the five Google STUN endpoints plus CounterPath, Sipgate, VoIPBuster,
and InternetCalls. STUN discovers NAT mappings; it is not a TURN relay and
cannot guarantee a direct route through symmetric or restrictive NATs.

`WebRtcStream` advertises flow-control support with `flow:1`. Two current
peers limit unacknowledged data to 256 KiB by default and send `ack:<bytes>`
only when the async consumer advances after processing a chunk. The handshake
is backward compatible: if the remote side does not advertise support, the
stream keeps the legacy transport behavior. A lightweight `ping`/`pong`
control message keeps an otherwise idle data channel active.

`peerDisconnected()` starts a bounded recovery window instead of turning an
unexpected WebRTC peer loss into EOF. While `connectionStatus` is
`reconnecting`, writes remain in the existing bounded queue.
`peerReconnected()` resumes that same logical stream and resets stale flow
credits. The default recovery window used by p2p-netcat is
`WEBRTC_RECONNECT_GRACE_MS` (120 seconds). Explicit EOF and `abort()` still
close immediately.

Core now contains the native signaling adapters, SDP controller, binary
data-channel protocol, and authenticated endpoint controller. The CLI and PWA
use this implementation exclusively. The former implementation-specific aliases
have been removed; all public names are transport-neutral WebRTC APIs. See the
[migration record](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/WEBRTC_MIGRATION.md)
for the protocol and validation matrix.

Every `NativeSignalingSession` exposes a read-only `trickleIce` capability.
Nostr sessions set it to `true`; tracker sessions set it to `false`. Endpoint
controllers therefore publish Nostr candidates as they appear while waiting
for complete SDP before a WebTorrent announce. Callers that provide custom
signaling sessions should set this field explicitly.

Minimal native client setup:

```js
import {
  connectNativeWebRtc,
  createNostrSignalingSession,
  createSignalingPeerId,
  createTorrentSignalingSession,
  defaultRtcConfiguration,
  verifyWebRtcAuthResponse,
  webRtcRoomId
} from 'p2p-netcat-core'

export async function connectToP2pNetcat (targetPeerId, logicalPort = 31337) {
  const roomId = webRtcRoomId(targetPeerId, logicalPort)
  const signalingPeerId = createSignalingPeerId()
  const signalingSessions = await Promise.all([
    createNostrSignalingSession({ roomId, peerId: signalingPeerId, WebSocket }),
    createTorrentSignalingSession({ roomId, peerId: signalingPeerId, WebSocket })
  ])

  const connection = connectNativeWebRtc({
    signalingSessions,
    RTCPeerConnection,
    rtcConfig: defaultRtcConfiguration(),
    verifyAuthResponse: (value, challenge) =>
      verifyWebRtcAuthResponse(value, targetPeerId, logicalPort, challenge)
  })

  return {
    stream: await connection.promise,
    close: () => connection.close()
  }
}
```

The function verifies the signed challenge against the exact PeerId and logical
port requested by the caller.

Install the standalone browser-safe package when it is the only functionality
needed:

```bash
npm install p2p-netcat-core
```

Example of constructing a shared dial plan:

```js
import { createRelayDialPlan } from 'p2p-netcat-core'

const plan = createRelayDialPlan({
  peerId: '12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9',
  service: 31337,
  relay: '/dns4/relay.example/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW',
  requireWebSocket: true,
  secureContext: true
})

console.log(plan.destination)
console.log(plan.protocol)
```
