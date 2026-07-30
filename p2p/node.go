// Package p2p owns the go-libp2p host, discovery, routing and relay lifecycle.
package p2p

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/go-cid"
	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/net/swarm"
	circuitclient "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/client"
	circuitproto "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/proto"
	circuitrelay "github.com/libp2p/go-libp2p/p2p/protocol/circuitv2/relay"
	quictransport "github.com/libp2p/go-libp2p/p2p/transport/quic"
	tcptransport "github.com/libp2p/go-libp2p/p2p/transport/tcp"
	webrtctransport "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	websockettransport "github.com/libp2p/go-libp2p/p2p/transport/websocket"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
)

const (
	ProtocolPrefix         = "/p2p-netcat/1.0.0"
	DatagramProtocolPrefix = "/p2p-netcat/udp/1.0.0"
	DefaultService         = uint16(31337)
)

type Config struct {
	PrivateKey     crypto.PrivKey
	TransportPort  int
	WebsocketPort  int
	IPVersion      int
	Announce       []string
	Relays         []string
	Bootstrap      []string
	EnableDHT      bool
	EnableMDNS     bool
	EnablePubSub   bool
	PubSubDiscover bool
	PubSubInterval time.Duration
	EnableQUIC     bool
	EnableWebRTC   bool
	Listen         bool
	DHTServer      bool
	RelayServer    bool
	Verbose        bool
}

type Node struct {
	Host               host.Host
	DHT                *dht.IpfsDHT
	PubSub             *pubsub.PubSub
	mdns               mdns.Service
	pubsubTopic        *pubsub.Topic
	pubsubSubscription *pubsub.Subscription
	cancel             context.CancelFunc
	verbose            bool
	once               sync.Once
}

func ProtocolForService(service uint16) protocol.ID {
	return protocol.ID(fmt.Sprintf("%s/%d", ProtocolPrefix, service))
}

func DatagramProtocolForService(service uint16) protocol.ID {
	return protocol.ID(fmt.Sprintf("%s/%d", DatagramProtocolPrefix, service))
}

