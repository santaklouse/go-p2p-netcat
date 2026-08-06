// Package tokenfile encrypts pairing tokens for storage and transfer.
// Its pnc1e_ envelope is not a network wire format; decrypting it yields the
// unchanged pnc1_ bearer token used by Go and browser peers.
package tokenfile

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/fxamacker/cbor/v2"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"golang.org/x/crypto/argon2"
)

const (
	Prefix              = "pnc1e_"
	Version             = uint64(1)
	MinimumPasswordSize = 8
	MaximumPasswordSize = 1024
	MaxEnvelopeSize     = 64 * 1024

	argonTime    = uint64(3)
	argonMemory  = uint64(64 * 1024)
	argonThreads = uint64(4)
	saltSize     = 16
	keySize      = 32
	nonceSize    = 12
)

const (
	keyVersion uint64 = iota
	keyKDF
	keyKDFTime
	keyKDFMemory
	keyKDFThreads
	keySalt
	keyCipher
	keyNonce
	keyCiphertext
)

const (
	kdfName    = "argon2id"
	cipherName = "aes-256-gcm"
)

var (
	canonical cbor.EncMode
	strict    cbor.DecMode
)

var additionalData = []byte("p2p-netcat/encrypted-pairing-token/v1\x00argon2id\x00aes-256-gcm")

func init() {
	var err error
	canonical, err = cbor.CanonicalEncOptions().EncMode()
	if err != nil {
		panic(err)
	}
	strict, err = cbor.DecOptions{
		DupMapKey:         cbor.DupMapKeyEnforcedAPF,
		IndefLength:       cbor.IndefLengthForbidden,
		TagsMd:            cbor.TagsForbidden,
		IntDec:            cbor.IntDecConvertSignedOrFail,
		MaxNestedLevels:   8,
		MaxArrayElements:  16,
		MaxMapPairs:       16,
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,
	}.DecMode()
	if err != nil {
		panic(err)
	}
}

// Encrypt wraps one valid pnc1_ token using a password-derived AES-256-GCM key.
func Encrypt(tokenText string, password []byte) (string, error) {
	tokenText = strings.TrimSpace(tokenText)
	if _, err := pairing.Decode(tokenText); err != nil {
		return "", fmt.Errorf("encrypt pairing token: %w", err)
	}
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, saltSize)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("create encrypted token salt: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("create encrypted token nonce: %w", err)
	}
	key := deriveKey(password, salt)
	defer clear(key)
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	ciphertext := aead.Seal(nil, nonce, []byte(tokenText), additionalData)
	payload, err := canonical.Marshal(map[uint64]any{
		keyVersion:    Version,
		keyKDF:        kdfName,
		keyKDFTime:    argonTime,
		keyKDFMemory:  argonMemory,
		keyKDFThreads: argonThreads,
		keySalt:       salt,
		keyCipher:     cipherName,
		keyNonce:      nonce,
		keyCiphertext: ciphertext,
	})
	if err != nil {
		return "", fmt.Errorf("encode encrypted pairing token: %w", err)
	}
	return Prefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decrypt opens a pnc1e_ envelope and returns its unchanged pnc1_ token.
func Decrypt(encryptedText string, password []byte) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	payload, fields, err := decodeEnvelope(encryptedText)
	if err != nil {
		return "", err
	}
	reencoded, err := canonical.Marshal(fields)
	if err != nil || !bytes.Equal(payload, reencoded) {
		return "", errors.New("encrypted pairing token is not deterministic RFC 8949 CBOR")
	}
	if err := validateEnvelopeFields(fields); err != nil {
		return "", err
	}
	salt := fields[keySalt].([]byte)
	nonce := fields[keyNonce].([]byte)
	ciphertext := fields[keyCiphertext].([]byte)
	key := deriveKey(password, salt)
	defer clear(key)
	aead, err := newAEAD(key)
	if err != nil {
		return "", err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return "", errors.New("decrypt encrypted pairing token: password is incorrect or data is corrupted")
	}
	tokenText := string(plaintext)
	clear(plaintext)
	if _, err := pairing.Decode(tokenText); err != nil {
		return "", fmt.Errorf("decrypted pairing token is invalid: %w", err)
	}
	return tokenText, nil
}

func decodeEnvelope(text string) ([]byte, map[uint64]any, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, Prefix) {
		return nil, nil, fmt.Errorf("encrypted pairing token must start with %s", Prefix)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(text, Prefix))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid encrypted pairing token base64url data: %w", err)
	}
	if len(payload) == 0 || len(payload) > MaxEnvelopeSize {
		return nil, nil, fmt.Errorf("encrypted pairing token must contain 1..%d bytes", MaxEnvelopeSize)
	}
	var fields map[uint64]any
	if err := strict.Unmarshal(payload, &fields); err != nil {
		return nil, nil, fmt.Errorf("invalid encrypted pairing token CBOR: %w", err)
	}
	return payload, fields, nil
}

func validateEnvelopeFields(fields map[uint64]any) error {
	if len(fields) != int(keyCiphertext+1) {
		return errors.New("encrypted pairing token must contain exactly nine fields")
	}
	for key := range fields {
		if key > keyCiphertext {
			return fmt.Errorf("unknown encrypted pairing token field: %d", key)
		}
	}
	if value, ok := unsignedInteger(fields[keyVersion]); !ok || value != Version {
		return errors.New("unsupported encrypted pairing token version")
	}
	if value, ok := fields[keyKDF].(string); !ok || value != kdfName {
		return errors.New("unsupported encrypted pairing token KDF")
	}
	if value, ok := unsignedInteger(fields[keyKDFTime]); !ok || value != argonTime {
		return errors.New("unsupported encrypted pairing token Argon2 time cost")
	}
	if value, ok := unsignedInteger(fields[keyKDFMemory]); !ok || value != argonMemory {
		return errors.New("unsupported encrypted pairing token Argon2 memory cost")
	}
	if value, ok := unsignedInteger(fields[keyKDFThreads]); !ok || value != argonThreads {
		return errors.New("unsupported encrypted pairing token Argon2 parallelism")
	}
	if value, ok := fields[keySalt].([]byte); !ok || len(value) != saltSize {
		return fmt.Errorf("encrypted pairing token salt must contain exactly %d bytes", saltSize)
	}
	if value, ok := fields[keyCipher].(string); !ok || value != cipherName {
		return errors.New("unsupported encrypted pairing token cipher")
	}
	if value, ok := fields[keyNonce].([]byte); !ok || len(value) != nonceSize {
		return fmt.Errorf("encrypted pairing token nonce must contain exactly %d bytes", nonceSize)
	}
	if value, ok := fields[keyCiphertext].([]byte); !ok || len(value) < aes.BlockSize {
		return errors.New("encrypted pairing token ciphertext is too short")
	}
	return nil
}

func unsignedInteger(value any) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	}
	return 0, false
}

func validatePassword(password []byte) error {
	if len(password) < MinimumPasswordSize {
		return fmt.Errorf("token password must contain at least %d bytes", MinimumPasswordSize)
	}
	if len(password) > MaximumPasswordSize {
		return fmt.Errorf("token password must contain at most %d bytes", MaximumPasswordSize)
	}
	return nil
}

func deriveKey(password, salt []byte) []byte {
	return argon2.IDKey(
		password,
		salt,
		uint32(argonTime),
		uint32(argonMemory),
		uint8(argonThreads),
		keySize,
	)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create encrypted token cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create encrypted token AEAD: %w", err)
	}
	return aead, nil
}
