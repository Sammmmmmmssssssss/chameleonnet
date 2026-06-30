package morpher

import (
	"context"
	"testing"
	"time"
)

func TestNewChaffInjector(t *testing.T) {
	ch := make(chan *Packet, 256)
	ci := NewChaffInjector(42, 50.0, ch, 512)
	if ci == nil {
		t.Fatal("NewChaffInjector() returned nil")
	}
	if ci.Lambda() != 50.0 {
		t.Errorf("Lambda() = %v, want 50.0", ci.Lambda())
	}
}

func TestChaffInjectorStartStop(t *testing.T) {
	ch := make(chan *Packet, 256)
	ci := NewChaffInjector(42, 100.0, ch, 512)

	ctx, cancel := context.WithCancel(context.Background())
	ci.Start(ctx)

	time.Sleep(50 * time.Millisecond)

	ci.Stop()
	cancel()

	stats := ci.Stats()
	t.Logf("Chaff stats: injected=%d dropped=%d", stats.Injected, stats.Dropped)
}

func TestChaffInjectorBuildPacket(t *testing.T) {
	ch := make(chan *Packet, 256)
	ci := NewChaffInjector(42, 50.0, ch, 512)

	ctx, cancel := context.WithCancel(context.Background())
	ci.Start(ctx)

	time.Sleep(20 * time.Millisecond)

	select {
	case pkt := <-ch:
		if pkt.Type != PacketChaff {
			t.Errorf("Type = %v, want PacketChaff", pkt.Type)
		}
		if len(pkt.Payload) < 512 {
			t.Errorf("payload len = %d, want >= 512", len(pkt.Payload))
		}
	default:
	}

	ci.Stop()
	cancel()
}

func TestChaffFilter(t *testing.T) {
	f := NewChaffFilter()

	realPkt := &Packet{Type: PacketReal, Payload: []byte("data")}
	chaffPkt := &Packet{Type: PacketChaff, Payload: []byte("chaff")}

	if !f.Filter(realPkt) {
		t.Error("real packet should pass filter")
	}
	if f.Filter(chaffPkt) {
		t.Error("chaff packet should be filtered")
	}

	stats := f.Stats()
	if stats.RealPackets != 1 {
		t.Errorf("RealPackets = %d, want 1", stats.RealPackets)
	}
	if stats.ChaffPackets != 1 {
		t.Errorf("ChaffPackets = %d, want 1", stats.ChaffPackets)
	}
}

func TestChaffFilterNil(t *testing.T) {
	f := NewChaffFilter()
	if f.Filter(nil) {
		t.Error("nil packet should not pass filter")
	}
}

func TestChaffFilterStats(t *testing.T) {
	f := NewChaffFilter()
	stats := f.Stats()
	if stats.RealPackets != 0 || stats.ChaffPackets != 0 {
		t.Error("initial stats should be zero")
	}

	f.Filter(&Packet{Type: PacketReal, Payload: []byte{}})
	f.Filter(&Packet{Type: PacketChaff, Payload: []byte{}})

	stats = f.Stats()
	if stats.RealPackets != 1 {
		t.Errorf("RealPackets = %d, want 1", stats.RealPackets)
	}
	if stats.ChaffPackets != 1 {
		t.Errorf("ChaffPackets = %d, want 1", stats.ChaffPackets)
	}

	f.ResetStats()
	stats = f.Stats()
	if stats.RealPackets != 0 {
		t.Errorf("after reset, RealPackets = %d, want 0", stats.RealPackets)
	}
}

func TestChaffMixer(t *testing.T) {
	m := NewChaffMixer(42, 50.0, 512, 0.3)
	if m == nil {
		t.Fatal("NewChaffMixer() returned nil")
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.Start(ctx)

	m.Input() <- &Packet{Type: PacketReal, Payload: []byte("test data")}

	select {
	case pkt := <-m.Output():
		if pkt.Type != PacketReal {
			t.Errorf("output packet type = %v, want PacketReal", pkt.Type)
		}
		if string(pkt.Payload) != "test data" {
			t.Errorf("payload = %q, want %q", pkt.Payload, "test data")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for output")
	}

	m.Stop()
	cancel()
}

func TestChaffInjectorSetLambda(t *testing.T) {
	ch := make(chan *Packet, 256)
	ci := NewChaffInjector(42, 50.0, ch, 512)
	ci.SetLambda(100.0)
	if ci.Lambda() != 100.0 {
		t.Errorf("Lambda() = %v, want 100.0", ci.Lambda())
	}
}

func TestChaffInjectorDefaultValues(t *testing.T) {
	ch := make(chan *Packet, 1)
	ci := NewChaffInjector(0, 0, ch, 0)
	if ci == nil {
		t.Fatal("NewChaffInjector with zeros returned nil")
	}
}

func TestChaffInjectorResetStats(t *testing.T) {
	ch := make(chan *Packet, 256)
	ci := NewChaffInjector(42, 100.0, ch, 512)

	ctx, cancel := context.WithCancel(context.Background())
	ci.Start(ctx)
	time.Sleep(20 * time.Millisecond)
	ci.Stop()
	cancel()

	ci.ResetStats()
	stats := ci.Stats()
	if stats.Injected != 0 {
		t.Errorf("after reset, Injected = %d, want 0", stats.Injected)
	}
}