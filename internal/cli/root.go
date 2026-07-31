package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/santaklouse/go-p2p-netcat/internal/identity"
	"github.com/santaklouse/go-p2p-netcat/nativewebrtc"
	p2pnode "github.com/santaklouse/go-p2p-netcat/p2p"
	"github.com/santaklouse/go-p2p-netcat/protocol/admission"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	relayserver "github.com/santaklouse/go-p2p-netcat/relay"
	"github.com/santaklouse/go-p2p-netcat/session"
	"github.com/spf13/cobra"
)

var Version = "0.4.1"

type options struct {
	listen           bool
	keepOpen         bool
	timeout          int
	timeoutExplicit  bool
	quitDelay        int
	destination      string
	port             int
	quiet            bool
	socks            bool
	tor              bool
	interactive      bool
	zero             bool
	exec             string
	udp              bool
	udpIdleTimeout   int
	transportPort    int
	ipv4             bool
	ipv6             bool
	identity         string
	relays           []string
	bootstrap        []string
	announce         []string
	noDHT            bool
	noMDNS           bool
	noPubsub         bool
	noQUIC           bool
	noWebRTC         bool
	pairingToken     string
	pairingTokenFile string
	bind             string
	json             bool
	verbose          bool
}

func NewRoot() *cobra.Command {
	opts := &options{}
	root := &cobra.Command{
		Use:           "p2p-nc [options] [PeerId|multiaddr] [logical-port]",
		Short:         "A netcat-like P2P client built on libp2p/IPFS",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(2),
		RunE: func(command *cobra.Command, args []string) error {
			opts.timeoutExplicit = command.Flags().Changed("timeout")
			if err := validateOptions(command, opts, args); err != nil {
				return err
			}
			if opts.listen {
				return runListener(command.Context(), opts, args)
			}
			return runClient(command.Context(), opts, args)
		},
	}
	flags := root.Flags()
	flags.BoolVarP(&opts.listen, "listen", "l", false, "listen for incoming connections")
	flags.BoolVarP(&opts.keepOpen, "keep-open", "k", false, "keep accepting connections")
	flags.IntVarP(&opts.timeout, "timeout", "w", 60, "discovery and connection timeout in seconds")
	flags.IntVar(&opts.quitDelay, "quit-delay", 0, "delay closing after stdin EOF, in seconds")
	flags.StringVarP(&opts.destination, "destination", "d", "", "server-side TCP/UDP forwarding destination")
	flags.IntVarP(&opts.port, "port", "p", 0, "destination port with -l or client local TCP/UDP listen port")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "suppress diagnostics")
	flags.BoolVarP(&opts.socks, "socks", "S", false, "run a SOCKS4/4a/5 server on the remote side")
	flags.BoolVarP(&opts.tor, "tor", "T", false, "connect to a relay through Tor/torsocks")
	flags.BoolVarP(&opts.interactive, "interactive", "i", false, "run an interactive PTY login shell")
	flags.BoolVarP(&opts.zero, "zero", "z", false, "check connectivity without transferring data")
	flags.StringVarP(&opts.exec, "exec", "e", "", "attach the server stream to a shell command")
	flags.BoolVarP(&opts.udp, "udp", "u", false, "preserve UDP datagrams over a P2P stream; requires -p")
	flags.IntVar(
		&opts.udpIdleTimeout,
		"udp-idle-timeout",
		int(session.DefaultUDPIdleTimeout/time.Second),
		"close an idle UDP source association after this many seconds; 0 disables",
	)
	addNodeFlags(root, opts)
	root.AddCommand(newIDCommand(), newTokenCommand(), newRelayCommand())
	root.InitDefaultVersionFlag()
	if versionFlag := root.Flags().Lookup("version"); versionFlag != nil {
		versionFlag.Shorthand = "V"
	}
	return root
}

