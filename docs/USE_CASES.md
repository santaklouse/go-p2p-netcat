# Practical usage cookbook

[Русская версия](USE_CASES.RU.md)

This cookbook shows complete CLI patterns for encrypted PeerId-addressed
access to SSH, TCP services, SOCKS, OpenVPN, files, and interactive shells.
All examples use this syntactically valid example PeerId:

```bash
export P2PNC_PEER_ID=12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
```

For a real connection, set `P2PNC_PEER_ID` to the value printed by the
listener. A logical port identifies a p2p-netcat service and does not need to
match the forwarded TCP port.

## Security baseline: use a pairing token

A pairing token makes discovery private, encrypts native WebRTC signaling, and
adds a mutual admission handshake. Create the server identity and an
SSH-forwarding token:

```bash
install -d -m 0700 ~/.config/p2p-netcat
p2p-nc id --identity ~/.config/p2p-netcat/identity.key
p2p-nc token \
  --identity ~/.config/p2p-netcat/identity.key \
  --expires-in 604800 \
  22022 >~/.config/p2p-netcat/ssh.token
chmod 0600 ~/.config/p2p-netcat/ssh.token
```

Copy `ssh.token` to the authorized client over an existing secure channel.
The token contains the server PeerId, logical port, secret, expiration, and
optional relay hints. Treat it as a password.

Start the protected listener:

```bash
p2p-nc -l -k \
  --identity ~/.config/p2p-netcat/identity.key \
  --pairing-token-file ~/.config/p2p-netcat/ssh.token \
  -d 127.0.0.1 -p 22 \
  22022
```

The client can omit the PeerId and logical port because both are encoded in the
token:

```bash
p2p-nc -p 2222 \
  --pairing-token-file ~/.config/p2p-netcat/ssh.token
```

## OpenSSH

### Local SSH port

On the machine that runs `sshd`:

```bash
p2p-nc -l -k -d 127.0.0.1 -p 22 22022
```

On the client:

```bash
p2p-nc -p 2222 "${P2PNC_PEER_ID}" 22022
ssh -p 2222 alice@127.0.0.1
scp -P 2222 ./report.pdf alice@127.0.0.1:/home/alice/
```

The local listener binds to `127.0.0.1` by default. Do not use
`--bind 0.0.0.0` unless other machines are intentionally allowed to use the
tunnel.

### SSH ProxyCommand without a local port

Keep the forwarding listener from the previous section running, then add this
entry to `~/.ssh/config`:

```sshconfig
Host home-p2p
    HostName p2p-netcat-peer
    User alice
    ProxyCommand p2p-nc -q 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 22022
    ServerAliveInterval 30
    ServerAliveCountMax 3
```

Connect using ordinary OpenSSH commands:

```bash
ssh home-p2p
scp ./report.pdf home-p2p:/home/alice/
sftp home-p2p
```

For private pairing, replace the `ProxyCommand` line with:

```sshconfig
    ProxyCommand p2p-nc -q --pairing-token-file /home/alice/.config/p2p-netcat/ssh.token
```

## Generic TCP port forwarding

The server-side listener connects every accepted P2P stream to a TCP service.
The client-side `-p` creates a local TCP listener and a new multiplexed P2P
stream for every local connection.

### HTTP development server

Server:

```bash
python3 -m http.server --bind 127.0.0.1 8000
p2p-nc -l -k -d 127.0.0.1 -p 8000 28000
```

Client:

```bash
p2p-nc -p 18000 "${P2PNC_PEER_ID}" 28000
curl http://127.0.0.1:18000/
```

### PostgreSQL

Server:

```bash
p2p-nc -l -k -d 127.0.0.1 -p 5432 25432
```

Client:

```bash
p2p-nc -p 15432 "${P2PNC_PEER_ID}" 25432
psql 'host=127.0.0.1 port=15432 user=postgres sslmode=prefer'
```

PostgreSQL can remain bound to loopback; it does not need to be exposed on the
server LAN.

### Windows Remote Desktop

Run on the Windows machine with Remote Desktop enabled:

```powershell
p2p-nc.exe -l -k -d 127.0.0.1 -p 3389 23389
```

Run on the client:

```bash
p2p-nc -p 13389 "${P2PNC_PEER_ID}" 23389
```

Then connect the RDP client to `127.0.0.1:13389`.

### Expose a client-side service to the listener side

Roles are symmetric. If a service runs on the client-side machine, start the
forwarding listener there and connect from the other machine:

```bash
# Machine A, where the service listens on 127.0.0.1:9000
p2p-nc -l -k -d 127.0.0.1 -p 9000 29000

# Machine B
p2p-nc -p 19000 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 29000
curl http://127.0.0.1:19000/
```

## SOCKS4/SOCKS4a/SOCKS5 proxy

