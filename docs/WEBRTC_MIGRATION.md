# Native WebRTC migration record

**English** | [Русский](WEBRTC_MIGRATION.RU.md)

`p2p-netcat-core` owns the WebRTC protocol, connection controller, and two
public-infrastructure signaling adapters. The Trystero runtime, adapters,
compatibility API aliases, UI switch, CLI flag, and npm dependencies have been
removed. Node.js and the static PWA now run the same native signaling design.

Removing Trystero does not remove the need for rendezvous infrastructure. Two
computers behind NAT cannot exchange SDP from a PeerId alone. The new code uses
public Nostr relays and WebTorrent WebSocket trackers transparently, but no
p2p-netcat server processes or server-side scripts are required.

## Current status

| Stage | Status |
|---|---|
| Signaling-independent byte stream, flow control, EOF, keepalive, and reconnect | Implemented in core |
| Native `RTCPeerConnection` and ordered reliable `RTCDataChannel` controller | Implemented in core |
| Signed Nostr signaling adapter | Implemented in core |
| WebTorrent WebSocket tracker signaling adapter | Implemented in core |
| Nostr trickle ICE with bounded candidate reordering | Implemented in core |
| CLI and static PWA integration | Native-only |
| Automated local WebRTC soak matrix | Weekly and manually dispatched on Linux/macOS |
| Removal of Trystero npm dependencies and adapters | Complete |
| Removal of implementation-specific compatibility aliases | Complete |

The browser still consists only of static assets suitable for GitHub Pages.
Node.js supplies `RTCPeerConnection` through `@roamhq/wrtc`; browsers use their
built-in implementation.

## Native connection algorithm

1. The listener derives a deterministic room from its persistent PeerId and
   logical port.
2. Both sides hash the versioned room into a non-reversible signaling topic.
3. Each process creates one random 20-character signaling identity shared by
   its Nostr and tracker adapters.
4. The Nostr attempt publishes its SDP immediately and trickles ICE candidates
   as signed events. Candidates that arrive before their offer are retained in
   a bounded 20-second listener cache. The WebTorrent attempt waits for ICE
   gathering and publishes complete SDP because tracker announces carry offers
   and answers rather than arbitrary candidate messages.
5. The first answer that opens `p2p-netcat-v2` starts a 32-byte challenge.
6. The server signs a domain-separated payload with its persistent libp2p
   Ed25519 key. The client reconstructs the PeerId and verifies that it is
   exactly the requested PeerId.
7. Only after successful verification does the client send `AUTH_READY`.
   Therefore an unverified candidate cannot start a PTY or application stream
   on the listener.
8. The first authenticated adapter wins. Losing Nostr/tracker peer connections
   are closed before application data is exposed.
9. A temporary data-channel loss keeps the same `WebRtcStream` and PTY for up
   to 120 seconds. Reconnection rebinds a new data channel to that stream.

There is no offer glare in this protocol: clients create offers and listeners
only answer them. Each retry has a unique session ID, and tracker/Nostr
duplicates are filtered before reaching the endpoint controller.

## Native wire protocol

Every `RTCDataChannel` message is binary:

```text
+---------+------------+--------------------------+
| version | frame type | payload                  |
+---------+------------+--------------------------+
| 0x02    | 0x00       | application bytes        |
| 0x02    | 0x01       | UTF-8 stream control     |
| 0x02    | 0x02       | authentication challenge |
| 0x02    | 0x03       | public key + signature   |
| 0x02    | 0x04       | authentication accepted  |
+---------+------------+--------------------------+
```

The stream control payload supports `flow:1`, `ack:<bytes>`, `resume`,
`ping`/`pong`, `eof`, and `abort`. `RTCDataChannel.bufferedAmount` provides the
first backpressure boundary; `WebRtcStream` adds a 256 KiB end-to-end window so
a fast PTY cannot grow browser memory without bound.

## Signaling adapters

### Nostr

The adapter opens several public relay WebSockets, creates an ephemeral Schnorr
key, and publishes signed kind `25050` events. Events contain a versioned room
tag, have a 120-second acceptance window, and are checked for canonical event
ID and signature before their SDP is accepted. The server PeerId is still
authenticated inside WebRTC; the ephemeral Nostr key authenticates only the
integrity of a signaling event.

