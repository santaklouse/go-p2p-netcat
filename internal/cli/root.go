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
	p2pnode "github.com/santaklouse/go-p2p-netcat/p2p"
	"github.com/santaklouse/go-p2p-netcat/protocol/admission"
	"github.com/santaklouse/go-p2p-netcat/protocol/pairing"
	"github.com/santaklouse/go-p2p-netcat/session"
	"github.com/spf13/cobra"
)

var Version = "0.1.0"

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
		Use:           "p2p-nc [опции] [PeerId|multiaddr] [логический-порт]",
		Short:         "Netcat-подобный P2P-клиент на libp2p/IPFS",
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
	flags.BoolVarP(&opts.listen, "listen", "l", false, "режим сервера")
	flags.BoolVarP(&opts.keepOpen, "keep-open", "k", false, "продолжать принимать подключения")
	flags.IntVarP(&opts.timeout, "timeout", "w", 60, "таймаут поиска/подключения в секундах")
	flags.IntVar(&opts.quitDelay, "quit-delay", 0, "задержка закрытия после EOF stdin в секундах")
	flags.StringVarP(&opts.destination, "destination", "d", "", "адрес назначения TCP forwarding на сервере")
	flags.IntVarP(&opts.port, "port", "p", 0, "порт назначения с -l или локальный listen-порт клиента")
	flags.BoolVarP(&opts.quiet, "quiet", "q", false, "не выводить диагностику")
	flags.BoolVarP(&opts.socks, "socks", "S", false, "SOCKS4/4a/5 server на удалённой стороне")
	flags.BoolVarP(&opts.tor, "tor", "T", false, "подключаться к relay через Tor/torsocks")
	flags.BoolVarP(&opts.interactive, "interactive", "i", false, "интерактивный PTY login shell")
	flags.BoolVarP(&opts.zero, "zero", "z", false, "только проверить соединение")
	flags.StringVarP(&opts.exec, "exec", "e", "", "подключить серверный поток к shell-команде")
	flags.BoolVarP(&opts.udp, "udp", "u", false, "UDP-режим (не поддерживается)")
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
	flags.IntVar(&opts.transportPort, "transport-port", 0, "локальный TCP/UDP-порт libp2p")
	flags.BoolVarP(&opts.ipv4, "ipv4", "4", false, "слушать только IPv4")
	flags.BoolVarP(&opts.ipv6, "ipv6", "6", false, "слушать только IPv6")
	flags.StringVarP(&opts.identity, "identity", "I", "", "файл постоянного приватного ключа")
	flags.StringSliceVar(&opts.relays, "relay", nil, "Circuit Relay v2 multiaddr; можно повторять")
	flags.StringSliceVar(&opts.bootstrap, "bootstrap", nil, "заменить стандартные IPFS bootstrap-узлы")
	flags.StringSliceVar(&opts.announce, "announce", nil, "публичный объявляемый multiaddr")
	flags.BoolVar(&opts.noDHT, "no-dht", false, "отключить IPFS Amino DHT")
	flags.BoolVar(&opts.noMDNS, "no-mdns", false, "отключить mDNS")
	flags.BoolVar(&opts.noPubsub, "no-pubsub", false, "отключить PubSub discovery")
	flags.BoolVar(&opts.noQUIC, "no-quic", false, "отключить QUIC")
	flags.BoolVar(&opts.noWebRTC, "no-webrtc", false, "отключить WebRTC-direct")
	flags.StringVar(&opts.pairingToken, "pairing-token", "", "приватный pairing token pnc1_...")
	flags.StringVar(&opts.pairingTokenFile, "pairing-token-file", "", "прочитать pairing token из файла")
	flags.StringVar(&opts.bind, "bind", "127.0.0.1", "локальный адрес для -p forwarding")
	flags.BoolVar(&opts.json, "json", false, "выводить сведения об узле как JSON в stderr")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "подробная диагностика")
}

