package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestDeriveKey(t *testing.T) {
	salt := [SaltSize]byte{}
	rand.Read(salt[:])

	key1, err := DeriveKey("test-password-16ch", salt, 100000)
	if err != nil {
		t.Fatal(err)
	}

	key2, err := DeriveKey("test-password-16ch", salt, 100000)
	if err != nil {
		t.Fatal(err)
	}

	if key1 != key2 {
		t.Error("same inputs produced different keys")
	}

	differentSalt := [SaltSize]byte{}
	copy(differentSalt[:], salt[:])
	differentSalt[0] ^= 0xFF

	key3, err := DeriveKey("test-password-16ch", differentSalt, 100000)
	if err != nil {
		t.Fatal(err)
	}

	if key1 == key3 {
		t.Error("different salts produced same key")
	}
}

func TestDeriveKeyDifferentPasswords(t *testing.T) {
	salt := [SaltSize]byte{}
	rand.Read(salt[:])

	key1, _ := DeriveKey("password-one-16ch", salt, 100000)
	key2, _ := DeriveKey("password-two-16ch", salt, 100000)

	if key1 == key2 {
		t.Error("different passwords produced same key")
	}
}

func TestDeriveKeyInvalidIterations(t *testing.T) {
	salt := [SaltSize]byte{}
	_, err := DeriveKey("test-password-16ch", salt, 1)
	if err == nil {
		t.Error("expected error for invalid iterations")
	}
}

func TestEncryptDecrypt(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	plaintext := []byte("Hello, ChameleonNet! This is a test message.")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	if len(ciphertext) != len(plaintext)+TagSize {
		t.Errorf("ciphertext length = %d, want %d", len(ciphertext), len(plaintext)+TagSize)
	}

	decrypted, err := dec.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWithNonce(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)

	plaintext := []byte("test data with explicit nonce")
	nonce, _ := RandomNonce()

	ciphertext := enc.EncryptWithNonce(plaintext, nonce)
	decrypted, err := dec.DecryptWithNonce(ciphertext, nonce)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptWithNonceWrongNonce(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)

	plaintext := []byte("test data")
	nonce1, _ := RandomNonce()
	nonce2, _ := RandomNonce()

	ciphertext := enc.EncryptWithNonce(plaintext, nonce1)
	_, err := dec.DecryptWithNonce(ciphertext, nonce2)
	if err == nil {
		t.Error("expected error for wrong nonce")
	}
}

func TestTamperedCiphertext(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)

	plaintext := []byte("tamper test data")
	ciphertext, err := enc.Encrypt(plaintext)
	if err != nil {
		t.Fatal(err)
	}

	ciphertext[0] ^= 0xFF

	_, err = dec.Decrypt(ciphertext)
	if err == nil {
		t.Error("expected error for tampered ciphertext")
	}
}

func TestNonceIncrementation(t *testing.T) {
	var n Nonce
	n[NonceSize-1] = 0xFF
	n.Increment()
	if n[NonceSize-1] != 0 {
		t.Errorf("expected overflow to 0, got %d", n[NonceSize-1])
	}
	if n[NonceSize-2] != 1 {
		t.Errorf("expected carry to byte 10, got %d", n[NonceSize-2])
	}

	n = Nonce{}
	n.Increment()
	if n[NonceSize-1] != 1 {
		t.Errorf("expected increment to 1, got %d", n[NonceSize-1])
	}
}

func TestRandomNonce(t *testing.T) {
	n1, err := RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	n2, err := RandomNonce()
	if err != nil {
		t.Fatal(err)
	}
	if n1 == n2 {
		t.Error("two random nonces are identical")
	}
}

func TestRandomSalt(t *testing.T) {
	s1, err := RandomSalt()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := RandomSalt()
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s2 {
		t.Error("two random salts are identical")
	}
}

func TestDeriveKeyWithCounter(t *testing.T) {
	salt := [SaltSize]byte{}
	rand.Read(salt[:])

	key1, _ := DeriveKeyWithCounter("pass", salt, 100000, 1)
	key2, _ := DeriveKeyWithCounter("pass", salt, 100000, 2)

	if key1 == key2 {
		t.Error("different counters should produce different keys")
	}
}

