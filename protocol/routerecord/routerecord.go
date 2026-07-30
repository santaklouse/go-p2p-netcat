// Package routerecord implements signed deterministic-CBOR route records.
package routerecord

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
)

const (
	Version            = uint64(1)
	EnvelopeVersion    = uint64(1)
	DefaultTTL         = 180 * time.Second
	MaxPayloadBytes    = 64 * 1024
	MaxAddresses       = 64
	MaxServices        = 64
	CapabilityTCP      = uint64(1)
	CapabilityQUIC     = uint64(2)
	CapabilityWS       = uint64(4)
	CapabilityWSS      = uint64(8)
	CapabilityWebTrans = uint64(16)
	CapabilityWebRTC   = uint64(32)
	CapabilityRelay    = uint64(64)
	allCapabilities    = CapabilityTCP | CapabilityQUIC | CapabilityWS | CapabilityWSS |
		CapabilityWebTrans | CapabilityWebRTC | CapabilityRelay
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
		DupMapKey:        cbor.DupMapKeyEnforcedAPF,
		IndefLength:      cbor.IndefLengthForbidden,
		TagsMd:           cbor.TagsForbidden,
		IntDec:           cbor.IntDecConvertSignedOrFail,
		MaxNestedLevels:  16,
		MaxArrayElements: 128,
		MaxMapPairs:      32,
	}.DecMode()
	if err != nil {
		panic(err)
	}
}

type Record struct {
	Version           uint64
	PeerID            peer.ID
	Sequence          uint64
	IssuedAt          uint64
	ExpiresAt         uint64
	Services          []uint16
	Addresses         []ma.Multiaddr
	RelayReservations []ma.Multiaddr
	Capabilities      uint64
}

type VerifyOptions struct {
	ExpectedPeerID  peer.ID
	ExpectedService uint16
	Now             time.Time
	ClockSkew       time.Duration
}

func Sign(privateKey crypto.PrivKey, record Record) ([]byte, error) {
	publicKey := privateKey.GetPublic()
	peerID, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	record.PeerID = peerID
	now := time.Now()
	if record.Version == 0 {
		record.Version = Version
	}
	if record.IssuedAt == 0 {
		record.IssuedAt = uint64(now.Unix())
	}
	if record.ExpiresAt == 0 {
		record.ExpiresAt = uint64(now.Add(DefaultTTL).Unix())
	}
	payload, err := EncodePayload(record)
	if err != nil {
		return nil, err
	}
	signature, err := privateKey.Sign(payload)
	if err != nil {
		return nil, err
	}
	publicBytes, err := crypto.MarshalPublicKey(publicKey)
	if err != nil {
		return nil, err
	}
	return canonical.Marshal(map[uint64]any{
		0: EnvelopeVersion,
		1: payload,
		2: publicBytes,
		3: signature,
	})
}

func Verify(envelope []byte, options VerifyOptions) (Record, error) {
	var empty Record
	fields, err := decodeMap(envelope, map[uint64]bool{0: true, 1: true, 2: true, 3: true})
	if err != nil {
		return empty, err
	}
	var version uint64
	var payload, publicBytes, signature []byte
	if strict.Unmarshal(fields[0], &version) != nil || version != EnvelopeVersion {
		return empty, errors.New("unsupported route record envelope version")
	}
	if strict.Unmarshal(fields[1], &payload) != nil || len(payload) == 0 || len(payload) > MaxPayloadBytes {
		return empty, fmt.Errorf("route record payload must contain 1..%d bytes", MaxPayloadBytes)
	}
	if strict.Unmarshal(fields[2], &publicBytes) != nil || strict.Unmarshal(fields[3], &signature) != nil {
		return empty, errors.New("invalid route record key or signature")
	}
	publicKey, err := crypto.UnmarshalPublicKey(publicBytes)
	if err != nil {
		return empty, err
	}
	valid, err := publicKey.Verify(payload, signature)
	if err != nil || !valid {
		return empty, errors.New("route record signature is invalid")
	}
	record, err := DecodePayload(payload)
	if err != nil {
		return empty, err
	}
	authenticated, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		return empty, err
	}
	if record.PeerID != authenticated {
		return empty, fmt.Errorf("route record PeerId %s does not match key %s", record.PeerID, authenticated)
	}
	if options.ExpectedPeerID != "" && record.PeerID != options.ExpectedPeerID {
		return empty, fmt.Errorf("route record belongs to %s, not %s", record.PeerID, options.ExpectedPeerID)
	}
	if options.ExpectedService != 0 && !containsService(record.Services, options.ExpectedService) {
		return empty, fmt.Errorf("route record does not advertise logical port %d", options.ExpectedService)
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now()
	}
	skew := options.ClockSkew
	if skew == 0 {
		skew = 30 * time.Second
	}
	if time.Unix(int64(record.IssuedAt), 0).After(now.Add(skew)) {
		return empty, errors.New("route record was issued in the future")
	}
	if time.Unix(int64(record.ExpiresAt), 0).Before(now.Add(-skew)) {
		return empty, errors.New("route record has expired")
	}
	reencoded, err := canonical.Marshal(map[uint64]any{
		0: version, 1: payload, 2: publicBytes, 3: signature,
	})
	if err != nil || !bytes.Equal(reencoded, envelope) {
		return empty, errors.New("route record envelope is not deterministic RFC 8949 CBOR")
	}
	return record, nil
}

