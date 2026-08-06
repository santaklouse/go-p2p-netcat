# Будущие идеи: relay-only и WSS gateway/agent

[English version](FUTURE_IDEAS.md)

Статус этого документа: **архитектурное предложение, реализация не начата**.
Он задаёт достаточно точные границы для последующей реализации, но не меняет
текущие CLI, wire protocols или гарантии совместимости.

## Цель

Добавить два связанных, но принципиально разных режима:

1. строгий `--relay-only`, при котором обычный `p2p-nc` сохраняет локальный
   PeerId и end-to-end libp2p security, но устанавливает внешние соединения
   только с явно заданным Circuit Relay v2;
2. опциональный thin-client режим `p2p-nc-lite` + `p2p-nc gateway`, при котором
   локальный компьютер держит исходящее WSS-соединение на TCP 443, а полный
   P2P host и его сетевые соединения работают на доверенном Internet server.

Динамические публичные TCP-порты для клиентов не используются. Control и data
переносятся одним versioned WSS protocol на фиксированном endpoint.

## Почему нужны два режима

Они решают похожую задачу reachability, но имеют разную trust boundary.

| Свойство | `--relay-only` | WSS gateway/agent |
|---|---|---|
| Где хранится private identity key | На локальном host | На gateway, отдельно для каждого device |
| Кто является P2P endpoint | Локальный host | Gateway |
| Видит ли relay/gateway application plaintext | Circuit Relay не видит plaintext | Trusted gateway потенциально видит plaintext |
| Размер локальной реализации | Полный `p2p-nc` | Уменьшенный `p2p-nc-lite` |
| Внешние соединения локального host | Только к заданным relays | Только WSS к gateway |
| Совместимость с обычным peer | Полная | Полная со стороны P2P, поскольку gateway запускает обычный protocol handler |

`--relay-only` является рекомендуемым первым этапом. WSS gateway нужен только
когда важнее минимальный agent, централизованный egress или browser-friendly
transport, а gateway входит в доверенную границу пользователя.

## Неподлежащие изменению инварианты

Реализация не должна менять:

- byte-stream protocol `/p2p-netcat/1.0.0/<logical-port>`;
- datagram protocol `/p2p-netcat/udp/1.0.0/<logical-port>`;
- диапазон logical port `1..65535`;
- PTY frame layout и constants;
- pairing token, admission handshake, signed route records и frozen auth-domain
  strings;
- правило «одна UDP datagram — один length-prefixed frame»;
- значение существующего `-i`: interactive PTY, а не адрес gateway;
- различие PeerId и маршрута.

WSS gateway protocol является новым внутренним transport между lite-agent и
gateway. Он не заменяет и не переименовывает существующие P2P protocols.

## Не-цели

Первая реализация не должна:

- обещать анонимность или неотслеживаемость;
- превращать gateway в unauthenticated public proxy;
- загружать существующий локальный identity private key на gateway;
- запускать отдельный `p2p-nc` process для каждой сессии;
- менять browser PWA одновременно с первым Go MVP;
- реализовывать multi-gateway migration, billing или публичный SaaS control
  plane;
- маскировать trusted gateway как end-to-end opaque relay;
- использовать URL query parameters для secrets;
- использовать случайные публичные TCP-порты как data plane.

## Этап 1: строгий `--relay-only`

### Пользовательская семантика

Новый boolean flag:

```text
--relay-only    use only explicitly configured Circuit Relay v2 routes; never dial or accept a direct peer route
```

Пример listener:

```bash
export P2PNC_RELAY=/dns4/relay.example.com/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
p2p-nc -l -k --relay-only --relay "${P2PNC_RELAY}" 34000
```

Пример client:

```bash
export P2PNC_RELAY=/dns4/relay.example.com/tcp/443/wss/p2p/12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9
p2p-nc --relay-only --relay "${P2PNC_RELAY}" 12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW 34000
```

