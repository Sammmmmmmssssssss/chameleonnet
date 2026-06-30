package morpher

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

type PacketBufferPool interface {
	Get(size int) []byte
	Put(buf []byte)
}

type slicePoolAdapter struct {
	pool interface {
		Get(size int) *[]byte
		Put(buf *[]byte)
	}
}

func (a *slicePoolAdapter) Get(size int) []byte {
	buf := a.pool.Get(size)
	if buf == nil {
		return make([]byte, size)
	}
	return *buf
}

func (a *slicePoolAdapter) Put(buf []byte) {
	return
}

type MorphPipeline struct {
	shaper   *Shaper
	chaff    *ChaffInjector
	chaffCh  chan *Packet
	padder   *Padder
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	writeMu  sync.Mutex
	stats    PipelineStats
	statsMu  sync.RWMutex
}

type PipelineStats struct {
	BytesProcessed  int64
	PacketsSent     int64
	PacketsChaff    int64
	PacketsReal     int64
	TotalDelay      time.Duration
	StartedAt       time.Time
}

func NewMorphPipeline(seed int64, profile *Profile) *MorphPipeline {
	if profile == nil {
		profile = GenericProfile
	}
	chaffCh := make(chan *Packet, 256)
	chaff := NewChaffInjector(seed, profile.ChaffLambda, chaffCh, profile.PadderBuckets[0])

	return &MorphPipeline{
		shaper:  NewShaper(seed+1, profile.ShaperClient),
		chaff:   chaff,
		chaffCh: chaffCh,
		padder:  NewPadder(profile.PadderBuckets),
		stats:   PipelineStats{StartedAt: time.Now()},
	}
}

func (p *MorphPipeline) Start(ctx context.Context) {
	p.ctx, p.cancel = context.WithCancel(ctx)
	p.chaff.Start(ctx)
}

func (p *MorphPipeline) Stop() {
	if p.cancel != nil {
		p.cancel()
	}
	p.chaff.Stop()
	p.wg.Wait()
}

func (p *MorphPipeline) WriteMorphed(src io.Reader, dst io.Writer) error {
	buf := make([]byte, 65536)
	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			data := buf[:n]
			if err := p.writeOnePacket(dst, data); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p *MorphPipeline) writeOnePacket(dst io.Writer, data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()

	padded := p.padder.Pad(data)

	pkt := &Packet{
		Type:    PacketReal,
		Payload: padded,
	}

	for {
		select {
		case chaffPkt := <-p.chaffCh:
			if err := p.sendPacket(dst, chaffPkt); err != nil {
				return err
			}
		default:
			return p.sendPacket(dst, pkt)
		}
	}
}

func (p *MorphPipeline) sendPacket(dst io.Writer, pkt *Packet) error {
	delay := p.shaper.NextDelay()
	if delay > 0 {
		time.Sleep(delay)
	}

	if _, err := dst.Write(pkt.Payload); err != nil {
		return err
	}

	p.statsMu.Lock()
	p.stats.PacketsSent++
	p.stats.BytesProcessed += int64(len(pkt.Payload))
	p.stats.TotalDelay += delay
	if pkt.Type == PacketChaff {
		p.stats.PacketsChaff++
	} else {
		p.stats.PacketsReal++
	}
	p.statsMu.Unlock()

	return nil
}

func (p *MorphPipeline) ReadDemorphed(src io.Reader, dst io.Writer) error {
	buf := make([]byte, 65536)
	for {
		select {
		case <-p.ctx.Done():
			return p.ctx.Err()
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			data := buf[:n]
			cleaned, _ := p.padder.RemovePadding(data)

			cleaned, isChaff := p.detectAndRemoveChaff(cleaned)
			if isChaff {
				continue
			}

			if len(cleaned) > 0 {
				if _, err := dst.Write(cleaned); err != nil {
					return err
				}
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (p *MorphPipeline) detectAndRemoveChaff(data []byte) ([]byte, bool) {
	if len(data) == 0 {
		return data, false
	}
	if len(data) == 1 && data[0] == 0 {
		return nil, true
	}
	allZeros := true
	for _, b := range data {
		if b != 0 {
			allZeros = false
			break
		}
	}
	if allZeros {
		return nil, true
	}
	return data, false
}

func (p *MorphPipeline) Stats() PipelineStats {
	p.statsMu.RLock()
	defer p.statsMu.RUnlock()
	return p.stats
}

func (p *MorphPipeline) ResetStats() {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()
	p.stats = PipelineStats{StartedAt: time.Now()}
}

func (p *MorphPipeline) ChaffStats() ChaffStats {
	return p.chaff.Stats()
}

func (p *MorphPipeline) Shaper() *Shaper {
	return p.shaper
}

func (p *MorphPipeline) Padder() *Padder {
	return p.padder
}

func (p *MorphPipeline) SetShaperConfig(cfg ShaperConfig) {
	p.shaper.SetConfig(cfg)
}

var (
	ErrPipelineClosed   = errors.New("morph pipeline is closed")
	ErrWriteFailed      = errors.New("morph pipeline write failed")
	ErrReadFailed       = errors.New("morph pipeline read failed")
)

func ValidateMorphProfile(profile *Profile) error {
	if profile == nil {
		return errNilProfile
	}
	if profile.ChaffLambda <= 0 {
		return errInvalidChaffLambda
	}
	if profile.ChaffRatio < 0 || profile.ChaffRatio > 1 {
		return errInvalidChaffRatio
	}
	if len(profile.PadderBuckets) == 0 {
		return errNoPadderBuckets
	}
	return nil
}

type DemorphPipeline struct {
	padder  *Padder
	filter  *ChaffFilter
	shaper  *Shaper
	stats   DemorphStats
	statsMu sync.Mutex
}

type DemorphStats struct {
	BytesProcessed int64
	PacketsReal    int64
	PacketsChaff   int64
	TotalDelay     time.Duration
}

func NewDemorphPipeline(seed int64, profile *Profile) *DemorphPipeline {
	if profile == nil {
		profile = GenericProfile
	}
	return &DemorphPipeline{
		padder: NewPadder(profile.PadderBuckets),
		filter: NewChaffFilter(),
		shaper: NewShaper(seed+2, profile.ShaperServer),
	}
}

func (d *DemorphPipeline) ProcessRead(src io.Reader, dst io.Writer) error {
	buf := make([]byte, 65536)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			data := buf[:n]
			delay := d.shaper.NextDelay()
			if delay > 0 {
				time.Sleep(delay)
			}

			unpadded, _ := d.padder.RemovePadding(data)
			if len(unpadded) > 0 {
				allChaff := true
				for _, b := range unpadded {
					if b != 0 {
						allChaff = false
						break
					}
				}
				if allChaff {
					d.statsMu.Lock()
					d.stats.PacketsChaff++
					d.statsMu.Unlock()
					continue
				}

				if _, err := dst.Write(unpadded); err != nil {
					return err
				}
				d.statsMu.Lock()
				d.stats.BytesProcessed += int64(len(unpadded))
				d.stats.PacketsReal++
				d.stats.TotalDelay += delay
				d.statsMu.Unlock()
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func (d *DemorphPipeline) DemorphStats() DemorphStats {
	d.statsMu.Lock()
	defer d.statsMu.Unlock()
	return d.stats
}