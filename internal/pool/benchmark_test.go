package pool

import (
	"testing"
)

func BenchmarkBufferPool(b *testing.B) {
	p := NewBufferPool()
	buf := p.Get(4096)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.Get(4096)
	}
	p.Put(buf)
}
