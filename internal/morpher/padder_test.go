package morpher

import (
	"bytes"
	"testing"
)

func TestNewPadder(t *testing.T) {
	p := NewPadder([]int{128, 256, 512})
	if p == nil {
		t.Fatal("NewPadder() returned nil")
	}
}

func TestDefaultBuckets(t *testing.T) {
	p := NewPadder(nil)
	if p == nil {
		t.Fatal("NewPadder(nil) returned nil")
	}
	if len(p.Buckets()) == 0 {
		t.Error("default buckets should not be empty")
	}
}

func TestPaddedLen(t *testing.T) {
	p := NewPadder([]int{128, 256, 512, 1024})

	tests := []struct {
		input int
		want  int
	}{
		{0, 128},
		{1, 128},
		{100, 128},
		{128, 128},
		{129, 256},
		{256, 256},
		{257, 512},
		{1000, 1024},
		{1024, 1024},
		{2000, 1024},
	}
	for _, tt := range tests {
		got := p.PaddedLen(tt.input)
		if got != tt.want {
			t.Errorf("PaddedLen(%d) = %d, want %d", tt.input, got, tt.want)
		}
	}
}

func TestPad(t *testing.T) {
	p := NewPadder([]int{128, 256})

	src := make([]byte, 50)
	for i := range src {
		src[i] = byte(i)
	}

	padded := p.Pad(src)
	if len(padded) != 128 {
		t.Errorf("len = %d, want 128", len(padded))
	}

	for i := 0; i < 50; i++ {
		if padded[i] != byte(i) {
			t.Errorf("padded[%d] = %d, want %d", i, padded[i], byte(i))
			break
		}
	}

	for i := 50; i < 128; i++ {
		if padded[i] != 0 {
			t.Errorf("padded[%d] = %d, want 0 (padding)", i, padded[i])
		}
	}
}

func TestPadNoAllocWhenSufficient(t *testing.T) {
	p := NewPadder([]int{128, 256})

	buf := make([]byte, 128, 256)
	for i := range buf {
		buf[i] = byte(i)
	}

	padded := p.Pad(buf)
	if len(padded) != 128 {
		t.Errorf("len = %d, want 128", len(padded))
	}
	if &padded[0] != &buf[0] {
		t.Error("Pad should return same backing array when capacity sufficient")
	}
}

func TestPadAllocWhenInsufficient(t *testing.T) {
	p := NewPadder([]int{128, 256})

	buf := make([]byte, 50)
	copy(buf, []byte("hello"))

	padded := p.Pad(buf)
	if len(padded) != 128 {
		t.Errorf("len = %d, want 128", len(padded))
	}
}

func TestPadCopy(t *testing.T) {
	p := NewPadder([]int{128, 256})

	src := []byte("hello")
	dst := make([]byte, 256)

	n := p.PadCopy(dst, src)
	if n != 128 {
		t.Errorf("n = %d, want 128", n)
	}
	if string(dst[:5]) != "hello" {
		t.Errorf("dst[:5] = %q, want %q", dst[:5], "hello")
	}
}

func TestRemovePadding(t *testing.T) {
	p := NewPadder([]int{128, 256})

	padded := make([]byte, 128)
	copy(padded, []byte("hello world"))

	unpadded, removed := p.RemovePadding(padded)
	if string(unpadded) != "hello world" {
		t.Errorf("unpadded = %q, want %q", unpadded, "hello world")
	}
	if removed != 128-11 {
		t.Errorf("removed = %d, want %d", removed, 128-11)
	}
}

func TestRemovePaddingAllZeros(t *testing.T) {
	p := NewPadder([]int{128})

	padded := make([]byte, 128)
	unpadded, removed := p.RemovePadding(padded)
	if len(unpadded) != 0 {
		t.Errorf("unpadded len = %d, want 0", len(unpadded))
	}
	if removed != 128 {
		t.Errorf("removed = %d, want %d", removed, 128)
	}
}

func TestIsPaddedLength(t *testing.T) {
	p := NewPadder([]int{128, 256, 512})

	if !p.IsPaddedLength(128) {
		t.Error("128 should be a valid padded length")
	}
	if !p.IsPaddedLength(256) {
		t.Error("256 should be a valid padded length")
	}
	if p.IsPaddedLength(100) {
		t.Error("100 should not be a valid padded length")
	}
}

func TestBucketsSorted(t *testing.T) {
	p := NewPadder([]int{512, 128, 256})
	buckets := p.Buckets()
	for i := 1; i < len(buckets); i++ {
		if buckets[i] <= buckets[i-1] {
			t.Errorf("buckets not sorted: %v", buckets)
		}
	}
}

func TestOverhead(t *testing.T) {
	p := NewPadder([]int{128, 256})

	overhead := p.Overhead(50)
	if overhead != 78 {
		t.Errorf("Overhead(50) = %d, want 78", overhead)
	}

	overhead = p.Overhead(128)
	if overhead != 0 {
		t.Errorf("Overhead(128) = %d, want 0", overhead)
	}
}

func TestOverheadRatio(t *testing.T) {
	p := NewPadder([]int{128, 256})

	ratio := p.OverheadRatio(64)
	if ratio <= 0 {
		t.Errorf("OverheadRatio(64) = %v, want > 0", ratio)
	}

	ratio = p.OverheadRatio(128)
	if ratio != 0 {
		t.Errorf("OverheadRatio(128) = %v, want 0", ratio)
	}

	ratio = p.OverheadRatio(0)
	if ratio != 0 {
		t.Errorf("OverheadRatio(0) = %v, want 0", ratio)
	}
}

func TestPadNonDestructive(t *testing.T) {
	p := NewPadder([]int{128})

	original := []byte("short data")
	dup := make([]byte, len(original))
	copy(dup, original)

	padded := p.Pad(original)
	if !bytes.Equal(original, dup) {
		t.Error("original data was modified")
	}
	_ = padded
}

func TestRecommendedBuckets(t *testing.T) {
	if len(RecommendedBuckets.Small) == 0 {
		t.Error("RecommendedBuckets.Small empty")
	}
	if len(RecommendedBuckets.Medium) == 0 {
		t.Error("RecommendedBuckets.Medium empty")
	}
	if len(RecommendedBuckets.Large) == 0 {
		t.Error("RecommendedBuckets.Large empty")
	}
}