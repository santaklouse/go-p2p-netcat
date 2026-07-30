// Package relay exposes an embeddable Circuit Relay v2 server.
package relay

import (
	"context"
	"errors"
	"sync"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/santaklouse/go-p2p-netcat/internal/identity"
	"github.com/santaklouse/go-p2p-netcat/p2p"
)

type Options struct {
	IdentityPath     string
	PrivateKey       crypto.PrivKey
	LocalPort        int
	WebsocketPort    int
	DisableWebsocket bool
	IPVersion        int
	Announce         []string
	NoMDNS           bool
	NoPubSub         bool
	NoQUIC           bool
	Verbose          bool
}

type Handle struct {
	Node         *p2p.Node
	IdentityPath string

	stopOnce sync.Once
	stopErr  error
}

func Start(ctx context.Context, options Options) (*Handle, error) {
	if options.LocalPort < 0 || options.LocalPort > 65535 {
		return nil, errors.New("relay local port must be between 0 and 65535")
	}
	if options.DisableWebsocket {
		options.WebsocketPort = 0
	} else if options.WebsocketPort == 0 {
		options.WebsocketPort = 9091
	}
	if options.WebsocketPort < 0 || options.WebsocketPort > 65535 {
		return nil, errors.New("relay WebSocket port must be between 1 and 65535")
	}
	if options.IPVersion != 0 && options.IPVersion != 4 && options.IPVersion != 6 {
		return nil, errors.New("relay IP version must be 4, 6, or 0")
	}
	key := options.PrivateKey
	path := options.IdentityPath
	var err error
	if key == nil {
		if path == "" {
			path = identity.DefaultPath() + ".relay"
		}
		key, err = identity.LoadOrCreate(path)
		if err != nil {
			return nil, err
		}
	} else {
		path = ""
	}
	node, err := p2p.New(ctx, p2p.Config{
		PrivateKey:     key,
		TransportPort:  options.LocalPort,
		WebsocketPort:  options.WebsocketPort,
		IPVersion:      options.IPVersion,
		Announce:       options.Announce,
		EnableMDNS:     !options.NoMDNS,
		EnablePubSub:   !options.NoPubSub,
		PubSubDiscover: !options.NoPubSub,
		EnableQUIC:     !options.NoQUIC,
		Listen:         true,
		RelayServer:    true,
		Verbose:        options.Verbose,
	})
	if err != nil {
		return nil, err
	}
	return &Handle{Node: node, IdentityPath: path}, nil
}

func (h *Handle) PeerID() string {
	if h == nil || h.Node == nil {
		return ""
	}
	return h.Node.Host.ID().String()
}

func (h *Handle) Addresses() []string {
	if h == nil || h.Node == nil {
		return nil
	}
	return h.Node.Addresses()
}

func (h *Handle) Stop() error {
	if h == nil {
		return nil
	}
	h.stopOnce.Do(func() {
		if h.Node != nil {
			h.stopErr = h.Node.Close()
		}
	})
	return h.stopErr
}
