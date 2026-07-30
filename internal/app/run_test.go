package app

import (
	"bytes"
	"strings"
	"testing"
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
	if !strings.Contains(stderr.String(), "не поддерживается") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
