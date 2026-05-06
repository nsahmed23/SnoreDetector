// Package config loads service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// shared groups the env-driven fields that every backend service
// needs. Service-specific configs embed it.
type shared struct {
	Addr           string
	DatabaseURL    string
	JWTSigningKey  []byte
	JWTIssuer      string
	JWTTTL         time.Duration
	LogLevel       string
	RequestTimeout time.Duration
}

func loadShared(defaultAddr string) (*shared, []error) {
	s := &shared{
		Addr:           getenv("ADDR", defaultAddr),
		DatabaseURL:    os.Getenv("DATABASE_URL"),
		JWTSigningKey:  []byte(os.Getenv("JWT_SIGNING_KEY")),
		JWTIssuer:      getenv("JWT_ISSUER", "snoreguard-sync"),
		LogLevel:       getenv("LOG_LEVEL", "info"),
		RequestTimeout: getDuration("REQUEST_TIMEOUT", 15*time.Second),
		JWTTTL:         getDuration("JWT_TTL", 1*time.Hour),
	}
	var errs []error
	if s.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	if len(s.JWTSigningKey) < 32 {
		errs = append(errs, errors.New("JWT_SIGNING_KEY must be at least 32 bytes"))
	}
	return s, errs
}

type SyncService struct {
	Addr             string
	DatabaseURL      string
	AppleAudience    string
	AppleIssuer      string
	JWTSigningKey    []byte
	JWTIssuer        string
	JWTRefreshIssuer string
	JWTTTL           time.Duration
	JWTRefreshTTL    time.Duration
	LogLevel         string
	RequestTimeout   time.Duration
}

type AnalyticsService struct {
	Addr           string
	DatabaseURL    string
	JWTSigningKey  []byte
	JWTIssuer      string
	JWTTTL         time.Duration
	LogLevel       string
	RequestTimeout time.Duration
}

type ExportService struct {
	Addr               string
	DatabaseURL        string
	JWTSigningKey      []byte
	JWTIssuer          string
	JWTTTL             time.Duration
	LogLevel           string
	RequestTimeout     time.Duration
	ExportBudgetPerDay int
}

func LoadSyncService() (*SyncService, error) {
	s, errs := loadShared(":8080")
	cfg := &SyncService{
		Addr:             s.Addr,
		DatabaseURL:      s.DatabaseURL,
		AppleAudience:    getenv("APPLE_AUDIENCE", "com.snoreguard.app"),
		AppleIssuer:      getenv("APPLE_ISSUER", "https://appleid.apple.com"),
		JWTSigningKey:    s.JWTSigningKey,
		JWTIssuer:        s.JWTIssuer,
		JWTRefreshIssuer: getenv("JWT_REFRESH_ISSUER", "snoreguard-refresh"),
		JWTTTL:           s.JWTTTL,
		JWTRefreshTTL:    getDuration("JWT_REFRESH_TTL", 60*24*time.Hour),
		LogLevel:         s.LogLevel,
		RequestTimeout:   s.RequestTimeout,
	}
	if cfg.AppleAudience == "" {
		errs = append(errs, errors.New("APPLE_AUDIENCE is required"))
	}
	if cfg.JWTRefreshIssuer == cfg.JWTIssuer {
		errs = append(errs, errors.New("JWT_REFRESH_ISSUER must differ from JWT_ISSUER"))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return cfg, nil
}

func LoadAnalyticsService() (*AnalyticsService, error) {
	s, errs := loadShared(":8081")
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &AnalyticsService{
		Addr:           s.Addr,
		DatabaseURL:    s.DatabaseURL,
		JWTSigningKey:  s.JWTSigningKey,
		JWTIssuer:      s.JWTIssuer,
		JWTTTL:         s.JWTTTL,
		LogLevel:       s.LogLevel,
		RequestTimeout: s.RequestTimeout,
	}, nil
}

func LoadExportService() (*ExportService, error) {
	s, errs := loadShared(":8082")
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return &ExportService{
		Addr:               s.Addr,
		DatabaseURL:        s.DatabaseURL,
		JWTSigningKey:      s.JWTSigningKey,
		JWTIssuer:          s.JWTIssuer,
		JWTTTL:             s.JWTTTL,
		LogLevel:           s.LogLevel,
		RequestTimeout:     s.RequestTimeout,
		ExportBudgetPerDay: getInt("EXPORT_BUDGET_PER_DAY", 5),
	}, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getInt returns the env var parsed as an int, or `fallback` if
// unset or unparseable.
func getInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: invalid %s=%q, using default %d\n", key, v, fallback)
		return fallback
	}
	return n
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