Домены и PeerIds в примерах иллюстративны.

### Validation rules

`--relay-only`:

- требует хотя бы один explicit `--relay` либо relay hint из валидного pairing
  token;
- запрещает `--announce`, потому что direct announced address противоречит
  политике;
- автоматически отключает DHT, mDNS, PubSub discovery, QUIC direct, WebRTC
  Direct и native Nostr/WebTorrent WebRTC;
- совместим с `-T`, только если каждый relay route использует TCP/WS/WSS, а не
  UDP/QUIC;
- разрешает несколько explicit relays для availability, но ни один другой
  Internet peer не может стать outer connection;
- не принимает direct multiaddr, даже если пользователь передал target как
  полный direct address;
- выдаёт явную ошибку, когда relay недоступен; direct fallback запрещён.

Проверка выполняется в два этапа: Cobra validation проверяет явно переданные
flags, а после `loadToken` выполняется final validation effective relay list.
Иначе команда с relay hint только внутри pairing token будет ошибочно
отклонена до чтения token.

Pairing mode не должен неявно включать private DHT или native WebRTC в этом
режиме. Relay hints из token разрешены, но все остальные route hints должны
быть отброшены.

### Route enforcement

Недостаточно только изменить порядок dial candidates. Политика должна
гарантироваться на нижнем уровне:

1. разобрать и зафиксировать множество разрешённых relay PeerIds и их base
   multiaddrs;
2. подключаться напрямую только к этим relay PeerIds;
3. нормализовать relay address так, чтобы он заканчивался
   `/p2p/<relay-id>`, и строить target route только добавлением
   `/p2p-circuit/p2p/<target-id>`;
4. удалить/игнорировать direct target addresses из peerstore при открытии
   application stream;
5. отклонять direct inbound connections; accepted application connection
   должна иметь relayed remote multiaddr с `p2p-circuit`;
6. публиковать для listener только circuit addresses, не transport listen
   addresses;
7. не запускать route race с native WebRTC или обычным `Node.OpenStream` без
   route policy.

Relay-only host предпочтительно создаётся без физических listen addresses:
исходящее соединение с relay и reservation должны быть достаточны для приёма
виртуальных relayed streams. Это поведение нужно доказать integration test на
текущей версии go-libp2p; если внутренний transport listener всё же необходим,
его address нельзя announce и direct inbound должен блокироваться gater.

Если используется libp2p `ConnectionGater`, нельзя слепо запрещать target в
`InterceptPeerDial`: target PeerId присутствует и в виртуальном relayed
connection. Проверка должна отличать direct multiaddr от `p2p-circuit` route.
Основной механизм безопасности — построение только circuit candidate и
фильтрация address-level dial/inbound; gater является дополнительной защитой.

### Предлагаемые изменения исходников

- `internal/cli/root.go`: flag, validation, diagnostics и mode tests;
- `internal/cli/node_config.go`: передача typed route policy;
- `p2p/node.go`: `RoutePolicy` либо `RelayOnly bool`, address filtering,
  circuit-only announce и inbound enforcement;
- P2P stream open helpers: отдельный circuit-only opener без direct race;
- tests: unit validation, address construction и реальные integration hosts.

Предпочтительно определить enum, а не накапливать booleans:

```go
type RoutePolicy uint8

const (
	RoutePolicyAny RoutePolicy = iota
	RoutePolicyRelayOnly
)
```

`RoutePolicyAny` обязан сохранить текущее поведение без изменений.

### Критерии готовности этапа 1

- direct target доступен и relay доступен: соединение использует только
  `p2p-circuit`;
- direct target доступен, relay не работает: команда завершается ошибкой и не
  соединяется напрямую;
- listener не объявляет direct address и не принимает direct stream;
- pairing token с direct и relay hints использует только relay hints;
- два explicit relays могут использоваться как fallback, не открывая другие
  outer connections;
