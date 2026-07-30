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

## OpenSSH

### Локальный SSH-порт

На машине с `sshd`:

```bash
p2p-nc -l -k -d 127.0.0.1 -p 22 22022
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
p2p-nc -l -k -d 127.0.0.1 -p 8000 28000
```

Клиент:

```bash
p2p-nc -p 18000 "${P2PNC_PEER_ID}" 28000
curl http://127.0.0.1:18000/
```

### PostgreSQL

Сервер:

```bash
p2p-nc -l -k -d 127.0.0.1 -p 5432 25432
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
p2p-nc.exe -l -k -d 127.0.0.1 -p 3389 23389
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
p2p-nc -l -k -d 127.0.0.1 -p 9000 29000

# Машина B
p2p-nc -p 19000 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 29000
curl http://127.0.0.1:19000/
```

## SOCKS4/SOCKS4a/SOCKS5 proxy

Запустите удалённый SOCKS endpoint:

```bash
p2p-nc -l -k -S 31080
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
UDP ASSOCIATE и UDP-forwarding не поддерживаются.

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
p2p-nc -l -k -d 127.0.0.1 -p 1194 31194
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

## WireGuard: важное ограничение

WireGuard передаёт зашифрованные IP-пакеты только через UDP. p2p-netcat сейчас
предоставляет надёжные byte streams и намеренно отклоняет `-u`, поэтому
WireGuard endpoint нельзя перенаправить напрямую:

```bash
p2p-nc -u -l 51820
```

Используйте один из поддерживаемых вариантов:

- OpenVPN в режимах `tcp-server`/`tcp-client`;
- SOCKS proxy для выбранных приложений;
- отдельные TCP-forwards для SSH, баз данных, HTTP, RDP и других сервисов;
- внешний UDP-over-stream bridge, сохраняющий границы datagram, после которого
  используется TCP-forwarding p2p-netcat.

Не ставьте обычный `socat UDP:... TCP:...` на обоих концах: TCP-stream не
сохраняет границы UDP-datagram, поэтому пакеты WireGuard могут объединяться или
разделяться. Для нативного datagram-forwarding потребуется будущее расширение
протокола.

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
