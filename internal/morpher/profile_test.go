package morpher

import (
	"testing"
	"time"
)

func TestLookupProfile(t *testing.T) {
	p := LookupProfile(ProfileSpotify)
	if p == nil {
		t.Fatal("LookupProfile(spotify) returned nil")
	}
	if p.Name != ProfileSpotify {
		t.Errorf("Name = %v, want spotify", p.Name)
	}

	p = LookupProfile(ProfileYouTube)
	if p == nil {
		t.Fatal("LookupProfile(youtube) returned nil")
	}
	if p.Name != ProfileYouTube {
		t.Errorf("Name = %v, want youtube", p.Name)
	}

	p = LookupProfile(ProfileGeneric)
	if p == nil {
		t.Fatal("LookupProfile(generic) returned nil")
	}
	if p.Name != ProfileGeneric {
		t.Errorf("Name = %v, want generic", p.Name)
	}
}

func TestLookupProfileDefault(t *testing.T) {
	p := LookupProfile("invalid")
	if p == nil {
		t.Fatal("LookupProfile(invalid) returned nil")
	}
	if p.Name != ProfileGeneric {
		t.Errorf("Name = %v, want generic (default)", p.Name)
	}
}

func TestProfileNames(t *testing.T) {
	names := ProfileNames()
	if len(names) != 3 {
		t.Errorf("got %d profiles, want 3", len(names))
	}
}

func TestSpotifyProfileValues(t *testing.T) {
	p := SpotityProfile

	if p.ChaffLambda != 50.0 {
		t.Errorf("ChaffLambda = %v, want 50.0", p.ChaffLambda)
	}
	if p.ChaffRatio != 0.4 {
		t.Errorf("ChaffRatio = %v, want 0.4", p.ChaffRatio)
	}
	if p.MaxDelay != 200*time.Millisecond {
		t.Errorf("MaxDelay = %v, want 200ms", p.MaxDelay)
	}

	if p.ShaperClient.W1 != 0.7 {
		t.Errorf("ShaperClient.W1 = %v, want 0.7", p.ShaperClient.W1)
	}
	if p.ShaperClient.Lambda1 != 50.0 {
		t.Errorf("ShaperClient.Lambda1 = %v, want 50.0", p.ShaperClient.Lambda1)
	}

	if p.ShaperServer.W1 != 0.6 {
		t.Errorf("ShaperServer.W1 = %v, want 0.6", p.ShaperServer.W1)
	}
}

func TestYouTubeProfileValues(t *testing.T) {
	p := YouTubeProfile

	if p.ChaffLambda != 30.0 {
		t.Errorf("ChaffLambda = %v, want 30.0", p.ChaffLambda)
	}
	if p.ShaperClient.W1 != 0.5 {
		t.Errorf("ShaperClient.W1 = %v, want 0.5", p.ShaperClient.W1)
	}
}

func TestGenericProfileValues(t *testing.T) {
	p := GenericProfile

	if p.ChaffLambda != 20.0 {
		t.Errorf("ChaffLambda = %v, want 20.0", p.ChaffLambda)
	}
}

func TestValidateProfile(t *testing.T) {
	if err := ValidateProfile(nil); err == nil {
		t.Error("expected error for nil profile")
	}

	if err := ValidateProfile(SpotityProfile); err != nil {
		t.Errorf("unexpected error for valid profile: %v", err)
	}

	invalid := &Profile{Name: "test"}
	if err := ValidateProfile(invalid); err == nil {
		t.Error("expected error for invalid profile")
	}
}

func TestProfileGetters(t *testing.T) {
	p := SpotityProfile

	if cfg := p.ShaperClientConfig(); cfg.W1 != 0.7 {
		t.Errorf("ShaperClientConfig().W1 = %v, want 0.7", cfg.W1)
	}

	if cfg := p.ShaperServerConfig(); cfg.W1 != 0.6 {
		t.Errorf("ShaperServerConfig().W1 = %v, want 0.6", cfg.W1)
	}

	if l := p.EffectiveChaffLambda(); l != 50.0 {
		t.Errorf("EffectiveChaffLambda() = %v, want 50.0", l)
	}

	if r := p.EffectiveChaffRatio(); r != 0.4 {
		t.Errorf("EffectiveChaffRatio() = %v, want 0.4", r)
	}

	buckets := p.EffectivePadderBuckets()
	if len(buckets) == 0 {
		t.Error("EffectivePadderBuckets() returned empty")
	}
}

func TestProfilesAreDistinct(t *testing.T) {
	if SpotityProfile == YouTubeProfile {
		t.Error("spotify and youtube profiles are the same instance")
	}
	if SpotityProfile == GenericProfile {
		t.Error("spotify and generic profiles are the same instance")
	}
	if YouTubeProfile == GenericProfile {
		t.Error("youtube and generic profiles are the same instance")
	}
}

func TestProfileChaffLambdas(t *testing.T) {
	if SpotityProfile.ChaffLambda <= YouTubeProfile.ChaffLambda {
		t.Error("expected spotify lambda > youtube lambda")
	}
	if YouTubeProfile.ChaffLambda <= GenericProfile.ChaffLambda {
		t.Error("expected youtube lambda > generic lambda")
	}
}

func TestProfilePadderBuckets(t *testing.T) {
	if len(SpotityProfile.PadderBuckets) < len(GenericProfile.PadderBuckets) {
		t.Error("expected spotify buckets >= generic buckets")
	}
}