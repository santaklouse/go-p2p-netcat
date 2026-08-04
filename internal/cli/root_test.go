package cli

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/santaklouse/go-p2p-netcat/session"
	"github.com/spf13/cobra"
)

func TestPreconnectedStreamOpenerUsesFirstCarrierExactlyOnce(t *testing.T) {
	first := &testStream{}
	var fallbackCalls atomic.Int32
	fallback := func(context.Context) (session.Stream, error) {
		fallbackCalls.Add(1)
		return &testStream{}, nil
	}
	opener := newPreconnectedStreamOpener(first, fallback)

	const callers = 16
	results := make(chan session.Stream, callers)
	var group sync.WaitGroup
	for range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			stream, err := opener.Open(context.Background())
			if err != nil {
				t.Errorf("Open: %v", err)
				return
			}
			results <- stream
		}()
	}
	group.Wait()
	close(results)
	firstCount := 0
	for stream := range results {
		if stream == first {
			firstCount++
		}
	}
	if firstCount != 1 {
		t.Fatalf("preconnected carrier returned %d times, want 1", firstCount)
	}
	if got := fallbackCalls.Load(); got != callers-1 {
		t.Fatalf("fallback calls = %d, want %d", got, callers-1)
	}
	if err := opener.Close(); err != nil {
		t.Fatal(err)
	}
	if first.closed.Load() {
		t.Fatal("consumed preconnected carrier was closed by opener")
	}
}

func TestPreconnectedStreamOpenerClosesUnusedCarrier(t *testing.T) {
	first := &testStream{}
	opener := newPreconnectedStreamOpener(first, func(context.Context) (session.Stream, error) {
		return nil, errors.New("unexpected fallback")
	})
	if err := opener.Close(); err != nil {
		t.Fatal(err)
	}
	if !first.closed.Load() {
		t.Fatal("unused preconnected carrier was not closed")
	}
}

type testStream struct {
	closed atomic.Bool
}

func (*testStream) Read([]byte) (int, error)        { return 0, io.EOF }
func (*testStream) Write(value []byte) (int, error) { return len(value), nil }
func (stream *testStream) Close() error {
	stream.closed.Store(true)
	return nil
}
func (*testStream) CloseWrite() error { return nil }
func (*testStream) Reset() error      { return nil }

func TestCombinedBooleanShortOptions(t *testing.T) {
	for _, test := range []struct {
		args  []string
		quiet bool
		tor   bool
	}{
		{args: []string{"-Tq", "--relay", "/ip4/127.0.0.1/tcp/1"}, quiet: true, tor: true},
		{args: []string{"-qT"}, quiet: true, tor: true},
		{args: []string{"-vTq"}, quiet: true, tor: true},
		{args: []string{"-I", "-Tq"}, quiet: false, tor: false},
		{args: []string{"--quiet"}, quiet: true, tor: false},
	} {
		if got := QuietRequested(test.args); got != test.quiet {
			t.Errorf("QuietRequested(%q) = %v, want %v", test.args, got, test.quiet)
		}
		if got := TorRequested(test.args); got != test.tor {
			t.Errorf("TorRequested(%q) = %v, want %v", test.args, got, test.tor)
		}
	}
}

func TestNodeConfigHonorsDiscoveryAndTorFlags(t *testing.T) {
	key, _, err := crypto.GenerateEd25519Key(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	normal := nodeConfig(&options{}, key, true, true, false, false)
	if !normal.EnableDHT || !normal.EnableMDNS || !normal.EnablePubSub ||
		!normal.PubSubDiscover || !normal.EnableQUIC || !normal.EnableWebRTC {
		t.Fatalf("normal config unexpectedly disabled: %+v", normal)
	}
	private := nodeConfig(&options{}, key, true, true, false, true)
	if private.PubSubDiscover {
		t.Fatal("pairing mode must not advertise through public pubsub discovery")
	}
	tor := nodeConfig(&options{tor: true}, key, false, false, false, false)
	if tor.EnableDHT || tor.EnableMDNS || tor.EnablePubSub || tor.EnableQUIC || tor.EnableWebRTC {
		t.Fatalf("Tor config left a direct/discovery transport enabled: %+v", tor)
	}
}

func TestRootRejectsInvalidAndUnsupportedCombinations(t *testing.T) {
	for _, args := range [][]string{
		{"-u"},
		{"-u", "-p", "51820", "-i"},
		{"-u", "-p", "51820", "-z"},
		{"-u", "-p", "51820", "--udp-idle-timeout", "-1"},
		{"--udp-idle-timeout", "0"},
		{"-w", "0"},
		{"-p", "0"},
		{"-4", "-6"},
		{"-T"},
		{"-l", "-S", "-p", "8080"},
		{"-i", "-e", "true", "-l"},
		{"-k", "12D3KooWQZPLb65sQXvujFQ1oRyx62arxnzT4rMTtdQWxSurvNq2", "12345"},
		{"-l", "-z", "12345"},
		{"-l", "-i", "-p", "22", "12345"},
		{"-l", "-e", "true", "-p", "22", "12345"},
		{"-l", "-S", "-e", "true", "12345"},
		{"--quit-delay", "1", "-p", "12345", "12D3KooWQZPLb65sQXvujFQ1oRyx62arxnzT4rMTtdQWxSurvNq2", "12345"},
	} {
		command := NewRoot()
		command.SetContext(context.Background())
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Errorf("NewRoot().Execute(%q) succeeded, want error", args)
		}
	}
}

