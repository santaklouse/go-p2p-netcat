// Package nativewebrtc implements the native p2p-netcat WebRTC protocol used
// by the JavaScript CLI and browser client.
package nativewebrtc

import (
	"crypto/rand"
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
)

var clientIDPattern = regexp.MustCompile(`^[0-9A-Za-z]{20}$`)

type Frame struct {
	Type    byte
	Payload []byte
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
		return Frame{}, errors.New("native WebRTC frame короче заголовка")
	}
	if value[0] != ProtocolVersion {
		return Frame{}, fmt.Errorf("неподдерживаемая версия native WebRTC: %d", value[0])
	}
	return Frame{Type: value[1], Payload: append([]byte(nil), value[2:]...)}, nil
}

func RoomID(peerID peer.ID, service uint16) (string, error) {
	if peerID == "" {
		return "", errors.New("PeerId не задан")
	}
	if service == 0 {
		return "", errors.New("логический порт должен быть от 1 до 65535")
	}
	return fmt.Sprintf("%s:%d", peerID, service), nil
}

func AuthPayload(peerID peer.ID, service uint16, challenge []byte) ([]byte, error) {
	if len(challenge) != 32 {
		return nil, fmt.Errorf("WebRTC challenge должен содержать 32 байта, получено: %d", len(challenge))
	}
	room, err := RoomID(peerID, service)
	if err != nil {
		return nil, err
	}
	prefix := []byte("p2p-netcat/trystero-auth/v1\x00" + room + "\x00")
	return append(prefix, challenge...), nil
}

func CreateClientChallenge(clientID string) ([]byte, error) {
	if !clientIDPattern.MatchString(clientID) {
		return nil, errors.New("WebRTC client ID должен содержать ровно 20 латинских букв или цифр")
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
		return nil, errors.New("libp2p private key не задан")
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

func EncodeAuthResponse(publicKey, signature []byte) ([]byte, error) {
	if len(publicKey) > 0xffff || len(signature) > 0xffff {
		return nil, errors.New("WebRTC authentication response слишком большой")
	}
	result := make([]byte, 5+len(publicKey)+len(signature))
	result[0] = 1
	binary.BigEndian.PutUint16(result[1:3], uint16(len(publicKey)))
	binary.BigEndian.PutUint16(result[3:5], uint16(len(signature)))
	copy(result[5:], publicKey)
	copy(result[5+len(publicKey):], signature)
	return result, nil
}

func DecodeAuthResponse(value []byte) ([]byte, []byte, error) {
	if len(value) < 5 || value[0] != 1 {
		return nil, nil, errors.New("неподдерживаемый WebRTC authentication response")
	}
	publicLength := int(binary.BigEndian.Uint16(value[1:3]))
	signatureLength := int(binary.BigEndian.Uint16(value[3:5]))
	if 5+publicLength+signatureLength != len(value) {
		return nil, nil, errors.New("повреждённый WebRTC authentication response")
	}
	return append([]byte(nil), value[5:5+publicLength]...),
		append([]byte(nil), value[5+publicLength:]...), nil
}
