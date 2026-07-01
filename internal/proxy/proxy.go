package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"sync"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/metrics"
	"chameleonnet/internal/morpher"
	"chameleonnet/internal/pool"
	"chameleonnet/internal/tunnel"
)

// Protocol identifies the inbound proxy protocol detected on a new connection.
type Protocol byte

const (
	ProtocolUnknown Protocol = 0
	ProtocolSOCKS5  Protocol = 5
	ProtocolHTTP    Protocol = 'H'
)

// Server is the minimal interface satisfied by both the client Proxy and the
// relay Server so that main.go can treat them uniformly.
type Server interface {
	ListenAndServe(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

// InstrumentedServer extends Server with observable internals.
type InstrumentedServer interface {
	Server
	Metrics() *metrics.ProxyMetrics
	StartTime() time.Time
}

// Proxy is the client-side listener: it accepts SOCKS5 or HTTP CONNECT
// connections, opens an encrypted ChameleonNet tunnel to the relay server,
// applies traffic morphing (padding + Poisson-chaff + shaping), and
// bidirectionally relays data.
type Proxy struct {
	config    *config.Config
	listener  net.Listener
	bufPool   *pool.BufferPool
	metrics   *metrics.ProxyMetrics
	wg        sync.WaitGroup
	closeCh   chan struct{}
	startTime time.Time
	connSem   chan struct{} // bounded semaphore — enforces MaxConnections
}

// NewProxy constructs a ready-to-serve Proxy.
func NewProxy(cfg *config.Config, bp *pool.BufferPool, m *metrics.ProxyMetrics) *Proxy {
	// Pre-fill the semaphore so each acquire is a non-blocking receive.
	sem := make(chan struct{}, cfg.MaxConnections)
	for i := 0; i < cfg.MaxConnections; i++ {
		sem <- struct{}{}
	}
	return &Proxy{
		config:    cfg,
		bufPool:   bp,
		metrics:   m,
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
		connSem:   sem,
	}
}

// ListenAndServe binds the configured listen address and serves until ctx is
// cancelled.
func (p *Proxy) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", p.config.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", p.config.ListenAddr, err)
	}
	p.listener = listener

	p.wg.Add(1)
	go p.acceptLoop(ctx)

	<-ctx.Done()
	p.listener.Close()
	return nil
}

func (p *Proxy) acceptLoop(ctx context.Context) {
	defer p.wg.Done()
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return
			}
		}

		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetKeepAlive(true)
			_ = tcp.SetKeepAlivePeriod(30 * time.Second)
			_ = tcp.SetNoDelay(true)
			_ = tcp.SetReadBuffer(p.config.BufferSize)
			_ = tcp.SetWriteBuffer(p.config.BufferSize)
		}

		// Enforce MaxConnections via semaphore: drop the connection
		// immediately if we are at capacity rather than queuing.
		select {
		case <-p.connSem:
			// acquired a slot — proceed
		default:
			_ = conn.Close()
			p.metrics.Errors.Inc()
			continue
		}

		p.metrics.ActiveConns.Inc()
		p.metrics.TotalConns.Inc()

		p.wg.Add(1)
		go func(c net.Conn) {
			defer p.wg.Done()
			defer c.Close()
			defer p.metrics.ActiveConns.Dec()
			defer func() { p.connSem <- struct{}{} }() // release slot
			p.handleConn(ctx, c)
		}(conn)
	}
}

func (p *Proxy) handleConn(ctx context.Context, conn net.Conn) {
	if p.config.HandshakeTimeout > 0 {
		_ = conn.SetDeadline(time.Now().Add(p.config.HandshakeTimeout.Duration()))
		defer conn.SetDeadline(time.Time{}) //nolint:errcheck
	}

	br := bufio.NewReaderSize(conn, 256)
	proto, err := p.detectProtocol(br)
	if err != nil {
		p.metrics.Errors.Inc()
		return
	}

	_ = conn.SetDeadline(time.Time{})

	switch proto {
	case ProtocolSOCKS5:
		p.handleSOCKS5(ctx, conn, br)
	case ProtocolHTTP:
		p.handleHTTP(ctx, conn, br)
	default:
		p.metrics.Errors.Inc()
	}
}

func (p *Proxy) detectProtocol(br *bufio.Reader) (Protocol, error) {
	peek, err := br.Peek(1)
	if err != nil {
		return ProtocolUnknown, err
	}
	switch peek[0] {
	case 0x05:
		return ProtocolSOCKS5, nil
	case 'C', 'G', 'P', 'D', 'H', 'O', 'T':
		return ProtocolHTTP, nil
	default:
		return ProtocolUnknown, fmt.Errorf("unknown protocol byte: 0x%02x", peek[0])
	}
}

