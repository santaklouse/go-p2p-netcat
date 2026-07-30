# Programmatic Circuit Relay API

[Русская версия](RELAY_API.RU.md)

The Go package `github.com/santaklouse/go-p2p-netcat/relay` starts an
embeddable Circuit Relay v2 server. The caller owns its context and shutdown.

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

`Options` supports an identity path or injected libp2p private key, TCP/QUIC
port, WebSocket port, IPv4/IPv6 restriction, announced multiaddrs, and
mDNS/PubSub/QUIC switches. `DisableWebsocket` disables the WS listener.

The relay applies a maximum of 128 reservations and default per-reservation
limits of two hours and 128 MiB. With PubSub enabled it also participates in
the signed p2p-netcat GossipSub mesh with peer exchange.

`Handle.Stop` is idempotent. `Handle.Node` exposes the started p2p node for
advanced integrations; `PeerID` and `Addresses` return its public identity and
current dialable multiaddrs.

For browser use, terminate TLS at a reverse proxy and announce a `/wss`
multiaddr. Circuit Relay sees PeerIds, timing, and traffic volume, while the
relayed libp2p application stream remains encrypted.