func addNodeFlags(command *cobra.Command, opts *options) {
	flags := command.Flags()
	flags.IntVar(&opts.transportPort, "transport-port", 0, "local libp2p TCP/UDP port")
	flags.BoolVarP(&opts.ipv4, "ipv4", "4", false, "listen on IPv4 only")
	flags.BoolVarP(&opts.ipv6, "ipv6", "6", false, "listen on IPv6 only")
	flags.StringVarP(&opts.identity, "identity", "I", "", "persistent private key file")
	flags.StringSliceVar(&opts.relays, "relay", nil, "Circuit Relay v2 multiaddr; may be repeated")
	flags.StringSliceVar(&opts.bootstrap, "bootstrap", nil, "replace the default IPFS bootstrap peers")
	flags.StringSliceVar(&opts.announce, "announce", nil, "public multiaddr to announce")
	flags.BoolVar(&opts.noDHT, "no-dht", false, "disable the IPFS Amino DHT")
	flags.BoolVar(&opts.noMDNS, "no-mdns", false, "disable mDNS")
	flags.BoolVar(&opts.noPubsub, "no-pubsub", false, "disable PubSub discovery")
	flags.BoolVar(&opts.noQUIC, "no-quic", false, "disable QUIC")
	flags.BoolVar(&opts.noWebRTC, "no-webrtc", false, "disable WebRTC Direct and native Nostr/WebTorrent WebRTC")
	flags.StringVar(&opts.pairingToken, "pairing-token", "", "private pnc1_... pairing token")
	flags.StringVar(&opts.pairingTokenFile, "pairing-token-file", "", "read a pairing token from a file")
	flags.StringVar(&opts.bind, "bind", "127.0.0.1", "local bind address for TCP/UDP -p forwarding")
	flags.BoolVar(&opts.json, "json", false, "write node information as JSON to stderr")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "enable verbose diagnostics")
}

func validateOptions(command *cobra.Command, opts *options, args []string) error {
	if opts.timeout <= 0 {
		return errors.New("-w/--timeout must be greater than zero")
	}
	if opts.quitDelay < 0 || opts.transportPort < 0 || opts.transportPort > 65535 {
		return errors.New("ports and delays cannot be negative")
	}
	if opts.udpIdleTimeout < 0 {
		return errors.New("--udp-idle-timeout cannot be negative")
	}
	if command.Flags().Changed("udp-idle-timeout") && !opts.udp {
		return errors.New("--udp-idle-timeout requires -u/--udp")
	}
	if opts.port < 0 || opts.port > 65535 || (command.Flags().Changed("port") && opts.port == 0) {
		return errors.New("-p/--port must be between 1 and 65535")
	}
	if opts.ipv4 && opts.ipv6 {
		return errors.New("-4 and -6 cannot be used together")
	}
	if opts.udp && opts.port == 0 {
		return errors.New("-u/--udp requires -p/--port for UDP forwarding")
	}
	if opts.udp && (opts.socks || opts.interactive || opts.zero || opts.exec != "" || opts.quitDelay != 0) {
		return errors.New("-u cannot be combined with -S, -i, -z, -e, or --quit-delay")
	}
	if opts.exec != "" && !opts.listen {
		return errors.New("-e is available only with -l")
	}
	if opts.destination != "" && (!opts.listen || opts.port == 0) {
		return errors.New("-d is available only with -l -p <port>")
	}
	if opts.socks && !opts.listen {
		return errors.New("-S is available only with -l")
	}
	if opts.socks && (opts.destination != "" || opts.port != 0) {
		return errors.New("-S cannot be combined with server-side -d/-p")
	}
	if opts.interactive && (opts.exec != "" || opts.socks) {
		return errors.New("-i cannot be combined with -e or -S")
	}
	if !opts.listen && opts.port != 0 && (opts.interactive || opts.zero) {
		return errors.New("client-side -p cannot be combined with -i or -z")
	}
	if opts.tor && opts.listen {
		return errors.New("-T is supported only in client mode")
	}
	if opts.tor && len(opts.relays) == 0 {
		return errors.New("-T requires an explicit --relay")
	}
	for _, relay := range opts.relays {
		if opts.tor && strings.Contains(relay, "/udp/") {
			return errors.New("Tor cannot carry QUIC/UDP; use a TCP/WS/WSS relay")
		}
	}
	if opts.listen && len(args) > 1 {
		return errors.New("listener mode accepts only a logical port: p2p-nc -l 8080")
	}
	return nil
}

