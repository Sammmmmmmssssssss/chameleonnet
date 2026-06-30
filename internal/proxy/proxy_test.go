package proxy

import (
	"bufio"
	"bytes"
	"net"
	"testing"

	"chameleonnet/internal/config"
	"chameleonnet/internal/metrics"
	"chameleonnet/internal/pool"
)

func TestDetectProtocolSOCKS5(t *testing.T) {
	p := &Proxy{}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00})
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	proto, err := p.detectProtocol(br)
	if err != nil {
		t.Fatal(err)
	}
	if proto != ProtocolSOCKS5 {
		t.Errorf("got %d, want %d", proto, ProtocolSOCKS5)
	}
}

func TestDetectProtocolHTTP(t *testing.T) {
	p := &Proxy{}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\n\r\n"))
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	proto, err := p.detectProtocol(br)
	if err != nil {
		t.Fatal(err)
	}
	if proto != ProtocolHTTP {
		t.Errorf("got %d, want %d", proto, ProtocolHTTP)
	}
}

func TestDetectProtocolUnknown(t *testing.T) {
	p := &Proxy{}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x00})
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	_, err := p.detectProtocol(br)
	if err == nil {
		t.Fatal("expected error for unknown protocol")
	}
}

func TestNegotiateSOCKS5IPv4(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00})
		resp := make([]byte, 2)
		_, _ = client.Read(resp)
		_, _ = client.Write([]byte{0x05, 0x01, 0x00, 0x01, 192, 168, 1, 1, 0x1F, 0x90})
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	addr, err := negotiateSOCKS5(server, br)
	if err != nil {
		t.Fatal(err)
	}
	want := "192.168.1.1:8080"
	if addr != want {
		t.Errorf("got %q, want %q", addr, want)
	}
}

func TestNegotiateSOCKS5Domain(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00})
		resp := make([]byte, 2)
		_, _ = client.Read(resp)
		req := []byte{0x05, 0x01, 0x00, 0x03, 11}
		req = append(req, []byte("example.com")...)
		req = append(req, 0x00, 0x50)
		_, _ = client.Write(req)
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	addr, err := negotiateSOCKS5(server, br)
	if err != nil {
		t.Fatal(err)
	}
	want := "example.com:80"
	if addr != want {
		t.Errorf("got %q, want %q", addr, want)
	}
}

func TestNegotiateSOCKS5InvalidVersion(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x04, 0x01, 0x00})
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	_, err := negotiateSOCKS5(server, br)
	if err != ErrInvalidGreeting {
		t.Errorf("got %v, want ErrInvalidGreeting", err)
	}
}

func TestNegotiateSOCKS5UnsupportedCommand(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	go func() {
		_, _ = client.Write([]byte{0x05, 0x01, 0x00})
		resp := make([]byte, 2)
		_, _ = client.Read(resp)
		_, _ = client.Write([]byte{0x05, 0x02, 0x00, 0x01, 192, 168, 1, 1, 0x1F, 0x90})
		client.Close()
	}()

	br := bufio.NewReaderSize(server, 256)
	_, err := negotiateSOCKS5(server, br)
	if err != ErrUnsupportedCommand {
		t.Errorf("got %v, want ErrUnsupportedCommand", err)
	}
}

func TestParseHTTPConnect(t *testing.T) {
	br := bufio.NewReader(bytes.NewBufferString("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
	addr, err := parseHTTPConnect(br)
	if err != nil {
		t.Fatal(err)
	}
	want := "example.com:443"
	if addr != want {
		t.Errorf("got %q, want %q", addr, want)
	}
}

func TestParseHTTPConnectNoConnect(t *testing.T) {
	br := bufio.NewReader(bytes.NewBufferString("GET / HTTP/1.1\r\n\r\n"))
	_, err := parseHTTPConnect(br)
	if err == nil {
		t.Fatal("expected error for non-CONNECT request")
	}
}

func TestParseHTTPConnectInvalidFormat(t *testing.T) {
	br := bufio.NewReader(bytes.NewBufferString("CONNECT\r\n\r\n"))
	_, err := parseHTTPConnect(br)
	if err == nil {
		t.Fatal("expected error for invalid CONNECT format")
	}
}

func TestNewProxy(t *testing.T) {
	cfg := &config.Config{
		ListenAddr: "127.0.0.1:0",
		Mode:       config.ModeClient,
	}
	bp := pool.NewBufferPool()
	met := metrics.NewProxyMetrics()
	p := NewProxy(cfg, bp, met)
	if p == nil {
		t.Fatal("NewProxy returned nil")
	}
	if p.config != cfg {
		t.Errorf("config not set correctly")
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrInvalidGreeting == nil {
		t.Error("ErrInvalidGreeting is nil")
	}
	if ErrUnsupportedCommand == nil {
		t.Error("ErrUnsupportedCommand is nil")
	}
	if ErrUnsupportedAddrType == nil {
		t.Error("ErrUnsupportedAddrType is nil")
	}
}

func TestProtocolConstants(t *testing.T) {
	if ProtocolSOCKS5 != 5 {
		t.Errorf("ProtocolSOCKS5 = %d, want 5", ProtocolSOCKS5)
	}
	if ProtocolHTTP != 'H' {
		t.Errorf("ProtocolHTTP = %d, want %d", ProtocolHTTP, 'H')
	}
}
