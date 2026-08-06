package nativewebrtc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/santaklouse/go-p2p-netcat/protocol/datagram"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

func TestFrameWireCodec(t *testing.T) {
	encoded := EncodeFrame(FrameControl, []byte("ack:42"))
	if !bytes.Equal(encoded, []byte{2, 1, 'a', 'c', 'k', ':', '4', '2'}) {
		t.Fatalf("encoded = %v", encoded)
	}
	frame, err := DecodeFrame(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != FrameControl || string(frame.Payload) != "ack:42" {
		t.Fatalf("decoded = %+v", frame)
	}
	if _, err := DecodeFrame([]byte{99, 0}); err == nil {
		t.Fatal("expected unsupported-version error")
	}
}

func TestPeerAuthenticationMatchesExactIdentityAndService(t *testing.T) {
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	challenge := bytes.Repeat([]byte{0x41}, 32)
	response, err := SignAuthResponse(privateKey, 31337, challenge)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyAuthResponse(response, id, 31337, challenge)
	if err != nil || !valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}
	valid, err = VerifyAuthResponse(response, id, 31338, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("signature must be bound to logical service")
	}
}

func TestPeerAuthenticationV2BindsExactWebRTCTranscript(t *testing.T) {
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	challenge := bytes.Repeat([]byte{0x41}, 32)
	transcript := AuthTranscript{
		SessionID: "0123456789abcdef0123456789abcdef",
		OfferSDP:  "v=0\r\na=fingerprint:sha-256 OFFER\r\n",
		AnswerSDP: "v=0\r\na=fingerprint:sha-256 ANSWER\r\n",
	}
	response, err := SignAuthResponseV2(privateKey, 31337, challenge, transcript)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyAuthResponseV2(response, id, 31337, challenge, transcript)
	if err != nil || !valid {
		t.Fatalf("valid=%v err=%v", valid, err)
	}

	changedChallenge := append([]byte(nil), challenge...)
	changedChallenge[31] ^= 1
	tests := []struct {
		name       string
		service    uint16
		challenge  []byte
		transcript AuthTranscript
	}{
		{name: "service", service: 31338, challenge: challenge, transcript: transcript},
		{name: "challenge", service: 31337, challenge: changedChallenge, transcript: transcript},
		{name: "session", service: 31337, challenge: challenge, transcript: AuthTranscript{
			SessionID: "fedcba9876543210fedcba9876543210", OfferSDP: transcript.OfferSDP, AnswerSDP: transcript.AnswerSDP,
		}},
		{name: "offer", service: 31337, challenge: challenge, transcript: AuthTranscript{
			SessionID: transcript.SessionID, OfferSDP: transcript.OfferSDP + "a=x:changed\r\n", AnswerSDP: transcript.AnswerSDP,
		}},
		{name: "answer", service: 31337, challenge: challenge, transcript: AuthTranscript{
			SessionID: transcript.SessionID, OfferSDP: transcript.OfferSDP, AnswerSDP: transcript.AnswerSDP + "a=x:changed\r\n",
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			valid, verifyErr := VerifyAuthResponseV2(response, id, test.service, test.challenge, test.transcript)
			if verifyErr != nil {
				t.Fatal(verifyErr)
			}
			if valid {
				t.Fatal("authentication proof transferred to a different WebRTC transcript")
			}
		})
	}

	legacy, err := SignAuthResponse(privateKey, 31337, challenge)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAuthResponseV2(legacy, id, 31337, challenge, transcript); err == nil {
		t.Fatal("legacy authentication response was accepted by the v2 verifier")
	}
}