func New(parent context.Context, cfg Config) (*Node, error) {
	if cfg.PrivateKey == nil {
		return nil, errors.New("private key is required")
	}
	if cfg.IPVersion != 0 && cfg.IPVersion != 4 && cfg.IPVersion != 6 {
		return nil, errors.New("IP version must be 4, 6, or 0")
	}
	ctx, cancel := context.WithCancel(parent)
	options := []libp2p.Option{
		libp2p.Identity(cfg.PrivateKey),
		libp2p.UserAgent("go-p2p-netcat/0.2.0"),
		libp2p.ProtocolVersion("p2p-netcat/1.0.0"),
		libp2p.NoTransports,
		libp2p.SwarmOpts(swarm.WithDialRanker(PreferDialRanker)),
		libp2p.Transport(tcptransport.NewTCPTransport),
		libp2p.Transport(websockettransport.New),
	}
	if cfg.EnableQUIC {
		options = append(options, libp2p.Transport(quictransport.NewTransport))
	}
	if cfg.EnableWebRTC {
		options = append(options, libp2p.Transport(webrtctransport.New))
	}

	if cfg.Listen {
		listen := listenAddresses(cfg)
		options = append(options, libp2p.ListenAddrStrings(listen...))
	} else {
		options = append(options, libp2p.NoListenAddrs)
	}

	staticRelays, err := parseRelayInfos(cfg.Relays)
	if err != nil {
		cancel()
		return nil, err
	}
	announced, err := parseMultiaddrs(cfg.Announce)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("announce: %w", err)
	}
	if cfg.DHTServer && !cfg.RelayServer {
		circuitAddresses, circuitErr := relayCircuitAddresses(cfg.Relays)
		if circuitErr != nil {
			cancel()
			return nil, circuitErr
		}
		announced = append(announced, circuitAddresses...)
	}
	if len(announced) > 0 {
		options = append(options, libp2p.AddrsFactory(func(current []ma.Multiaddr) []ma.Multiaddr {
			return uniqueAddrs(append(current, announced...))
		}))
	}

	var relaySourceHost host.Host
	if cfg.RelayServer {
		resources := circuitrelay.DefaultResources()
		resources.Limit = &circuitrelay.RelayLimit{
			Duration: 2 * time.Hour,
			Data:     128 * 1024 * 1024,
		}
		resources.MaxReservations = 128
		options = append(options,
			libp2p.EnableRelayService(circuitrelay.WithResources(resources)),
			libp2p.ForceReachabilityPublic(),
		)
	} else if len(staticRelays) > 0 {
		options = append(options,
			libp2p.EnableAutoRelayWithStaticRelays(staticRelays),
			libp2p.ForceReachabilityPrivate(),
		)
	} else if cfg.Listen {
		options = append(options, libp2p.EnableAutoRelayWithPeerSource(
			connectedRelayCandidates(&relaySourceHost),
			autorelay.WithMinCandidates(1),
			autorelay.WithNumRelays(1),
			autorelay.WithBootDelay(15*time.Second),
		))
	}

	h, err := libp2p.New(options...)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create libp2p host: %w", err)
	}
	relaySourceHost = h
	node := &Node{Host: h, cancel: cancel, verbose: cfg.Verbose}
	fail := func(cause error) (*Node, error) {
		_ = node.Close()
		return nil, cause
	}

	for _, relay := range staticRelays {
		h.Peerstore().AddAddrs(relay.ID, relay.Addrs, peerstore.PermanentAddrTTL)
		connectCtx, connectCancel := context.WithTimeout(ctx, 15*time.Second)
		err := h.Connect(connectCtx, relay)
		connectCancel()
		if err != nil && cfg.Verbose {
			log.Printf("[p2p-nc] relay %s is not available yet: %v", relay.ID, err)
		}
		if err == nil && cfg.DHTServer && !cfg.RelayServer {
			reserveCtx, reserveCancel := context.WithTimeout(ctx, 15*time.Second)
			_, reserveErr := circuitclient.Reserve(reserveCtx, h, relay)
			reserveCancel()
			if reserveErr != nil {
				return fail(fmt.Errorf("reserve Circuit Relay v2 %s: %w", relay.ID, reserveErr))
			}
		}
	}

	if cfg.EnableDHT {
		bootstrappers, err := bootstrapPeers(cfg.Bootstrap)
		if err != nil {
			return fail(err)
		}
		mode := dht.ModeClient
		if cfg.DHTServer {
			mode = dht.ModeServer
		}
		node.DHT, err = dht.New(h, dht.Mode(mode), dht.BootstrapPeers(bootstrappers...))
		if err != nil {
			return fail(fmt.Errorf("create IPFS Amino DHT: %w", err))
		}
		for _, bootstrap := range bootstrappers {
			h.Peerstore().AddAddrs(bootstrap.ID, bootstrap.Addrs, peerstore.PermanentAddrTTL)
			connectCtx, connectCancel := context.WithTimeout(ctx, 10*time.Second)
			connectErr := h.Connect(connectCtx, bootstrap)
			connectCancel()
			if connectErr != nil && cfg.Verbose {
				log.Printf("[p2p-nc] bootstrap peer %s is not available yet: %v", bootstrap.ID, connectErr)
			}
		}
		if err := node.DHT.Bootstrap(ctx); err != nil {
			return fail(fmt.Errorf("start DHT bootstrap: %w", err))
		}
	}

	if cfg.EnableMDNS {
		node.mdns = mdns.NewMdnsService(h, "", mdnsNotifee{ctx: ctx, host: h, verbose: cfg.Verbose})
		if err := node.mdns.Start(); err != nil {
			return fail(fmt.Errorf("start mDNS: %w", err))
		}
	}
	if cfg.EnablePubSub {
		if err := node.startPubSub(
			ctx,
			staticRelays,
			cfg.PubSubDiscover,
			cfg.RelayServer,
			cfg.PubSubInterval,
		); err != nil {
			return fail(fmt.Errorf("start GossipSub discovery: %w", err))
		}
	}
	return node, nil
}

