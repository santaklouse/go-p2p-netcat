package p2p

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/santaklouse/go-p2p-netcat/protocol/datagram"
)

func TestApplicationProtocolsKeepStreamAndDatagramModesSeparate(t *testing.T) {
	const service = uint16(51820)
	if got := ProtocolForService(service); got != "/p2p-netcat/1.0.0/51820" {
		t.Fatalf("stream protocol = %q", got)
	}
	if got := DatagramProtocolForService(service); got != "/p2p-netcat/udp/1.0.0/51820" {
		t.Fatalf("datagram protocol = %q", got)
	}
	if ProtocolForService(service) == DatagramProtocolForService(service) {
		t.Fatal("stream and datagram protocols must not collide")
	}
}

func TestDatagramProtocolOverTCPTransport(t *testing.T) {
	listener := newPubSubTestNode(t, false)
	client := newPubSubTestNode(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connectTestNodes(t, ctx, client, listener)

	const service = uint16(51820)
	handlerErr := make(chan error, 1)
	listener.Host.SetStreamHandler(DatagramProtocolForService(service), func(stream network.Stream) {
		defer stream.Close()
		for index := 0; index < 2; index++ {
			payload, err := datagram.Read(stream)
			if err != nil {
				handlerErr <- err
				return
			}
			if err := datagram.Write(stream, append(
				[]byte(fmt.Sprintf("%d:", index)),
				payload...,
			)); err != nil {
				handlerErr <- err
				return
			}
		}
		handlerErr <- nil
	})

	stream, err := client.OpenDatagramStream(
		ctx,
		listener.Host.ID().String(),
		service,
		nil,
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if address := stream.Conn().RemoteMultiaddr().String(); !strings.Contains(address, "/tcp/") {
		t.Fatalf("datagram test route = %q, want a TCP transport", address)
	}
	for index, payload := range [][]byte{[]byte("handshake"), []byte("transport")} {
		if err := datagram.Write(stream, payload); err != nil {
			t.Fatal(err)
		}
		response, err := datagram.Read(stream)
		if err != nil {
			t.Fatal(err)
		}
		expected := fmt.Sprintf("%d:%s", index, payload)
		if string(response) != expected {
			t.Fatalf("response %d = %q, want %q", index, response, expected)
		}
	}
	if err := <-handlerErr; err != nil {
		t.Fatal(err)
	}
}
