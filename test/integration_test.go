package chameleontest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/crypto"
	tunnel "chameleonnet/internal/tunnel"
)

const testPassphrase = "integration-test-password-16ch"

func startEchoServer(ctx context.Context) (net.Addr, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()
	return listener.Addr(), nil
}

func startRelayServer(ctx context.Context, cfg *config.Config) (net.Addr, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	cfg.RemoteAddr = ""
	cfg.ListenAddr = listener.Addr().String()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				handleRelayConn(c)
			}(conn)
		}
	}()
	return listener.Addr(), nil
}

func handleRelayConn(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	buf := make([]byte, tunnel.MagicLength+1+crypto.SaltSize)
	if _, err := io.ReadFull(conn, buf); err != nil {
		_, _ = conn.Write([]byte{tunnel.HandshakeErr})
		return
	}
	var msg tunnel.HandshakeMessage
	if err := msg.Unmarshal(buf); err != nil {
		_, _ = conn.Write([]byte{tunnel.HandshakeErr})
		return
	}
	_, fullKeyErr := crypto.DeriveKey(testPassphrase, msg.Salt, 100000)
	if fullKeyErr != nil {
		_, _ = conn.Write([]byte{tunnel.HandshakeErr})
		return
	}

	clientSalt := msg.Salt
	for i := 0; i < 8; i++ {
		clientSalt[i] ^= 0x01
	}
	serverSalt := msg.Salt
	for i := 0; i < 8; i++ {
		serverSalt[i] ^= 0x02
	}

	clientKey := crypto.DeriveSubKey(testPassphrase, clientSalt)
	serverKey := crypto.DeriveSubKey(testPassphrase, serverSalt)

	clientSum := sha256.Sum256(clientSalt[:])
	serverSum := sha256.Sum256(serverSalt[:])

	dec := crypto.NewDecryptor(clientKey, 0)
	enc := crypto.NewEncryptor(serverKey, 0)
	var clientPrefix, serverPrefix [4]byte
	copy(clientPrefix[:], clientSum[:4])
	copy(serverPrefix[:], serverSum[:4])
	dec.SetNoncePrefix(clientPrefix)
	enc.SetNoncePrefix(serverPrefix)

	resp := tunnel.HandshakeResponse{Status: tunnel.HandshakeOK}
	if _, err := conn.Write(resp.Marshal()); err != nil {
		return
	}

	connectPkt, err := tunnel.ReadPacket(conn, dec)
	if err != nil {
		return
	}
	if connectPkt.Type != tunnel.PacketReal || len(connectPkt.Payload) == 0 {
		return
	}
	echoTarget := string(connectPkt.Payload)

	target, err := net.DialTimeout("tcp", echoTarget, 5*time.Second)
	if err != nil {
		return
	}
	defer target.Close()

	okPkt := &tunnel.PlainPacket{Type: tunnel.PacketReal, Payload: []byte{0x00}}
	if _, err := tunnel.WritePacket(conn, okPkt, enc); err != nil {
		return
	}

	relayCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var rwg sync.WaitGroup
	rwg.Add(2)
	go func() {
		defer rwg.Done()
		defer cancel()
		buf := make([]byte, 32768)
		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}
			n, err := target.Read(buf)
			if n > 0 {
				pkt := &tunnel.PlainPacket{
					Type:    tunnel.PacketReal,
					Payload: make([]byte, n),
				}
				copy(pkt.Payload, buf[:n])
				if _, err := tunnel.WritePacket(conn, pkt, enc); err != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		defer rwg.Done()
		defer cancel()
		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}
			pkt, err := tunnel.ReadPacket(conn, dec)
			if err != nil {
				return
			}
			if pkt.Type == tunnel.PacketChaff {
				continue
			}
			if len(pkt.Payload) > 0 {
				if _, err := target.Write(pkt.Payload); err != nil {
					return
				}
			}
		}
	}()
	rwg.Wait()
}

type testClient struct {
	conn net.Conn
	enc  *crypto.Encryptor
	dec  *crypto.Decryptor
}