Start the remote SOCKS endpoint:

```bash
p2p-nc -l -k -S 31080
```

Expose it as a loopback-only proxy:

```bash
p2p-nc -p 1080 "${P2PNC_PEER_ID}" 31080
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

Use `socks5h`, not `socks5`, when DNS resolution must happen on the remote
side. Firefox can use `127.0.0.1`, port `1080`, SOCKS v5, with “Proxy DNS when
using SOCKS v5” enabled.

SOCKS supports CONNECT with no username/password authentication. Protect the
P2P service with a pairing token and keep the local listener on loopback.
SOCKS BIND and UDP ASSOCIATE are not supported. Fixed-destination UDP
forwarding is available through `-u -p`.

## OpenVPN through p2p-netcat

OpenVPN can run over TCP and therefore fits the p2p-netcat stream model.
Configure the OpenVPN server with these transport settings while keeping its
existing keys, certificates, routes, and authentication:

```text
port 1194
proto tcp-server
dev tun
local 127.0.0.1
```

On the OpenVPN server host:

```bash
sudo openvpn --config /etc/openvpn/server/server.conf
p2p-nc -l -k -d 127.0.0.1 -p 1194 31194
```

On the client, expose OpenVPN locally:

```bash
p2p-nc -p 1194 "${P2PNC_PEER_ID}" 31194
```

Use these transport settings in the existing client configuration:

```text
client
dev tun
proto tcp-client
remote 127.0.0.1 1194
connect-retry 5
connect-retry-max infinite
```

Start OpenVPN:

```bash
sudo openvpn --config ./client.ovpn
```

This creates nested reliable transports (OpenVPN TCP inside a reliable P2P
stream). It is compatible but can amplify head-of-line blocking under packet
loss. Prefer direct OpenVPN UDP when it is reachable.

## WireGuard and packet-preserving UDP forwarding

WireGuard transports encrypted IP packets exclusively over UDP. The `-u`
forwarding mode preserves every UDP packet as one length-prefixed frame on the
P2P stream. It therefore works through native WebRTC with Nostr/WebTorrent
signaling, direct libp2p QUIC/WebRTC connections, TCP/WSS, Tor with an explicit
TCP relay, and Circuit Relay v2.

Assume WireGuard listens on UDP `51820` on the remote host. Use a different
local forwarding port, such as `15182`, so it does not conflict with a local
WireGuard `ListenPort`.

On the WireGuard server host:

```bash
p2p-nc -u -l -k -d 127.0.0.1 -p 51820 35182
```

On the WireGuard client host:

```bash
sudo wireguard-full-tunnel.sh -- \
  /usr/local/bin/p2p-nc -u --udp-idle-timeout 0 \
  -p 15182 "${P2PNC_PEER_ID}" 35182
```

Install the wrapper shipped with the release first:

```bash
sudo install -m 0755 deploy/wireguard-full-tunnel.sh \
  /usr/local/sbin/wireguard-full-tunnel.sh
```

The UDP carrier is established before the local socket is announced. The
wrapper then runs the complete p2p-netcat process under an unused numeric UID
and installs IPv4 and IPv6 `ip rule ... uidrange ... lookup main` rules for its
lifetime. Consequently libp2p, DNS, Nostr/WebTorrent signaling, STUN, ICE, and
carrier reconnects continue to use the physical route after WireGuard installs
`0.0.0.0/0`; none of them can recursively enter the tunnel they transport.
The wrapper requires Linux, root, `iproute2`, and `setpriv` from `util-linux`.
Use `--home /var/lib/p2p-netcat-client` when a persistent client identity is
required, and make that directory writable by the UID selected with `--uid`.

Both commands enable native WebRTC by default. Public Nostr relays and
WebTorrent trackers exchange signaling only; application packets travel
peer-to-peer through the ICE-selected DataChannel. This crosses common
cone/restricted NAT combinations without a user-operated relay. Symmetric NAT
or networks that block UDP still require TURN, a Circuit Relay, or a reachable
TCP/WSS route.

Merge these transport values into the client WireGuard configuration:

```ini
[Interface]
Address = 10.66.66.2/24
DNS = 1.1.1.1
MTU = 1280

[Peer]
Endpoint = 127.0.0.1:15182
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
```

Start p2p-netcat through the wrapper first, wait for
`P2P UDP carrier established`, and only then run `wg-quick up wg0`.

### WireGuard gateway for full Internet access

The remote host must forward and masquerade tunnel traffic. Run this complete
setup on the gateway; it generates a new server/client key pair, detects the
physical egress interface, writes `/etc/wireguard/wg0.conf`, and creates the
matching `wg0-client.conf` in the current directory:

```bash
set -euo pipefail
umask 077

