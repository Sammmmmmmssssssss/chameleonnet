package tunnel

import (
	"bytes"
	"testing"

	"chameleonnet/internal/crypto"
)

func TestHandshakeMessageMarshalUnmarshal(t *testing.T) {
	salt, err := crypto.RandomSalt()
	if err != nil {
		t.Fatal(err)
	}

	msg := NewHandshakeMessage(salt)
	data := msg.Marshal()

	var unmarshaled HandshakeMessage
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	if string(unmarshaled.Magic[:]) != MagicBytes {
		t.Errorf("Magic = %q, want %q", unmarshaled.Magic, MagicBytes)
	}
	if unmarshaled.Version != Version {
		t.Errorf("Version = %d, want %d", unmarshaled.Version, Version)
	}
	if unmarshaled.Salt != salt {
		t.Errorf("Salt mismatch")
	}
}

func TestHandshakeMessageInvalidMagic(t *testing.T) {
	data := make([]byte, MagicLength+1+crypto.SaltSize)
	data[0] = 'X'
	data[1] = 'X'
	data[2] = 'X'
	data[3] = 'X'

	var msg HandshakeMessage
	if err := msg.Unmarshal(data); err != ErrInvalidMagic {
		t.Errorf("got %v, want ErrInvalidMagic", err)
	}
}

func TestHandshakeMessageInvalidVersion(t *testing.T) {
	salt, _ := crypto.RandomSalt()
	msg := NewHandshakeMessage(salt)
	msg.Version = 0xFF
	data := msg.Marshal()

	var unmarshaled HandshakeMessage
	if err := unmarshaled.Unmarshal(data); err != ErrInvalidVersion {
		t.Errorf("got %v, want ErrInvalidVersion", err)
	}
}

func TestHandshakeMessageShortData(t *testing.T) {
	var msg HandshakeMessage
	if err := msg.Unmarshal([]byte{0x00}); err == nil {
		t.Error("expected error for short data")
	}
}

func TestHandshakeResponse(t *testing.T) {
	resp := HandshakeResponse{Status: HandshakeOK}
	data := resp.Marshal()

	var unmarshaled HandshakeResponse
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if unmarshaled.Status != HandshakeOK {
		t.Errorf("Status = %d, want %d", unmarshaled.Status, HandshakeOK)
	}
}

func TestHandshakeResponseError(t *testing.T) {
	err := (&HandshakeResponse{}).Unmarshal(nil)
	if err == nil {
		t.Error("expected error for nil data")
	}
}

func TestConnectRequestIPv4(t *testing.T) {
	req := &ConnectRequest{
		AddrType: AddrTypeIPv4,
		Addr:     []byte{192, 168, 1, 1},
		Port:     8080,
	}

	data := req.Marshal()

	var unmarshaled ConnectRequest
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	if unmarshaled.AddrType != AddrTypeIPv4 {
		t.Errorf("AddrType = %d, want %d", unmarshaled.AddrType, AddrTypeIPv4)
	}
	if len(unmarshaled.Addr) != 4 {
		t.Errorf("Addr len = %d, want 4", len(unmarshaled.Addr))
	}
	if unmarshaled.Port != 8080 {
		t.Errorf("Port = %d, want 8080", unmarshaled.Port)
	}
}

func TestConnectRequestIPv6(t *testing.T) {
	req := &ConnectRequest{
		AddrType: AddrTypeIPv6,
		Addr:     make([]byte, 16),
		Port:     443,
	}
	for i := range req.Addr {
		req.Addr[i] = byte(i)
	}

	data := req.Marshal()

	var unmarshaled ConnectRequest
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	if unmarshaled.AddrType != AddrTypeIPv6 {
		t.Errorf("AddrType = %d, want %d", unmarshaled.AddrType, AddrTypeIPv6)
	}
	if len(unmarshaled.Addr) != 16 {
		t.Errorf("Addr len = %d, want 16", len(unmarshaled.Addr))
	}
	if unmarshaled.Port != 443 {
		t.Errorf("Port = %d, want 443", unmarshaled.Port)
	}
}

func TestConnectRequestDomain(t *testing.T) {
	req := &ConnectRequest{
		AddrType: AddrTypeDomain,
		Addr:     []byte("example.com"),
		Port:     80,
	}

	data := req.Marshal()

	var unmarshaled ConnectRequest
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	if unmarshaled.AddrType != AddrTypeDomain {
		t.Errorf("AddrType = %d, want %d", unmarshaled.AddrType, AddrTypeDomain)
	}
	if string(unmarshaled.Addr) != "example.com" {
		t.Errorf("Addr = %q, want %q", unmarshaled.Addr, "example.com")
	}
	if unmarshaled.Port != 80 {
		t.Errorf("Port = %d, want 80", unmarshaled.Port)
	}
}

func TestConnectRequestShortData(t *testing.T) {
	var req ConnectRequest
	if err := req.Unmarshal(nil); err == nil {
		t.Error("expected error for nil data")
	}
	if err := req.Unmarshal([]byte{0x01, 0x00}); err == nil {
		t.Error("expected error for short data")
	}
}

func TestConnectRequestUnknownAddrType(t *testing.T) {
	data := []byte{0x05, 0x00, 0x00, 0x00}
	var req ConnectRequest
	if err := req.Unmarshal(data); err == nil {
		t.Error("expected error for unknown address type")
	}
}

func TestConnectResponse(t *testing.T) {
	resp := ConnectResponse{Status: HandshakeOK}
	data := resp.Marshal()

	var unmarshaled ConnectResponse
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if !unmarshaled.OK() {
		t.Error("OK() should be true for HandshakeOK")
	}

	resp.Status = 0xFF
	if resp.OK() {
		t.Error("OK() should be false for non-zero status")
	}
}

