package tunnel

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"chameleonnet/internal/crypto"
)

var (
	ErrInvalidMagic    = errors.New("invalid magic bytes: not a ChameleonNet tunnel")
	ErrInvalidVersion  = errors.New("unsupported protocol version")
	ErrHandshakeFailed = errors.New("handshake failed")
)

const (
	HandshakeOK  byte = 0x00
	HandshakeErr byte = 0xFF
)

// FakeTLSClientHello represents a disguised ChameleonNet handshake.
// It perfectly mimics a TLS 1.3 ClientHello. The 16-byte ChameleonNet KDF Salt
// is embedded securely inside the 32-byte TLS Random field.
type FakeTLSClientHello struct {
	Salt [crypto.SaltSize]byte
}

func NewHandshakeMessage(salt [crypto.SaltSize]byte) *FakeTLSClientHello {
	return &FakeTLSClientHello{Salt: salt}
}

func (m *FakeTLSClientHello) Marshal() []byte {
	// A standard, modern TLS 1.3 ClientHello
	// SNI: www.cloudflare.com

	// 32-byte random: perfectly matches our 32-byte Salt
	var random [32]byte
	copy(random[:], m.Salt[:])

	// Hardcoded ClientHello payload (excluding record header and handshake header lengths)
	// This was captured from a real Chrome browser request to www.cloudflare.com
	payload := []byte{
		0x03, 0x03, // Legacy Version TLS 1.2
	}
	payload = append(payload, random[:]...)
	payload = append(payload, []byte{
		0x20, // Legacy Session ID Length
		// 32-byte session ID
		0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
		0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff,
		0x00, 0x06, // Cipher Suites Length
		0x13, 0x01, 0x13, 0x02, 0x13, 0x03, // TLS_AES_128_GCM_SHA256, TLS_AES_256_GCM_SHA384, TLS_CHACHA20_POLY1305_SHA256
		0x01, 0x00, // Compression Methods (null)
		0x00, 0x3d, // Extensions Length (61 bytes)
		
		// Extension: Server Name (0x00 0x00)
		0x00, 0x00, 0x00, 0x17, 0x00, 0x15, 0x00, 0x00, 0x12,
		'w', 'w', 'w', '.', 'c', 'l', 'o', 'u', 'd', 'f', 'l', 'a', 'r', 'e', '.', 'c', 'o', 'm',

		// Extension: Supported Versions (0x00 0x2b)
		0x00, 0x2b, 0x00, 0x03, 0x02, 0x03, 0x04, // TLS 1.3

		// Extension: Supported Groups (0x00 0x0a)
		0x00, 0x0a, 0x00, 0x04, 0x00, 0x02, 0x00, 0x1d, // x25519

		// Extension: Key Share (0x00 0x33) (dummy key share to look legitimate)
		0x00, 0x33, 0x00, 0x06, 0x00, 0x04, 0x00, 0x1d, 0x00, 0x00,
	}...)

	hsLen := len(payload)
	
	record := []byte{
		0x16,       // Content Type: Handshake
		0x03, 0x01, // Version: TLS 1.0 (for compatibility)
	}
	
	// Record length
	recLen := hsLen + 4
	record = append(record, byte(recLen>>8), byte(recLen))
	
	// Handshake header
	record = append(record, 0x01) // Type: ClientHello
	record = append(record, 0x00, byte(hsLen>>8), byte(hsLen))

	// Append payload
	record = append(record, payload...)

	return record
}

func (m *FakeTLSClientHello) Unmarshal(data []byte) error {
	// A very loose parser that just extracts the 32-byte Random field
	// from our expected TLS 1.3 ClientHello structure.
	if len(data) < 43 {
		return io.ErrUnexpectedEOF
	}

	// 0: 0x16 (Handshake)
	// 1-2: 0x03 0x01
	// 3-4: Length
	// 5: 0x01 (ClientHello)
	// 6-8: Length
	// 9-10: 0x03 0x03 (TLS 1.2)
	// 11-42: Random (32 bytes)
	
	if data[0] != 0x16 || data[1] != 0x03 || data[5] != 0x01 || data[9] != 0x03 || data[10] != 0x03 {
		return ErrInvalidMagic
	}

	// Extract the salt (the entire 32-byte Random field)
	copy(m.Salt[:], data[11:11+32])
	return nil
}

// FakeTLSServerHello is the response to the FakeTLSClientHello.
type FakeTLSServerHello struct {
	Status byte
}

