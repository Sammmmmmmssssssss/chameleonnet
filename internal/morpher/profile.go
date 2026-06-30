package morpher

import "time"

type ProfileName string

const (
	ProfileSpotify ProfileName = "spotify"
	ProfileYouTube ProfileName = "youtube"
	ProfileGeneric ProfileName = "generic"
)

type Profile struct {
	Name          ProfileName
	ChaffLambda   float64
	ChaffRatio    float64
	MaxDelay      time.Duration
	PadderBuckets []int
	ShaperClient  ShaperConfig
	ShaperServer  ShaperConfig
}

var SpotityProfile = &Profile{
	Name:        ProfileSpotify,
	ChaffLambda: 50.0,
	ChaffRatio:  0.4,
	MaxDelay:    200 * time.Millisecond,
	PadderBuckets: []int{128, 256, 512, 768, 1024, 1280, 1500},
	ShaperClient: ShaperConfig{
		W1:       0.7,
		Lambda1:  50.0,
		Mu:       -4.0,
		Sigma:    1.5,
		MinDelay: 5 * time.Millisecond,
		MaxDelay: 200 * time.Millisecond,
	},
	ShaperServer: ShaperConfig{
		W1:       0.6,
		Lambda1:  10.0,
		Mu:       -2.0,
		Sigma:    1.0,
		MinDelay: 10 * time.Millisecond,
		MaxDelay: 1000 * time.Millisecond,
	},
}

var YouTubeProfile = &Profile{
	Name:        ProfileYouTube,
	ChaffLambda: 30.0,
	ChaffRatio:  0.3,
	MaxDelay:    500 * time.Millisecond,
	PadderBuckets: []int{128, 256, 512, 768, 1024, 1280, 1500},
	ShaperClient: ShaperConfig{
		W1:       0.5,
		Lambda1:  30.0,
		Mu:       -2.0,
		Sigma:    2.0,
		MinDelay: 2 * time.Millisecond,
		MaxDelay: 500 * time.Millisecond,
	},
	ShaperServer: ShaperConfig{
		W1:       0.4,
		Lambda1:  5.0,
		Mu:       -1.0,
		Sigma:    1.5,
		MinDelay: 5 * time.Millisecond,
		MaxDelay: 2000 * time.Millisecond,
	},
}

var GenericProfile = &Profile{
	Name:        ProfileGeneric,
	ChaffLambda: 20.0,
	ChaffRatio:  0.2,
	MaxDelay:    1000 * time.Millisecond,
	PadderBuckets: []int{256, 512, 1024, 1500},
	ShaperClient: ShaperConfig{
		W1:       0.8,
		Lambda1:  40.0,
		Mu:       -3.0,
		Sigma:    1.0,
		MinDelay: 1 * time.Millisecond,
		MaxDelay: 1000 * time.Millisecond,
	},
	ShaperServer: ShaperConfig{
		W1:       0.7,
		Lambda1:  15.0,
		Mu:       -2.5,
		Sigma:    1.2,
		MinDelay: 5 * time.Millisecond,
		MaxDelay: 1500 * time.Millisecond,
	},
}

func LookupProfile(name ProfileName) *Profile {
	switch name {
	case ProfileSpotify:
		return SpotityProfile
	case ProfileYouTube:
		return YouTubeProfile
	case ProfileGeneric:
		return GenericProfile
	default:
		return GenericProfile
	}
}

func ProfileNames() []ProfileName {
	return []ProfileName{ProfileSpotify, ProfileYouTube, ProfileGeneric}
}

func ValidateProfile(p *Profile) error {
	if p == nil {
		return errNilProfile
	}
	if p.ChaffLambda <= 0 {
		return errInvalidChaffLambda
	}
	if p.ChaffRatio < 0 || p.ChaffRatio > 1 {
		return errInvalidChaffRatio
	}
	if err := ValidateShaperConfig(p.ShaperClient); err != nil {
		return err
	}
	if err := ValidateShaperConfig(p.ShaperServer); err != nil {
		return err
	}
	if len(p.PadderBuckets) == 0 {
		return errNoPadderBuckets
	}
	return nil
}

var (
	errNilProfile          = newError("profile is nil")
	errInvalidChaffLambda  = newError("chaff lambda must be positive")
	errInvalidChaffRatio   = newError("chaff ratio must be in [0, 1]")
	errNoPadderBuckets     = newError("at least one padder bucket required")
)

func (p *Profile) ShaperClientConfig() ShaperConfig {
	return p.ShaperClient
}

func (p *Profile) ShaperServerConfig() ShaperConfig {
	return p.ShaperServer
}

func (p *Profile) EffectiveChaffLambda() float64 {
	return p.ChaffLambda
}

func (p *Profile) EffectiveChaffRatio() float64 {
	return p.ChaffRatio
}

func (p *Profile) EffectivePadderBuckets() []int {
	return p.PadderBuckets
}