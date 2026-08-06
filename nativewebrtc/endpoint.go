package nativewebrtc

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pion/webrtc/v4"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

var DefaultICEServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
	"stun:stun2.l.google.com:19302",
	"stun:stun3.l.google.com:19302",
	"stun:stun4.l.google.com:19302",
	"stun:stun.counterpath.com:3478",
	"stun:stun.sipgate.net:3478",
	"stun:stun.voipbuster.com:3478",
	"stun:stun.internetcalls.com:3478",
}

const (
	nativeFrameQueueDepth       = 128
	maxConcurrentHandshakes     = 32
	maxConcurrentHandshakesPeer = 2
)

type EndpointOptions struct {
	ICEServers  []string
	NostrURLs   []string
	TorrentURLs []string
	Token       *pairing.Token
}

type nativePeer struct {
	connection *webrtc.PeerConnection
	channel    *webrtc.DataChannel
	channelMu  sync.Mutex
	frames     chan Frame
	open       chan struct{}
	closed     chan struct{}
	openOnce   sync.Once
	closeOnce  sync.Once
	sendMu     sync.Mutex
}

func newNativePeer(initiator bool, iceServers []string) (*nativePeer, error) {
	if iceServers == nil {
		iceServers = DefaultICEServers
	}
	connection, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{{URLs: append([]string(nil), iceServers...)}},
	})
	if err != nil {
		return nil, err
	}
	value := &nativePeer{
		connection: connection, frames: make(chan Frame, nativeFrameQueueDepth),
		open: make(chan struct{}), closed: make(chan struct{}),
	}
	connection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		switch state {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			_ = value.Close()
		case webrtc.PeerConnectionStateDisconnected:
			go func() {
				timer := time.NewTimer(8 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
					if value.connection.ConnectionState() == webrtc.PeerConnectionStateDisconnected {
						_ = value.Close()
					}
				case <-value.closed:
				}
			}()
		}
	})
	if initiator {
		channel, err := connection.CreateDataChannel(DataChannelLabel, &webrtc.DataChannelInit{
			Ordered: boolPointer(true),
		})
		if err != nil {
			_ = connection.Close()
			return nil, err
		}
		value.attach(channel)
	} else {
		connection.OnDataChannel(func(channel *webrtc.DataChannel) {
			if channel.Label() != DataChannelLabel {
				_ = channel.Close()
				return
			}
			value.attach(channel)
		})
	}
	return value, nil
}

func (p *nativePeer) attach(channel *webrtc.DataChannel) {
	p.channelMu.Lock()
	select {
	case <-p.closed:
		p.channelMu.Unlock()
		return
	default:
	}
	if p.channel != nil && p.channel != channel {
		p.channelMu.Unlock()
		_ = channel.Close()
		return
	}
	p.channel = channel
	p.channelMu.Unlock()
	channel.OnOpen(func() { p.openOnce.Do(func() { close(p.open) }) })
	channel.OnMessage(func(message webrtc.DataChannelMessage) {
		frame, err := DecodeFrame(message.Data)
		if err != nil {
			_ = p.Close()
			return
		}
		select {
		case p.frames <- frame:
		case <-p.closed:
		default:
			_ = p.Close()
		}
	})
	channel.OnClose(func() { _ = p.Close() })
	channel.OnError(func(error) { _ = p.Close() })
}

func (p *nativePeer) startOffer(ctx context.Context, publish func(Signal) error, sessionID string) error {
	offer, err := p.connection.CreateOffer(nil)
	if err != nil {
		return err
	}
	gathering := webrtc.GatheringCompletePromise(p.connection)
	if err := p.connection.SetLocalDescription(offer); err != nil {
		return err
	}
	select {
	case <-gathering:
	case <-ctx.Done():
		return ctx.Err()
	}
	return publish(Signal{Type: "offer", SessionID: sessionID, SDP: p.connection.LocalDescription().SDP})
}

func (p *nativePeer) acceptOffer(ctx context.Context, offer Signal, publish func(Signal) error) error {
	if err := p.connection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer, SDP: offer.SDP,
	}); err != nil {
		return err
	}
	answer, err := p.connection.CreateAnswer(nil)
	if err != nil {
		return err
	}
	gathering := webrtc.GatheringCompletePromise(p.connection)
	if err := p.connection.SetLocalDescription(answer); err != nil {
		return err
	}
	select {
	case <-gathering:
	case <-ctx.Done():
		return ctx.Err()
	}
	return publish(Signal{
		Type: "answer", SessionID: offer.SessionID, To: offer.From,
		SDP: p.connection.LocalDescription().SDP,
	})
}

