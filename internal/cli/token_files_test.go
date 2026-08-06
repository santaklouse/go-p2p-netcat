package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santaklouse/go-p2p-netcat/internal/secretfile"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"github.com/santaklouse/go-p2p-netcat/protocol/tokenfile"
)

func TestEncryptedTokenCommandRoundTrip(t *testing.T) {
	directory := t.TempDir()
	passwordPath := filepath.Join(directory, "password")
	writeTestSecret(t, passwordPath, []byte("correct horse battery staple\n"))
	identityPath := filepath.Join(directory, "identity.key")
	encryptedPath := filepath.Join(directory, "pairing.token.enc")
	command := NewRoot()
	command.SetContext(context.Background())
	command.SetArgs([]string{
		"token", "31337",
		"--identity", identityPath,
		"--encrypt-to", encryptedPath,
		"--password-file", passwordPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	encrypted, err := os.ReadFile(encryptedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(encrypted), tokenfile.Prefix) {
		t.Fatalf("encrypted token = %q", encrypted)
	}
	assertPrivateTokenFile(t, encryptedPath)

	unlockedPath := filepath.Join(directory, "pairing.token")
	command = NewRoot()
	command.SetContext(context.Background())
	command.SetArgs([]string{
		"token", "unlock", encryptedPath,
		"--output", unlockedPath,
		"--password-file", passwordPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	unlocked, err := os.ReadFile(unlockedPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := pairing.Decode(string(unlocked))
	if err != nil {
		t.Fatal(err)
	}
	if token.Service != 31337 {
		t.Fatalf("unlocked token service = %d, want 31337", token.Service)
	}
	assertPrivateTokenFile(t, unlockedPath)

	command = NewRoot()
	command.SetContext(context.Background())
	command.SetArgs([]string{
		"token", "unlock", encryptedPath,
		"--output", unlockedPath,
		"--password-file", passwordPath,
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "file exists") {
		t.Fatalf("existing output error = %v", err)
	}
}

func TestEncryptedTokenWrongPasswordDoesNotCreateOutput(t *testing.T) {
	directory := t.TempDir()
	passwordPath := filepath.Join(directory, "password")
	wrongPasswordPath := filepath.Join(directory, "wrong-password")
	for path, password := range map[string]string{
		passwordPath:      "correct horse battery staple\n",
		wrongPasswordPath: "this password is incorrect\n",
	} {
		writeTestSecret(t, path, []byte(password))
	}
	encryptedPath := filepath.Join(directory, "pairing.token.enc")
	command := NewRoot()
	command.SetContext(context.Background())
	command.SetArgs([]string{
		"token", "31337",
		"--identity", filepath.Join(directory, "identity.key"),
		"--encrypt-to", encryptedPath,
		"--password-file", passwordPath,
	})
	if err := command.Execute(); err != nil {
		t.Fatal(err)
	}
	unlockedPath := filepath.Join(directory, "pairing.token")
	command = NewRoot()
	command.SetContext(context.Background())
	command.SetArgs([]string{
		"token", "unlock", encryptedPath,
		"--output", unlockedPath,
		"--password-file", wrongPasswordPath,
	})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "incorrect or data is corrupted") {
		t.Fatalf("wrong password error = %v", err)
	}
	if _, err := os.Stat(unlockedPath); !os.IsNotExist(err) {
		t.Fatalf("wrong password created output: %v", err)
	}
}

func TestReadPairingTokenFileRejectsNonRegularAndOversizedFiles(t *testing.T) {
	directory := t.TempDir()
	if _, err := readPairingTokenFile(directory); err == nil {
		t.Fatal("directory was accepted as a pairing token file")
	}
	path := filepath.Join(directory, "oversized.token")
	writeTestSecret(t, path, []byte(strings.Repeat("x", 64*1024)))
	if _, err := readPairingTokenFile(path); err == nil {
		t.Fatal("oversized pairing token file was accepted")
	}
}

func writeTestSecret(t *testing.T, path string, value []byte) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write(value); err != nil {
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
}