- raw, PTY, TCP forwarding и framed UDP проходят через relay;
- существующие тесты без `--relay-only` сохраняют поведение;
- diagnostics явно печатают `route policy: relay-only` в verbose mode.

## Этап 2: trusted WSS gateway и `p2p-nc-lite`

### Trust model

Это сознательно **trusted gateway**:

- TLS/WSS защищает local-to-gateway hop;
- libp2p Noise/TLS защищает gateway-to-peer hop;
- gateway находится между этими hop и потенциально может видеть application
  bytes, metadata и pairing material;
- это не эквивалент Circuit Relay v2 с end-to-end libp2p encryption;
- для недоверенного server пользователь должен выбирать `--relay-only`.

CLI и документация обязаны показывать это предупреждение до включения режима.

### Identity model

Каждое зарегистрированное device получает отдельную постоянную Ed25519
identity на gateway. Нельзя использовать один общий PeerId для всех tenants:
это создаст collisions logical ports и смешает authorization boundaries.

- private key генерируется и хранится gateway с permission `0600` либо в
  encrypted secret store;
- agent получает PeerId, но не получает private key;
- один device может одновременно слушать несколько logical ports;
- разные devices могут использовать одинаковый logical port, потому что у них
  разные PeerIds;
- удалённый обычный `p2p-nc` видит hosted device PeerId;
- pairing token привязывается к hosted PeerId и logical port;
- удаление device должно отзывать gateway credential, закрывать sessions и
  безопасно удалять либо архивировать hosted identity по явной policy.

Перенос hosted identity между gateway instances не входит в первый MVP.

### P2P network topology на gateway

Один libp2p host не может владеть несколькими PeerIds. Поэтому active device
получает отдельный `p2p.Node`, лениво запущенный с его hosted private key после
успешного `HELLO`. Нельзя пытаться разделить один host между device identities.

Чтобы per-device hosts не занимали случайные public ports, они не слушают
Internet transports напрямую. Каждый из них делает reservation на одном
общем Circuit Relay v2, запущенном на том же VPS либо в доверенной gateway
infrastructure:

```text
local agent ── WSS 443 ──> gateway control/data service
                              ├── device host A / PeerId A ──┐
                              ├── device host B / PeerId B ──┼──> shared Circuit Relay v2 / WSS 443
                              └── device host C / PeerId C ──┘
remote peer ─────────────────────────────────────────────────> relay circuit address of device
```

Gateway WSS и Circuit Relay WSS являются разными logical endpoints. Reverse
proxy может публиковать их на двух DNS names, например
`gateway.example.com:443` и `relay.example.com:443`, на одном VPS. Это сохраняет
фиксированный TCP 443 и не требует per-device ports.

- hosted listener объявляет только circuit address через shared relay;
- hosted client может набирать remote peer полным обычным route selection, но
  все outer sockets всё равно создаются на VPS;
- relay reservation появляется до ответа `LISTEN_OK`;
- при disconnect agent gateway удаляет stream handlers, закрывает hosted node
  и освобождает reservation после bounded drain period;
- одна active identity означает один hosted node; `--max-devices` обязан
  учитывать memory, file descriptors и relay reservation capacity;
- стандартный relay limit 128 reservations нельзя сочетать с более высоким
  `--max-devices` без явного увеличения и load tests.

Эта внутренняя Circuit Relay ступень не превращает gateway в opaque relay:
hosted libp2p Noise/TLS session всё равно завершается в per-device node на VPS.

### Process model

Gateway является одним долгоживущим service и использует Go packages напрямую.
Запрещено запускать `p2p-nc` subprocess на session. Предлагаемая структура:

```text
cmd/p2p-nc-lite/        отдельный маленький binary
gateway/                embeddable trusted gateway service
protocol/gateway/       WSS framing, CBOR control messages, validation
internal/gatewaycli/    gateway Cobra command and administration
internal/lite/          agent lifecycle and local session adapters
```

