package datagram

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestFramesPreserveDatagramBoundaries(t *testing.T) {
	var wire bytes.Buffer
	payloads := [][]byte{
		nil,
		[]byte("first"),
		bytes.Repeat([]byte{0xa5}, MaxPayloadLength),
	}
	for _, payload := range payloads {
		if err := Write(&wire, payload); err != nil {
			t.Fatal(err)
		}
	}
	for index, expected := range payloads {
		actual, err := Read(&wire)
		if err != nil {
			t.Fatalf("read frame %d: %v", index, err)
		}
		if !bytes.Equal(actual, expected) {
			t.Fatalf("frame %d differs: got %d bytes, want %d", index, len(actual), len(expected))
		}
	}
	if wire.Len() != 0 {
		t.Fatalf("wire still contains %d bytes", wire.Len())
	}
}

func TestReadHandlesFragmentedStream(t *testing.T) {
	var encoded bytes.Buffer
	if err := Write(&encoded, []byte("wireguard-packet")); err != nil {
		t.Fatal(err)
	}
	actual, err := Read(&oneByteReader{reader: bytes.NewReader(encoded.Bytes())})
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != "wireguard-packet" {
		t.Fatalf("payload = %q", actual)
	}
}

func TestWriteRejectsOversizedDatagram(t *testing.T) {
	err := Write(io.Discard, make([]byte, MaxPayloadLength+1))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Write() error = %v, want ErrPayloadTooLarge", err)
	}
}

func TestReadRejectsTruncatedPayload(t *testing.T) {
	_, err := Read(bytes.NewReader([]byte{0, 3, 1, 2}))
	if err == nil {
		t.Fatal("Read() accepted a truncated datagram")
	}
}

type oneByteReader struct {
	reader io.Reader
}

func (reader *oneByteReader) Read(value []byte) (int, error) {
	if len(value) > 1 {
		value = value[:1]
	}
	return reader.reader.Read(value)
}
