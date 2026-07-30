package nativewebrtc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type testWebSocketClient struct {
	socket *websocket.Conn
	mu     sync.Mutex
	subID  string
	peerID string
}

func (c *testWebSocketClient) write(value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.socket.WriteJSON(value)
}

type testWebSocketHub struct {
	mu      sync.Mutex
	clients map[*testWebSocketClient]struct{}
}

func newTestWebSocketHub(t *testing.T, protocol string) (*testWebSocketHub, string) {
	t.Helper()
	hub := &testWebSocketHub{clients: make(map[*testWebSocketClient]struct{})}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		socket, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			return
		}
		client := &testWebSocketClient{socket: socket}
		hub.mu.Lock()
		hub.clients[client] = struct{}{}
		hub.mu.Unlock()
		defer func() {
			hub.mu.Lock()
			delete(hub.clients, client)
			hub.mu.Unlock()
			_ = socket.Close()
		}()
		if protocol == "nostr" {
			hub.serveNostr(client)
		} else {
			hub.serveTorrent(client)
		}
	}))
	t.Cleanup(server.Close)
	return hub, "ws" + strings.TrimPrefix(server.URL, "http")
}

func (h *testWebSocketHub) serveNostr(client *testWebSocketClient) {
	for {
		var raw []json.RawMessage
		if client.socket.ReadJSON(&raw) != nil || len(raw) == 0 {
			return
		}
		var kind string
		if json.Unmarshal(raw[0], &kind) != nil {
			continue
		}
		switch kind {
		case "REQ":
			if len(raw) > 1 {
				var subID string
				_ = json.Unmarshal(raw[1], &subID)
				h.mu.Lock()
				client.subID = subID
				h.mu.Unlock()
			}
		case "EVENT":
			if len(raw) < 2 {
				continue
			}
			var event map[string]any
			if json.Unmarshal(raw[1], &event) != nil {
				continue
			}
			eventID, _ := event["id"].(string)
			client.write([]any{"OK", eventID, true, ""})
			h.mu.Lock()
			destinations := make([]*testWebSocketClient, 0, len(h.clients))
			for destination := range h.clients {
				if destination.subID != "" {
					destinations = append(destinations, destination)
				}
			}
			h.mu.Unlock()
			for _, destination := range destinations {
				destination.write([]any{"EVENT", destination.subID, event})
			}
		}
	}
}

func (h *testWebSocketHub) serveTorrent(client *testWebSocketClient) {
	for {
		var message map[string]any
		if client.socket.ReadJSON(&message) != nil {
			return
		}
		h.mu.Lock()
		client.peerID, _ = message["peer_id"].(string)
		destinations := make([]*testWebSocketClient, 0, len(h.clients))
		for destination := range h.clients {
			if destination != client {
				destinations = append(destinations, destination)
			}
		}
		h.mu.Unlock()
		if offers, ok := message["offers"].([]any); ok {
			for _, rawOffer := range offers {
				offer, _ := rawOffer.(map[string]any)
				for _, destination := range destinations {
					destination.write(map[string]any{
						"peer_id": client.peerID, "offer_id": offer["offer_id"],
						"offer": offer["offer"],
					})
				}
			}
		}
		if answer, ok := message["answer"].(map[string]any); ok {
			target, _ := message["to_peer_id"].(string)
			for _, destination := range destinations {
				if destination.peerID == target {
					destination.write(map[string]any{
						"peer_id": client.peerID, "offer_id": message["offer_id"],
						"answer": answer,
					})
				}
			}
		}
	}
}

func (h *testWebSocketHub) waitClients(t *testing.T, count int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		current := len(h.clients)
		h.mu.Unlock()
		if current >= count {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("WebSocket clients did not reach %d", count)
}

func waitSignalingOpen(t *testing.T, session SignalingSession) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if open, _ := session.Status(); open > 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s did not connect", session.Name())
}

func TestNostrSignalingSessionExchange(t *testing.T) {
	hub, url := newTestWebSocketHub(t, "nostr")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := NewNostrSession(ctx, "room", "Client0123456789abcd", []string{url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := NewNostrSession(ctx, "room", "Server0123456789abcd", []string{url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	hub.waitClients(t, 2)
	if err := client.Publish(ctx, Signal{
		Type: "offer", SessionID: "0123456789abcdef", To: server.PeerID(), SDP: "v=0\r\n",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case signal := <-server.Events():
		if signal.Type != "offer" || signal.SDP != "v=0\r\n" || signal.From != client.PeerID() {
			t.Fatalf("signal = %+v", signal)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestTorrentSignalingRoutesOfferAndAnswer(t *testing.T) {
	_, url := newTestWebSocketHub(t, "torrent")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := NewTorrentSession(ctx, "room", "Client0123456789abcd", []string{url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server, err := NewTorrentSession(ctx, "room", "Server0123456789abcd", []string{url}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	waitSignalingOpen(t, client)
	waitSignalingOpen(t, server)
	const sessionID = "0123456789abcdef"
	if err := client.Publish(ctx, Signal{Type: "offer", SessionID: sessionID, SDP: "offer-sdp"}); err != nil {
		t.Fatal(err)
	}
	var offer Signal
	select {
	case offer = <-server.Events():
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if offer.Type != "offer" || offer.SDP != "offer-sdp" {
		t.Fatalf("offer = %+v", offer)
	}
	if err := server.Publish(ctx, Signal{
		Type: "answer", SessionID: sessionID, To: offer.From, SDP: "answer-sdp",
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case answer := <-client.Events():
		if answer.Type != "answer" || answer.SDP != "answer-sdp" || answer.To != client.PeerID() {
			t.Fatalf("answer = %+v", answer)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