func runListener(ctx context.Context, opts *options, args []string) error {
	identityPath := opts.identity
	if identityPath == "" {
		identityPath = identity.DefaultPath()
	}
	privateKey, err := identity.LoadOrCreate(identityPath)
	if err != nil {
		return err
	}
	peerID, err := peer.IDFromPrivateKey(privateKey)
	if err != nil {
		return err
	}
	token, err := loadToken(opts)
	if err != nil {
		return err
	}
	service := p2pnode.DefaultService
	if len(args) == 1 {
		service, err = parseService(args[0])
	} else if token != nil {
		service = token.Service
	}
	if err != nil {
		return err
	}
	if token != nil {
		if err := validateTokenFor(token, peerID, service); err != nil {
			return err
		}
		if len(opts.relays) == 0 {
			opts.relays = relayStrings(token)
		}
	}
	node, err := p2pnode.New(ctx, nodeConfig(opts, privateKey, true, true, false, token != nil))
	if err != nil {
		return err
	}
	defer node.Close()
	node.Advertise(ctx, token)

	persistent := opts.keepOpen || opts.interactive || opts.socks || opts.port != 0
	result := make(chan error, 1)
	var acceptedMu sync.Mutex
	accepted := false
	handleStream := func(stream session.Stream, remote string) {
		acceptedMu.Lock()
		if accepted && !persistent && token == nil {
			acceptedMu.Unlock()
			_ = stream.Reset()
			return
		}
		if token == nil {
			accepted = true
		}
		acceptedMu.Unlock()
		go func() {
			defer stream.Close()
			if token != nil {
				if err := admission.AuthenticateServer(stream, token, minDuration(time.Duration(opts.timeout)*time.Second, 10*time.Second)); err != nil {
					_ = stream.Reset()
					diagnostic(opts, "pairing-token authentication: %v", err)
					return
				}
				acceptedMu.Lock()
				if accepted && !persistent {
					acceptedMu.Unlock()
					_ = stream.Reset()
					return
				}
				accepted = true
				acceptedMu.Unlock()
			}
			diagnostic(opts, "peer %s connected to logical port %d", remote, service)
			sessionErr := runServerSession(ctx, stream, opts)
			if persistent {
				if sessionErr != nil {
					diagnostic(opts, "session ended with an error: %v", sessionErr)
				}
				return
			}
			select {
			case result <- sessionErr:
			default:
			}
		}()
	}
	applicationProtocol := p2pnode.ProtocolForService(service)
	if opts.udp {
		applicationProtocol = p2pnode.DatagramProtocolForService(service)
	}
	node.Host.SetStreamHandler(applicationProtocol, func(stream network.Stream) {
		handleStream(stream, stream.Conn().RemotePeer().String())
	})
	var nativeListener *nativewebrtc.Listener
	if !opts.udp && !opts.noWebRTC && !opts.tor {
		nativeListener, err = nativewebrtc.StartListener(
			ctx, privateKey, service, token, nil, nil,
			func(stream *nativewebrtc.Stream, remote string) {
				handleStream(stream, "WebRTC/"+remote)
			},
		)
		if err != nil {
			diagnostic(opts, "native WebRTC signaling did not start: %v", err)
		} else {
			defer nativeListener.Close()
			diagnostic(opts, "native WebRTC Nostr/WebTorrent signaling started")
		}
	}
	printNodeInfo(opts, node, fmt.Sprintf("listener:%d", service))
	diagnostic(opts, "persistent key: %s", identityPath)
	if token != nil {
		diagnostic(opts, "private pairing-token mode enabled")
	}
	if persistent {
		<-ctx.Done()
		return nil
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-result:
		return err
	}
}

