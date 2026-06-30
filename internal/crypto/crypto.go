package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync/atomic"

	"chameleonnet/internal/pool"
)

const (
	KeySize   = 32
	NonceSize = 12
	TagSize   = 16
	SaltSize  = 32
)

var ErrInvalidKeySize = errors.New("invalid key size: must be 32 bytes")
var ErrInvalidNonceSize = errors.New("invalid nonce size: must be 12 bytes")
var ErrDecryptFailed = errors.New("decryption failed: authentication tag mismatch")
var ErrBufferTooSmall = errors.New("output buffer too small")

type SessionKey [KeySize]byte

func (k *SessionKey) Bytes() []byte {
	return k[:]
}

type Nonce [NonceSize]byte

func (n *Nonce) Bytes() []byte {
	return n[:]
}

func (n *Nonce) Increment() {
	for i := NonceSize - 1; i >= 0; i-- {
		n[i]++
		if n[i] != 0 {
			break
		}
	}
}

func RandomNonce() (Nonce, error) {
	var n Nonce
	_, err := rand.Read(n[:])
	return n, err
}

func DeriveKey(passphrase string, salt [SaltSize]byte, iterations int) (SessionKey, error) {
	if iterations < 10000 {
		return SessionKey{}, ErrInvalidIterations
	}

	var key SessionKey
	hmac := hmac.New(sha256.New, salt[:])
	hmac.Write([]byte(passphrase))
	key = SessionKey(hmac.Sum(key[:0]))

	for i := 0; i < iterations-1; i++ {
		hmac.Reset()
		hmac.Write(key[:])
		key = SessionKey(hmac.Sum(key[:0]))
	}

	return key, nil
}

func DeriveKeyWithCounter(passphrase string, salt [SaltSize]byte, iterations int, counter uint64) (SessionKey, error) {
	var counterBytes [8]byte
	binary.BigEndian.PutUint64(counterBytes[:], counter)
	
	combinedSalt := salt
	for i := range 8 {
		combinedSalt[i] ^= counterBytes[i]
	}

	return DeriveKey(passphrase, combinedSalt, iterations)
}

var ErrInvalidIterations = errors.New("iterations must be at least 10000")

func RandomSalt() ([SaltSize]byte, error) {
	var salt [SaltSize]byte
	_, err := rand.Read(salt[:])
	return salt, err
}

type Encryptor struct {
	aead       cipher.AEAD
	nonce      atomic.Uint64
	noncePrefix [4]byte
	pool       *pool.SizedBufferPool
}

func NewEncryptor(key SessionKey, initialNonce uint64) *Encryptor {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	var noncePrefix [4]byte
	rand.Read(noncePrefix[:])

	e := &Encryptor{
		aead:       aead,
		noncePrefix: noncePrefix,
		pool:       pool.NewSizedBufferPool(65536),
	}
	e.nonce.Store(initialNonce)
	return e
}

func (e *Encryptor) Encrypt(plaintext []byte) ([]byte, error) {
	counter := e.nonce.Add(1) - 1
	return e.EncryptWithCounter(plaintext, counter)
}

func (e *Encryptor) EncryptWithCounter(plaintext []byte, counter uint64) ([]byte, error) {
	var nonce Nonce
	copy(nonce[:4], e.noncePrefix[:])
	binary.BigEndian.PutUint64(nonce[4:], counter)

	ciphertext := e.aead.Seal(nil, nonce[:], plaintext, nil)
	return ciphertext, nil
}

func (e *Encryptor) EncryptWithNonce(plaintext []byte, nonce Nonce) []byte {
	return e.aead.Seal(nil, nonce[:], plaintext, nil)
}

func (e *Encryptor) EncryptToBuffer(plaintext, buffer []byte) (int, error) {
	counter := e.nonce.Add(1) - 1
	return e.EncryptToBufferWithCounter(plaintext, buffer, counter)
}

func (e *Encryptor) EncryptToBufferWithCounter(plaintext, buffer []byte, counter uint64) (int, error) {
	var nonce Nonce
	copy(nonce[:4], e.noncePrefix[:])
	binary.BigEndian.PutUint64(nonce[4:], counter)

	n := e.aead.Seal(buffer[:0], nonce[:], plaintext, nil)
	if len(n) > cap(buffer) {
		return 0, ErrBufferTooSmall
	}
	return len(n), nil
}

func (e *Encryptor) GetBuffer() *[]byte {
	return e.pool.Get()
}

func (e *Encryptor) PutBuffer(buf *[]byte) {
	e.pool.Put(buf)
}

type Decryptor struct {
	aead       cipher.AEAD
	nonce      atomic.Uint64
	noncePrefix [4]byte
	pool       *pool.SizedBufferPool
}

func NewDecryptor(key SessionKey, initialNonce uint64) *Decryptor {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		panic(err)
	}

	var noncePrefix [4]byte
	rand.Read(noncePrefix[:])

	d := &Decryptor{
		aead:       aead,
		noncePrefix: noncePrefix,
		pool:       pool.NewSizedBufferPool(65536),
	}
	d.nonce.Store(initialNonce)
	return d
}

func (d *Decryptor) Decrypt(ciphertext []byte) ([]byte, error) {
	counter := d.nonce.Add(1) - 1
	return d.DecryptWithCounter(ciphertext, counter)
}

