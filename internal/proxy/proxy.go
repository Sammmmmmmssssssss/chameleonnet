package proxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"chameleonnet/internal/config"
	"chameleonnet/internal/metrics"
	"chameleonnet/internal/pool"
	"chameleonnet/internal/tunnel"
)

type Protocol byte

const (
	ProtocolUnknown Protocol = 0
	ProtocolSOCKS5  Protocol = 5
	ProtocolHTTP    Protocol = 'H'
)

type Server interface {
	ListenAndServe(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type InstrumentedServer interface {
	Server
	Metrics() *metrics.ProxyMetrics
	StartTime() time.Time
}

type Proxy struct {
	config    *config.Config
	listener  net.Listener
	bufPool   *pool.BufferPool
	metrics   *metrics.ProxyMetrics
	wg        sync.WaitGroup
	closeCh   chan struct{}
	startTime time.Time
}

func NewProxy(cfg *config.Config, bp *pool.BufferPool, m *metrics.ProxyMetrics) *Proxy {
	return &Proxy{
		config:    cfg,
		bufPool:   bp,
		metrics:   m,
		closeCh:   make(chan struct{}),
		startTime: time.Now(),
	}
}

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
				if ne, ok := err.(net.Error); ok && ne.Temporary() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return
			}
		}

		if tcp, ok := conn.(*net.TCPConn); ok {
			tcp.SetKeepAlive(true)
			tcp.SetKeepAlivePeriod(30 * time.Second)
			tcp.SetNoDelay(true)
			tcp.SetReadBuffer(p.config.BufferSize)
			tcp.SetWriteBuffer(p.config.BufferSize)
		}

		p.metrics.ActiveConns.Inc()
		p.metrics.TotalConns.Inc()

		p.wg.Add(1)
		go func(c net.Conn) {
			defer p.wg.Done()
			defer c.Close()
			defer p.metrics.ActiveConns.Dec()
			p.handleConn(ctx, c)
		}(conn)
	}
}

func (p *Proxy) handleConn(ctx context.Context, conn net.Conn) {
	if p.config.HandshakeTimeout > 0 {
		conn.SetDeadline(time.Now().Add(p.config.HandshakeTimeout.Duration()))
		defer conn.SetDeadline(time.Time{})
	}

	br := bufio.NewReaderSize(conn, 256)
	proto, err := p.detectProtocol(br)
	if err != nil {
		p.metrics.Errors.Inc()
		return
	}

	conn.SetDeadline(time.Time{})

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
	defer remote.Close()

	if err := remote.Handshake(targetAddr); err != nil {
		p.replySOCKS5Error(conn, 0x01)
		p.metrics.Errors.Inc()
		return
	}

	p.replySOCKS5Success(conn)
	p.relayConnections(conn, remote)
}

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
	defer remote.Close()

	if err := remote.Handshake(targetAddr); err != nil {
		writeHTTPStatus(conn, 502, "Bad Gateway")
		p.metrics.Errors.Inc()
		return
	}

	writeHTTP200(conn)
	p.relayConnections(conn, remote)
}

func (p *Proxy) relayConnections(local, remote net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		b := p.bufPool.Get(p.config.BufferSize)
		defer p.bufPool.Put(b)
		n, err := io.CopyBuffer(remote, local, *b)
		p.metrics.BytesUp.Add(n)
		if err != nil && !isClosedError(err) {
			p.metrics.Errors.Inc()
		}
	}()

	go func() {
		defer wg.Done()
		b := p.bufPool.Get(p.config.BufferSize)
		defer p.bufPool.Put(b)
		n, err := io.CopyBuffer(local, remote, *b)
		p.metrics.BytesDown.Add(n)
		if err != nil && !isClosedError(err) {
			p.metrics.Errors.Inc()
		}
	}()

	wg.Wait()
}

func negotiateSOCKS5(conn net.Conn, br *bufio.Reader) (string, error) {
	greeting := make([]byte, 2)
	if _, err := io.ReadFull(br, greeting); err != nil {
		return "", err
	}
	if greeting[0] != 0x05 {
		conn.Write([]byte{0x05, 0xFF})
		return "", ErrInvalidGreeting
	}

	nMethods := int(greeting[1])
	if nMethods < 1 || nMethods > 255 {
		conn.Write([]byte{0x05, 0xFF})
		return "", ErrInvalidGreeting
	}

	methods := make([]byte, nMethods)
	if _, err := io.ReadFull(br, methods); err != nil {
		return "", ErrInvalidGreeting
	}

	conn.Write([]byte{0x05, 0x00})

	request := make([]byte, 4)
	if _, err := io.ReadFull(br, request); err != nil {
		return "", ErrInvalidGreeting
	}

	if request[1] != 0x01 {
		conn.Write([]byte{0x05, 0x07, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return "", ErrUnsupportedCommand
	}

	atyp := request[3]
	var addr string
	var port uint16

	switch atyp {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		addr = net.IP(b).String()
	case 3:
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
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(br, b); err != nil {
			return "", err
		}
		addr = "[" + net.IP(b).String() + "]"
	default:
		conn.Write([]byte{0x05, 0x08, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
		return "", ErrUnsupportedAddrType
	}

	pb := make([]byte, 2)
	if _, err := io.ReadFull(br, pb); err != nil {
		return "", err
	}
	port = uint16(pb[0])<<8 | uint16(pb[1])

	return net.JoinHostPort(addr, formatPort(int(port))), nil
}

func (p *Proxy) replySOCKS5Success(conn net.Conn) {
	conn.Write([]byte{0x05, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

func (p *Proxy) replySOCKS5Error(conn net.Conn, rep byte) {
	conn.Write([]byte{0x05, rep, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
}

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
	fmt.Fprintf(conn, "HTTP/1.1 %d %s\r\nContent-Length: 0\r\nConnection: close\r\n\r\n", code, msg)
}

func writeHTTP200(conn net.Conn) {
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
}

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

func (p *Proxy) Shutdown(ctx context.Context) error {
	close(p.closeCh)
	if p.listener != nil {
		p.listener.Close()
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

var (
	ErrUnsupportedCommand  = errors.New("unsupported SOCKS command")
	ErrUnsupportedAddrType = errors.New("unsupported address type")
	ErrInvalidGreeting     = errors.New("invalid SOCKS greeting")
)