Существующие `protocol/admission`, `protocol/datagram`, `protocol/pty` и
`session` должны переиспользоваться, а не копироваться.

### Публичный endpoint

Один endpoint на TCP 443:

```text
wss://relay.example.com/v1/agent
```

WebSocket subprotocol:

```text
p2p-netcat-gateway-v1
```

TLS может завершаться в gateway либо доверенном reverse proxy. При reverse
proxy предел body/frame, idle timeout и forwarding реального client IP должны
быть настроены явно. Plain `ws://` разрешён только в loopback tests.

REST используется только для optional administration. Listener/client session
не создаются через unauthenticated `curl`; они создаются control frames внутри
аутентифицированного WSS connection.

### Gateway authentication

Gateway credential отделён от P2P pairing token.

Предлагаемый provisioning command:

```bash
p2p-nc gateway token create \
  --data-dir /var/lib/p2p-netcat-gateway \
  --device alex-laptop \
  --expires 720h \
  --output /var/lib/p2p-netcat-gateway/alex-laptop.token
```

Credential имеет отдельный prefix `pncg1_`, содержит не менее 256 random bits
и хранится server-side только как password hash/KDF result. Agent передаёт его
в HTTP header:

```text
Authorization: Bearer pncg1_...
```

Запрещено передавать credential в URL, query string, WebSocket subprotocol или
логировать его. Token file на agent должен проверяться как private regular
file аналогично pairing token files.

Первый MVP может использовать bearer authentication. mTLS и OIDC являются
последующими mutually exclusive auth backends.

### WSS multiplexing protocol v1

Один WSS connection переносит control и несколько data channels. Одно binary
WebSocket message содержит ровно один gateway frame.

Header имеет фиксированные 16 bytes:

```text
0               4 5 6     8            12            16
+----------------+-+-+-+-+--------------+--------------+
| magic "PNGW"  |v| type | flags (u16) | channel(u32) |
+----------------+-+-+-+-+--------------+--------------+
| payload length (u32)    | payload ...                |
+-------------------------+----------------------------+
```

Точная раскладка:

- bytes `0..3`: ASCII `PNGW`;
- byte `4`: version, для этого protocol `1`;
- byte `5`: frame type;
- bytes `6..7`: big-endian flags, в v1 должны быть zero;
- bytes `8..11`: big-endian channel ID; zero означает control channel;
- bytes `12..15`: big-endian payload length;
- оставшиеся bytes: payload ровно указанной длины.

Limits:

- control payload: максимум 64 KiB;
- `DATA`/`DATAGRAM`: максимум 64 KiB на frame;
- WebSocket message: максимум header + 64 KiB;
- unknown version, non-zero reserved flags, wrong length или unknown required
  frame type закрывают connection с protocol error;
- even channel IDs назначает agent, odd channel IDs назначает gateway; zero
  никогда не используется для data.

Control payload кодируется deterministic CBOR с integer map keys. Codecs
должны иметь golden vectors и reject duplicate keys, indefinite lengths,
unknown required fields и oversized strings/arrays.

Frame types v1:

