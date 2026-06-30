package tunnel

import (
	"encoding/binary"
	"errors"
	"io"
	"net"

	"chameleonnet/internal/crypto"
)

const (
	MagicBytes  = "CHAM"
	MagicLength = 4
	Version     = 0x01
)

var (
	ErrInvalidMagic   = errors.New("invalid magic bytes: not a ChameleonNet tunnel")
	ErrInvalidVersion = errors.New("unsupported protocol version")
	ErrHandshakeFailed = errors.New("handshake failed")
)

type HandshakeMessage struct {
	Magic [MagicLength]byte
	Version byte
	Salt   [crypto.SaltSize]byte
}

func (m *HandshakeMessage) Marshal() []byte {
	buf := make([]byte, MagicLength+1+crypto.SaltSize)
	copy(buf[0:MagicLength], m.Magic[:])
	buf[MagicLength] = m.Version
	copy(buf[MagicLength+1:], m.Salt[:])
	return buf
}

func (m *HandshakeMessage) Unmarshal(data []byte) error {
	if len(data) < MagicLength+1+crypto.SaltSize {
		return io.ErrUnexpectedEOF
	}
	copy(m.Magic[:], data[0:MagicLength])
	if string(m.Magic[:]) != MagicBytes {
		return ErrInvalidMagic
	}
	m.Version = data[MagicLength]
	if m.Version != Version {
		return ErrInvalidVersion
	}
	copy(m.Salt[:], data[MagicLength+1:MagicLength+1+crypto.SaltSize])
	return nil
}

func NewHandshakeMessage(salt [crypto.SaltSize]byte) *HandshakeMessage {
	m := &HandshakeMessage{
		Version: Version,
		Salt:    salt,
	}
	copy(m.Magic[:], MagicBytes)
	return m
}

type HandshakeResponse struct {
	Status byte
}

const (
	HandshakeOK  byte = 0x00
	HandshakeErr byte = 0xFF
)

func (r *HandshakeResponse) Marshal() []byte {
	return []byte{r.Status}
}

func (r *HandshakeResponse) Unmarshal(data []byte) error {
	if len(data) < 1 {
		return io.ErrUnexpectedEOF
	}
	r.Status = data[0]
	if r.Status != HandshakeOK {
		return ErrHandshakeFailed
	}
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
	return r.Status == HandshakeOK
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