func runClient(ctx context.Context, opts *options, args []string) error {
	token, err := loadToken(opts)
	if err != nil {
		return err
	}
	var target string
	if len(args) > 0 {
		target = args[0]
	} else if token != nil {
		target = token.PeerID.String()
	}
	if target == "" {
		return errors.New("PeerId is required; example: p2p-nc 12D3KooW... 8080")
	}
	service := p2pnode.DefaultService
	if len(args) == 2 {
		service, err = parseService(args[1])
	} else if token != nil {
		service = token.Service
	}
	if err != nil {
		return err
	}
	targetID, err := peerIDFromTarget(target)
	if err != nil {
		return err
	}
	if token != nil {
		if err := validateTokenFor(token, targetID, service); err != nil {
			return err
		}
		if len(opts.relays) == 0 {
			opts.relays = relayStrings(token)
		}
	}
	privateKey, err := identity.LoadOrCreate(opts.identity)
	if err != nil {
		return err
	}
	node, err := p2pnode.New(ctx, nodeConfig(opts, privateKey, true, false, false, token != nil))
	if err != nil {
		return err
	}
	defer node.Close()
	timeout := time.Duration(opts.timeout) * time.Second
	openStream := func(openCtx context.Context) (session.Stream, error) {
		dialCtx, cancel := context.WithTimeout(openCtx, timeout)
		defer cancel()
		var stream session.Stream
		var openErr error
		if opts.udp {
			stream, openErr = node.OpenDatagramStream(dialCtx, target, service, opts.relays, token)
		} else {
			stream, openErr = openAnyStream(
				dialCtx, node, target, targetID, service, opts.relays, token,
				!opts.noWebRTC && !opts.tor,
			)
		}
		if openErr != nil {
			return nil, openErr
		}
		if token != nil {
			if authErr := admission.AuthenticateClient(stream, token, minDuration(timeout, 10*time.Second)); authErr != nil {
				_ = stream.Reset()
				return nil, authErr
			}
		}
		return stream, nil
	}
	if opts.verbose {
		printNodeInfo(opts, node, "client")
	}
	if opts.port != 0 {
		if opts.udp {
			listener, err := session.StartLocalUDPForward(
				ctx,
				opts.bind,
				opts.port,
				time.Duration(opts.udpIdleTimeout)*time.Second,
				openStream,
				func(value error) {
					diagnostic(opts, "UDP forwarding session: %v", value)
				},
			)
			if err != nil {
				return err
			}
			defer listener.Close()
			diagnostic(opts, "local UDP %s -> %s:%d", listener.LocalAddr(), target, service)
			<-ctx.Done()
			return nil
		}
		listener, err := session.StartLocalForward(ctx, opts.bind, opts.port, openStream, func(value error) {
			diagnostic(opts, "TCP forwarding session: %v", value)
		})
		if err != nil {
			return err
		}
		defer listener.Close()
		diagnostic(opts, "local TCP %s -> %s:%d", listener.Addr(), target, service)
		<-ctx.Done()
		return nil
	}
	stream, err := openStream(ctx)
	if err != nil {
		return fmt.Errorf("no route established a connection: %w", err)
	}
	defer stream.Close()
	if opts.verbose || opts.zero {
		diagnostic(opts, "connected to %s:%d", target, service)
	}
	if opts.zero {
		return stream.Close()
	}
	if opts.interactive {
		return session.PTYClient(ctx, stream)
	}
	inactivity := time.Duration(0)
	if opts.timeoutExplicit {
		inactivity = time.Duration(opts.timeout) * time.Second
	}
	return session.Bridge(ctx, stream, os.Stdin, os.Stdout, time.Duration(opts.quitDelay)*time.Second, inactivity)
}

