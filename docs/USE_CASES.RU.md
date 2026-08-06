# Практические сценарии использования

[English version](USE_CASES.md)

Здесь приведены полные шаблоны команд для защищённого доступа по PeerId к SSH,
TCP-сервисам, SOCKS, OpenVPN, файлам и интерактивной оболочке. Во всех примерах
используется синтаксически корректный демонстрационный PeerId:

```bash
export P2PNC_PEER_ID=12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
```

Для реального подключения задайте в `P2PNC_PEER_ID` значение, напечатанное
listener-узлом. Логический порт идентифицирует сервис p2p-netcat и не обязан
совпадать с перенаправляемым TCP-портом.

## Базовая защита: pairing token

Pairing token делает discovery приватным, шифрует native WebRTC signaling и
добавляет взаимный admission handshake. Создайте identity сервера и token для
SSH-forwarding:

```bash
install -d -m 0700 ~/.config/p2p-netcat
p2p-nc id --identity ~/.config/p2p-netcat/identity.key
p2p-nc token \
  --identity ~/.config/p2p-netcat/identity.key \
  --expires-in 604800 \
  22022 >~/.config/p2p-netcat/ssh.token
chmod 0600 ~/.config/p2p-netcat/ssh.token
```

Передайте `ssh.token` авторизованному клиенту по уже существующему защищённому
каналу. Token содержит PeerId сервера, логический порт, secret, срок действия и
опциональные relay hints. Обращайтесь с ним как с паролем.

Запустите защищённый listener:

```bash
p2p-nc -l -k \
  --identity ~/.config/p2p-netcat/identity.key \
  --pairing-token-file ~/.config/p2p-netcat/ssh.token \
  -d 127.0.0.1 -p 22 \
  22022
```

Клиент может не указывать PeerId и логический порт — они записаны в token:

```bash
p2p-nc -p 2222 \
  --pairing-token-file ~/.config/p2p-netcat/ssh.token
```

Привилегированные listener по умолчанию требуют pairing token. Чтобы остальные
примеры транспорта не зависели от повторения шагов генерации token, snippets
без `--pairing-token-file` используют явный публичный override
`--allow-unauthenticated-listener`. Для любого непубличного назначения замените
его привязанным к сервису pairing token.

## OpenSSH

### Локальный SSH-порт

На машине с `sshd`:

```bash
p2p-nc -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 22 22022
```

На клиенте:

```bash
p2p-nc -p 2222 "${P2PNC_PEER_ID}" 22022
ssh -p 2222 alice@127.0.0.1
scp -P 2222 ./report.pdf alice@127.0.0.1:/home/alice/
```

Локальный listener по умолчанию привязан к `127.0.0.1`. Не используйте
`--bind 0.0.0.0`, если другие машины не должны получать доступ к туннелю.

### SSH ProxyCommand без локального порта

Оставьте forwarding-listener из предыдущего раздела запущенным и добавьте в
`~/.ssh/config`:

```sshconfig
Host home-p2p
    HostName p2p-netcat-peer
    User alice
    ProxyCommand p2p-nc -q 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 22022
    ServerAliveInterval 30
    ServerAliveCountMax 3
```

После этого работают обычные команды OpenSSH:

```bash
ssh home-p2p
scp ./report.pdf home-p2p:/home/alice/
sftp home-p2p
```

Для приватного pairing замените строку `ProxyCommand` на:

```sshconfig
    ProxyCommand p2p-nc -q --pairing-token-file /home/alice/.config/p2p-netcat/ssh.token
```

## Универсальный TCP port-forwarding

Listener на стороне сервера подключает каждый принятый P2P-stream к
TCP-сервису. Клиентский `-p` создаёт локальный TCP-listener и новый
мультиплексированный P2P-stream для каждого локального подключения.

### HTTP-сервер для разработки

Сервер:

```bash
python3 -m http.server --bind 127.0.0.1 8000
p2p-nc -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 8000 28000
```

Клиент:

```bash
p2p-nc -p 18000 "${P2PNC_PEER_ID}" 28000
curl http://127.0.0.1:18000/
```

### PostgreSQL

Сервер:

```bash
p2p-nc -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 5432 25432
```

Клиент:

```bash
p2p-nc -p 15432 "${P2PNC_PEER_ID}" 25432
psql 'host=127.0.0.1 port=15432 user=postgres sslmode=prefer'
```

PostgreSQL может оставаться привязанным к loopback и не должен открываться в
локальную сеть сервера.

