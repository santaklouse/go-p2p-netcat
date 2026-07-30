package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

func TestLoadOrCreatePersistsIdentityWithPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "identity.key")
	first, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	firstBytes, err := crypto.MarshalPrivateKey(first)
	if err != nil {
		t.Fatal(err)
	}
	secondBytes, err := crypto.MarshalPrivateKey(second)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatal("LoadOrCreate returned a different key")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity permissions = %o, want 600", info.Mode().Perm())
	}
}

func TestLoadOrCreateRejectsMalformedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.key")
	if err := os.WriteFile(path, []byte("not a libp2p key"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("expected malformed identity error")
	}
}
