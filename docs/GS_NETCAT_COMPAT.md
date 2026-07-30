# gs-netcat-compatible CLI modes

**English** | [Русский](GS_NETCAT_COMPAT.RU.md)

This document describes the p2p-netcat implementation of the gs-netcat options
`-d`, `-p`, `-q`, `-S`, `-T`, and `-i`. The option meanings follow gs-netcat,
while peer rendezvous, authentication, and streams remain based on libp2p
PeerId.

## Option mapping

| Option | p2p-netcat behavior |
|---|---|
| `-d, --destination HOST` | Destination host for server-side TCP forwarding. Requires listener mode and `-p`. |
| `-p, --port PORT` | In listener mode, the remote destination port. In client mode, a local TCP listen port that forwards each connection to a new P2P stream. |
| `-q, --quiet` | Suppresses p2p-netcat diagnostics on stderr. Application data on stdout is unchanged. |
| `-S, --socks` | Runs a SOCKS4, SOCKS4a, and SOCKS5 CONNECT proxy on the listener side. |
| `-T, --tor` | Runs a client through `torsocks` and requires an explicit TCP/WS/WSS Circuit Relay. |
| `-i, --interactive` | Listener: spawn a true PTY login shell per client. Client: raw TTY with resize and control-character forwarding. |

The old internal transport-port meaning of `-p` moved to
`--transport-port`. The old short identity flag moved from `-i` to `-I`; the
long `--identity` spelling did not change. EOF delay remains available as
`--quit-delay` without the old `-q` alias.

## TCP forwarding: `-d` and `-p`

The listener connects each accepted P2P stream to the configured TCP target:

```bash
p2p-nc -l -d 192.168.6.7 -p 22 31337
```

If `-d` is omitted while listener `-p` is present, the destination host is
`127.0.0.1`. The final positional value is still the p2p-netcat logical port,
not a TCP port.

The client opens a local loopback listener. Each local TCP connection creates
an independent Yamux/libp2p stream to the same PeerId and logical port:

```bash
p2p-nc -p 2222 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
ssh -p 2222 root@127.0.0.1
```

The local listener binds to `127.0.0.1` by default. Exposing it on another
interface is explicit:

```bash
p2p-nc --bind 0.0.0.0 -p 2222 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Multi-connection `-p` uses multiplexed libp2p streams rather than the current
single-stream WebRTC adapter. When direct libp2p routing is unavailable, pass
the same Circuit Relay to the forwarding server and client.

## SOCKS proxy: `-S`

Start the remote SOCKS endpoint:

```bash
p2p-nc -l -S 31337
```

Expose it locally through the ordinary client forwarding mode:

```bash
p2p-nc -p 1080 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

Supported protocols are SOCKS4 CONNECT, SOCKS4a CONNECT, and SOCKS5 CONNECT
with the no-authentication method. SOCKS BIND, UDP ASSOCIATE, username/password
authentication, and UDP forwarding are not implemented.

## Interactive PTY: `-i`

Start a login shell on the listener:

```bash
p2p-nc -l -i 31337
```

Connect from a real terminal:

```bash
p2p-nc -i 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

The server uses `node-pty` to create a pseudoterminal and starts `$SHELL -l`
on Unix or PowerShell on Windows. The client puts stdin into raw mode. `Ctrl-C`
and other terminal bytes therefore reach the remote PTY instead of being
handled by the local p2p-netcat process. Window size changes are sent as PTY
control frames. Type `exit` to close the shell, or press `Ctrl-e q` to close the
client stream.

The static web client can also connect to this listener. Enable
**Interactive PTY -i** before connecting; its xterm widget uses the shared PTY
codec, forwards keyboard input and resize events, and implements `Ctrl-e q`.
The switch must remain off for a listener started without `-i` because the
ordinary and interactive modes intentionally use different wire encodings.

`-i` requires a TTY and cannot be combined with `-e`, `-S`, or client `-p`.
Listener PTY mode remains active and can serve multiple clients concurrently.
The gs-netcat `Ctrl-e c` command console and its `get`/`put` file-transfer
commands are not implemented yet; p2p-netcat currently implements raw PTY,
resize, `Ctrl-e q`, and ordinary stream/file transfer outside PTY mode.

## Quiet mode: `-q`

`-q` suppresses status, warnings, discovery diagnostics, session failures, and
the final CLI error on stderr. It does not discard bytes received from the peer
or produced by a forwarded TCP connection. Do not combine `-q` with `-v` when
diagnostics are needed; quiet mode wins.

## Tor routing: `-T`

Tor only carries TCP. To prevent a silent privacy bypass, `-T`:

1. is accepted only in client mode;
2. requires an explicit `--relay` multiaddr using TCP, WS, or WSS;
3. rejects UDP/QUIC relay addresses;
4. disables QUIC, native/legacy WebRTC, STUN, mDNS, PubSub, bootstrap, and DHT;
5. re-executes the complete client under `torsocks -i` for Tor circuit
   isolation.

Example:

```bash
P2P_NETCAT_TOR_HOST=127.0.0.1 \
P2P_NETCAT_TOR_PORT=9050 \
p2p-nc -T \
  --relay /ip4/203.0.113.10/tcp/9090/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW \
  12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Configuration variables:

| Variable | Default | Meaning |
|---|---:|---|
| `P2P_NETCAT_TOR_HOST` | `127.0.0.1` | Numeric IP address of the Tor SOCKS service. |
| `P2P_NETCAT_TOR_PORT` | `9050` | Tor SOCKS port. |
| `P2P_NETCAT_TORSOCKS_COMMAND` | `torsocks` | Alternate wrapper executable. |
| `GSOCKET_SOCKS_IP` | — | gs-netcat-compatible fallback name for the host. |
| `GSOCKET_SOCKS_PORT` | — | gs-netcat-compatible fallback name for the port. |

Tor mode is currently supported on Linux and macOS where `torsocks` is
available. The client-to-relay TCP connection travels through Tor; the relay
then carries the end-to-end encrypted libp2p circuit to the server. This does
not make application-level behavior anonymous: the relay sees PeerIds, timing,
and traffic volume, and the destination reached through `-d` or `-S` sees the
server's network identity.

## Security boundary

Noise, QUIC TLS, and the interactive signed WebRTC challenge authenticate the
server PeerId and protect stream contents in transit. They do not authorize a
client. In the current version, any peer that knows a listener PeerId and
logical port can attempt to use `-d`, `-S`, `-e`, or `-i`.

Consequently:

- keep the PeerId and service private when exposing privileged capabilities;
- bind client `-p` to loopback unless LAN exposure is intentional;
- run PTY and command modes under an unprivileged, isolated account;
- restrict destination access with the host firewall;
- do not treat `-q` as stealth, authentication, or traffic obfuscation.

PeerId allowlists and an application authorization layer are planned but are
not part of this implementation.
