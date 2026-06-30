package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var ConfigPath string

type ProxyMode int

const (
	ModeClient ProxyMode = iota
	ModeServer
)

func (m ProxyMode) String() string {
	if m == ModeClient {
		return "client"
	}
	return "server"
}

func ParseProxyMode(s string) (ProxyMode, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "client":
		return ModeClient, nil
	case "server":
		return ModeServer, nil
	default:
		return 0, errors.New("invalid mode: must be 'client' or 'server'")
	}
}

type ProfileName string

const (
	ProfileSpotify ProfileName = "spotify"
	ProfileYouTube ProfileName = "youtube"
	ProfileGeneric ProfileName = "generic"
)

func (p ProfileName) String() string {
	return string(p)
}

func ParseProfileName(s string) (ProfileName, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "spotify":
		return ProfileSpotify, nil
	case "youtube":
		return ProfileYouTube, nil
	case "generic":
		return ProfileGeneric, nil
	default:
		return "", errors.New("invalid profile: must be 'spotify', 'youtube', or 'generic'")
	}
}

type Duration time.Duration

func (d *Duration) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" {
		*d = 0
		return nil
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

const (
	envListenAddr       = "CHAMELEON_LISTEN"
	envRemoteAddr       = "CHAMELEON_TARGET"
	envMode             = "CHAMELEON_MODE"
	envPassphrase       = "CHAMELEON_PASSPHRASE"
	envProfile          = "CHAMELEON_PROFILE"
	envPoissonLambda    = "CHAMELEON_POISSON_LAMBDA"
	envChaffRatio       = "CHAMELEON_CHAFF_RATIO"
	envMinShapeDelay    = "CHAMELEON_MIN_SHAPE_DELAY"
	envMaxShapeDelay    = "CHAMELEON_MAX_SHAPE_DELAY"
	envMaxConnections   = "CHAMELEON_MAX_CONNECTIONS"
	envBufferSize       = "CHAMELEON_BUFFER_SIZE"
	envKDFIterations    = "CHAMELEON_KDF_ITERATIONS"
	envReadTimeout      = "CHAMELEON_READ_TIMEOUT"
	envWriteTimeout     = "CHAMELEON_WRITE_TIMEOUT"
	envHandshakeTimeout = "CHAMELEON_HANDSHAKE_TIMEOUT"
	envIdleTimeout      = "CHAMELEON_IDLE_TIMEOUT"
	envShaperW1         = "CHAMELEON_SHAPER_W1"
	envShaperLambda1    = "CHAMELEON_SHAPER_LAMBDA1"
	envShaperMu         = "CHAMELEON_SHAPER_MU"
	envShaperSigma      = "CHAMELEON_SHAPER_SIGMA"
	envServerW1         = "CHAMELEON_SERVER_SHAPER_W1"
	envServerLambda1    = "CHAMELEON_SERVER_SHAPER_LAMBDA1"
	envServerMu         = "CHAMELEON_SERVER_SHAPER_MU"
	envServerSigma      = "CHAMELEON_SERVER_SHAPER_SIGMA"
	envServerMinDelay   = "CHAMELEON_SERVER_MIN_SHAPE_DELAY"
	envServerMaxDelay   = "CHAMELEON_SERVER_MAX_SHAPE_DELAY"
)

type ShaperConfig struct {
	W1       float64
	Lambda1  float64
	Mu       float64
	Sigma    float64
	MinDelay Duration
	MaxDelay Duration
}

func (s ShaperConfig) Validate() error {
	if s.W1 < 0 || s.W1 > 1 {
		return errors.New("shaper W1 must be in [0, 1]")
	}
	if s.Lambda1 <= 0 {
		return errors.New("shaper Lambda1 must be positive")
	}
	if s.Sigma <= 0 {
		return errors.New("shaper Sigma must be positive")
	}
	if s.MinDelay < 0 {
		return errors.New("shaper MinDelay cannot be negative")
	}
	if s.MaxDelay < 0 {
		return errors.New("shaper MaxDelay cannot be negative")
	}
	if s.MinDelay > s.MaxDelay {
		return errors.New("shaper MinDelay cannot exceed MaxDelay")
	}
	return nil
}

