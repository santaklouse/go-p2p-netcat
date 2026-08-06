# p2p-netcat web

[English](README.md) | **Русский**

Общий алгоритм взаимодействия core-пакета, CLI, browser discovery и защищённого
потока подробно разобран в
[`docs/ARCHITECTURE.RU.md`](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/ARCHITECTURE.RU.md).

Статический браузерный клиент для `p2p-netcat`. В проекте нет SSR, API routes,
базы данных и серверных скриптов: production-сборка состоит только из HTML,
CSS, JavaScript, Web Worker, Service Worker, manifest и изображений.

## Возможности

- подключение к CLI-серверу по `PeerId` и логическому порту;
- автоматический поиск через подписанные GossipSub-объявления, HTTP Delegated
  Routing и IPFS Amino DHT;
- pairing token `pnc1_` для приватного вращающегося discovery, зашифрованного
  signaling, взаимного admission, привилегированных CLI-listener и native
  WebRTC;
- собственный прямой WebRTC через подписанные Nostr events и публичные
  WebTorrent trackers;
- WebTransport или WebSocket/WSS через libp2p Circuit Relay v2;
- необязательный ручной relay multiaddr как аварийный override;
- Noise-шифрование и Yamux внутри отдельного Web Worker;
- терминальный виджет для текста и бинарного вывода;
- интерактивный xterm-совместимый PTY-клиент для listener, запущенного с `-i`;
- отправка текста, файлов и EOF, сохранение принятых байтов в файл;
- устанавливаемая PWA с офлайн-кешем интерфейса и автообновлением Service Worker;
- адаптивный интерфейс для компьютера, планшета и телефона.

## Подключение к серверу `-i`

Создайте pairing token для сервиса и запустите защищённый CLI listener:

```bash
install -d -m 0700 ~/.config/p2p-netcat
p2p-nc token 31337 \
  --identity ~/.config/p2p-netcat/identity.key \
  >~/.config/p2p-netcat/web-pty.token
chmod 0600 ~/.config/p2p-netcat/web-pty.token
p2p-nc -l -i \
  --identity ~/.config/p2p-netcat/identity.key \
  --pairing-token-file ~/.config/p2p-netcat/web-pty.token
```

В веб-интерфейсе вставьте содержимое `web-pty.token`, затем включите
переключатель **«Интерактивный PTY -i»** до подключения. Token заполнит PeerId
и логический порт. В этом режиме браузер
использует тот же фреймированный PTY-протокол, что и CLI-клиент: клавиатурный
ввод передаётся напрямую, ANSI-последовательности обрабатываются терминалом, а
изменение размера виджета отправляется удалённому `node-pty`.

Для выхода введите `exit` или нажмите `Ctrl-E`, затем `q`. Переключатель режима
явный, потому что обычный поток и PTY используют один логический protocol ID и
сервер не отправляет отдельное сообщение согласования режима. Если listener
запущен без `-i`, оставьте этот переключатель выключенным.

