package tunnel

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/crypto"
)

type ClientConn struct {
	relay       net.Conn
	enc         *crypto.Encryptor
	dec         *crypto.Decryptor
	config      *config.Config
	writeMu     sync.Mutex
	readMu      sync.Mutex
	closed      bool
	closeMu     sync.RWMutex
	pendingBuf  []byte
	pendingOff  int
}

func Dial(ctx context.Context, remoteAddr string, cfg *config.Config) (*ClientConn, error) {
	dialer := &net.Dialer{
		Timeout: cfg.HandshakeTimeout.Duration(),
	}
	relay, err := dialer.DialContext(ctx, "tcp", remoteAddr)
	if err != nil {
		return nil, err
	}

	if tcp, ok := relay.(*net.TCPConn); ok {
		_ = tcp.SetNoDelay(true)
	}

	salt, err := crypto.RandomSalt()
	if err != nil {
		_ = relay.Close()
		return nil, err
	}

	handshakeMsg := NewHandshakeMessage(salt)
	if _, err := relay.Write(handshakeMsg.Marshal()); err != nil {
		_ = relay.Close()
		return nil, err
	}

	reply := make([]byte, 1)
	if _, err := io.ReadFull(relay, reply); err != nil {
		_ = relay.Close()
		return nil, err
	}
	var resp HandshakeResponse
	if err := resp.Unmarshal(reply); err != nil {
		relay.Close()
		return nil, err
	}

	_, deriveErr := crypto.DeriveKey(cfg.Passphrase, salt, cfg.KDFIterations)
	if deriveErr != nil {
		relay.Close()
		return nil, deriveErr
	}

	clientSalt := salt
	for i := 0; i < 8; i++ {
		clientSalt[i] ^= 0x01
	}
	serverSalt := salt
	for i := 0; i < 8; i++ {
		serverSalt[i] ^= 0x02
	}

	clientKey := crypto.DeriveSubKey(cfg.Passphrase, clientSalt)
	serverKey := crypto.DeriveSubKey(cfg.Passphrase, serverSalt)

	clientSum := sha256.Sum256(clientSalt[:])
	serverSum := sha256.Sum256(serverSalt[:])

	enc := crypto.NewEncryptor(clientKey, 0)
	dec := crypto.NewDecryptor(serverKey, 0)
	var clientPrefix, serverPrefix [4]byte
	copy(clientPrefix[:], clientSum[:4])
	copy(serverPrefix[:], serverSum[:4])
	enc.SetNoncePrefix(clientPrefix)
	dec.SetNoncePrefix(serverPrefix)

	return &ClientConn{
		relay:  relay,
		enc:    enc,
		dec:    dec,
		config: cfg,
	}, nil
}

func (c *ClientConn) Handshake(targetAddr string) error {
	pkt := &PlainPacket{
		Type:    PacketReal,
		Payload: []byte(targetAddr),
	}

	if c.config.HandshakeTimeout > 0 {
		_ = c.relay.SetWriteDeadline(time.Now().Add(c.config.HandshakeTimeout.Duration()))
	}
	if _, err := WritePacket(c.relay, pkt, c.enc); err != nil {
		return err
	}
	if c.config.HandshakeTimeout > 0 {
		_ = c.relay.SetWriteDeadline(time.Time{})
	}

	if c.config.HandshakeTimeout > 0 {
		_ = c.relay.SetReadDeadline(time.Now().Add(c.config.HandshakeTimeout.Duration()))
	}
	respPkt, err := ReadPacket(c.relay, c.dec)
	if err != nil {
		return err
	}
	if c.config.HandshakeTimeout > 0 {
		_ = c.relay.SetReadDeadline(time.Time{})
	}

	if respPkt.Type != PacketReal {
		return ErrHandshakeFailed
	}

	return nil
}