type Config struct {
	ListenAddr       string        `json:"listen_addr,omitempty"`
	RemoteAddr       string        `json:"remote_addr,omitempty"`
	Mode             ProxyMode     `json:"-"`
	ModeStr          string        `json:"mode,omitempty"`
	Passphrase       string        `json:"passphrase,omitempty"`
	Profile          ProfileName   `json:"profile,omitempty"`
	ChaffLambda      float64       `json:"chaff_lambda,omitempty"`
	ChaffRatio       float64       `json:"chaff_ratio,omitempty"`
	MinShapeDelay    Duration      `json:"min_shape_delay,omitempty"`
	MaxShapeDelay    Duration      `json:"max_shape_delay,omitempty"`
	MaxConnections   int           `json:"max_connections,omitempty"`
	BufferSize       int           `json:"buffer_size,omitempty"`
	KDFIterations    int           `json:"kdf_iterations,omitempty"`
	ReadTimeout      Duration      `json:"read_timeout,omitempty"`
	WriteTimeout     Duration      `json:"write_timeout,omitempty"`
	HandshakeTimeout Duration      `json:"handshake_timeout,omitempty"`
	IdleTimeout      Duration      `json:"idle_timeout,omitempty"`
	ShaperClient     ShaperConfig  `json:"shaper_client,omitempty"`
	ShaperServer     ShaperConfig  `json:"shaper_server,omitempty"`
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return Duration(d)
		}
	}
	return Duration(fallback)
}

func DefaultShaperClient(profile ProfileName) ShaperConfig {
	switch profile {
	case ProfileSpotify:
		return ShaperConfig{
			W1:       0.7,
			Lambda1:  50.0,
			Mu:       -4.0,
			Sigma:    1.5,
			MinDelay: Duration(5 * time.Millisecond),
			MaxDelay: Duration(200 * time.Millisecond),
		}
	case ProfileYouTube:
		return ShaperConfig{
			W1:       0.5,
			Lambda1:  30.0,
			Mu:       -2.0,
			Sigma:    2.0,
			MinDelay: Duration(2 * time.Millisecond),
			MaxDelay: Duration(500 * time.Millisecond),
		}
	default:
		return ShaperConfig{
			W1:       0.8,
			Lambda1:  40.0,
			Mu:       -3.0,
			Sigma:    1.0,
			MinDelay: Duration(1 * time.Millisecond),
			MaxDelay: Duration(1000 * time.Millisecond),
		}
	}
}

func DefaultShaperServer(profile ProfileName) ShaperConfig {
	switch profile {
	case ProfileSpotify:
		return ShaperConfig{
			W1:       0.6,
			Lambda1:  10.0,
			Mu:       -2.0,
			Sigma:    1.0,
			MinDelay: Duration(10 * time.Millisecond),
			MaxDelay: Duration(1000 * time.Millisecond),
		}
	case ProfileYouTube:
		return ShaperConfig{
			W1:       0.4,
			Lambda1:  5.0,
			Mu:       -1.0,
			Sigma:    1.5,
			MinDelay: Duration(5 * time.Millisecond),
			MaxDelay: Duration(2000 * time.Millisecond),
		}
	default:
		return ShaperConfig{
			W1:       0.7,
			Lambda1:  15.0,
			Mu:       -2.5,
			Sigma:    1.2,
			MinDelay: Duration(5 * time.Millisecond),
			MaxDelay: Duration(1500 * time.Millisecond),
		}
	}
}

func DefaultChaffLambda(profile ProfileName) float64 {
	switch profile {
	case ProfileSpotify:
		return 50.0
	case ProfileYouTube:
		return 30.0
	default:
		return 20.0
	}
}

func DefaultChaffRatio(profile ProfileName) float64 {
	switch profile {
	case ProfileSpotify:
		return 0.4
	case ProfileYouTube:
		return 0.3
	default:
		return 0.2
	}
}

func loadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config file: %w", err)
	}
	cfg := &Config{}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config file: %w", err)
	}
	if cfg.ModeStr != "" {
		m, err := ParseProxyMode(cfg.ModeStr)
		if err != nil {
			return nil, err
		}
		cfg.Mode = m
	}
	return cfg, nil
}