Секрет хранится только в React state текущей страницы и не записывается в
`localStorage`. Полный формат описан в
[спецификации pairing](https://github.com/santaklouse/go-p2p-netcat/blob/main/docs/PAIRING_PROTOCOL.RU.md).

## Архитектура

Web Worker импортирует browser-safe пакет
[`p2p-netcat-core`](https://www.npmjs.com/package/p2p-netcat-core).
Go CLI независимо реализует те же wire-форматы.
Общими являются protocol ID, проверка PeerId и логического порта, PTY codec,
правила WS/WSS и построение Circuit Relay dial plan. Core также управляет native
Nostr/tracker signaling, lifecycle `RTCPeerConnection`, data-channel framing и
восстановлением WebRTC stream. В самом веб-проекте остаются Delegated Routing,
DHT-клиент, libp2p WebTransport/WebSocket-транспорты, обмен сообщениями с Worker,
терминальный интерфейс и PWA/Service Worker. Серверного JavaScript-кода у этой
архитектуры нет.

Для большого терминального вывода используются два ограниченных уровня flow
control. Worker перестаёт читать libp2p-поток, когда UI ещё не обработал 512
КиБ, и продолжает после снижения очереди до 128 КиБ. Main thread подтверждает
каждый блок только из callback завершения `Terminal.write` в xterm. Для
Для WebRTC общее ядро дополнительно согласует окно в 256 КиБ неподтверждённых
данных. Интерактивный вывод не сохраняется в буфер скачиваемого ответа, поэтому
долгий shell не удерживает всю историю в JavaScript-памяти; видимая история
остаётся в ограниченном scrollback xterm.

Потеря WebRTC peer больше не считается немедленным EOF для PTY. UI переходит
в состояние «Переподключение», сервер сохраняет существующий shell, а
ограниченная очередь записи ждёт тот же peer до 120 секунд. Если за это время
native endpoint controller перестроит WebRTC data channel, продолжаются прежний
поток и PTY. Обычная смена фокуса окна не вызывает `stop()`. Полная выгрузка
вкладки из памяти или reload отличается тем, что уничтожает
JavaScript-контекст браузера и его временную signaling identity.

При пустом поле relay и наличии pairing token одновременно запускаются native
WebRTC и Worker. Native WebRTC соревнуёт подписанные Nostr events и WebTorrent
tracker announce. Worker запрашивает только provider CID из секрета, а native
signaling шифрует SDP/ICE. Без pairing token native WebRTC отключён и
используется только libp2p-маршрут Worker с Noise-аутентификацией.
Worker слушает подписанные объявления в отдельной GossipSub-теме приложения,
запрашивает адрес PeerId через `https://delegated-ipfs.dev/routing/v1`, затем
использует DHT как fallback. Первый аутентифицированный канал побеждает.
Успешный libp2p-маршрут кешируется в IndexedDB на 24 часа. WebRTC-сервер
аутентифицируется channel-bound Ed25519 transcript, который покрывает challenge,
signaling session, логический порт и точные offer/answer SDP для введённого
PeerId. Указанный в UI таймаут является единым deadline для Worker/libp2p,
native WebRTC, поэтому медленный DHT-запрос не блокирует
форму дольше выбранного интервала. Файл `public/network-config.json` позволяет добавить другие совместимые
routing endpoint и скрытый пул WSS relay без изменения интерфейса:

```json
{
  "delegatedRouting": [
    "https://delegated-ipfs.dev/routing/v1"
  ],
  "relays": []
}
```

Файл `.npmrc` включает `install-links=true`: благодаря этому локальный пакет
копируется в `node_modules` при `npm ci`, и чистая сборка GitHub Actions не
зависит от заранее установленных пакетов в корне репозитория.

## PubSub discovery и WebRTC STUN

Worker и CLI используют одну тему:
`io.github.santaklouse.p2p-netcat.peer-discovery.v1`. Сервис
`@libp2p/pubsub-peer-discovery` периодически публикует публичный ключ узла и его
текущие multiaddr. GossipSub по умолчанию требует подпись сообщения; получатель
вычисляет PeerId из переданного публичного ключа и только после этого принимает
адреса в peer store. Это не делает объявление доверенным: финальный libp2p
handshake всё равно должен доказать запрошенный PeerId. В список кандидатов
веб-клиента попадают лишь доступные браузеру адреса WSS/WebTransport/WebRTC.

Сам PubSub не является bootstrap-механизмом. Объявление попадёт в браузер только
после подключения к совместимому подписчику той же темы. Обычные публичные IPFS
bootstrap-узлы не обязаны подписываться или передавать тему этого приложения.
Relay p2p-netcat по умолчанию участвует в теме, поэтому уже доступный relay
может также распространять discovery-сообщения.

Native-реализация использует полный non-trickle SDP с несколькими публичными
WebTorrent trackers и короткоживущие подписанные Nostr offer, answer и
trickle-ICE candidate events через несколько relay. Оба адаптера удаляют
дубликаты и автоматически переподключаются. Listener ограниченно и недолго
хранит Nostr candidates, пришедшие до offer. После аутентификации PeerId
управляющие `ping`/`pong` поддерживают неактивный data channel. Эти меры
повышают надёжность discovery и сеанса, но не превращают публичные trackers,
relays или STUN в инфраструктуру с гарантией доступности.

Браузерная и Go WebRTC-реализации используют один ICE/STUN-пул:

```text
stun:stun.l.google.com:19302
stun:stun1.l.google.com:19302
stun:stun2.l.google.com:19302
stun:stun3.l.google.com:19302
stun:stun4.l.google.com:19302
stun:stun.counterpath.com:3478
stun:stun.sipgate.net:3478
stun:stun.voipbuster.com:3478
stun:stun.internetcalls.com:3478
```

STUN-серверы определяют публичный NAT mapping и не переносят данные приложения.
Это сторонние сервисы, которые могут видеть исходный IP и время запросов. STUN
не ретранслирует трафик, поэтому не гарантирует WebRTC через symmetric NAT или
сети с заблокированным UDP. Для такого случая детерминированным fallback
остаётся Circuit Relay, если настроен достижимый relay.

## Установка и сборка

Требуется Node.js 22.13 или новее.

Пакет npm содержит полностью собранный каталог `dist` и не имеет runtime
npm-зависимостей. Чтобы скопировать его в каталог статического хостинга:

```bash
npm install p2p-netcat-web
mkdir -p public/p2p-netcat
cp -R node_modules/p2p-netcat-web/dist/. public/p2p-netcat/
```

В скопированном каталоге находятся только статические файлы. Их нужно отдавать
по HTTPS; запущенный Node.js-процесс или серверные скрипты не требуются.

Для сборки из исходного кода репозитория:

```bash
cd web
npm install
npm run lint
npm run build
```

Готовый автономный статический пакет находится в `web/dist`. Его можно
разместить на любом HTTPS-хостинге статических файлов: backend для приложения
не требуется.

Для разработки:

```bash
cd web
npm run dev
```

## GitHub Pages

В корне репозитория уже находится workflow `.github/workflows/pages.yml`. Он
проверяет TypeScript, собирает только каталог `web`, автоматически вычисляет
базовый путь из имени GitHub-репозитория и публикует содержимое `web/dist`.

После загрузки репозитория откройте **Settings → Pages** и выберите
**Build and deployment → Source → GitHub Actions**. После следующего push в
ветку `main` страница будет опубликована автоматически. Для репозитория
`santaklouse/p2p-netcat` ожидаемый адрес:

```text
https://santaklouse.github.io/go-p2p-netcat/
```

По умолчанию открывается английская версия. Полный русский интерфейс доступен
по постоянной ссылке в шапке страницы или напрямую:

```text
https://santaklouse.github.io/go-p2p-netcat/?lang=ru
```

GitHub Pages предоставляет HTTPS, поэтому Service Worker, установка PWA,
Delegated Routing, WebRTC и защищённые WSS/WebTransport-маршруты будут работать.

## Проверяемый ручной relay-маршрут

Поле relay в интерфейсе необязательно. Следующий пример нужен для локальной
проверки явного fallback, когда автоматический поиск не может использовать
TCP/QUIC-адрес CLI-сервера.

В первом терминале запустите relay с отдельным WebSocket-портом:

```bash
p2p-nc relay -4 -p 9090 --websocket-port 9091
```

Скопируйте напечатанный адрес, который содержит `/tcp/9091/ws/p2p/`. Во втором
терминале запустите сервер и передайте ему этот адрес через `--relay`:

```bash
p2p-nc -l 31337 --relay /ip4/127.0.0.1/tcp/9091/ws/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW
```

В веб-интерфейсе укажите PeerId и логический порт `31337`. Затем раскройте
«Дополнительный relay» и укажите тот же WebSocket relay multiaddr.

PeerId в команде выше относится к проверочному ключу разработчика. При обычном
запуске relay напечатает другой PeerId — всегда используйте фактически
напечатанный полный адрес.

## HTTPS, WSS и запуск без backend

Service Worker и установка PWA доступны только в secure context: на HTTPS или
на `localhost`. Поэтому открытие `dist/index.html` через `file://` не является
поддерживаемым режимом браузеров. Это ограничение безопасности браузера, а не
потребность приложения в серверной логике.

Адрес `http://*.github.io` автоматически заменяется на соответствующий
`https://` до запуска приложения. Это необходимо для Web Crypto, WebRTC и
Service Worker. Если другой статический хостинг отдаёт страницу по обычному
HTTP, сетевой Worker остановится с понятным сообщением о необходимости HTTPS.

Если используется ручной relay, при размещении страницы по HTTPS он должен быть
доступен по WSS. Обычно TLS завершается на статическом reverse proxy/CDN,
который проксирует WebSocket на порт `9091`, а публичный multiaddr имеет вид:

```text
/dns4/p2p.example.com/tcp/443/wss/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW
```

Веб-приложение само не принимает HTTP-запросы и не выполняет серверный код.
После первой загрузки оболочка интерфейса доступна офлайн; для P2P-сеанса,
разумеется, требуется сеть и хотя бы один доступный браузерный маршрут.

## Проверка

```bash
cd web
npm test
npm run lint
```