// handleSOCKS5 negotiates the SOCKS5 handshake, opens a tunnel, and relays.
func (p *Proxy) handleSOCKS5(ctx context.Context, conn net.Conn, br *bufio.Reader) {
	targetAddr, err := negotiateSOCKS5(conn, br)
	if err != nil {
		p.replySOCKS5Error(conn, 0x01)
		p.metrics.Errors.Inc()
		return
	}

	remote, err := tunnel.Dial(ctx, p.config.RemoteAddr, p.config)
	if err != nil {
		p.replySOCKS5Error(conn, 0x04)
		p.metrics.Errors.Inc()
		return
	}
	// NOTE: do NOT close remote here — it must stay open for Handshake + relay.

	if err := remote.Handshake(targetAddr); err != nil {
		_ = remote.Close()
		p.replySOCKS5Error(conn, 0x01)
		p.metrics.Errors.Inc()
		return
	}

	p.replySOCKS5Success(conn)
	p.morphedRelay(ctx, conn, remote)
}

// handleHTTP negotiates HTTP CONNECT, opens a tunnel, and relays.
func (p *Proxy) handleHTTP(ctx context.Context, conn net.Conn, br *bufio.Reader) {
	targetAddr, err := parseHTTPConnect(br)
	if err != nil {
		writeHTTPStatus(conn, 400, "Bad Request")
		p.metrics.Errors.Inc()
		return
	}

	remote, err := tunnel.Dial(ctx, p.config.RemoteAddr, p.config)
	if err != nil {
		writeHTTPStatus(conn, 502, "Bad Gateway")
		p.metrics.Errors.Inc()
		return
	}
	// NOTE: do NOT close remote here — it must stay open for Handshake + relay.

	if err := remote.Handshake(targetAddr); err != nil {
		_ = remote.Close()
		writeHTTPStatus(conn, 502, "Bad Gateway")
		p.metrics.Errors.Inc()
		return
	}

	writeHTTP200(conn)
	p.morphedRelay(ctx, conn, remote)
}