func (d *Decryptor) DecryptWithCounter(ciphertext []byte, counter uint64) ([]byte, error) {
	var nonce Nonce
	copy(nonce[:4], d.noncePrefix[:])
	binary.BigEndian.PutUint64(nonce[4:], counter)

	plaintext, err := d.aead.Open(nil, nonce[:], ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

func (d *Decryptor) DecryptWithNonce(ciphertext []byte, nonce Nonce) ([]byte, error) {
	plaintext, err := d.aead.Open(nil, nonce[:], ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

func (d *Decryptor) DecryptToBuffer(ciphertext, buffer []byte) (int, error) {
	counter := d.nonce.Add(1) - 1
	return d.DecryptToBufferWithCounter(ciphertext, buffer, counter)
}

func (d *Decryptor) DecryptToBufferWithCounter(ciphertext, buffer []byte, counter uint64) (int, error) {
	var nonce Nonce
	copy(nonce[:4], d.noncePrefix[:])
	binary.BigEndian.PutUint64(nonce[4:], counter)

	n, err := d.aead.Open(buffer[:0], nonce[:], ciphertext, nil)
	if err != nil {
		return 0, ErrDecryptFailed
	}
	if len(n) > cap(buffer) {
		return 0, ErrBufferTooSmall
	}
	return len(n), nil
}

func (d *Decryptor) GetBuffer() *[]byte {
	return d.pool.Get()
}

func (d *Decryptor) PutBuffer(buf *[]byte) {
	d.pool.Put(buf)
}

func (e *Encryptor) NoncePrefix() [4]byte {
	return e.noncePrefix
}

func (d *Decryptor) NoncePrefix() [4]byte {
	return d.noncePrefix
}

func (e *Encryptor) SetNoncePrefix(prefix [4]byte) {
	e.noncePrefix = prefix
}

func (d *Decryptor) SetNoncePrefix(prefix [4]byte) {
	d.noncePrefix = prefix
}

func (e *Encryptor) CurrentCounter() uint64 {
	return e.nonce.Load()
}

func (d *Decryptor) CurrentCounter() uint64 {
	return d.nonce.Load()
}

type AESCTRStream struct {
	stream cipher.Stream
	pool   *pool.SizedBufferPool
}

func NewAESCTRStream(key SessionKey, iv [16]byte) *AESCTRStream {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		panic(err)
	}
	stream := cipher.NewCTR(block, iv[:])
	return &AESCTRStream{
		stream: stream,
		pool:   pool.NewSizedBufferPool(65536),
	}
}

func (s *AESCTRStream) Read(p []byte) (int, error) {
	s.stream.XORKeyStream(p, p)
	return len(p), nil
}

func (s *AESCTRStream) GetBuffer() *[]byte {
	return s.pool.Get()
}

func (s *AESCTRStream) PutBuffer(buf *[]byte) {
	s.pool.Put(buf)
}

func (s *AESCTRStream) GenerateKeyStream(length int) []byte {
	buf := make([]byte, length)
	s.stream.XORKeyStream(buf, buf)
	return buf
}

type DeterministicRNG struct {
	stream *AESCTRStream
}

func NewDeterministicRNG(seed []byte) (*DeterministicRNG, error) {
	var key SessionKey
	if len(seed) >= KeySize {
		copy(key[:], seed[:KeySize])
	} else {
		var sum [sha256.Size]byte
		sum = sha256.Sum256(seed)
		key = SessionKey(sum)
	}

	var iv [16]byte
	ivSum := sha256.Sum256(append(seed, []byte(":iv")...))
	copy(iv[:], ivSum[:16])

	stream := NewAESCTRStream(key, iv)
	return &DeterministicRNG{stream: stream}, nil
}

func (r *DeterministicRNG) Read(p []byte) (int, error) {
	return r.stream.Read(p)
}

func (r *DeterministicRNG) Uint64() uint64 {
	var buf [8]byte
	r.stream.Read(buf[:])
	return binary.BigEndian.Uint64(buf[:])
}

func (r *DeterministicRNG) Float64() float64 {
	return float64(r.Uint64()) / float64(^uint64(0))
}

func (r *DeterministicRNG) Int63() int64 {
	return int64(r.Uint64() >> 1)
}

func (r *DeterministicRNG) Intn(n int) int {
	if n <= 0 {
		return 0
	}
	return int(r.Uint64()) % n
}

func (r *DeterministicRNG) Seed(seed int64) {
	var key SessionKey
	var iv [16]byte
	
	keyBuf := make([]byte, 8)
	binary.BigEndian.PutUint64(keyBuf, uint64(seed))
	keySum := sha256.Sum256(keyBuf)
	key = SessionKey(keySum)
	
	r.stream = NewAESCTRStream(key, iv)
}

type ReadWriter interface {
	Read(p []byte) (n int, err error)
	Write(p []byte) (n int, err error)
}

func NewEncryptingReader(key SessionKey, r ReadWriter) (*EncryptedReadWriter, error) {
	enc := NewEncryptor(key, 0)
	dec := NewDecryptor(key, 0)
	return &EncryptedReadWriter{reader: r, encryptor: enc, decryptor: dec}, nil
}

type EncryptedReadWriter struct {
	reader     ReadWriter
	encryptor  *Encryptor
	decryptor  *Decryptor
}

func (e *EncryptedReadWriter) Read(p []byte) (int, error) {
	buf := e.decryptor.GetBuffer()
	defer e.decryptor.PutBuffer(buf)

	n, err := e.reader.Read(*buf)
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, nil
	}

	plaintext, err := e.decryptor.Decrypt((*buf)[:n])
	if err != nil {
		return 0, err
	}

	copy(p, plaintext)
	return len(plaintext), nil
}

func (e *EncryptedReadWriter) Write(p []byte) (int, error) {
	ciphertext, err := e.encryptor.Encrypt(p)
	if err != nil {
		return 0, err
	}

	return e.reader.Write(ciphertext)
}