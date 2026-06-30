package morpher

import (
	"crypto/rand"
	"testing"
	"time"
)

func BenchmarkShaperDelay(b *testing.B) {
	cfg := ShaperConfig{
		W1:       0.7,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: 5 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
	}
	s := NewShaper(42, cfg)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = s.NextDelay()
	}
}

func BenchmarkPoissonSample(b *testing.B) {
	p := NewPoissonProcess(42, 10.0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.NextInterval()
	}
}

func BenchmarkPad(b *testing.B) {
	padder := NewPadder([]int{64, 128, 256, 512, 1024, 2048, 4096})
	data := make([]byte, 100)
	rand.Read(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = padder.Pad(data)
	}
}

func BenchmarkBuildChaffPacket(b *testing.B) {
	writeCh := make(chan *Packet, 100)
	inj := NewChaffInjector(42, 10.0, writeCh, 64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = inj.buildChaffPacket()
	}
	close(writeCh)
}
