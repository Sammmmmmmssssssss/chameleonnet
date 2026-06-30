package pool

import (
	"sync"
	"testing"
)

func TestNewBufferPool(t *testing.T) {
	p := NewBufferPool()
	if p == nil {
		t.Fatal("NewBufferPool() returned nil")
	}
	if len(p.sizeClasses) != 4 {
		t.Errorf("got %d size classes, want 4", len(p.sizeClasses))
	}
}

func TestBufferPoolGetPut(t *testing.T) {
	p := NewBufferPool()

	buf := p.Get(100)
	if buf == nil {
		t.Fatal("Get(100) returned nil")
	}
	if len(*buf) != 100 {
		t.Errorf("got length %d, want 100", len(*buf))
	}
	if cap(*buf) < 100 {
		t.Errorf("got cap %d, want >= 100", cap(*buf))
	}

	original := *buf
	original[0] = 42
	p.Put(buf)

	buf2 := p.Get(100)
	if buf2 == nil {
		t.Fatal("Get(100) returned nil on second call")
	}
	_ = buf2
}

func TestBufferPoolSizeDistribution(t *testing.T) {
	p := NewBufferPool()

	sizes := []int{100, 4000, 20000, 50000, 60000}
	for _, size := range sizes {
		buf := p.Get(size)
		if buf == nil {
			t.Fatalf("Get(%d) returned nil", size)
		}
		if len(*buf) != size {
			t.Errorf("Get(%d) returned length %d", size, len(*buf))
		}
		(*buf)[size-1] = byte(size)
		p.Put(buf)
	}
}

func TestBufferPoolGetZeroSize(t *testing.T) {
	p := NewBufferPool()
	buf := p.Get(0)
	if buf != nil {
		t.Error("Get(0) should return nil")
	}

	buf = p.Get(-1)
	if buf != nil {
		t.Error("Get(-1) should return nil")
	}
}

func TestBufferPoolPutNil(t *testing.T) {
	p := NewBufferPool()
	p.Put(nil)
}

func TestNewSizedBufferPool(t *testing.T) {
	p := NewSizedBufferPool(32768)
	if p == nil {
		t.Fatal("NewSizedBufferPool() returned nil")
	}
	if p.Size() != 32768 {
		t.Errorf("Size() = %d, want 32768", p.Size())
	}
}

func TestSizedBufferPoolGetPut(t *testing.T) {
	p := NewSizedBufferPool(32768)

	buf := p.Get()
	if buf == nil {
		t.Fatal("Get() returned nil")
	}
	if len(*buf) != 32768 {
		t.Errorf("len = %d, want 32768", len(*buf))
	}

	(*buf)[0] = 0xFF
	(*buf)[32767] = 0xFE

	if p.TotalInUse() != 1 {
		t.Errorf("TotalInUse = %d, want 1", p.TotalInUse())
	}

	p.Put(buf)

	if p.TotalInUse() != 0 {
		t.Errorf("TotalInUse = %d, want 0 after Put", p.TotalInUse())
	}
}

func TestSizedBufferPoolPutWrongSize(t *testing.T) {
	p := NewSizedBufferPool(32768)
	buf := make([]byte, 16384)
	p.Put(&buf)

	if p.TotalInUse() != 0 {
		t.Errorf("TotalInUse = %d, want 0", p.TotalInUse())
	}
}

func TestSizedBufferPoolPutNil(t *testing.T) {
	p := NewSizedBufferPool(32768)
	p.Put(nil)
}

func TestConcurrentBufferPool(t *testing.T) {
	p := NewBufferPool()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf := p.Get(4096)
				if buf != nil {
					(*buf)[0] = byte(j)
					p.Put(buf)
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrentSizedBufferPool(t *testing.T) {
	p := NewSizedBufferPool(32768)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				buf := p.Get()
				(*buf)[0] = byte(j)
				p.Put(buf)
			}
		}()
	}
	wg.Wait()
}

func TestGlobalPool(t *testing.T) {
	p := GetGlobalPool()
	p2 := GetGlobalPool()
	if p != p2 {
		t.Error("GetGlobalPool() returned different instances")
	}
}

func TestConveniencePools(t *testing.T) {
	buf32 := Get32KB()
	if buf32 == nil {
		t.Fatal("Get32KB() returned nil")
	}
	if len(*buf32) != 32768 {
		t.Errorf("len = %d, want 32768", len(*buf32))
	}
	Put32KB(buf32)

	buf64 := Get64KB()
	if buf64 == nil {
		t.Fatal("Get64KB() returned nil")
	}
	if len(*buf64) != 65536 {
		t.Errorf("len = %d, want 65536", len(*buf64))
	}
	Put64KB(buf64)

	buf128 := Get128KB()
	if buf128 == nil {
		t.Fatal("Get128KB() returned nil")
	}
	if len(*buf128) != 131072 {
		t.Errorf("len = %d, want 131072", len(*buf128))
	}
	Put128KB(buf128)
}

func TestPoolStats(t *testing.T) {
	p := NewSizedBufferPool(32768)

	buf := p.Get()
	stats := p.Stats()
	if stats.TotalInUse != 1 {
		t.Errorf("Stats.TotalInUse = %d, want 1", stats.TotalInUse)
	}
	if stats.Size != 32768 {
		t.Errorf("Stats.Size = %d, want 32768", stats.Size)
	}

	p.Put(buf)
	stats = p.Stats()
	if stats.TotalInUse != 0 {
		t.Errorf("Stats.TotalInUse = %d, want 0", stats.TotalInUse)
	}
}

func TestGetPoolStats(t *testing.T) {
	ResetStats()
	stats := GetPoolStats()
	if len(stats) != 3 {
		t.Errorf("got %d pool stats, want 3", len(stats))
	}
	for name, s := range stats {
		if s.Size <= 0 {
			t.Errorf("pool %q has invalid size %d", name, s.Size)
		}
	}
}

func TestResetStats(t *testing.T) {
	p := NewSizedBufferPool(32768)
	buf := p.Get()
	p.Put(buf)
	ResetStats()
	stats := GetPoolStats()
	for _, s := range stats {
		if s.TotalInUse != 0 {
			t.Errorf("after reset, TotalInUse = %d, want 0", s.TotalInUse)
		}
	}
}

func BenchmarkBufferPoolGetPut(b *testing.B) {
	p := NewBufferPool()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := p.Get(4096)
		p.Put(buf)
	}
}

func BenchmarkSizedBufferPoolGetPut(b *testing.B) {
	p := NewSizedBufferPool(32768)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := p.Get()
		p.Put(buf)
	}
}

func BenchmarkConveniencePool(b *testing.B) {
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf := Get32KB()
		Put32KB(buf)
	}
}