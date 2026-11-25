package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr          string
	RCONAddr          string
	RCONPassword      string
	DatabaseURL       string
	InboxPath         string
	ArtifactPath      string
	RCONTimeout       time.Duration
	CaptureTimeout    time.Duration
	PollInterval      time.Duration
	ArtifactTTL       time.Duration
	RetentionInterval time.Duration
	MaxRequestBytes   int64
	MaxArtifactBytes  int64
	MaxPixels         int64
}

func Load() (Config, error) {
	rconTimeout, err := duration("RUNNER_RCON_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	captureTimeout, err := duration("RUNNER_CAPTURE_TIMEOUT", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	pollInterval, err := duration("RUNNER_POLL_INTERVAL", 500*time.Millisecond)
	if err != nil {
		return Config{}, err
	}
	artifactTTL, err := duration("RUNNER_ARTIFACT_TTL", 24*time.Hour)
	if err != nil {
		return Config{}, err
	}
	retentionInterval, err := duration("RUNNER_RETENTION_INTERVAL", 15*time.Minute)
	if err != nil {
		return Config{}, err
	}
	maxRequestBytes, err := integer("RUNNER_MAX_REQUEST_BYTES", 2<<20)
	if err != nil {
		return Config{}, err
	}
	maxArtifactBytes, err := integer("RUNNER_MAX_ARTIFACT_BYTES", 80<<20)
	if err != nil {
		return Config{}, err
	}
	maxPixels, err := integer("RUNNER_MAX_ARTIFACT_PIXELS", 4096*4096)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPAddr:          env("RUNNER_HTTP_ADDR", ":8081"),
		RCONAddr:          env("FACTORIO_RCON_ADDR", "factorio-server:27015"),
		RCONPassword:      os.Getenv("FACTORIO_RCON_PASSWORD"),
		DatabaseURL:       os.Getenv("RUNNER_DATABASE_URL"),
		InboxPath:         env("RUNNER_INBOX_PATH", "/data/inbox"),
		ArtifactPath:      env("RUNNER_ARTIFACT_PATH", "/data/artifacts"),
		RCONTimeout:       rconTimeout,
		CaptureTimeout:    captureTimeout,
		PollInterval:      pollInterval,
		ArtifactTTL:       artifactTTL,
		RetentionInterval: retentionInterval,
		MaxRequestBytes:   int64(maxRequestBytes),
		MaxArtifactBytes:  int64(maxArtifactBytes),
		MaxPixels:         int64(maxPixels),
	}

	if cfg.RCONPassword == "" {
		return Config{}, fmt.Errorf("FACTORIO_RCON_PASSWORD is required")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("RUNNER_DATABASE_URL is required")
	}
	if cfg.MaxRequestBytes < 1 || cfg.MaxArtifactBytes < 1 || cfg.MaxPixels < 1 {
		return Config{}, fmt.Errorf("runner limits must be positive")
	}
	return cfg, nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func duration(key string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", key, err)
	}
	if parsed <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return parsed, nil
}

func integer(key string, fallback int) (int, error) {
	value := os.Getenv(key)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return parsed, nil
}
