// Package pairing implements the language-neutral p2p-netcat pairing protocol.
package pairing

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/multiformats/go-multihash"
	"golang.org/x/crypto/hkdf"
)

const (
	TokenPrefix        = "pnc1_"
	TokenVersion       = uint64(1)
	SecretSize         = 32
	MaxTokenSize       = 16 * 1024
	MaxRelayHints      = 16
	RendezvousInterval = 300 * time.Second
)

var (
	canonical cbor.EncMode
	strict    cbor.DecMode
)

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
		MaxNestedLevels:   16,
		MaxArrayElements:  64,
		MaxMapPairs:       32,
		ExtraReturnErrors: cbor.ExtraDecErrorUnknownField,
	}.DecMode()
	if err != nil {
		panic(err)
	}
}

// Token is a bearer credential shared by the listener and its clients.
type Token struct {
	Version    uint64
	PeerID     peer.ID
	Service    uint16
	Secret     [SecretSize]byte
	RelayHints []ma.Multiaddr
	ExpiresAt  *uint64
}

func New(peerID peer.ID, service uint16, relayHints []ma.Multiaddr, expiresAt *uint64) (*Token, error) {
	if service == 0 {
		return nil, errors.New("logical port must be between 1 and 65535")
	}
	token := &Token{
		Version:    TokenVersion,
		PeerID:     peerID,
		Service:    service,
		RelayHints: append([]ma.Multiaddr(nil), relayHints...),
		ExpiresAt:  expiresAt,
	}
	if _, err := rand.Read(token.Secret[:]); err != nil {
		return nil, fmt.Errorf("create pairing secret: %w", err)
	}
	if err := token.Validate(time.Now()); err != nil {
		return nil, err
	}
	return token, nil
}

func (t *Token) Validate(now time.Time) error {
	if t == nil {
		return errors.New("pairing token is required")
	}
	if t.Version != TokenVersion {
		return fmt.Errorf("unsupported pairing token version: %d", t.Version)
	}
	if _, err := peer.Decode(t.PeerID.String()); err != nil {
		return fmt.Errorf("invalid PeerId in pairing token: %w", err)
	}
	if t.Service == 0 {
		return errors.New("pairing token logical port must be between 1 and 65535")
	}
	if len(t.RelayHints) > MaxRelayHints {
		return fmt.Errorf("pairing token supports at most %d relay hints", MaxRelayHints)
	}
	seen := make(map[string]struct{}, len(t.RelayHints))
	for _, hint := range t.RelayHints {
		if hint == nil || len(hint.String()) > 2048 {
			return errors.New("invalid relay hint in pairing token")
		}
		info, err := peer.AddrInfoFromP2pAddr(hint)
		if err != nil || info.ID == "" {
			return fmt.Errorf("relay hint must contain /p2p/PeerId: %s", hint)
		}
		if _, ok := seen[hint.String()]; ok {
			return fmt.Errorf("relay hints must be unique: %s", hint)
		}
		seen[hint.String()] = struct{}{}
	}
	if t.ExpiresAt != nil {
		if *t.ExpiresAt == 0 {
			return errors.New("pairing token expiration must be a positive Unix timestamp")
		}
		if !now.IsZero() && uint64(now.Unix()) > *t.ExpiresAt {
			return fmt.Errorf("pairing token expired at Unix time %d", *t.ExpiresAt)
		}
	}
	return nil
}

