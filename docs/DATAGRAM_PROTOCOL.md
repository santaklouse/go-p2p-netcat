# Datagram forwarding protocol

[Русская версия](DATAGRAM_PROTOCOL.RU.md)

## Goal

The datagram protocol carries fixed-destination UDP services such as
WireGuard, OpenVPN UDP, DNS, and game traffic through the same PeerId discovery,
pairing authentication, and relay infrastructure as p2p-netcat streams.

It must:

- preserve every UDP datagram boundary and zero-length datagrams;
- return replies to the exact local source endpoint that sent the request;
- work through direct TCP, QUIC, WebSocket, libp2p WebRTC Direct, and Circuit
  Relay v2;
- support UDP-over-TCP when the outer network blocks UDP;
- keep the existing `/p2p-netcat/1.0.0/<logical-port>` protocol unchanged.

SOCKS5 UDP ASSOCIATE, broadcast/multicast, arbitrary per-packet destinations,
and an unreliable native QUIC datagram mode are outside this version.

## Protocol identifier

UDP forwarding uses:

```text
/p2p-netcat/udp/1.0.0/<logical-port>
```

The separate identifier prevents a UDP client from accidentally sending framed
packets to a raw, PTY, command, SOCKS, or TCP-forwarding listener. The logical
port remains in the range `1..65535`. Pairing tokens continue to bind the
PeerId and logical port; the normal admission handshake runs before any
datagram frames.

## Frame format

Each stream contains a sequence of frames:

```text
0               1               2
+---------------+---------------+-----------------------------+
| payload length, uint16 BE     | payload, exactly N bytes    |
+---------------+---------------+-----------------------------+
```

- header size: 2 bytes;
- payload length: `0..65535`;
- byte order: network/big-endian;
- no padding, address field, checksum, or implicit delimiter;
- EOF in a header or payload terminates the association.

UDP already has its own checksum. The P2P transport provides authenticated
encryption and stream integrity, so another frame checksum would add cost
without detecting a new failure class.

## Association model

The client binds one local UDP port. Its association key is the complete local
source address and port returned by the UDP socket.

1. The first packet from a source creates a P2P datagram stream.
2. Packets from that source are queued in arrival order while dialing.
3. The listener opens one connected UDP socket to its configured fixed target.
4. Target replies are framed on the same P2P stream and written back to the
   original local source.
5. A different source endpoint receives a different P2P stream and remote UDP
   socket.
6. EOF, cancellation, an I/O error, or the idle timeout removes the
   association. A later packet can create it again.

This design avoids sending local source addresses across the network and
isolates congestion and failure between local applications. A single
address-tagged multiplexed stream was considered, but it would share
head-of-line blocking and failure across all UDP clients and would require a
more complex authorization boundary.

The implementation limits one local forwarder to 256 simultaneous source
associations and queues at most 256 packets while a stream is being opened or
temporarily backpressured. UDP semantics permit dropping packets when those
bounds are exceeded.

## Transport variants

### Reliable framing over a libp2p stream — implemented

This is the common mode used by `-u`. It works over every route supported by a
standard Go libp2p stream:

- QUIC and libp2p WebRTC Direct when UDP is reachable;
- TCP and WSS through restrictive firewalls;
- Circuit Relay v2;
- TCP/WSS relay access through Tor.

The benefit is one protocol and security model for all routes. The cost is
ordered reliable delivery. On a TCP route, one lost outer segment delays later
datagrams. This is especially visible when the tunneled VPN carries TCP.

### Native unreliable QUIC datagrams — considered, not implemented

Native QUIC datagrams could avoid ordered-stream head-of-line blocking on a
direct route. They cannot currently provide the same behavior through Circuit
Relay v2, TCP/WSS, Tor, or the browser/native adapter. A future optional
protocol would also need explicit peer capability negotiation, loss/reordering
semantics, datagram size discovery, and a fallback to the reliable framing
protocol.

### One P2P stream per packet — rejected

Opening and authenticating a stream for every UDP packet would preserve packet
boundaries but creates excessive allocations, multistream negotiation,
admission handshakes, and relay load. A long-lived association is substantially
cheaper and matches WireGuard's stable endpoint behavior.

## WireGuard operating guidance

- Bind the client forwarder to loopback unless LAN exposure is intentional.
- Use a local forwarding port different from the WireGuard interface's own
  `ListenPort`.
- Set `PersistentKeepalive = 25` for peers behind NAT and to keep the default
  five-minute p2p-netcat association active.
- Use `--udp-idle-timeout 0` only when unlimited idle lifetime is required.
- Start with `MTU = 1280`, then increase it after path testing.
- Prefer direct QUIC or libp2p WebRTC Direct; use TCP/WSS or a relay when
  reachability matters more than latency.
- Use a private pairing token for a listener that can reach a non-public UDP
  service.

## Browser boundary

The static browser client does not expose a local UDP socket and therefore
cannot provide this forwarding mode. The custom Nostr/WebTorrent native WebRTC
adapter is also excluded from UDP associations in this version. Standard
libp2p WebRTC Direct remains available to the Go CLI.
