package listenerlock

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

const helperEnvironment = "P2P_NETCAT_LISTENER_LOCK_HELPER"

func TestAcquireRejectsDuplicateAndReleasesPort(t *testing.T) {
	t.Setenv(DirectoryEnvironment, t.TempDir())
	if _, err := Acquire(0); err == nil {
		t.Fatal("logical port zero was accepted")
	}
	first, err := Acquire(41001)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(41001); err == nil || !strings.Contains(err.Error(), "already has an active listener") {
		t.Fatalf("duplicate Acquire error = %v", err)
	}

	other, err := Acquire(41002)
	if err != nil {
		t.Fatalf("different logical port was rejected: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Acquire(41001)
	if err != nil {
		t.Fatalf("released logical port was not reusable: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAcquireRejectsAnotherProcess(t *testing.T) {
	directory := t.TempDir()
	t.Setenv(DirectoryEnvironment, directory)
	lock, err := Acquire(41003)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()

	command := exec.Command(os.Args[0], "-test.run=^TestListenerLockHelperProcess$")
	command.Env = append(os.Environ(), helperEnvironment+"=1", DirectoryEnvironment+"="+directory)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	err = command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 23 {
		t.Fatalf("helper error = %v, stderr = %q", err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "logical port 41003 already has an active listener") {
		t.Fatalf("unexpected helper stderr: %q", stderr.String())
	}
}

func TestListenerLockHelperProcess(t *testing.T) {
	if os.Getenv(helperEnvironment) != "1" {
		return
	}
	lock, err := Acquire(41003)
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error())
		os.Exit(23)
	}
	_ = lock.Close()
	os.Exit(0)
}
