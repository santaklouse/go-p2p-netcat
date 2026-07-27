package admission

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const vectorToken = "pnc1_pgABAXg0MTJEM0tvb1dRM3V4cEhnakRLRTZ2R212ektTOFJQYnhVREx3SjdYQ0xhRDZZWGRVZmJSOQIZemkDWCAAAQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHwSABRp3NZQA"

func TestAdmissionVectors(t *testing.T) {
	token, err := pairing.Decode(vectorToken)
	if err != nil {
		t.Fatal(err)
	}
	clientNonce, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")
	helloFrame, hello, err := CreateHello(token, time.Unix(1_700_000_000, 0), clientNonce)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(helloFrame); got != "504e43410101000000006553f100000102030405060708090a0b0c0d0e0ffa6363937e457a4bad2b60a5d0ab571b842cd30db93d77d613aca8a0208b5e23" {
		t.Fatalf("client hello: %s", got)
	}
	serverNonce, _ := hex.DecodeString("f0f1f2f3f4f5f6f7f8f9fafbfcfdfeff")
	ack, err := CreateAck(token, hello, serverNonce)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(ack); got != "504e43410102000000006553f100f0f1f2f3f4f5f6f7f8f9fafbfcfdfeffc0c3fb8250fd6fae4e1e58520ed5048f7da7dec31f0bf355c1d0c4e8c3910b61" {
		t.Fatalf("server ack: %s", got)
	}
	if _, err := VerifyHello(token, helloFrame, time.Unix(1_700_000_001, 0), MaxClockSkew); err != nil {
		t.Fatal(err)
	}
	if err := VerifyAck(token, hello, ack); err != nil {
		t.Fatal(err)
	}
}
