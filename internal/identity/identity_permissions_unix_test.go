//go:build unix

package identity

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateUsesPrivatePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.key")
	if _, err := LoadOrCreate(path); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("identity permissions = %o, want 600", info.Mode().Perm())
	}
}
