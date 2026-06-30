package metrics

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

type Counter struct {
	val atomic.Int64
}

func (c *Counter) Add(n int64) {
	c.val.Add(n)
}

func (c *Counter) Inc() {
	c.val.Add(1)
}

func (c *Counter) Value() int64 {
	return c.val.Load()
}

func (c *Counter) Reset() {
	c.val.Store(0)
}

type Gauge struct {
	val atomic.Int64
}

func (g *Gauge) Set(n int64) {
	g.val.Store(n)
}

func (g *Gauge) Add(n int64) {
	g.val.Add(n)
}

func (g *Gauge) Inc() {
	g.val.Add(1)
}

func (g *Gauge) Dec() {
	g.val.Add(-1)
}

func (g *Gauge) Value() int64 {
	return g.val.Load()
}

func (g *Gauge) Reset() {
	g.val.Store(0)
}

type Histogram struct {
	buckets  []int64
	counts   []atomic.Int64
	total    atomic.Int64
	count    atomic.Int64
}

func NewHistogram(buckets []int64) *Histogram {
	return &Histogram{
		buckets: buckets,
		counts:  make([]atomic.Int64, len(buckets)+1),
	}
}

func (h *Histogram) Observe(val int64) {
	h.total.Add(val)
	c := h.count.Add(1)
	_ = c
	for i, b := range h.buckets {
		if val <= b {
			h.counts[i].Add(1)
			return
		}
	}
	h.counts[len(h.buckets)].Add(1)
}

func (h *Histogram) Mean() float64 {
	c := h.count.Load()
	if c == 0 {
		return 0
	}
	return float64(h.total.Load()) / float64(c)
}

func (h *Histogram) Count() int64 {
	return h.count.Load()
}

func (h *Histogram) Reset() {
	h.total.Store(0)
	h.count.Store(0)
	for i := range h.counts {
		h.counts[i].Store(0)
	}
}

type PoolAllocationTracker struct {
	lastTotal     int64
	peakDelta     int64
	spikeCount    int64
	spikeMu       atomic.Int64
}

func (t *PoolAllocationTracker) Observe(currentTotal int64) {
	delta := currentTotal - t.lastTotal
	if delta > t.peakDelta {
		t.peakDelta = delta
	}
	if delta > 100 {
		t.spikeMu.Add(1)
	}
	t.lastTotal = currentTotal
}

func (t *PoolAllocationTracker) PeakDelta() int64 {
	return t.peakDelta
}

func (t *PoolAllocationTracker) SpikeCount() int64 {
	return t.spikeMu.Load()
}

func (t *PoolAllocationTracker) Reset() {
	t.lastTotal = 0
	t.peakDelta = 0
	t.spikeMu.Store(0)
}

type ProxyMetrics struct {
	BytesUp       Counter
	BytesDown     Counter
	ChaffSent     Counter
	ChaffReceived Counter
	PacketsUp     Counter
	PacketsDown   Counter
	PacketChaff   Counter
	ActiveConns   Gauge
	TotalConns    Counter
	Errors        Counter
	PoolAllocs    Counter
	Latency       *Histogram
	startTime     time.Time
}

func NewProxyMetrics() *ProxyMetrics {
	return &ProxyMetrics{
		Latency: NewHistogram([]int64{
			1_000_000,
			5_000_000,
			10_000_000,
			50_000_000,
			100_000_000,
			500_000_000,
			1_000_000_000,
		}),
		startTime: time.Now(),
	}
}

type MetricsSnapshot struct {
	BytesUp       int64
	BytesDown     int64
	BytesTotal    int64
	ChaffSent     int64
	ChaffReceived int64
	PacketsUp     int64
	PacketsDown   int64
	PacketsChaff  int64
	PacketsTotal  int64
	ActiveConns   int64
	TotalConns    int64
	Errors        int64
	PoolAllocs    int64
	LatencyMeanNs int64
	LatencyCount  int64
	Uptime        time.Duration
	Timestamp     time.Time
}