Nostr has the `trickleIce` capability enabled. Offer/answer and candidate
events may be delivered through different relays and out of order. The
endpoint accepts at most 128 candidate-only session keys and 32 candidates per
attempt, expiring each unresolved group after 20 seconds. This bounds memory
usage while allowing a candidate received before its offer to remain useful.

### WebTorrent trackers

The adapter derives a deterministic 20-byte tracker `info_hash`, maintains a
bounded offer pool, routes answers with `offer_id` and `to_peer_id`, announces
periodically, deduplicates tracker replies, and reconnects with exponential
backoff. User traffic never passes through a tracker.

## Public core API

```js
import {
  NativeWebRtcPeer,
  WebRtcStream,
  connectNativeWebRtc,
  createNostrSignalingSession,
  createSignalingPeerId,
  createTorrentSignalingSession,
  defaultRtcConfiguration,
  startNativeWebRtcListener,
  webRtcRoomId
} from 'p2p-netcat/core'
```

The same exports are available from the standalone `p2p-netcat-core` package.
The signaling functions accept injected `WebSocket` and
`RTCPeerConnection` constructors, which makes them browser-safe and testable
without Node.js globals.

## Removal result

There is no delayed legacy branch. CLI and PWA clients race only the complete
libp2p route against native WebRTC. Native WebRTC races the independent Nostr
and WebTorrent signaling sessions internally, authenticates the requested
PeerId, and closes every losing attempt.

Verbose CLI runs expose the exact production path:

```bash
p2p-nc -l -i -v 31337
p2p-nc -i -v 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

The PeerId in the second command must be replaced with the value printed by
the listener. The removed `--no-trystero` option and PWA native-only switch are
unnecessary because this is now the only WebRTC implementation.

The authentication transcript intentionally retains its historical
`p2p-netcat/trystero-auth/v1` domain string. Changing it would break wire
compatibility between existing native peers and the planned Go port. It is a
frozen protocol value, not a package import or runtime dependency.

## Automated soak matrix

The repository contains a deterministic runner built on real
`RTCPeerConnection` and `RTCDataChannel` objects from `@roamhq/wrtc`:

```bash
npm run soak:webrtc -- \
  --profile smoke \
  --report artifacts/webrtc-soak-local.json
```

Profiles:

| Profile | Iterations per scenario | Payload per direction |
|---|---:|---:|
| `smoke` | 1 | 64 KiB |
| `ci` | 2 | 512 KiB |
| `soak` | 12 | 8 MiB |

`--iterations` and `--payload-bytes` override a profile.
`--scenarios nostr-trickle,reconnect-same-stream` selects a subset. Every
transfer is binary, bidirectional, flow-controlled, and verified by SHA-256.
The runner covers:

- Nostr trickle ICE with candidates intentionally able to overtake the offer;
- WebTorrent-compatible complete non-trickle SDP;
- simultaneous adapter attempts and duplicate offer delivery;
- one signaling adapter failing while the other wins;
- forced peer-connection loss, rebinding, and data transfer through the same
  logical `WebRtcStream`.

`.github/workflows/webrtc-soak.yml` runs weekly with the `soak` profile on
Ubuntu and macOS. A manual dispatch can select `smoke`, `ci`, or `soak`, and
each job uploads its JSON report.

This matrix validates the core state machine, real local ICE/data channels,
backpressure, adapter capability split, failure isolation, and reconnect. It
does not simulate public Nostr/tracker outages, a browser lifecycle, geographic
latency, or real NAT/firewall combinations. Those remain release gates rather
than claims made by the automated runner.

The dependency removal is complete. Continue running this real-network matrix
as an ongoing stability gate:

- browser to Linux CLI and macOS CLI to Linux CLI across countries;
- Chrome, Firefox, and Safari;
- common, symmetric, and restrictive NAT;
- background tabs and sleep/wake;
- high-volume PTY output for several hours;
- repeated network loss and recovery inside the 120-second window;
- current browser to current CLI in both directions where applicable.

STUN discovers NAT mappings but is not a relay. Symmetric NAT or blocked UDP
can still require TURN or a configured libp2p Circuit Relay. Public relays and
trackers are third-party infrastructure: they can observe signaling topics,
timing, and SDP/ICE candidates, but not application bytes in the encrypted
peer-to-peer channel. They cannot provide a 100% availability guarantee.