func TestValidateModeMatrix(t *testing.T) {
	tests := []struct {
		name    string
		opts    options
		args    []string
		changed []string
	}{
		{name: "listener raw", opts: options{listen: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"10001"}},
		{name: "listener keep-open", opts: options{listen: true, keepOpen: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"10002"}},
		{name: "listener command", opts: options{listen: true, exec: "printf ok", timeout: 60, udpIdleTimeout: 300}, args: []string{"10003"}},
		{name: "listener PTY", opts: options{listen: true, interactive: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"10004"}},
		{name: "listener SOCKS", opts: options{listen: true, socks: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"10005"}},
		{name: "listener TCP forward", opts: options{listen: true, port: 22, destination: "127.0.0.1", timeout: 60, udpIdleTimeout: 300}, args: []string{"10006"}, changed: []string{"port"}},
		{name: "listener UDP forward", opts: options{listen: true, keepOpen: true, udp: true, port: 51820, destination: "127.0.0.1", timeout: 60, udpIdleTimeout: 0}, args: []string{"10007"}, changed: []string{"port", "udp-idle-timeout"}},
		{name: "client raw", opts: options{timeout: 60, udpIdleTimeout: 300}, args: []string{"peer", "10001"}},
		{name: "client raw quit delay", opts: options{timeout: 60, quitDelay: 1, udpIdleTimeout: 300}, args: []string{"peer", "10001"}},
		{name: "client zero", opts: options{zero: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"peer", "10001"}},
		{name: "client PTY", opts: options{interactive: true, timeout: 60, udpIdleTimeout: 300}, args: []string{"peer", "10004"}},
		{name: "client TCP forward", opts: options{port: 10022, timeout: 60, udpIdleTimeout: 300}, args: []string{"peer", "10006"}, changed: []string{"port"}},
		{name: "client UDP forward", opts: options{udp: true, port: 15182, timeout: 60, udpIdleTimeout: 0}, args: []string{"peer", "10007"}, changed: []string{"port", "udp-idle-timeout"}},
		{name: "client Tor relay", opts: options{tor: true, relays: []string{"/ip4/127.0.0.1/tcp/9090/p2p/peer"}, timeout: 60, udpIdleTimeout: 300}, args: []string{"peer", "10001"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := &cobra.Command{}
			command.Flags().Int("port", 0, "")
			command.Flags().Int("udp-idle-timeout", 300, "")
			for _, flag := range test.changed {
				value := "1"
				if flag == "port" {
					value = strconv.Itoa(test.opts.port)
				} else if flag == "udp-idle-timeout" {
					value = strconv.Itoa(test.opts.udpIdleTimeout)
				}
				if err := command.Flags().Set(flag, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := validateOptions(command, &test.opts, test.args); err != nil {
				t.Fatalf("valid mode rejected: %v", err)
			}
		})
	}
}

func TestValidateUDPForwardingOptions(t *testing.T) {
	command := &cobra.Command{}
	command.Flags().Int("port", 0, "")
	if err := command.Flags().Set("port", "51820"); err != nil {
		t.Fatal(err)
	}
	for _, opts := range []*options{
		{
			listen:         true,
			udp:            true,
			port:           51820,
			timeout:        60,
			udpIdleTimeout: 300,
		},
		{
			udp:            true,
			port:           51820,
			timeout:        60,
			udpIdleTimeout: 0,
			tor:            true,
			relays:         []string{"/ip4/127.0.0.1/tcp/9090"},
		},
	} {
		if err := validateOptions(command, opts, []string{"31337"}); err != nil {
			t.Fatalf("validateOptions(%+v): %v", opts, err)
		}
	}
}

func TestNativeWebRTCEnabledForUDPForwarding(t *testing.T) {
	if !nativeWebRTCEnabled(&options{udp: true}) {
		t.Fatal("UDP forwarding must enable native WebRTC NAT traversal")
	}
	if nativeWebRTCEnabled(&options{udp: true, noWebRTC: true}) {
		t.Fatal("--no-webrtc must disable native WebRTC for UDP forwarding")
	}
	if nativeWebRTCEnabled(&options{udp: true, tor: true}) {
		t.Fatal("Tor mode must disable native WebRTC for UDP forwarding")
	}
}

func TestServiceParsing(t *testing.T) {
	for _, valid := range []string{"1", "80", "65535"} {
		if _, err := parseService(valid); err != nil {
			t.Errorf("parseService(%q): %v", valid, err)
		}
	}
	for _, invalid := range []string{"", "0", "65536", "-1", "http"} {
		if _, err := parseService(invalid); err == nil {
			t.Errorf("parseService(%q) succeeded", invalid)
		}
	}
}
