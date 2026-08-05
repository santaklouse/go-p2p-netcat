package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseArgumentsProfilesAndOverrides(t *testing.T) {
	parsed, err := parseArguments([]string{
		"--", "--profile", "ci", "--iterations", "3", "--payload-bytes", "4096",
		"--scenarios", "nostr-trickle,torrent-full-sdp,nostr-trickle",
	}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Profile != "ci" || parsed.Iterations != 3 || parsed.PayloadBytes != 4096 {
		t.Fatalf("parsed options = %+v", parsed)
	}
	wantScenarios := []string{"nostr-trickle", "torrent-full-sdp"}
	if !reflect.DeepEqual(parsed.Scenarios, wantScenarios) {
		t.Fatalf("scenarios = %q, want %q", parsed.Scenarios, wantScenarios)
	}

	defaults, err := parseArguments([]string{"--profile", "soak"}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Iterations != 12 || defaults.PayloadBytes != 8*1024*1024 {
		t.Fatalf("soak defaults = %+v", defaults)
	}
}

func TestRunRejectsInvalidConfiguration(t *testing.T) {
	for _, arguments := range [][]string{
		{"--profile", "unknown"},
		{"--iterations", "0"},
		{"--iterations", "-1"},
		{"--payload-bytes", "0"},
		{"--payload-bytes", "-1"},
		{"--scenarios", ""},
		{"--scenarios", "missing"},
		{"unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(arguments, &stdout, &stderr); code == 0 {
			t.Fatalf("run(%q) exit code = 0", arguments)
		}
		if !strings.Contains(stderr.String(), "[soak] fatal:") {
			t.Fatalf("run(%q) stderr = %q", arguments, stderr.String())
		}
	}
}

func TestHelpUsesStdoutAndSuccessExitCode(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("help exit code = %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage: go run ./cmd/webrtc-soak") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestSmokeMatrixWritesVerifiedJSONReport(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "reports", "webrtc-soak.json")
	var stdout bytes.Buffer
	err := execute(options{
		Profile: "smoke", Iterations: 1, PayloadBytes: 64 * 1024,
		Report: reportPath,
	}, &stdout)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var report soakReport
	if err := json.Unmarshal(encoded, &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Passed != len(scenarios) || report.Summary.Failed != 0 {
		t.Fatalf("report summary = %+v", report.Summary)
	}
	if report.Summary.TransferredBytes != 12*64*1024 {
		t.Fatalf("transferred bytes = %d", report.Summary.TransferredBytes)
	}
	if len(report.Results) != len(scenarios) {
		t.Fatalf("results = %+v", report.Results)
	}
	for _, result := range report.Results {
		if result.Strategy == "" || result.Signaling == nil {
			t.Fatalf("incomplete result = %+v", result)
		}
		if result.Scenario == "reconnect-same-stream" && result.TransferredBytes != 4*64*1024 {
			t.Fatalf("reconnect transferred bytes = %d", result.TransferredBytes)
		}
	}
	if !strings.Contains(stdout.String(), "[soak] passed=5 failed=0") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestDeterministicPayload(t *testing.T) {
	first := deterministicPayload(1024, 12345)
	second := deterministicPayload(1024, 12345)
	other := deterministicPayload(1024, 12346)
	if !bytes.Equal(first, second) {
		t.Fatal("same seed generated different payloads")
	}
	if bytes.Equal(first, other) {
		t.Fatal("different seeds generated identical payloads")
	}
}