| Hex | Name | Direction | Channel | Назначение |
|---:|---|---|---:|---|
| `01` | `HELLO` | agent → gateway | 0 | Version, device name, capabilities |
| `02` | `HELLO_OK` | gateway → agent | 0 | Hosted PeerId, limits, connection ID |
| `03` | `LISTEN` | agent → gateway | 0 | Зарегистрировать logical service |
| `04` | `LISTEN_OK` | gateway → agent | 0 | Listener готов и объявлен |
| `05` | `UNLISTEN` | agent → gateway | 0 | Удалить listener |
| `06` | `DIAL` | agent → gateway | 0 | Набрать exact PeerId/service |
| `07` | `DIAL_OK` | gateway → agent | data ID | P2P stream готов |
| `08` | `INCOMING` | gateway → agent | data ID | Новый incoming P2P stream |
| `09` | `ACCEPT` | agent → gateway | data ID | Agent принял incoming stream |
| `0a` | `REJECT` | agent → gateway | data ID | Agent отказал до application data |
| `10` | `DATA` | both | data ID | Byte-stream bytes |
| `11` | `DATAGRAM` | both | data ID | Ровно одна application datagram |
| `12` | `EOF` | both | data ID | Graceful half-close/write EOF |
| `13` | `CLOSE` | both | data ID | Graceful full close после drain |
| `14` | `RESET` | both | data ID | Abort without drain |
| `15` | `WINDOW_UPDATE` | both | data ID | Credit-based flow control |
| `16` | `PING` | both | 0 | Liveness nonce |
| `17` | `PONG` | both | 0 | Exact liveness nonce response |
| `18` | `ERROR` | gateway → agent | 0 или data ID | Structured bounded error |

`HELLO`, `LISTEN` и `DIAL` содержат monotonically increasing `request_id`.
Соответствующий response повторяет его, поэтому control operations можно
безопасно сопоставлять при concurrent requests.

Минимальные semantic fields:

- `HELLO`: protocol version `1`, device label, supported stream kinds,
  supported session modes, agent version;
- `HELLO_OK`: hosted PeerId, gateway connection ID, initial window, max
  channels, max frame, idle timeout;
- `LISTEN`: request ID, logical port, stream kind `byte`/`datagram`, keep-open,
  optional pairing token и explicit relay hints;
- `DIAL`: request ID, exact target PeerId, logical port, stream kind, optional
  pairing token, explicit relay hints и connect timeout;
- `INCOMING`: logical port, stream kind, authenticated remote PeerId when known;
- `ERROR`: stable machine code plus bounded English message.

Exact CBOR integer keys и error codes должны быть зафиксированы в отдельном
`docs/GATEWAY_PROTOCOL.md` одновременно с кодом. Этот future document задаёт
семантику, но не считается wire specification до появления golden vectors.

### Admission и pairing

Gateway создаёт/принимает libp2p stream, но lite-agent владеет session mode.
Pairing admission bytes проходят через WSS channel к agent до application data.
Agent переиспользует `protocol/admission` как client или server.

Gateway должен использовать pairing token для private discovery/signaling,
если такой route необходим. Поэтому trusted mode разрешает передавать token в
зашифрованном WSS `LISTEN`/`DIAL` payload, но:

- token хранится только в памяти сессии;
- token, rendezvous и derived keys не попадают в logs/metrics/errors;
- после завершения route setup лишние buffers зануляются, где это практически
  возможно;
- admission всё равно выполняется agent, а не считается пройденным только
  потому, что gateway получил token.

Это уменьшает accidental exposure, но не делает gateway недоверенным.

### Session ownership

`p2p-nc-lite`, а не gateway, выполняет локальные действия:

- `-i`: создаёт PTY/ConPTY на локальном компьютере;
- `-e`: запускает локальную command;
- listener `-p`: соединяется с local destination;
- client `-p`: открывает local forwarding listener;
- `-S`: запускает SOCKS session относительно agent host;
- raw mode: соединяет WSS channel со stdin/stdout;
- UDP: сохраняет datagram boundaries.

Gateway никогда не должен интерпретировать `-i` как просьбу запустить shell на
VPS. Он переносит protocol bytes и metadata между P2P stream и agent channel.

### CLI `p2p-nc-lite`

CLI должен максимально сохранять существующую netcat-style семантику.
Gateway задаётся отдельным long option; `-i` не переиспользуется как адрес.

Listener example:

```bash
p2p-nc-lite -l -k -i \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token \
  --pairing-token-file /home/alex/.config/p2p-netcat/shell.token \
  34000
```

Client example:

```bash
p2p-nc-lite -i \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token \
  --pairing-token-file /home/alex/.config/p2p-netcat/shell.token \
  12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW \
  34000
```

