package morpher

import "sort"

type Padder struct {
	buckets []int
}

func NewPadder(buckets []int) *Padder {
	if len(buckets) == 0 {
		buckets = DefaultBuckets()
	}
	sorted := make([]int, len(buckets))
	copy(sorted, buckets)
	sort.Ints(sorted)
	return &Padder{buckets: sorted}
}

func DefaultBuckets() []int {
	return []int{128, 256, 512, 768, 1024, 1280, 1500}
}

func (p *Padder) PaddedLen(plainLen int) int {
	if plainLen <= 0 {
		return p.buckets[0]
	}
	for _, b := range p.buckets {
		if plainLen <= b {
			return b
		}
	}
	return p.buckets[len(p.buckets)-1]
}

func (p *Padder) Pad(buf []byte) []byte {
	needed := p.PaddedLen(len(buf))
	if needed <= len(buf) {
		return buf
	}
	if cap(buf) >= needed {
		orig := buf
		buf = buf[:needed]
		for i := len(orig); i < needed; i++ {
			buf[i] = 0
		}
		return buf
	}
	padded := make([]byte, needed)
	copy(padded, buf)
	return padded
}

func (p *Padder) PadCopy(dst, src []byte) int {
	if len(dst) == 0 {
		return 0
	}
	needed := p.PaddedLen(len(src))
	if needed > len(dst) {
		needed = len(dst)
	}
	n := copy(dst, src)
	if n < needed {
		for i := n; i < needed; i++ {
			dst[i] = 0
		}
	}
	return needed
}

func (p *Padder) RemovePadding(buf []byte) ([]byte, int) {
	if len(buf) == 0 {
		return buf, 0
	}
	n := len(buf)
	for n > 0 && buf[n-1] == 0 {
		n--
	}
	return buf[:n], len(buf) - n
}

func (p *Padder) IsPaddedLength(l int) bool {
	for _, b := range p.buckets {
		if l == b {
			return true
		}
	}
	return false
}

func (p *Padder) Buckets() []int {
	result := make([]int, len(p.buckets))
	copy(result, p.buckets)
	return result
}

func (p *Padder) Overhead(plainLen int) int {
	return p.PaddedLen(plainLen) - plainLen
}

func (p *Padder) OverheadRatio(plainLen int) float64 {
	if plainLen <= 0 {
		return 0
	}
	return float64(p.Overhead(plainLen)) / float64(plainLen)
}

var RecommendedBuckets = struct {
	Small  []int
	Medium []int
	Large  []int
}{
	Small:  []int{128, 256, 512},
	Medium: []int{128, 256, 512, 768, 1024, 1280, 1500},
	Large:  []int{256, 512, 1024, 1500, 2048, 4096},
}