// Package nativewebrtc implements the native p2p-netcat WebRTC protocol used
// by the JavaScript CLI and browser client.
package nativewebrtc

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	ProtocolVersion  = byte(2)
	DataChannelLabel = "p2p-netcat-v2"

	FrameData          = byte(0)
	FrameControl       = byte(1)
	FrameAuthChallenge = byte(2)
	FrameAuthResponse  = byte(3)
	FrameAuthReady     = byte(4)

	ReconnectGrace = 120 * time.Second

	AuthResponseVersionV1 = byte(1)
	AuthResponseVersionV2 = byte(2)
	AuthDomainV2          = "p2p-netcat/native-webrtc-auth/v2"
)

var clientIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{20}$`)

type Frame struct {
	Type    byte
	Payload []byte
}

// AuthTranscript identifies one exact signaling exchange and its negotiated
// WebRTC transport. OfferSDP and AnswerSDP are the exact strings transmitted
// through signaling, before any PeerConnection normalization.
type AuthTranscript struct {
	SessionID string
	OfferSDP  string
	AnswerSDP string
}

func EncodeFrame(frameType byte, payload []byte) []byte {
	result := make([]byte, 2+len(payload))
	result[0] = ProtocolVersion
	result[1] = frameType
	copy(result[2:], payload)
	return result
}

func DecodeFrame(value []byte) (Frame, error) {
	if len(value) < 2 {
		return Frame{}, errors.New("native WebRTC frame is shorter than its header")
	}
	if value[0] != ProtocolVersion {
		return Frame{}, fmt.Errorf("unsupported native WebRTC version: %d", value[0])
	}
	return Frame{Type: value[1], Payload: append([]byte(nil), value[2:]...)}, nil
}

func RoomID(peerID peer.ID, service uint16) (string, error) {
	if peerID == "" {
		return "", errors.New("PeerId is required")
	}
	if service == 0 {
		return "", errors.New("logical port must be between 1 and 65535")
	}
	return fmt.Sprintf("%s:%d", peerID, service), nil
}

func AuthPayload(peerID peer.ID, service uint16, challenge []byte) ([]byte, error) {
	if len(challenge) != 32 {
		return nil, fmt.Errorf("WebRTC challenge must contain 32 bytes; got %d", len(challenge))
	}
	room, err := RoomID(peerID, service)
	if err != nil {
		return nil, err
	}
	prefix := []byte("p2p-netcat/trystero-auth/v1\x00" + room + "\x00")
	return append(prefix, challenge...), nil
}

// AuthPayloadV2 binds the server identity proof to one exact WebRTC signaling
// transcript. SDP hashes cover the DTLS certificate fingerprints embedded in
// the offer and answer, preventing a valid proof from being moved to another
// PeerConnection.
func AuthPayloadV2(peerID peer.ID, service uint16, challenge []byte, transcript AuthTranscript) ([]byte, error) {
	if len(challenge) != 32 {
		return nil, fmt.Errorf("WebRTC challenge must contain 32 bytes; got %d", len(challenge))
	}
	if _, err := RoomID(peerID, service); err != nil {
		return nil, err
	}
	if len(transcript.SessionID) < 8 || len(transcript.SessionID) > 128 {
		return nil, errors.New("WebRTC signaling session ID must contain between 8 and 128 bytes")
	}
	if transcript.OfferSDP == "" || len(transcript.OfferSDP) > maxSDPBytes {
		return nil, errors.New("WebRTC offer SDP is empty or exceeds the signaling limit")
	}
	if transcript.AnswerSDP == "" || len(transcript.AnswerSDP) > maxSDPBytes {
		return nil, errors.New("WebRTC answer SDP is empty or exceeds the signaling limit")
	}
	offerHash := sha256.Sum256([]byte(transcript.OfferSDP))
	answerHash := sha256.Sum256([]byte(transcript.AnswerSDP))
	serviceBytes := make([]byte, 2)
	binary.BigEndian.PutUint16(serviceBytes, service)
	fields := [][]byte{
		[]byte(AuthDomainV2),
		{AuthResponseVersionV2},
		[]byte("client"),
		[]byte("server"),
		[]byte(peerID.String()),
		serviceBytes,
		[]byte(transcript.SessionID),
		challenge,
		offerHash[:],
		answerHash[:],
	}
	length := 0
	for _, field := range fields {
		length += 4 + len(field)
	}
	payload := make([]byte, 0, length)
	for _, field := range fields {
		var encodedLength [4]byte
		binary.BigEndian.PutUint32(encodedLength[:], uint32(len(field)))
		payload = append(payload, encodedLength[:]...)
		payload = append(payload, field...)
	}
	return payload, nil
}

func CreateClientChallenge(clientID string) ([]byte, error) {
	if !clientIDPattern.MatchString(clientID) {
		return nil, errors.New("WebRTC client ID must contain exactly 20 ASCII letters or digits")
	}
	result := make([]byte, 32)
	if _, err := rand.Read(result); err != nil {
		return nil, err
	}
	copy(result, clientID)
	return result, nil
}

func ClientIDFromChallenge(challenge []byte) string {
	if len(challenge) != 32 {
		return ""
	}
	value := string(challenge[:20])
	if !clientIDPattern.MatchString(value) {
		return ""
	}
	return value
}

func SignAuthResponse(privateKey crypto.PrivKey, service uint16, challenge []byte) ([]byte, error) {
	if privateKey == nil {
		return nil, errors.New("libp2p private key is required")
	}
	peerID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	payload, err := AuthPayload(peerID, service, challenge)
	if err != nil {
		return nil, err
	}
	signature, err := privateKey.Sign(payload)
	if err != nil {
		return nil, err
	}
	publicKey, err := crypto.MarshalPublicKey(privateKey.GetPublic())
	if err != nil {
		return nil, err
	}
	return EncodeAuthResponse(publicKey, signature)
}

func SignAuthResponseV2(
	privateKey crypto.PrivKey,
	service uint16,
	challenge []byte,
	transcript AuthTranscript,
) ([]byte, error) {
	if privateKey == nil {
		return nil, errors.New("libp2p private key is required")
	}
	peerID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	payload, err := AuthPayloadV2(peerID, service, challenge, transcript)
	if err != nil {
		return nil, err
	}
	signature, err := privateKey.Sign(payload)
	if err != nil {
		return nil, err
	}
	publicKey, err := crypto.MarshalPublicKey(privateKey.GetPublic())
	if err != nil {
		return nil, err
	}
	return encodeAuthResponse(AuthResponseVersionV2, publicKey, signature)
}

func VerifyAuthResponse(value []byte, expected peer.ID, service uint16, challenge []byte) (bool, error) {
	publicBytes, signature, err := DecodeAuthResponse(value)
	if err != nil {
		return false, err
	}
	publicKey, err := crypto.UnmarshalPublicKey(publicBytes)
	if err != nil {
		return false, err
	}
	actual, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		return false, err
	}
	if actual != expected {
		return false, nil
	}
	payload, err := AuthPayload(expected, service, challenge)
	if err != nil {
		return false, err
	}
	return publicKey.Verify(payload, signature)
}

func VerifyAuthResponseV2(
	value []byte,
	expected peer.ID,
	service uint16,
	challenge []byte,
	transcript AuthTranscript,
) (bool, error) {
	publicBytes, signature, err := decodeAuthResponse(AuthResponseVersionV2, value)
	if err != nil {
		return false, err
	}
	publicKey, err := crypto.UnmarshalPublicKey(publicBytes)
	if err != nil {
		return false, err
	}
	actual, err := peer.IDFromPublicKey(publicKey)
	if err != nil {
		return false, err
	}
	if actual != expected {
		return false, nil
	}
	payload, err := AuthPayloadV2(expected, service, challenge, transcript)
	if err != nil {
		return false, err
	}
	return publicKey.Verify(payload, signature)
}

func EncodeAuthResponse(publicKey, signature []byte) ([]byte, error) {
	return encodeAuthResponse(AuthResponseVersionV1, publicKey, signature)
}

func encodeAuthResponse(version byte, publicKey, signature []byte) ([]byte, error) {
	if len(publicKey) > 0xffff || len(signature) > 0xffff {
		return nil, errors.New("WebRTC authentication response is too large")
	}
	result := make([]byte, 5+len(publicKey)+len(signature))
	result[0] = version
	binary.BigEndian.PutUint16(result[1:3], uint16(len(publicKey)))
	binary.BigEndian.PutUint16(result[3:5], uint16(len(signature)))
	copy(result[5:], publicKey)
	copy(result[5+len(publicKey):], signature)
	return result, nil
}

func DecodeAuthResponse(value []byte) ([]byte, []byte, error) {
	return decodeAuthResponse(AuthResponseVersionV1, value)
}

func decodeAuthResponse(version byte, value []byte) ([]byte, []byte, error) {
	if len(value) < 5 || value[0] != version {
		return nil, nil, errors.New("unsupported WebRTC authentication response")
	}
	publicLength := int(binary.BigEndian.Uint16(value[1:3]))
	signatureLength := int(binary.BigEndian.Uint16(value[3:5]))
	if 5+publicLength+signatureLength != len(value) {
		return nil, nil, errors.New("malformed WebRTC authentication response")
	}
	return append([]byte(nil), value[5:5+publicLength]...),
		append([]byte(nil), value[5+publicLength:]...), nil
}