type streamAttempt struct {
	stream session.Stream
	err    error
}

func openAnyStream(
	ctx context.Context,
	node *p2pnode.Node,
	target string,
	targetID peer.ID,
	service uint16,
	relays []string,
	token *pairing.Token,
	enableNative bool,
) (session.Stream, error) {
	if !enableNative {
		return node.OpenStream(ctx, target, service, relays, token)
	}
	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	attempts := make(chan streamAttempt, 2)
	go func() {
		stream, err := node.OpenStream(raceCtx, target, service, relays, token)
		select {
		case attempts <- streamAttempt{stream: stream, err: err}:
		case <-raceCtx.Done():
			if stream != nil {
				_ = stream.Reset()
			}
		}
	}()
	go func() {
		connection, err := nativewebrtc.Connect(
			raceCtx, targetID, service, token, time.Until(deadlineOr(raceCtx, time.Now().Add(30*time.Second))),
			nil, nil,
		)
		if err != nil {
			attempts <- streamAttempt{err: err}
			return
		}
		select {
		case attempts <- streamAttempt{stream: connection.Stream}:
		case <-raceCtx.Done():
			_ = connection.Close()
		}
	}()
	var failures []error
	for range 2 {
		select {
		case attempt := <-attempts:
			if attempt.err == nil && attempt.stream != nil {
				cancel()
				return attempt.stream, nil
			}
			failures = append(failures, attempt.err)
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return nil, errors.Join(failures...)
}

func deadlineOr(ctx context.Context, fallback time.Time) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return fallback
}

func runServerSession(ctx context.Context, stream session.Stream, opts *options) error {
	timeout := time.Duration(opts.timeout) * time.Second
	switch {
	case opts.udp:
		host := opts.destination
		if host == "" {
			host = "127.0.0.1"
		}
		return session.UDPForward(
			ctx,
			stream,
			host,
			opts.port,
			timeout,
			time.Duration(opts.udpIdleTimeout)*time.Second,
		)
	case opts.interactive:
		return session.PTYServer(ctx, stream, opts.verbose)
	case opts.socks:
		return session.SOCKS(ctx, stream, timeout)
	case opts.port != 0:
		host := opts.destination
		if host == "" {
			host = "127.0.0.1"
		}
		return session.TCPForward(ctx, stream, host, opts.port, timeout)
	case opts.exec != "":
		return session.Exec(ctx, stream, opts.exec, opts.verbose)
	default:
		inactivity := time.Duration(0)
		if opts.timeoutExplicit {
			inactivity = timeout
		}
		return session.Bridge(
			ctx,
			stream,
			os.Stdin,
			os.Stdout,
			time.Duration(opts.quitDelay)*time.Second,
			inactivity,
		)
	}
}

func diagnostic(opts *options, format string, values ...any) {
	if opts.quiet {
		return
	}
	fmt.Fprintf(os.Stderr, "[p2p-nc] "+format+"\n", values...)
}

func printNodeInfo(opts *options, node *p2pnode.Node, label string) {
	payload := struct {
		PeerID    string   `json:"peerId"`
		Addresses []string `json:"addresses"`
	}{PeerID: node.Host.ID().String(), Addresses: node.Addresses()}
	if opts.json {
		encoded, _ := json.Marshal(payload)
		diagnostic(opts, "%s", encoded)
		return
	}
	diagnostic(opts, "%s PeerId: %s", label, payload.PeerID)
	for _, address := range payload.Addresses {
		diagnostic(opts, "address: %s", address)
	}
}

func parseService(text string) (uint16, error) {
	value, err := strconv.ParseUint(text, 10, 16)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("logical port must be an integer between 1 and 65535: %s", text)
	}
	return uint16(value), nil
}

