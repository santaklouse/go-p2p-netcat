package session

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

func TCPForward(ctx context.Context, stream Stream, host string, port int, timeout time.Duration) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("connect to TCP %s:%d: %w", host, port, err)
	}
	defer connection.Close()
	if tcp, ok := connection.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}
	return Bridge(ctx, stream, connection, connection, 0, 0)
}

func SOCKS(ctx context.Context, stream Stream, timeout time.Duration) error {
	reader := bufio.NewReader(stream)
	version, err := reader.ReadByte()
	if err != nil {
		return err
	}
	var host string
	var port int
	var success, failure []byte
	switch version {
	case 5:
		host, port, success, failure, err = negotiateSOCKS5(reader, stream)
	case 4:
		host, port, success, failure, err = negotiateSOCKS4(reader, stream)
	default:
		err = fmt.Errorf("unsupported SOCKS version: %d", version)
	}
	if err != nil {
		return err
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		_, _ = stream.Write(failure)
		return err
	}
	defer connection.Close()
	if _, err := stream.Write(success); err != nil {
		return err
	}
	return bridgeBuffered(ctx, stream, reader, connection)
}

func StartLocalForward(
	ctx context.Context,
	host string,
	port int,
	openStream func(context.Context) (Stream, error),
	onError func(error),
) (net.Listener, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go func() {
		for {
			connection, acceptErr := listener.Accept()
			if acceptErr != nil {
				if ctx.Err() == nil {
					onError(acceptErr)
				}
				return
			}
			go func() {
				defer connection.Close()
				stream, streamErr := openStream(ctx)
				if streamErr != nil {
					onError(streamErr)
					return
				}
				defer stream.Close()
				if err := Bridge(ctx, stream, connection, connection, 0, 0); err != nil && ctx.Err() == nil {
					onError(err)
				}
			}()
		}
	}()
	return listener, nil
}

func negotiateSOCKS5(reader *bufio.Reader, writer io.Writer) (string, int, []byte, []byte, error) {
	count, err := reader.ReadByte()
	if err != nil {
		return "", 0, nil, nil, err
	}
	methods := make([]byte, int(count))
	if _, err := io.ReadFull(reader, methods); err != nil {
		return "", 0, nil, nil, err
	}
	supportsNone := false
	for _, method := range methods {
		supportsNone = supportsNone || method == 0
	}
	if !supportsNone {
		_, _ = writer.Write([]byte{5, 0xff})
		return "", 0, nil, nil, errors.New("SOCKS5 client did not offer the no-authentication method")
	}
	if _, err := writer.Write([]byte{5, 0}); err != nil {
		return "", 0, nil, nil, err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", 0, nil, nil, err
	}
	if header[0] != 5 || header[1] != 1 || header[2] != 0 {
		return "", 0, nil, nil, errors.New("only SOCKS5 CONNECT is supported")
	}
	var host string
	switch header[3] {
	case 1:
		value := make([]byte, 4)
		_, err = io.ReadFull(reader, value)
		host = net.IP(value).String()
	case 3:
		length, readErr := reader.ReadByte()
		if readErr != nil {
			err = readErr
			break
		}
		value := make([]byte, int(length))
		_, err = io.ReadFull(reader, value)
		host = string(value)
	case 4:
		value := make([]byte, 16)
		_, err = io.ReadFull(reader, value)
		host = net.IP(value).String()
	default:
		err = fmt.Errorf("unsupported SOCKS5 address type: %d", header[3])
	}
	if err != nil {
		return "", 0, nil, nil, err
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return "", 0, nil, nil, err
	}
	success := []byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0}
	failure := []byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0}
	return host, int(binary.BigEndian.Uint16(portBytes)), success, failure, nil
}

func negotiateSOCKS4(reader *bufio.Reader, writer io.Writer) (string, int, []byte, []byte, error) {
	command, err := reader.ReadByte()
	if err != nil {
		return "", 0, nil, nil, err
	}
	portBytes := make([]byte, 2)
	ip := make([]byte, 4)
	if _, err := io.ReadFull(reader, portBytes); err != nil {
		return "", 0, nil, nil, err
	}
	if _, err := io.ReadFull(reader, ip); err != nil {
		return "", 0, nil, nil, err
	}
	if _, err := reader.ReadString(0); err != nil {
		return "", 0, nil, nil, err
	}
	if command != 1 {
		return "", 0, nil, nil, errors.New("only SOCKS4 CONNECT is supported")
	}
	host := net.IP(ip).String()
	if ip[0] == 0 && ip[1] == 0 && ip[2] == 0 && ip[3] != 0 {
		host, err = reader.ReadString(0)
		host = host[:len(host)-1]
		if err != nil {
			return "", 0, nil, nil, err
		}
	}
	success := append([]byte{0, 0x5a}, append(portBytes, ip...)...)
	failure := append([]byte{0, 0x5b}, append(portBytes, ip...)...)
	return host, int(binary.BigEndian.Uint16(portBytes)), success, failure, nil
}

func bridgeBuffered(ctx context.Context, stream Stream, reader io.Reader, connection net.Conn) error {
	errorsCh := make(chan error, 2)
	go func() {
		_, err := io.Copy(connection, reader)
		if tcp, ok := connection.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		errorsCh <- err
	}()
	go func() {
		_, err := io.Copy(stream, connection)
		_ = stream.CloseWrite()
		errorsCh <- err
	}()
	for range 2 {
		select {
		case <-ctx.Done():
			_ = stream.Reset()
			return ctx.Err()
		case err := <-errorsCh:
			if err != nil {
				return err
			}
		}
	}
	return nil
}
