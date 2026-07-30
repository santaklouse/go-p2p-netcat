package p2p

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func TestPubSubPeerRecordRoundTrip(t *testing.T) {
	node := newPubSubTestNode(t, false)
	encoded, err := encodePubSubPeerRecord(node.Host)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodePubSubPeerRecord(encoded)
	if err != nil {
		t.Fatal(err)
	}
	peerID, err := peer.IDFromPublicKey(decoded.publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if peerID != node.Host.ID() {
		t.Fatalf("PeerId mismatch: %s != %s", peerID, node.Host.ID())
	}
	if len(decoded.addresses) == 0 {
		t.Fatal("peer record did not preserve listen addresses")
	}
}

func TestGossipSubDiscoversPeerThroughIntermediateNode(t *testing.T) {
	listener := newPubSubTestNode(t, true)
	middle := newPubSubTestNode(t, true)
	client := newPubSubTestNode(t, true)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	connectTestNodes(t, ctx, listener, middle)
	connectTestNodes(t, ctx, client, middle)

	const service = uint16(43210)
	listener.Host.SetStreamHandler(ProtocolForService(service), func(stream network.Stream) {
		defer stream.Close()
		request := make([]byte, 4)
		_, _ = io.ReadFull(stream, request)
		if bytes.Equal(request, []byte("ping")) {
			_, _ = stream.Write([]byte("pong"))
		}
	})

	var stream network.Stream
	var err error
	for ctx.Err() == nil {
		stream, err = client.OpenStream(ctx, listener.Host.ID().String(), service, nil, nil)
		if err == nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("peer was not discovered through GossipSub: %v", err)
	}
	defer stream.Close()
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(stream, response); err != nil {
		t.Fatal(err)
	}
	if string(response) != "pong" {
		t.Fatalf("unexpected response: %q", response)
	}
}

func TestPreferDialRanker(t *testing.T) {
	values := []string{
		"/ip4/127.0.0.1/tcp/4001/p2p-circuit",
		"/ip4/127.0.0.1/tcp/4001",
		"/ip4/127.0.0.1/udp/4001/quic-v1",
		"/ip4/127.0.0.1/udp/4001/webrtc-direct",
	}
	addresses := make([]ma.Multiaddr, 0, len(values))
	for _, value := range values {
		address, err := ma.NewMultiaddr(value)
		if err != nil {
			t.Fatal(err)
		}
		addresses = append(addresses, address)
	}
	ranked := PreferDialRanker(addresses)
	if len(ranked) != len(addresses) {
		t.Fatalf("ranked %d addresses, expected %d", len(ranked), len(addresses))
	}
	if dialAddressRank(ranked[0].Addr) != 0 ||
		dialAddressRank(ranked[len(ranked)-1].Addr) != 6 {
		t.Fatalf("unexpected rank order: %#v", ranked)
	}
	for index := 1; index < len(ranked); index++ {
		if ranked[index].Delay <= ranked[index-1].Delay {
			t.Fatalf("delay did not increase between transport classes: %#v", ranked)
		}
	}
}

func newPubSubTestNode(t *testing.T, discovery bool) *Node {
	t.Helper()
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node, err := New(context.Background(), Config{
		PrivateKey:     key,
		TransportPort:  0,
		IPVersion:      4,
		EnablePubSub:   true,
		PubSubDiscover: discovery,
		PubSubInterval: 100 * time.Millisecond,
		Listen:         true,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Close() })
	return node
}

func connectTestNodes(t *testing.T, ctx context.Context, from, to *Node) {
	t.Helper()
	if err := from.Host.Connect(ctx, peer.AddrInfo{ID: to.Host.ID(), Addrs: to.Host.Addrs()}); err != nil {
		t.Fatal(err)
	}
}
