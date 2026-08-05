# Установка go-p2p-netcat

[English version](INSTALLATION.md)

## Требования

- Go с `GOTOOLCHAIN=auto`: нужная версия toolchain выбирается из `go.mod`;
- macOS, Linux или Windows;
- исходящие TCP/WSS и UDP для discovery, signaling, QUIC и WebRTC.

## Проверяемый deploy-скрипт

Установить последний release для Linux, macOS или Android:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  bash
```

Скрипт определяет release target, загружает архив и `SHA256SUMS`, проверяет
SHA-256 до распаковки и устанавливает все три имени команды. Он не запускает
listener, background service, shell, cron job или login item.

Закрепить версию:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_VERSION=v0.5.1 bash
```

Установить без повышенных привилегий:

```bash
install -d -m 0755 ~/.local/bin
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_INSTALL_DIR="$HOME/.local/bin" P2PNC_NO_SUDO=1 bash
```

Удалить из того же каталога:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  P2PNC_INSTALL_DIR="$HOME/.local/bin" P2PNC_UNINSTALL=1 bash
```

Если требуется аудит, сначала скачайте и изучите скрипт:

```bash
curl -fsSLo /tmp/p2p-nc-deploy.sh \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh
less /tmp/p2p-nc-deploy.sh
bash /tmp/p2p-nc-deploy.sh
```

Полный список переменных находится в начале
[`deploy/deploy.sh`](../deploy/deploy.sh).

## Docker и GitHub Packages

Release images публикуются в GitHub Container Registry под именем
`ghcr.io/santaklouse/go-p2p-netcat` для Linux `amd64` и `arm64`.

Загрузить и проверить последний стабильный image:

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker run --rm ghcr.io/santaklouse/go-p2p-netcat:latest --version
```

Используются следующие tags:

| Git ref | Container tags |
|---|---|
| `main` | `main`, например `sha-0123456789ab` |
| `v1.2.3` | `1.2.3`, `1.2`, `1`, `latest`, например `sha-0123456789ab` |

Для воспроизводимого deployment используйте version tag или digest вместо
`latest`:

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker inspect \
  --format='{{index .RepoDigests 0}}' \
  ghcr.io/santaklouse/go-p2p-netcat:latest
```

Процесс работает с UID/GID `65532`, содержит CA certificates и `/bin/sh`, а
постоянная identity по умолчанию находится в
`/config/p2p-netcat/identity.key`. Сохраняйте `/config` в named volume:

```bash
docker volume create p2p-netcat-config
docker run --rm \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  id
```

В Linux host networking предоставляет libp2p и forwarded services тот же
network namespace, что и хосту. Это самый прямой вариант запуска listener:

```bash
docker run --rm --init \
  --name p2p-netcat \
  --network host \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -l -k --transport-port 4001 31337
```

Так UDP listener также получает доступ к WireGuard endpoint, привязанному к
host loopback:

```bash
docker run --rm --init \
  --name p2p-netcat-wireguard \
  --network host \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -u -l -k --transport-port 4001 \
  -d 127.0.0.1 -p 51820 35182
```

Docker Desktop не предоставляет Linux host networking с полностью идентичным
поведением. Используйте bridge networking, публикуйте требуемые TCP/UDP-порты
и укажите явный relay или публичный `--announce` address:

```bash
docker run --rm --init \
  --name p2p-netcat \
  --publish 4001:4001/tcp \
  --publish 4001:4001/udp \
  --publish 127.0.0.1:15182:15182/udp \
  --volume p2p-netcat-config:/config \
  ghcr.io/santaklouse/go-p2p-netcat:latest \
  -u --bind 0.0.0.0 -p 15182 --transport-port 4001 \
  12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 35182
```

Target PeerId в команде выше является синтаксически корректным примером;
реальное соединение должно использовать PeerId listener. Если direct discovery
или reachability недостаточны, передайте обоим пирам одинаковый явный
`--relay` multiaddr. Опубликованный Docker UDP-порт остаётся доступен только
через loopback хоста, хотя внутри изолированного контейнера p2p-netcat
привязывается ко всем интерфейсам.

Локальная сборка и тестирование контейнера:

```bash
docker build --build-arg VERSION=local -t p2p-netcat:local .
docker run --rm p2p-netcat:local --version
bash scripts/docker_test.sh
```

Release workflow в GitHub Actions публикует multi-platform OCI images с SBOM
и provenance attestations через repository `GITHUB_TOKEN`; долгоживущий
registry token не требуется. GitHub по умолчанию создаёт новый container
package приватным. После первой публикации администратор package должен явно
выбрать **Public** в настройках, если нужны анонимные загрузки. После этого
public container можно загружать без login.

## Установка через Go

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