func (p *nativePeer) acceptAnswer(answer Signal) error {
	return p.connection.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer, SDP: answer.SDP,
	})
}

func (p *nativePeer) Send(frame Frame) error {
	select {
	case <-p.open:
	case <-p.closed:
		return io.ErrClosedPipe
	}
	p.sendMu.Lock()
	defer p.sendMu.Unlock()
	p.channelMu.Lock()
	channel := p.channel
	p.channelMu.Unlock()
	if channel == nil {
		return io.ErrClosedPipe
	}
	return channel.Send(EncodeFrame(frame.Type, frame.Payload))
}

func (p *nativePeer) Close() error {
	var result error
	p.closeOnce.Do(func() {
		close(p.closed)
		p.channelMu.Lock()
		p.channel = nil
		p.channelMu.Unlock()
		result = p.connection.Close()
	})
	return result
}

type Connection struct {
	Stream      *Stream
	Signaling   string
	close       func() error
	interrupt   func() error
	reconnected <-chan struct{}
}

type peerLink struct {
	mu     sync.Mutex
	cond   *sync.Cond
	peer   *nativePeer
	closed bool
}

func newPeerLink(peer *nativePeer) *peerLink {
	link := &peerLink{peer: peer}
	link.cond = sync.NewCond(&link.mu)
	return link
}

func (l *peerLink) Send(frame Frame) error {
	l.mu.Lock()
	for l.peer == nil && !l.closed {
		l.cond.Wait()
	}
	if l.closed {
		l.mu.Unlock()
		return io.ErrClosedPipe
	}
	peer := l.peer
	l.mu.Unlock()
	return peer.Send(frame)
}

func (l *peerLink) Attach(peer *nativePeer) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed || l.peer != nil {
		return false
	}
	l.peer = peer
	l.cond.Broadcast()
	return true
}

func (l *peerLink) Detach(peer *nativePeer) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.peer != peer {
		return false
	}
	l.peer = nil
	return true
}

func (l *peerLink) Close() error {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil
	}
	l.closed = true
	peer := l.peer
	l.peer = nil
	l.cond.Broadcast()
	l.mu.Unlock()
	if peer != nil {
		return peer.Close()
	}
	return nil
}

func (l *peerLink) Interrupt() error {
	l.mu.Lock()
	peer := l.peer
	l.mu.Unlock()
	if peer == nil {
		return errors.New("native WebRTC transport is already reconnecting")
	}
	return peer.Close()
}

func (c *Connection) Close() error {
	if c == nil || c.close == nil {
		return nil
	}
	return c.close()
}

