// Package metrics owns the service-scoped OpenTelemetry instruments
// (counters + histograms) used across the three backend services.
//
// Build the Instruments struct once at startup with metrics.New(...)
// and pass it through each service's Deps. All instruments inherit
// the meter provider installed by internal/otel.Setup, so the
// OTEL_EXPORTER_OTLP_ENDPOINT env var toggles export on/off without
// the call sites needing conditional logic.
//
// Call sites that don't construct Instruments (e.g. unit tests) can
// rely on Deps.Metrics being nil — every increment + record method
// on *Instruments is nil-safe and silently no-ops on a nil receiver,
// which keeps test-only code paths from having to wire up a real
// meter provider.
package metrics

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

// Instruments are the service-scoped OTel instruments. Build once at
// startup with New(); the zero value (nil) is also valid and turns
// every method below into a no-op.
type Instruments struct {
	// Counters — dimensionless int64s.
	EventsIngested  metric.Int64Counter // events_ingested_total{format}
	EventsExported  metric.Int64Counter // events_exported_total{format}
	AuthAttempts    metric.Int64Counter // auth_attempts_total{outcome}
	RefreshAttempts metric.Int64Counter // refresh_attempts_total{outcome}

	// Histograms — seconds.
	HandlerDuration    metric.Float64Histogram // http_handler_duration_seconds{route}
	StoreQueryDuration metric.Float64Histogram // store_query_duration_seconds{query}
	JWTVerifyDuration  metric.Float64Histogram // jwt_verify_duration_seconds{kind}
}

// New builds the global instruments scoped to `service`. The returned
// error is only non-nil on instrument-builder failure, which is a
// startup-time bug rather than a runtime concern; call sites can
// panic-on-error.
func New(service string) (*Instruments, error) {
	m := otel.Meter(service)

	eventsIngested, err := m.Int64Counter(
		"events_ingested_total",
		metric.WithDescription("Snore events ingested via POST /events"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: events_ingested_total: %w", err)
	}
	eventsExported, err := m.Int64Counter(
		"events_exported_total",
		metric.WithDescription("Snore events emitted from /export/events.{csv,json}"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: events_exported_total: %w", err)
	}
	authAttempts, err := m.Int64Counter(
		"auth_attempts_total",
		metric.WithDescription("Apple sign-in attempts, labeled by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: auth_attempts_total: %w", err)
	}
	refreshAttempts, err := m.Int64Counter(
		"refresh_attempts_total",
		metric.WithDescription("Refresh-token attempts, labeled by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: refresh_attempts_total: %w", err)
	}

	handlerDur, err := m.Float64Histogram(
		"http_handler_duration_seconds",
		metric.WithDescription("HTTP handler latency from middleware entry to response complete"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: http_handler_duration_seconds: %w", err)
	}
	storeQueryDur, err := m.Float64Histogram(
		"store_query_duration_seconds",
		metric.WithDescription("Postgres query latency, labeled by named query"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: store_query_duration_seconds: %w", err)
	}
	jwtVerifyDur, err := m.Float64Histogram(
		"jwt_verify_duration_seconds",
		metric.WithDescription("Session-token verification latency"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("metrics: jwt_verify_duration_seconds: %w", err)
	}

	return &Instruments{
		EventsIngested:     eventsIngested,
		EventsExported:     eventsExported,
		AuthAttempts:       authAttempts,
		RefreshAttempts:    refreshAttempts,
		HandlerDuration:    handlerDur,
		StoreQueryDuration: storeQueryDur,
		JWTVerifyDuration:  jwtVerifyDur,
	}, nil
}

// AddEventsIngested increments events_ingested_total{format} by n.
// Nil-safe: calls on a nil receiver no-op.
func (i *Instruments) AddEventsIngested(ctx context.Context, n int64, format string) {
	if i == nil || i.EventsIngested == nil {
		return
	}
	i.EventsIngested.Add(ctx, n, metric.WithAttributes(attrFormat(format)))
}

// AddEventsExported increments events_exported_total{format} by n.
func (i *Instruments) AddEventsExported(ctx context.Context, n int64, format string) {
	if i == nil || i.EventsExported == nil {
		return
	}
	i.EventsExported.Add(ctx, n, metric.WithAttributes(attrFormat(format)))
}

// AddAuthAttempt increments auth_attempts_total{outcome} by 1.
func (i *Instruments) AddAuthAttempt(ctx context.Context, outcome string) {
	if i == nil || i.AuthAttempts == nil {
		return
	}
	i.AuthAttempts.Add(ctx, 1, metric.WithAttributes(attrOutcome(outcome)))
}

// AddRefreshAttempt increments refresh_attempts_total{outcome} by 1.
func (i *Instruments) AddRefreshAttempt(ctx context.Context, outcome string) {
	if i == nil || i.RefreshAttempts == nil {
		return
	}
	i.RefreshAttempts.Add(ctx, 1, metric.WithAttributes(attrOutcome(outcome)))
}

// RecordHandlerDuration records http_handler_duration_seconds{route}.
func (i *Instruments) RecordHandlerDuration(ctx context.Context, seconds float64, route string) {
	if i == nil || i.HandlerDuration == nil {
		return
	}
	i.HandlerDuration.Record(ctx, seconds, metric.WithAttributes(attrRoute(route)))
}

// RecordStoreQueryDuration records store_query_duration_seconds{query}.
func (i *Instruments) RecordStoreQueryDuration(ctx context.Context, seconds float64, query string) {
	if i == nil || i.StoreQueryDuration == nil {
		return
	}
	i.StoreQueryDuration.Record(ctx, seconds, metric.WithAttributes(attrQuery(query)))
}

// RecordJWTVerifyDuration records jwt_verify_duration_seconds{kind}.
// `kind` is "access" or "refresh".
func (i *Instruments) RecordJWTVerifyDuration(ctx context.Context, seconds float64, kind string) {
	if i == nil || i.JWTVerifyDuration == nil {
		return
	}
	i.JWTVerifyDuration.Record(ctx, seconds, metric.WithAttributes(attrKind(kind)))
}

// HashUserID returns a stable, non-reversible 16-character hex string
// derived from the user's UUID. Use this anywhere a user ID would
// otherwise appear in span attributes — never log raw UUIDs as span
// attributes.
//
// 8 bytes (16 hex chars) is plenty of cardinality for grouping
// while keeping the hash short enough for trace UIs.
func HashUserID(uid uuid.UUID) string {
	sum := sha256.Sum256(uid[:])
	return hex.EncodeToString(sum[:8])
}
