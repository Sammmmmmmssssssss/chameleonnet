package morpher

import (
	"context"
	"math/rand"
	"sync"
	"time"
)

type PacketType byte

const (
	PacketReal  PacketType = 0x00
	PacketChaff PacketType = 0x01
)

type Packet struct {
	Type    PacketType
	Payload []byte
}

type ChaffInjector struct {
	poisson    *PoissonProcessNoLock
	rng        *rand.Rand
	packetSize int
	writeCh    chan<- *Packet
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	stats      ChaffStats
	statsMu    sync.Mutex
}

type ChaffStats struct {
	Injected      int64
	Dropped       int64
	BytesSent     int64
	LastInjectAt  time.Time
}

func NewChaffInjector(seed int64, lambda float64, writeCh chan<- *Packet, packetSize int) *ChaffInjector {
	if packetSize <= 0 {
		packetSize = 512
	}
	if lambda <= 0 {
		lambda = 10
	}
	return &ChaffInjector{
		poisson:    NewPoissonProcessNoLock(seed, lambda),
		rng:        rand.New(rand.NewSource(seed + 1)),
		packetSize: packetSize,
		writeCh:    writeCh,
	}
}

func (c *ChaffInjector) Start(ctx context.Context) {
	c.ctx, c.cancel = context.WithCancel(ctx)
	c.wg.Add(1)
	go c.run()
}

func (c *ChaffInjector) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
}

func (c *ChaffInjector) run() {
	defer c.wg.Done()

	timer := time.NewTimer(c.poisson.NextInterval())
	defer timer.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-timer.C:
			packet := c.buildChaffPacket()
			select {
			case c.writeCh <- packet:
				c.statsMu.Lock()
				c.stats.Injected++
				c.stats.BytesSent += int64(len(packet.Payload))
				c.stats.LastInjectAt = time.Now()
				c.statsMu.Unlock()
			default:
				c.statsMu.Lock()
				c.stats.Dropped++
				c.statsMu.Unlock()
			}
			timer.Reset(c.poisson.NextInterval())
		}
	}
}

func (c *ChaffInjector) buildChaffPacket() *Packet {
	payloadSize := c.packetSize
	if c.rng != nil {
		jitter := c.rng.Intn(c.packetSize / 4)
		payloadSize = c.packetSize + jitter
	}
	payload := make([]byte, payloadSize)
	if c.rng != nil {
		c.rng.Read(payload)
	}
	return &Packet{
		Type:    PacketChaff,
		Payload: payload,
	}
}

func (c *ChaffInjector) SetLambda(lambda float64) {
	c.poisson.SetLambda(lambda)
}

func (c *ChaffInjector) Lambda() float64 {
	return c.poisson.Lambda()
}

func (c *ChaffInjector) Stats() ChaffStats {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	return c.stats
}

func (c *ChaffInjector) ResetStats() {
	c.statsMu.Lock()
	defer c.statsMu.Unlock()
	c.stats = ChaffStats{}
}

type ChaffFilter struct {
	stats    ChaffFilterStats
	statsMu  sync.Mutex
}

type ChaffFilterStats struct {
	RealPackets  int64
	ChaffPackets int64
	BytesDropped int64
}

func NewChaffFilter() *ChaffFilter {
	return &ChaffFilter{}
}

func (f *ChaffFilter) Filter(packet *Packet) bool {
	if packet == nil {
		return false
	}
	if packet.Type == PacketChaff {
		f.statsMu.Lock()
		f.stats.ChaffPackets++
		f.stats.BytesDropped += int64(len(packet.Payload))
		f.statsMu.Unlock()
		return false
	}
	f.statsMu.Lock()
	f.stats.RealPackets++
	f.statsMu.Unlock()
	return true
}

func (f *ChaffFilter) Stats() ChaffFilterStats {
	f.statsMu.Lock()
	defer f.statsMu.Unlock()
	return f.stats
}

func (f *ChaffFilter) ResetStats() {
	f.statsMu.Lock()
	defer f.statsMu.Unlock()
	f.stats = ChaffFilterStats{}
}

type ChaffMixer struct {
	injector     *ChaffInjector
	filter       *ChaffFilter
	inputCh      chan *Packet
	outputCh     chan *Packet
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
}

func NewChaffMixer(seed int64, lambda float64, packetSize int, chaffRatio float64) *ChaffMixer {
	inputCh := make(chan *Packet, 128)
	outputCh := make(chan *Packet, 256)

	w1 := chaffRatio
	if w1 <= 0 {
		w1 = 0.3
	}
	if w1 >= 1 {
		w1 = 0.9
	}
	finalLambda := lambda * w1

	injector := NewChaffInjector(seed, finalLambda, outputCh, packetSize)
	filter := NewChaffFilter()

	return &ChaffMixer{
		injector: injector,
		filter:   filter,
		inputCh:  inputCh,
		outputCh: outputCh,
	}
}

func (m *ChaffMixer) Start(ctx context.Context) {
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.injector.Start(ctx)

	m.wg.Add(1)
	go m.run()
}

func (m *ChaffMixer) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	m.wg.Wait()
	m.injector.Stop()
}

func (m *ChaffMixer) run() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case packet, ok := <-m.inputCh:
			if !ok {
				return
			}
			if m.filter.Filter(packet) {
				select {
				case m.outputCh <- packet:
				default:
				}
			}
		}
	}
}

func (m *ChaffMixer) Input() chan<- *Packet {
	return m.inputCh
}

func (m *ChaffMixer) Output() <-chan *Packet {
	return m.outputCh
}

func (m *ChaffMixer) Injector() *ChaffInjector {
	return m.injector
}

func (m *ChaffMixer) Filter() *ChaffFilter {
	return m.filter
}