// Reconnect closes only the active PeerConnection and waits until the endpoint
// has rebound a replacement transport to the same logical Stream. It is useful
// for network-change handling and deterministic recovery tests.
func (c *Connection) Reconnect(ctx context.Context) error {
	if c == nil || c.reconnected == nil {
		return errors.New("native WebRTC connection does not expose reconnect state")
	}
	for {
		select {
		case <-c.reconnected:
			continue
		default:
			goto drained
		}
	}

drained:
	if c.interrupt == nil {
		return errors.New("native WebRTC connection cannot be interrupted")
	}
	if err := c.interrupt(); err != nil {
		return err
	}
	select {
	case <-c.reconnected:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Connect establishes the JavaScript-compatible native WebRTC data channel
// through both Nostr and WebTorrent signaling and returns the first winner.
func Connect(
	ctx context.Context,
	expected peer.ID,
	service uint16,
	token *pairing.Token,
	timeout time.Duration,
	nos []string,
	torrents []string,
) (*Connection, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	room, err := RoomID(expected, service)
	if err != nil {
		return nil, err
	}
	peerID, err := CreateSignalingPeerID()
	if err != nil {
		return nil, err
	}
	sessions, err := createSessions(ctx, room, peerID, token, nos, torrents)
	if err != nil {
		return nil, err
	}
	return ConnectWithSignalingSessions(
		ctx, expected, service, timeout, copyICEServers(DefaultICEServers), sessions,
	)
}

// ConnectWithSignalingSessions establishes a native WebRTC connection through
// caller-provided signaling adapters. The returned connection owns all
// sessions and closes the losing adapters after the first successful race.
func ConnectWithSignalingSessions(
	ctx context.Context,
	expected peer.ID,
	service uint16,
	timeout time.Duration,
	iceServers []string,
	sessions []SignalingSession,
) (*Connection, error) {
	if expected == "" {
		return nil, errors.New("expected native WebRTC PeerId is required")
	}
	if service == 0 {
		return nil, errors.New("logical port must be between 1 and 65535")
	}
	if len(sessions) == 0 {
		return nil, errors.New("at least one native WebRTC signaling session is required")
	}
	for _, session := range sessions {
		if session == nil {
			return nil, errors.New("native WebRTC signaling session cannot be nil")
		}
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if iceServers != nil {
		iceServers = copyICEServers(iceServers)
	}
	attemptCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result := make(chan *Connection, 1)
	errorsChannel := make(chan error, len(sessions))
	var winnerOnce sync.Once
	for _, signaling := range sessions {
		go func(session SignalingSession) {
			connection, attemptErr := connectWithSession(
				attemptCtx, session, expected, service, iceServers,
			)
			if attemptErr != nil {
				errorsChannel <- fmt.Errorf("%s: %w", session.Name(), attemptErr)
				return
			}
			won := false
			winnerOnce.Do(func() {
				won = true
				result <- connection
			})
			if !won {
				_ = connection.Close()
			}
		}(signaling)
	}
	var failures []error
	for len(failures) < len(sessions) {
		select {
		case winner := <-result:
			for _, signaling := range sessions {
				if signaling.Name() != winner.Signaling {
					_ = signaling.Close()
				}
			}
			return winner, nil
		case failure := <-errorsChannel:
			failures = append(failures, failure)
		case <-attemptCtx.Done():
			for _, signaling := range sessions {
				_ = signaling.Close()
			}
			return nil, fmt.Errorf("native WebRTC did not establish a connection: %w", attemptCtx.Err())
		}
	}
	for _, signaling := range sessions {
		_ = signaling.Close()
	}
	return nil, fmt.Errorf("native WebRTC signaling failed: %w", errors.Join(failures...))
}

func connectWithSession(
	ctx context.Context,
	signaling SignalingSession,
	expected peer.ID,
	service uint16,
	iceServers []string,
) (*Connection, error) {
	p2p, err := dialAuthenticatedPeer(ctx, signaling, expected, service, iceServers)
	if err != nil {
		return nil, err
	}
	connectionCtx, cancel := context.WithCancel(context.Background())
	link := newPeerLink(p2p)
	reconnected := make(chan struct{}, 1)
	var closeOnce sync.Once
	stream := NewStream(link.Send, func() error {
		var result error
		closeOnce.Do(func() {
			cancel()
			result = errors.Join(link.Close(), signaling.Close())
		})
		return result
	})
	go superviseClientConnection(
		connectionCtx, signaling, expected, service, iceServers,
		link, stream, p2p, reconnected,
	)
	return &Connection{
		Stream: stream, Signaling: signaling.Name(), close: stream.Close,
		interrupt: link.Interrupt, reconnected: reconnected,
	}, nil
}

func dialAuthenticatedPeer(
	ctx context.Context,
	signaling SignalingSession,
	expected peer.ID,
	service uint16,
	iceServers []string,
) (*nativePeer, error) {
	sessionID, err := CreateSignalingSessionID()
	if err != nil {
		return nil, err
	}
	p2p, err := newNativePeer(true, iceServers)
	if err != nil {
		return nil, err
	}
	closePeer := func() error {
		_ = signaling.Publish(context.Background(), Signal{Type: "bye", SessionID: sessionID})
		return p2p.Close()
	}
	var offerSDP string
	var transcript AuthTranscript
	if err := p2p.startOffer(ctx, func(signal Signal) error {
		offerSDP = signal.SDP
		return signaling.Publish(ctx, signal)
	}, sessionID); err != nil {
		_ = closePeer()
		return nil, err
	}
	for {
		select {
		case signal, ok := <-signaling.Events():
			if !ok {
				_ = closePeer()
				return nil, errors.New("native WebRTC signaling session closed")
			}
			if signal.SessionID != sessionID || signal.To != signaling.PeerID() {
				continue
			}
			if signal.Type == "answer" {
				answerSDP := signal.SDP
				if err := p2p.acceptAnswer(signal); err != nil {
					_ = closePeer()
					return nil, err
				}
				transcript = AuthTranscript{
					SessionID: sessionID,
					OfferSDP:  offerSDP,
					AnswerSDP: answerSDP,
				}
				goto connected
			}
		case <-ctx.Done():
			_ = closePeer()
			return nil, ctx.Err()
		}
	}

connected:
	select {
	case <-p2p.open:
	case <-p2p.closed:
		_ = closePeer()
		return nil, errors.New("native WebRTC data channel closed before authentication")
	case <-ctx.Done():
		_ = closePeer()
		return nil, ctx.Err()
	}
	challenge := make([]byte, 32)
	if _, err := rand.Read(challenge); err != nil {
		_ = closePeer()
		return nil, err
	}
	if err := p2p.Send(Frame{Type: FrameAuthChallenge, Payload: challenge}); err != nil {
		_ = closePeer()
		return nil, err
	}
	for {
		select {
		case frame := <-p2p.frames:
			if frame.Type != FrameAuthResponse {
				_ = closePeer()
				return nil, fmt.Errorf("expected a WebRTC auth response, received frame %d", frame.Type)
			}
			ok, err := VerifyAuthResponseV2(frame.Payload, expected, service, challenge, transcript)
			if err != nil || !ok {
				_ = closePeer()
				if err == nil {
					err = errors.New("native WebRTC PeerId authentication failed")
				}
				return nil, err
			}
			if err := p2p.Send(Frame{Type: FrameAuthReady}); err != nil {
				_ = closePeer()
				return nil, err
			}
			return p2p, nil
		case <-ctx.Done():
			_ = closePeer()
			return nil, ctx.Err()
		}
	}
}

func forwardFrames(peer *nativePeer, stream *Stream) error {
	for {
		select {
		case frame := <-peer.frames:
			if err := stream.Receive(frame); err != nil {
				return err
			}
		case <-peer.closed:
			return errors.New("native WebRTC peer disconnected")
		}
	}
}

func superviseClientConnection(
	ctx context.Context,
	signaling SignalingSession,
	expected peer.ID,
	service uint16,
	iceServers []string,
	link *peerLink,
	stream *Stream,
	current *nativePeer,
	reconnected chan<- struct{},
) {
	for {
		_ = forwardFrames(current, stream)
		if ctx.Err() != nil {
			return
		}
		link.Detach(current)
		reconnectCtx, cancel := context.WithTimeout(ctx, ReconnectGrace)
		var next *nativePeer
		for reconnectCtx.Err() == nil {
			attemptCtx, attemptCancel := context.WithTimeout(reconnectCtx, 20*time.Second)
			candidate, err := dialAuthenticatedPeer(attemptCtx, signaling, expected, service, iceServers)
			attemptCancel()
			if err == nil {
				next = candidate
				break
			}
			if !waitContext(reconnectCtx, 750*time.Millisecond) {
				break
			}
		}
		cancel()
		if next == nil || !link.Attach(next) {
			stream.fail(errors.New("native WebRTC peer did not reconnect within 120 seconds"), true)
			_ = link.Close()
			return
		}
		if err := stream.Reconnected(); err != nil {
			stream.fail(err, true)
			_ = link.Close()
			return
		}
		select {
		case reconnected <- struct{}{}:
		default:
		}
		current = next
	}
}

func copyICEServers(iceServers []string) []string {
	result := make([]string, len(iceServers))
	copy(result, iceServers)
	return result
}

func boolPointer(value bool) *bool { return &value }

func createSessions(
	ctx context.Context,
	room, peerID string,
	token *pairing.Token,
	nostrURLs, torrentURLs []string,
) ([]SignalingSession, error) {
	nostrSession, nostrErr := NewNostrSession(ctx, room, peerID, nostrURLs, token)
	torrentSession, torrentErr := NewTorrentSession(ctx, room, peerID, torrentURLs, token)
	result := make([]SignalingSession, 0, 2)
	if nostrErr == nil {
		result = append(result, nostrSession)
	}
	if torrentErr == nil {
		result = append(result, torrentSession)
	}
	if len(result) == 0 {
		return nil, errors.Join(nostrErr, torrentErr)
	}
	return result, nil
}

type Listener struct {
	cancel   context.CancelFunc
	sessions []SignalingSession
	wg       sync.WaitGroup
	once     sync.Once
	mu       sync.Mutex
	closed   bool
	streams  map[string]*listenerStream
	onStream func(*Stream, string)

	handshakeMu    sync.Mutex
	handshakes     int
	peerHandshakes map[string]int
}

type listenerStream struct {
	stream *Stream
	link   *peerLink
	peer   *nativePeer
	timer  *time.Timer
}

// StartListener accepts JavaScript/browser compatible native WebRTC channels.
func StartListener(
	parent context.Context,
	privateKey crypto.PrivKey,
	service uint16,
	token *pairing.Token,
	nostrURLs, torrentURLs []string,
	onStream func(*Stream, string),
) (*Listener, error) {
	if privateKey == nil {
		return nil, errors.New("libp2p private key is required")
	}
	id, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	room, err := RoomID(id, service)
	if err != nil {
		return nil, err
	}
	signalingPeerID, err := CreateSignalingPeerID()
	if err != nil {
		return nil, err
	}
	sessions, err := createSessions(parent, room, signalingPeerID, token, nostrURLs, torrentURLs)
	if err != nil {
		return nil, err
	}
	listener, err := StartListenerWithSignalingSessions(
		parent, privateKey, service, nil, sessions, onStream,
	)
	if err != nil {
		for _, session := range sessions {
			_ = session.Close()
		}
	}
	return listener, err
}

// StartListenerWithSignalingSessions starts a native WebRTC listener with
// caller-provided signaling adapters. The listener owns and closes every
// session after this function succeeds.
func StartListenerWithSignalingSessions(
	parent context.Context,
	privateKey crypto.PrivKey,
	service uint16,
	iceServers []string,
	sessions []SignalingSession,
	onStream func(*Stream, string),
) (*Listener, error) {
	if privateKey == nil {
		return nil, errors.New("libp2p private key is required")
	}
	if _, err := peer.IDFromPrivateKey(privateKey); err != nil {
		return nil, err
	}
	if service == 0 {
		return nil, errors.New("logical port must be between 1 and 65535")
	}
	if len(sessions) == 0 {
		return nil, errors.New("at least one native WebRTC signaling session is required")
	}
	for _, session := range sessions {
		if session == nil {
			return nil, errors.New("native WebRTC signaling session cannot be nil")
		}
	}
	if iceServers != nil {
		iceServers = copyICEServers(iceServers)
	}
	ctx, cancel := context.WithCancel(parent)
	listener := &Listener{
		cancel: cancel, sessions: sessions,
		streams: make(map[string]*listenerStream), onStream: onStream,
		peerHandshakes: make(map[string]int),
	}
	for _, session := range sessions {
		listener.wg.Add(1)
		go func(signaling SignalingSession) {
			defer listener.wg.Done()
			for {
				select {
				case signal, ok := <-signaling.Events():
					if !ok {
						return
					}
					if signal.Type == "offer" {
						if !listener.beginHandshake(signal.From) {
							continue
						}
						listener.wg.Add(1)
						go func(signal Signal) {
							defer listener.wg.Done()
							defer listener.endHandshake(signal.From)
							p2p, acceptErr := answerNativeOffer(
								ctx, signaling, privateKey, service, iceServers, signal,
							)
							if acceptErr == nil {
								listener.activate(signal.From, p2p)
							}
						}(signal)
					}
				case <-ctx.Done():
					return
				}
			}
		}(session)
	}
	return listener, nil
}

func (l *Listener) beginHandshake(remote string) bool {
	if !clientIDPattern.MatchString(remote) {
		return false
	}
	l.handshakeMu.Lock()
	defer l.handshakeMu.Unlock()
	if l.handshakes >= maxConcurrentHandshakes || l.peerHandshakes[remote] >= maxConcurrentHandshakesPeer {
		return false
	}
	l.handshakes++
	l.peerHandshakes[remote]++
	return true
}

func (l *Listener) endHandshake(remote string) {
	l.handshakeMu.Lock()
	l.handshakes--
	l.peerHandshakes[remote]--
	if l.peerHandshakes[remote] == 0 {
		delete(l.peerHandshakes, remote)
	}
	l.handshakeMu.Unlock()
}

func (l *Listener) Close() error {
	var result error
	l.once.Do(func() {
		l.cancel()
		for _, session := range l.sessions {
			result = errors.Join(result, session.Close())
		}
		l.mu.Lock()
		l.closed = true
		for remote, entry := range l.streams {
			if entry.timer != nil {
				entry.timer.Stop()
			}
			entry.stream.fail(context.Canceled, true)
			result = errors.Join(result, entry.link.Close())
			delete(l.streams, remote)
		}
		l.mu.Unlock()
		l.wg.Wait()
	})
	return result
}

func answerNativeOffer(
	ctx context.Context,
	signaling SignalingSession,
	privateKey crypto.PrivKey,
	service uint16,
	iceServers []string,
	offer Signal,
) (*nativePeer, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	p2p, err := newNativePeer(false, iceServers)
	if err != nil {
		return nil, err
	}
	closePeer := func() error {
		_ = signaling.Publish(context.Background(), Signal{
			Type: "bye", SessionID: offer.SessionID, To: offer.From,
		})
		return p2p.Close()
	}
	var answerSDP string
	if err := p2p.acceptOffer(attemptCtx, offer, func(signal Signal) error {
		answerSDP = signal.SDP
		return signaling.Publish(attemptCtx, signal)
	}); err != nil {
		_ = closePeer()
		return nil, err
	}
	var challengeAnswered bool
	transcript := AuthTranscript{
		SessionID: offer.SessionID,
		OfferSDP:  offer.SDP,
		AnswerSDP: answerSDP,
	}
	for {
		select {
		case frame := <-p2p.frames:
			switch frame.Type {
			case FrameAuthChallenge:
				response, signErr := SignAuthResponseV2(privateKey, service, frame.Payload, transcript)
				if signErr != nil || p2p.Send(Frame{Type: FrameAuthResponse, Payload: response}) != nil {
					_ = closePeer()
					return nil, errors.New("send native WebRTC auth response")
				}
				challengeAnswered = true
			case FrameAuthReady:
				if !challengeAnswered {
					_ = closePeer()
					return nil, errors.New("native WebRTC auth ready was received before the challenge")
				}
				return p2p, nil
			default:
				_ = closePeer()
				return nil, fmt.Errorf("unexpected native WebRTC auth frame: %d", frame.Type)
			}
		case <-attemptCtx.Done():
			_ = closePeer()
			return nil, attemptCtx.Err()
		case <-p2p.closed:
			return nil, errors.New("native WebRTC peer closed during authentication")
		}
	}
}

func (l *Listener) activate(remote string, p2p *nativePeer) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = p2p.Close()
		return
	}
	entry := l.streams[remote]
	isNew := entry == nil
	if isNew {
		link := newPeerLink(p2p)
		var stream *Stream
		stream = NewStream(link.Send, func() error {
			l.mu.Lock()
			current := l.streams[remote]
			if current != nil && current.stream == stream {
				if current.timer != nil {
					current.timer.Stop()
				}
				delete(l.streams, remote)
			}
			l.mu.Unlock()
			return link.Close()
		})
		entry = &listenerStream{stream: stream, link: link, peer: p2p}
		l.streams[remote] = entry
	} else {
		if entry.peer != nil || !entry.link.Attach(p2p) {
			l.mu.Unlock()
			_ = p2p.Close()
			return
		}
		if entry.timer != nil {
			entry.timer.Stop()
			entry.timer = nil
		}
		entry.peer = p2p
	}
	stream := entry.stream
	l.mu.Unlock()

	if !isNew {
		if err := stream.Reconnected(); err != nil {
			_ = stream.Reset()
			return
		}
	}
	go l.watch(remote, entry, p2p)
	if isNew && l.onStream != nil {
		l.onStream(stream, remote)
	}
}

func (l *Listener) watch(remote string, entry *listenerStream, p2p *nativePeer) {
	_ = forwardFrames(p2p, entry.stream)
	l.mu.Lock()
	defer l.mu.Unlock()
	current := l.streams[remote]
	if current != entry || entry.peer != p2p {
		return
	}
	entry.link.Detach(p2p)
	entry.peer = nil
	entry.timer = time.AfterFunc(ReconnectGrace, func() {
		l.mu.Lock()
		current := l.streams[remote]
		if current == entry && entry.peer == nil {
			delete(l.streams, remote)
			entry.stream.fail(errors.New("native WebRTC peer did not reconnect within 120 seconds"), true)
			_ = entry.link.Close()
		}
		l.mu.Unlock()
	})
}