### Windows Remote Desktop

На Windows-машине с включённым Remote Desktop:

```powershell
p2p-nc.exe -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 3389 23389
```

На клиенте:

```bash
p2p-nc -p 13389 "${P2PNC_PEER_ID}" 23389
```

Подключите RDP-клиент к `127.0.0.1:13389`.

### Публикация клиентского сервиса в обратную сторону

Роли симметричны. Если сервис работает на клиентской машине, запустите
forwarding-listener на ней, а подключение — на другой машине:

```bash
# Машина A, сервис слушает 127.0.0.1:9000
p2p-nc -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 9000 29000

# Машина B
p2p-nc -p 19000 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 29000
curl http://127.0.0.1:19000/
```

## SOCKS4/SOCKS4a/SOCKS5 proxy

Запустите удалённый SOCKS endpoint:

```bash
p2p-nc -l -k -S --allow-unauthenticated-listener 31080
```

Опубликуйте его как локальный loopback-only proxy:

```bash
p2p-nc -p 1080 "${P2PNC_PEER_ID}" 31080
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

Используйте `socks5h`, а не `socks5`, если DNS должен разрешаться на удалённой
стороне. Для Firefox задайте `127.0.0.1`, порт `1080`, SOCKS v5 и включите
“Proxy DNS when using SOCKS v5”.

SOCKS поддерживает CONNECT без username/password-аутентификации. Защитите
P2P-сервис pairing token и оставьте локальный listener на loopback. SOCKS BIND,
и UDP ASSOCIATE не поддерживаются. UDP forwarding к фиксированному назначению
доступен через `-u -p`.

## OpenVPN через p2p-netcat

OpenVPN умеет работать через TCP и соответствует stream-модели p2p-netcat. В
конфигурации OpenVPN-сервера задайте следующие transport-настройки, сохранив
существующие ключи, сертификаты, маршруты и аутентификацию:

```text
port 1194
proto tcp-server
dev tun
local 127.0.0.1
```

На машине OpenVPN-сервера:

```bash
sudo openvpn --config /etc/openvpn/server/server.conf
p2p-nc -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 1194 31194
```

На клиенте опубликуйте OpenVPN локально:

```bash
p2p-nc -p 1194 "${P2PNC_PEER_ID}" 31194
```

В существующей клиентской конфигурации используйте:

```text
client
dev tun
proto tcp-client
remote 127.0.0.1 1194
connect-retry 5
connect-retry-max infinite
```

Запустите OpenVPN:

```bash
sudo openvpn --config ./client.ovpn
```

Получается вложение двух надёжных транспортов: OpenVPN TCP внутри надёжного
P2P-stream. Это совместимо, но при потере пакетов может усиливать head-of-line
blocking. Если доступен прямой OpenVPN UDP, он обычно предпочтительнее.

## WireGuard и UDP forwarding с сохранением пакетов

WireGuard передаёт зашифрованные IP-пакеты только через UDP. Режим forwarding
`-u` сохраняет каждый UDP-пакет как один length-prefixed frame внутри
P2P-stream. Поэтому он работает через native WebRTC с Nostr/WebTorrent
signaling, прямой libp2p QUIC/WebRTC, TCP/WSS, Tor с явным TCP relay и Circuit
Relay v2.

Предположим, что на удалённой машине WireGuard слушает UDP `51820`. Для
локального forwarding используйте другой порт, например `15182`, чтобы не
конфликтовать с локальным WireGuard `ListenPort`.

На машине WireGuard-сервера:

```bash
p2p-nc -u -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 51820 35182
```

На машине WireGuard-клиента:

```bash
sudo wireguard-full-tunnel.sh -- \
  /usr/local/bin/p2p-nc -u --udp-idle-timeout 0 \
  -p 15182 "${P2PNC_PEER_ID}" 35182
```

Сначала установите wrapper из release:

```bash
sudo install -m 0755 deploy/wireguard-full-tunnel.sh \
  /usr/local/sbin/wireguard-full-tunnel.sh
