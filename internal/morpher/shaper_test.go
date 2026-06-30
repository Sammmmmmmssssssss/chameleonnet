package morpher

import (
	"math"
	"testing"
	"time"
)

func TestNewPoissonProcess(t *testing.T) {
	p := NewPoissonProcess(42, 50.0)
	if p == nil {
		t.Fatal("NewPoissonProcess() returned nil")
	}
	if p.Lambda() != 50.0 {
		t.Errorf("Lambda() = %v, want 50.0", p.Lambda())
	}
}

func TestPoissonProcessZeroLambda(t *testing.T) {
	p := NewPoissonProcess(42, 0)
	if p == nil {
		t.Fatal("NewPoissonProcess(0) returned nil")
	}
	if p.Lambda() <= 0 {
		t.Errorf("Lambda() = %v, want > 0", p.Lambda())
	}
}

func TestPoissonProcessMeanInterval(t *testing.T) {
	lambda := 100.0
	p := NewPoissonProcess(42, lambda)

	var total time.Duration
	samples := 10000
	for i := 0; i < samples; i++ {
		total += p.NextInterval()
	}

	mean := total.Seconds() / float64(samples)
	expected := 1.0 / lambda
	tolerance := 0.15

	if math.Abs(mean-expected)/expected > tolerance {
		t.Errorf("mean interval = %v, expected %v (tolerance %v)", mean, expected, tolerance)
	}
}

func TestPoissonProcessNoLock(t *testing.T) {
	p := NewPoissonProcessNoLock(42, 50.0)
	if p == nil {
		t.Fatal("NewPoissonProcessNoLock() returned nil")
	}

	var total float64
	samples := 10000
	for i := 0; i < samples; i++ {
		total += p.NextIntervalFloat64()
	}

	mean := total / float64(samples)
	expected := 1.0 / 50.0
	tolerance := 0.15

	if math.Abs(mean-expected)/expected > tolerance {
		t.Errorf("mean interval = %v, expected %v", mean, expected)
	}
}

func TestPoissonProcessSampleCount(t *testing.T) {
	p := NewPoissonProcess(42, 10.0)
	count := p.SampleCount(time.Second)
	if count < 0 {
		t.Error("SampleCount returned negative value")
	}
}

func TestPoissonProcessSetLambda(t *testing.T) {
	p := NewPoissonProcess(42, 50.0)
	p.SetLambda(75.0)
	if p.Lambda() != 75.0 {
		t.Errorf("Lambda() = %v, want 75.0", p.Lambda())
	}
}

func TestPoissonProcessReset(t *testing.T) {
	p1 := NewPoissonProcess(42, 50.0)
	p2 := NewPoissonProcess(42, 50.0)

	i1 := p1.NextInterval()
	i2 := p2.NextInterval()

	if i1 != i2 {
		t.Errorf("same seed should produce same first interval: %v vs %v", i1, i2)
	}
}

func TestNewShaper(t *testing.T) {
	cfg := ShaperConfig{
		W1:       0.7,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: time.Millisecond,
		MaxDelay: time.Second,
	}
	s := NewShaper(42, cfg)
	if s == nil {
		t.Fatal("NewShaper() returned nil")
	}
}

func TestShaperDelayBounds(t *testing.T) {
	cfg := ShaperConfig{
		W1:       0.5,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: 5 * time.Millisecond,
		MaxDelay: 100 * time.Millisecond,
	}
	s := NewShaper(42, cfg)

	for i := 0; i < 1000; i++ {
		d := s.NextDelay()
		if d < cfg.MinDelay {
			t.Errorf("delay %v below MinDelay %v", d, cfg.MinDelay)
		}
		if d > cfg.MaxDelay {
			t.Errorf("delay %v above MaxDelay %v", d, cfg.MaxDelay)
		}
	}
}

func TestShaperDefaultConfig(t *testing.T) {
	cfg := ShaperConfig{
		MinDelay: time.Millisecond,
		MaxDelay: time.Second,
	}
	s := NewShaper(42, cfg)
	if s == nil {
		t.Fatal("NewShaper() returned nil")
	}
	if s.config.W1 <= 0 {
		t.Error("W1 should have default value")
	}
	if s.config.Lambda1 <= 0 {
		t.Error("Lambda1 should have default value")
	}
}

func TestShaperNoLock(t *testing.T) {
	cfg := ShaperConfig{
		W1:       0.7,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: time.Millisecond,
		MaxDelay: time.Second,
	}
	s := NewShaperNoLock(42, cfg)

	for i := 0; i < 100; i++ {
		d := s.NextDelay()
		if d < cfg.MinDelay || d > cfg.MaxDelay {
			t.Errorf("delay %v out of bounds [%v, %v]", d, cfg.MinDelay, cfg.MaxDelay)
		}
	}
}

func TestShaperReset(t *testing.T) {
	cfg := ShaperConfig{
		W1:       0.7,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: time.Millisecond,
		MaxDelay: time.Second,
	}
	s1 := NewShaper(42, cfg)
	s2 := NewShaper(42, cfg)

	d1 := s1.NextDelay()
	d2 := s2.NextDelay()
	if d1 != d2 {
		t.Errorf("same seed: %v vs %v", d1, d2)
	}

	s1.Reset(100)
	s2.Reset(100)

	d1 = s1.NextDelay()
	d2 = s2.NextDelay()
	if d1 != d2 {
		t.Errorf("same seed after reset: %v vs %v", d1, d2)
	}
}

func TestShaperSetConfig(t *testing.T) {
	s := NewShaper(42, ShaperConfig{
		W1:       0.5,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: time.Millisecond,
		MaxDelay: time.Second,
	})

	newCfg := ShaperConfig{
		W1:       0.9,
		Lambda1:  100.0,
		Mu:       -3.0,
		Sigma:    1.0,
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 500 * time.Millisecond,
	}
	s.SetConfig(newCfg)

	cfg := s.Config()
	if cfg.W1 != 0.9 {
		t.Errorf("W1 = %v, want 0.9", cfg.W1)
	}
}

func TestValidateShaperConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  ShaperConfig
		err  bool
	}{
		{"valid", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 1, MinDelay: time.Millisecond, MaxDelay: time.Second}, false},
		{"negative W1", ShaperConfig{W1: -0.1, Lambda1: 10, Sigma: 1}, true},
		{"W1 > 1", ShaperConfig{W1: 1.5, Lambda1: 10, Sigma: 1}, true},
		{"zero lambda", ShaperConfig{W1: 0.5, Lambda1: 0, Sigma: 1}, true},
		{"zero sigma", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 0}, true},
		{"neg min delay", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 1, MinDelay: -1}, true},
		{"neg max delay", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 1, MaxDelay: -1}, true},
		{"min > max", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 1, MinDelay: time.Second, MaxDelay: time.Millisecond}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateShaperConfig(tt.cfg)
			if (err != nil) != tt.err {
				t.Errorf("ValidateShaperConfig() error = %v, wantErr = %v", err, tt.err)
			}
		})
	}
}

func TestMorpherError(t *testing.T) {
	e := newError("test error")
	if e.Error() != "test error" {
		t.Errorf("got %q, want %q", e.Error(), "test error")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		value, min, max, want float64
	}{
		{5, 0, 10, 5},
		{-5, 0, 10, 0},
		{15, 0, 10, 10},
		{0, 0, 10, 0},
	}
	for _, tt := range tests {
		got := clamp(tt.value, tt.min, tt.max)
		if got != tt.want {
			t.Errorf("clamp(%v, %v, %v) = %v, want %v", tt.value, tt.min, tt.max, got, tt.want)
		}
	}
}