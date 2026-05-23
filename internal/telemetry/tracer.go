package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// NewTracerProvider creates an OTel tracer provider that exports to the OTel Collector
// via HTTP. Call tp.Shutdown(ctx) on app exit to flush remaining spans.
func NewTracerProvider(ctx context.Context, endpoint string) (*sdktrace.TracerProvider, error) {
	// The OTel batch exporter retries automatically; demote transient export
	// errors (e.g. collector not yet ready at startup) from log.Printf to debug
	// so they don't pollute the application's output.
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		slog.Default().Debug("otel exporter error", "error", err)
	}))

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", "event-app"),
			attribute.String("service.version", "1.0.0"),
			attribute.String("deployment.environment", "development"),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp, nil
}