```

UDP carrier устанавливается до объявления локального socket. Wrapper запускает
весь процесс p2p-netcat под свободным числовым UID и на время его работы
добавляет IPv4/IPv6 правила `ip rule ... uidrange ... lookup main`. Поэтому
libp2p, DNS, Nostr/WebTorrent signaling, STUN, ICE и повторные подключения
carrier продолжают использовать физический маршрут после установки WireGuard
маршрута `0.0.0.0/0` и не могут рекурсивно попасть в переносимый ими туннель.
Wrapper требует Linux, root, `iproute2` и `setpriv` из `util-linux`. Для
постоянной клиентской identity используйте
`--home /var/lib/p2p-netcat-client` и сделайте каталог доступным UID, выбранному
через `--uid`.

При наличии pairing token обе команды включают Native WebRTC и шифруют
signaling, которым обмениваются публичные Nostr relay и WebTorrent trackers;
прикладные пакеты идут peer-to-peer через выбранный ICE DataChannel. Без token
Native WebRTC отключён, если не задан отдельный небезопасный override. Это
позволяет проходить многие cone/restricted NAT без собственного relay.
Symmetric NAT или сети с заблокированным UDP по-прежнему требуют TURN, Circuit
Relay или доступный TCP/WSS-маршрут.

Добавьте эти transport-параметры в клиентскую конфигурацию WireGuard:

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

Сначала запустите p2p-netcat через wrapper, дождитесь сообщения
`P2P UDP carrier established` и только затем выполните `wg-quick up wg0`.

### WireGuard gateway для полного доступа в Internet

Удалённый хост должен пересылать и маскировать трафик туннеля. Запустите этот
полный setup на gateway: он создаст новую пару server/client keys, определит
физический egress-интерфейс, запишет `/etc/wireguard/wg0.conf` и создаст
соответствующий `wg0-client.conf` в текущем каталоге:

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

Безопасно перенесите `wg0-client.conf` на клиент. Для production сохраните
`net.ipv4.ip_forward=1` в sysctl-конфигурации хоста. Добавляйте `::/0` на
клиенте только при наличии routed IPv6 forwarding на gateway: приведённый IPv4
пример намеренно не скрывает IPv6 за NAT66. Полный путь проверяется так:

```bash
wg show wg0 latest-handshakes
curl --fail https://ifconfig.me/ip
```

Клиент заранее устанавливает один P2P-stream; первый пакет от локального source
endpoint занимает его и создаёт один connected UDP socket на удалённой стороне.
Ответы возвращаются только этому source. Разные локальные source endpoints используют независимые streams,
поэтому их пакеты и ответы не смешиваются.

По умолчанию неактивная association закрывается через 300 секунд.
`PersistentKeepalive = 25` сохраняет association и NAT state. Если сервис
должен сохранять association без собственного keepalive, отключите expiration
в обоих процессах p2p-netcat:

```bash
p2p-nc -u --udp-idle-timeout 0 -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 51820 35182
sudo wireguard-full-tunnel.sh -- \
  /usr/local/bin/p2p-nc -u --udp-idle-timeout 0 \
  -p 15182 "${P2PNC_PEER_ID}" 35182
```

Если logical service или destination не публичны, защитите listener через
pairing token. Pairing-аутентификация завершается до приёма UDP frames.

### Выбор транспорта и компромиссы

| Маршрут | Поведение |
|---|---|
| Native WebRTC через Nostr/WebTorrent signaling | Проходит совместимые NAT через ICE/STUN без собственного media relay. Ordered DataChannel переносит те же length-prefixed datagrams. |
| Прямой QUIC или libp2p WebRTC Direct | Обычно лучший маршрут при доступном UDP. Границы datagram сохраняются, но приложение всё равно использует упорядоченный надёжный libp2p-stream. |
| Прямой TCP или WSS | UDP-over-stream работает через TCP-only firewall. Потеря одного внешнего TCP segment задерживает последующие WireGuard-пакеты из-за head-of-line blocking. |
| Circuit Relay v2 через TCP/WSS | Надёжный fallback за сложным NAT. Добавляет latency relay и такое же head-of-line поведение. |
| Tor и явный TCP/WSS relay | Поддерживается для reachability/privacy routing, но обычно слишком медленно для VPN общего назначения. |

Чтобы принудительно использовать UDP-over-TCP, передайте обоим пирам явный
TCP/WSS relay и при необходимости отключите direct discovery:

```bash
export P2PNC_RELAY=/dns4/relay.example.net/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW

p2p-nc -u -l -k \
  --allow-unauthenticated-listener \
  --relay "${P2PNC_RELAY}" \
  --no-quic --no-webrtc --no-mdns --no-pubsub --no-dht \
  -d 127.0.0.1 -p 51820 35182

p2p-nc -u -p 15182 \
  --relay "${P2PNC_RELAY}" \
  --no-quic --no-webrtc --no-mdns --no-pubsub --no-dht \
  "${P2PNC_PEER_ID}" 35182