func validateOptions(command *cobra.Command, opts *options, args []string) error {
	if opts.timeout <= 0 {
		return errors.New("-w/--timeout должен быть больше нуля")
	}
	if opts.quitDelay < 0 || opts.transportPort < 0 || opts.transportPort > 65535 {
		return errors.New("порты и задержки не могут быть отрицательными")
	}
	if opts.port < 0 || opts.port > 65535 {
		return errors.New("-p/--port должен быть от 1 до 65535")
	}
	if opts.ipv4 && opts.ipv6 {
		return errors.New("опции -4 и -6 нельзя использовать одновременно")
	}
	if opts.udp {
		return errors.New("-u пока не поддерживается: протокол передаёт надёжный двунаправленный поток")
	}
	if opts.exec != "" && !opts.listen {
		return errors.New("-e доступна только вместе с -l")
	}
	if opts.destination != "" && (!opts.listen || opts.port == 0) {
		return errors.New("-d доступна только вместе с -l -p <порт>")
	}
	if opts.socks && !opts.listen {
		return errors.New("-S доступна только вместе с -l")
	}
	if opts.socks && (opts.destination != "" || opts.port != 0) {
		return errors.New("-S несовместима с -d/-p на сервере")
	}
	if opts.interactive && (opts.exec != "" || opts.socks) {
		return errors.New("-i несовместима с -e и -S")
	}
	if !opts.listen && opts.port != 0 && (opts.interactive || opts.zero) {
		return errors.New("клиентская -p несовместима с -i и -z")
	}
	if opts.tor && opts.listen {
		return errors.New("-T поддерживается только в клиентском режиме")
	}
	if opts.tor && len(opts.relays) == 0 {
		return errors.New("-T требует явный --relay")
	}
	for _, relay := range opts.relays {
		if opts.tor && strings.Contains(relay, "/udp/") {
			return errors.New("Tor не переносит QUIC/UDP; используйте TCP/WS/WSS relay")
		}
	}
	if opts.listen && len(args) > 1 {
		return errors.New("в режиме -l укажите только логический порт: p2p-nc -l 8080")
	}
	_ = command
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
	node, err := p2pnode.New(ctx, nodeConfig(opts, privateKey, true, true, false))
	if err != nil {
		return err
	}
	defer node.Close()
	node.Advertise(ctx, token)

	persistent := opts.keepOpen || opts.interactive || opts.socks || opts.port != 0
	result := make(chan error, 1)
	var acceptedMu sync.Mutex
	accepted := false
	handler := func(stream network.Stream) {
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
			diagnostic(opts, "peer %s подключен к логическому порту %d", stream.Conn().RemotePeer(), service)
			sessionErr := runServerSession(ctx, stream, opts)
			if persistent {
				if sessionErr != nil {
					diagnostic(opts, "сеанс завершён с ошибкой: %v", sessionErr)
				}
				return
			}
			select {
			case result <- sessionErr:
			default:
			}
		}()
	}
	node.Host.SetStreamHandler(p2pnode.ProtocolForService(service), handler)
	printNodeInfo(opts, node, fmt.Sprintf("слушатель:%d", service))
	diagnostic(opts, "постоянный ключ: %s", identityPath)
	if token != nil {
		diagnostic(opts, "приватный pairing-token режим включён")
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
		return errors.New("не указан PeerId; пример: p2p-nc 12D3KooW... 8080")
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
	node, err := p2pnode.New(ctx, nodeConfig(opts, privateKey, true, false, false))
	if err != nil {
		return err
	}
	defer node.Close()
	timeout := time.Duration(opts.timeout) * time.Second
	openStream := func(openCtx context.Context) (session.Stream, error) {
		dialCtx, cancel := context.WithTimeout(openCtx, timeout)
		defer cancel()
		stream, openErr := node.OpenStream(dialCtx, target, service, opts.relays, token)
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
		printNodeInfo(opts, node, "клиент")
	}
	if opts.port != 0 {
		listener, err := session.StartLocalForward(ctx, opts.bind, opts.port, openStream, func(value error) {
			diagnostic(opts, "TCP forwarding session: %v", value)
		})
		if err != nil {
			return err
		}
		defer listener.Close()
		diagnostic(opts, "локальный TCP %s -> %s:%d", listener.Addr(), target, service)
		<-ctx.Done()
		return nil
	}
	stream, err := openStream(ctx)
	if err != nil {
		return fmt.Errorf("ни один маршрут не установил соединение: %w", err)
	}
	defer stream.Close()
	if opts.verbose || opts.zero {
		diagnostic(opts, "соединение с %s:%d установлено", target, service)
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

func runServerSession(ctx context.Context, stream session.Stream, opts *options) error {
	timeout := time.Duration(opts.timeout) * time.Second
	switch {
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
		return session.Bridge(ctx, stream, os.Stdin, os.Stdout, time.Duration(opts.quitDelay)*time.Second, 0)
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
		diagnostic(opts, "адрес: %s", address)
	}
}

func parseService(text string) (uint16, error) {
	value, err := strconv.ParseUint(text, 10, 16)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("логический порт должен быть целым числом от 1 до 65535: %s", text)
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
			return nil, fmt.Errorf("pairing token различается между источниками, включая %s", label)
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
		return fmt.Errorf("pairing token принадлежит PeerId %s, а не %s", token.PeerID, expected)
	}
	if token.Service != service {
		return fmt.Errorf("pairing token принадлежит логическому порту %d, а не %d", token.Service, service)
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
		return "", errors.New("полный multiaddr должен содержать /p2p/PeerId")
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
	for _, value := range args {
		if value == "-q" || value == "--quiet" || (strings.HasPrefix(value, "-") && !strings.HasPrefix(value, "--") && strings.Contains(value, "q")) {
			return true
		}
	}
	return false
}

func newIDCommand() *cobra.Command {
	var path string
	command := &cobra.Command{
		Use:   "id",
		Short: "показать постоянный PeerId",
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
	command.Flags().StringVarP(&path, "identity", "I", "", "файл постоянного приватного ключа")
	return command
}

func newTokenCommand() *cobra.Command {
	var path string
	var relays []string
	var expiresIn int
	command := &cobra.Command{
		Use:   "token [логический-порт]",
		Short: "создать приватный pairing token",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
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
			if expiresIn < 0 {
				return errors.New("--expires-in должен быть больше нуля")
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
	command.Flags().StringVarP(&path, "identity", "I", "", "файл постоянного приватного ключа")
	command.Flags().StringSliceVar(&relays, "relay", nil, "добавить relay hint")
	command.Flags().IntVar(&expiresIn, "expires-in", 0, "срок действия token в секундах")
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
		Short: "запустить Circuit Relay v2",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if opts.ipv4 && opts.ipv6 {
				return errors.New("-4 и -6 нельзя использовать одновременно")
			}
			if opts.localPort < 0 || opts.localPort > 65535 || opts.websocketPort < 1 || opts.websocketPort > 65535 {
				return errors.New("некорректный relay port")
			}
			path := opts.identity
			if path == "" {
				path = identity.DefaultPath() + ".relay"
			}
			key, err := identity.LoadOrCreate(path)
			if err != nil {
				return err
			}
			ipVersion := 0
			if opts.ipv4 {
				ipVersion = 4
			} else if opts.ipv6 {
				ipVersion = 6
			}
			node, err := p2pnode.New(command.Context(), p2pnode.Config{
				PrivateKey: key, TransportPort: opts.localPort, WebsocketPort: opts.websocketPort,
				IPVersion: ipVersion, Announce: opts.announce, EnableMDNS: !opts.noMDNS,
				EnableQUIC: !opts.noQUIC, EnableWebRTC: false, Listen: true, RelayServer: true,
				Verbose: opts.verbose,
			})
			if err != nil {
				return err
			}
			defer node.Close()
			common := &options{json: opts.json, verbose: opts.verbose}
			printNodeInfo(common, node, "relay")
			diagnostic(common, "relay готов; постоянный ключ: %s", path)
			<-command.Context().Done()
			return nil
		},
	}
	flags := command.Flags()
	flags.IntVarP(&opts.localPort, "local-port", "p", 9090, "публичный TCP/UDP-порт relay")
	flags.IntVar(&opts.websocketPort, "websocket-port", 9091, "WebSocket-порт")
	flags.BoolVarP(&opts.ipv4, "ipv4", "4", false, "только IPv4")
	flags.BoolVarP(&opts.ipv6, "ipv6", "6", false, "только IPv6")
	flags.StringVarP(&opts.identity, "identity", "I", "", "файл постоянного ключа")
	flags.StringSliceVar(&opts.announce, "announce", nil, "публичный relay multiaddr")
	flags.BoolVar(&opts.noMDNS, "no-mdns", false, "отключить mDNS")
	flags.BoolVar(&opts.noPubsub, "no-pubsub", false, "отключить PubSub discovery")
	flags.BoolVar(&opts.noQUIC, "no-quic", false, "отключить QUIC")
	flags.BoolVar(&opts.json, "json", false, "JSON в stderr")
	flags.BoolVarP(&opts.verbose, "verbose", "v", false, "подробная диагностика")
	return command
}