Identity query:

```bash
p2p-nc-lite id \
  --gateway wss://relay.example.com/v1/agent \
  --gateway-token-file /home/alex/.config/p2p-netcat/gateway.token
```

Первый MVP поддерживает raw, PTY и TCP forwarding. UDP, SOCKS и `-e` добавляются
после стабильных close/flow-control tests, но wire design сразу резервирует их.

### Gateway CLI

Предлагаемый startup за Caddy/nginx:

```bash
p2p-nc gateway \
  --listen 127.0.0.1:8080 \
  --public-url wss://relay.example.com/v1/agent \
  --data-dir /var/lib/p2p-netcat-gateway \
  --max-devices 100 \
  --max-channels-per-device 64 \
  --idle-timeout 2m
```

Runtime messages остаются English по правилам проекта. Server обязан корректно
обрабатывать SIGINT/SIGTERM: прекратить новые upgrades, закрыть listeners,
послать bounded shutdown error, дать активным frames короткое drain window и
после этого reset streams.

### Flow control и backpressure

Простой `io.Copy` между WebSocket и несколькими P2P streams недостаточен.

- initial send window на channel: 256 KiB;
- `DATA` и `DATAGRAM` уменьшают credit на payload length;
- receiver посылает `WINDOW_UPDATE` только после передачи bytes следующему
  consumer;
- sender с нулевым credit блокирует конкретный channel, но не control channel
  и не остальные data channels;
- per-channel queued unsent data не превышает 256 KiB;
- per-device aggregate queue имеет отдельный hard limit;
- превышение limits вызывает channel `RESET`, а repeated abuse закрывает WSS;
- control frames имеют priority над data;
- WebSocket write выполняется одним serialized writer goroutine, чтобы не
  нарушать требования выбранной library.

Для `DATAGRAM` credit учитывает полный payload, а frame всегда содержит ровно
одну datagram. Нельзя объединять или делить одну datagram между frames.

### Close semantics

- `EOF` соответствует graceful `CloseWrite` и не закрывает обратное направление;
- `CLOSE` отправляется после drain обоих направлений;
- `RESET` соответствует abort и должен вызвать stream reset;
- normal Unix PTY `EIO` после закрытия последнего slave трактуется как EOF;
- gateway disconnect в MVP сбрасывает все channels и local sessions;
- автоматический resume не входит в MVP; последующая версия может добавить
  120-second grace только с versioned resume protocol и bounded replay buffer.

### Security requirements

До merge обязательны:

- TLS 1.2 minimum, предпочтительно TLS 1.3; plain WS только loopback tests;
- constant-time credential verification;
- per-IP и per-device rate limits для upgrade, auth, LISTEN и DIAL;
- deadlines на HTTP headers, WebSocket handshake, HELLO, route setup, idle и
  shutdown;
- limits на devices, listeners, channels, frames, queues, bytes и session TTL;
- exact PeerId validation, logical port validation и stream-kind validation;
- запрет произвольного TCP destination на gateway: destination относится к
  локальному agent либо к P2P PeerId, но не превращает VPS в SSRF proxy;
- secrets redaction во всех logs, errors, traces и metrics;
- Origin allowlist для browser use; non-browser agent всё равно требует bearer
  authentication;
- CSRF protection для любых cookie-based administration endpoints;
- no compression (`permessage-deflate` off) в MVP, чтобы уменьшить memory/side
  channel surface;
- audit events без payload: device, operation, result, byte counters, duration;
- graceful revocation: revoked credential закрывает active WSS и listeners;
- fuzz tests для frame/CBOR decoders и race-enabled lifecycle tests.

Gateway должен по умолчанию bind на loopback, если TLS не настроен внутри
процесса. Public plaintext bind требует отдельного явно unsafe override и не
должен появляться в документационных примерах.

### Observability

Metrics не содержат PeerId полностью, tokens, logical payload или command
arguments. Разрешены:

