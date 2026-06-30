package config

import (
	"os"
	"testing"
	"time"
)

func TestParseProxyMode(t *testing.T) {
	tests := []struct {
		input string
		want  ProxyMode
		err   bool
	}{
		{"client", ModeClient, false},
		{"server", ModeServer, false},
		{"Client", ModeClient, false},
		{"SERVER", ModeServer, false},
		{"", 0, true},
		{"proxy", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseProxyMode(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseProxyMode(%q) error = %v, wantErr = %v", tt.input, err, tt.err)
			continue
		}
		if !tt.err && got != tt.want {
			t.Errorf("ParseProxyMode(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestParseProfileName(t *testing.T) {
	tests := []struct {
		input string
		want  ProfileName
		err   bool
	}{
		{"spotify", ProfileSpotify, false},
		{"youtube", ProfileYouTube, false},
		{"generic", ProfileGeneric, false},
		{"Spotify", ProfileSpotify, false},
		{"YOUTUBE", ProfileYouTube, false},
		{"", "", true},
		{"netflix", "", true},
	}
	for _, tt := range tests {
		got, err := ParseProfileName(tt.input)
		if (err != nil) != tt.err {
			t.Errorf("ParseProfileName(%q) error = %v, wantErr = %v", tt.input, err, tt.err)
			continue
		}
		if !tt.err && got != tt.want {
			t.Errorf("ParseProfileName(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestDurationUnmarshal(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("100ms")); err != nil {
		t.Fatal(err)
	}
	if d.Duration() != 100*time.Millisecond {
		t.Errorf("got %v, want %v", d.Duration(), 100*time.Millisecond)
	}

	d = 0
	if err := d.UnmarshalText([]byte("")); err != nil {
		t.Fatal(err)
	}
	if d != 0 {
		t.Errorf("got %v, want 0", d)
	}

	d = 0
	if err := d.UnmarshalText([]byte("invalid")); err == nil {
		t.Error("expected error for invalid duration")
	}
}

func TestLoadDefaults(t *testing.T) {
	os.Clearenv()
	os.Setenv("CHAMELEON_PASSPHRASE", "test-password-16ch")
	os.Setenv("CHAMELEON_TARGET", "relay.example.com:9000")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != "127.0.0.1:1080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "127.0.0.1:1080")
	}
	if cfg.RemoteAddr != "relay.example.com:9000" {
		t.Errorf("RemoteAddr = %q, want %q", cfg.RemoteAddr, "relay.example.com:9000")
	}
	if cfg.Mode != ModeClient {
		t.Errorf("Mode = %v, want client", cfg.Mode)
	}
	if cfg.Profile != ProfileSpotify {
		t.Errorf("Profile = %v, want spotify", cfg.Profile)
	}
	if cfg.ChaffLambda <= 0 {
		t.Errorf("ChaffLambda = %v, want > 0", cfg.ChaffLambda)
	}
	if cfg.MaxConnections != 100 {
		t.Errorf("MaxConnections = %d, want 100", cfg.MaxConnections)
	}
	if cfg.BufferSize != 32768 {
		t.Errorf("BufferSize = %d, want 32768", cfg.BufferSize)
	}
	if cfg.KDFIterations != 100000 {
		t.Errorf("KDFIterations = %d, want 100000", cfg.KDFIterations)
	}
}

func TestLoadEnvOverride(t *testing.T) {
	os.Clearenv()
	os.Setenv("CHAMELEON_PASSPHRASE", "test-password-16ch")
	os.Setenv("CHAMELEON_TARGET", "relay.example.com:9000")
	os.Setenv("CHAMELEON_LISTEN", "0.0.0.0:2080")
	os.Setenv("CHAMELEON_MODE", "server")
	os.Setenv("CHAMELEON_PROFILE", "youtube")
	os.Setenv("CHAMELEON_POISSON_LAMBDA", "75")
	os.Setenv("CHAMELEON_MAX_CONNECTIONS", "250")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.ListenAddr != "0.0.0.0:2080" {
		t.Errorf("ListenAddr = %q, want %q", cfg.ListenAddr, "0.0.0.0:2080")
	}
	if cfg.Mode != ModeServer {
		t.Errorf("Mode = %v, want server", cfg.Mode)
	}
	if cfg.Profile != ProfileYouTube {
		t.Errorf("Profile = %v, want youtube", cfg.Profile)
	}
	if cfg.ChaffLambda != 75 {
		t.Errorf("ChaffLambda = %v, want 75", cfg.ChaffLambda)
	}
	if cfg.MaxConnections != 250 {
		t.Errorf("MaxConnections = %d, want 250", cfg.MaxConnections)
	}
}

func TestValidateErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"no passphrase", map[string]string{"CHAMELEON_TARGET": "relay:9000"}},
		{"short passphrase", map[string]string{"CHAMELEON_PASSPHRASE": "short", "CHAMELEON_TARGET": "relay:9000"}},
		{"bad listen addr", map[string]string{"CHAMELEON_PASSPHRASE": "test-password-16ch", "CHAMELEON_LISTEN": "invalid", "CHAMELEON_TARGET": "relay:9000"}},
		{"no target in client mode", map[string]string{"CHAMELEON_PASSPHRASE": "test-password-16ch", "CHAMELEON_TARGET": ""}},
		{"bad buffer size", map[string]string{"CHAMELEON_PASSPHRASE": "test-password-16ch", "CHAMELEON_TARGET": "relay:9000", "CHAMELEON_BUFFER_SIZE": "100"}},
		{"negative lambda", map[string]string{"CHAMELEON_PASSPHRASE": "test-password-16ch", "CHAMELEON_TARGET": "relay:9000", "CHAMELEON_POISSON_LAMBDA": "-1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tt.env {
				os.Setenv(k, v)
			}
			_, err := Load()
			if err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

func TestDefaultShaperClient(t *testing.T) {
	spotify := DefaultShaperClient(ProfileSpotify)
	if spotify.W1 != 0.7 {
		t.Errorf("Spotify W1 = %v, want 0.7", spotify.W1)
	}
	if spotify.Lambda1 != 50.0 {
		t.Errorf("Spotify Lambda1 = %v, want 50", spotify.Lambda1)
	}

	youtube := DefaultShaperClient(ProfileYouTube)
	if youtube.W1 != 0.5 {
		t.Errorf("YouTube W1 = %v, want 0.5", youtube.W1)
	}

	generic := DefaultShaperClient(ProfileGeneric)
	if generic.W1 != 0.8 {
		t.Errorf("Generic W1 = %v, want 0.8", generic.W1)
	}
}

func TestDefaultShaperServer(t *testing.T) {
	spotify := DefaultShaperServer(ProfileSpotify)
	if spotify.W1 != 0.6 {
		t.Errorf("Spotify server W1 = %v, want 0.6", spotify.W1)
	}
}

func TestProfileNames(t *testing.T) {
	for _, name := range []ProfileName{ProfileSpotify, ProfileYouTube, ProfileGeneric} {
		if name == "" {
			t.Error("empty profile name")
		}
	}
}

func TestModeString(t *testing.T) {
	if ModeClient.String() != "client" {
		t.Errorf("ModeClient.String() = %q, want %q", ModeClient.String(), "client")
	}
	if ModeServer.String() != "server" {
		t.Errorf("ModeServer.String() = %q, want %q", ModeServer.String(), "server")
	}
}

func TestValidateShaperConfigBounds(t *testing.T) {
	tests := []struct {
		name string
		cfg  ShaperConfig
		err  bool
	}{
		{"valid", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 1, MinDelay: Duration(time.Millisecond), MaxDelay: Duration(time.Second)}, false},
		{"negative W1", ShaperConfig{W1: -0.1, Lambda1: 10, Sigma: 1}, true},
		{"W1 > 1", ShaperConfig{W1: 1.5, Lambda1: 10, Sigma: 1}, true},
		{"Lambda1 = 0", ShaperConfig{W1: 0.5, Lambda1: 0, Sigma: 1}, true},
		{"Sigma = 0", ShaperConfig{W1: 0.5, Lambda1: 10, Sigma: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.err {
				t.Errorf("Validate() error = %v, wantErr = %v", err, tt.err)
			}
		})
	}
}