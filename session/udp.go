package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/santaklouse/go-p2p-netcat/protocol/datagram"
)

const (
	DefaultUDPIdleTimeout = 5 * time.Minute
	maxUDPAssociations    = 256
	udpPacketQueueDepth   = 256
)

type udpAssociation struct {
	cancel  context.CancelFunc
	packets chan []byte
}

// UDPForward connects one framed P2P datagram session to a fixed UDP target.
func UDPForward(
	ctx context.Context,
	stream Stream,
	host string,
	port int,
	timeout time.Duration,
	idleTimeout time.Duration,
) error {
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return fmt.Errorf("connect to UDP %s:%d: %w", host, port, err)
	}
	defer connection.Close()

	errorsCh := make(chan error, 2)
	activity := make(chan struct{}, 1)
	go func() {
		for {
			payload, readErr := datagram.Read(stream)
			if readErr != nil {
				errorsCh <- readErr
				return
			}
			if err := writePacket(connection, payload); err != nil {
				errorsCh <- err
				return
			}
			reportDatagramActivity(activity)
		}
	}()
	go func() {
		buffer := make([]byte, datagram.MaxPayloadLength)
		for {
			count, readErr := connection.Read(buffer)
			if readErr != nil {
				errorsCh <- readErr
				return
			}
			if err := datagram.Write(stream, buffer[:count]); err != nil {
				errorsCh <- err
				return
			}
			reportDatagramActivity(activity)
		}
	}()
	return waitForDatagramBridge(ctx, stream, activity, errorsCh, idleTimeout)
}

// StartLocalUDPForward binds a local UDP port. Every source endpoint receives
// an independent P2P stream so replies can be routed back without placing local
// addresses on the wire.
func StartLocalUDPForward(
	ctx context.Context,
	host string,
	port int,
	idleTimeout time.Duration,
	openStream func(context.Context) (Stream, error),
	onError func(error),
) (*net.UDPConn, error) {
	address, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return nil, fmt.Errorf("resolve local UDP address: %w", err)
	}
	listener, err := net.ListenUDP("udp", address)
	if err != nil {
		return nil, err
	}

	var associationsMu sync.Mutex
	associations := make(map[string]*udpAssociation)

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go func() {
		defer func() {
			associationsMu.Lock()
			cancels := make([]context.CancelFunc, 0, len(associations))
			for _, association := range associations {
				cancels = append(cancels, association.cancel)
			}
			associationsMu.Unlock()
			for _, cancel := range cancels {
				cancel()
			}
		}()

		buffer := make([]byte, datagram.MaxPayloadLength)
		for {
			count, source, readErr := listener.ReadFromUDP(buffer)
			if readErr != nil {
				if ctx.Err() == nil && !errors.Is(readErr, net.ErrClosed) {
					reportUDPError(onError, readErr)
				}
				return
			}
			payload := append([]byte(nil), buffer[:count]...)
			key := source.String()

			associationsMu.Lock()
			association := associations[key]
			if association == nil {
				if len(associations) >= maxUDPAssociations {
					associationsMu.Unlock()
					reportUDPError(onError, fmt.Errorf(
						"UDP association limit of %d reached; dropped packet from %s",
						maxUDPAssociations,
						source,
					))
					continue
				}
				associationCtx, cancel := context.WithCancel(ctx)
				association = &udpAssociation{
					cancel:  cancel,
					packets: make(chan []byte, udpPacketQueueDepth),
				}
				associations[key] = association
				sourceCopy := *source
				go func(current *udpAssociation) {
					defer current.cancel()
					defer func() {
						associationsMu.Lock()
						if associations[key] == current {
							delete(associations, key)
						}
						associationsMu.Unlock()
					}()

					stream, streamErr := openStream(associationCtx)
					if streamErr != nil {
						if associationCtx.Err() == nil {
							reportUDPError(onError, fmt.Errorf("open UDP session for %s: %w", key, streamErr))
						}
						return
					}
					defer stream.Close()
					if bridgeErr := bridgeLocalUDP(
						associationCtx,
						stream,
						listener,
						&sourceCopy,
						current.packets,
						idleTimeout,
					); bridgeErr != nil && associationCtx.Err() == nil {
						reportUDPError(onError, fmt.Errorf("UDP session for %s: %w", key, bridgeErr))
					}
				}(association)
			}
			associationsMu.Unlock()

			select {
			case association.packets <- payload:
			default:
				reportUDPError(onError, fmt.Errorf(
					"UDP packet queue for %s is full; dropped %d-byte packet",
					source,
					len(payload),
				))
			}
		}
	}()
	return listener, nil
}

func bridgeLocalUDP(
	ctx context.Context,
	stream Stream,
	listener *net.UDPConn,
	source *net.UDPAddr,
	packets <-chan []byte,
	idleTimeout time.Duration,
) error {
	errorsCh := make(chan error, 2)
	activity := make(chan struct{}, 1)
	go func() {
		for {
			select {
			case <-ctx.Done():
				errorsCh <- ctx.Err()
				return
			case payload := <-packets:
				if err := datagram.Write(stream, payload); err != nil {
					errorsCh <- err
					return
				}
				reportDatagramActivity(activity)
			}
		}
	}()
	go func() {
		for {
			payload, readErr := datagram.Read(stream)
			if readErr != nil {
				errorsCh <- readErr
				return
			}
			count, writeErr := listener.WriteToUDP(payload, source)
			if writeErr == nil && count != len(payload) {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				errorsCh <- writeErr
				return
			}
			reportDatagramActivity(activity)
		}
	}()
	return waitForDatagramBridge(ctx, stream, activity, errorsCh, idleTimeout)
}

func waitForDatagramBridge(
	ctx context.Context,
	stream Stream,
	activity <-chan struct{},
	errorsCh <-chan error,
	idleTimeout time.Duration,
) error {
	var idle *time.Timer
	var idleC <-chan time.Time
	if idleTimeout > 0 {
		idle = time.NewTimer(idleTimeout)
		idleC = idle.C
		defer idle.Stop()
	}
	for {
		select {
		case <-ctx.Done():
			_ = stream.Reset()
			return ctx.Err()
		case <-idleC:
			return nil
		case <-activity:
			if idle != nil {
				if !idle.Stop() {
					select {
					case <-idle.C:
					default:
					}
				}
				idle.Reset(idleTimeout)
			}
		case err := <-errorsCh:
			if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			_ = stream.Reset()
			return err
		}
	}
}

func writePacket(connection net.Conn, payload []byte) error {
	count, err := connection.Write(payload)
	if err == nil && count != len(payload) {
		return io.ErrShortWrite
	}
	return err
}

func reportDatagramActivity(activity chan<- struct{}) {
	select {
	case activity <- struct{}{}:
	default:
	}
}

func reportUDPError(onError func(error), err error) {
	if onError != nil {
		onError(err)
	}
}
