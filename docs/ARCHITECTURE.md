# go-p2p-netcat architecture

[Русская версия](ARCHITECTURE.RU.md)

View the product and technical presentation as a portrait [mobile PDF](p2p-netcat-product-technical-overview-en-mobile.pdf),
the original [16:9 PDF](p2p-netcat-product-technical-overview-en.pdf), or download the editable
[PPTX](p2p-netcat-product-technical-overview-en.pptx) for a visual overview
of real-world use cases, routing, native WebRTC, UDP forwarding, WireGuard full tunnel,
deployment, and reliability checks.

This repository combines the canonical Go CLI/network implementation with the
browser-safe TypeScript core and static PWA.

## Compatibility boundary

- identity: libp2p protobuf Ed25519 private/public keys and stable PeerId;
- application protocol: `/p2p-netcat/1.0.0/<logical-port>`;
- UDP forwarding protocol: `/p2p-netcat/udp/1.0.0/<logical-port>`, with each
  datagram encoded as a big-endian uint16 length followed by the exact payload;
- PTY frames: one-byte type plus big-endian 32-bit payload length;
- pairing: deterministic CBOR `pnc1_` tokens, HKDF-SHA-256, AES-256-GCM,
  rotating rendezvous, and the fixed mutual admission handshake;
- native WebRTC: protocol v2, `p2p-netcat-v2` ordered data channel,
  authentication-response v2 with the
  `p2p-netcat/native-webrtc-auth/v2` transcript domain, and identical control
  frames.

## Go CLI route selection

The Go host supports TCP, QUIC v1, WebSocket, libp2p WebRTC Direct, Noise/TLS,
Yamux, Circuit Relay v2, mDNS, signed GossipSub discovery, and IPFS Amino DHT.
PeerId is an identity, not a route: mDNS, GossipSub, DHT provider records,
bootstrap peers, explicit multiaddrs, or a relay must provide an address.

Dial order is native WebRTC Direct, QUIC, WebTransport, WSS, WS, direct TCP,
Circuit Relay, then other addresses. The custom native WebRTC branch runs in
parallel with libp2p and races Nostr and WebTorrent signaling. The first
authenticated route wins.

Pairing-token mode suppresses public discovery, derives private DHT/signaling
rendezvous, encrypts signaling, and authenticates the stream before exposing
application bytes.

The Go CLI can wrap a `pnc1_` bearer token in a password-encrypted `pnc1e_`
storage envelope. Unlocking restores the unchanged `pnc1_` token, so this
at-rest format does not alter the Go/browser pairing or network wire protocol.

UDP mode races the standard libp2p datagram stream with the custom native
WebRTC stream. Native WebRTC uses Nostr/WebTorrent signaling and ICE/STUN for
NAT traversal without a user-operated media relay. Direct QUIC and libp2p
WebRTC Direct are also available, while TCP, WSS, Tor through a TCP relay, and
Circuit Relay v2 provide UDP-over-stream fallbacks.

## Native WebRTC

The listener signs a versioned transcript with its persistent libp2p identity.
The transcript binds the exact expected PeerId and service to the challenge,
signaling session, roles, and hashes of both SDP descriptions. The SDP hashes
bind the proof to the negotiated DTLS fingerprints. Data and control frames
implement EOF, abort, keepalive, acknowledgements, and a 256 KiB flow window.

An unexpected disconnect starts a 120-second reconnect grace period. New
offers reuse the signaling peer identity, attach the replacement Pion data
channel to the existing logical stream, and exchange `resume`/`flow:1` so
queued writes and PTY processes survive transient ICE failures.

Nostr uses short-lived signed kind-25050 events scoped by a hashed topic.
WebTorrent uses a 20-character tracker `peer_id`, bounded offers, and complete
non-trickle SDP. Both reconnect their WebSockets with bounded backoff.

## Browser PWA

The React UI talks to a module Web Worker. The worker owns browser libp2p,
IndexedDB route caching, GossipSub, Delegated Routing, DHT, and relay dialing.
Native WebRTC runs beside that branch using the browser implementation in
`packages/core`. A Service Worker only caches the static shell.

An HTTPS browser accepts WebTransport, native WebRTC, or secure WebSocket
routes; it cannot dial ordinary TCP, QUIC, or insecure WS.

## Sessions

Every accepted stream is connected to one of:

- raw stdin/stdout;
- shell command execution;
- local or remote TCP forwarding;
- fixed-destination UDP forwarding with packet boundaries and one association
  per local source endpoint;
- SOCKS4/4a/5 CONNECT;
- interactive PTY (Unix PTY or Windows ConPTY).

`-w` limits discovery/connect time and, when explicitly supplied for raw mode,
also acts as an inactivity timeout. UDP associations expire after five minutes
by default; `--udp-idle-timeout 0` disables expiration. `-k` keeps a listener
open for additional sessions.

## Relay

Explicit relays are connected as permanent peers. Without an explicit relay,
a listening private host can reserve a suitable connected Circuit Relay v2
peer discovered through the mesh. Relay servers use 128 reservations with a
two-hour/128-MiB default limit and GossipSub peer exchange.

## Main source map

| Path | Responsibility |
|---|---|
| `p2p/` | libp2p host, transports, discovery, DHT, relay reservations |
| `nativewebrtc/` | Pion endpoint, signaling, authentication, reconnecting stream |
| `protocol/` | pairing, admission, datagram, PTY, and signed route wire formats |
| `session/` | raw, exec, TCP/UDP forwarding, SOCKS, PTY, ConPTY |
| `relay/` | public embeddable relay API |
| `internal/cli/` | CLI validation and listener/client orchestration |
| `packages/core/` | browser-safe protocol and native WebRTC library |
| `web/` | static bilingual PWA |
