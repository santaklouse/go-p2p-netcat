package nativewebrtc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const nostrEventKind = 25050

type NostrSession struct {
	ctx     context.Context
	cancel  context.CancelFunc
	pool    *nostr.SimplePool
	urls    []string
	topic   string
	peerID  string
	token   *pairing.Token
	secret  string
	public  string
	events  chan Signal
	open    atomic.Int32
	closeMu sync.Once
}

func NewNostrSession(
	parent context.Context,
	roomID, peerID string,
	urls []string,
	token *pairing.Token,
) (*NostrSession, error) {
	topic, err := SignalingRoomTopic(roomID, token)
	if err != nil {
		return nil, err
	}
	if !clientIDPattern.MatchString(peerID) {
		return nil, errors.New("native signaling peerId must contain exactly 20 ASCII letters or digits")
	}
	if len(urls) == 0 {
		urls = append([]string(nil), DefaultNostrURLs...)
	}
	ctx, cancel := context.WithCancel(parent)
	secret := nostr.GeneratePrivateKey()
	public, err := nostr.GetPublicKey(secret)
	if err != nil {
		cancel()
		return nil, err
	}
	session := &NostrSession{
		ctx: ctx, cancel: cancel, pool: nostr.NewSimplePool(ctx),
		urls: append([]string(nil), urls...), topic: topic, peerID: peerID,
		token: token, secret: secret, public: public, events: make(chan Signal, 64),
	}
	go session.subscribe()
	return session, nil
}

func (s *NostrSession) Name() string          { return "Native Nostr" }
func (s *NostrSession) PeerID() string        { return s.peerID }
func (s *NostrSession) Events() <-chan Signal { return s.events }
func (s *NostrSession) Status() (int, int)    { return int(s.open.Load()), len(s.urls) }

func (s *NostrSession) Publish(ctx context.Context, value Signal) error {
	signal, err := prepareOutgoing(value, s.topic, s.peerID, s.token)
	if err != nil {
		return err
	}
	content, err := json.Marshal(signal)
	if err != nil {
		return err
	}
	event := nostr.Event{
		PubKey: s.public, CreatedAt: nostr.Now(), Kind: nostrEventKind,
		Tags: nostr.Tags{nostr.Tag{"t", s.topic}}, Content: string(content),
	}
	if err := event.Sign(s.secret); err != nil {
		return err
	}
	publishCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	var firstErr error
	for result := range s.pool.PublishMany(publishCtx, s.urls, event) {
		if result.Error == nil {
			s.open.Store(1)
			return nil
		}
		if firstErr == nil {
			firstErr = result.Error
		}
	}
	return firstErr
}

func (s *NostrSession) Close() error {
	s.closeMu.Do(func() {
		s.cancel()
		s.pool.Close("p2p-netcat native signaling stopped")
	})
	return nil
}

func (s *NostrSession) subscribe() {
	since := nostr.Timestamp(time.Now().Add(-10 * time.Second).Unix())
	stream := s.pool.SubscribeMany(s.ctx, s.urls, nostr.Filter{
		Kinds: []int{nostrEventKind},
		Tags:  nostr.TagMap{"t": []string{s.topic}},
		Since: &since,
	})
	for event := range stream {
		s.open.Store(1)
		if event.Event == nil {
			continue
		}
		if len(event.Event.Content) > maxSignalingMessageBytes {
			continue
		}
		valid, err := event.Event.CheckSignature()
		if err != nil || !valid {
			continue
		}
		var signal Signal
		if json.Unmarshal([]byte(event.Event.Content), &signal) != nil {
			continue
		}
		if opened, ok := openIncoming(signal, s.topic, s.peerID, s.token); ok {
			select {
			case s.events <- opened:
			case <-s.ctx.Done():
				return
			}
		}
	}
}
