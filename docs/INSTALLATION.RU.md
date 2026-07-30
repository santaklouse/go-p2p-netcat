# Установка go-p2p-netcat

[English version](INSTALLATION.md)

## Требования

- Go с `GOTOOLCHAIN=auto`: нужная версия toolchain выбирается из `go.mod`;
- macOS, Linux или Windows;
- исходящие TCP/WSS и UDP для discovery, signaling, QUIC и WebRTC.

Установить оба имени команды:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
```

Проверить установку:

```bash
p2p-nc --version
pnc --version
```

## Первое соединение

Запустить listener:

```bash
p2p-nc -l -v 31337
```

На другом компьютере использовать напечатанный PeerId:

```bash
printf 'hello\n' | p2p-nc -v -w 90 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

PeerId выше синтаксически корректен, но для реального соединения используйте
значение listener. Для предсказуемого маршрута через relay передайте обеим
сторонам одинаковый явный multiaddr через `--relay`.

## Интерактивный PTY

```bash
# listener
p2p-nc -l -i -v 31337

# client
p2p-nc -i -v 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

На Unix используется native PTY, на Windows — ConPTY. Для выхода нажмите
`Ctrl-E`, затем `Q`.

## Браузерный PWA

Статический клиент публикуется из `web/`:

- английский: <https://santaklouse.github.io/go-p2p-netcat/>
- русский: <https://santaklouse.github.io/go-p2p-netcat/?lang=ru>

Браузер не может набирать обычные TCP- или QUIC-multiaddr. Он использует
native WebRTC, WebTransport либо маршрут через secure WebSocket relay.

## Постоянная identity и приватный pairing

Listener по умолчанию создаёт `~/.config/p2p-netcat/identity.key`. Другой файл
задаётся через `--identity`. Сделайте резервную копию: новый ключ означает
новый PeerId.

Создать приватный token:

```bash
p2p-nc token --identity ~/.config/p2p-netcat/identity.key 31337
```

Token передаётся через `--pairing-token`, `--pairing-token-file` или
`P2P_NETCAT_TOKEN`. Приватный режим выводит секретные DHT/signaling
rendezvous, шифрует native signaling и выполняет mutual admission handshake
до прикладных данных.

## Сборка и тестирование

```bash
GOTOOLCHAIN=auto go build ./cmd/p2p-nc ./cmd/pnc
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go vet ./...
```

Для браузерных пакетов нужен Node 22.12 или новее:

```bash
cd packages/core
npm ci
npm test
cd ../../web
npm ci
npm test
npm run lint
```