func TestAuthPayloadV2MatchesJavaScriptCompatibilityVector(t *testing.T) {
	id, err := peer.Decode("12D3KooWQ3uxpHgjDKE6vGmvzKS8RPbxUDLwJ7XCLaD6YXdUfbR9")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := AuthPayloadV2(id, 31337, bytes.Repeat([]byte{0x41}, 32), AuthTranscript{
		SessionID: "0123456789abcdef0123456789abcdef",
		OfferSDP:  "v=0\r\na=fingerprint:sha-256 OFFER\r\n",
		AnswerSDP: "v=0\r\na=fingerprint:sha-256 ANSWER\r\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	const expected = "000000207032702d6e65746361742f6e61746976652d7765627274632d617574682f7632" +
		"000000010200000006636c69656e740000000673657276657200000034313244334b6f6f5751337578704867" +
		"6a444b453676476d767a4b53385250627855444c774a3758434c6144365958645566625239000000027a69" +
		"0000002030313233343536373839616263646566303132333435363738396162636465660000002041414141" +
		"41414141414141414141414141414141414141414141414141414141000000200f117532f93ee2cecff98bd8" +
		"2f00fd4aa2e0ba73241e0eadd9f6b5c3dec6d0e200000020105188d110c9603ce7cba3d3668933c4014cbdc5" +
		"ad0d8a8a00295b97b4fc060b"
	if hex.EncodeToString(payload) != expected {
		t.Fatalf("payload = %x", payload)
	}
	privateKey, _, err := crypto.GenerateEd25519Key(bytes.NewReader(bytes.Repeat([]byte{0x11}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	privateKeyBytes, err := crypto.MarshalPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	response, err := SignAuthResponseV2(privateKey, 31337, bytes.Repeat([]byte{0x41}, 32), AuthTranscript{
		SessionID: "0123456789abcdef0123456789abcdef",
		OfferSDP:  "v=0\r\na=fingerprint:sha-256 OFFER\r\n",
		AnswerSDP: "v=0\r\na=fingerprint:sha-256 ANSWER\r\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	generatedID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	const expectedPrivateKey = "080112401111111111111111111111111111111111111111111111111111111111111111" +
		"d04ab232742bb4ab3a1368bd4615e4e6d0224ab71a016baf8520a332c9778737"
	const expectedPeerID = "12D3KooWPqT2nMDSiXUSx5D7fasaxhxKigVhcqfkKqrLghCq9jxz"
	const expectedResponse = "020024004008011220d04ab232742bb4ab3a1368bd4615e4e6d0224ab71a016baf8520a332c9778737" +
		"cc75656ed3c03a6dfa36f74183414d2a463301f80f50e8e56382d207ae879cb20ade38f766f5c94a69c007835633a0a475cb7e1d82069a70483523ceb26c7f0f"
	if hex.EncodeToString(privateKeyBytes) != expectedPrivateKey {
		t.Fatalf("private key = %x", privateKeyBytes)
	}
	if generatedID.String() != expectedPeerID {
		t.Fatalf("PeerId = %s", generatedID)
	}
	if hex.EncodeToString(response) != expectedResponse {
		t.Fatalf("response = %x", response)
	}
}

func TestClientChallengeCarriesSignalingIdentity(t *testing.T) {
	const clientID = "0123456789AbCdEfGhIj"
	challenge, err := CreateClientChallenge(clientID)
	if err != nil {
		t.Fatal(err)
	}
	if got := ClientIDFromChallenge(challenge); got != clientID {
		t.Fatalf("client ID = %q", got)
	}
}

func TestEncryptedSignalingRoundTrip(t *testing.T) {
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	token, err := pairing.New(id, 31337, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	topic, err := SignalingRoomTopic(id.String()+":31337", token)
	if err != nil {
		t.Fatal(err)
	}
	outgoing, err := prepareOutgoing(Signal{
		Type: "offer", SessionID: "0123456789abcdef", SDP: "v=0\r\n",
	}, topic, "0123456789AbCdEfGhIj", token)
	if err != nil {
		t.Fatal(err)
	}
	if outgoing.SDP != "" || outgoing.Encrypted == "" {
		t.Fatalf("private signal leaked SDP: %+v", outgoing)
	}
	opened, ok := openIncoming(outgoing, topic, "JihGfEdCbA9876543210", token)
	if !ok || opened.SDP != "v=0\r\n" {
		t.Fatalf("opened=%+v ok=%v", opened, ok)
	}
	other, err := pairing.New(id, 31337, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := openIncoming(outgoing, topic, "JihGfEdCbA9876543210", other); ok {
		t.Fatal("another pairing secret decrypted the signal")
	}
}

func TestSignalingRejectsOversizedPayloads(t *testing.T) {
	base := Signal{
		Type: "offer", SessionID: "0123456789abcdef",
		SDP: strings.Repeat("s", maxSDPBytes+1),
	}
	if _, err := prepareOutgoing(base, "topic", "0123456789AbCdEfGhIj", nil); err == nil {
		t.Fatal("oversized outgoing SDP was accepted")
	}
	base.Version = SignalVersion
	base.Room = "topic"
	base.From = "0123456789AbCdEfGhIj"
	base.CreatedAt = time.Now().UnixMilli()
	if _, ok := openIncoming(base, "topic", "JihGfEdCbA9876543210", nil); ok {
		t.Fatal("oversized incoming SDP was accepted")
	}
	base.SDP = "v=0\r\n"
	base.Encrypted = strings.Repeat("e", maxEncryptedSignalBytes+1)
	if _, ok := openIncoming(base, "topic", "JihGfEdCbA9876543210", nil); ok {
		t.Fatal("oversized encrypted signal was accepted")
	}
}

func TestStreamDataEOFAndAbort(t *testing.T) {
	var sent []Frame
	var sentMu sync.Mutex
	stream := NewStream(func(frame Frame) error {
		sentMu.Lock()
		defer sentMu.Unlock()
		sent = append(sent, Frame{Type: frame.Type, Payload: append([]byte(nil), frame.Payload...)})
		return nil
	}, nil)
	defer stream.Close()
	if err := stream.Receive(Frame{Type: FrameData, Payload: []byte("hello")}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Receive(Frame{Type: FrameControl, Payload: []byte("eof")}); err != nil {
		t.Fatal(err)
	}
	value, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "hello" {
		t.Fatalf("read = %q", value)
	}
	if _, err := stream.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	var sawData, sawEOF bool
	sentMu.Lock()
	defer sentMu.Unlock()
	for _, frame := range sent {
		sawData = sawData || frame.Type == FrameData && string(frame.Payload) == "world"
		sawEOF = sawEOF || frame.Type == FrameControl && string(frame.Payload) == "eof"
	}
	if !sawData || !sawEOF {
		t.Fatalf("sent frames = %+v", sent)
	}
}

func TestStreamRejectsReceiveQueueOverflow(t *testing.T) {
	stream := NewStream(func(Frame) error { return nil }, nil)
	defer stream.Close()
	if err := stream.Receive(Frame{Type: FrameData, Payload: make([]byte, maxReadQueueBytes)}); err != nil {
		t.Fatal(err)
	}
	if err := stream.Receive(Frame{Type: FrameData, Payload: []byte{1}}); err == nil {
		t.Fatal("receive queue overflow was accepted")
	}
	if stream.readQueued > maxReadQueueBytes {
		t.Fatalf("queued bytes = %d, maximum = %d", stream.readQueued, maxReadQueueBytes)
	}
}

func TestListenerHandshakeLimits(t *testing.T) {
	listener := &Listener{peerHandshakes: make(map[string]int)}
	const firstPeer = "0123456789AbCdEfGhIj"
	if !listener.beginHandshake(firstPeer) || !listener.beginHandshake(firstPeer) {
		t.Fatal("per-peer handshake limit rejected an allowed handshake")
	}
	if listener.beginHandshake(firstPeer) {
		t.Fatal("per-peer handshake limit was not enforced")
	}
	for index := 0; index < maxConcurrentHandshakes-maxConcurrentHandshakesPeer; index++ {
		remote := fmt.Sprintf("%020d", index)
		if !listener.beginHandshake(remote) {
			t.Fatalf("global handshake slot %d was rejected", index)
		}
	}
	if listener.beginHandshake("Z123456789AbCdEfGhIj") {
		t.Fatal("global handshake limit was not enforced")
	}
	listener.endHandshake(firstPeer)
	if !listener.beginHandshake("Z123456789AbCdEfGhIj") {
		t.Fatal("released handshake slot was not reusable")
	}
}

func TestStreamCloseSendsGracefulEOF(t *testing.T) {
	sender, receiver, cleanup := linkedTestStreams()
	defer cleanup()

	if _, err := sender.Write([]byte("final output")); err != nil {
		t.Fatal(err)
	}
	if err := sender.Close(); err != nil {
		t.Fatal(err)
	}
	value, err := io.ReadAll(receiver)
	if err != nil {
		t.Fatalf("graceful Close produced a read error: %v", err)
	}
	if string(value) != "final output" {
		t.Fatalf("read = %q", value)
	}
}

func TestStreamResetSendsAbort(t *testing.T) {
	sender, receiver, cleanup := linkedTestStreams()
	defer cleanup()
	if err := sender.Reset(); err != nil {
		t.Fatal(err)
	}
	if _, err := receiver.Read(make([]byte, 1)); err == nil || errors.Is(err, io.EOF) {
		t.Fatalf("Reset produced %v, want a non-EOF abort error", err)
	}
}

func linkedTestStreams() (*Stream, *Stream, func()) {
	frames := make(chan Frame, 32)
	done := make(chan struct{})
	sender := NewStream(func(frame Frame) error {
		select {
		case frames <- frame:
			return nil
		case <-done:
			return io.ErrClosedPipe
		}
	}, nil)
	receiver := NewStream(func(Frame) error { return nil }, nil)
	go func() {
		for {
			select {
			case frame := <-frames:
				_ = receiver.Receive(frame)
			case <-done:
				return
			}
		}
	}()
	var once sync.Once
	return sender, receiver, func() {
		once.Do(func() {
			_ = sender.Close()
			_ = receiver.Close()
			close(done)
		})
	}
}

func TestPionPeersExchangeAuthenticatedFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, server, err := connectNativePeerPair(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	defer server.Close()
	if err := client.Send(Frame{Type: FrameData, Payload: []byte("ping")}); err != nil {
		t.Fatal(err)
	}
	select {
	case frame := <-server.frames:
		if frame.Type != FrameData || string(frame.Payload) != "ping" {
			t.Fatalf("frame = %+v", frame)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestPionAuthenticationProofCannotMoveBetweenPeerConnections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client1, server1, transcript1, err := connectNativePeerPairWithTranscript(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client1.Close()
	defer server1.Close()
	client2, server2, transcript2, err := connectNativePeerPairWithTranscript(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	defer server2.Close()
	if transcript1.OfferSDP == transcript2.OfferSDP || transcript1.AnswerSDP == transcript2.AnswerSDP {
		t.Fatal("independent PeerConnections unexpectedly produced identical SDP")
	}
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	challenge := bytes.Repeat([]byte{0x7c}, 32)
	response, err := SignAuthResponseV2(privateKey, 31337, challenge, transcript1)
	if err != nil {
		t.Fatal(err)
	}
	valid, err := VerifyAuthResponseV2(response, id, 31337, challenge, transcript2)
	if err != nil {
		t.Fatal(err)
	}
	if valid {
		t.Fatal("authentication proof from the first PeerConnection was accepted on the second")
	}
}

func connectNativePeerPair(ctx context.Context) (*nativePeer, *nativePeer, error) {
	client, server, _, err := connectNativePeerPairWithTranscript(ctx)
	return client, server, err
}

func connectNativePeerPairWithTranscript(ctx context.Context) (*nativePeer, *nativePeer, AuthTranscript, error) {
	client, err := newNativePeer(true, []string{})
	if err != nil {
		return nil, nil, AuthTranscript{}, err
	}
	server, err := newNativePeer(false, []string{})
	if err != nil {
		_ = client.Close()
		return nil, nil, AuthTranscript{}, err
	}
	sessionID, err := CreateSignalingSessionID()
	if err != nil {
		_ = client.Close()
		_ = server.Close()
		return nil, nil, AuthTranscript{}, err
	}
	offerChannel := make(chan Signal, 1)
	answerChannel := make(chan Signal, 1)
	go func() {
		_ = client.startOffer(ctx, func(signal Signal) error {
			offerChannel <- signal
			return nil
		}, sessionID)
	}()
	var offer Signal
	select {
	case offer = <-offerChannel:
	case <-ctx.Done():
		return nil, nil, AuthTranscript{}, ctx.Err()
	}
	go func() {
		_ = server.acceptOffer(ctx, offer, func(signal Signal) error {
			answerChannel <- signal
			return nil
		})
	}()
	var answer Signal
	select {
	case answer = <-answerChannel:
		if err := client.acceptAnswer(answer); err != nil {
			return nil, nil, AuthTranscript{}, err
		}
	case <-ctx.Done():
		return nil, nil, AuthTranscript{}, ctx.Err()
	}
	select {
	case <-client.open:
	case <-ctx.Done():
		return nil, nil, AuthTranscript{}, ctx.Err()
	}
	select {
	case <-server.open:
	case <-ctx.Done():
		return nil, nil, AuthTranscript{}, ctx.Err()
	}
	return client, server, AuthTranscript{
		SessionID: sessionID,
		OfferSDP:  offer.SDP,
		AnswerSDP: answer.SDP,
	}, nil
}

type memorySignalHub struct {
	mu       sync.Mutex
	sessions map[string]*memorySignalSession
}

type memorySignalSession struct {
	hub    *memorySignalHub
	id     string
	events chan Signal
}

func newMemorySignalPair() (*memorySignalSession, *memorySignalSession) {
	hub := &memorySignalHub{sessions: make(map[string]*memorySignalSession)}
	client := &memorySignalSession{
		hub: hub, id: "Client0123456789abcd", events: make(chan Signal, 32),
	}
	server := &memorySignalSession{
		hub: hub, id: "Server0123456789abcd", events: make(chan Signal, 32),
	}
	hub.sessions[client.id] = client
	hub.sessions[server.id] = server
	return client, server
}

func (s *memorySignalSession) Name() string          { return "memory" }
func (s *memorySignalSession) PeerID() string        { return s.id }
func (s *memorySignalSession) Events() <-chan Signal { return s.events }
func (s *memorySignalSession) Status() (int, int)    { return 1, 1 }
func (s *memorySignalSession) Close() error          { return nil }
func (s *memorySignalSession) Publish(ctx context.Context, signal Signal) error {
	signal.Version = SignalVersion
	signal.Room = "memory"
	signal.From = s.id
	signal.CreatedAt = time.Now().UnixMilli()
	s.hub.mu.Lock()
	destinations := make([]*memorySignalSession, 0, len(s.hub.sessions))
	for id, destination := range s.hub.sessions {
		if id != s.id && (signal.To == "" || signal.To == id) {
			destinations = append(destinations, destination)
		}
	}
	s.hub.mu.Unlock()
	for _, destination := range destinations {
		select {
		case destination.events <- signal:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func TestCompleteNativeEndpointHandshakeAndData(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	previousICEServers := DefaultICEServers
	DefaultICEServers = []string{}
	defer func() { DefaultICEServers = previousICEServers }()
	privateKey, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	clientSignal, serverSignal := newMemorySignalPair()
	serverStreamChannel := make(chan *Stream, 1)
	go func() {
		for {
			select {
			case offer := <-serverSignal.Events():
				if offer.Type == "offer" {
					p2p, answerErr := answerNativeOffer(
						ctx, serverSignal, privateKey, 31337, []string{}, offer,
					)
					if answerErr != nil {
						return
					}
					stream := NewStream(p2p.Send, p2p.Close)
					go func() { _ = forwardFrames(p2p, stream) }()
					serverStreamChannel <- stream
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	clientConnection, err := connectWithSession(ctx, clientSignal, id, 31337, []string{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientConnection.Close()
	var serverStream *Stream
	select {
	case serverStream = <-serverStreamChannel:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defer serverStream.Close()
	if _, err := clientConnection.Stream.Write([]byte("client-to-server")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("client-to-server"))
	if _, err := io.ReadFull(serverStream, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "client-to-server" {
		t.Fatalf("server received %q", buffer)
	}
	if _, err := serverStream.Write([]byte("server-to-client")); err != nil {
		t.Fatal(err)
	}
	buffer = make([]byte, len("server-to-client"))
	if _, err := io.ReadFull(clientConnection.Stream, buffer); err != nil {
		t.Fatal(err)
	}
	if string(buffer) != "server-to-client" {
		t.Fatalf("client received %q", buffer)
	}
	for _, payload := range [][]byte{
		[]byte("wireguard-handshake"),
		[]byte("wireguard-transport-data"),
		bytes.Repeat([]byte{0xa5}, datagram.MaxPayloadLength),
	} {
		if err := datagram.Write(clientConnection.Stream, payload); err != nil {
			t.Fatal(err)
		}
		received, err := datagram.Read(serverStream)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(received, payload) {
			t.Fatalf("server received UDP payload %q, want %q", received, payload)
		}
		if err := datagram.Write(serverStream, received); err != nil {
			t.Fatal(err)
		}
		reply, err := datagram.Read(clientConnection.Stream)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(reply, payload) {
			t.Fatalf("client received UDP payload %q, want %q", reply, payload)
		}
	}
	if err := serverStream.Close(); err != nil {
		t.Fatal(err)
	}
	readDone := make(chan error, 1)
	go func() {
		_, err := io.ReadAll(clientConnection.Stream)
		readDone <- err
	}()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("native Close did not deliver a graceful EOF: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("native Close did not deliver EOF")
	}
}

func TestCopyICEServersCreatesIndependentNonNilSnapshot(t *testing.T) {
	original := []string{"stun:example.test:3478"}
	snapshot := copyICEServers(original)
	original[0] = "stun:changed.test:3478"
	if snapshot[0] != "stun:example.test:3478" {
		t.Fatalf("ICE server snapshot changed to %q", snapshot[0])
	}
	if empty := copyICEServers(nil); empty == nil {
		t.Fatal("empty ICE server snapshot must be non-nil")
	}
}

func TestListenerReconnectsTheSameLogicalStream(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	listenerCtx, listenerCancel := context.WithCancel(ctx)
	accepted := make(chan *Stream, 2)
	listener := &Listener{
		cancel:  listenerCancel,
		streams: make(map[string]*listenerStream),
		onStream: func(stream *Stream, _ string) {
			accepted <- stream
		},
	}
	client1, server1, err := connectNativePeerPair(listenerCtx)
	if err != nil {
		t.Fatal(err)
	}
	listener.activate("remote-peer", server1)
	var logical *Stream
	select {
	case logical = <-accepted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := client1.Send(Frame{Type: FrameData, Payload: []byte("before")}); err != nil {
		t.Fatal(err)
	}
	before := make([]byte, 6)
	if _, err := io.ReadFull(logical, before); err != nil || string(before) != "before" {
		t.Fatalf("before=%q err=%v", before, err)
	}
	_ = client1.Close()
	_ = server1.Close()
	deadline := time.Now().Add(2 * time.Second)
	for {
		listener.mu.Lock()
		detached := listener.streams["remote-peer"] != nil &&
			listener.streams["remote-peer"].peer == nil
		listener.mu.Unlock()
		if detached {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not enter reconnecting state")
		}
		time.Sleep(10 * time.Millisecond)
	}
	client2, server2, err := connectNativePeerPair(listenerCtx)
	if err != nil {
		t.Fatal(err)
	}
	defer client2.Close()
	listener.activate("remote-peer", server2)
	select {
	case extra := <-accepted:
		t.Fatalf("reconnection created a second logical stream: %p", extra)
	case <-time.After(100 * time.Millisecond):
	}
	if err := client2.Send(Frame{Type: FrameData, Payload: []byte("after")}); err != nil {
		t.Fatal(err)
	}
	after := make([]byte, 5)
	if _, err := io.ReadFull(logical, after); err != nil || string(after) != "after" {
		t.Fatalf("after=%q err=%v", after, err)
	}
	if listener.streams["remote-peer"].stream != logical {
		t.Fatal("logical stream identity changed across reconnection")
	}
	_ = listener.Close()
}
