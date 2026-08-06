# План исправлений по безопасности

[English version](SECURITY_REMEDIATION_PLAN.md)

Дата плана: 6 августа 2026 года.

База: `v0.6.0` и отчёты `SECURITY_AUDIT.md` и `SECURITY_AUDIT.RU.md`. Аудит
остаётся исторической оценкой коммита `8676929`; этот файл отслеживает
исправления в `codex/security-audit-fixes`.

## Политика релиза

Новые безопасные значения по умолчанию намеренно отклоняют конфигурации,
которые раньше запускали привилегированные сервисы без аутентификации. Их
следует выпустить как `v0.7.0` с явной инструкцией по миграции. Data-channel
frame protocol остаётся версии 2. Production-аутентификация использует response
версии 2 и новый domain `p2p-netcat/native-webrtc-auth/v2`. Историческое
значение `p2p-netcat/trystero-auth/v1` доступно только через явные legacy
helpers и никогда не принимается production endpoint.

## P0: исключить доступ без учётных данных

### S-01 — Pairing обязателен для привилегированных listener

Статус: реализовано; локальная проверка прошла.

Listener `-i`, `-e`, `-S`, TCP forwarding и UDP forwarding отклоняют запуск,
если не настроен источник pairing token. Намеренно публичный запуск требует
длинный флаг `--allow-unauthenticated-listener` и выводит предупреждение. Raw
stream listener сохраняет публичное поведение в стиле netcat.

Критерии приёмки:

- каждый привилегированный listener отклоняется без token;
- `--pairing-token`, `--pairing-token-file` и `P2P_NETCAT_TOKEN` проходят
  стартовый guard, после чего token полностью декодируется и проверяется до
  запуска сети;
- небезопасный override допустим только для привилегированного listener;
- новое поведение покрыто mode-matrix и listener-lock тестами.

### S-02 — Native WebRTC без аутентификации выключен по умолчанию

Статус: реализовано; локальная проверка прошла.

Go CLI запускает собственный Native WebRTC через Nostr/WebTorrent только с
валидным pairing token. Явный аварийный флаг
`--allow-unauthenticated-native-webrtc` выводит предупреждение. Стандартный
libp2p WebRTC Direct остаётся доступным, потому что Noise привязывает соединение
к ожидаемому PeerId. В browser PWA небезопасного UI-переключателя нет: без
pairing token используется только libp2p-маршрут Worker с Noise-аутентификацией.

Критерии приёмки:

- Go listener и client не создают Native WebRTC signaling sessions без token
  или явного небезопасного флага;
- PWA не создаёт `BrowserNativeWebRtcClient` без token;
- защищённые Go- и browser-соединения продолжают соревновать libp2p и Native
  WebRTC;
- Tor и `--no-webrtc` по-прежнему отключают все WebRTC-маршруты.

## P1: привязать сессию и ограничить управляемую атакующим работу

### S-03 — Миграция channel binding для Native WebRTC

Статус: реализовано; локальные cross-language и real-Pion проверки прошли.

Authentication response версии 2 использует новый domain. Подписываемый
транскрипт содержит length-delimited значения:

- новый authentication domain и версию response;
- роли клиента и сервера;
- ожидаемый PeerId сервера и логический порт;
- signaling session ID;
- challenge клиента;
- SHA-256 точного offer SDP;
- SHA-256 точного answer SDP.

Хэши SDP привязывают DTLS certificate fingerprints и контекст ICE/session к
подписи PeerId. Клиент отклоняет legacy response без downgrade fallback. Go и
`packages/core` используют общий фиксированный payload vector, а regression-
тест с двумя реальными Pion-соединениями подтверждает, что proof одной сессии
нельзя использовать в другой.

Критерии приёмки:

- подпись, перенесённая из второго PeerConnection, не проходит проверку;
- изменение offer, answer, роли, session ID, PeerId, порта или challenge
  нарушает доказательство;
