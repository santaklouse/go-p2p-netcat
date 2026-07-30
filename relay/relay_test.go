package relay

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func TestStartAndStopAreEmbeddableAndIdempotent(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := Start(context.Background(), Options{
		PrivateKey:       key,
		LocalPort:        0,
		DisableWebsocket: true,
		IPVersion:        4,
		NoMDNS:           true,
		NoPubSub:         true,
		NoQUIC:           true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.PeerID() == "" || len(handle.Addresses()) == 0 {
		t.Fatalf("relay did not expose its identity and addresses: %#v", handle)
	}
	if err := handle.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := handle.Stop(); err != nil {
		t.Fatal(err)
	}
}
