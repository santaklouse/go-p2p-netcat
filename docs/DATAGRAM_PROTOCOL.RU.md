# Протокол datagram forwarding

[English version](DATAGRAM_PROTOCOL.md)

## Цель

Datagram-протокол переносит UDP-сервисы с фиксированным назначением — WireGuard,
OpenVPN UDP, DNS и игровой traffic — через ту же инфраструктуру PeerId
discovery, pairing authentication и relay, что и stream-режимы p2p-netcat.

Он должен:

- сохранять границу каждого UDP datagram, включая пакеты нулевой длины;
- возвращать ответы точному локальному source endpoint запроса;
- работать через прямой TCP, QUIC, WebSocket, libp2p WebRTC Direct и Circuit
  Relay v2;
- поддерживать UDP-over-TCP, когда внешняя сеть блокирует UDP;
- не менять существующий `/p2p-netcat/1.0.0/<логический-порт>`.

SOCKS5 UDP ASSOCIATE, broadcast/multicast, произвольное назначение в каждом
пакете и ненадёжный native QUIC datagram mode не входят в эту версию.

## Идентификатор протокола

UDP forwarding использует:

```text
/p2p-netcat/udp/1.0.0/<логический-порт>
```

Отдельный идентификатор не позволяет UDP-клиенту случайно отправить framed
пакеты в raw, PTY, command, SOCKS или TCP-forwarding listener. Logical port
остаётся в диапазоне `1..65535`. Pairing token по-прежнему привязывает PeerId и
logical port; обычный admission handshake завершается до передачи datagram
frames.

## Формат frame

Stream содержит последовательность frames:

```text
0               1               2
+---------------+---------------+-----------------------------+
| payload length, uint16 BE     | payload, exactly N bytes    |
+---------------+---------------+-----------------------------+
```

- размер header: 2 байта;
- длина payload: `0..65535`;
- порядок байтов: network/big-endian;
- нет padding, адреса, checksum или неявного delimiter;
- EOF внутри header или payload завершает association.

UDP уже имеет checksum. P2P transport обеспечивает authenticated encryption и
stream integrity, поэтому ещё один frame checksum добавил бы стоимость, не
обнаруживая новый класс ошибок.

## Модель association

Клиент связывает один локальный UDP-порт. Ключ association — полный локальный
source address и port, возвращённые UDP socket.

1. Клиент заранее устанавливает один carrier до объявления локального socket;
   первый source занимает этот stream. Последующие sources создают независимые
   P2P datagram streams.
2. Пакеты source стоят в порядке поступления, пока открывается его stream.
3. Listener открывает один connected UDP socket к настроенной фиксированной
   цели.
4. Ответы цели кодируются в тот же P2P-stream и возвращаются исходному
   локальному source.
5. Другой source endpoint получает отдельный P2P-stream и remote UDP socket.
6. EOF, cancellation, I/O error или idle timeout удаляет association.
   Следующий пакет может создать её снова.

Эта схема не передаёт локальные source addresses по сети и изолирует congestion
и failure разных локальных приложений. Рассматривался единый
address-tagged multiplexed stream, но он разделял бы head-of-line blocking и
failure между всеми UDP-клиентами и усложнял authorization boundary.

Один локальный forwarder ограничен 256 одновременными source associations и
очередью до 256 пакетов во время открытия stream или временного backpressure.
UDP semantics допускает потерю пакетов при превышении этих границ.

## Варианты транспорта

### Надёжный framing поверх P2P-stream — реализован

Это общий режим `-u`. Он работает через собственный native WebRTC stream и
любой route стандартного Go libp2p-stream:

- native WebRTC с Nostr/WebTorrent signaling и прохождением NAT через ICE/STUN;
- QUIC и libp2p WebRTC Direct при доступном UDP;
- TCP и WSS через ограничивающие firewall;
- Circuit Relay v2;
- доступ к TCP/WSS relay через Tor.

Преимущество — один protocol и security model для всех маршрутов. Цена —
упорядоченная надёжная доставка. При TCP route потеря одного внешнего segment
задерживает последующие datagrams. Это особенно заметно, когда VPN внутри
переносит TCP.

### Ненадёжные native QUIC datagrams — рассмотрены, не реализованы

Native QUIC datagrams могли бы исключить head-of-line blocking упорядоченного
stream на прямом route. Сейчас они не дают одинакового поведения через Circuit
Relay v2, TCP/WSS, Tor и browser/native adapter. Будущий optional protocol также
потребует capability negotiation, явных semantics потери/перестановки, поиска
допустимого datagram size и fallback к надёжному framed protocol.

### Один P2P-stream на пакет — отклонено

Открытие и аутентификация stream для каждого UDP-пакета сохранили бы границы,
но создавали бы слишком много allocations, multistream negotiation, admission
handshakes и relay load. Долгоживущая association значительно дешевле и
соответствует стабильному endpoint WireGuard.

## Рекомендации для WireGuard

- Оставляйте client forwarder на loopback, если LAN-доступ не требуется.
- Используйте forwarding port, отличный от собственного `ListenPort`
  WireGuard interface.
- Задайте `PersistentKeepalive = 25` для пиров за NAT и сохранения стандартной
  пятиминутной association p2p-netcat.
- Используйте `--udp-idle-timeout 0`, только если действительно требуется
  неограниченная idle lifetime.
- Начните с `MTU = 1280` и увеличивайте после path testing.
- Предпочитайте native WebRTC, прямой QUIC или libp2p WebRTC Direct; TCP/WSS и
  relay нужны, когда reachability важнее latency.
- Защищайте private pairing token listener, имеющий доступ к непубличному
  UDP-сервису.
- Для WireGuard-клиента с `0.0.0.0/0` на Linux запускайте p2p-netcat через
  `deploy/wireguard-full-tunnel.sh`. Предварительное соединение устраняет
  startup race, а policy rule отдельного UID также защищает DNS, signaling,
  ICE и reconnect после изменения default route.

## Граница браузера

Статический browser client не может открыть локальный UDP socket и поэтому не
предоставляет этот forwarding mode. Go-пиры могут переносить те же datagram
frames через собственный Nostr/WebTorrent native WebRTC adapter, используя
ICE/STUN для прохождения NAT без собственного media relay.
