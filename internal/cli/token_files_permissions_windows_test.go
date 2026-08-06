//go:build windows

package cli

import (
	"os"
	"testing"
)

func assertPrivateTokenFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("token output is not a regular file: %s", path)
	}
}