func (m *ProxyMetrics) Snapshot() MetricsSnapshot {
	s := MetricsSnapshot{
		BytesUp:       m.BytesUp.Value(),
		BytesDown:     m.BytesDown.Value(),
		ChaffSent:     m.ChaffSent.Value(),
		ChaffReceived: m.ChaffReceived.Value(),
		PacketsUp:     m.PacketsUp.Value(),
		PacketsDown:   m.PacketsDown.Value(),
		PacketsChaff:  m.PacketChaff.Value(),
		ActiveConns:   m.ActiveConns.Value(),
		TotalConns:    m.TotalConns.Value(),
		Errors:        m.Errors.Value(),
		PoolAllocs:    m.PoolAllocs.Value(),
		LatencyMeanNs: int64(m.Latency.Mean()),
		LatencyCount:  m.Latency.Count(),
		Uptime:        time.Since(m.startTime),
		Timestamp:     time.Now(),
	}
	s.BytesTotal = s.BytesUp + s.BytesDown
	s.PacketsTotal = s.PacketsUp + s.PacketsDown + s.PacketsChaff
	return s
}

func (s MetricsSnapshot) Fprint(w io.Writer) {
	fmt.Fprintf(w, "\n=== ChameleonNet Metrics ===\n")
	fmt.Fprintf(w, "Uptime:        %s\n", s.Uptime.Round(time.Second))
	fmt.Fprintf(w, "Time:          %s\n", s.Timestamp.Format("15:04:05"))
	fmt.Fprintf(w, "--- Traffic ---\n")
	fmt.Fprintf(w, "Bytes Up:      %s (%d pkts)\n", formatBytes(s.BytesUp), s.PacketsUp)
	fmt.Fprintf(w, "Bytes Down:    %s (%d pkts)\n", formatBytes(s.BytesDown), s.PacketsDown)
	fmt.Fprintf(w, "Chaff Sent:    %s (%d pkts)\n", formatBytes(s.ChaffSent), s.PacketsChaff)
	fmt.Fprintf(w, "Total:         %s (%d pkts)\n", formatBytes(s.BytesTotal), s.PacketsTotal)
	fmt.Fprintf(w, "--- Connections ---\n")
	fmt.Fprintf(w, "Active:        %d\n", s.ActiveConns)
	fmt.Fprintf(w, "Total:         %d\n", s.TotalConns)
	fmt.Fprintf(w, "--- Health ---\n")
	fmt.Fprintf(w, "Errors:        %d\n", s.Errors)
	fmt.Fprintf(w, "Pool Allocs:   %d\n", s.PoolAllocs)
	if s.LatencyCount > 0 {
		fmt.Fprintf(w, "Latency Mean:  %s (%d samples)\n",
			time.Duration(s.LatencyMeanNs).Round(time.Microsecond), s.LatencyCount)
	}
	fmt.Fprintf(w, "============================\n")
}

func (m *ProxyMetrics) Reset() {
	m.BytesUp.Reset()
	m.BytesDown.Reset()
	m.ChaffSent.Reset()
	m.ChaffReceived.Reset()
	m.PacketsUp.Reset()
	m.PacketsDown.Reset()
	m.PacketChaff.Reset()
	m.ActiveConns.Reset()
	m.TotalConns.Reset()
	m.Errors.Reset()
	m.PoolAllocs.Reset()
	m.Latency.Reset()
}

func (m *ProxyMetrics) StartTime() time.Time {
	return m.startTime
}

func formatBytes(b int64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

type ChaffMetrics struct {
	Injected      atomic.Int64
	Dropped       atomic.Int64
	BytesSent     atomic.Int64
	PacketsFiltered atomic.Int64
}

func (c *ChaffMetrics) Snapshot() (injected, dropped, bytes, filtered int64) {
	return c.Injected.Load(), c.Dropped.Load(), c.BytesSent.Load(), c.PacketsFiltered.Load()
}

type MemMetrics struct {
	HeapInuse     uint64
	HeapAlloc     uint64
	Sys           uint64
	NumGC         uint32
	PauseTotalNs  uint64
	Goroutines    int
}

func ReadMem() MemMetrics {
	return MemMetrics{
		Goroutines: 0,
	}
}