// morphedRelay is the core data path for client mode.
//
// It runs three concurrent goroutines:
//
//  1. upload   — local → remote, with padding + Exp-LogNormal shaping delay
//  2. download — remote → local, with depadding (chaff already filtered by ClientConn.Read)
//  3. chaff    — Poisson-distributed decoy packets injected into the tunnel
//
// Any goroutine finishing cancels the shared context, unblocking the others.
func (p *Proxy) morphedRelay(ctx context.Context, local net.Conn, remote *tunnel.ClientConn) {
	defer func() { _ = remote.Close() }()

	prof := morpher.LookupProfile(morpher.ProfileName(p.config.Profile))
	padder := morpher.NewPadder(prof.PadderBuckets)
	shaper := morpher.NewShaper(time.Now().UnixNano(), prof.ShaperClient)

	relayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(3)

	// ── Goroutine 1: local → remote (upload with padding + shaping) ──────────
	go func() {
		defer wg.Done()
		defer cancel()

		buf := make([]byte, p.config.BufferSize)
		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}

			if p.config.ReadTimeout > 0 {
				_ = local.SetReadDeadline(time.Now().Add(p.config.ReadTimeout.Duration()))
			}
			n, err := local.Read(buf)
			if p.config.ReadTimeout > 0 {
				_ = local.SetReadDeadline(time.Time{})
			}

			if n > 0 {
				// Pad to the nearest bucket boundary.
				padded := padder.Pad(buf[:n])

				// Apply inter-packet shaping delay (Exp + LogNormal mixture).
				delay := shaper.NextDelay()
				if delay > 0 {
					select {
					case <-relayCtx.Done():
						return
					case <-time.After(delay):
					}
				}

				if _, werr := remote.Write(padded); werr != nil {
					if !isClosedError(werr) {
						p.metrics.Errors.Inc()
					}
					return
				}
				p.metrics.BytesUp.Add(int64(n))
				p.metrics.PacketsUp.Inc()
			}
			if err != nil {
				return
			}
		}
	}()

	// ── Goroutine 2: remote → local (download with depadding) ────────────────
	// ClientConn.Read already skips PacketChaff frames, so we only see real data.
	go func() {
		defer wg.Done()
		defer cancel()

		buf := make([]byte, p.config.BufferSize)
		for {
			select {
			case <-relayCtx.Done():
				return
			default:
			}

			n, err := remote.Read(buf)
			if n > 0 {
				unpadded, _ := padder.RemovePadding(buf[:n])
				if len(unpadded) > 0 {
					if p.config.WriteTimeout > 0 {
						_ = local.SetWriteDeadline(time.Now().Add(p.config.WriteTimeout.Duration()))
					}
					_, werr := local.Write(unpadded)
					if p.config.WriteTimeout > 0 {
						_ = local.SetWriteDeadline(time.Time{})
					}
					if werr != nil {
						if !isClosedError(werr) {
							p.metrics.Errors.Inc()
						}
						return
					}
					p.metrics.BytesDown.Add(int64(len(unpadded)))
					p.metrics.PacketsDown.Inc()
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// ── Goroutine 3: chaff injection (Poisson-distributed decoy packets) ─────
	go func() {
		defer wg.Done()

		lambda := prof.ChaffLambda
		if lambda <= 0 {
			return
		}

		chaffSize := prof.PadderBuckets[0] // smallest bucket = smallest fingerprint
		rng := rand.New(rand.NewSource(time.Now().UnixNano() + 0xDEAD))
		poisson := morpher.NewPoissonProcessNoLock(time.Now().UnixNano()+0xBEEF, lambda)

		for {
			interval := poisson.NextInterval()
			select {
			case <-relayCtx.Done():
				return
			case <-time.After(interval):
			}

			payload := make([]byte, chaffSize)
			_, _ = rng.Read(payload)

			if werr := remote.WriteChaff(payload); werr != nil {
				// Tunnel closed — exit quietly, upload goroutine handles the error.
				return
			}
			p.metrics.ChaffSent.Add(int64(chaffSize))
		}
	}()

	wg.Wait()
}

// ── SOCKS5 negotiation ────────────────────────────────────────────────────────

func negotiateSOCKS5(conn net.Conn, br *bufio.Reader) (string, error) {
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(br, greeting); err != nil {
		return "", err
	}
	if greeting[0] != 0x05 {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return "", ErrInvalidGreeting
	}

	nMethods := int(greeting[1])
	if nMethods < 1 || nMethods > 255 {
		_, _ = conn.Write([]byte{0x05, 0xFF})
		return "", ErrInvalidGreeting
	}

	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return "", ErrInvalidGreeting
	}

	// We only support NO AUTH (0x00).
	_, _ = conn.Write([]byte{0x05, 0x00})

	request := make([]byte, 4)
	if _, err := io.ReadFull(br, request); err != nil {
		return "", ErrInvalidGreeting
	}

	if request[1] != 0x01 { // only CONNECT is supported
		_, _ = conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return "", ErrUnsupportedCommand
	}

	atyp := request[3]
	var addr string

	switch atyp {
	case 1: // IPv4
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		addr = net.IP(b).String()
	case 3: // domain
		lb := make([]byte, 1)
		if _, err := io.ReadFull(br, lb); err != nil {
			return "", err
		}
		dl := int(lb[0])
		if dl < 1 || dl > 255 {
			return "", ErrInvalidGreeting
		}
		db := make([]byte, dl)
		if _, err := io.ReadFull(br, db); err != nil {
			return "", err
		}
		addr = string(db)
	case 4: // IPv6
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		addr = "[" + net.IP(b).String() + "]"
	default:
		_, _ = conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return "", ErrUnsupportedAddrType
	}

	pb := make([]byte, 2)
	if _, err := io.ReadFull(br, pb); err != nil {
		return "", err
	}
	port := uint16(pb[0])<<8 | uint16(pb[1])

	return net.JoinHostPort(addr, formatPort(int(port))), nil
}

func (p *Proxy) replySOCKS5Success(conn net.Conn) {
	_, _ = conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

func (p *Proxy) replySOCKS5Error(conn net.Conn, rep byte) {
	_, _ = conn.Write([]byte{0x05, rep, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

// ── HTTP CONNECT parsing ──────────────────────────────────────────────────────

func parseHTTPConnect(br *bufio.Reader) (string, error) {
	req, err := readHTTPRequest(br)
	if err != nil {
		return "", err
	}
	if req.method != "CONNECT" {
		return "", fmt.Errorf("unsupported HTTP method: %s", req.method)
	}
	return req.host, nil
}

type httpReq struct {
	method string
	host   string
}

func readHTTPRequest(br *bufio.Reader) (*httpReq, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = trimCRLF(line)

	var method, host string
	_, err = fmt.Sscanf(line, "%s %s", &method, &host)
	if err != nil {
		return nil, fmt.Errorf("malformed HTTP request line: %w", err)
	}

	// Drain remaining headers.
	for {
		hdr, err := br.ReadString('\n')
		if err != nil {
			break
		}
		hdr = trimCRLF(hdr)
		if hdr == "" {
			break
		}
	}

	return &httpReq{method: method, host: host}, nil
}

func trimCRLF(s string) string {
	if len(s) >= 2 && s[len(s)-2] == '\r' && s[len(s)-1] == '\n' {
		return s[:len(s)-2]
	}
	if len(s) >= 1 && s[len(s)-1] == '\n' {
		return s[:len(s)-1]
	}
	return s
}

func writeHTTPStatus(conn net.Conn, code int, msg string) {
	_, _ = fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, msg)
}

func writeHTTP200(conn net.Conn) {
	_, _ = conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func formatPort(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [5]byte
	i := len(buf)
	for n > 0 && i > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func isClosedError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return true
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return err.Error() == "use of closed network connection"
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

func (p *Proxy) Shutdown(ctx context.Context) error {
	close(p.closeCh)
	if p.listener != nil {
		_ = p.listener.Close()
	}

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-done:
		return nil
	}
}

func (p *Proxy) Metrics() *metrics.ProxyMetrics {
	return p.metrics
}

func (p *Proxy) StartTime() time.Time {
	return p.startTime
}

// ── Sentinel errors ───────────────────────────────────────────────────────────

var (
	ErrUnsupportedCommand  = errors.New("unsupported SOCKS command")
	ErrUnsupportedAddrType = errors.New("unsupported address type")
	ErrInvalidGreeting     = errors.New("invalid SOCKS greeting")
)
