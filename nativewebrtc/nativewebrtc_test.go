package nativewebrtc

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
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

func connectNativePeerPair(ctx context.Context) (*nativePeer, *nativePeer, error) {
	client, err := newNativePeer(true, []string{})
	if err != nil {
		return nil, nil, err
	}
	server, err := newNativePeer(false, []string{})
	if err != nil {
		_ = client.Close()
		return nil, nil, err
	}
	sessionID := "0123456789abcdef"
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
		return nil, nil, ctx.Err()
	}
	go func() {
		_ = server.acceptOffer(ctx, offer, func(signal Signal) error {
			answerChannel <- signal
			return nil
		})
	}()
	select {
	case answer := <-answerChannel:
		if err := client.acceptAnswer(answer); err != nil {
			return nil, nil, err
		}
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	select {
	case <-client.open:
	case <-ctx.Done():
		return nil, nil, ctx.Err()
	}
	return client, server, nil
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
					p2p, answerErr := answerNativeOffer(ctx, serverSignal, privateKey, 31337, offer)
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
	clientConnection, err := connectWithSession(ctx, clientSignal, id, 31337)
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