server_private_key="$(wg genkey)"
server_public_key="$(printf '%s' "${server_private_key}" | wg pubkey)"
client_private_key="$(wg genkey)"
client_public_key="$(printf '%s' "${client_private_key}" | wg pubkey)"
egress_interface="$(ip -4 route show default | awk 'NR == 1 { for (i = 1; i <= NF; i++) if ($i == "dev") { print $(i + 1); exit } }')"
test -n "${egress_interface}"

sudo install -d -m 0700 /etc/wireguard
sudo install -m 0600 /dev/null /etc/wireguard/wg0.conf
sudo tee /etc/wireguard/wg0.conf >/dev/null <<EOF
[Interface]
Address = 10.66.66.1/24
ListenPort = 51820
PrivateKey = ${server_private_key}
PostUp = sysctl -w net.ipv4.ip_forward=1; iptables -A FORWARD -i %i -j ACCEPT; iptables -A FORWARD -o %i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; iptables -t nat -A POSTROUTING -s 10.66.66.0/24 -o ${egress_interface} -j MASQUERADE
PostDown = iptables -D FORWARD -i %i -j ACCEPT; iptables -D FORWARD -o %i -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT; iptables -t nat -D POSTROUTING -s 10.66.66.0/24 -o ${egress_interface} -j MASQUERADE

[Peer]
PublicKey = ${client_public_key}
AllowedIPs = 10.66.66.2/32
EOF
sudo chmod 0600 /etc/wireguard/wg0.conf

cat >wg0-client.conf <<EOF
[Interface]
Address = 10.66.66.2/24
DNS = 1.1.1.1
MTU = 1280
PrivateKey = ${client_private_key}

[Peer]
PublicKey = ${server_public_key}
Endpoint = 127.0.0.1:15182
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = 25
EOF
chmod 0600 wg0-client.conf
```

Transfer `wg0-client.conf` securely to the client. Persist
`net.ipv4.ip_forward=1` in the host's sysctl configuration for production. Add
`::/0` on the client only when the gateway also has routed IPv6 forwarding;
the IPv4 example intentionally does not hide IPv6 behind NAT66. Verify the
complete path with:

```bash
wg show wg0 latest-handshakes
curl --fail https://ifconfig.me/ip
```

The client pre-establishes one P2P stream; the first packet from a local source
endpoint claims it and creates one connected UDP socket on the remote side.
Replies are returned only to that source. Different local source endpoints use independent streams, so their
packets and replies cannot be mixed.

Idle associations close after 300 seconds by default. WireGuard's
`PersistentKeepalive = 25` keeps the association and NAT state active. For a
service that must retain an association without an application keepalive,
disable expiration on both p2p-netcat processes:

```bash
p2p-nc -u --udp-idle-timeout 0 -l -k -d 127.0.0.1 -p 51820 35182
sudo wireguard-full-tunnel.sh -- \
  /usr/local/bin/p2p-nc -u --udp-idle-timeout 0 \
  -p 15182 "${P2PNC_PEER_ID}" 35182
```

Protect the listener with a pairing token when the logical service or
destination is not public. Pairing authentication completes before UDP frames
are accepted.

### Transport choices and tradeoffs

| Route | Behavior |
|---|---|
| Native WebRTC over Nostr/WebTorrent signaling | Traverses compatible NATs with ICE/STUN and no user-operated media relay. The ordered DataChannel carries the same length-prefixed datagrams. |
| Direct QUIC or libp2p WebRTC Direct | Usually the best route when UDP is reachable. Datagram boundaries are preserved, but the application still uses an ordered reliable libp2p stream. |
| Direct TCP or WSS | UDP-over-stream works through TCP-only firewalls. Loss of one outer TCP segment delays later WireGuard packets because of head-of-line blocking. |
| Circuit Relay v2 over TCP/WSS | Reliable fallback behind difficult NAT. It adds relay latency and the same head-of-line behavior. |
| Tor plus an explicit TCP/WSS relay | Supported for reachability/privacy routing, but normally too slow for a general-purpose VPN. |

To force UDP-over-TCP, give both peers an explicit TCP/WSS relay and disable
direct discovery as appropriate:

```bash
export P2PNC_RELAY=/dns4/relay.example.net/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW

p2p-nc -u -l -k \
  --relay "${P2PNC_RELAY}" \
  --no-quic --no-webrtc --no-mdns --no-pubsub --no-dht \
  -d 127.0.0.1 -p 51820 35182

p2p-nc -u -p 15182 \
  --relay "${P2PNC_RELAY}" \
  --no-quic --no-webrtc --no-mdns --no-pubsub --no-dht \
  "${P2PNC_PEER_ID}" 35182
