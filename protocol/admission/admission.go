// Package admission implements the fixed-size mutual pairing-token handshake.
package admission

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const (
	Version      = byte(1)
	ClientHello  = byte(1)
	ServerAck    = byte(2)
	NonceSize    = 16
	MACSize      = 32
	FrameSize    = 62
	MaxClockSkew = 120 * time.Second
)

var magic = [4]byte{'P', 'N', 'C', 'A'}

type Hello struct {
	Timestamp   uint64
	ClientNonce [NonceSize]byte
}

type frame struct {
	kind      byte
	timestamp uint64
	nonce     [NonceSize]byte
	mac       [MACSize]byte
}

func CreateHello(token *pairing.Token, now time.Time, nonce []byte) ([]byte, Hello, error) {
	var hello Hello
	if err := token.Validate(now); err != nil {
		return nil, hello, err
	}
	hello.Timestamp = uint64(now.Unix())
	if err := fillNonce(hello.ClientNonce[:], nonce); err != nil {
		return nil, hello, err
	}
	mac, err := calculateMAC(token, macInput("client", token, hello.Timestamp, hello.ClientNonce[:], nil))
	if err != nil {
		return nil, hello, err
	}
	return encode(ClientHello, hello.Timestamp, hello.ClientNonce, mac), hello, nil
}

func VerifyHello(token *pairing.Token, encoded []byte, now time.Time, maxSkew time.Duration) (Hello, error) {
	var hello Hello
	if err := token.Validate(now); err != nil {
		return hello, err
	}
	value, err := decode(encoded, ClientHello)
	if err != nil {
		return hello, err
	}
	timestamp := time.Unix(int64(value.timestamp), 0)
	if maxSkew < 0 || timestamp.Before(now.Add(-maxSkew)) || timestamp.After(now.Add(maxSkew)) {
		return hello, errors.New("timestamp admission handshake находится вне допустимого окна")
	}
	expected, err := calculateMAC(token, macInput("client", token, value.timestamp, value.nonce[:], nil))
	if err != nil {
		return hello, err
	}
	if subtle.ConstantTimeCompare(expected[:], value.mac[:]) != 1 {
		return hello, errors.New("pairing-token authentication не прошла")
	}
	hello.Timestamp = value.timestamp
	hello.ClientNonce = value.nonce
	return hello, nil
}

func CreateAck(token *pairing.Token, hello Hello, nonce []byte) ([]byte, error) {
	var serverNonce [NonceSize]byte
	if err := fillNonce(serverNonce[:], nonce); err != nil {
		return nil, err
	}
	mac, err := calculateMAC(token, macInput(
		"server", token, hello.Timestamp, hello.ClientNonce[:], serverNonce[:],
	))
	if err != nil {
		return nil, err
	}
	return encode(ServerAck, hello.Timestamp, serverNonce, mac), nil
}

func VerifyAck(token *pairing.Token, hello Hello, encoded []byte) error {
	value, err := decode(encoded, ServerAck)
	if err != nil {
		return err
	}
	if value.timestamp != hello.Timestamp {
		return errors.New("timestamp admission handshake изменился")
	}
	expected, err := calculateMAC(token, macInput(
		"server", token, hello.Timestamp, hello.ClientNonce[:], value.nonce[:],
	))
	if err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(expected[:], value.mac[:]) != 1 {
		return errors.New("подтверждение pairing-token сервером не прошло")
	}
	return nil
}

func AuthenticateClient(stream io.ReadWriter, token *pairing.Token, timeout time.Duration) error {
	now := time.Now()
	encoded, hello, err := CreateHello(token, now, nil)
	if err != nil {
		return err
	}
	if deadline, ok := stream.(interface{ SetDeadline(time.Time) error }); ok {
		_ = deadline.SetDeadline(now.Add(timeout))
		defer deadline.SetDeadline(time.Time{})
	}
	if err := writeFull(stream, encoded); err != nil {
		return fmt.Errorf("отправить admission hello: %w", err)
	}
	ack := make([]byte, FrameSize)
	if _, err := io.ReadFull(stream, ack); err != nil {
		return fmt.Errorf("прочитать admission ack: %w", err)
	}
	return VerifyAck(token, hello, ack)
}

func AuthenticateServer(stream io.ReadWriter, token *pairing.Token, timeout time.Duration) error {
	now := time.Now()
	if deadline, ok := stream.(interface{ SetDeadline(time.Time) error }); ok {
		_ = deadline.SetDeadline(now.Add(timeout))
		defer deadline.SetDeadline(time.Time{})
	}
	encoded := make([]byte, FrameSize)
	if _, err := io.ReadFull(stream, encoded); err != nil {
		return fmt.Errorf("прочитать admission hello: %w", err)
	}
	hello, err := VerifyHello(token, encoded, time.Now(), MaxClockSkew)
	if err != nil {
		return err
	}
	ack, err := CreateAck(token, hello, nil)
	if err != nil {
		return err
	}
	if err := writeFull(stream, ack); err != nil {
		return fmt.Errorf("отправить admission ack: %w", err)
	}
	return nil
}

func encode(kind byte, timestamp uint64, nonce [NonceSize]byte, mac [MACSize]byte) []byte {
	result := make([]byte, FrameSize)
	copy(result[0:4], magic[:])
	result[4] = Version
	result[5] = kind
	binary.BigEndian.PutUint64(result[6:14], timestamp)
	copy(result[14:30], nonce[:])
	copy(result[30:62], mac[:])
	return result
}

func decode(encoded []byte, expectedKind byte) (frame, error) {
	var result frame
	if len(encoded) != FrameSize {
		return result, fmt.Errorf("admission frame должен содержать ровно %d байта", FrameSize)
	}
	if subtle.ConstantTimeCompare(encoded[0:4], magic[:]) != 1 {
		return result, errors.New("некорректная сигнатура admission frame")
	}
	if encoded[4] != Version {
		return result, fmt.Errorf("неподдерживаемая версия admission: %d", encoded[4])
	}
	if encoded[5] != expectedKind {
		return result, fmt.Errorf("неожиданный тип admission frame: %d", encoded[5])
	}
	result.kind = encoded[5]
	result.timestamp = binary.BigEndian.Uint64(encoded[6:14])
	copy(result.nonce[:], encoded[14:30])
	copy(result.mac[:], encoded[30:62])
	return result, nil
}

func calculateMAC(token *pairing.Token, message []byte) ([MACSize]byte, error) {
	var result [MACSize]byte
	key, err := token.DeriveKey("admission")
	if err != nil {
		return result, err
	}
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(message)
	copy(result[:], mac.Sum(nil))
	return result, nil
}

func macInput(role string, token *pairing.Token, timestamp uint64, clientNonce, serverNonce []byte) []byte {
	prefix := []byte(fmt.Sprintf(
		"p2p-netcat/session-auth/v1\x00%s\x00%s\x00%d\x00%d\x00",
		role, token.PeerID, token.Service, timestamp,
	))
	result := make([]byte, 0, len(prefix)+len(clientNonce)+len(serverNonce))
	result = append(result, prefix...)
	result = append(result, clientNonce...)
	result = append(result, serverNonce...)
	return result
}

func fillNonce(destination, source []byte) error {
	if source == nil {
		_, err := rand.Read(destination)
		return err
	}
	if len(source) != NonceSize {
		return fmt.Errorf("admission nonce должен содержать ровно %d байт", NonceSize)
	}
	copy(destination, source)
	return nil
}

func writeFull(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}