func (t *Token) Encode() (string, error) {
	if err := t.Validate(time.Time{}); err != nil {
		return "", err
	}
	hints := make([]string, 0, len(t.RelayHints))
	for _, hint := range t.RelayHints {
		hints = append(hints, hint.String())
	}
	sort.Strings(hints)
	fields := map[uint64]any{
		0: t.Version,
		1: t.PeerID.String(),
		2: uint64(t.Service),
		3: t.Secret[:],
		4: hints,
	}
	if t.ExpiresAt != nil {
		fields[5] = *t.ExpiresAt
	}
	payload, err := canonical.Marshal(fields)
	if err != nil {
		return "", fmt.Errorf("encode pairing token: %w", err)
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(payload), nil
}

func Decode(text string) (*Token, error) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, TokenPrefix) {
		return nil, fmt.Errorf("pairing token must start with %s", TokenPrefix)
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(text, TokenPrefix))
	if err != nil {
		return nil, fmt.Errorf("invalid pairing token base64url data: %w", err)
	}
	if len(payload) == 0 || len(payload) > MaxTokenSize {
		return nil, fmt.Errorf("pairing token must contain 1..%d bytes", MaxTokenSize)
	}
	var raw map[uint64]cbor.RawMessage
	if err := strict.Unmarshal(payload, &raw); err != nil {
		return nil, fmt.Errorf("invalid deterministic CBOR pairing token: %w", err)
	}
	for key := range raw {
		if key > 5 {
			return nil, fmt.Errorf("unknown pairing token field: %d", key)
		}
	}
	for _, key := range []uint64{0, 1, 2, 3, 4} {
		if _, ok := raw[key]; !ok {
			return nil, fmt.Errorf("missing pairing token field: %d", key)
		}
	}
	var version, service uint64
	var peerText string
	var secret []byte
	var hints []string
	if err := strict.Unmarshal(raw[0], &version); err != nil {
		return nil, fmt.Errorf("pairing token version: %w", err)
	}
	if err := strict.Unmarshal(raw[1], &peerText); err != nil {
		return nil, fmt.Errorf("PeerId pairing token: %w", err)
	}
	if err := strict.Unmarshal(raw[2], &service); err != nil || service == 0 || service > 65535 {
		return nil, errors.New("pairing token logical port must be between 1 and 65535")
	}
	if err := strict.Unmarshal(raw[3], &secret); err != nil || len(secret) != SecretSize {
		return nil, fmt.Errorf("pairing secret must contain exactly %d bytes", SecretSize)
	}
	if err := strict.Unmarshal(raw[4], &hints); err != nil {
		return nil, fmt.Errorf("relay hints pairing token: %w", err)
	}
	if len(hints) > MaxRelayHints {
		return nil, fmt.Errorf("pairing token supports at most %d relay hints", MaxRelayHints)
	}
	peerID, err := peer.Decode(peerText)
	if err != nil {
		return nil, fmt.Errorf("invalid PeerId in pairing token: %w", err)
	}
	token := &Token{Version: version, PeerID: peerID, Service: uint16(service)}
	copy(token.Secret[:], secret)
	for _, value := range hints {
		hint, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, fmt.Errorf("invalid relay hint %q: %w", value, err)
		}
		token.RelayHints = append(token.RelayHints, hint)
	}
	if value, ok := raw[5]; ok {
		var expires uint64
		if err := strict.Unmarshal(value, &expires); err != nil {
			return nil, fmt.Errorf("expiration pairing token: %w", err)
		}
		token.ExpiresAt = &expires
	}
	if err := token.Validate(time.Time{}); err != nil {
		return nil, err
	}
	reencoded, err := token.Encode()
	if err != nil {
		return nil, err
	}
	expected := TokenPrefix + base64.RawURLEncoding.EncodeToString(payload)
	if reencoded != expected {
		return nil, errors.New("CBOR pairing token is not deterministic RFC 8949 CBOR")
	}
	return token, nil
}

func (t *Token) DeriveKey(purpose string) ([32]byte, error) {
	var result [32]byte
	switch purpose {
	case "rendezvous", "signaling", "admission", "route-record":
	default:
		return result, fmt.Errorf("unsupported pairing key purpose: %s", purpose)
	}
	reader := hkdf.New(sha256.New, t.Secret[:], []byte("p2p-netcat/pairing/v1"), []byte("p2p-netcat/"+purpose+"/v1"))
	if _, err := io.ReadFull(reader, result[:]); err != nil {
		return result, err
	}
	return result, nil
}

