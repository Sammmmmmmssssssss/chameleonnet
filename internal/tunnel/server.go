package tunnel

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/crypto"
	"chameleonnet/internal/morpher"
)

type Server struct {
	config   *config.Config
	listener net.Listener
	active   sync.WaitGroup
	closeCh  chan struct{}
}

func NewServer(cfg *config.Config) *Server {
	return &Server{
		config:  cfg,
		closeCh: make(chan struct{}),
	}
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", s.config.ListenAddr)
	if err != nil {
		return err
	}
	s.listener = listener
	defer listener.Close() //nolint:errcheck

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return err
			}
		}

		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetNoDelay(true)
		}

		s.active.Add(1)
		go func(tunnelConn net.Conn) {
			defer s.active.Done()
			s.handleConn(ctx, tunnelConn)
		}(conn)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.closeCh)
	if s.listener != nil {
		_ = s.listener.Close()
	}

	done := make(chan struct{})
	go func() {
		s.active.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (s *Server) handleConn(ctx context.Context, tunnel net.Conn) {
	defer tunnel.Close() //nolint:errcheck

	sc, err := s.handshake(tunnel)
	if err != nil {
		_, _ = tunnel.Write([]byte{HandshakeErr})
		return
	}

	targetAddr, err := sc.readTarget()
	if err != nil {
		_, _ = tunnel.Write([]byte{HandshakeErr})
		return
	}

	target, err := sc.dialTarget(targetAddr)
	if err != nil {
		respPkt := &PlainPacket{
			Type:    PacketReal,
			Payload: []byte(err.Error()),
		}
		_, _ = WritePacket(tunnel, respPkt, sc.enc)
		return
	}
	defer target.Close() //nolint:errcheck

	respPkt := &PlainPacket{
		Type:    PacketReal,
		Payload: []byte{HandshakeOK},
	}
	if _, err := WritePacket(tunnel, respPkt, sc.enc); err != nil {
		return
	}

	sc.relayBidirectional(ctx, target)
}

func (s *Server) handshake(tunnel net.Conn) (*ServerConn, error) {
	// Read FakeTLSClientHello header
	header := make([]byte, 5)
	if _, err := io.ReadFull(tunnel, header); err != nil {
		return nil, err
	}
	recordLen := int(header[3])<<8 | int(header[4])
	if recordLen > 8192 {
		return nil, errors.New("invalid TLS record length")
	}

	payload := make([]byte, recordLen)
	if _, err := io.ReadFull(tunnel, payload); err != nil {
		return nil, err
	}

	fullRecord := append(header, payload...)
	var msg FakeTLSClientHello
	if err := msg.Unmarshal(fullRecord); err != nil {
		return nil, err
	}

	_, fullKeyErr := crypto.DeriveKey(s.config.Passphrase, msg.Salt, s.config.KDFIterations)
	if fullKeyErr != nil {
		return nil, fullKeyErr
	}

	clientSalt := msg.Salt
	for i := 0; i < 8; i++ {
		clientSalt[i] ^= 0x01
	}
	serverSalt := msg.Salt
	for i := 0; i < 8; i++ {
		serverSalt[i] ^= 0x02
	}

	clientKey := crypto.DeriveSubKey(s.config.Passphrase, clientSalt)
	serverKey := crypto.DeriveSubKey(s.config.Passphrase, serverSalt)

	serverSum := sha256.Sum256(serverSalt[:])
	clientSum := sha256.Sum256(clientSalt[:])

	enc := crypto.NewEncryptor(serverKey, 0)
	dec := crypto.NewDecryptor(clientKey, 0)
	var serverPrefix, clientPrefix [4]byte
	copy(serverPrefix[:], serverSum[:4])
	copy(clientPrefix[:], clientSum[:4])
	enc.SetNoncePrefix(serverPrefix)
	dec.SetNoncePrefix(clientPrefix)

	resp := FakeTLSServerHello{Status: 0x00} // HandshakeOK
	if _, err := tunnel.Write(resp.Marshal()); err != nil {
		return nil, err
	}

	return &ServerConn{
		tunnel: tunnel,
		enc:    enc,
		dec:    dec,
		config: s.config,
	}, nil
}

type ServerConn struct {
	tunnel  net.Conn
	enc     *crypto.Encryptor
	dec     *crypto.Decryptor
	config  *config.Config
	writeMu sync.Mutex
	closed  bool
	closeMu sync.RWMutex
}

func (sc *ServerConn) readTarget() (string, error) {
	pkt, err := ReadPacket(sc.tunnel, sc.dec)
	if err != nil {
		return "", err
	}
	return string(pkt.Payload), nil
}

func (sc *ServerConn) dialTarget(addr string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	return dialer.Dial("tcp", addr)
}

func (sc *ServerConn) WriteChaff(payload []byte) error {
	pkt := &PlainPacket{
		Type:    PacketChaff,
		Payload: payload,
	}
	sc.writeMu.Lock()
	defer sc.writeMu.Unlock()
	_, err := WritePacket(sc.tunnel, pkt, sc.enc)
	return err
}

func (sc *ServerConn) relayBidirectional(ctx context.Context, target net.Conn) {
	prof := morpher.LookupProfile(morpher.ProfileName(sc.config.Profile))
	padder := morpher.NewPadder(prof.PadderBuckets)
	shaper := morpher.NewShaper(time.Now().UnixNano(), prof.ShaperServer)

	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)

	// ── Goroutine 1: target → tunnel (upload to client with padding + shaping)
	go func() {
		defer wg.Done()
		defer cancel()

		buf := make([]byte, sc.config.BufferSize)
		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}

			n, err := target.Read(buf)
			if n > 0 {
				padded := padder.Pad(buf[:n])

				delay := shaper.NextDelay()
				if delay > 0 {
					select {
					case <-relayCtx.Done():
						return
					case <-time.After(delay):
					}
				}

				pkt := &PlainPacket{
					Type:    PacketReal,
					Payload: padded,
				}
				sc.writeMu.Lock()
				_, werr := WritePacket(sc.tunnel, pkt, sc.enc)
				sc.writeMu.Unlock()

				if werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// ── Goroutine 2: tunnel → target (download from client with depadding)
	go func() {
		defer wg.Done()
		defer cancel()

		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}
			if sc.config.ReadTimeout > 0 {
				_ = sc.tunnel.SetReadDeadline(time.Now().Add(sc.config.ReadTimeout.Duration())) //nolint:errcheck
			}
			pkt, err := ReadPacket(sc.tunnel, sc.dec)
			if sc.config.ReadTimeout > 0 {
				_ = sc.tunnel.SetReadDeadline(time.Time{}) //nolint:errcheck
			}

			if err != nil {
				return
			}
			if pkt.Type == PacketChaff {
				continue
			}
			if len(pkt.Payload) > 0 {
				unpadded, _ := padder.RemovePadding(pkt.Payload)
				if len(unpadded) > 0 {
					if _, werr := target.Write(unpadded); werr != nil {
						return
					}
				}
			}
		}
	}()

	// ── Goroutine 3: chaff injection (server to client)
	go func() {
		defer wg.Done()

		lambda := prof.ChaffLambda
		if lambda <= 0 {
			return
		}

		chaffSize := prof.PadderBuckets[0]
		rng := rand.New(rand.NewSource(time.Now().UnixNano() + 0xCAFE))
		poisson := morpher.NewPoissonProcessNoLock(time.Now().UnixNano()+0xFEED, lambda)

		for {
			interval := poisson.NextInterval()
			select {
			case <-relayCtx.Done():
				return
			case <-time.After(interval):
			}

			payload := make([]byte, chaffSize)
			_, _ = rng.Read(payload)

			if werr := sc.WriteChaff(payload); werr != nil {
				return
			}
		}
	}()

	wg.Wait()
}

func (sc *ServerConn) Close() error {
	sc.closeMu.Lock()
	defer sc.closeMu.Unlock()
	if sc.closed {
		return nil
	}
	sc.closed = true

	if sc.tunnel != nil {
		return sc.tunnel.Close()
	}
	return nil
}