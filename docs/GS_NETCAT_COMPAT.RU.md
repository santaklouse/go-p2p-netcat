# Режимы CLI, совместимые с gs-netcat

[English](GS_NETCAT_COMPAT.md) | **Русский**

Здесь описана реализация опций gs-netcat `-d`, `-p`, `-q`, `-S`, `-T` и `-i`
в p2p-netcat. Значения опций повторяют gs-netcat, но rendezvous, аутентификация
и потоки по-прежнему основаны на libp2p PeerId.

## Соответствие опций

| Опция | Поведение p2p-netcat |
|---|---|
| `-d, --destination HOST` | Адрес назначения TCP forwarding на стороне сервера. Требует listener mode и `-p`. |
| `-p, --port PORT` | В listener mode — порт удалённого назначения. В client mode — локальный TCP listen-порт, где каждое соединение переносится в новый P2P-поток. |
| `-q, --quiet` | Скрывает диагностику p2p-netcat в stderr. Прикладные данные stdout не изменяются. |
| `-S, --socks` | Запускает SOCKS4, SOCKS4a и SOCKS5 CONNECT proxy на стороне listener. |
| `-T, --tor` | Запускает клиент через `torsocks` и требует явный TCP/WS/WSS Circuit Relay. |
| `-i, --interactive` | Listener создаёт настоящий PTY login shell для каждого клиента; клиент включает raw TTY и передаёт resize и управляющие символы. |

Прежнее техническое значение `-p` перенесено на `--transport-port`. Короткая
опция файла идентичности перенесена с `-i` на `-I`; длинная `--identity` не
изменилась. Задержка EOF доступна как `--quit-delay`, уже без старого alias
`-q`.

## TCP forwarding: `-d` и `-p`

Listener соединяет каждый принятый P2P-поток с настроенной TCP-целью:

```bash
p2p-nc -l -d 192.168.6.7 -p 22 31337
```

Если при listener `-p` не передан `-d`, адресом назначения становится
`127.0.0.1`. Последнее позиционное значение по-прежнему является логическим
портом p2p-netcat, а не TCP-портом.

Клиент открывает локальный loopback listener. Каждое локальное TCP-соединение
создаёт отдельный Yamux/libp2p-поток к тому же PeerId и логическому порту:

```bash
p2p-nc -p 2222 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
ssh -p 2222 root@127.0.0.1
```

По умолчанию локальный listener привязан к `127.0.0.1`. Публикация на другом
интерфейсе должна быть явной:

```bash
p2p-nc --bind 0.0.0.0 -p 2222 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Многосеансовый `-p` использует мультиплексированные libp2p-потоки, а не текущий
однопоточный WebRTC-адаптер. Если прямой libp2p-маршрут недоступен, передайте
один Circuit Relay серверу forwarding и клиенту.

## SOCKS proxy: `-S`

Запустите удалённый SOCKS endpoint:

```bash
p2p-nc -l -S 31337
```

Опубликуйте его локально через обычный клиентский forwarding:

```bash
p2p-nc -p 1080 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
curl --proxy socks5h://127.0.0.1:1080 https://example.com/
```

Поддерживаются SOCKS4 CONNECT, SOCKS4a CONNECT и SOCKS5 CONNECT без
аутентификации. SOCKS BIND, UDP ASSOCIATE, username/password authentication и
UDP forwarding не реализованы.

## Интерактивный PTY: `-i`

Запустите login shell на listener:

```bash
p2p-nc -l -i 31337
```

Подключитесь из настоящего терминала:

```bash
p2p-nc -i 12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Сервер через `node-pty` создаёт псевдотерминал и запускает `$SHELL -l` на Unix
или PowerShell на Windows. Клиент переводит stdin в raw mode, поэтому `Ctrl-C`
и другие терминальные байты попадают в удалённый PTY, а не обрабатываются
локальным процессом p2p-netcat. Изменения размера окна передаются отдельными
PTY control frames. Введите `exit` для завершения shell либо нажмите `Ctrl-e q`
для закрытия клиентского потока.

