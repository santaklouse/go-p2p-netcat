# p2p-netcat-core

[English](README.md) | **Русский**

Полная схема взаимодействия библиотеки с CLI и браузерным Worker описана в
[`docs/ARCHITECTURE.RU.md`](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/ARCHITECTURE.RU.md).

Общее browser-safe ядро `p2p-netcat`. Пакет не использует Node.js API и может
импортироваться Web Worker и другими JavaScript-клиентами. Go CLI реализует те
же документированные wire-форматы.

Пакет отвечает за:

- логические порты и protocol ID;
- нормализацию PeerId и multiaddr;
- валидацию relay-адресов;
- построение Circuit Relay dial plan;
- определение browser-compatible адресов;
- единый порядок предпочтения транспортов;
- общую тему и интервал PubSub discovery;
- общий пул STUN-серверов WebRTC;
- browser-safe кадрирование PTY-данных/resize и инкрементальное декодирование;
- собственное бинарное кадрирование WebRTC и lifecycle `RTCPeerConnection`;
- подписанные Nostr и WebTorrent tracker signaling adapters;
- canonical pairing token, вращающиеся rendezvous-ключи и AES-GCM envelopes;
- взаимный stream admission и подписанный RouteRecord codec;
- независимое от signaling сопоставление WebRTC actions;
- согласованный flow control WebRTC с окном байтов, подтверждениями, keepalive,
  EOF, abort и восстановлением.

Создание libp2p-узла, DHT, Web Worker RPC и stdin/stdout остаются в платформенных
пакетах.

## Экспортируемый API

| Функция | Назначение |
|---|---|
| `validateService(value)` | Проверяет логический порт `1..65535` |
| `protocolForService(service)` | Строит `/p2p-netcat/1.0.0/{service}` |
| `encodePtyData(value)` | Кадрирует клавиатурные или терминальные байты интерактивного сеанса |
| `encodePtyResize(columns, rows)` | Кадрирует размеры терминала |
| `decodePtyResize(value)` | Декодирует и проверяет размеры терминала |
| `PtyFrameDecoder` | Инкрементально разбирает PTY-фреймы между transport chunks |
| `normalizePeerId(value)` | Проверяет и канонизирует PeerId |
| `normalizeMultiaddr(value)` | Проверяет и канонизирует multiaddr |
| `normalizeRelayAddress(value, options)` | Проверяет relay, WS/WSS и secure-context ограничения |
| `relayedTargetAddress(relay, peerId, options)` | Возвращает Circuit Relay multiaddr цели |
| `createRelayDialPlan(input)` | Возвращает неизменяемый план подключения |
| `browserDialableAddress(address, options)` | Проверяет пригодность адреса для браузера |
| `addressRank(address)` | Возвращает числовой приоритет транспорта |
| `preferDialAddresses(a, b)` | Comparator для сортировки multiaddr |
| `PUBSUB_DISCOVERY_TOPIC` | Отдельная GossipSub-тема discovery приложения |
| `PUBSUB_DISCOVERY_INTERVAL_MS` | Интервал повторной публикации объявления |
| `DEFAULT_STUN_URLS` | Неизменяемый общий список STUN URL |
| `defaultRtcConfiguration()` | Возвращает новую WebRTC-конфигурацию с общим STUN-пулом |
| `webRtcRoomId(peerId, service)` | Строит детерминированную WebRTC room |
| `webRtcAuthPayload(...)` | Строит legacy v1 challenge для compatibility tests |
| `encodeWebRtcAuthResponse(...)` / `decodeWebRtcAuthResponse(...)` | Кодирует или декодирует legacy response v1 |
| `signWebRtcAuthResponse(...)` / `verifyWebRtcAuthResponse(...)` | Legacy compatibility helpers; production endpoints их не используют |
| `webRtcAuthPayloadV2(...)` | Привязывает PeerId, service, signaling session, challenge, роли и точные хэши SDP |
| `signWebRtcAuthResponseV2(...)` | Подписывает channel-bound authentication transcript |
| `verifyWebRtcAuthResponseV2(...)` | Проверяет response v2 и отклоняет legacy downgrade |
| `WebRtcStream` | Адаптирует action-канал к потоку с backpressure, recovery и EOF |
| `createWebRtcActionHub(room, options)` | Связывает data/control actions и lifecycle пира с общими потоками |
| `NativeWebRtcPeer` | Управляет ordered reliable data channel и бинарным протоколом |
| `createNostrSignalingSession(options)` | Открывает подписанный Nostr signaling для одной room |
| `createTorrentSignalingSession(options)` | Открывает signaling через WebTorrent WebSocket trackers |
| `startNativeWebRtcListener(options)` | Отвечает на offer и отдаёт аутентифицированные `WebRtcStream` |
| `connectNativeWebRtc(options)` | Соревнуёт signaling sessions, проверяет PeerId и восстанавливает поток |
| `createPairingToken(input)` / `decodePairingToken(value)` | Создаёт или проверяет canonical bearer token `pnc1_` |
| `pairingProviderCids(token, options)` | Выводит приватные IPFS provider CID предыдущего, текущего и следующего окна |
| `sealPairingPayload(...)` / `openPairingPayload(...)` | Защищает binary payload отдельным AES-256-GCM ключом |
| `authenticateClientStream(...)` / `authenticateServerStream(...)` | Выполняет взаимный admission, не передавая его frames приложению |
| `signRouteRecord(...)` / `verifyRouteRecord(...)` | Подписывает и проверяет route metadata для точного PeerId |

