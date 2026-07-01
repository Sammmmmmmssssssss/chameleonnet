package tunnel

import (
	"bytes"
	"crypto/rand"
	"testing"

	"chameleonnet/internal/crypto"
)

func BenchmarkWriteReadPacket(b *testing.B) {
	key := [32]byte{}
	rand.Read(key[:])
	sessionKey := crypto.SessionKey(key)
	enc := crypto.NewEncryptor(sessionKey, 0)
	dec := crypto.NewDecryptor(sessionKey, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	payload := make([]byte, 4096)
	rand.Read(payload)
	pkt := &PlainPacket{Type: PacketReal, Payload: payload}

	var buf bytes.Buffer

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = WritePacket(&buf, pkt, enc)
		_, _ = ReadPacket(&buf, dec)
	}
}

func BenchmarkHandshakeMarshal(b *testing.B) {
	salt := [crypto.SaltSize]byte{}
	rand.Read(salt[:])
	msg := NewHandshakeMessage(salt)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = msg.Marshal()
	}
}

func BenchmarkHandshakeUnmarshal(b *testing.B) {
	salt := [crypto.SaltSize]byte{}
	rand.Read(salt[:])
	data := NewHandshakeMessage(salt).Marshal()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var msg FakeTLSClientHello
		_ = msg.Unmarshal(data)
	}
}
