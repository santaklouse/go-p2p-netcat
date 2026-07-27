package routerecord

import (
	"crypto/rand"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

func TestSignedRecord(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	address, _ := ma.NewMultiaddr("/ip4/203.0.113.7/udp/4001/quic-v1")
	envelope, err := Sign(key, Record{
		Version: Version, Sequence: 42, IssuedAt: 1_900_000_000, ExpiresAt: 1_900_000_180,
		Services: []uint16{31337, 8080}, Addresses: []ma.Multiaddr{address},
		Capabilities: CapabilityQUIC | CapabilityWebRTC | CapabilityRelay,
	})
	if err != nil {
		t.Fatal(err)
	}
	record, err := Verify(envelope, VerifyOptions{
		ExpectedPeerID: id, ExpectedService: 31337, Now: time.Unix(1_900_000_030, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Sequence != 42 || record.PeerID != id {
		t.Fatalf("unexpected record: %#v", record)
	}
	envelope[len(envelope)-1] ^= 1
	if _, err := Verify(envelope, VerifyOptions{Now: time.Unix(1_900_000_030, 0)}); err == nil {
		t.Fatal("tampered record was accepted")
	}
}