func TestAESCTRStream(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	var iv [16]byte
	rand.Read(iv[:])

	s1 := NewAESCTRStream(key, iv)
	s2 := NewAESCTRStream(key, iv)

	buf1 := make([]byte, 64)
	buf2 := make([]byte, 64)

	s1.Read(buf1)
	s2.Read(buf2)

	if !bytes.Equal(buf1, buf2) {
		t.Error("two CTR streams with same key+IV produced different output")
	}

	buf3 := make([]byte, 64)
	s1.Read(buf3)
	if bytes.Equal(buf1, buf3) {
		t.Error("CTR stream produced same output for different positions")
	}
}

func TestDeterministicRNG(t *testing.T) {
	rng1, err := NewDeterministicRNG([]byte("seed"))
	if err != nil {
		t.Fatal(err)
	}
	rng2, err := NewDeterministicRNG([]byte("seed"))
	if err != nil {
		t.Fatal(err)
	}

	buf1 := make([]byte, 32)
	buf2 := make([]byte, 32)

	rng1.Read(buf1)
	rng2.Read(buf2)

	if !bytes.Equal(buf1, buf2) {
		t.Error("two RNGs with same seed produced different output")
	}
}

func TestDeterministicRNGDifferentSeeds(t *testing.T) {
	rng1, _ := NewDeterministicRNG([]byte("seed1"))
	rng2, _ := NewDeterministicRNG([]byte("seed2"))

	buf1 := make([]byte, 32)
	buf2 := make([]byte, 32)

	rng1.Read(buf1)
	rng2.Read(buf2)

	if bytes.Equal(buf1, buf2) {
		t.Error("different seeds should produce different output")
	}
}

func TestSessionKeyBytes(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])
	if len(key.Bytes()) != KeySize {
		t.Errorf("Bytes() length = %d, want %d", len(key.Bytes()), KeySize)
	}
}

func TestEncryptMultipleTimes(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	for i := 0; i < 1000; i++ {
		plaintext := make([]byte, 100)
		rand.Read(plaintext)

		ciphertext, err := enc.Encrypt(plaintext)
		if err != nil {
			t.Fatal(err)
		}

		decrypted, err := dec.Decrypt(ciphertext)
		if err != nil {
			t.Fatal(err)
		}

		if !bytes.Equal(plaintext, decrypted) {
			t.Fatalf("round-trip failed at iteration %d", i)
		}
	}
}

func TestEncryptEmpty(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	ciphertext, err := enc.Encrypt([]byte{})
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := dec.Decrypt(ciphertext)
	if err != nil {
		t.Fatal(err)
	}

	if len(decrypted) != 0 {
		t.Errorf("decrypted length = %d, want 0", len(decrypted))
	}
}

func TestEncryptToBuffer(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	dec.SetNoncePrefix(enc.NoncePrefix())

	plaintext := []byte("buffer test data")
	buffer := make([]byte, 1024)

	n, err := enc.EncryptToBuffer(plaintext, buffer)
	if err != nil {
		t.Fatal(err)
	}

	decrypted, err := dec.Decrypt(buffer[:n])
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(plaintext, decrypted) {
		t.Errorf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestNoncePrefix(t *testing.T) {
	key := SessionKey{}
	rand.Read(key[:])

	enc := NewEncryptor(key, 0)
	prefix := enc.NoncePrefix()

	prefix[0] ^= 0xFF
	enc.SetNoncePrefix(prefix)

	if enc.NoncePrefix() != prefix {
		t.Error("NoncePrefix mismatch after SetNoncePrefix")
	}
}

func BenchmarkEncrypt(b *testing.B) {
	key := SessionKey{}
	rand.Read(key[:])
	enc := NewEncryptor(key, 0)
	plaintext := make([]byte, 4096)
	rand.Read(plaintext)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		enc.Encrypt(plaintext)
	}
}

func BenchmarkDecrypt(b *testing.B) {
	key := SessionKey{}
	rand.Read(key[:])
	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	plaintext := make([]byte, 4096)
	rand.Read(plaintext)
	ciphertext, _ := enc.Encrypt(plaintext)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dec.Decrypt(ciphertext)
	}
}

func BenchmarkKeyDerivation(b *testing.B) {
	salt := [SaltSize]byte{}
	rand.Read(salt[:])

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		DeriveKey("test-password-16ch-test-password-16ch", salt, 100000)
	}
}