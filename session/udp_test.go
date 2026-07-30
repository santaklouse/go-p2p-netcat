package session

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/santaklouse/go-p2p-netcat/protocol/datagram"
)

func TestUDPForwardPreservesPacketsAndReplies(t *testing.T) {
	target, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	targetPort := target.LocalAddr().(*net.UDPAddr).Port
	targetErr := make(chan error, 1)
	go func() {
		buffer := make([]byte, datagram.MaxPayloadLength)
		for index := 0; index < 2; index++ {
			count, source, readErr := target.ReadFromUDP(buffer)
			if readErr != nil {
				targetErr <- readErr
				return
			}
			response := append([]byte(fmt.Sprintf("%d:", index)), buffer[:count]...)
			if _, writeErr := target.WriteToUDP(response, source); writeErr != nil {
				targetErr <- writeErr
				return
			}
		}
		targetErr <- nil
	}()

	clientConnection, serverConnection := net.Pipe()
	clientStream := &pipeStream{Conn: clientConnection}
	serverStream := &pipeStream{Conn: serverConnection}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	forwardErr := make(chan error, 1)
	go func() {
		forwardErr <- UDPForward(
			ctx,
			serverStream,
			"127.0.0.1",
			targetPort,
			time.Second,
			time.Second,
		)
	}()

	for index, payload := range [][]byte{[]byte("one"), []byte("second-packet")} {
		if err := datagram.Write(clientStream, payload); err != nil {
			t.Fatal(err)
		}
		response, err := datagram.Read(clientStream)
		if err != nil {
			t.Fatal(err)
		}
		expected := fmt.Sprintf("%d:%s", index, payload)
		if string(response) != expected {
			t.Fatalf("response %d = %q, want %q", index, response, expected)
		}
	}
	if err := <-targetErr; err != nil {
		t.Fatal(err)
	}
	if err := clientStream.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-forwardErr:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UDPForward did not stop after the stream closed")
	}
}

func TestStartLocalUDPForwardReusesAssociation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened atomic.Int32
	openStream := func(context.Context) (Stream, error) {
		opened.Add(1)
		local, remote := net.Pipe()
		go echoDatagramStream(&pipeStream{Conn: remote})
		return &pipeStream{Conn: local}, nil
	}
	errorsCh := make(chan error, 8)
	listener, err := StartLocalUDPForward(
		ctx,
		"127.0.0.1",
		0,
		time.Second,
		openStream,
		func(value error) { errorsCh <- value },
	)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	client, err := net.DialUDP("udp", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	for _, payload := range [][]byte{[]byte("wireguard-handshake"), []byte("transport-data")} {
		if _, err := client.Write(payload); err != nil {
			t.Fatal(err)
		}
		buffer := make([]byte, 1024)
		count, err := client.Read(buffer)
		if err != nil {
			t.Fatal(err)
		}
		if string(buffer[:count]) != "reply:"+string(payload) {
			t.Fatalf("response = %q", buffer[:count])
		}
	}
	if got := opened.Load(); got != 1 {
		t.Fatalf("opened streams = %d, want 1", got)
	}
	select {
	case err := <-errorsCh:
		t.Fatalf("unexpected forwarding error: %v", err)
	default:
	}
}

func TestStartLocalUDPForwardSeparatesSourceEndpoints(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened atomic.Int32
	openStream := func(context.Context) (Stream, error) {
		opened.Add(1)
		local, remote := net.Pipe()
		go echoDatagramStream(&pipeStream{Conn: remote})
		return &pipeStream{Conn: local}, nil
	}
	listener, err := StartLocalUDPForward(ctx, "127.0.0.1", 0, time.Second, openStream, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	for index := 0; index < 2; index++ {
		client, err := net.DialUDP("udp", nil, listener.LocalAddr().(*net.UDPAddr))
		if err != nil {
			t.Fatal(err)
		}
		if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
			client.Close()
			t.Fatal(err)
		}
		payload := []byte(fmt.Sprintf("source-%d", index))
		if _, err := client.Write(payload); err != nil {
			client.Close()
			t.Fatal(err)
		}
		buffer := make([]byte, 64)
		count, err := client.Read(buffer)
		client.Close()
		if err != nil {
			t.Fatal(err)
		}
		if string(buffer[:count]) != "reply:"+string(payload) {
			t.Fatalf("response = %q", buffer[:count])
		}
	}
	if got := opened.Load(); got != 2 {
		t.Fatalf("opened streams = %d, want 2", got)
	}
}

func TestStartLocalUDPForwardReopensExpiredAssociation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var opened atomic.Int32
	openStream := func(context.Context) (Stream, error) {
		opened.Add(1)
		local, remote := net.Pipe()
		go echoDatagramStream(&pipeStream{Conn: remote})
		return &pipeStream{Conn: local}, nil
	}
	listener, err := StartLocalUDPForward(ctx, "127.0.0.1", 0, 40*time.Millisecond, openStream, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	client, err := net.DialUDP("udp", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	sendUDPAndExpectReply(t, client, []byte("first"))
	time.Sleep(120 * time.Millisecond)
	sendUDPAndExpectReply(t, client, []byte("second"))
	if got := opened.Load(); got != 2 {
		t.Fatalf("opened streams = %d, want 2 after idle expiration", got)
	}
}

func TestStartLocalUDPForwardCloseCancelsAssociations(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reset := make(chan struct{}, 1)
	openStream := func(context.Context) (Stream, error) {
		local, remote := net.Pipe()
		go echoDatagramStream(&pipeStream{Conn: remote})
		return &resetSignalStream{
			pipeStream: &pipeStream{Conn: local},
			reset:      reset,
		}, nil
	}
	listener, err := StartLocalUDPForward(ctx, "127.0.0.1", 0, time.Second, openStream, nil)
	if err != nil {
		t.Fatal(err)
	}

	client, err := net.DialUDP("udp", nil, listener.LocalAddr().(*net.UDPAddr))
	if err != nil {
		listener.Close()
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		listener.Close()
		t.Fatal(err)
	}
	sendUDPAndExpectReply(t, client, []byte("establish"))
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reset:
	case <-time.After(2 * time.Second):
		t.Fatal("closing the UDP listener did not cancel its active association")
	}
}

func sendUDPAndExpectReply(t *testing.T, client *net.UDPConn, payload []byte) {
	t.Helper()
	if _, err := client.Write(payload); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 1024)
	count, err := client.Read(buffer)
	if err != nil {
		t.Fatal(err)
	}
	if string(buffer[:count]) != "reply:"+string(payload) {
		t.Fatalf("response = %q", buffer[:count])
	}
}

func echoDatagramStream(stream Stream) {
	defer stream.Close()
	for {
		payload, err := datagram.Read(stream)
		if err != nil {
			return
		}
		if err := datagram.Write(stream, append([]byte("reply:"), payload...)); err != nil {
			return
		}
	}
}

type pipeStream struct {
	net.Conn
}

func (stream *pipeStream) CloseWrite() error {
	return nil
}

func (stream *pipeStream) Reset() error {
	return stream.Close()
}

type resetSignalStream struct {
	*pipeStream
	reset chan<- struct{}
}

func (stream *resetSignalStream) Reset() error {
	select {
	case stream.reset <- struct{}{}:
	default:
	}
	return stream.pipeStream.Reset()
}
