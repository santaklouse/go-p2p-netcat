package cli

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/santaklouse/go-p2p-netcat/internal/secretfile"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"github.com/santaklouse/go-p2p-netcat/protocol/tokenfile"
	"golang.org/x/term"
)

func readPairingTokenFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open pairing token file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect pairing token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("pairing token file must be a regular file")
	}
	if err := secretfile.CheckPermissions(file, info); err != nil {
		return "", fmt.Errorf("pairing token file is not private: %w", err)
	}
	maximum := len(pairing.TokenPrefix) + base64.RawURLEncoding.EncodedLen(pairing.MaxTokenSize) + 2
	data, err := io.ReadAll(io.LimitReader(file, int64(maximum+1)))
	if err != nil {
		return "", fmt.Errorf("read pairing token file: %w", err)
	}
	if len(data) > maximum {
		return "", fmt.Errorf("pairing token file exceeds %d bytes", maximum)
	}
	return string(data), nil
}

func readTokenPassword(path string, confirm bool) ([]byte, error) {
	if path != "" {
		password, err := readPasswordFile(path)
		if err != nil {
			return nil, err
		}
		if err := validateTokenPasswordSize(password); err != nil {
			clear(password)
			return nil, err
		}
		return password, nil
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("password input requires a terminal; use --password-file for non-interactive operation")
	}
	password, err := readTerminalPassword(fd, "Token password: ")
	if err != nil {
		return nil, err
	}
	if err := validateTokenPasswordSize(password); err != nil {
		clear(password)
		return nil, err
	}
	if !confirm {
		return password, nil
	}
	repeated, err := readTerminalPassword(fd, "Confirm token password: ")
	if err != nil {
		clear(password)
		return nil, err
	}
	defer clear(repeated)
	if subtle.ConstantTimeCompare(password, repeated) != 1 {
		clear(password)
		return nil, errors.New("token passwords do not match")
	}
	return password, nil
}

func readTerminalPassword(fd int, prompt string) ([]byte, error) {
	if _, err := fmt.Fprint(os.Stderr, prompt); err != nil {
		return nil, err
	}
	password, err := term.ReadPassword(fd)
	_, newlineErr := fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, fmt.Errorf("read token password: %w", err)
	}
	if newlineErr != nil {
		clear(password)
		return nil, newlineErr
	}
	return password, nil
}

func readPasswordFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open password file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect password file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("password file must be a regular file")
	}
	if err := secretfile.CheckPermissions(file, info); err != nil {
		return nil, fmt.Errorf("password file is not private: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, tokenfile.MaximumPasswordSize+3))
	if err != nil {
		return nil, fmt.Errorf("read password file: %w", err)
	}
	data = bytes.TrimSuffix(data, []byte{'\n'})
	data = bytes.TrimSuffix(data, []byte{'\r'})
	if len(data) > tokenfile.MaximumPasswordSize {
		clear(data)
		return nil, fmt.Errorf("token password must contain at most %d bytes", tokenfile.MaximumPasswordSize)
	}
	return data, nil
}

func validateTokenPasswordSize(password []byte) error {
	if len(password) < tokenfile.MinimumPasswordSize {
		return fmt.Errorf("token password must contain at least %d bytes", tokenfile.MinimumPasswordSize)
	}
	if len(password) > tokenfile.MaximumPasswordSize {
		return fmt.Errorf("token password must contain at most %d bytes", tokenfile.MaximumPasswordSize)
	}
	return nil
}

func readEncryptedTokenFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open encrypted token file: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("inspect encrypted token file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("encrypted token file must be a regular file")
	}
	if err := secretfile.CheckPermissions(file, info); err != nil {
		return "", fmt.Errorf("encrypted token file is not private: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(file, tokenfile.MaxEnvelopeSize*2+1))
	if err != nil {
		return "", fmt.Errorf("read encrypted token file: %w", err)
	}
	if len(data) > tokenfile.MaxEnvelopeSize*2 {
		return "", errors.New("encrypted token file is too large")
	}
	return string(data), nil
}

func writeExclusiveTokenFile(path, value string) (returnErr error) {
	if path == "" {
		return errors.New("token output path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve token output path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return fmt.Errorf("create token output directory: %w", err)
	}
	file, err := os.OpenFile(absolute, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create token output file: %w", err)
	}
	removeOnFailure := true
	defer func() {
		if removeOnFailure {
			_ = os.Remove(absolute)
		}
	}()
	if _, err := io.WriteString(file, value+"\n"); err != nil {
		_ = file.Close()
		return fmt.Errorf("write token output file: %w", err)
	}
	if err := secretfile.Protect(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect token output file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close token output file: %w", err)
	}
	removeOnFailure = false
	return nil
}