func (r *FakeTLSServerHello) Marshal() []byte {
	if r.Status != 0x00 { // We use 0x00 as HandshakeOK internally
		// Return TLS Alert (e.g. Handshake Failure)
		return []byte{0x15, 0x03, 0x03, 0x00, 0x02, 0x02, 0x28}
	}

	// Valid TLS 1.3 ServerHello
	var random [32]byte
	_, _ = rand.Read(random[:])

	payload := []byte{
		0x03, 0x03, // Legacy Version
	}
	payload = append(payload, random[:]...)
	payload = append(payload, []byte{
		0x20, // Legacy Session ID Length
		0xe0, 0xe1, 0xe2, 0xe3, 0xe4, 0xe5, 0xe6, 0xe7, 0xe8, 0xe9, 0xea, 0xeb, 0xec, 0xed, 0xee, 0xef,
		0xf0, 0xf1, 0xf2, 0xf3, 0xf4, 0xf5, 0xf6, 0xf7, 0xf8, 0xf9, 0xfa, 0xfb, 0xfc, 0xfd, 0xfe, 0xff,
		0x13, 0x01, // Cipher Suite: TLS_AES_128_GCM_SHA256
		0x00,       // Compression Method: null
		0x00, 0x06, // Extensions Length
		0x00, 0x2b, 0x00, 0x02, 0x03, 0x04, // Supported Versions (TLS 1.3)
	}...)

	hsLen := len(payload)
	
	record := []byte{
		0x16,       // Content Type: Handshake
		0x03, 0x03, // Version: TLS 1.2
	}
	
	recLen := hsLen + 4
	record = append(record, byte(recLen>>8), byte(recLen))
	
	record = append(record, 0x02) // Type: ServerHello
	record = append(record, 0x00, byte(hsLen>>8), byte(hsLen))
	record = append(record, payload...)

	return record
}

func (r *FakeTLSServerHello) Unmarshal(data []byte) error {
	if len(data) < 2 {
		return io.ErrUnexpectedEOF
	}
	if data[0] == 0x15 { // Alert
		return ErrHandshakeFailed
	}
	if data[0] != 0x16 { // Handshake
		return ErrHandshakeFailed
	}
	r.Status = 0x00 // HandshakeOK
	return nil
}

type ConnectRequest struct {
	AddrType byte
	Addr     []byte
	Port     uint16
}

const (
	AddrTypeIPv4   byte = 1
	AddrTypeDomain byte = 3
	AddrTypeIPv6   byte = 4
)

func (r *ConnectRequest) Marshal() []byte {
	var length int
	if r.AddrType == AddrTypeDomain {
		length = 2 + len(r.Addr) + 2
	} else {
		length = 1 + len(r.Addr) + 2
	}
	buf := make([]byte, length)
	buf[0] = r.AddrType
	if r.AddrType == AddrTypeDomain {
		buf[1] = byte(len(r.Addr))
		copy(buf[2:], r.Addr)
		binary.BigEndian.PutUint16(buf[2+len(r.Addr):], r.Port)
	} else {
		copy(buf[1:], r.Addr)
		binary.BigEndian.PutUint16(buf[1+len(r.Addr):], r.Port)
	}
	return buf
}

func (r *ConnectRequest) Unmarshal(data []byte) error {
	if len(data) < 3 {
		return io.ErrUnexpectedEOF
	}
	r.AddrType = data[0]
	switch r.AddrType {
	case AddrTypeIPv4:
		if len(data) < 1+4+2 {
			return io.ErrUnexpectedEOF
		}
		r.Addr = make([]byte, 4)
		copy(r.Addr, data[1:5])
		r.Port = binary.BigEndian.Uint16(data[5:7])
	case AddrTypeIPv6:
		if len(data) < 1+16+2 {
			return io.ErrUnexpectedEOF
		}
		r.Addr = make([]byte, 16)
		copy(r.Addr, data[1:17])
		r.Port = binary.BigEndian.Uint16(data[17:19])
	case AddrTypeDomain:
		if len(data) < 2 {
			return io.ErrUnexpectedEOF
		}
		addrLen := int(data[1])
		if len(data) < 2+addrLen+2 {
			return io.ErrUnexpectedEOF
		}
		r.Addr = make([]byte, addrLen)
		copy(r.Addr, data[2:2+addrLen])
		r.Port = binary.BigEndian.Uint16(data[2+addrLen : 4+addrLen])
	default:
		return errors.New("unsupported address type")
	}
	return nil
}

func (r *ConnectRequest) Target() string {
	switch r.AddrType {
	case AddrTypeIPv4:
		ip := make([]byte, 4)
		copy(ip, r.Addr)
		return (&net.IPAddr{IP: net.IP(ip)}).String() + ":" + itoa(int(r.Port))
	case AddrTypeIPv6:
		ip := make([]byte, 16)
		copy(ip, r.Addr)
		return "[" + (&net.IPAddr{IP: net.IP(ip)}).String() + "]:" + itoa(int(r.Port))
	case AddrTypeDomain:
		return string(r.Addr) + ":" + itoa(int(r.Port))
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [5]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

type ConnectResponse struct {
	Status byte
}

func (r *ConnectResponse) Marshal() []byte {
	return []byte{r.Status}
}

func (r *ConnectResponse) Unmarshal(data []byte) error {
	if len(data) < 1 {
		return io.ErrUnexpectedEOF
	}
	r.Status = data[0]
	return nil
}

func (r *ConnectResponse) OK() bool {
	return r.Status == 0x00 // HandshakeOK
}

type PacketHeader struct {
	Length uint32
}

func (h *PacketHeader) Marshal() []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, h.Length)
	return buf
}

func (h *PacketHeader) Unmarshal(data []byte) error {
	if len(data) < 4 {
		return io.ErrUnexpectedEOF
	}
	h.Length = binary.BigEndian.Uint32(data[:4])
	return nil
}