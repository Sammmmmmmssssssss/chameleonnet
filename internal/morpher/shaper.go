package morpher

import (
	"math"
	"math/rand"
	"sync"
	"time"
)

type ShaperConfig struct {
	W1       float64
	Lambda1  float64
	Mu       float64
	Sigma    float64
	MinDelay time.Duration
	MaxDelay time.Duration
}

type Shaper struct {
	config ShaperConfig
	rng    *rand.Rand
	mu     sync.Mutex
}

func NewShaper(seed int64, config ShaperConfig) *Shaper {
	if config.W1 <= 0 {
		config.W1 = 0.5
	}
	if config.W1 > 1 {
		config.W1 = 1.0
	}
	if config.Lambda1 <= 0 {
		config.Lambda1 = 1.0
	}
	if config.Sigma <= 0 {
		config.Sigma = 0.1
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 1000
	}
	return &Shaper{
		config: config,
		rng:    rand.New(rand.NewSource(seed)),
	}
}

func (s *Shaper) NextDelay() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.nextDelayLocked()
}

func (s *Shaper) nextDelayLocked() time.Duration {
	u := s.rng.Float64()
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}

	var delay float64
	if u < s.config.W1 {
		u2 := s.rng.Float64()
		if u2 <= 0 {
			u2 = 1e-15
		} else if u2 >= 1 {
			u2 = 1 - 1e-15
		}
		delay = -math.Log(u2) / s.config.Lambda1
	} else {
		u1 := s.rng.Float64()
		if u1 <= 0 {
			u1 = 1e-15
		} else if u1 >= 1 {
			u1 = 1 - 1e-15
		}
		u2 := s.rng.Float64()
		if u2 <= 0 {
			u2 = 1e-15
		} else if u2 >= 1 {
			u2 = 1 - 1e-15
		}
		z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
		delay = math.Exp(s.config.Mu + z*s.config.Sigma)
	}

	delaySeconds := clamp(delay, s.config.MinDelay.Seconds(), s.config.MaxDelay.Seconds())
	return time.Duration(delaySeconds * float64(time.Second))
}

func (s *Shaper) Delay() time.Duration {
	d := s.NextDelay()
	if d > 0 {
		time.Sleep(d)
	}
	return d
}

func (s *Shaper) DelayWithJitter(baseDelay time.Duration) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	jitter := s.rng.Float64()*0.2 - 0.1
	jitterDuration := time.Duration(float64(baseDelay) * jitter)

	total := baseDelay + jitterDuration
	if total < 0 {
		total = 0
	}
	if total > s.config.MaxDelay {
		total = s.config.MaxDelay
	}

	time.Sleep(total)
	return total
}

func (s *Shaper) SetConfig(config ShaperConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

func (s *Shaper) Config() ShaperConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.config
}

func (s *Shaper) Reset(seed int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rng = rand.New(rand.NewSource(seed))
}

type ShaperNoLock struct {
	config ShaperConfig
	rng    *rand.Rand
}

func NewShaperNoLock(seed int64, config ShaperConfig) *ShaperNoLock {
	if config.W1 <= 0 {
		config.W1 = 0.5
	}
	if config.W1 > 1 {
		config.W1 = 1.0
	}
	if config.Lambda1 <= 0 {
		config.Lambda1 = 1.0
	}
	if config.Sigma <= 0 {
		config.Sigma = 0.1
	}
	if config.MaxDelay <= 0 {
		config.MaxDelay = 1000
	}
	return &ShaperNoLock{
		config: config,
		rng:    rand.New(rand.NewSource(seed)),
	}
}

func (s *ShaperNoLock) NextDelay() time.Duration {
	u := s.rng.Float64()
	delay := s.nextDelayFromUniform(u)
	delaySeconds := clamp(delay, s.config.MinDelay.Seconds(), s.config.MaxDelay.Seconds())
	return time.Duration(delaySeconds * float64(time.Second))
}

func (s *ShaperNoLock) nextDelayFromUniform(u float64) float64 {
	if u <= 0 {
		u = 1e-15
	} else if u >= 1 {
		u = 1 - 1e-15
	}

	if u < s.config.W1 {
		u2 := s.rng.Float64()
		if u2 <= 0 {
			u2 = 1e-15
		} else if u2 >= 1 {
			u2 = 1 - 1e-15
		}
		return -math.Log(u2) / s.config.Lambda1
	}

	u1 := s.rng.Float64()
	if u1 <= 0 {
		u1 = 1e-15
	}
	u2 := s.rng.Float64()
	if u2 <= 0 {
		u2 = 1e-15
	}
	z := math.Sqrt(-2*math.Log(u1)) * math.Cos(2*math.Pi*u2)
	return math.Exp(s.config.Mu + z*s.config.Sigma)
}

func (s *ShaperNoLock) Delay() time.Duration {
	d := s.NextDelay()
	if d > 0 {
		time.Sleep(d)
	}
	return d
}

func (s *ShaperNoLock) SetConfig(config ShaperConfig) {
	s.config = config
}

func (s *ShaperNoLock) Reset(seed int64) {
	s.rng = rand.New(rand.NewSource(seed))
}

func clamp(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func ValidateShaperConfig(cfg ShaperConfig) error {
	if cfg.W1 < 0 || cfg.W1 > 1 {
		return ErrInvalidShaperW1
	}
	if cfg.Lambda1 <= 0 {
		return ErrInvalidShaperLambda
	}
	if cfg.Sigma <= 0 {
		return ErrInvalidShaperSigma
	}
	if cfg.MinDelay < 0 {
		return ErrInvalidShaperMinDelay
	}
	if cfg.MaxDelay < 0 {
		return ErrInvalidShaperMaxDelay
	}
	if cfg.MinDelay > cfg.MaxDelay {
		return ErrInvalidShaperMinMax
	}
	return nil
}

var (
	ErrInvalidShaperW1       = newError("shaper W1 must be in [0, 1]")
	ErrInvalidShaperLambda   = newError("shaper Lambda1 must be positive")
	ErrInvalidShaperSigma    = newError("shaper Sigma must be positive")
	ErrInvalidShaperMinDelay = newError("shaper MinDelay cannot be negative")
	ErrInvalidShaperMaxDelay = newError("shaper MaxDelay cannot be negative")
	ErrInvalidShaperMinMax   = newError("shaper MinDelay cannot exceed MaxDelay")
)

func newError(msg string) error {
	return &morpherError{msg: msg}
}

type morpherError struct {
	msg string
}

func (e *morpherError) Error() string {
	return e.msg
}