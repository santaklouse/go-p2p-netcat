package tokenfile

import (
	"strings"
	"testing"
)

const testToken = "pnc1_pgABAXg0MTJEM0tvb1dRM3V4cEhnakRLRTZ2R212ektTOFJQYnhVREx3SjdYQ0xhRDZZWGRVZmJSOQIZemkDWCAAAQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHwSABRp3NZQA"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	password := []byte("correct horse battery staple")
	encrypted, err := Encrypt(testToken, password)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(encrypted, Prefix) || strings.Contains(encrypted, testToken) {
		t.Fatalf("unexpected encrypted token: %q", encrypted)
	}
	decrypted, err := Decrypt(encrypted, password)
	if err != nil {
		t.Fatal(err)
	}
	if decrypted != testToken {
		t.Fatalf("decrypted token mismatch:\n%s\n%s", decrypted, testToken)
	}
	if _, err := Decrypt(encrypted, []byte("incorrect password")); err == nil ||
		!strings.Contains(err.Error(), "incorrect or data is corrupted") {
		t.Fatalf("wrong password error = %v", err)
	}
}

func TestEncryptRejectsInvalidInputs(t *testing.T) {
	if _, err := Encrypt("not-a-token", []byte("correct horse battery staple")); err == nil {
		t.Fatal("invalid pairing token was accepted")
	}
	if _, err := Encrypt(testToken, []byte("short")); err == nil {
		t.Fatal("short password was accepted")
	}
	if _, err := Decrypt("pnc1e_invalid!", []byte("correct horse battery staple")); err == nil {
		t.Fatal("invalid encrypted token was accepted")
	}
}
