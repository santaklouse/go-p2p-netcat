# Программный API Circuit Relay

[English version](RELAY_API.md)

Go-пакет `github.com/santaklouse/go-p2p-netcat/relay` запускает встраиваемый
сервер Circuit Relay v2. Жизненным циклом context и остановкой управляет
вызывающее приложение.

```go
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/santaklouse/go-p2p-netcat/relay"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	handle, err := relay.Start(ctx, relay.Options{
		IdentityPath:  "./data/p2p-netcat-relay.key",
		LocalPort:     9090,
		WebsocketPort: 9091,
		NoMDNS:        true,
	})
	if err != nil {
		panic(err)
	}
	defer handle.Stop()

	fmt.Println("Relay PeerId:", handle.PeerID())
	for _, address := range handle.Addresses() {
		fmt.Println("Relay address:", address)
	}
	<-ctx.Done()
}
```

`Options` принимает путь identity либо готовый libp2p private key, порт
TCP/QUIC, WebSocket-порт, ограничение IPv4/IPv6, объявляемые multiaddr и
переключатели mDNS/PubSub/QUIC. `DisableWebsocket` отключает WS listener.

Relay допускает не более 128 reservations; стандартный предел одной
reservation — два часа и 128 МиБ. При включённом PubSub он также участвует в
подписанной GossipSub mesh p2p-netcat с peer exchange.

`Handle.Stop` идемпотентен. `Handle.Node` открывает запущенный p2p-узел для
расширенной интеграции, а `PeerID` и `Addresses` возвращают публичную identity
и текущие dialable multiaddr.

Для браузера завершите TLS на reverse proxy и объявите `/wss` multiaddr.
Circuit Relay видит PeerId, время и объём трафика, но прикладной libp2p-поток
остаётся зашифрованным.
