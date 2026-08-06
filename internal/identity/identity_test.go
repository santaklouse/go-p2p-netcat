package identity

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/santaklouse/go-p2p-netcat/internal/secretfile"
)

func TestLoadOrCreatePersistsIdentity(t *testing.T) {
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
}

func TestLoadOrCreateRejectsMalformedIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.key")
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("not a libp2p key")); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := secretfile.Protect(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("expected malformed identity error")
	}
}