```

The framing prevents the packet merging/splitting problem caused by a plain
`socat UDP:... TCP:...` bridge. It does not turn TCP into an unreliable
datagram transport: when tunneled IP traffic itself contains TCP, packet loss
can produce nested head-of-line blocking. Prefer a direct QUIC/WebRTC route,
use a conservative WireGuard MTU such as `1280`, and benchmark before carrying
latency-sensitive traffic.

The same mode works for OpenVPN UDP, DNS, game protocols, and other
fixed-destination UDP services:

```bash
# OpenVPN UDP server host
p2p-nc -u -l -k -d 127.0.0.1 -p 1194 31194

# OpenVPN UDP client host; configure OpenVPN remote 127.0.0.1 11194 udp
p2p-nc -u -p 11194 "${P2PNC_PEER_ID}" 31194
```

SOCKS5 UDP ASSOCIATE and native unreliable QUIC datagrams are separate
protocols and are not implemented by this fixed-destination mode.

## Interactive shell and command execution

Create separate short-lived tokens for the shell and fixed command:

```bash
p2p-nc token \
  --identity ~/.config/p2p-netcat/identity.key \
  --expires-in 3600 \
  32001 >~/.config/p2p-netcat/shell.token
p2p-nc token \
  --identity ~/.config/p2p-netcat/identity.key \
  --expires-in 3600 \
  32000 >~/.config/p2p-netcat/status.token
chmod 0600 ~/.config/p2p-netcat/shell.token ~/.config/p2p-netcat/status.token
```

Start a native PTY login shell:

```bash
p2p-nc -l -i --pairing-token-file ~/.config/p2p-netcat/shell.token
p2p-nc -i --pairing-token-file ~/.config/p2p-netcat/shell.token
```

The listener uses a Unix PTY or Windows ConPTY. On the client, leave the raw
terminal with `Ctrl-E`, then `Q`.

Run a fixed command without a PTY:

```bash
p2p-nc -l -k \
  --pairing-token-file ~/.config/p2p-netcat/status.token \
  -e 'uname -a && uptime' \
  32000
p2p-nc --pairing-token-file ~/.config/p2p-netcat/status.token
```

`-e` executes through the platform shell. Never expose an unrestricted command
or PTY listener without a private pairing token.

## File transfer and backup streams

Receive one compressed archive:

```bash
# Receiver
p2p-nc -l 33000 >project-backup.tar.zst

# Sender
tar -C ~/projects -cf - important-project |
  zstd -T0 |
  p2p-nc "${P2PNC_PEER_ID}" 33000
```

Verify the transferred file out of band or send a checksum through a separate
authenticated channel:

```bash
sha256sum project-backup.tar.zst
```

For repeated transfers, use `-k` and arrange one application-level request per
connection.

## Explicit Circuit Relay

When direct discovery or NAT traversal is unreliable, pass the same reachable
Circuit Relay v2 multiaddr to both peers:

```bash
export P2PNC_RELAY=/dns4/relay.example.net/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9

p2p-nc -l -k --relay "${P2PNC_RELAY}" 34000
p2p-nc --relay "${P2PNC_RELAY}" "${P2PNC_PEER_ID}" 34000
```

The relay multiaddr above demonstrates the complete syntax; it is not a public
service. Use a relay you operate or trust.

To route the client-to-relay connection through Tor, the relay must use
TCP/WS/WSS rather than QUIC:

```bash
p2p-nc -T \
  --relay "${P2PNC_RELAY}" \
  "${P2PNC_PEER_ID}" \
  34000
```

## Long-running listener with systemd

Install the binary and token first. Create
`/etc/systemd/system/p2p-netcat-ssh.service`:

```ini
[Unit]
Description=p2p-netcat SSH tunnel
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=p2pnetcat
ExecStart=/usr/local/bin/p2p-nc -l -k --identity /var/lib/p2p-netcat/identity.key --pairing-token-file /var/lib/p2p-netcat/ssh.token -d 127.0.0.1 -p 22 22022
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/p2p-netcat

[Install]
WantedBy=multi-user.target
```

Prepare the restricted account and start the service:

```bash
sudo useradd --system --home /var/lib/p2p-netcat --create-home --shell /usr/sbin/nologin p2pnetcat
sudo install -o p2pnetcat -g p2pnetcat -m 0600 ~/.config/p2p-netcat/identity.key /var/lib/p2p-netcat/identity.key
sudo install -o p2pnetcat -g p2pnetcat -m 0600 ~/.config/p2p-netcat/ssh.token /var/lib/p2p-netcat/ssh.token
sudo systemctl daemon-reload
sudo systemctl enable --now p2p-netcat-ssh.service
sudo journalctl -u p2p-netcat-ssh.service -f
```

The deployment script installs binaries only. Service creation remains an
explicit administrator action so a downloaded script never silently opens a
listener or shell.
