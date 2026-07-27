# go-p2p-netcat

[English](README.md) | **Русский**

Go-реализация `p2p-netcat`: двунаправленный netcat-подобный поток, адресуемый
по libp2p `PeerId`, а не по IP-адресу. Wire-протокол
`/p2p-netcat/1.0.0/<логический-порт>`, identity-файлы, pairing token,
admission handshake и PTY frames совместимы с исходной JavaScript-версией.

## Состояние переноса

Реализовано:

- постоянная Ed25519 identity в совместимом protobuf-формате;
- TCP, QUIC v1, WebSocket и стандартный libp2p WebRTC-direct;
- Noise/TLS, Yamux и Circuit Relay v2;
- mDNS и IPFS Amino DHT, включая provider records;
- приватные вращающиеся DHT rendezvous из `pnc1_` token;
- mutual admission handshake до передачи прикладных байтов;
- canonical CBOR, HKDF-SHA-256, AES-256-GCM и подписанные RouteRecord;
- raw stdin/stdout, `-e`, TCP forwarding, SOCKS4/4a/5 и PTY;
- `-l`, `-k`, `-w`, `-d`, `-p`, `-q`, `-S`, `-T`, `-i`, `-z`, `-e`,
  `-4`, `-6`, relay, id и token.

Пока не перенесены два JavaScript-специфичных механизма:

- собственный WebRTC signaling через публичные Nostr relay и WebTorrent
  trackers с 120-секундным resume;
- GossipSub `pubsub-peer-discovery` announcements. Флаг `--no-pubsub`
  сохранён для CLI-совместимости, но сейчас ничего не переключает.

Go-версия использует стандартный `webrtc-direct` транспорт go-libp2p.
TCP/QUIC/Noise/Yamux, identity, pairing и прикладные протоколы совместимы с
JavaScript CLI. PTY работает на macOS и Linux; Windows-сборка возвращает для
`-i` явную ошибку, остальные режимы доступны. Статический браузерный PWA
остаётся в исходном репозитории.

## Требования и сборка

В `go.mod` закреплён Go 1.25.7. Современный Go с включённым
`GOTOOLCHAIN=auto` скачает нужный toolchain автоматически.

```bash
cd /Users/alexnevpryaga/projects/santaklouse/go-p2p-netcat
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o p2p-nc ./cmd/p2p-nc
./p2p-nc --version
```

Установить в `GOBIN`:

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/p2p-nc
```

На этой машине `/usr/local/bin/go` — устаревший Go 1.13 для Intel. Для
разработки следует вызывать `/opt/homebrew/bin/go` или исправить `PATH`:

```bash
export PATH="/opt/homebrew/bin:$PATH"
go version
```

## Быстрый запуск

На слушателе:

```bash
./p2p-nc -l 8080
```

Команда выводит PeerId и доступные multiaddr в `stderr`. На клиенте:

```bash
./p2p-nc 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 8080
```

В этой команде нужно использовать фактический PeerId, напечатанный слушателем.
Для полностью локальной проверки без DHT:

```bash
DEMO_DIR="$(mktemp -d)"
DEMO_KEY="$DEMO_DIR/listener.key"
DEMO_ID="$(./p2p-nc id --identity "$DEMO_KEY")"
./p2p-nc -l 8080 \
  --transport-port 43127 \
  --no-dht \
  --no-mdns \
  --no-quic \
  --no-webrtc \
  --identity "$DEMO_KEY" \
  >"$DEMO_DIR/received.txt" &
DEMO_PID=$!
sleep 2
printf 'hello over go-libp2p\n' | ./p2p-nc \
  --no-dht \
  --no-mdns \
  --no-quic \
  --no-webrtc \
  "/ip4/127.0.0.1/tcp/43127/p2p/$DEMO_ID" \
  8080
wait "$DEMO_PID"
cat "$DEMO_DIR/received.txt"
```

## Приватный pairing

Создать token из постоянной identity:

```bash
./p2p-nc token 31337 \
  --identity "$HOME/.config/p2p-netcat/identity.key" \
  >"$HOME/.config/p2p-netcat/pairing.token"
chmod 600 "$HOME/.config/p2p-netcat/pairing.token"
```

Слушатель:

```bash
./p2p-nc -l -i \
  --identity "$HOME/.config/p2p-netcat/identity.key" \
  --pairing-token-file "$HOME/.config/p2p-netcat/pairing.token"
```

После безопасной передачи token-файла клиенту:

```bash
./p2p-nc -i \
  --pairing-token-file "$HOME/.config/p2p-netcat/pairing.token"
```

Token содержит PeerId и логический порт, поэтому в приватном режиме их можно не
передавать отдельными аргументами. Token-файл следует хранить с правами `0600`.

## Forwarding, SOCKS и PTY

Удалённый TCP forwarding к `127.0.0.1:5432`:

```bash
./p2p-nc -l 15432 -p 5432
./p2p-nc -p 15432 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 15432
```

SOCKS server на удалённой стороне:

```bash
./p2p-nc -l -S 1080
./p2p-nc -p 1080 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 1080
curl --socks5-hostname 127.0.0.1:1080 https://example.com/
```

Интерактивный login shell:

```bash
./p2p-nc -l -i 2222
./p2p-nc -i 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 2222
```

В PTY-клиенте последовательность `Ctrl-E`, затем `q` закрывает сеанс.

## Собственный relay

Локальная проверка Circuit Relay v2:

```bash
./p2p-nc relay -4 -p 9090 --websocket-port 9091
```

Для публичного VPS добавьте фактические `--announce` multiaddr. Relay должен
быть доступен извне по TCP/UDP 9090 и TCP 9091, если нужен WebSocket.

## Проверки

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go fmt ./...
GOTOOLCHAIN=auto /opt/homebrew/bin/go vet ./...
GOTOOLCHAIN=auto /opt/homebrew/bin/go test ./...
```

Тесты включают опубликованные JavaScript test vectors для token, четырёх
HKDF-ключей, rendezvous ID, provider CID, AES-GCM envelope и обоих admission
frames.

## Структура

```text
cmd/p2p-nc/             CLI entrypoint
internal/cli/           parsing, validation и lifecycle
internal/identity/      совместимые постоянные Ed25519 keys
p2p/                    host, transports, DHT, mDNS и relay
protocol/pairing/       token, HKDF, rendezvous и AEAD
protocol/admission/     mutual fixed-frame handshake
protocol/routerecord/   deterministic CBOR и identity signatures
protocol/pty/           бинарные PTY frames
session/                raw, exec, forwarding, SOCKS и PTY
```

## Лицензия

MIT.
