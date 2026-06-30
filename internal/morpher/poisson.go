package morpher

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

type PoissonProcess struct {
	lambda float64
	rng    *rand.Rand
	mu     sync.Mutex
}

func NewPoissonProcess(seed int64, lambda float64) *PoissonProcess {
	if lambda <= 0 {
		lambda = 1.0
	}
	return &PoissonProcess{
		lambda: lambda,
		rng:    rand.New(rand.NewSource(seed)),
	}
}

func (p *PoissonProcess) NextInterval() time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()

	u := p.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}
	interval := -math.Log(u) / p.lambda
	return time.Duration(interval * float64(time.Second))
}

func (p *PoissonProcess) NextIntervalFloat64() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	u := p.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}
	return -math.Log(u) / p.lambda
}

func (p *PoissonProcess) SampleCount(duration time.Duration) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	expected := p.lambda * duration.Seconds()
	if expected <= 0 {
		return 0
	}

	u := p.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}

	k := int(math.Floor(-math.Log(u)))
	return k
}

func (p *PoissonProcess) SetLambda(lambda float64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if lambda > 0 {
		p.lambda = lambda
	}
}

func (p *PoissonProcess) Lambda() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lambda
}

func (p *PoissonProcess) Reset(seed int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rng = rand.New(rand.NewSource(seed))
}

type PoissonProcessNoLock struct {
	lambda float64
	rng    *rand.Rand
}

func NewPoissonProcessNoLock(seed int64, lambda float64) *PoissonProcessNoLock {
	if lambda <= 0 {
		lambda = 1.0
	}
	return &PoissonProcessNoLock{
		lambda: lambda,
		rng:    rand.New(rand.NewSource(seed)),
	}
}

func (p *PoissonProcessNoLock) NextInterval() time.Duration {
	u := p.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}
	interval := -math.Log(u) / p.lambda
	return time.Duration(interval * float64(time.Second))
}

func (p *PoissonProcessNoLock) NextIntervalFloat64() float64 {
	u := p.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}
	return -math.Log(u) / p.lambda
}

func (p *PoissonProcessNoLock) SetLambda(lambda float64) {
	if lambda > 0 {
		p.lambda = lambda
	}
}

func (p *PoissonProcessNoLock) Lambda() float64 {
	return p.lambda
}

func (p *PoissonProcessNoLock) Reset(seed int64) {
	p.rng = rand.New(rand.NewSource(seed))
}