- active devices/listeners/channels;
- auth failures по bounded reason;
- route setup latency;
- bytes/frames by stream kind;
- resets, graceful closes и protocol errors;
- queue utilization и backpressure events.

PeerId в metrics заменяется keyed, rotating pseudonymous label; full PeerId
может присутствовать только в access-controlled audit log при явной policy.

### Критерии готовности WSS MVP

End-to-end integration matrix должна доказать:

1. обычный client ↔ hosted listener/agent: bidirectional raw bytes;
2. lite client/gateway ↔ обычный listener: bidirectional raw bytes;
3. pairing success, wrong token rejection и отсутствие application bytes до
   admission;
4. PTY resize, shell `exit`, final output, graceful EOF и abort;
5. TCP forwarding в обоих направлениях;
6. два devices с одинаковым logical port и разными hosted PeerIds не
   конфликтуют;
7. один device не может принять stream другого device;
8. WSS disconnect очищает listeners, P2P streams, goroutines и local sockets;
9. slow consumer ограничен flow window и не вызывает unbounded memory growth;
10. malformed/oversized/unknown frames отклоняются детерминированно;
11. revoked/expired gateway credential не может открыть или сохранить session;
12. gateway logs не содержат gateway credential или pairing token;
13. `go test -race` не обнаруживает races при concurrent open/close/reset;
14. soak с 64 concurrent channels переживает не менее часа без goroutine или
    memory leak.

После raw/PTY/TCP MVP отдельные acceptance tests добавляются для UDP packet
boundaries, idle associations, SOCKS authorization и local `-e` execution.

## Порядок реализации

Каждый пункт должен быть отдельным reviewable change:

1. добавить typed `RoutePolicy` и unit tests без изменения CLI;
2. добавить `--relay-only`, validation и circuit-only integration tests;
3. обновить English/Russian CLI docs для relay-only;
4. зафиксировать `GATEWAY_PROTOCOL.md`, constants, codecs, golden vectors и
   fuzz tests;
5. реализовать gateway credential store и hosted device identities;
6. реализовать authenticated HELLO и empty multiplexed WSS connection;
7. реализовать LISTEN/DIAL/INCOMING и raw byte channels;
8. добавить admission/pairing и exact PeerId checks;
9. добавить flow control, EOF/CLOSE/RESET и cleanup stress tests;
10. реализовать `cmd/p2p-nc-lite` raw mode;
11. переиспользовать local PTY и TCP forwarding adapters;
12. пройти security review до добавления UDP/SOCKS/exec;
13. добавить UDP с packet-boundary tests;
14. добавить reconnect/resume только как protocol v2 либо negotiated v1
    extension, не меняя молча v1 semantics;
15. рассмотреть browser PWA adapter после стабилизации Go agent.

## Проверка

При реализации выполнять сначала targeted tests, затем полный набор проекта:

```bash
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go test ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go test -race -count=1 -timeout=25m ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go vet ./...
GOTOOLCHAIN=auto /opt/homebrew/opt/go/libexec/bin/go run ./cmd/webrtc-soak --profile smoke
bash scripts/sync-wiki_test.sh
```

Для gateway integration tests следует поднимать loopback TLS server с
одноразовым test CA; production tests не должны зависеть от внешнего domain.

## Открытые решения перед кодом

До начала WSS implementation нужно зафиксировать отдельным design review:

- concrete WebSocket library и её concurrency/close semantics;
- deterministic CBOR library/configuration;
- encrypted-at-rest backend для hosted identities;
- reverse-proxy trust и trusted proxy CIDRs;
- должен ли pairing token передаваться gateway для private discovery в MVP
  либо MVP ограничивается explicit relay routes;
- packaging `p2p-nc-lite` и допустимый binary size;
- policy удаления hosted identity после device revocation.

Эти решения не разрешают ослаблять описанные trust warnings, framing limits,
route guarantees или compatibility invariants.
