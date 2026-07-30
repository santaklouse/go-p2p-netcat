package cli

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/libp2p/go-libp2p/core/crypto"
)

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
		{"-w", "0"},
		{"-p", "0"},
		{"-4", "-6"},
		{"-T"},
		{"-l", "-S", "-p", "8080"},
		{"-i", "-e", "true", "-l"},
	} {
		command := NewRoot()
		command.SetContext(context.Background())
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Errorf("NewRoot().Execute(%q) succeeded, want error", args)
		}
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
