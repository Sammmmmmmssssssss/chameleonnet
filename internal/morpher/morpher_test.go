package morpher

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestNewMorphPipeline(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	if p == nil {
		t.Fatal("NewMorphPipeline() returned nil")
	}
}

func TestNewMorphPipelineNilProfile(t *testing.T) {
	p := NewMorphPipeline(42, nil)
	if p == nil {
		t.Fatal("NewMorphPipeline(nil) returned nil")
	}
	stats := p.Stats()
	if stats.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}
}

func TestMorphPipelineStartStop(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	p.Stop()
	cancel()
}

func TestMorphPipelineWriteMorphed(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	input := "Hello, ChameleonNet! This is a test message."
	src := strings.NewReader(input)
	var dst bytes.Buffer

	err := p.WriteMorphed(src, &dst)
	if err != nil {
		t.Fatal(err)
	}

	if dst.Len() == 0 {
		t.Error("output buffer is empty")
	}

	stats := p.Stats()
	if stats.PacketsSent <= 0 {
		t.Errorf("PacketsSent = %d, want > 0", stats.PacketsSent)
	}
	if stats.BytesProcessed <= 0 {
		t.Errorf("BytesProcessed = %d, want > 0", stats.BytesProcessed)
	}
}

func TestMorphPipelineReadDemorphed(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	input := []byte("test data for round trip through pipeline")
	src := bytes.NewReader(input)
	var morphed bytes.Buffer
	if err := p.WriteMorphed(src, &morphed); err != nil {
		t.Fatal(err)
	}

	var demorphed bytes.Buffer
	if err := p.ReadDemorphed(&morphed, &demorphed); err != nil {
		t.Fatal(err)
	}

	_ = demorphed
}

func TestMorphPipelineStats(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)

	stats := p.Stats()
	if stats.StartedAt.IsZero() {
		t.Error("StartedAt should not be zero")
	}

	p.ResetStats()
	stats = p.Stats()
	if !stats.StartedAt.IsZero() && stats.PacketsSent != 0 {
		t.Error("after ResetStats, packets should be zero")
	}
}

func TestValidateMorphProfile(t *testing.T) {
	if err := ValidateMorphProfile(nil); err == nil {
		t.Error("expected error for nil profile")
	}
	if err := ValidateMorphProfile(SpotityProfile); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDetectAndRemoveChaff(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)

	data, isChaff := p.detectAndRemoveChaff([]byte{0})
	if !isChaff {
		t.Error("single zero byte should be chaff")
	}
	if data != nil {
		t.Error("data should be nil for chaff")
	}

	data, isChaff = p.detectAndRemoveChaff(nil)
	if isChaff {
		t.Error("nil should not be chaff")
	}
	_ = data

	allZeros := make([]byte, 100)
	data, isChaff = p.detectAndRemoveChaff(allZeros)
	if !isChaff {
		t.Error("100 zero bytes should be chaff")
	}
	if data != nil {
		t.Error("data should be nil for chaff")
	}

	data, isChaff = p.detectAndRemoveChaff([]byte("real data"))
	if isChaff {
		t.Error("non-zero data should not be chaff")
	}
}
func TestNewDemorphPipeline(t *testing.T) {
	d := NewDemorphPipeline(42, SpotityProfile)
	if d == nil {
		t.Fatal("NewDemorphPipeline() returned nil")
	}
}

func TestDemorphPipelineNilProfile(t *testing.T) {
	d := NewDemorphPipeline(42, nil)
	if d == nil {
		t.Fatal("NewDemorphPipeline(nil) returned nil")
	}
}

func TestWriteOnePacket(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	var buf bytes.Buffer
	err := p.writeOnePacket(&buf, []byte("test packet"))
	if err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Error("buffer should contain output")
	}
}

func TestMorphPipelineError(t *testing.T) {
	if ErrPipelineClosed.Error() == "" {
		t.Error("ErrPipelineClosed has empty message")
	}
	if ErrWriteFailed.Error() == "" {
		t.Error("ErrWriteFailed has empty message")
	}
	if ErrReadFailed.Error() == "" {
		t.Error("ErrReadFailed has empty message")
	}
}

func TestMorphPipelineSetShaperConfig(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	newCfg := ShaperConfig{
		W1:       0.9,
		Lambda1:  100,
		Mu:       -3,
		Sigma:    1,
		MinDelay: 0,
		MaxDelay: 500,
	}
	p.SetShaperConfig(newCfg)
}

func TestSmallPacketPadded(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	input := []byte{0x01}
	src := bytes.NewReader(input)
	var dst bytes.Buffer
	if err := p.WriteMorphed(src, &dst); err != nil {
		t.Fatal(err)
	}
	if dst.Len() < 128 {
		t.Errorf("expected output >= 128 bytes for single byte input, got %d", dst.Len())
	}
}

func TestMorphPipelineSendPacket(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	var buf bytes.Buffer
	err := p.sendPacket(&buf, &Packet{
		Type:    PacketReal,
		Payload: []byte("test"),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestMorphPipelineChaffStats(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	cs := p.ChaffStats()
	_ = cs
}

func TestMorphPipelineSetters(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)

	if p.Shaper() == nil {
		t.Error("Shaper() returned nil")
	}
	if p.Padder() == nil {
		t.Error("Padder() returned nil")
	}
}

func TestValidateMorphProfileErrors(t *testing.T) {
	tests := []struct {
		name    string
		profile *Profile
	}{
		{"nil", nil},
		{"zero lambda", &Profile{Name: "test", ChaffLambda: 0, ChaffRatio: 0.3, PadderBuckets: []int{128}}},
		{"negative lambda", &Profile{Name: "test", ChaffLambda: -1, ChaffRatio: 0.3, PadderBuckets: []int{128}}},
		{"bad ratio", &Profile{Name: "test", ChaffLambda: 10, ChaffRatio: 1.5, PadderBuckets: []int{128}}},
		{"no buckets", &Profile{Name: "test", ChaffLambda: 10, ChaffRatio: 0.3, PadderBuckets: []int{}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateMorphProfile(tt.profile); err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestWriteMorphedLargeData(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	data := make([]byte, 100000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	src := bytes.NewReader(data)
	var dst bytes.Buffer
	err := p.WriteMorphed(src, &dst)
	if err != nil {
		t.Fatal(err)
	}
	if dst.Len() < len(data) {
		t.Errorf("output %d < input %d", dst.Len(), len(data))
	}
}

func TestReadDemorphedEmpty(t *testing.T) {
	d := NewDemorphPipeline(42, SpotityProfile)

	var src bytes.Buffer
	var dst bytes.Buffer
	err := d.ProcessRead(&src, &dst)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDemorphStats(t *testing.T) {
	d := NewDemorphPipeline(42, SpotityProfile)
	stats := d.DemorphStats()
	if stats.BytesProcessed != 0 {
		t.Error("initial BytesProcessed should be 0")
	}
}

func TestEOFPropagation(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)
	defer p.Stop()
	defer cancel()

	var src bytes.Buffer
	var dst bytes.Buffer
	err := p.WriteMorphed(&src, &dst)
	if err != nil {
		t.Fatal(err)
	}
}

func TestContextCancellation(t *testing.T) {
	p := NewMorphPipeline(42, SpotityProfile)
	ctx, cancel := context.WithCancel(context.Background())
	p.Start(ctx)

	cancel()

	var src bytes.Buffer
	var dst bytes.Buffer
	err := p.WriteMorphed(&src, &dst)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}