# Отчёт о миграции на native WebRTC

[English](WEBRTC_MIGRATION.md) | **Русский**

`p2p-netcat-core` владеет WebRTC-протоколом, контроллером соединения и двумя
signaling-адаптерами для публичной инфраструктуры. Runtime Trystero, adapters,
совместимые aliases API, переключатель UI, флаг CLI и npm dependencies удалены.
Node.js и статическая PWA теперь используют один native signaling design.

Отказ от Trystero не отменяет необходимость rendezvous-инфраструктуры. Два
компьютера за NAT не могут обменяться SDP, зная только PeerId. Новый код
прозрачно использует публичные Nostr relays и WebTorrent WebSocket trackers, но
не требует серверов p2p-netcat или серверных скриптов.

## Текущее состояние

| Этап | Состояние |
|---|---|
| Независимый от signaling byte stream, flow control, EOF, keepalive и reconnect | Реализован в core |
| Собственный контроллер `RTCPeerConnection` и ordered reliable `RTCDataChannel` | Реализован в core |
| Подписанный Nostr signaling adapter | Реализован в core |
| WebTorrent WebSocket tracker signaling adapter | Реализован в core |
| Nostr trickle ICE с ограниченной перестановкой candidates | Реализован в core |
| Интеграция с CLI и статической PWA | Только native |
| Автоматическая локальная WebRTC soak-матрица | Еженедельно и вручную на Linux/macOS |
| Удаление Trystero npm dependencies и adapters | Завершено |
| Удаление implementation-specific compatibility aliases | Завершено |

Браузерное приложение по-прежнему состоит только из статических файлов для
GitHub Pages. В Node.js `RTCPeerConnection` предоставляет `@roamhq/wrtc`, а
браузер использует встроенную реализацию.

## Алгоритм native-соединения

1. Listener строит детерминированную room из постоянного PeerId и логического
   порта.
2. Обе стороны хешируют versioned room в необратимую signaling topic.
3. Каждый процесс создаёт одну случайную 20-символьную signaling identity,
   общую для Nostr и tracker adapters.
4. Nostr-попытка сразу публикует SDP и передаёт ICE candidates отдельными
   подписанными events. Candidates, пришедшие раньше offer, сохраняются в
   ограниченном listener-cache на 20 секунд. WebTorrent-попытка ждёт завершения
   ICE gathering и публикует полный SDP, потому что tracker announce переносит
   offers и answers, но не произвольные candidate messages.
5. Первый ответ, открывший `p2p-netcat-v2`, запускает 32-байтовый challenge.
6. Сервер подписывает domain-separated payload постоянным libp2p Ed25519-ключом.
   Клиент восстанавливает PeerId и проверяет точное совпадение с запрошенным.
7. Только после успешной проверки клиент отправляет `AUTH_READY`. Поэтому
   непроверенный кандидат не может запустить PTY или прикладной поток listener.
8. Побеждает первый аутентифицированный адаптер. Проигравшие Nostr/tracker
   peer connections закрываются до передачи прикладных данных.
9. При временной потере data channel тот же `WebRtcStream` и PTY сохраняются до
   120 секунд. Reconnect привязывает новый data channel к прежнему потоку.

Offer glare здесь отсутствует по конструкции: клиенты создают offer, listener
только отвечает. Каждая повторная попытка получает уникальный session ID, а
дубликаты из tracker/Nostr фильтруются до endpoint controller.

## Native wire protocol

Каждое сообщение `RTCDataChannel` бинарное:

```text
+---------+------------+--------------------------+
| version | frame type | payload                  |
+---------+------------+--------------------------+
| 0x02    | 0x00       | байты приложения         |
| 0x02    | 0x01       | UTF-8 stream control     |
| 0x02    | 0x02       | authentication challenge |
| 0x02    | 0x03       | public key + signature   |
| 0x02    | 0x04       | authentication accepted  |
+---------+------------+--------------------------+
```

Stream control поддерживает `flow:1`, `ack:<bytes>`, `resume`, `ping`/`pong`,
`eof` и `abort`. `RTCDataChannel.bufferedAmount` создаёт первую границу
backpressure, а `WebRtcStream` добавляет сквозное окно 256 КиБ, чтобы быстрый
вывод PTY не увеличивал память браузера без ограничения.

## Signaling adapters

### Nostr

Адаптер открывает WebSocket к нескольким публичным relay, создаёт временный
Schnorr-ключ и публикует подписанные events kind `25050`. Event содержит
versioned room tag, принимается не старше 120 секунд и проверяется по
каноническому event ID и подписи до обработки SDP. PeerId сервера всё равно
проверяется внутри WebRTC: временный Nostr-ключ подтверждает только целостность
signaling event.