К этому listener также может подключиться статический веб-клиент. До соединения
включите **«Интерактивный PTY -i»**: xterm-виджет использует общий PTY codec,
передаёт клавиатуру и resize и поддерживает `Ctrl-e q`. Для listener без `-i`
переключатель должен оставаться выключенным, поскольку обычный и интерактивный
режимы используют разное wire-кодирование.

Для `-i` нужен TTY. Опция несовместима с `-e`, `-S` и клиентским `-p`. Listener
PTY остаётся активным и может одновременно обслуживать несколько клиентов.
Консоль команд gs-netcat по `Ctrl-e c` и её команды передачи файлов `get`/`put`
пока не реализованы. Сейчас p2p-netcat поддерживает raw PTY, resize, `Ctrl-e q`
и обычную передачу потока/файла вне PTY mode.

## Quiet mode: `-q`

`-q` скрывает статус, предупреждения, discovery-диагностику, ошибки сеансов и
финальную ошибку CLI в stderr. Опция не удаляет байты, полученные от пира или из
TCP forwarding. Не сочетайте `-q` с `-v`, если нужна диагностика: quiet имеет
приоритет.

## Маршрутизация через Tor: `-T`

Tor переносит только TCP. Чтобы исключить незаметный обход Tor, `-T`:

1. разрешён только в client mode;
2. требует явный `--relay` multiaddr с TCP, WS или WSS;
3. отклоняет UDP/QUIC relay-адреса;
4. отключает QUIC, native/legacy WebRTC, STUN, mDNS, PubSub, bootstrap и DHT;
5. повторно запускает весь клиент через `torsocks -i`, получая отдельный Tor
   circuit.

Пример:

```bash
P2P_NETCAT_TOR_HOST=127.0.0.1 \
P2P_NETCAT_TOR_PORT=9050 \
p2p-nc -T \
  --relay /ip4/203.0.113.10/tcp/9090/p2p/12D3KooWEqeQRAJ61HSv9yMPk8yzjke7NxmTFcvFt4GzwXxzVjXW \
  12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9 31337
```

Переменные конфигурации:

| Переменная | По умолчанию | Назначение |
|---|---:|---|
| `P2P_NETCAT_TOR_HOST` | `127.0.0.1` | Числовой IP-адрес Tor SOCKS service. |
| `P2P_NETCAT_TOR_PORT` | `9050` | Порт Tor SOCKS. |
| `P2P_NETCAT_TORSOCKS_COMMAND` | `torsocks` | Другой исполняемый wrapper. |
| `GSOCKET_SOCKS_IP` | — | Совместимое с gs-netcat резервное имя адреса. |
| `GSOCKET_SOCKS_PORT` | — | Совместимое с gs-netcat резервное имя порта. |

Tor mode сейчас поддерживается на Linux и macOS при наличии `torsocks`.
TCP-соединение клиент → relay проходит через Tor, после чего relay переносит к
серверу сквозным образом зашифрованный libp2p circuit. Это не делает поведение
приложения анонимным: relay видит PeerId, время и объём трафика, а цель `-d` или
`-S` видит сетевую идентичность сервера.

## Граница безопасности

Noise, QUIC TLS и подписанный challenge интерактивного WebRTC аутентифицируют
PeerId сервера и защищают содержимое потока. Они не авторизуют клиента. В
текущей версии любой пир, знающий PeerId listener и логический порт, может
попытаться использовать `-d`, `-S`, `-e` или `-i`.

Поэтому:

- не публикуйте PeerId и service привилегированного режима без необходимости;
- оставляйте клиентский `-p` на loopback, если LAN-доступ не нужен;
- запускайте PTY и команды от непривилегированного изолированного пользователя;
- ограничивайте доступ к целям системным firewall;
- не считайте `-q` скрытностью, аутентификацией или маскировкой трафика.

Allowlist PeerId и прикладной слой авторизации запланированы, но пока не входят
в реализацию.
