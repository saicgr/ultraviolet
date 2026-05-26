// Package tracing wires up an OpenTelemetry tracer provider with an OTLP/gRPC
// exporter. Tracing is OPT-IN: if OTEL_EXPORTER_OTLP_ENDPOINT is unset (and the
// caller passes an empty otlpEndpoint), Init installs a no-op tracer and returns
// a no-op shutdown so production binaries pay zero cost in dev / when an OTel
// collector is not deployed.
package tracing

import (
	"context"
	"fmt"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Init initialises the global OpenTelemetry tracer provider for the calling
// service. The returned shutdown func MUST be deferred by the caller; it
// flushes any buffered spans and tears down the exporter.
//
// Resolution order for the OTLP endpoint:
//  1. otlpEndpoint argument (if non-empty)
//  2. OTEL_EXPORTER_OTLP_ENDPOINT env var
//
// If neither is set, Init returns a no-op shutdown and no error — tracing is
// disabled cleanly.
func Init(ctx context.Context, serviceName, otlpEndpoint string) (func(), error) {
	if otlpEndpoint == "" {
		otlpEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if otlpEndpoint == "" {
		// Tracing is opt-in. Install a no-op provider so StartSpan calls remain valid.
		otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{}, propagation.Baggage{},
		))
		return func() {}, nil
	}

	res, err := sdkresource.New(ctx,
		sdkresource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
		sdkresource.WithProcess(),
		sdkresource.WithHost(),
	)
	if err != nil {
		return func() {}, fmt.Errorf("tracing: build resource: %w", err)
	}

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exp, err := otlptrace.New(dialCtx, otlptracegrpc.NewClient(
		otlptracegrpc.WithEndpoint(otlpEndpoint),
		otlptracegrpc.WithInsecure(),
	))
	if err != nil {
		return func() {}, fmt.Errorf("tracing: otlp exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))

	return func() {
		shutCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = tp.Shutdown(shutCtx)
	}, nil
}

// StartSpan is a thin helper so call sites don't have to import otel directly.
// Returns a child span of ctx (or a root if there is none) tagged with name.
func StartSpan(ctx context.Context, name string) (context.Context, trace.Span) {
	return otel.Tracer("github.com/ultraviolet-dev/ultraviolet").Start(ctx, name)
}