Для Nostr включена capability `trickleIce`. Offer/answer и candidate events
могут пройти через разные relay и прийти в другом порядке. Endpoint принимает
не более 128 session keys, для которых пока известны только candidates, и не
более 32 candidates на попытку; неразрешённая группа удаляется через 20 секунд.
Это ограничивает память, но сохраняет candidate, который пришёл до offer.

### WebTorrent trackers

Адаптер выводит детерминированный 20-байтовый tracker `info_hash`, хранит
ограниченный пул offer, адресует answer через `offer_id` и `to_peer_id`,
периодически повторяет announce, удаляет дубликаты ответов и переподключается с
exponential backoff. Пользовательские данные через tracker не проходят.

## Публичный API core

```js
import {
  NativeWebRtcPeer,
  WebRtcStream,
  connectNativeWebRtc,
  createNostrSignalingSession,
  createSignalingPeerId,
  createTorrentSignalingSession,
  defaultRtcConfiguration,
  startNativeWebRtcListener,
  webRtcRoomId
} from 'p2p-netcat/core'
```

Те же exports доступны в отдельном пакете `p2p-netcat-core`. Signaling-функции
принимают внедряемые конструкторы `WebSocket` и `RTCPeerConnection`, поэтому
они browser-safe и тестируются без Node.js globals.

## Результат удаления

Отложенной legacy-ветки больше нет. CLI и PWA соревнуют только полный
libp2p-маршрут и native WebRTC. Внутри native WebRTC параллельно работают
независимые Nostr и WebTorrent signaling sessions, проверяется запрошенный
PeerId, а проигравшие попытки закрываются.

Подробный режим CLI показывает настоящий production-путь:

```bash
p2p-nc -l -i -v 31337
p2p-nc -i -v 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Во второй команде замените PeerId значением, напечатанным listener. Удалённая
опция `--no-trystero` и переключатель PWA native-only больше не нужны: теперь
это единственная WebRTC-реализация.

Authentication transcript намеренно сохраняет исторический domain
`p2p-netcat/trystero-auth/v1`. Его изменение нарушило бы wire-совместимость
существующих native peers и будущего Go-порта. Это замороженное значение
протокола, а не импорт пакета или runtime dependency.

## Автоматическая soak-матрица

В репозитории есть детерминированный runner с настоящими
`RTCPeerConnection` и `RTCDataChannel` из `@roamhq/wrtc`:

```bash
npm run soak:webrtc -- \
  --profile smoke \
  --report artifacts/webrtc-soak-local.json
```

Профили:

| Профиль | Итераций на сценарий | Payload в каждом направлении |
|---|---:|---:|
| `smoke` | 1 | 64 КиБ |
| `ci` | 2 | 512 КиБ |
| `soak` | 12 | 8 МиБ |

Параметры `--iterations` и `--payload-bytes` переопределяют профиль.
`--scenarios nostr-trickle,reconnect-same-stream` выбирает часть сценариев.
Каждая передача бинарная, двунаправленная, использует flow control и
проверяется по SHA-256. Runner проверяет:

- Nostr trickle ICE, где candidates намеренно могут обогнать offer;
- полный non-trickle SDP, совместимый с WebTorrent tracker;
- одновременные попытки адаптеров и дубли offer;
- отказ одного signaling adapter при успешной работе второго;
- принудительную потерю peer connection, повторную привязку и передачу через
  тот же логический `WebRtcStream`.

`.github/workflows/webrtc-soak.yml` еженедельно запускает профиль `soak` на
Ubuntu и macOS. При ручном запуске можно выбрать `smoke`, `ci` или `soak`;
каждый job загружает JSON report.

Эта матрица проверяет core state machine, настоящие локальные ICE/data channels,
backpressure, разделение capability адаптеров, изоляцию отказа и reconnect. Она
не моделирует отказы публичных Nostr/tracker, жизненный цикл browser, задержку
между странами и реальные сочетания NAT/firewall. Они остаются release gates,
а не свойствами, которые якобы гарантирует автоматический runner.

Удаление зависимости завершено. Эту real-network матрицу нужно продолжать
выполнять как постоянный stability gate:

- browser → Linux CLI и macOS CLI → Linux CLI между разными странами;
- Chrome, Firefox и Safari;
- обычный, symmetric и restrictive NAT;
- фоновые вкладки и сон/пробуждение;
- большой непрерывный вывод PTY в течение нескольких часов;
- повторные потери и восстановления сети в пределах 120 секунд;
- текущий браузер → текущий CLI и обратное направление, где оно применимо.

STUN обнаруживает NAT mapping, но не является relay. При symmetric NAT или
заблокированном UDP всё ещё может потребоваться TURN либо настроенный libp2p
Circuit Relay. Публичные relays и trackers принадлежат третьим сторонам: они
видят signaling topics, время и SDP/ICE candidates, но не прикладные данные
зашифрованного peer-to-peer channel. Они не гарантируют 100% доступность.