func dialClient(ctx context.Context, relayAddr string) (*testClient, error) {
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", relayAddr)
	if err != nil {
		return nil, err
	}

	salt, err := crypto.RandomSalt()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}

	handshakeMsg := tunnel.NewHandshakeMessage(salt)
	if _, err := conn.Write(handshakeMsg.Marshal()); err != nil {
		conn.Close()
		return nil, err
	}

	reply := make([]byte, 1)
	if _, err := io.ReadFull(conn, reply); err != nil {
		conn.Close()
		return nil, err
	}
	var hsResp tunnel.HandshakeResponse
	if err := hsResp.Unmarshal(reply); err != nil {
		conn.Close()
		return nil, err
	}
	if hsResp.Status != tunnel.HandshakeOK {
		conn.Close()
		return nil, io.ErrUnexpectedEOF
	}

	_, fullErr := crypto.DeriveKey(testPassphrase, salt, 100000)
	if fullErr != nil {
		conn.Close()
		return nil, fullErr
	}

	clientSalt := salt
	for i := 0; i < 8; i++ {
		clientSalt[i] ^= 0x01
	}
	serverSalt := salt
	for i := 0; i < 8; i++ {
		serverSalt[i] ^= 0x02
	}

	clientKey := crypto.DeriveSubKey(testPassphrase, clientSalt)
	serverKey := crypto.DeriveSubKey(testPassphrase, serverSalt)

	clientSum := sha256.Sum256(clientSalt[:])
	serverSum := sha256.Sum256(serverSalt[:])

	enc := crypto.NewEncryptor(clientKey, 0)
	dec := crypto.NewDecryptor(serverKey, 0)
	var clientPrefix, serverPrefix [4]byte
	copy(clientPrefix[:], clientSum[:4])
	copy(serverPrefix[:], serverSum[:4])
	enc.SetNoncePrefix(clientPrefix)
	dec.SetNoncePrefix(serverPrefix)

	return &testClient{conn: conn, enc: enc, dec: dec}, nil
}

func (tc *testClient) Close() {
	tc.conn.Close()
}

func (tc *testClient) writePkt(payload []byte) error {
	pkt := &tunnel.PlainPacket{Type: tunnel.PacketReal, Payload: payload}
	_, err := tunnel.WritePacket(tc.conn, pkt, tc.enc)
	return err
}

func (tc *testClient) readPkt() ([]byte, error) {
	for {
		pkt, err := tunnel.ReadPacket(tc.conn, tc.dec)
		if err != nil {
			return nil, err
		}
		if pkt.Type == tunnel.PacketChaff {
			continue
		}
		return pkt.Payload, nil
	}
}

func TestFullTunnelRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	echoAddr, err := startEchoServer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Mode:             config.ModeServer,
		Passphrase:       testPassphrase,
		KDFIterations:    100000,
		HandshakeTimeout: config.Duration(10 * time.Second),
		ReadTimeout:      config.Duration(30 * time.Second),
		WriteTimeout:     config.Duration(30 * time.Second),
		MaxConnections:   10,
		BufferSize:       32768,
	}

	relayAddr, err := startRelayServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	tc, err := dialClient(ctx, relayAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer tc.Close()

	if err := tc.writePkt([]byte(echoAddr.String())); err != nil {
		t.Fatal(err)
	}

	ok, err := tc.readPkt()
	if err != nil {
		t.Fatal(err)
	}
	if len(ok) != 1 || ok[0] != 0x00 {
		t.Fatal("expected OK from relay")
	}

	testData := []byte("Hello ChameleonNet! Integration test payload.")
	if err := tc.writePkt(testData); err != nil {
		t.Fatal(err)
	}

	recv, err := tc.readPkt()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(recv, testData) {
		t.Errorf("received = %q, want %q", recv, testData)
	}
}

func TestTunnelHandshakeErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg := &config.Config{
		Mode:             config.ModeServer,
		Passphrase:       testPassphrase,
		KDFIterations:    100000,
		HandshakeTimeout: config.Duration(3 * time.Second),
		ReadTimeout:      config.Duration(5 * time.Second),
		WriteTimeout:     config.Duration(5 * time.Second),
		MaxConnections:   5,
		BufferSize:       32768,
	}

	relayAddr, err := startRelayServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	conn, err := net.DialTimeout("tcp", relayAddr.String(), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	garbage := make([]byte, tunnel.MagicLength+1+crypto.SaltSize)
	for i := range garbage {
		garbage[i] = 0xFF
	}
	if _, err := conn.Write(garbage); err != nil {
		t.Fatal(err)
	}

	reply := make([]byte, 1)
	if _, err := io.ReadFull(conn, reply); err != nil {
		return
	}
	if reply[0] == tunnel.HandshakeErr {
		return
	}
	t.Error("expected HandshakeErr for invalid magic bytes")
}

func TestConcurrentTunnelConnections(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping concurrent test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	echoAddr, err := startEchoServer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Mode:             config.ModeServer,
		Passphrase:       testPassphrase,
		KDFIterations:    100000,
		HandshakeTimeout: config.Duration(10 * time.Second),
		ReadTimeout:      config.Duration(30 * time.Second),
		WriteTimeout:     config.Duration(30 * time.Second),
		MaxConnections:   50,
		BufferSize:       32768,
	}

	relayAddr, err := startRelayServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	numConns := 10

	for i := 0; i < numConns; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			tc, err := dialClient(ctx, relayAddr.String())
			if err != nil {
				t.Errorf("conn %d: dial error: %v", id, err)
				return
			}
			defer tc.Close()

			if err := tc.writePkt([]byte(echoAddr.String())); err != nil {
				t.Errorf("conn %d: connect error: %v", id, err)
				return
			}

			ok, err := tc.readPkt()
			if err != nil {
				t.Errorf("conn %d: connect response error: %v", id, err)
				return
			}
			if len(ok) != 1 || ok[0] != 0x00 {
				t.Errorf("conn %d: unexpected connect response", id)
				return
			}

			payload := []byte{byte(id)}
			if err := tc.writePkt(payload); err != nil {
				t.Errorf("conn %d: write error: %v", id, err)
				return
			}

			recv, err := tc.readPkt()
			if err != nil {
				t.Errorf("conn %d: read error: %v", id, err)
				return
			}
			if len(recv) != 1 || recv[0] != byte(id) {
				t.Errorf("conn %d: received %v, want [%d]", id, recv, id)
			}
		}(i)
	}
	wg.Wait()
}

func TestSingleBytePayload(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	echoAddr, err := startEchoServer(ctx)
	if err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		Mode:             config.ModeServer,
		Passphrase:       testPassphrase,
		KDFIterations:    100000,
		HandshakeTimeout: config.Duration(10 * time.Second),
		ReadTimeout:      config.Duration(30 * time.Second),
		WriteTimeout:     config.Duration(30 * time.Second),
		MaxConnections:   10,
		BufferSize:       32768,
	}

	relayAddr, err := startRelayServer(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}

	tc, err := dialClient(ctx, relayAddr.String())
	if err != nil {
		t.Fatal(err)
	}
	defer tc.Close()

	if err := tc.writePkt([]byte(echoAddr.String())); err != nil {
		t.Fatal(err)
	}

	ok, err := tc.readPkt()
	if err != nil {
		t.Fatal(err)
	}
	if len(ok) != 1 || ok[0] != 0x00 {
		t.Fatal("expected OK from relay")
	}

	tests := []byte{0x00, 0x01, 0x7F, 0xFF}
	for _, b := range tests {
		if err := tc.writePkt([]byte{b}); err != nil {
			t.Fatal(err)
		}
		recv, err := tc.readPkt()
		if err != nil {
			t.Fatal(err)
		}
		if len(recv) != 1 || recv[0] != b {
			t.Errorf("for byte 0x%02x, received %v", b, recv)
		}
	}
}
