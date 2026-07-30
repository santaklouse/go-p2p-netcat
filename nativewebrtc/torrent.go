package nativewebrtc

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const encryptedTrackerPrefix = "pnc-signal-v1:"

type torrentSocket struct {
	url    string
	mu     sync.Mutex
	socket *websocket.Conn
}

func (s *torrentSocket) write(value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.socket == nil {
		return errors.New("tracker WebSocket не подключён")
	}
	return s.socket.WriteJSON(value)
}

type TorrentSession struct {
	ctx    context.Context
	cancel context.CancelFunc
	urls   []string
	topic  string
	hash   string
	peerID string
	token  *pairing.Token
	events chan Signal

	mu           sync.Mutex
	sockets      map[string]*torrentSocket
	offers       map[string]Signal
	offerOrigins map[string]string
	seen         map[string]time.Time
	open         atomic.Int32
	closeMu      sync.Once
}

func NewTorrentSession(
	parent context.Context,
	roomID, peerID string,
	urls []string,
	token *pairing.Token,
) (*TorrentSession, error) {
	topic, err := SignalingRoomTopic(roomID, token)
	if err != nil {
		return nil, err
	}
	if !clientIDPattern.MatchString(peerID) {
		return nil, errors.New("native signaling peerId должен содержать ровно 20 латинских букв или цифр")
	}
	if len(urls) == 0 {
		urls = append([]string(nil), DefaultTorrentURLs...)
	}
	ctx, cancel := context.WithCancel(parent)
	session := &TorrentSession{
		ctx: ctx, cancel: cancel, urls: append([]string(nil), urls...),
		topic: topic, hash: TorrentInfoHash(topic), peerID: peerID, token: token,
		events: make(chan Signal, 64), sockets: make(map[string]*torrentSocket),
		offers: make(map[string]Signal), offerOrigins: make(map[string]string),
		seen: make(map[string]time.Time),
	}
	for _, url := range session.urls {
		entry := &torrentSocket{url: url}
		session.sockets[url] = entry
		go session.connectLoop(entry)
	}
	return session, nil
}

func (s *TorrentSession) Name() string          { return "Native BitTorrent" }
func (s *TorrentSession) PeerID() string        { return s.peerID }
func (s *TorrentSession) Events() <-chan Signal { return s.events }
func (s *TorrentSession) Status() (int, int)    { return int(s.open.Load()), len(s.urls) }

func (s *TorrentSession) Publish(_ context.Context, value Signal) error {
	signal, err := prepareOutgoing(value, s.topic, s.peerID, s.token)
	if err != nil {
		return err
	}
	if signal.Type == "candidate" {
		return errors.New("native BitTorrent signaling требует complete non-trickle SDP")
	}
	s.mu.Lock()
	switch signal.Type {
	case "offer":
		s.offers[signal.SessionID] = signal
	case "bye":
		delete(s.offers, signal.SessionID)
		delete(s.offerOrigins, signal.SessionID+":"+signal.To)
	}
	origin := s.offerOrigins[signal.SessionID+":"+signal.To]
	s.mu.Unlock()

	if signal.Type == "answer" {
		payload := map[string]any{
			"action": "announce", "info_hash": s.hash, "peer_id": s.peerID,
			"answer":   map[string]any{"type": "answer", "sdp": trackerSignal(signal)},
			"offer_id": signal.SessionID, "to_peer_id": signal.To,
		}
		if origin != "" {
			return s.sockets[origin].write(payload)
		}
		return s.writeAll(payload)
	}
	return s.announceAll()
}

func (s *TorrentSession) Close() error {
	s.closeMu.Do(func() {
		s.cancel()
		s.mu.Lock()
		for _, entry := range s.sockets {
			entry.mu.Lock()
			if entry.socket != nil {
				_ = entry.socket.Close()
			}
			entry.socket = nil
			entry.mu.Unlock()
		}
		s.mu.Unlock()
	})
	return nil
}