func Load() (*Config, error) {
	cfg := &Config{
		ListenAddr:     "127.0.0.1:1080",
		Mode:           ModeClient,
		Profile:        ProfileSpotify,
		MinShapeDelay:  Duration(1 * time.Millisecond),
		MaxShapeDelay:  Duration(500 * time.Millisecond),
		MaxConnections: 100,
		BufferSize:     32768,
		KDFIterations:  100000,
		ReadTimeout:    Duration(30 * time.Second),
		WriteTimeout:   Duration(30 * time.Second),
		HandshakeTimeout: Duration(10 * time.Second),
		IdleTimeout:    Duration(300 * time.Second),
	}

	if ConfigPath != "" {
		fileCfg, err := loadFromFile(ConfigPath)
		if err != nil {
			return nil, err
		}
		if fileCfg.ListenAddr != "" {
			cfg.ListenAddr = fileCfg.ListenAddr
		}
		if fileCfg.RemoteAddr != "" {
			cfg.RemoteAddr = fileCfg.RemoteAddr
		}
		if fileCfg.ModeStr != "" {
			cfg.Mode = fileCfg.Mode
		}
		if fileCfg.Passphrase != "" {
			cfg.Passphrase = fileCfg.Passphrase
		}
		if fileCfg.Profile != "" {
			cfg.Profile = fileCfg.Profile
		}
		if fileCfg.ChaffLambda != 0 {
			cfg.ChaffLambda = fileCfg.ChaffLambda
		}
		if fileCfg.ChaffRatio != 0 {
			cfg.ChaffRatio = fileCfg.ChaffRatio
		}
		if fileCfg.MinShapeDelay != 0 {
			cfg.MinShapeDelay = fileCfg.MinShapeDelay
		}
		if fileCfg.MaxShapeDelay != 0 {
			cfg.MaxShapeDelay = fileCfg.MaxShapeDelay
		}
		if fileCfg.MaxConnections != 0 {
			cfg.MaxConnections = fileCfg.MaxConnections
		}
		if fileCfg.BufferSize != 0 {
			cfg.BufferSize = fileCfg.BufferSize
		}
		if fileCfg.KDFIterations != 0 {
			cfg.KDFIterations = fileCfg.KDFIterations
		}
		if fileCfg.ReadTimeout != 0 {
			cfg.ReadTimeout = fileCfg.ReadTimeout
		}
		if fileCfg.WriteTimeout != 0 {
			cfg.WriteTimeout = fileCfg.WriteTimeout
		}
		if fileCfg.HandshakeTimeout != 0 {
			cfg.HandshakeTimeout = fileCfg.HandshakeTimeout
		}
		if fileCfg.IdleTimeout != 0 {
			cfg.IdleTimeout = fileCfg.IdleTimeout
		}
		if fileCfg.ShaperClient != (ShaperConfig{}) {
			cfg.ShaperClient = fileCfg.ShaperClient
		}
		if fileCfg.ShaperServer != (ShaperConfig{}) {
			cfg.ShaperServer = fileCfg.ShaperServer
		}
	}

	cfg.ListenAddr = getEnv(envListenAddr, cfg.ListenAddr)
	cfg.RemoteAddr = getEnv(envRemoteAddr, cfg.RemoteAddr)
	cfg.Passphrase = getEnv(envPassphrase, cfg.Passphrase)
	cfg.MaxConnections = getEnvInt(envMaxConnections, cfg.MaxConnections)
	cfg.BufferSize = getEnvInt(envBufferSize, cfg.BufferSize)
	cfg.KDFIterations = getEnvInt(envKDFIterations, cfg.KDFIterations)

	if v := os.Getenv(envMode); v != "" {
		m, err := ParseProxyMode(v)
		if err != nil {
			return nil, err
		}
		cfg.Mode = m
	}

	if v := os.Getenv(envProfile); v != "" {
		p, err := ParseProfileName(v)
		if err != nil {
			return nil, err
		}
		cfg.Profile = p
	}

	cfg.ChaffLambda = getEnvFloat(envPoissonLambda, 0)
	if cfg.ChaffLambda == 0 {
		cfg.ChaffLambda = DefaultChaffLambda(cfg.Profile)
	}

	cfg.ChaffRatio = getEnvFloat(envChaffRatio, 0)
	if cfg.ChaffRatio == 0 {
		cfg.ChaffRatio = DefaultChaffRatio(cfg.Profile)
	}

	cfg.MinShapeDelay = getEnvDuration(envMinShapeDelay, cfg.MinShapeDelay.Duration())
	cfg.MaxShapeDelay = getEnvDuration(envMaxShapeDelay, cfg.MaxShapeDelay.Duration())

	cfg.ReadTimeout = getEnvDuration(envReadTimeout, cfg.ReadTimeout.Duration())
	cfg.WriteTimeout = getEnvDuration(envWriteTimeout, cfg.WriteTimeout.Duration())
	cfg.HandshakeTimeout = getEnvDuration(envHandshakeTimeout, cfg.HandshakeTimeout.Duration())
	cfg.IdleTimeout = getEnvDuration(envIdleTimeout, cfg.IdleTimeout.Duration())

	cfg.ShaperClient = DefaultShaperClient(cfg.Profile)
	cfg.ShaperClient.MinDelay = getEnvDuration(envMinShapeDelay, cfg.ShaperClient.MinDelay.Duration())
	cfg.ShaperClient.MaxDelay = getEnvDuration(envMaxShapeDelay, cfg.ShaperClient.MaxDelay.Duration())
	cfg.ShaperClient.W1 = getEnvFloat(envShaperW1, cfg.ShaperClient.W1)
	cfg.ShaperClient.Lambda1 = getEnvFloat(envShaperLambda1, cfg.ShaperClient.Lambda1)
	cfg.ShaperClient.Mu = getEnvFloat(envShaperMu, cfg.ShaperClient.Mu)
	cfg.ShaperClient.Sigma = getEnvFloat(envShaperSigma, cfg.ShaperClient.Sigma)

	cfg.ShaperServer = DefaultShaperServer(cfg.Profile)
	cfg.ShaperServer.MinDelay = getEnvDuration(envServerMinDelay, cfg.ShaperServer.MinDelay.Duration())
	cfg.ShaperServer.MaxDelay = getEnvDuration(envServerMaxDelay, cfg.ShaperServer.MaxDelay.Duration())
	cfg.ShaperServer.W1 = getEnvFloat(envServerW1, cfg.ShaperServer.W1)
	cfg.ShaperServer.Lambda1 = getEnvFloat(envServerLambda1, cfg.ShaperServer.Lambda1)
	cfg.ShaperServer.Mu = getEnvFloat(envServerMu, cfg.ShaperServer.Mu)
	cfg.ShaperServer.Sigma = getEnvFloat(envServerSigma, cfg.ShaperServer.Sigma)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return errors.New("listen address cannot be empty")
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return errors.New("invalid listen address format: " + err.Error())
	}

	if c.Mode == ModeClient {
		if strings.TrimSpace(c.RemoteAddr) == "" {
			return errors.New("remote address (CHAMELEON_TARGET) is required in client mode")
		}
		if _, _, err := net.SplitHostPort(c.RemoteAddr); err != nil {
			return errors.New("invalid remote address format: " + err.Error())
		}
	}

	if strings.TrimSpace(c.Passphrase) == "" {
		return errors.New("passphrase cannot be empty (set CHAMELEON_PASSPHRASE)")
	}
	if len(c.Passphrase) < 16 {
		return errors.New("passphrase must be at least 16 characters")
	}

	if c.ChaffLambda <= 0 {
		return errors.New("chaff lambda must be positive")
	}
	if c.ChaffLambda > 1000 {
		return errors.New("chaff lambda exceeds maximum (1000)")
	}

	if c.ChaffRatio < 0 || c.ChaffRatio > 1 {
		return errors.New("chaff ratio must be in [0, 1]")
	}

	if c.MinShapeDelay < 0 {
		return errors.New("min shape delay cannot be negative")
	}
	if c.MaxShapeDelay < 0 {
		return errors.New("max shape delay cannot be negative")
	}
	if c.MinShapeDelay > c.MaxShapeDelay {
		return errors.New("min shape delay cannot exceed max shape delay")
	}

	if c.MaxConnections <= 0 {
		return errors.New("max connections must be positive")
	}
	if c.MaxConnections > 10000 {
		return errors.New("max connections exceeds limit (10000)")
	}

	if c.BufferSize < 4096 {
		return errors.New("buffer size must be at least 4096")
	}
	if c.BufferSize > 1048576 {
		return errors.New("buffer size exceeds maximum (1MB)")
	}
	if (c.BufferSize & (c.BufferSize - 1)) != 0 {
		return errors.New("buffer size must be a power of two")
	}

	if c.KDFIterations < 10000 {
		return errors.New("KDF iterations must be at least 10000")
	}
	if c.KDFIterations > 1000000 {
		return errors.New("KDF iterations exceeds maximum (1000000)")
	}

	if c.ReadTimeout <= 0 {
		return errors.New("read timeout must be positive")
	}
	if c.WriteTimeout <= 0 {
		return errors.New("write timeout must be positive")
	}
	if c.HandshakeTimeout <= 0 {
		return errors.New("handshake timeout must be positive")
	}
	if c.IdleTimeout <= 0 {
		return errors.New("idle timeout must be positive")
	}
	if c.HandshakeTimeout > c.ReadTimeout {
		return errors.New("handshake timeout cannot exceed read timeout")
	}
	if c.HandshakeTimeout > c.WriteTimeout {
		return errors.New("handshake timeout cannot exceed write timeout")
	}

	if err := c.ShaperClient.Validate(); err != nil {
		return errors.New("client shaper config: " + err.Error())
	}
	if err := c.ShaperServer.Validate(); err != nil {
		return errors.New("server shaper config: " + err.Error())
	}

	return nil
}

func (c *Config) ClientShaperConfig() ShaperConfig {
	return c.ShaperClient
}

func (c *Config) ServerShaperConfig() ShaperConfig {
	return c.ShaperServer
}

func (c *Config) EffectiveChaffLambda() float64 {
	return c.ChaffLambda
}

func (c *Config) EffectiveChaffRatio() float64 {
	return c.ChaffRatio
}