func EncodePayload(record Record) ([]byte, error) {
	record, err := normalize(record)
	if err != nil {
		return nil, err
	}
	services := make([]uint64, len(record.Services))
	for index, service := range record.Services {
		services[index] = uint64(service)
	}
	return canonical.Marshal(map[uint64]any{
		0: record.Version,
		1: record.PeerID.String(),
		2: record.Sequence,
		3: record.IssuedAt,
		4: record.ExpiresAt,
		5: services,
		6: addressStrings(record.Addresses),
		7: addressStrings(record.RelayReservations),
		8: record.Capabilities,
	})
}

func DecodePayload(payload []byte) (Record, error) {
	fields, err := decodeMap(payload, map[uint64]bool{
		0: true, 1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true,
	})
	if err != nil {
		return Record{}, err
	}
	var version, sequence, issuedAt, expiresAt, capabilities uint64
	var peerText string
	var services []uint64
	var addresses, relays []string
	values := []struct {
		raw    cbor.RawMessage
		target any
	}{
		{fields[0], &version}, {fields[1], &peerText}, {fields[2], &sequence},
		{fields[3], &issuedAt}, {fields[4], &expiresAt}, {fields[5], &services},
		{fields[6], &addresses}, {fields[7], &relays}, {fields[8], &capabilities},
	}
	for _, value := range values {
		if err := strict.Unmarshal(value.raw, value.target); err != nil {
			return Record{}, fmt.Errorf("decode route record: %w", err)
		}
	}
	peerID, err := peer.Decode(peerText)
	if err != nil {
		return Record{}, err
	}
	record := Record{
		Version: version, PeerID: peerID, Sequence: sequence, IssuedAt: issuedAt,
		ExpiresAt: expiresAt, Capabilities: capabilities,
	}
	for _, service := range services {
		if service == 0 || service > 65535 {
			return Record{}, errors.New("invalid logical port in route record")
		}
		record.Services = append(record.Services, uint16(service))
	}
	record.Addresses, err = parseAddresses(addresses)
	if err != nil {
		return Record{}, err
	}
	record.RelayReservations, err = parseAddresses(relays)
	if err != nil {
		return Record{}, err
	}
	record, err = normalize(record)
	if err != nil {
		return Record{}, err
	}
	reencoded, err := EncodePayload(record)
	if err != nil || !bytes.Equal(reencoded, payload) {
		return Record{}, errors.New("route record payload is not deterministic RFC 8949 CBOR")
	}
	return record, nil
}

func normalize(record Record) (Record, error) {
	if record.Version != Version {
		return Record{}, fmt.Errorf("unsupported route record version: %d", record.Version)
	}
	if record.PeerID == "" {
		return Record{}, errors.New("route record PeerId is required")
	}
	if record.ExpiresAt <= record.IssuedAt {
		return Record{}, errors.New("route record expiration must be later than issuedAt")
	}
	if len(record.Services) == 0 || len(record.Services) > MaxServices {
		return Record{}, fmt.Errorf("route record must contain 1..%d services", MaxServices)
	}
	record.Services = uniqueServices(record.Services)
	if len(record.Addresses) > MaxAddresses || len(record.RelayReservations) > MaxAddresses {
		return Record{}, fmt.Errorf("route record supports at most %d addresses of each type", MaxAddresses)
	}
	record.Addresses = uniqueAddresses(record.Addresses)
	record.RelayReservations = uniqueAddresses(record.RelayReservations)
	if record.Capabilities&^allCapabilities != 0 {
		return Record{}, fmt.Errorf("invalid capability mask: %d", record.Capabilities)
	}
	return record, nil
}

func decodeMap(input []byte, allowed map[uint64]bool) (map[uint64]cbor.RawMessage, error) {
	var fields map[uint64]cbor.RawMessage
	if err := strict.Unmarshal(input, &fields); err != nil {
		return nil, fmt.Errorf("invalid deterministic CBOR route record: %w", err)
	}
	for key := range fields {
		if !allowed[key] {
			return nil, fmt.Errorf("unknown route record field: %d", key)
		}
	}
	if len(fields) != len(allowed) {
		return nil, errors.New("route record is missing required fields")
	}
	return fields, nil
}

func uniqueServices(values []uint16) []uint16 {
	seen := make(map[uint16]struct{}, len(values))
	result := make([]uint16, 0, len(values))
	for _, value := range values {
		if value == 0 {
			continue
		}
		if _, ok := seen[value]; !ok {
			seen[value] = struct{}{}
			result = append(result, value)
		}
	}
	sort.Slice(result, func(left, right int) bool { return result[left] < result[right] })
	return result
}

func uniqueAddresses(values []ma.Multiaddr) []ma.Multiaddr {
	seen := make(map[string]ma.Multiaddr, len(values))
	for _, value := range values {
		if value != nil {
			seen[value.String()] = value
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ma.Multiaddr, 0, len(keys))
	for _, key := range keys {
		result = append(result, seen[key])
	}
	return result
}

func addressStrings(values []ma.Multiaddr) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func parseAddresses(values []string) ([]ma.Multiaddr, error) {
	result := make([]ma.Multiaddr, 0, len(values))
	for _, value := range values {
		address, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, err
		}
		result = append(result, address)
	}
	return result, nil
}

func containsService(values []uint16, expected uint16) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
