package pty

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	FrameData      = byte(0)
	FrameResize    = byte(1)
	MaxFrameLength = 1024 * 1024
)

type Frame struct {
	Type byte
	Data []byte
}

func WriteFrame(writer io.Writer, kind byte, data []byte) error {
	if len(data) > MaxFrameLength {
		return fmt.Errorf("PTY frame превышает %d байт", MaxFrameLength)
	}
	header := [5]byte{kind}
	binary.BigEndian.PutUint32(header[1:], uint32(len(data)))
	if err := writeFull(writer, header[:]); err != nil {
		return err
	}
	return writeFull(writer, data)
}

func ReadFrame(reader io.Reader) (Frame, error) {
	var header [5]byte
	if _, err := io.ReadFull(reader, header[:]); err != nil {
		return Frame{}, err
	}
	length := binary.BigEndian.Uint32(header[1:])
	if length > MaxFrameLength {
		return Frame{}, fmt.Errorf("PTY frame превышает %d байт", MaxFrameLength)
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return Frame{}, errors.New("PTY stream завершился внутри frame")
	}
	return Frame{Type: header[0], Data: data}, nil
}

func EncodeResize(columns, rows uint16) []byte {
	if columns == 0 {
		columns = 1
	}
	if rows == 0 {
		rows = 1
	}
	result := make([]byte, 4)
	binary.BigEndian.PutUint16(result[0:2], columns)
	binary.BigEndian.PutUint16(result[2:4], rows)
	return result
}

func DecodeResize(value []byte) (uint16, uint16, error) {
	if len(value) != 4 {
		return 0, 0, errors.New("PTY resize payload должен содержать 4 байта")
	}
	columns := binary.BigEndian.Uint16(value[0:2])
	rows := binary.BigEndian.Uint16(value[2:4])
	if columns == 0 {
		columns = 1
	}
	if rows == 0 {
		rows = 1
	}
	return columns, rows, nil
}

func writeFull(writer io.Writer, value []byte) error {
	for len(value) > 0 {
		count, err := writer.Write(value)
		if err != nil {
			return err
		}
		if count == 0 {
			return io.ErrShortWrite
		}
		value = value[count:]
	}
	return nil
}