Форматы pairing не зависят от JavaScript и снабжены фиксированными
interoperability vectors для будущей реализации на Go. См.
[спецификацию приватного pairing](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/PAIRING_PROTOCOL.RU.md).

Приоритет сортировки: WebRTC Direct, QUIC v1, WebTransport, WSS, WS, TCP,
прочие адреса и Circuit Relay. Наличие позиции в общем рейтинге не означает,
что конкретная платформа реализует соответствующий транспорт.

`defaultRtcConfiguration()` каждый раз возвращает новый объект, поскольку
реализации WebRTC могут нормализовать или изменять конфигурацию. Сейчас пул
содержит пять Google STUN endpoint, а также CounterPath, Sipgate, VoIPBuster и
InternetCalls. STUN помогает определить NAT mapping, но не является TURN relay
и не гарантирует прямое соединение через symmetric или жёсткий NAT.

`WebRtcStream` объявляет поддержку flow control сообщением `flow:1`. Два
актуальных пира по умолчанию ограничивают неподтверждённые данные окном 256 КиБ
и отправляют `ack:<bytes>` лишь тогда, когда async consumer перешёл к следующему
блоку после обработки текущего. Согласование обратно совместимо: старый пир без
`flow:1` продолжает работать по прежним правилам транспорта. Лёгкие
`ping`/`pong` поддерживают неактивный data channel.

`peerDisconnected()` запускает ограниченное окно восстановления вместо
превращения неожиданной потери WebRTC peer в EOF. Пока `connectionStatus`
равен `reconnecting`, запись остаётся в существующей ограниченной очереди.
`peerReconnected()` продолжает тот же логический поток и сбрасывает устаревшие
flow credits. p2p-netcat по умолчанию использует
`WEBRTC_RECONNECT_GRACE_MS` — 120 секунд. Явный EOF и `abort()` по-прежнему
закрывают поток немедленно.

Core теперь содержит native signaling adapters, SDP controller, бинарный
протокол data channel и аутентифицированный endpoint controller. CLI и PWA
используют только эту реализацию. Прежние имена, привязанные к конкретной
реализации, удалены; публичный API использует нейтральные WebRTC-имена. Протокол
и матрица проверки описаны в
[отчёте о миграции](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/WEBRTC_MIGRATION.RU.md).

Каждый `NativeSignalingSession` предоставляет read-only capability
`trickleIce`. Для Nostr она равна `true`, для tracker — `false`. Поэтому
endpoint controllers публикуют Nostr candidates сразу, а перед WebTorrent
announce ожидают полный SDP. Пользовательский signaling adapter должен задавать
это поле явно.

Минимальный native client:

```js
import {
  connectNativeWebRtc,
  createNostrSignalingSession,
  createSignalingPeerId,
  createTorrentSignalingSession,
  defaultRtcConfiguration,
  verifyWebRtcAuthResponseV2,
  webRtcRoomId
} from 'p2p-netcat-core'

export async function connectToP2pNetcat (targetPeerId, logicalPort = 31337) {
  const roomId = webRtcRoomId(targetPeerId, logicalPort)
  const signalingPeerId = createSignalingPeerId()
  const signalingSessions = await Promise.all([
    createNostrSignalingSession({ roomId, peerId: signalingPeerId, WebSocket }),
    createTorrentSignalingSession({ roomId, peerId: signalingPeerId, WebSocket })
  ])

  const connection = connectNativeWebRtc({
    signalingSessions,
    RTCPeerConnection,
    rtcConfig: defaultRtcConfiguration(),
    verifyAuthResponse: (value, challenge, transcript) =>
      verifyWebRtcAuthResponseV2(
        value,
        targetPeerId,
        logicalPort,
        challenge,
        transcript
      )
  })

  return {
    stream: await connection.promise,
    close: () => connection.close()
  }
}
```

Функция проверяет authentication-response версии 2 для точных PeerId,
логического порта, signaling session и offer/answer SDP вызывающей стороны.
Production endpoints не откатываются к legacy response format.

Если нужно только browser-safe ядро, установите отдельный пакет:

```bash
npm install p2p-netcat-core
```

Пример использования общего плана подключения:

```js
import { createRelayDialPlan } from 'p2p-netcat-core'

const plan = createRelayDialPlan({
  peerId: '12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9',
  service: 31337,
  relay: '/dns4/relay.example/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW',
  requireWebSocket: true,
  secureContext: true
})

console.log(plan.destination)
console.log(plan.protocol)
```