func loadToken(opts *options) (*pairing.Token, error) {
	sources := make(map[string]string)
	if value := strings.TrimSpace(opts.pairingToken); value != "" {
		sources["--pairing-token"] = value
	}
	if path := strings.TrimSpace(opts.pairingTokenFile); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		sources["--pairing-token-file"] = strings.TrimSpace(string(data))
	}
	if value := strings.TrimSpace(os.Getenv("P2P_NETCAT_TOKEN")); value != "" {
		sources["P2P_NETCAT_TOKEN"] = value
	}
	if len(sources) == 0 {
		return nil, nil
	}
	var selected string
	for label, value := range sources {
		if selected != "" && selected != value {
			return nil, fmt.Errorf("pairing token differs between sources, including %s", label)
		}
		selected = value
	}
	token, err := pairing.Decode(selected)
	if err != nil {
		return nil, err
	}
	if err := token.Validate(time.Now()); err != nil {
		return nil, err
	}
	return token, nil
}

func validateTokenFor(token *pairing.Token, expected peer.ID, service uint16) error {
	if token.PeerID != expected {
		return fmt.Errorf("pairing token belongs to PeerId %s, not %s", token.PeerID, expected)
	}
	if token.Service != service {
		return fmt.Errorf("pairing token belongs to logical port %d, not %d", token.Service, service)
	}
	return token.Validate(time.Now())
}

func peerIDFromTarget(target string) (peer.ID, error) {
	if !strings.HasPrefix(target, "/") {
		return peer.Decode(target)
	}
	address, err := ma.NewMultiaddr(target)
	if err != nil {
		return "", err
	}
	info, err := peer.AddrInfoFromP2pAddr(address)
	if err != nil {
		return "", errors.New("full multiaddr must contain /p2p/PeerId")
	}
	return info.ID, nil
}

func relayStrings(token *pairing.Token) []string {
	result := make([]string, 0, len(token.RelayHints))
	for _, value := range token.RelayHints {
		result = append(result, value.String())
	}
	return result
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func QuietRequested(args []string) bool {
	return booleanShortOptionRequested(args, 'q', "quiet")
}

func TorRequested(args []string) bool {
	return booleanShortOptionRequested(args, 'T', "tor")
}

func booleanShortOptionRequested(args []string, shorthand byte, longhand string) bool {
	skipNext := false
	for _, value := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if value == "--"+longhand || value == "-"+string(shorthand) {
			return true
		}
		switch value {
		case "-I", "-w", "-d", "-p", "-e", "--identity", "--timeout",
			"--destination", "--port", "--exec":
			skipNext = true
			continue
		}
		if len(value) < 3 || !strings.HasPrefix(value, "-") || strings.HasPrefix(value, "--") {
			continue
		}
		if strings.ContainsRune("Iwdpe", rune(value[1])) {
			continue
		}
		if strings.ContainsRune(value[1:], rune(shorthand)) {
			return true
		}
	}
	return false
}

func newIDCommand() *cobra.Command {
	var path string
	command := &cobra.Command{
		Use:   "id",
		Short: "show the persistent PeerId",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if path == "" {
				path = identity.DefaultPath()
			}
			key, err := identity.LoadOrCreate(path)
			if err != nil {
				return err
			}
			id, err := peer.IDFromPrivateKey(key)
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, id)
			return nil
		},
	}
	command.Flags().StringVarP(&path, "identity", "I", "", "persistent private key file")
	return command
}

