package nativewebrtc

import (
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const (
	SignalVersion = 2
	signalTTL     = 120 * time.Second
)

var (
	DefaultNostrURLs = []string{
		"wss://nos.lol",
		"wss://nostr-01.yakihonne.com",
		"wss://relay.primal.net",
		"wss://purplerelay.com",
		"wss://relay.nostr.place",
	}
	DefaultTorrentURLs = []string{
		"wss://open.ftorrent.com",
		"wss://tracker.webtorrent.dev",
		"wss://tracker.openwebtorrent.com",
		"wss://tracker.btorrent.xyz",
		"wss://tracker.files.fm:7073/announce",
	}
)

type Signal struct {
	Version   int            `json:"version"`
	Room      string         `json:"room"`
	Type      string         `json:"type"`
	SessionID string         `json:"sessionId"`
	From      string         `json:"from"`
	To        string         `json:"to,omitempty"`
	CreatedAt int64          `json:"createdAt"`
	SDP       string         `json:"sdp,omitempty"`
	Candidate map[string]any `json:"candidate,omitempty"`
	Encrypted string         `json:"encrypted,omitempty"`
}

type SignalingSession interface {
	Name() string
	PeerID() string
	Publish(context.Context, Signal) error
	Events() <-chan Signal
	Status() (open, total int)
	Close() error
}

func CreateSignalingPeerID() (string, error) {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	random := make([]byte, 20)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	result := make([]byte, len(random))
	for index, value := range random {
		result[index] = alphabet[int(value)%len(alphabet)]
	}
	return string(result), nil
}

func CreateSignalingSessionID() (string, error) {
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return hex.EncodeToString(random), nil
}

func SignalingRoomTopic(roomID string, token *pairing.Token) (string, error) {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return "", errors.New("native signaling roomId is required")
	}
	if token != nil {
		return token.RendezvousID("signaling", 0)
	}
	sum := sha256.Sum256([]byte("p2p-netcat:native-webrtc:v2:" + roomID))
	return hex.EncodeToString(sum[:]), nil
}

func TorrentInfoHash(topic string) string {
	sum := sha1.Sum([]byte(topic))
	var builder strings.Builder
	for _, value := range sum {
		builder.WriteString(strconv.FormatUint(uint64(value), 36))
	}
	result := builder.String()
	if len(result) > 20 {
		result = result[:20]
	}
	return result
}

func prepareOutgoing(signal Signal, topic, peerID string, token *pairing.Token) (Signal, error) {
	if !clientIDPattern.MatchString(peerID) {
		return Signal{}, errors.New("native signaling peerId must contain exactly 20 ASCII letters or digits")
	}
	switch signal.Type {
	case "offer", "answer":
		if signal.SDP == "" {
			return Signal{}, fmt.Errorf("native signaling %s does not contain SDP", signal.Type)
		}
	case "candidate":
		if signal.Candidate == nil {
			return Signal{}, errors.New("native signaling candidate is required")
		}
	case "bye":
	default:
		return Signal{}, fmt.Errorf("unsupported native signaling type: %s", signal.Type)
	}
	if len(signal.SessionID) < 8 || len(signal.SessionID) > 128 {
		return Signal{}, errors.New("native signaling sessionId is invalid")
	}
	signal.Version = SignalVersion
	signal.Room = topic
	signal.From = peerID
	signal.CreatedAt = time.Now().UnixMilli()
	if token == nil {
		return signal, nil
	}
	metadata := signal
	metadata.SDP = ""
	metadata.Candidate = nil
	payload := struct {
		SDP       string         `json:"sdp,omitempty"`
		Candidate map[string]any `json:"candidate,omitempty"`
	}{SDP: signal.SDP, Candidate: signal.Candidate}
	plaintext, err := json.Marshal(payload)
	if err != nil {
		return Signal{}, err
	}
	additional, err := signalAdditionalData(metadata)
	if err != nil {
		return Signal{}, err
	}
	sealed, err := token.Seal("signaling", plaintext, additional, nil)
	if err != nil {
		return Signal{}, err
	}
	metadata.Encrypted = base64.RawURLEncoding.EncodeToString(sealed)
	return metadata, nil
}

func openIncoming(signal Signal, topic, peerID string, token *pairing.Token) (Signal, bool) {
	if token != nil {
		if signal.Encrypted == "" {
			return Signal{}, false
		}
		metadata := signal
		metadata.SDP = ""
		metadata.Candidate = nil
		additional, err := signalAdditionalData(metadata)
		if err != nil {
			return Signal{}, false
		}
		envelope, err := base64.RawURLEncoding.Strict().DecodeString(signal.Encrypted)
		if err != nil {
			return Signal{}, false
		}
		plaintext, err := token.Open("signaling", envelope, additional)
		if err != nil {
			return Signal{}, false
		}
		var payload struct {
			SDP       string         `json:"sdp"`
			Candidate map[string]any `json:"candidate"`
		}
		if json.Unmarshal(plaintext, &payload) != nil {
			return Signal{}, false
		}
		signal.SDP, signal.Candidate = payload.SDP, payload.Candidate
	}
	if signal.Version != SignalVersion || signal.Room != topic || signal.From == peerID {
		return Signal{}, false
	}
	if signal.To != "" && signal.To != peerID {
		return Signal{}, false
	}
	if signal.SessionID == "" || signal.From == "" ||
		time.Since(time.UnixMilli(signal.CreatedAt)).Abs() > signalTTL {
		return Signal{}, false
	}
	switch signal.Type {
	case "offer", "answer":
		if signal.SDP == "" {
			return Signal{}, false
		}
	case "candidate":
		if signal.Candidate == nil {
			return Signal{}, false
		}
	case "bye":
	default:
		return Signal{}, false
	}
	return signal, true
}

func signalAdditionalData(signal Signal) ([]byte, error) {
	return json.Marshal([]any{
		SignalVersion,
		signal.Room,
		signal.Type,
		signal.SessionID,
		signal.From,
		signal.To,
		signal.CreatedAt,
	})
}
