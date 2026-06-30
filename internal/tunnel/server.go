package tunnel

import (
	"context"
	"crypto/sha256"
	"io"
	"net"
	"sync"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/crypto"
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
	defer listener.Close()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return err
			}
		}

		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetNoDelay(true)
		}

		s.active.Add(1)
		go func(tunnelConn net.Conn) {
			defer s.active.Done()
			s.handleConn(tunnelConn)
		}(conn)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	close(s.closeCh)
	if s.listener != nil {
		s.listener.Close()
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

func (s *Server) handleConn(tunnel net.Conn) {
	defer tunnel.Close()

	sc, err := s.handshake(tunnel)
	if err != nil {
		tunnel.Write([]byte{HandshakeErr})
		return
	}

	targetAddr, err := sc.readTarget()
	if err != nil {
		tunnel.Write([]byte{HandshakeErr})
		return
	}

	target, err := sc.dialTarget(targetAddr)
	if err != nil {
		respPkt := &PlainPacket{
			Type:    PacketReal,
			Payload: []byte(err.Error()),
		}
		WritePacket(tunnel, respPkt, sc.enc)
		return
	}
	defer target.Close()

	respPkt := &PlainPacket{
		Type:    PacketReal,
		Payload: []byte{HandshakeOK},
	}
	if _, err := WritePacket(tunnel, respPkt, sc.enc); err != nil {
		return
	}

	sc.relayBidirectional(target)
}

func (s *Server) handshake(tunnel net.Conn) (*ServerConn, error) {
	buf := make([]byte, MagicLength+1+crypto.SaltSize)
	if _, err := io.ReadFull(tunnel, buf); err != nil {
		return nil, err
	}

	var msg HandshakeMessage
	if err := msg.Unmarshal(buf); err != nil {
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

	clientKey, _ := crypto.DeriveKey(s.config.Passphrase, clientSalt, 1)
	serverKey, _ := crypto.DeriveKey(s.config.Passphrase, serverSalt, 1)

	serverSum := sha256.Sum256(serverSalt[:])
	clientSum := sha256.Sum256(clientSalt[:])

	enc := crypto.NewEncryptor(serverKey, 0)
	dec := crypto.NewDecryptor(clientKey, 0)
	var serverPrefix, clientPrefix [4]byte
	copy(serverPrefix[:], serverSum[:4])
	copy(clientPrefix[:], clientSum[:4])
	enc.SetNoncePrefix(serverPrefix)
	dec.SetNoncePrefix(clientPrefix)

	resp := HandshakeResponse{Status: HandshakeOK}
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
	readMu  sync.Mutex
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

func (sc *ServerConn) relayBidirectional(target net.Conn) {
	var wg sync.WaitGroup
	_, cancel := context.WithCancel(context.Background())
	defer cancel()

	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		if err := sc.readFromTunnelWriteToTarget(target); err != nil {
			if !isClosedConnError(err) {
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		if err := sc.readFromTargetWriteToTunnel(target); err != nil {
			if !isClosedConnError(err) {
			}
		}
	}()
	wg.Wait()
}

func (sc *ServerConn) readFromTunnelWriteToTarget(target net.Conn) error {
	for {
		if sc.config.ReadTimeout > 0 {
			sc.tunnel.SetReadDeadline(time.Now().Add(sc.config.ReadTimeout.Duration()))
		}
		pkt, err := ReadPacket(sc.tunnel, sc.dec)
		if sc.config.ReadTimeout > 0 {
			sc.tunnel.SetReadDeadline(time.Time{})
		}
		if err != nil {
			return err
		}
		if pkt.Type == PacketChaff {
			continue
		}
		if len(pkt.Payload) > 0 {
			if _, err := target.Write(pkt.Payload); err != nil {
				return err
			}
		}
	}
}

func (sc *ServerConn) readFromTargetWriteToTunnel(target net.Conn) error {
	buf := make([]byte, 32768)
	for {
		n, err := target.Read(buf)
		if n > 0 {
			pkt := &PlainPacket{
				Type:    PacketReal,
				Payload: make([]byte, n),
			}
			copy(pkt.Payload, buf[:n])

			sc.writeMu.Lock()
			_, writeErr := WritePacket(sc.tunnel, pkt, sc.enc)
			sc.writeMu.Unlock()

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