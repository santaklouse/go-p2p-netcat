# Архитектура go-p2p-netcat

[English version](ARCHITECTURE.md)

Репозиторий объединяет каноническую Go-реализацию CLI/сети, browser-safe
TypeScript core и статический PWA.

## Граница совместимости

- identity: protobuf-ключи libp2p Ed25519 и постоянный PeerId;
- прикладной протокол: `/p2p-netcat/1.0.0/<логический-порт>`;
- PTY frames: тип в одном байте и big-endian 32-битная длина payload;
- pairing: deterministic CBOR token `pnc1_`, HKDF-SHA-256, AES-256-GCM,
  вращающийся rendezvous и фиксированный mutual admission handshake;
- native WebRTC: protocol v2, ordered data channel `p2p-netcat-v2`,
  исторический домен подписи `p2p-netcat/trystero-auth/v1` и одинаковые
  control frames.

## Выбор маршрута Go CLI

Go-host поддерживает TCP, QUIC v1, WebSocket, libp2p WebRTC Direct, Noise/TLS,
Yamux, Circuit Relay v2, mDNS, подписанный GossipSub discovery и IPFS Amino
DHT. PeerId — identity, а не маршрут: адрес должен прийти через mDNS,
GossipSub, DHT provider record, bootstrap peer, явный multiaddr или relay.

Порядок набора: native WebRTC Direct, QUIC, WebTransport, WSS, WS, прямой TCP,
Circuit Relay и остальные адреса. Собственная native WebRTC-ветка работает
параллельно libp2p и одновременно использует Nostr и WebTorrent signaling.
Побеждает первый аутентифицированный маршрут.

Pairing-token режим отключает публичный discovery, выводит приватные
DHT/signaling rendezvous, шифрует signaling и аутентифицирует поток до
передачи прикладных байтов.

## Native WebRTC

Listener подписывает 32-байтный challenge постоянной libp2p identity. Клиент
восстанавливает точный ожидаемый PeerId из public key и проверяет подпись,
привязанную к logical service. Data/control frames реализуют EOF, abort,
keepalive, acknowledgement и flow window 256 КиБ.

Неожиданный разрыв запускает 120-секундное окно reconnect. Новые offer
используют ту же signaling identity, прикрепляют новый Pion data channel к
существующему логическому потоку и обмениваются `resume`/`flow:1`. Очередь
записи и PTY-процесс переживают кратковременный ICE failure.

Nostr использует короткоживущие подписанные события kind 25050 в хешированной
теме. WebTorrent использует 20-символьный tracker `peer_id`, ограниченный пул
offer и complete non-trickle SDP. WebSocket обоих адаптеров переподключается с
ограниченным backoff.

## Браузерный PWA

React UI общается с module Web Worker. Worker владеет browser libp2p,
IndexedDB-кешем маршрутов, GossipSub, Delegated Routing, DHT и relay dialing.
Native WebRTC работает рядом через browser-реализацию из `packages/core`.
Service Worker только кеширует статическую оболочку.

HTTPS-браузер принимает WebTransport, native WebRTC или secure WebSocket
маршруты; обычный TCP, QUIC и небезопасный WS ему недоступны.

## Сеансы

Принятый поток подключается к одному из режимов:

- raw stdin/stdout;
- выполнение shell-команды;
- локальный или удалённый TCP forwarding;
- SOCKS4/4a/5 CONNECT;
- интерактивный PTY (Unix PTY либо Windows ConPTY).

`-w` ограничивает discovery/connect, а при явном указании в raw-режиме также
задаёт inactivity timeout. `-k` оставляет listener открытым для новых сеансов.

## Relay

Явные relay подключаются как постоянные peers. Без явного relay listening-host
может зарезервировать подходящий подключённый Circuit Relay v2 peer,
обнаруженный через mesh. Relay server допускает 128 reservations со
стандартным пределом два часа/128 МиБ и включает GossipSub peer exchange.

## Карта исходников

| Путь | Ответственность |
|---|---|
| `p2p/` | libp2p host, transports, discovery, DHT, relay reservations |
| `nativewebrtc/` | Pion endpoint, signaling, authentication, reconnecting stream |
| `protocol/` | wire-форматы pairing, admission, PTY и signed route |
| `session/` | raw, exec, forwarding, SOCKS, PTY, ConPTY |
| `relay/` | публичный встраиваемый relay API |
| `internal/cli/` | CLI validation и оркестрация listener/client |
| `packages/core/` | browser-safe protocol и native WebRTC library |
| `web/` | статический двуязычный PWA |
