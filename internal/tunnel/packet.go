package tunnel

import (
	"encoding/binary"
	"io"

	"chameleonnet/internal/crypto"
)

const (
	PacketMaxLength    = 65536
	PacketHeaderLength = 5
	NonceLength        = crypto.NonceSize
	TagLength          = crypto.TagSize
	TypeLength         = 1
	PayloadLenLength   = 2
)

type PacketType byte

const (
	PacketReal  PacketType = 0x00
	PacketChaff PacketType = 0x01
)

type PlainPacket struct {
	Type    PacketType
	Payload []byte
}

func (p *PlainPacket) Marshal() []byte {
	payloadLen := len(p.Payload)
	buf := make([]byte, TypeLength+PayloadLenLength+payloadLen)
	buf[0] = byte(p.Type)
	binary.BigEndian.PutUint16(buf[1:3], uint16(payloadLen))
	copy(buf[3:], p.Payload)
	return buf
}

func (p *PlainPacket) Unmarshal(data []byte) error {
	if len(data) < TypeLength+PayloadLenLength {
		return io.ErrUnexpectedEOF
	}
	p.Type = PacketType(data[0])
	payloadLen := int(binary.BigEndian.Uint16(data[1:3]))
	if TypeLength+PayloadLenLength+payloadLen > len(data) {
		payloadLen = len(data) - TypeLength - PayloadLenLength
	}
	if payloadLen > 0 {
		p.Payload = make([]byte, payloadLen)
		copy(p.Payload, data[TypeLength+PayloadLenLength:TypeLength+PayloadLenLength+payloadLen])
	} else {
		p.Payload = nil
	}
	return nil
}

func WritePacket(w io.Writer, pkt *PlainPacket, enc *crypto.Encryptor) (int, error) {
	plaintext := pkt.Marshal()

	nonce, err := crypto.RandomNonce()
	if err != nil {
		return 0, err
	}

	ciphertext := enc.EncryptWithNonce(plaintext, nonce)

	bodyLen := NonceLength + len(ciphertext)
	totalLen := PacketHeaderLength + bodyLen

	buf := make([]byte, totalLen)
	// TLS Application Data (0x17), Version TLS 1.2 (0x03, 0x03)
	buf[0] = 0x17
	buf[1] = 0x03
	buf[2] = 0x03
	binary.BigEndian.PutUint16(buf[3:5], uint16(bodyLen))
	
	copy(buf[5:5+NonceLength], nonce[:])
	copy(buf[5+NonceLength:], ciphertext)

	return w.Write(buf)
}

func ReadPacket(r io.Reader, dec *crypto.Decryptor) (*PlainPacket, error) {
	headerBuf := make([]byte, PacketHeaderLength)
	if _, err := io.ReadFull(r, headerBuf); err != nil {
		return nil, err
	}

	if headerBuf[0] != 0x17 || headerBuf[1] != 0x03 || headerBuf[2] != 0x03 {
		return nil, ErrInvalidPacketLength
	}

	bodyLen := int(binary.BigEndian.Uint16(headerBuf[3:5]))
	if bodyLen < NonceLength || bodyLen > PacketMaxLength {
		return nil, ErrInvalidPacketLength
	}

	bodyBuf := make([]byte, bodyLen)
	if _, err := io.ReadFull(r, bodyBuf); err != nil {
		return nil, err
	}

	var nonce crypto.Nonce
	copy(nonce[:], bodyBuf[:NonceLength])
	ciphertext := bodyBuf[NonceLength:]

	plaintext, err := dec.DecryptWithNonce(ciphertext, nonce)
	if err != nil {
		return nil, ErrPacketDecryptFailed
	}

	var pkt PlainPacket
	if err := pkt.Unmarshal(plaintext); err != nil {
		return nil, err
	}

	return &pkt, nil
}

func WriteRawPacket(w io.Writer, data []byte, enc *crypto.Encryptor) (int, error) {
	pkt := &PlainPacket{
		Type:    PacketReal,
		Payload: data,
	}
	return WritePacket(w, pkt, enc)
}

func ReadRawPacket(r io.Reader, dec *crypto.Decryptor) ([]byte, error) {
	pkt, err := ReadPacket(r, dec)
	if err != nil {
		return nil, err
	}
	return pkt.Payload, nil
}

var (
	ErrPacketTooLarge      = &tunnelError{"packet exceeds maximum size"}
	ErrInvalidPacketLength = &tunnelError{"invalid packet length"}
	ErrPacketDecryptFailed = &tunnelError{"packet decryption failed"}
)

type tunnelError struct {
	msg string
}

func (e *tunnelError) Error() string {
	return e.msg
}