func newTokenCommand() *cobra.Command {
	var path string
	var relays []string
	var expiresIn int
	command := &cobra.Command{
		Use:   "token [logical-port]",
		Short: "create a private pairing token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(command *cobra.Command, args []string) error {
			if path == "" {
				path = identity.DefaultPath()
			}
			key, err := identity.LoadOrCreate(path)
			if err != nil {
				return err
			}
			id, err := peer.IDFromPrivateKey(key)
			if err != nil {
				return err
			}
			service := p2pnode.DefaultService
			if len(args) == 1 {
				service, err = parseService(args[0])
				if err != nil {
					return err
				}
			}
			addresses := make([]ma.Multiaddr, 0, len(relays))
			for _, value := range relays {
				address, parseErr := ma.NewMultiaddr(value)
				if parseErr != nil {
					return parseErr
				}
				addresses = append(addresses, address)
			}
			var expires *uint64
			if expiresIn < 0 || (command.Flags().Changed("expires-in") && expiresIn == 0) {
				return errors.New("--expires-in must be greater than zero")
			}
			if expiresIn > 0 {
				value := uint64(time.Now().Unix() + int64(expiresIn))
				expires = &value
			}
			token, err := pairing.New(id, service, addresses, expires)
			if err != nil {
				return err
			}
			encoded, err := token.Encode()
			if err != nil {
				return err
			}
			fmt.Fprintln(os.Stdout, encoded)
			return nil
		},
	}
	command.Flags().StringVarP(&path, "identity", "I", "", "persistent private key file")
	command.Flags().StringSliceVar(&relays, "relay", nil, "add a relay hint")
	command.Flags().IntVar(&expiresIn, "expires-in", 0, "token lifetime in seconds")
	return command
}

type relayOptions struct {
	localPort     int
	websocketPort int
	ipv4          bool
	ipv6          bool
	identity      string
	announce      []string
	noMDNS        bool
	noPubsub      bool
	noQUIC        bool
	json          bool
	verbose       bool
}

func newRelayCommand() *cobra.Command {
	opts := &relayOptions{}
	command := &cobra.Command{
		Use:   "relay",
		Short: "run a Circuit Relay v2 server",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.ipv4 && opts.ipv6 {
				return errors.New("-4 and -6 cannot be used together")
			}
			if opts.localPort < 0 || opts.localPort > 65535 || opts.websocketPort < 1 || opts.websocketPort > 65535 {
				return errors.New("invalid relay port")
			}
			path := opts.identity
			if path == "" {
				path = identity.DefaultPath() + ".relay"
			}
			ipVersion := 0
			if opts.ipv4 {
				ipVersion = 4
			} else if opts.ipv6 {
				ipVersion = 6
			}
			relay, err := relayserver.Start(command.Context(), relayserver.Options{
				IdentityPath:  path,
				LocalPort:     opts.localPort,
				WebsocketPort: opts.websocketPort,
				IPVersion:     ipVersion,
				Announce:      opts.announce,
				NoMDNS:        opts.noMDNS,
				NoPubSub:      opts.noPubsub,
				NoQUIC:        opts.noQUIC,
				Verbose:       opts.verbose,
			})
			if err != nil {
				return err
			}
			defer relay.Stop()
			common := &options{json: opts.json, verbose: opts.verbose}
			printNodeInfo(common, relay.Node, "relay")
			diagnostic(common, "relay ready; persistent key: %s", path)
			<-command.Context().Done()
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.localPort, "local-port", "p", 9090, "public relay TCP/UDP port")
	flags.IntVar(&opts.websocketPort, "websocket-port", 9091, "WebSocket port")
	flags.BoolVarP(&opts.ipv4, "ipv4", "4", false, "IPv4 only")
	flags.BoolVarP(&opts.ipv6, "ipv6", "6", false, "IPv6 only")
	flags.StringVarP(&opts.identity, "identity", "I", "", "persistent key file")
	flags.StringSliceVar(&opts.announce, "announce", nil, "public relay multiaddr")
	flags.BoolVar(&opts.noMDNS, "no-mdns", false, "disable mDNS")
	flags.BoolVar(&opts.noPubsub, "no-pubsub", false, "disable PubSub discovery")
	flags.BoolVar(&opts.noQUIC, "no-quic", false, "disable QUIC")
	flags.BoolVar(&opts.json, "json", false, "write JSON to stderr")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "enable verbose diagnostics")
	return command
}
