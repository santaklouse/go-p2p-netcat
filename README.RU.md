# go-p2p-netcat

[English](README.md) | **Русский**

## Быстрая установка

Установить последнюю версию с семантическим тегом:

```bash
curl -fsSL \
  https://raw.githubusercontent.com/santaklouse/go-p2p-netcat/main/deploy/deploy.sh |
  bash
```

Deploy-скрипт определяет Linux, macOS или Android и архитектуру процессора,
загружает подходящий release, проверяет его по `SHA256SUMS` и устанавливает
`p2p-nc`, `pnc` и `p2p-netcat`. Закрепление версии, другой каталог и удаление
описаны в разделе [установки](docs/INSTALLATION.RU.md#проверяемый-deploy-скрипт).

Linux-контейнеры `amd64`/`arm64` публикуются в
[GitHub Packages](https://github.com/santaklouse/go-p2p-netcat/pkgs/container/go-p2p-netcat):

```bash
docker pull ghcr.io/santaklouse/go-p2p-netcat:latest
docker run --rm ghcr.io/santaklouse/go-p2p-netcat:latest --version
```

Контейнер работает не от root и хранит постоянную identity в `/config`.
Listener, UDP, networking, version tags и локальная сборка описаны в разделе
[Docker и GitHub Packages](docs/INSTALLATION.RU.md#docker-и-github-packages).

Альтернативная установка через Go:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
```

`CGO_ENABLED=0` указан намеренно: проект не требует CGO, а этот режим обходит
ошибку DWARF-линковки в macOS, когда Homebrew LLVM имеет приоритет перед
toolchain Apple. Если CGO действительно нужен, следует явно выбрать Apple
Clang:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=1 CC=/usr/bin/clang CXX=/usr/bin/clang++ \
  go install github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
```

На macOS и Linux можно сразу создать рядом с `p2p-nc` символическую ссылку
`p2p-netcat`:

```bash
set -e

GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@latest
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@latest
P2PNC_BIN_DIR="$(go env GOBIN)"
if [ -z "$P2PNC_BIN_DIR" ]; then
  P2PNC_BIN_DIR="$(go env GOPATH)/bin"
fi
ln -sf "$P2PNC_BIN_DIR/p2p-nc" "$P2PNC_BIN_DIR/p2p-netcat"
export PATH="$P2PNC_BIN_DIR:$PATH"
p2p-netcat --version
```

Go-реализация `p2p-netcat`: двунаправленный netcat-подобный поток, адресуемый
по libp2p `PeerId`, а не по IP-адресу. Существующий stream-протокол
`/p2p-netcat/1.0.0/<логический-порт>`, identity-файлы, pairing token,
admission handshake и PTY frames совместимы с исходной JavaScript-версией.
Go-пиры дополнительно поддерживают framed UDP forwarding через
`/p2p-netcat/udp/1.0.0/<логический-порт>`.

## Состояние переноса

Реализовано:

- постоянная Ed25519 identity в совместимом protobuf-формате;
- TCP, QUIC v1, WebSocket и стандартный libp2p WebRTC-direct;
- Noise/TLS, Yamux и Circuit Relay v2;
- mDNS, GossipSub peer discovery и IPFS Amino DHT, включая provider records;
- приватные вращающиеся DHT rendezvous из `pnc1_` token;
- mutual admission handshake до передачи прикладных байтов;
- canonical CBOR, HKDF-SHA-256, AES-256-GCM и подписанные RouteRecord;
- native WebRTC v2 с Nostr/WebTorrent signaling, проверкой PeerId,
  шифрованием через pairing token, flow control, 120-секундным восстановлением
  потока и гонкой libp2p-маршрутов;
- raw stdin/stdout, `-e`, TCP и сохраняющий границы пакетов UDP forwarding,
  SOCKS4/4a/5 и PTY, включая Windows ConPTY;
- `-l`, `-k`, `-w`, `-d`, `-p`, `-u`, `-q`, `-S`, `-T`, `-i`, `-z`, `-e`,
  `-4`, `-6`, relay, id и token;
- имена команд `p2p-nc` и короткое `pnc`;
- browser-safe core и статический англо-/русскоязычный PWA в `packages/core`
  и `web`.

Перенос из JavaScript-репозитория завершён. Go CLI сохраняет те же прикладные
протоколы и совместим со старым CLI и браузерным клиентом. Браузерный код
остаётся на TypeScript, поскольку исполняется непосредственно через Web API
браузера, но теперь версионируется и развёртывается из этого репозитория.

## Установка

Текущая стабильная версия — `v0.5.0`. Архив релиза содержит исполняемые файлы
`p2p-nc` и `pnc`, лицензию MIT и обе версии README. Перед установкой следует проверить
архив по файлу `SHA256SUMS`.

### Linux

Следующая команда автоматически выбирает `amd64` или `arm64`, проверяет архив
и устанавливает программу в `/usr/local/bin`:

```bash
set -euo pipefail

P2PNC_VERSION="v0.5.0"
case "$(uname -m)" in
  x86_64|amd64) P2PNC_ARCH="amd64" ;;
  aarch64|arm64) P2PNC_ARCH="arm64" ;;
  *) echo "Неподдерживаемая архитектура Linux: $(uname -m)" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-linux-${P2PNC_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | sha256sum --check -
tar -xzf "$P2PNC_ARCHIVE"
sudo install -m 0755 "p2p-nc-linux-${P2PNC_ARCH}/p2p-nc" /usr/local/bin/p2p-nc
p2p-nc --version
```

### macOS

Поддерживаются процессоры Intel и Apple Silicon. Команда автоматически
определяет архитектуру:

```bash
set -euo pipefail

P2PNC_VERSION="v0.5.0"
case "$(uname -m)" in
  x86_64|amd64) P2PNC_ARCH="amd64" ;;
  arm64|aarch64) P2PNC_ARCH="arm64" ;;
  *) echo "Неподдерживаемая архитектура macOS: $(uname -m)" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-darwin-${P2PNC_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | shasum -a 256 --check
tar -xzf "$P2PNC_ARCHIVE"
sudo mkdir -p /usr/local/bin
sudo install -m 0755 "p2p-nc-darwin-${P2PNC_ARCH}/p2p-nc" /usr/local/bin/p2p-nc
p2p-nc --version
```

Бинарный файл релиза не нотарифицирован Apple. Если после скачивания через
браузер Gatekeeper поместил его в карантин, сначала проверьте файл, затем
удалите только атрибут карантина:

```bash
sudo xattr -d com.apple.quarantine /usr/local/bin/p2p-nc
```

### Windows

Откройте PowerShell и выполните:

```powershell
$ErrorActionPreference = 'Stop'
$Version = 'v0.5.0'
$Architecture = switch ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString()) {
    'X64' { 'amd64' }
    'Arm64' { 'arm64' }
    default { throw "Неподдерживаемая архитектура Windows: $([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture)" }
}
$Archive = "p2p-nc-windows-$Architecture.zip"
$ReleaseUrl = "https://github.com/santaklouse/go-p2p-netcat/releases/download/$Version"

Invoke-WebRequest "$ReleaseUrl/$Archive" -OutFile $Archive
Invoke-WebRequest "$ReleaseUrl/SHA256SUMS" -OutFile 'SHA256SUMS'
$ChecksumLine = Get-Content 'SHA256SUMS' | Where-Object { $_ -match ([regex]::Escape($Archive) + '$') }
if (-not $ChecksumLine) { throw "Не найдена контрольная сумма для $Archive" }
$ExpectedHash = ($ChecksumLine -split '\s+')[0].ToLowerInvariant()
$ActualHash = (Get-FileHash $Archive -Algorithm SHA256).Hash.ToLowerInvariant()
if ($ActualHash -ne $ExpectedHash) { throw "Проверка SHA-256 не пройдена для $Archive" }

Expand-Archive $Archive -DestinationPath . -Force
$InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\p2p-netcat'
New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
Copy-Item "p2p-nc-windows-$Architecture\p2p-nc.exe" "$InstallDir\p2p-nc.exe" -Force

$CurrentPath = [Environment]::GetEnvironmentVariable('Path', 'User')
if (($CurrentPath -split ';') -notcontains $InstallDir) {
    $NewPath = if ([string]::IsNullOrWhiteSpace($CurrentPath)) { $InstallDir } else { "$CurrentPath;$InstallDir" }
    [Environment]::SetEnvironmentVariable('Path', $NewPath, 'User')
}
& "$InstallDir\p2p-nc.exe" --version
```

Обновлённый пользовательский `PATH` будет доступен в новых окнах терминала.
Windows SmartScreen может показать предупреждение, поскольку исполняемый файл
релиза не подписан сертификатом.

### Телефоны Android

Android-сборки — самостоятельные CLI-файлы, а не APK. Они требуют Android 7.0
или новее и не требуют root при запуске через `adb shell`. Включите USB
debugging, подключите телефон, установите Android Platform Tools и выполните:

```bash
set -euo pipefail

P2PNC_VERSION="v0.5.0"
P2PNC_ANDROID_ABI="$(adb shell getprop ro.product.cpu.abi | tr -d '\r')"
case "$P2PNC_ANDROID_ABI" in
  arm64-v8a) P2PNC_ANDROID_ARCH="arm64" ;;
  armeabi-v7a|armeabi) P2PNC_ANDROID_ARCH="armv7" ;;
  *) echo "Неподдерживаемый Android ABI: $P2PNC_ANDROID_ABI" >&2; exit 1 ;;
esac

P2PNC_ARCHIVE="p2p-nc-android-${P2PNC_ANDROID_ARCH}.tar.gz"
P2PNC_RELEASE_URL="https://github.com/santaklouse/go-p2p-netcat/releases/download/${P2PNC_VERSION}"

curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/${P2PNC_ARCHIVE}"
curl --fail --location --remote-name "${P2PNC_RELEASE_URL}/SHA256SUMS"
if command -v sha256sum >/dev/null 2>&1; then
  grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | sha256sum --check -
else
  grep "  ${P2PNC_ARCHIVE}$" SHA256SUMS | shasum -a 256 --check
fi
tar -xzf "$P2PNC_ARCHIVE"
adb push "p2p-nc-android-${P2PNC_ANDROID_ARCH}/p2p-nc" /data/local/tmp/p2p-nc
adb shell chmod 755 /data/local/tmp/p2p-nc
adb shell /data/local/tmp/p2p-nc --version
```

Для постоянной identity укажите доступный для записи путь явно:

```bash
adb shell /data/local/tmp/p2p-nc id --identity /data/local/tmp/p2p-nc-identity.key
```

### Установка через Go

При наличии современной версии Go и разрешённой автоматической загрузке
toolchain:

```bash
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/p2p-nc@v0.5.0
GOTOOLCHAIN=auto CGO_ENABLED=0 go install -ldflags="-s -w" \
  github.com/santaklouse/go-p2p-netcat/cmd/pnc@v0.5.0
"$(go env GOPATH)/bin/p2p-nc" --version
```

## Сборка из исходного кода

В `go.mod` закреплён Go 1.25.7. Современный Go с включённым
`GOTOOLCHAIN=auto` скачает нужный toolchain автоматически.

```bash
cd /Users/alexnevpryaga/projects/santaklouse/go-p2p-netcat
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o p2p-nc ./cmd/p2p-nc
GOTOOLCHAIN=auto /opt/homebrew/bin/go build -o pnc ./cmd/pnc
./p2p-nc --version
```

Установить в `GOBIN`:

```bash
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/p2p-nc
GOTOOLCHAIN=auto /opt/homebrew/bin/go install ./cmd/pnc
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

Перенаправление локального UDP endpoint к WireGuard на удалённом пире:

```bash
# Машина с WireGuard-сервером
./p2p-nc -u -l -d 127.0.0.1 -p 51820 35182

# Машина с WireGuard-клиентом
./p2p-nc -u -p 15182 12D3KooWJ7satLo5LXjhSZBMVTWRG1AZ77sQYtX81qHHf2VtscdL 35182
```

В WireGuard-конфигурации клиента укажите peer endpoint
`127.0.0.1:15182`. `p2p-nc` сохраняет границы UDP-пакетов при переносе через
выбранный надёжный stream. Native WebRTC использует публичный
Nostr/WebTorrent signaling и ICE/STUN для прохождения совместимых NAT без
собственного relay. Стандартные libp2p TCP, QUIC, WebRTC Direct, WSS и Circuit
Relay маршруты также остаются доступными.

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

## Автоматические релизы

Каждый push в `main`, включая merge pull request, и каждый семантический тег
`v*.*.*` запускает `.github/workflows/release-main.yml`. После успешных тестов
и статического анализа workflow публикует GitHub Release со следующими
сборками:

- Linux: `amd64`, `arm64`;
- macOS: `amd64`, `arm64`;
- Windows: `amd64`, `arm64`;
- Android 7.0 (API 24) и новее: `arm64`, `armv7`.

Сборки Linux и macOS публикуются как `.tar.gz`, сборки Windows — как `.zip`.
Сборки Android также публикуются как `.tar.gz`. Каждый релиз содержит файл
`SHA256SUMS`. Семантические теги, например `v0.5.0`, создают стабильные релизы.
Сборки из `main` помечаются как prerelease и получают детерминированный тег,
который начинается с `main-` и заканчивается первыми 12 символами SHA коммита.
Повторный запуск workflow обновляет тот же релиз, а не создаёт дубликат.

Android-артефакты — это CLI-файлы для Android shell, а не APK. Вариант
`android-arm64` подходит почти для всех современных физических телефонов и
планшетов. `android-armv7` предназначен для старых 32-битных ARM-устройств.
Пример установки и запуска:

```bash
tar -xzf p2p-nc-android-arm64.tar.gz
adb push p2p-nc-android-arm64/p2p-nc /data/local/tmp/p2p-nc
adb shell chmod 755 /data/local/tmp/p2p-nc
adb shell /data/local/tmp/p2p-nc --version
```

## Документация

- [Практические сценарии](docs/USE_CASES.RU.md): OpenSSH, OpenVPN,
  TCP/UDP forwarding, SOCKS, WireGuard, передача файлов, relay и
  systemd.
- [Установка](docs/INSTALLATION.RU.md)
- [Архитектура](docs/ARCHITECTURE.RU.md)
- [Протокол datagram forwarding](docs/DATAGRAM_PROTOCOL.RU.md)
- [Совместимость с gs-netcat](docs/GS_NETCAT_COMPAT.RU.md)
- [Pairing protocol](docs/PAIRING_PROTOCOL.RU.md)
- [Relay API](docs/RELAY_API.RU.md)
- [Миграция native WebRTC](docs/WEBRTC_MIGRATION.RU.md)
- [GitHub Wiki](https://github.com/santaklouse/go-p2p-netcat/wiki)

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
