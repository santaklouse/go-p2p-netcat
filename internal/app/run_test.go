package app

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/santaklouse/go-p2p-netcat/internal/listenerlock"
)

func TestVersionWorksForBothCommandAliases(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-V"}, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "p2p-nc version") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestInvalidOptionsReturnFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run([]string{"-u"}, strings.NewReader(""), &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "requires -p/--port") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestDuplicateLogicalPortReturnsFailureForEveryListenerMode(t *testing.T) {
	t.Setenv(listenerlock.DirectoryEnvironment, t.TempDir())
	const service = uint16(41234)
	lock, err := listenerlock.Acquire(service)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	port := strconv.Itoa(int(service))
	tests := []struct {
		name  string
		args  []string
		quiet bool
	}{
		{name: "raw", args: []string{"-l", port}},
		{name: "keep-open", args: []string{"-l", "-k", port}},
		{name: "PTY", args: []string{"-l", "-i", "--allow-unauthenticated-listener", port}},
		{name: "exec", args: []string{"-l", "-e", "true", "--allow-unauthenticated-listener", port}},
		{name: "SOCKS", args: []string{"-l", "-S", "--allow-unauthenticated-listener", port}},
		{name: "TCP forwarding", args: []string{"-l", "-p", "22", "--allow-unauthenticated-listener", port}},
		{name: "UDP forwarding", args: []string{"-l", "-u", "-p", "51820", "--allow-unauthenticated-listener", port}},
		{name: "quiet", args: []string{"-q", "-l", port}, quiet: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(test.args, strings.NewReader(""), &stdout, &stderr); code == 0 {
				t.Fatalf("Run(%q) exit code = 0, want non-zero", test.args)
			}
			if test.quiet {
				if stderr.Len() != 0 {
					t.Fatalf("quiet stderr = %q", stderr.String())
				}
				return
			}
			if !strings.Contains(stderr.String(), "logical port 41234 already has an active listener") {
				t.Fatalf("stderr = %q", stderr.String())
			}
		})
	}
}