func TestConnectResponseShortData(t *testing.T) {
	var resp ConnectResponse
	if err := resp.Unmarshal(nil); err == nil {
		t.Error("expected error for nil data")
	}
}

func TestPacketHeader(t *testing.T) {
	h := PacketHeader{Length: 1024}
	data := h.Marshal()

	var unmarshaled PacketHeader
	if err := unmarshaled.Unmarshal(data); err != nil {
		t.Fatal(err)
	}
	if unmarshaled.Length != 1024 {
		t.Errorf("Length = %d, want 1024", unmarshaled.Length)
	}
}

func TestPacketHeaderInvalid(t *testing.T) {
	var h PacketHeader
	if err := h.Unmarshal(nil); err == nil {
		t.Error("expected error for nil data")
	}
}

func TestPlainPacketMarshalUnmarshal(t *testing.T) {
	tests := []struct {
		name string
		pkt  PlainPacket
	}{
		{"real data", PlainPacket{Type: PacketReal, Payload: []byte("hello world")}},
		{"empty", PlainPacket{Type: PacketReal, Payload: []byte{}}},
		{"chaff", PlainPacket{Type: PacketChaff, Payload: []byte{0x01, 0x02, 0x03}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.pkt.Marshal()

			var unmarshaled PlainPacket
			if err := unmarshaled.Unmarshal(data); err != nil {
				t.Fatal(err)
			}

			if unmarshaled.Type != tt.pkt.Type {
				t.Errorf("Type = %v, want %v", unmarshaled.Type, tt.pkt.Type)
			}
			if !bytes.Equal(unmarshaled.Payload, tt.pkt.Payload) {
				t.Errorf("Payload = %v, want %v", unmarshaled.Payload, tt.pkt.Payload)
			}
		})
	}
}

func TestPlainPacketUnmarshalShortData(t *testing.T) {
	var pkt PlainPacket
	if err := pkt.Unmarshal([]byte{0x00}); err == nil {
		t.Error("expected error for short data")
	}
}

func TestPlainPacketWriteReadRoundTrip(t *testing.T) {
	key := [32]byte{}
	key[0] = 0x01
	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	var buf bytes.Buffer
	original := &PlainPacket{Type: PacketReal, Payload: []byte("round trip test")}
	if _, err := WritePacket(&buf, original, enc); err != nil {
		t.Fatal(err)
	}

	decoded, err := ReadPacket(&buf, dec)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, original.Type)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload = %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestPacketMaxSize(t *testing.T) {
	_ = PacketMaxLength
	_ = NonceLength
}

func TestWriteReadPacket(t *testing.T) {
	key := [32]byte{}
	key[0] = 0x01
	key[15] = 0x02
	key[31] = 0x03

	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)

	original := &PlainPacket{
		Type:    PacketReal,
		Payload: []byte("test packet data for round trip"),
	}

	var buf bytes.Buffer
	if _, err := WritePacket(&buf, original, enc); err != nil {
		t.Fatal(err)
	}

	decoded, err := ReadPacket(&buf, dec)
	if err != nil {
		t.Fatal(err)
	}

	if decoded.Type != original.Type {
		t.Errorf("Type = %v, want %v", decoded.Type, original.Type)
	}
	if !bytes.Equal(decoded.Payload, original.Payload) {
		t.Errorf("Payload = %q, want %q", decoded.Payload, original.Payload)
	}
}

func TestWriteReadRawPacket(t *testing.T) {
	key := [32]byte{}
	key[0] = 0x42
	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)

	data := []byte("raw data for test")
	var buf bytes.Buffer

	if _, err := WriteRawPacket(&buf, data, enc); err != nil {
		t.Fatal(err)
	}

	decoded, err := ReadRawPacket(&buf, dec)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(decoded, data) {
		t.Errorf("decoded = %q, want %q", decoded, data)
	}
}

func TestWriteReadPacketEmpty(t *testing.T) {
	key := [32]byte{}
	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)

	var buf bytes.Buffer
	if _, err := WriteRawPacket(&buf, []byte{}, enc); err != nil {
		t.Fatal(err)
	}

	decoded, err := ReadRawPacket(&buf, dec)
	if err != nil {
		t.Fatal(err)
	}

	if len(decoded) != 0 {
		t.Errorf("decoded len = %d, want 0", len(decoded))
	}
}

func TestTunnelError(t *testing.T) {
	e := &tunnelError{msg: "test error"}
	if e.Error() != "test error" {
		t.Errorf("got %q, want %q", e.Error(), "test error")
	}
}

func TestErrorVariables(t *testing.T) {
	if ErrPacketTooLarge == nil {
		t.Error("ErrPacketTooLarge is nil")
	}
	if ErrInvalidPacketLength == nil {
		t.Error("ErrInvalidPacketLength is nil")
	}
	if ErrPacketDecryptFailed == nil {
		t.Error("ErrPacketDecryptFailed is nil")
	}
}

func TestMultipleEncryptedPackets(t *testing.T) {
	key := [32]byte{}
	key[0] = 0xAA
	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)

	var buf bytes.Buffer
	for i := 0; i < 100; i++ {
		pkt := &PlainPacket{
			Type:    PacketReal,
			Payload: []byte{byte(i)},
		}
		if _, err := WritePacket(&buf, pkt, enc); err != nil {
			t.Fatal(err)
		}
	}

	for i := 0; i < 100; i++ {
		pkt, err := ReadPacket(&buf, dec)
		if err != nil {
			t.Fatal(err)
		}
		if len(pkt.Payload) != 1 || pkt.Payload[0] != byte(i) {
			t.Errorf("packet %d: got %v, want [%d]", i, pkt.Payload, i)
		}
	}
}