// Package otel sets up OpenTelemetry for every backend service.
//
// Configuration is environment-driven via the standard OTEL_* vars
// (https://opentelemetry.io/docs/specs/otel/configuration/sdk-environment-variables/):
//
//   OTEL_EXPORTER_OTLP_ENDPOINT   default: empty → no-op exporters
//   OTEL_EXPORTER_OTLP_PROTOCOL   default: http/protobuf
//   OTEL_SERVICE_NAME             overrides the `service` arg
//   OTEL_RESOURCE_ATTRIBUTES      extra resource attributes
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is empty the providers are still
// installed globally but use no-op exporters, so app code can
// unconditionally call `otel.Tracer(...)` etc. without nil checks.
package otel

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// Shutdown drains exporters with a hard timeout. Always call it on
// service exit; missed shutdowns drop the last batch of telemetry.
type Shutdown func(context.Context) error

// Setup installs the global tracer + meter providers. Returns a
// Shutdown closure that flushes both exporters.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is unset, exporters are no-ops
// (the providers are still installed so callers can use otel.Tracer
// without conditional logic).
func Setup(ctx context.Context, service, version string) (Shutdown, error) {
	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
		resource.WithAttributes(
			semconv.ServiceName(service),
			semconv.ServiceVersion(version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("otel: resource: %w", err)
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	var tp *sdktrace.TracerProvider
	var mp *metric.MeterProvider

	if endpoint == "" {
		// No-op exporters keep instrumentation cheap when telemetry
		// is disabled (e.g. in tests).
		tp = sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		mp = metric.NewMeterProvider(metric.WithResource(res))
	} else {
		traceExp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, fmt.Errorf("otel: trace exporter: %w", err)
		}
		metricExp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			_ = traceExp.Shutdown(ctx)
			return nil, fmt.Errorf("otel: metric exporter: %w", err)
		}
		tp = sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(traceExp,
				sdktrace.WithBatchTimeout(5*time.Second),
			),
			sdktrace.WithResource(res),
		)
		mp = metric.NewMeterProvider(
			metric.WithReader(metric.NewPeriodicReader(metricExp,
				metric.WithInterval(15*time.Second),
			)),
			metric.WithResource(res),
		)
	}

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return func(shutdownCtx context.Context) error {
		var errs []error
		if err := tp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
		if err := mp.Shutdown(shutdownCtx); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	}, nil
}