- проходят paired Go-to-browser и browser-to-Go compatibility тесты;
- добавлен целевой MitM regression test с двумя PeerConnection.

### S-04 — Лимиты ресурсов и parsing

Статус: реализовано; локальная проверка прошла.

Добавлены лимиты: 32 одновременных Native WebRTC handshake глобально, два на
signaling peer ID, очередь чтения stream 1 МиБ, fail-closed поведение очереди
Pion frames, SDP 256 КиБ, JSON ICE candidate 64 КиБ, encrypted и WebSocket
signaling message 512 КиБ, deadline SOCKS negotiation, поля SOCKS4 user/domain
по 255 байт.

Критерии приёмки:

- тесты подтверждают отклонение без неограниченного выделения памяти или роста
  числа goroutine;
- race detector проходит пути закрытия listener и handshake;
- проходят real-Pion smoke profile и browser-тесты.

### S-05 — Политика секретных файлов

Статус: реализовано в Unix и Windows; runtime-проверка Windows остаётся CI gate.

Существующие identity и pairing-token файлы должны быть обычными файлами, а
чтение ограничено по размеру. Unix отклоняет права group/other. Windows
проверяет DACL и разрешает доступ к секрету только owner, текущему пользователю,
LocalSystem и встроенным Administrators; отсутствующий или неподдерживаемый DACL
отклоняется. Новые файлы сохраняют mode `0600` в Unix, а в Windows отключают
наследование и получают защищённый DACL для текущего пользователя, LocalSystem
и встроенных Administrators.

## P2: supply chain и доступность

### S-06 — Уязвимости зависимостей

Статус: реализовано частично.

Web override обновлён с `brace-expansion@5.0.8` до `5.0.9`, а
`npm audit --package-lock-only --audit-level=high` сообщает об отсутствии
уязвимостей. `govulncheck` всё ещё находит GO-2024-3218 в
`go-libp2p-kad-dht@v0.42.1`; исправленная версия в advisory не указана. DHT
остаётся отключаемым. Для чувствительных к доступности сценариев документируем
`--no-dht` вместе с доверенным прямым адресом или Circuit Relay и повторяем
проверку при каждом обновлении зависимостей.

### S-07 — Подписанные релизы

Статус: реализовано; первые опубликованные bundles ожидают релиз `v0.7.0`.

Release workflow создаёт keyless Sigstore bundle для каждого архива, installer
script и `SHA256SUMS`, сохраняя GitHub artifact attestations. Deploy-скрипт
проверяет bundle `SHA256SUMS` по точной identity workflow репозитория и GitHub
Actions OIDC issuer до использования checksum. Cosign installer action
закреплён по commit SHA. Legacy-установка только с checksum требует явного
opt-in `P2PNC_ALLOW_UNSIGNED=1`.
Дополнительные UPX-архивы создаются только для Linux `amd64` и `arm64`, рядом с
исходными архивами. Они сохраняют штатные UPX-метаданные, проходят проверку
целостности и сравнение после контрольной распаковки, а также покрываются теми
же checksum, подписями и attestations, что и остальные release artifacts.

## Финальный набор проверок

Перед релизом должны пройти все команды:

```bash
GOTOOLCHAIN=auto go test ./...
GOTOOLCHAIN=auto go test -race -count=1 -timeout=25m ./...
GOTOOLCHAIN=auto go vet ./...
GOTOOLCHAIN=auto go run ./cmd/webrtc-soak --profile smoke
bash deploy/deploy_test.sh
bash deploy/wireguard-full-tunnel_test.sh
bash scripts/sync-wiki_test.sh
bash scripts/docker_test.sh
(cd packages/core && npm ci && npm test && npm run lint && npm pack --dry-run)
(cd web && npm ci && npm test && npm run lint && npm pack --dry-run)
```

Привилегированный Linux-тест в network namespace остаётся отдельным
обязательным gate на совместимом Linux-хосте.
