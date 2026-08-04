package nativewebrtc

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const flowWindowBytes = 256 * 1024

const controlDeliveryDelay = 50 * time.Millisecond

// Stream adapts the native WebRTC framed data channel to session.Stream.
type Stream struct {
	send      func(Frame) error
	closePeer func() error

	readMu      sync.Mutex
	readCond    *sync.Cond
	readQueue   [][]byte
	readOffset  int
	readErr     error
	closedRead  bool
	writeMu     sync.Mutex
	writeClosed bool
	closed      atomic.Bool

	flowMu      sync.Mutex
	flowCond    *sync.Cond
	flowEnabled bool
	inFlight    int
}

func NewStream(send func(Frame) error, closePeer func() error) *Stream {
	value := &Stream{send: send, closePeer: closePeer}
	value.readCond = sync.NewCond(&value.readMu)
	value.flowCond = sync.NewCond(&value.flowMu)
	go func() { _ = value.sendControl("flow:1") }()
	go value.keepAlive()
	return value
}

func (s *Stream) Read(value []byte) (int, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	for len(s.readQueue) == 0 && !s.closedRead && s.readErr == nil {
		s.readCond.Wait()
	}
	if len(s.readQueue) == 0 {
		if s.readErr != nil {
			return 0, s.readErr
		}
		return 0, io.EOF
	}
	chunk := s.readQueue[0]
	count := copy(value, chunk[s.readOffset:])
	s.readOffset += count
	if s.readOffset == len(chunk) {
		s.readQueue = s.readQueue[1:]
		s.readOffset = 0
		go func() { _ = s.sendControl("ack:" + strconv.Itoa(len(chunk))) }()
	}
	return count, nil
}

func (s *Stream) Write(value []byte) (int, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeClosed || s.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	payload := append([]byte(nil), value...)
	s.flowMu.Lock()
	for s.flowEnabled && s.inFlight > 0 &&
		s.inFlight+len(payload) > flowWindowBytes && !s.closed.Load() {
		s.flowCond.Wait()
	}
	if s.closed.Load() {
		s.flowMu.Unlock()
		return 0, io.ErrClosedPipe
	}
	if s.flowEnabled {
		s.inFlight += len(payload)
	}
	s.flowMu.Unlock()
	if err := s.send(Frame{Type: FrameData, Payload: payload}); err != nil {
		s.acknowledge(len(payload))
		return 0, err
	}
	return len(value), nil
}

func (s *Stream) CloseWrite() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if s.writeClosed {
		return nil
	}
	s.writeClosed = true
	return s.sendControl("eof")
}

func (s *Stream) Close() error {
	// Match libp2p network.Stream semantics: Close is graceful and sends EOF.
	// Reset is the operation that aborts a stream. Sending "abort" here could
	// race with the preceding PTY EOF and leave an interactive client waiting
	// after its remote shell had already exited.
	writeErr := s.CloseWrite()
	s.fail(io.EOF, false)
	if s.closePeer != nil {
		// DataChannel.Send only queues the control frame. Match the browser
		// endpoint's delivery grace so closing the PeerConnection cannot discard
		// EOF before the remote reader observes it.
		if writeErr == nil {
			timer := time.NewTimer(controlDeliveryDelay)
			<-timer.C
		}
		return errors.Join(writeErr, s.closePeer())
	}
	return writeErr
}

func (s *Stream) Reset() error {
	s.fail(errors.New("native WebRTC stream reset"), false)
	_ = s.sendControl("abort")
	if s.closePeer != nil {
		return s.closePeer()
	}
	return nil
}

func (s *Stream) Receive(frame Frame) error {
	switch frame.Type {
	case FrameData:
		s.readMu.Lock()
		if !s.closedRead && !s.closed.Load() {
			s.readQueue = append(s.readQueue, append([]byte(nil), frame.Payload...))
			s.readCond.Signal()
		}
		s.readMu.Unlock()
	case FrameControl:
		s.receiveControl(string(frame.Payload))
	default:
		return fmt.Errorf("unexpected native WebRTC frame after authentication: %d", frame.Type)
	}
	return nil
}

// Reconnected resets flow-control accounting after a new WebRTC data channel
// has been attached to the same logical stream.
func (s *Stream) Reconnected() error {
	s.flowMu.Lock()
	s.inFlight = 0
	s.flowCond.Broadcast()
	s.flowMu.Unlock()
	if err := s.sendControl("resume"); err != nil {
		return err
	}
	return s.sendControl("flow:1")
}

func (s *Stream) receiveControl(control string) {
	switch {
	case control == "eof":
		s.readMu.Lock()
		s.closedRead = true
		s.readCond.Broadcast()
		s.readMu.Unlock()
	case control == "abort":
		s.fail(errors.New("remote native WebRTC peer aborted the stream"), true)
	case control == "flow:1":
		s.flowMu.Lock()
		s.flowEnabled = true
		s.flowCond.Broadcast()
		s.flowMu.Unlock()
	case control == "resume":
		s.flowMu.Lock()
		s.inFlight = 0
		s.flowCond.Broadcast()
		s.flowMu.Unlock()
		_ = s.sendControl("flow:1")
	case strings.HasPrefix(control, "ack:"):
		value, err := strconv.Atoi(strings.TrimPrefix(control, "ack:"))
		if err == nil && value > 0 {
			s.acknowledge(value)
		}
	case control == "ping":
		_ = s.sendControl("pong")
	}
}

func (s *Stream) acknowledge(value int) {
	s.flowMu.Lock()
	s.inFlight -= value
	if s.inFlight < 0 {
		s.inFlight = 0
	}
	s.flowCond.Broadcast()
	s.flowMu.Unlock()
}

func (s *Stream) sendControl(value string) error {
	if s.send == nil {
		return io.ErrClosedPipe
	}
	return s.send(Frame{Type: FrameControl, Payload: []byte(value)})
}

func (s *Stream) keepAlive() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if s.closed.Load() {
			return
		}
		_ = s.sendControl("ping")
	}
}

func (s *Stream) fail(err error, remote bool) {
	if !s.closed.CompareAndSwap(false, true) {
		return
	}
	s.flowMu.Lock()
	s.flowCond.Broadcast()
	s.flowMu.Unlock()
	s.readMu.Lock()
	s.closedRead = true
	if !errors.Is(err, io.EOF) {
		s.readErr = err
	}
	s.readCond.Broadcast()
	s.readMu.Unlock()
	s.writeMu.Lock()
	s.writeClosed = true
	s.writeMu.Unlock()
	_ = remote
}
