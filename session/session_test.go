package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBridgeTransfersBothDirectionsAndHalfCloses(t *testing.T) {
	serverConnection, clientConnection := testTCPPair(t)
	serverStream := &testTCPStream{TCPConn: serverConnection}
	clientStream := &testTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()
	var received bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bridgeErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		bridgeErr <- Bridge(ctx, serverStream, strings.NewReader("server-to-client"), &received, 0, 0)
	}()
	if _, err := clientStream.Write([]byte("client-to-server")); err != nil {
		t.Fatal(err)
	}
	if err := clientStream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(clientStream)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != "server-to-client" {
		t.Fatalf("response = %q", response)
	}
	if err := <-bridgeErr; err != nil {
		t.Fatal(err)
	}
	if received.String() != "client-to-server" {
		t.Fatalf("server received = %q", received.String())
	}
}

func TestTCPForwardEndToEnd(t *testing.T) {
	target := startTCPEchoServer(t)
	defer target.Close()
	serverConnection, clientConnection := testTCPPair(t)
	serverStream := &testTCPStream{TCPConn: serverConnection}
	clientStream := &testTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	forwardErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		forwardErr <- TCPForward(ctx, serverStream, "127.0.0.1", target.Addr().(*net.TCPAddr).Port, time.Second)
	}()
	assertStreamEcho(t, clientStream, []byte("TCP forwarding payload"))
	if err := <-forwardErr; err != nil {
		t.Fatal(err)
	}
}

func TestStartLocalForwardEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	openStream := func(context.Context) (Stream, error) {
		forwarder, remote := testTCPPair(t)
		go func() {
			defer remote.Close()
			_, _ = io.Copy(remote, remote)
			_ = remote.CloseWrite()
		}()
		return &testTCPStream{TCPConn: forwarder}, nil
	}
	errorsCh := make(chan error, 1)
	listener, err := StartLocalForward(ctx, "127.0.0.1", 0, openStream, func(err error) {
		errorsCh <- err
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	connection, err := net.DialTCP("tcp", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	assertStreamEcho(t, &testTCPStream{TCPConn: connection}, []byte("local TCP listener"))
	select {
	case err := <-errorsCh:
		t.Fatalf("forwarding error: %v", err)
	default:
	}
}

func TestSOCKS5ConnectEndToEnd(t *testing.T) {
	target := startTCPEchoServer(t)
	defer target.Close()
	serverConnection, clientConnection := testTCPPair(t)
	serverStream := &testTCPStream{TCPConn: serverConnection}
	clientStream := &testTCPStream{TCPConn: clientConnection}
	defer clientStream.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	socksErr := make(chan error, 1)
	go func() {
		defer serverStream.Close()
		socksErr <- SOCKS(ctx, serverStream, time.Second)
	}()
	if _, err := clientStream.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(clientStream, method); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(method, []byte{5, 0}) {
		t.Fatalf("SOCKS method response = %v", method)
	}
	request := []byte{5, 1, 0, 1, 127, 0, 0, 1}
	request = binary.BigEndian.AppendUint16(request, uint16(target.Addr().(*net.TCPAddr).Port))
	if _, err := clientStream.Write(request); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 10)
	if _, err := io.ReadFull(clientStream, response); err != nil {
		t.Fatal(err)
	}
	if response[1] != 0 {
		t.Fatalf("SOCKS CONNECT response = %v", response)
	}
	assertStreamEcho(t, clientStream, []byte("SOCKS payload"))
	if err := <-socksErr; err != nil {
		t.Fatal(err)
	}
}

func TestSOCKSHandshakeTimeoutResetsStream(t *testing.T) {
	serverConnection, clientConnection := testTCPPair(t)
	serverStream := &testTCPStream{TCPConn: serverConnection}
	defer clientConnection.Close()
	started := time.Now()
	err := SOCKS(context.Background(), serverStream, 20*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "handshake timed out") {
		t.Fatalf("SOCKS timeout error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("SOCKS timeout took %s", elapsed)
	}
}

type testTCPStream struct {
	*net.TCPConn
}

func (stream *testTCPStream) Reset() error {
	return stream.Close()
}

func testTCPPair(t *testing.T) (*net.TCPConn, *net.TCPConn) {
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
		t.Fatal("timed out accepting test connection")
	}
	return nil, nil
}

func startTCPEchoServer(t *testing.T) *net.TCPListener {
	t.Helper()
	listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		connection, err := listener.AcceptTCP()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = io.Copy(connection, connection)
		_ = connection.CloseWrite()
	}()
	return listener
}

func assertStreamEcho(t *testing.T, stream *testTCPStream, payload []byte) {
	t.Helper()
	defer stream.Close()
	if _, err := stream.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := stream.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(response, payload) {
		t.Fatalf("echo response = %q, want %q", response, payload)
	}
}
