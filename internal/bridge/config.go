package bridge

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultListenAddr          = "127.0.0.1:8787"
	defaultUpstreamURL         = "https://openrouter.ai/api/v1/images"
	defaultUpstreamTimeout     = 10 * time.Minute
	defaultMaxRequestBytes     = int64(64 * 1024 * 1024)
	defaultMaxResponseBytes    = int64(128 * 1024 * 1024)
	defaultMaxReferenceImages  = 10
	defaultMaxConcurrent       = 2
	defaultMaxScalarFieldBytes = int64(1 * 1024 * 1024)
)

// Config controls the bridge. No API key is accepted here by design.
type Config struct {
	ListenAddr          string
	UpstreamURL         *url.URL
	UpstreamTimeout     time.Duration
	MaxRequestBytes     int64
	MaxResponseBytes    int64
	MaxReferenceImages  int
	MaxConcurrent       int
	MaxScalarFieldBytes int64
	AllowHTTPUpstream   bool
	AllowNonLoopback    bool
}

func LoadConfigFromEnv() (Config, error) {
	cfg := Config{
		ListenAddr:          envOrDefault("LISTEN_ADDR", defaultListenAddr),
		UpstreamTimeout:     defaultUpstreamTimeout,
		MaxRequestBytes:     defaultMaxRequestBytes,
		MaxResponseBytes:    defaultMaxResponseBytes,
		MaxReferenceImages:  defaultMaxReferenceImages,
		MaxConcurrent:       defaultMaxConcurrent,
		MaxScalarFieldBytes: defaultMaxScalarFieldBytes,
		AllowHTTPUpstream:   envBool("ALLOW_HTTP_UPSTREAM", false),
		AllowNonLoopback:    envBool("ALLOW_NON_LOOPBACK_LISTEN", false),
	}

	var err error
	if raw := strings.TrimSpace(os.Getenv("UPSTREAM_TIMEOUT")); raw != "" {
		cfg.UpstreamTimeout, err = time.ParseDuration(raw)
		if err != nil || cfg.UpstreamTimeout <= 0 {
			return Config{}, fmt.Errorf("invalid UPSTREAM_TIMEOUT %q", raw)
		}
	}
	if cfg.MaxRequestBytes, err = envInt64("MAX_REQUEST_BYTES", cfg.MaxRequestBytes); err != nil {
		return Config{}, err
	}
	if cfg.MaxResponseBytes, err = envInt64("MAX_RESPONSE_BYTES", cfg.MaxResponseBytes); err != nil {
		return Config{}, err
	}
	if cfg.MaxScalarFieldBytes, err = envInt64("MAX_SCALAR_FIELD_BYTES", cfg.MaxScalarFieldBytes); err != nil {
		return Config{}, err
	}
	if cfg.MaxReferenceImages, err = envInt("MAX_REFERENCE_IMAGES", cfg.MaxReferenceImages); err != nil {
		return Config{}, err
	}
	if cfg.MaxConcurrent, err = envInt("MAX_CONCURRENT_REQUESTS", cfg.MaxConcurrent); err != nil {
		return Config{}, err
	}

	rawURL := envOrDefault("OPENROUTER_IMAGES_URL", defaultUpstreamURL)
	cfg.UpstreamURL, err = url.Parse(rawURL)
	if err != nil {
		return Config{}, fmt.Errorf("invalid OPENROUTER_IMAGES_URL: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ListenAddr) == "" {
		return fmt.Errorf("LISTEN_ADDR cannot be empty")
	}
	host, _, err := net.SplitHostPort(c.ListenAddr)
	if err != nil {
		return fmt.Errorf("invalid LISTEN_ADDR %q: %w", c.ListenAddr, err)
	}
	if !c.AllowNonLoopback {
		ip := net.ParseIP(host)
		if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return fmt.Errorf("LISTEN_ADDR must use a loopback address unless ALLOW_NON_LOOPBACK_LISTEN=true")
		}
	}
	if c.UpstreamURL == nil || c.UpstreamURL.Host == "" {
		return fmt.Errorf("OPENROUTER_IMAGES_URL must be an absolute URL")
	}
	if c.UpstreamURL.User != nil || c.UpstreamURL.Fragment != "" {
		return fmt.Errorf("OPENROUTER_IMAGES_URL must not contain userinfo or a fragment")
	}
	if c.UpstreamURL.Scheme != "https" && !(c.AllowHTTPUpstream && c.UpstreamURL.Scheme == "http") {
		return fmt.Errorf("OPENROUTER_IMAGES_URL must use https unless ALLOW_HTTP_UPSTREAM=true")
	}
	if c.UpstreamTimeout <= 0 {
		return fmt.Errorf("UPSTREAM_TIMEOUT must be greater than zero")
	}
	if c.MaxRequestBytes < 1024 {
		return fmt.Errorf("MAX_REQUEST_BYTES must be at least 1024")
	}
	if c.MaxResponseBytes < 1024 {
		return fmt.Errorf("MAX_RESPONSE_BYTES must be at least 1024")
	}
	if c.MaxScalarFieldBytes < 1024 || c.MaxScalarFieldBytes > c.MaxRequestBytes {
		return fmt.Errorf("MAX_SCALAR_FIELD_BYTES must be between 1024 and MAX_REQUEST_BYTES")
	}
	if c.MaxReferenceImages < 1 || c.MaxReferenceImages > 100 {
		return fmt.Errorf("MAX_REFERENCE_IMAGES must be between 1 and 100")
	}
	if c.MaxConcurrent < 1 || c.MaxConcurrent > 64 {
		return fmt.Errorf("MAX_CONCURRENT_REQUESTS must be between 1 and 64")
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func envBool(name string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return fallback
	}
	return value
}

func envInt(name string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return value, nil
}

func envInt64(name string, fallback int64) (int64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q", name, raw)
	}
	return value, nil
}