// PreferDialRanker preserves the JavaScript client's transport preference while
// still racing addresses of the same class in parallel.
func PreferDialRanker(addresses []ma.Multiaddr) []network.AddrDelay {
	sorted := append([]ma.Multiaddr(nil), addresses...)
	sort.SliceStable(sorted, func(left, right int) bool {
		return dialAddressRank(sorted[left]) < dialAddressRank(sorted[right])
	})
	result := make([]network.AddrDelay, 0, len(sorted))
	lastRank := -1
	delay := time.Duration(0)
	for _, address := range sorted {
		rank := dialAddressRank(address)
		if lastRank >= 0 && rank != lastRank {
			delay += 50 * time.Millisecond
		}
		result = append(result, network.AddrDelay{Addr: address, Delay: delay})
		lastRank = rank
	}
	return result
}

func dialAddressRank(address ma.Multiaddr) int {
	value := address.String()
	switch {
	case strings.Contains(value, "/webrtc-direct"):
		return 0
	case strings.Contains(value, "/quic-v1"):
		return 1
	case strings.Contains(value, "/webtransport"):
		return 2
	case strings.Contains(value, "/wss"):
		return 3
	case strings.Contains(value, "/ws"):
		return 4
	case strings.Contains(value, "/tcp/") && !strings.Contains(value, "/p2p-circuit"):
		return 5
	case strings.Contains(value, "/p2p-circuit"):
		return 6
	default:
		return 7
	}
}

func (n *Node) Close() error {
	var result error
	n.once.Do(func() {
		n.cancel()
		if n.mdns != nil {
			_ = n.mdns.Close()
		}
		if n.pubsubSubscription != nil {
			n.pubsubSubscription.Cancel()
		}
		if n.pubsubTopic != nil {
			_ = n.pubsubTopic.Close()
		}
		if n.DHT != nil {
			_ = n.DHT.Close()
		}
		result = n.Host.Close()
	})
	return result
}

