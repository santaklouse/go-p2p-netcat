package pty

import (
	"bytes"
	"testing"
)

func TestFramesRoundTrip(t *testing.T) {
	var wire bytes.Buffer
	if err := WriteFrame(&wire, FrameData, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	frame, err := ReadFrame(&wire)
	if err != nil {
		t.Fatal(err)
	}
	if frame.Type != FrameData || string(frame.Data) != "hello" {
		t.Fatalf("unexpected frame: %#v", frame)
	}
}
