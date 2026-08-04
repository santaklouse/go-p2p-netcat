//go:build !windows

package session

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestExecTransfersInputOutputAndExits(t *testing.T) {
	serverConnection, clientConnection := testTCPPair(t)
	serverStream := &testTCPStream{TCPConn: serverConnection}
	clientStream := &testTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	execErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		execErr <- Exec(ctx, serverStream, "printf 'ready:'; cat", false)
	}()
	if _, err := clientStream.Write([]byte("command-input")); err != nil {
		t.Fatal(err)
	}
	if err := clientStream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(clientStream)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(response), "ready:command-input") {
		t.Fatalf("command response = %q", response)
	}
	if err := <-execErr; err != nil {
		t.Fatal(err)
	}
}
