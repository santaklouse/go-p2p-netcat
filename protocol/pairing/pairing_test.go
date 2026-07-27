package pairing

import (
	"encoding/hex"
	"testing"
	"time"
)

const vectorToken = "pnc1_pgABAXg0MTJEM0tvb1dRM3V4cEhnakRLRTZ2R212ektTOFJQYnhVREx3SjdYQ0xhRDZZWGRVZmJSOQIZemkDWCAAAQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHwSABRp3NZQA"

func TestInteroperabilityVector(t *testing.T) {
	token, err := Decode(vectorToken)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := token.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if encoded != vectorToken {
		t.Fatalf("token mismatch:\n%s\n%s", encoded, vectorToken)
	}
	expectedKeys := map[string]string{
		"rendezvous":   "e8d5fc0873810ff06039af654896909c86521e878d5970c3f8b3fed58df0385f",
		"signaling":    "f4c7b6f69d0024bdfec6c7c017843977f3adb728bb1f398b09e222031d19abeb",
		"admission":    "976b46fe450808bae0694e793fc9db6de10107ffa58b7b0cbfa8e86cb94a3b57",
		"route-record": "7fe47fb8573ec1e37980a3621d29cf2a7f8f600ab41663821a87280b15070fd4",
	}
	for purpose, expected := range expectedKeys {
		key, err := token.DeriveKey(purpose)
		if err != nil {
			t.Fatal(err)
		}
		if hex.EncodeToString(key[:]) != expected {
			t.Errorf("%s key: %x", purpose, key)
		}
	}
	id, err := token.RendezvousID("dht", 12345)
	if err != nil {
		t.Fatal(err)
	}
	if id != "9mtMRyxbxPkVQlj7WJW9oCXuVBlgtkxzj9z0F0H8gW0" {
		t.Fatalf("rendezvous: %s", id)
	}
	provider, err := ProviderCID(id)
	if err != nil {
		t.Fatal(err)
	}
	if provider.String() != "bafkreihzwnotx7weylzypbxqzsogwwec44rq6ujft6ewn4lh3jjdscylgi" {
		t.Fatalf("provider CID: %s", provider)
	}
	nonce, _ := hex.DecodeString("000102030405060708090a0b")
	sealed, err := token.Seal("signaling", []byte("hello"), []byte("vector-aad"), nonce)
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(sealed) != "a30001014c000102030405060708090a0b02556e5682c115e1b4ce0fe7930b863d097a7734a2b530" {
		t.Fatalf("envelope: %x", sealed)
	}
	opened, err := token.Open("signaling", sealed, []byte("vector-aad"))
	if err != nil || string(opened) != "hello" {
		t.Fatalf("open: %q, %v", opened, err)
	}
}

func TestExpiration(t *testing.T) {
	token, err := Decode(vectorToken)
	if err != nil {
		t.Fatal(err)
	}
	if err := token.Validate(time.Unix(2_000_000_001, 0)); err == nil {
		t.Fatal("expected expired token")
	}
}
