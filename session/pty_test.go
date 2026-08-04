//go:build !windows

package session

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	ptyframe "github.com/santaklouse/go-p2p-netcat/protocol/pty"
)

func TestPTYServerTreatsCtrlDAsGracefulEOF(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	serverConnection, clientConnection := tcpStreamPair(t)
	serverStream := &trackingTCPStream{TCPConn: serverConnection}
	clientStream := &trackingTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		serverErr <- PTYServer(ctx, serverStream, false)
	}()

	// Ctrl-D at an empty canonical input line asks the login shell to exit.
	if err := ptyframe.WriteFrame(clientStream, ptyframe.FrameData, []byte{0x04}); err != nil {
		t.Fatal(err)
	}
	for {
		_, err := ptyframe.ReadFrame(clientStream)
		if err == nil {
			continue
		}
		if !errors.Is(err, io.EOF) {
			t.Fatalf("client read after Ctrl-D: %v", err)
		}
		break
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("PTYServer returned an error for normal Ctrl-D: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("PTYServer did not finish after Ctrl-D")
	}
	if !serverStream.writeClosed.Load() {
		t.Fatal("PTYServer did not gracefully close its write side")
	}
}

func TestPTYServerClientHalfCloseTerminatesShell(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	serverConnection, clientConnection := tcpStreamPair(t)
	serverStream := &trackingTCPStream{TCPConn: serverConnection}
	clientStream := &trackingTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	serverErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		serverErr <- PTYServer(ctx, serverStream, false)
	}()
	if err := clientStream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-serverErr:
		if err != nil {
			t.Fatalf("PTYServer returned an error for a client half-close: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("PTYServer did not terminate its shell after the client half-closed")
	}
}

type trackingTCPStream struct {
	*net.TCPConn
	writeClosed atomic.Bool
}

func (stream *trackingTCPStream) CloseWrite() error {
	stream.writeClosed.Store(true)
	return stream.TCPConn.CloseWrite()
}

func (stream *trackingTCPStream) Reset() error {
	return stream.Close()
}

func tcpStreamPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan *net.TCPConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- connection
	}()
	client, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case server := <-accepted:
		return server, client
	case err := <-acceptErr:
		client.Close()
		t.Fatal(err)
	case <-time.After(3 * time.Second):
		client.Close()
		t.Fatal("timed out accepting the test TCP connection")
	}
	return nil, nil
}