func (s *TorrentSession) connectLoop(entry *torrentSocket) {
	retry := time.Second
	for s.ctx.Err() == nil {
		socket, _, err := websocket.DefaultDialer.DialContext(s.ctx, entry.url, nil)
		if err != nil {
			if !waitContext(s.ctx, retry+time.Duration(rand.Int64N(int64(retry)))) {
				return
			}
			retry = min(30*time.Second, retry*2)
			continue
		}
		retry = time.Second
		entry.mu.Lock()
		entry.socket = socket
		entry.mu.Unlock()
		s.open.Add(1)
		_ = s.announce(entry)
		announceCtx, stopAnnounce := context.WithCancel(s.ctx)
		done := make(chan struct{})
		go func() {
			ticker := time.NewTicker(10 * time.Second)
			defer ticker.Stop()
			defer close(done)
			for {
				select {
				case <-announceCtx.Done():
					return
				case <-ticker.C:
					if entry.write(s.announcePayload()) != nil {
						return
					}
				}
			}
		}()
		for s.ctx.Err() == nil {
			var value map[string]any
			if err := socket.ReadJSON(&value); err != nil {
				break
			}
			s.receive(entry.url, value)
		}
		_ = socket.Close()
		stopAnnounce()
		entry.mu.Lock()
		if entry.socket == socket {
			entry.socket = nil
		}
		entry.mu.Unlock()
		s.open.Add(-1)
		<-done
		if !waitContext(s.ctx, retry+time.Duration(rand.Int64N(int64(retry)))) {
			return
		}
		retry = min(30*time.Second, retry*2)
	}
}

func (s *TorrentSession) receive(url string, value map[string]any) {
	remote, _ := value["peer_id"].(string)
	offerID, _ := value["offer_id"].(string)
	if remote == "" || remote == s.peerID || offerID == "" {
		return
	}
	var signal Signal
	if offer, ok := value["offer"].(map[string]any); ok {
		sdp, _ := offer["sdp"].(string)
		if sdp == "" {
			return
		}
		signal = s.trackerIncoming(sdp, "offer", offerID, remote, s.peerID)
		s.mu.Lock()
		s.offerOrigins[offerID+":"+remote] = url
		s.mu.Unlock()
	} else if answer, ok := value["answer"].(map[string]any); ok {
		sdp, _ := answer["sdp"].(string)
		s.mu.Lock()
		_, expected := s.offers[offerID]
		delete(s.offers, offerID)
		s.mu.Unlock()
		if !expected || sdp == "" {
			return
		}
		signal = s.trackerIncoming(sdp, "answer", offerID, remote, s.peerID)
	} else {
		return
	}
	opened, ok := openIncoming(signal, s.topic, s.peerID, s.token)
	if !ok {
		return
	}
	key := opened.Type + ":" + opened.SessionID + ":" + opened.From
	s.mu.Lock()
	if seen := s.seen[key]; !seen.IsZero() && time.Since(seen) < 5*time.Second {
		s.mu.Unlock()
		return
	}
	s.seen[key] = time.Now()
	for id, seen := range s.seen {
		if time.Since(seen) > signalTTL {
			delete(s.seen, id)
		}
	}
	s.mu.Unlock()
	select {
	case s.events <- opened:
	case <-s.ctx.Done():
	}
}

func (s *TorrentSession) trackerIncoming(
	value, signalType, sessionID, from, to string,
) Signal {
	if !strings.HasPrefix(value, encryptedTrackerPrefix) {
		return Signal{
			Version: SignalVersion, Room: s.topic, Type: signalType,
			SessionID: sessionID, From: from, To: to,
			SDP: value, CreatedAt: time.Now().UnixMilli(),
		}
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(strings.TrimPrefix(value, encryptedTrackerPrefix))
	if err != nil {
		return Signal{}
	}
	var signal Signal
	if json.Unmarshal(decoded, &signal) != nil ||
		signal.Type != signalType || signal.SessionID != sessionID ||
		signal.From != from || signal.Room != s.topic ||
		(signal.To != "" && signal.To != to) {
		return Signal{}
	}
	return signal
}

func trackerSignal(signal Signal) string {
	if signal.Encrypted == "" {
		return signal.SDP
	}
	encoded, _ := json.Marshal(signal)
	return encryptedTrackerPrefix + base64.RawURLEncoding.EncodeToString(encoded)
}

func (s *TorrentSession) announcePayload() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	offers := make([]map[string]any, 0, 3)
	for _, signal := range s.offers {
		offers = append(offers, map[string]any{
			"offer_id": signal.SessionID,
			"offer":    map[string]any{"type": "offer", "sdp": trackerSignal(signal)},
		})
		if len(offers) == 3 {
			break
		}
	}
	return map[string]any{
		"action": "announce", "info_hash": s.hash, "peer_id": s.peerID,
		"numwant": 3, "offers": offers,
	}
}

func (s *TorrentSession) announce(entry *torrentSocket) error {
	return entry.write(s.announcePayload())
}

func (s *TorrentSession) announceAll() error {
	return s.writeAll(s.announcePayload())
}

func (s *TorrentSession) writeAll(payload any) error {
	var sent int
	var firstErr error
	for _, entry := range s.sockets {
		if err := entry.write(payload); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			sent++
		}
	}
	if sent == 0 {
		if firstErr != nil {
			return firstErr
		}
		return fmt.Errorf("ни один WebTorrent tracker не подключён")
	}
	return nil
}

func waitContext(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
