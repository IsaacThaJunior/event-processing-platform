package middleware

import (
	"context"
	"net/http"

	"go.opentelemetry.io/otel/trace"
)

type contextKey string

const TraceIDKey contextKey = "trace_id"

// TraceMiddleware reads the OTel trace ID from the active span (set by otelhttp)
// and stores it in context + X-Trace-ID response header. Must run after otelhttp middleware.
func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := trace.SpanFromContext(r.Context()).SpanContext().TraceID().String()

		ctx := context.WithValue(r.Context(), TraceIDKey, traceID)
		w.Header().Set("X-Trace-ID", traceID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}