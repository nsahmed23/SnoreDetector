// Package config loads service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type SyncService struct {
	Addr           string
	DatabaseURL    string
	AppleAudience  string
	AppleIssuer    string
	JWTSigningKey  []byte
	JWTIssuer      string
	JWTTTL         time.Duration
	LogLevel       string
	RequestTimeout time.Duration
}

func LoadSyncService() (*SyncService, error) {
	cfg := &SyncService{
		Addr:           getenv("ADDR", ":8080"),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		AppleAudience:  getenv("APPLE_AUDIENCE", "com.snoreguard.app"),
		AppleIssuer:    getenv("APPLE_ISSUER", "https://appleid.apple.com"),
		JWTSigningKey:  []byte(os.Getenv("JWT_SIGNING_KEY")),
		JWTIssuer:      getenv("JWT_ISSUER", "snoreguard-sync"),
		LogLevel:       getenv("LOG_LEVEL", "info"),
		RequestTimeout: getDuration("REQUEST_TIMEOUT", 15*time.Second),
		JWTTTL:         getDuration("JWT_TTL", 30*24*time.Hour),
	}

	var errs []error
	if cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(cfg.JWTSigningKey) < 32 {
		errs = append(errs, errors.New("JWT_SIGNING_KEY must be at least 32 bytes"))
	}
	if cfg.AppleAudience == "" {
		errs = append(errs, errors.New("APPLE_AUDIENCE is required"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		// Fall back to seconds-as-int if it parses that way.
		if n, perr := strconv.Atoi(v); perr == nil {
			return time.Duration(n) * time.Second
		}
		fmt.Fprintf(os.Stderr, "config: invalid %s=%q, using default %s\n", key, v, fallback)
		return fallback
	}
	return d
}