func connectedRelayCandidates(hostPointer *host.Host) autorelay.PeerSource {
	return func(ctx context.Context, limit int) <-chan peer.AddrInfo {
		output := make(chan peer.AddrInfo)
		go func() {
			defer close(output)
			if hostPointer == nil || *hostPointer == nil || limit <= 0 {
				return
			}
			h := *hostPointer
			for _, peerID := range h.Peerstore().PeersWithAddrs() {
				if peerID == h.ID() {
					continue
				}
				supported, err := h.Peerstore().SupportsProtocols(peerID, circuitproto.ProtoIDv2Hop)
				if err != nil || len(supported) == 0 {
					continue
				}
				info := peer.AddrInfo{ID: peerID, Addrs: h.Peerstore().Addrs(peerID)}
				select {
				case output <- info:
					limit--
					if limit == 0 {
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
		return output
	}
}

func (n *Node) Addresses() []string {
	result := make([]string, 0, len(n.Host.Addrs()))
	for _, address := range n.Host.Addrs() {
		text := address.String()
		if !strings.HasSuffix(text, "/p2p/"+n.Host.ID().String()) {
			text += "/p2p/" + n.Host.ID().String()
		}
		result = append(result, text)
	}
	sort.Strings(result)
	return result
}

func (n *Node) OpenStream(ctx context.Context, target string, service uint16, relays []string, token *pairing.Token) (network.Stream, error) {
	return n.openStream(ctx, target, ProtocolForService(service), relays, token)
}

func (n *Node) OpenDatagramStream(ctx context.Context, target string, service uint16, relays []string, token *pairing.Token) (network.Stream, error) {
	return n.openStream(ctx, target, DatagramProtocolForService(service), relays, token)
}

func (n *Node) openStream(ctx context.Context, target string, protocolID protocol.ID, relays []string, token *pairing.Token) (network.Stream, error) {
	targetID, err := n.resolve(ctx, target, relays, token)
	if err != nil {
		return nil, err
	}
	return n.Host.NewStream(
		network.WithAllowLimitedConn(ctx, "p2p-netcat application stream"),
		targetID,
		protocolID,
	)
}

func (n *Node) resolve(ctx context.Context, target string, relays []string, token *pairing.Token) (peer.ID, error) {
	if strings.HasPrefix(target, "/") {
		address, err := ma.NewMultiaddr(target)
		if err != nil {
			return "", err
		}
		info, err := peer.AddrInfoFromP2pAddr(address)
		if err != nil {
			return "", errors.New("full multiaddr must contain /p2p/PeerId")
		}
		n.Host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.TempAddrTTL)
		return info.ID, nil
	}
	targetID, err := peer.Decode(target)
	if err != nil {
		return "", fmt.Errorf("invalid PeerId %q: %w", target, err)
	}
	if len(relays) > 0 {
		relayAddress, err := ma.NewMultiaddr(strings.TrimSuffix(relays[0], "/"))
		if err != nil {
			return "", err
		}
		circuit, _ := ma.NewMultiaddr("/p2p-circuit/p2p/" + targetID.String())
		info, err := peer.AddrInfoFromP2pAddr(relayAddress.Encapsulate(circuit))
		if err != nil {
			return "", fmt.Errorf("build relay route: %w", err)
		}
		n.Host.Peerstore().AddAddrs(targetID, info.Addrs, peerstore.TempAddrTTL)
		return targetID, nil
	}
	if len(n.Host.Peerstore().Addrs(targetID)) > 0 {
		return targetID, nil
	}
	if n.DHT == nil {
		return "", errors.New("PeerId was not found locally and the DHT is disabled; provide a full multiaddr or --relay")
	}

	var providerCIDs []cid.Cid
	if token == nil {
		providerCIDs = []cid.Cid{peer.ToCid(targetID)}
	} else {
		providerCIDs, err = token.ProviderCIDs(time.Now())
		if err != nil {
			return "", err
		}
	}
	for _, providerCID := range providerCIDs {
		lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		for info := range n.DHT.FindProvidersAsync(lookupCtx, providerCID, 20) {
			if info.ID != targetID || len(info.Addrs) == 0 {
				continue
			}
			n.Host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.TempAddrTTL)
			cancel()
			return targetID, nil
		}
		cancel()
	}
	if token == nil {
		info, err := n.DHT.FindPeer(ctx, targetID)
		if err == nil && len(info.Addrs) > 0 {
			n.Host.Peerstore().AddAddrs(info.ID, info.Addrs, peerstore.TempAddrTTL)
			return targetID, nil
		}
	}
	return "", fmt.Errorf("could not find PeerId %s; provide --relay or a full multiaddr", targetID)
}

func (n *Node) Advertise(ctx context.Context, token *pairing.Token) {
	if n.DHT == nil {
		return
	}
	go func() {
		for {
			cids := []cid.Cid{peer.ToCid(n.Host.ID())}
			interval := 6 * time.Hour
			if token != nil {
				var err error
				cids, err = token.ProviderCIDs(time.Now())
				if err != nil {
					return
				}
				interval = 5 * time.Minute
			}
			for _, value := range cids {
				provideCtx, cancel := context.WithTimeout(ctx, time.Minute)
				err := n.DHT.Provide(provideCtx, value, true)
				cancel()
				if err != nil && n.verbose {
					log.Printf("[p2p-nc] DHT provider record has not been published yet: %v", err)
				}
			}
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func listenAddresses(cfg Config) []string {
	var result []string
	port := cfg.TransportPort
	if cfg.IPVersion != 6 {
		result = append(result, fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
		if cfg.EnableQUIC {
			result = append(result, fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", port))
		}
		if cfg.EnableWebRTC {
			result = append(result, fmt.Sprintf("/ip4/0.0.0.0/udp/%d/webrtc-direct", port))
		}
		if cfg.WebsocketPort > 0 {
			result = append(result, fmt.Sprintf("/ip4/0.0.0.0/tcp/%d/ws", cfg.WebsocketPort))
		}
	}
	if cfg.IPVersion != 4 {
		result = append(result, fmt.Sprintf("/ip6/::/tcp/%d", port))
		if cfg.EnableQUIC {
			result = append(result, fmt.Sprintf("/ip6/::/udp/%d/quic-v1", port))
		}
		if cfg.EnableWebRTC {
			result = append(result, fmt.Sprintf("/ip6/::/udp/%d/webrtc-direct", port))
		}
		if cfg.WebsocketPort > 0 {
			result = append(result, fmt.Sprintf("/ip6/::/tcp/%d/ws", cfg.WebsocketPort))
		}
	}
	return result
}

func parseMultiaddrs(values []string) ([]ma.Multiaddr, error) {
	result := make([]ma.Multiaddr, 0, len(values))
	for _, value := range values {
		address, err := ma.NewMultiaddr(value)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", value, err)
		}
		result = append(result, address)
	}
	return result, nil
}

func parseRelayInfos(values []string) ([]peer.AddrInfo, error) {
	result := make([]peer.AddrInfo, 0, len(values))
	for _, value := range values {
		address, err := ma.NewMultiaddr(strings.TrimSuffix(value, "/"))
		if err != nil {
			return nil, fmt.Errorf("invalid relay multiaddr %q: %w", value, err)
		}
		info, err := peer.AddrInfoFromP2pAddr(address)
		if err != nil {
			return nil, fmt.Errorf("relay multiaddr must contain /p2p/PeerId: %s", value)
		}
		result = append(result, *info)
	}
	return result, nil
}

func relayCircuitAddresses(values []string) ([]ma.Multiaddr, error) {
	circuit, _ := ma.NewMultiaddr("/p2p-circuit")
	result := make([]ma.Multiaddr, 0, len(values))
	for _, value := range values {
		address, err := ma.NewMultiaddr(strings.TrimSuffix(value, "/"))
		if err != nil {
			return nil, fmt.Errorf("invalid relay multiaddr %q: %w", value, err)
		}
		if _, err := peer.AddrInfoFromP2pAddr(address); err != nil {
			return nil, fmt.Errorf("relay multiaddr must contain /p2p/PeerId: %s", value)
		}
		result = append(result, address.Encapsulate(circuit))
	}
	return result, nil
}

func bootstrapPeers(values []string) ([]peer.AddrInfo, error) {
	if len(values) == 0 {
		return dht.GetDefaultBootstrapPeerAddrInfos(), nil
	}
	return parseRelayInfos(values)
}

func uniqueAddrs(values []ma.Multiaddr) []ma.Multiaddr {
	seen := make(map[string]struct{}, len(values))
	result := make([]ma.Multiaddr, 0, len(values))
	for _, value := range values {
		if _, ok := seen[value.String()]; ok {
			continue
		}
		seen[value.String()] = struct{}{}
		result = append(result, value)
	}
	return result
}

type mdnsNotifee struct {
	ctx     context.Context
	host    host.Host
	verbose bool
}

func (n mdnsNotifee) HandlePeerFound(info peer.AddrInfo) {
	if info.ID == n.host.ID() {
		return
	}
	if err := n.host.Connect(n.ctx, info); err != nil && n.verbose {
		log.Printf("[p2p-nc] mDNS peer %s: %v", info.ID, err)
	}
}
