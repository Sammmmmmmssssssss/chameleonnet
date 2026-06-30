package pool

import (
	"sync"
	"sync/atomic"
)

type BufferPool struct {
	pools          []*sync.Pool
	sizeClasses    []int
	totalAllocated atomic.Int64
	totalInUse     atomic.Int64
}

var globalPool *BufferPool
var once sync.Once

func GetGlobalPool() *BufferPool {
	once.Do(func() {
		globalPool = NewBufferPool()
	})
	return globalPool
}

func NewBufferPool() *BufferPool {
	sizeClasses := []int{
		4096,
		16384,
		32768,
		65536,
	}
	bp := &BufferPool{
		sizeClasses: sizeClasses,
	}
	pools := make([]*sync.Pool, len(sizeClasses))
	for i, size := range sizeClasses {
		size := size
		pools[i] = &sync.Pool{
			New: func() interface{} {
				bp.totalAllocated.Add(1)
				buf := make([]byte, size)
				return &buf
			},
		}
	}
	bp.pools = pools
	return bp
}

func (p *BufferPool) Get(size int) *[]byte {
	if size <= 0 {
		return nil
	}
	idx := p.findSizeClass(size)
	if idx == -1 {
		return nil
	}
	v := p.pools[idx].Get()
	if v == nil {
		return nil
	}
	buf := v.(*[]byte)
	*buf = (*buf)[:size]
	p.totalInUse.Add(1)
	return buf
}

func (p *BufferPool) Put(buf *[]byte) {
	if buf == nil {
		return
	}
	size := cap(*buf)
	idx := p.findSizeClass(size)
	if idx == -1 {
		return
	}
	*buf = (*buf)[:cap(*buf)]
	p.pools[idx].Put(buf)
	p.totalInUse.Add(-1)
}

func (p *BufferPool) findSizeClass(size int) int {
	for i, classSize := range p.sizeClasses {
		if size <= classSize {
			return i
		}
	}
	return -1
}

func (p *BufferPool) TotalAllocated() int64 {
	return p.totalAllocated.Load()
}

func (p *BufferPool) TotalInUse() int64 {
	return p.totalInUse.Load()
}

func (p *BufferPool) Stats() PoolStats {
	return PoolStats{
		TotalAllocated: p.totalAllocated.Load(),
		TotalInUse:     p.totalInUse.Load(),
		SizeClasses:    append([]int(nil), p.sizeClasses...),
	}
}

type PoolStats struct {
	TotalAllocated int64
	TotalInUse     int64
	SizeClasses    []int
}

type SizedBufferPool struct {
	pool           *sync.Pool
	size           int
	totalAllocated atomic.Int64
	totalInUse     atomic.Int64
}

func NewSizedBufferPool(size int) *SizedBufferPool {
	sbp := &SizedBufferPool{
		size: size,
	}
	sbp.pool = &sync.Pool{
		New: func() interface{} {
			sbp.totalAllocated.Add(1)
			buf := make([]byte, size)
			return &buf
		},
	}
	return sbp
}

func (p *SizedBufferPool) Get() *[]byte {
	v := p.pool.Get()
	if v == nil {
		return nil
	}
	buf := v.(*[]byte)
	*buf = (*buf)[:p.size]
	p.totalInUse.Add(1)
	return buf
}

func (p *SizedBufferPool) Put(buf *[]byte) {
	if buf == nil {
		return
	}
	if cap(*buf) != p.size {
		return
	}
	*buf = (*buf)[:p.size]
	p.pool.Put(buf)
	p.totalInUse.Add(-1)
}

func (p *SizedBufferPool) Size() int {
	return p.size
}

func (p *SizedBufferPool) TotalAllocated() int64 {
	return p.totalAllocated.Load()
}

func (p *SizedBufferPool) TotalInUse() int64 {
	return p.totalInUse.Load()
}

func (p *SizedBufferPool) Stats() SizedPoolStats {
	return SizedPoolStats{
		Size:           p.size,
		TotalAllocated: p.totalAllocated.Load(),
		TotalInUse:     p.totalInUse.Load(),
	}
}

type SizedPoolStats struct {
	Size           int
	TotalAllocated int64
	TotalInUse     int64
}

var defaultPools struct {
	p32KB  *SizedBufferPool
	p64KB  *SizedBufferPool
	p128KB *SizedBufferPool
}

func init() {
	defaultPools.p32KB = NewSizedBufferPool(32768)
	defaultPools.p64KB = NewSizedBufferPool(65536)
	defaultPools.p128KB = NewSizedBufferPool(131072)
}

func Get32KB() *[]byte {
	return defaultPools.p32KB.Get()
}

func Put32KB(buf *[]byte) {
	defaultPools.p32KB.Put(buf)
}

func Get64KB() *[]byte {
	return defaultPools.p64KB.Get()
}

func Put64KB(buf *[]byte) {
	defaultPools.p64KB.Put(buf)
}

func Get128KB() *[]byte {
	return defaultPools.p128KB.Get()
}

func Put128KB(buf *[]byte) {
	defaultPools.p128KB.Put(buf)
}

func GetPoolStats() map[string]SizedPoolStats {
	return map[string]SizedPoolStats{
		"32KB":  defaultPools.p32KB.Stats(),
		"64KB":  defaultPools.p64KB.Stats(),
		"128KB": defaultPools.p128KB.Stats(),
	}
}

func ResetStats() {
	defaultPools.p32KB.totalAllocated.Store(0)
	defaultPools.p32KB.totalInUse.Store(0)
	defaultPools.p64KB.totalAllocated.Store(0)
	defaultPools.p64KB.totalInUse.Store(0)
	defaultPools.p128KB.totalAllocated.Store(0)
	defaultPools.p128KB.totalInUse.Store(0)
}