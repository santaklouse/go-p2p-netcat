//go:build unix

package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func assertPrivateTokenFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o600 {
		t.Fatalf("token permissions = %o, want 600", permissions)
	}
}

func TestReadPairingTokenFileRejectsInsecurePermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pairing.token")
	if err := os.WriteFile(path, []byte("pnc1_test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPairingTokenFile(path); err == nil {
		t.Fatal("pairing token readable by group or others was accepted")
	}
}
