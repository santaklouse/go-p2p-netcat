package cli

import (
	"github.com/libp2p/go-libp2p/core/crypto"
	p2pnode "github.com/santaklouse/go-p2p-netcat/p2p"
)

func nodeConfig(opts *options, privateKey crypto.PrivKey, listen, dhtServer, relayServer bool) p2pnode.Config {
	ipVersion := 0
	if opts.ipv4 {
		ipVersion = 4
	} else if opts.ipv6 {
		ipVersion = 6
	}
	enableDHT := !opts.noDHT && !opts.tor
	enableMDNS := !opts.noMDNS && !opts.tor
	enableQUIC := !opts.noQUIC && !opts.tor
	enableWebRTC := !opts.noWebRTC && !opts.tor
	bootstrap := opts.bootstrap
	if !enableDHT {
		bootstrap = nil
	}
	return p2pnode.Config{
		PrivateKey:    privateKey,
		TransportPort: opts.transportPort,
		IPVersion:     ipVersion,
		Announce:      opts.announce,
		Relays:        opts.relays,
		Bootstrap:     bootstrap,
		EnableDHT:     enableDHT,
		EnableMDNS:    enableMDNS,
		EnableQUIC:    enableQUIC,
		EnableWebRTC:  enableWebRTC,
		Listen:        listen,
		DHTServer:     dhtServer,
		RelayServer:   relayServer,
		Verbose:       opts.verbose,
	}
}