func (t *Token) RendezvousID(purpose string, epoch uint64) (string, error) {
	switch purpose {
	case "dht", "pubsub", "signaling":
	default:
		return "", fmt.Errorf("unsupported rendezvous purpose: %s", purpose)
	}
	key, err := t.DeriveKey("rendezvous")
	if err != nil {
		return "", err
	}
	message := fmt.Sprintf("p2p-netcat/rendezvous/v1\x00%s\x00%s\x00%d\x00%d", purpose, t.PeerID, t.Service, epoch)
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write([]byte(message))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func ProviderCID(rendezvousID string) (cid.Cid, error) {
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(rendezvousID)
	if err != nil || len(decoded) != 32 || base64.RawURLEncoding.EncodeToString(decoded) != rendezvousID {
		return cid.Undef, errors.New("invalid rendezvous identifier")
	}
	digest, err := multihash.Sum([]byte("p2p-netcat:rendezvous:v1:"+rendezvousID), multihash.SHA2_256, -1)
	if err != nil {
		return cid.Undef, err
	}
	return cid.NewCidV1(cid.Raw, digest), nil
}

func (t *Token) ProviderCIDs(now time.Time) ([]cid.Cid, error) {
	epoch := now.Unix() / int64(RendezvousInterval/time.Second)
	result := make([]cid.Cid, 0, 3)
	for _, offset := range []int64{-1, 0, 1} {
		if epoch+offset < 0 {
			continue
		}
		id, err := t.RendezvousID("dht", uint64(epoch+offset))
		if err != nil {
			return nil, err
		}
		value, err := ProviderCID(id)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

func (t *Token) Seal(purpose string, plaintext, additionalData, nonce []byte) ([]byte, error) {
	key, err := t.DeriveKey(purpose)
	if err != nil {
		return nil, err
	}
	if nonce == nil {
		nonce = make([]byte, 12)
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
	}
	if len(nonce) != 12 {
		return nil, errors.New("AES-GCM nonce must contain exactly 12 bytes")
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ciphertext := aead.Seal(nil, nonce, plaintext, additionalData)
	return canonical.Marshal(map[uint64]any{0: uint64(1), 1: nonce, 2: ciphertext})
}

func (t *Token) Open(purpose string, envelope, additionalData []byte) ([]byte, error) {
	var fields map[uint64]cbor.RawMessage
	if err := strict.Unmarshal(envelope, &fields); err != nil {
		return nil, fmt.Errorf("invalid AES-GCM envelope: %w", err)
	}
	if len(fields) != 3 || fields[0] == nil || fields[1] == nil || fields[2] == nil {
		return nil, errors.New("AES-GCM envelope contains unknown or missing fields")
	}
	var version uint64
	var nonce, ciphertext []byte
	if strict.Unmarshal(fields[0], &version) != nil || version != 1 {
		return nil, errors.New("unsupported AES-GCM envelope version")
	}
	if strict.Unmarshal(fields[1], &nonce) != nil || len(nonce) != 12 {
		return nil, errors.New("AES-GCM nonce must contain exactly 12 bytes")
	}
	if strict.Unmarshal(fields[2], &ciphertext) != nil || len(ciphertext) < 16 {
		return nil, errors.New("AES-GCM ciphertext is shorter than the authentication tag")
	}
	key, err := t.DeriveKey(purpose)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, additionalData)
	if err != nil {
		return nil, errors.New("pairing payload authentication failed")
	}
	reencoded, err := canonical.Marshal(map[uint64]any{0: version, 1: nonce, 2: ciphertext})
	if err != nil || !bytes.Equal(reencoded, envelope) {
		return nil, errors.New("AES-GCM envelope is not deterministic RFC 8949 CBOR")
	}
	return plaintext, nil
}
