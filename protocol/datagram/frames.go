// Package datagram preserves UDP packet boundaries over a reliable byte stream.
package datagram

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// HeaderLength is the size of the big-endian payload-length prefix.
	HeaderLength = 2
	// MaxPayloadLength covers the complete uint16 length space. Normal IPv4 and
	// IPv6 UDP payloads are smaller than this limit.
	MaxPayloadLength = int(^uint16(0))
)

var ErrPayloadTooLarge = errors.New("datagram payload exceeds 65535 bytes")

// Write writes exactly one length-prefixed datagram.
func Write(writer io.Writer, payload []byte) error {
	if len(payload) > MaxPayloadLength {
		return ErrPayloadTooLarge
	}
	header := [HeaderLength]byte{}
	binary.BigEndian.PutUint16(header[:], uint16(len(payload)))
	if err := writeAll(writer, header[:]); err != nil {
		return fmt.Errorf("write datagram header: %w", err)
	}
	if err := writeAll(writer, payload); err != nil {
		return fmt.Errorf("write datagram payload: %w", err)
	}
	return nil
}

// Read reads exactly one length-prefixed datagram.
func Read(reader io.Reader) ([]byte, error) {
	header := [HeaderLength]byte{}
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return nil, err
	}
	payload := make([]byte, int(binary.BigEndian.Uint16(header[:])))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, fmt.Errorf("read datagram payload: %w", err)
	}
	return payload, nil
}

func writeAll(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if count > 0 {
			value = value[count:]
		}
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