```

Framing исключает объединение и разрезание пакетов, которое возникает у
обычного `socat UDP:... TCP:...`. Оно не превращает TCP в ненадёжный datagram
transport: если внутри WireGuard передаётся TCP, потеря пакета может вызвать
вложенный head-of-line blocking. По возможности используйте прямой
QUIC/WebRTC-маршрут, задайте консервативный WireGuard MTU, например `1280`, и
проведите benchmark до переноса latency-sensitive traffic.

Этот же режим подходит для OpenVPN UDP, DNS, игровых протоколов и других
UDP-сервисов с фиксированным назначением:

```bash
# Машина OpenVPN UDP-сервера
p2p-nc -u -l -k --allow-unauthenticated-listener -d 127.0.0.1 -p 1194 31194

# Машина OpenVPN UDP-клиента; в OpenVPN задайте remote 127.0.0.1 11194 udp
p2p-nc -u -p 11194 "${P2PNC_PEER_ID}" 31194
```

SOCKS5 UDP ASSOCIATE и нативные ненадёжные QUIC datagrams являются отдельными
протоколами и не реализованы этим fixed-destination mode.

## Интерактивная оболочка и запуск команд

Создайте отдельные краткоживущие tokens для shell и фиксированной команды:

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

Запуск нативной PTY login shell:

```bash
p2p-nc -l -i --pairing-token-file ~/.config/p2p-netcat/shell.token
p2p-nc -i --pairing-token-file ~/.config/p2p-netcat/shell.token
```

Listener использует Unix PTY или Windows ConPTY. На клиенте raw terminal
закрывается последовательностью `Ctrl-E`, затем `Q`.

Запуск фиксированной команды без PTY:

```bash
p2p-nc -l -k \
  --pairing-token-file ~/.config/p2p-netcat/status.token \
  -e 'uname -a && uptime' \
  32000
p2p-nc --pairing-token-file ~/.config/p2p-netcat/status.token
```

`-e` выполняет команду через platform shell. Никогда не публикуйте
неограниченную команду или PTY-listener без приватного pairing token.

## Передача файлов и backup streams

Получение одного сжатого архива:

```bash
# Получатель
p2p-nc -l 33000 >project-backup.tar.zst

# Отправитель
tar -C ~/projects -cf - important-project |
  zstd -T0 |
  p2p-nc "${P2PNC_PEER_ID}" 33000
```

Проверьте файл через независимый канал или передайте checksum через отдельный
аутентифицированный канал:

```bash
sha256sum project-backup.tar.zst
```

Для повторяющихся передач используйте `-k` и один application-level request на
каждое подключение.

## Явный Circuit Relay

Если direct discovery или NAT traversal работают нестабильно, передайте обоим
peers один и тот же доступный Circuit Relay v2 multiaddr:

```bash
export P2PNC_RELAY=/dns4/relay.example.net/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9

p2p-nc -l -k --relay "${P2PNC_RELAY}" 34000
p2p-nc --relay "${P2PNC_RELAY}" "${P2PNC_PEER_ID}" 34000
```

Этот relay multiaddr показывает полный синтаксис, но не является публичным
сервисом. Используйте relay, которым вы управляете или которому доверяете.

Чтобы направить клиентское соединение к relay через Tor, relay должен работать
по TCP/WS/WSS, а не QUIC:

```bash
p2p-nc -T \
  --relay "${P2PNC_RELAY}" \
  "${P2PNC_PEER_ID}" \
  34000
```

## Долгоживущий listener через systemd

Сначала установите binary и token. Создайте
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

Подготовьте ограниченную учётную запись и запустите service:

```bash
sudo useradd --system --home /var/lib/p2p-netcat --create-home --shell /usr/sbin/nologin p2pnetcat
sudo install -o p2pnetcat -g p2pnetcat -m 0600 ~/.config/p2p-netcat/identity.key /var/lib/p2p-netcat/identity.key
sudo install -o p2pnetcat -g p2pnetcat -m 0600 ~/.config/p2p-netcat/ssh.token /var/lib/p2p-netcat/ssh.token
sudo systemctl daemon-reload
sudo systemctl enable --now p2p-netcat-ssh.service
sudo journalctl -u p2p-netcat-ssh.service -f
```

Deploy-скрипт устанавливает только binaries. Создание service остаётся явным
действием администратора, поэтому загруженный скрипт никогда сам не открывает
listener или shell.
