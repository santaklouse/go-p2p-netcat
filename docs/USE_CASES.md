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
SOCKS BIND, UDP ASSOCIATE, and UDP forwarding are not supported.

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

## WireGuard: important limitation

WireGuard transports encrypted IP packets exclusively over UDP. p2p-netcat
currently exposes reliable byte streams and intentionally rejects `-u`, so a
WireGuard endpoint cannot be forwarded directly:

```bash
p2p-nc -u -l 51820
```

Use one of these supported designs instead:

- OpenVPN in `tcp-server`/`tcp-client` mode as shown above;
- a SOCKS proxy for selected applications;
- individual TCP forwards for SSH, databases, HTTP, RDP, and similar services;
- an external UDP-over-stream bridge that preserves datagram boundaries,
  followed by p2p-netcat TCP forwarding.

Do not place plain `socat UDP:... TCP:...` on both ends: a TCP stream does not
preserve UDP datagram boundaries, and WireGuard packets can be merged or split.
Native datagram forwarding requires a future protocol extension.

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
