package crypto

import (
	"crypto/rand"
	"testing"
)

func BenchmarkDeriveKey(b *testing.B) {
	salt := [SaltSize]byte{}
	rand.Read(salt[:])

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = DeriveKey("benchmark-passphrase-here!", salt, 100000)
	}
}

func BenchmarkDeterministicRNG(b *testing.B) {
	rng, _ := NewDeterministicRNG([]byte("bench-seed"))
	buf := make([]byte, 64)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rng.Read(buf)
	}
}

func BenchmarkEncryptWithNonce(b *testing.B) {
	key := SessionKey{}
	rand.Read(key[:])
	enc := NewEncryptor(key, 0)
	plaintext := make([]byte, 4096)
	rand.Read(plaintext)
	nonce, _ := RandomNonce()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = enc.EncryptWithNonce(plaintext, nonce)
	}
}