func (c *ClientConn) Relay(ctx context.Context, local net.Conn) error {
	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	var relayErr error
	var relayErrMu sync.Mutex

	setRelayErr := func(err error) {
		relayErrMu.Lock()
		if relayErr == nil {
			relayErr = err
		}
		relayErrMu.Unlock()
		cancel()
	}

	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		err := c.relayFromLocal(relayCtx, local)
		if err != nil && !isClosedConnError(err) {
			setRelayErr(err)
		}
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		err := c.relayToLocal(relayCtx, local)
		if err != nil && !isClosedConnError(err) {
			setRelayErr(err)
		}
	}()
	wg.Wait()

	relayErrMu.Lock()
	err := relayErr
	relayErrMu.Unlock()
	return err
}

func (c *ClientConn) relayFromLocal(ctx context.Context, local net.Conn) error {
	buf := make([]byte, 32768)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := local.Read(buf)
		if n > 0 {
			pkt := &PlainPacket{
				Type:    PacketReal,
				Payload: make([]byte, n),
			}
			copy(pkt.Payload, buf[:n])

			c.writeMu.Lock()
			writeErr := func() error {
				if c.config.WriteTimeout > 0 {
					_ = c.relay.SetWriteDeadline(time.Now().Add(c.config.WriteTimeout.Duration()))
				}
				_, err := WritePacket(c.relay, pkt, c.enc)
				if c.config.WriteTimeout > 0 {
					c.relay.SetWriteDeadline(time.Time{})
				}
				return err
			}()
			c.writeMu.Unlock()

			if writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (c *ClientConn) relayToLocal(ctx context.Context, local net.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if c.config.ReadTimeout > 0 {
			_ = c.relay.SetReadDeadline(time.Now().Add(c.config.ReadTimeout.Duration()))
		}
		pkt, err := ReadPacket(c.relay, c.dec)
		if c.config.ReadTimeout > 0 {
			c.relay.SetReadDeadline(time.Time{})
		}

		if err != nil {
			return err
		}

		if pkt.Type == PacketChaff {
			continue
		}

		if len(pkt.Payload) > 0 {
			if _, err := local.Write(pkt.Payload); err != nil {
				return err
			}
		}
	}
}

func (c *ClientConn) Read(b []byte) (int, error) {
	if c.pendingOff < len(c.pendingBuf) {
		n := copy(b, c.pendingBuf[c.pendingOff:])
		c.pendingOff += n
		if c.pendingOff >= len(c.pendingBuf) {
			c.pendingBuf = nil
			c.pendingOff = 0
		}
		return n, nil
	}

	for {
		c.readMu.Lock()
		pkt, err := ReadPacket(c.relay, c.dec)
		c.readMu.Unlock()
		if err != nil {
			return 0, err
		}
		if pkt.Type == PacketChaff || len(pkt.Payload) == 0 {
			continue
		}
		n := copy(b, pkt.Payload)
		if n < len(pkt.Payload) {
			c.pendingBuf = pkt.Payload
			c.pendingOff = n
		}
		return n, nil
	}
}

func (c *ClientConn) Write(b []byte) (int, error) {
	pkt := &PlainPacket{
		Type:    PacketReal,
		Payload: b,
	}
	c.writeMu.Lock()
	_, err := WritePacket(c.relay, pkt, c.enc)
	c.writeMu.Unlock()
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *ClientConn) SetDeadline(t time.Time) error {
	return c.relay.SetDeadline(t)
}

func (c *ClientConn) SetReadDeadline(t time.Time) error {
	return c.relay.SetReadDeadline(t)
}

func (c *ClientConn) SetWriteDeadline(t time.Time) error {
	return c.relay.SetWriteDeadline(t)
}

func (c *ClientConn) Close() error {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	return c.relay.Close()
}

func (c *ClientConn) IsClosed() bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()
	return c.closed
}

func (c *ClientConn) LocalAddr() net.Addr {
	return c.relay.LocalAddr()
}

func (c *ClientConn) RemoteAddr() net.Addr {
	return c.relay.RemoteAddr()
}

func isClosedConnError(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, io.EOF) ||
		err.Error() == "use of closed network connection"
}