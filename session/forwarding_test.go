package session

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"testing"
)

func TestNegotiateSOCKS5Domain(t *testing.T) {
	request := []byte{1, 0, 5, 1, 0, 3, 11}
	request = append(request, []byte("example.com")...)
	request = binary.BigEndian.AppendUint16(request, 443)
	var response bytes.Buffer
	host, port, success, failure, err := negotiateSOCKS5(bufio.NewReader(bytes.NewReader(request)), &response)
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != 443 {
		t.Fatalf("target = %s:%d", host, port)
	}
	if !bytes.Equal(response.Bytes(), []byte{5, 0}) {
		t.Fatalf("method response = %v", response.Bytes())
	}
	if len(success) != 10 || success[1] != 0 || len(failure) != 10 || failure[1] == 0 {
		t.Fatalf("unexpected SOCKS5 responses: success=%v failure=%v", success, failure)
	}
}

func TestNegotiateSOCKS5RejectsAuthentication(t *testing.T) {
	var response bytes.Buffer
	_, _, _, _, err := negotiateSOCKS5(
		bufio.NewReader(bytes.NewReader([]byte{1, 2})),
		&response,
	)
	if err == nil {
		t.Fatal("expected authentication-method error")
	}
	if !bytes.Equal(response.Bytes(), []byte{5, 0xff}) {
		t.Fatalf("response = %v", response.Bytes())
	}
}

func TestNegotiateSOCKS4aDomain(t *testing.T) {
	request := []byte{1, 0x01, 0xbb, 0, 0, 0, 1}
	request = append(request, 0)
	request = append(request, []byte("example.com")...)
	request = append(request, 0)
	host, port, success, failure, err := negotiateSOCKS4(
		bufio.NewReader(bytes.NewReader(request)),
		io.Discard,
	)
	if err != nil {
		t.Fatal(err)
	}
	if host != "example.com" || port != 443 {
		t.Fatalf("target = %s:%d", host, port)
	}
	if success[1] != 0x5a || failure[1] != 0x5b {
		t.Fatalf("unexpected SOCKS4 responses: success=%v failure=%v", success, failure)
	}
}

func TestNegotiateSOCKS4RejectsOversizedFields(t *testing.T) {
	tests := []struct {
		name    string
		request []byte
	}{
		{
			name:    "user ID",
			request: append([]byte{1, 0x01, 0xbb, 127, 0, 0, 1}, bytes.Repeat([]byte{'u'}, maxSOCKS4UserIDLength+1)...),
		},
		{
			name: "domain",
			request: append(
				append([]byte{1, 0x01, 0xbb, 0, 0, 0, 1, 0}, bytes.Repeat([]byte{'d'}, maxSOCKS4DomainLength+1)...),
				0,
			),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, _, err := negotiateSOCKS4(
				bufio.NewReader(bytes.NewReader(test.request)), io.Discard,
			); err == nil {
				t.Fatal("oversized SOCKS4 field was accepted")
			}
		})
	}
}
