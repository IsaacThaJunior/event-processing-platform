package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

type contextKey string

const TraceIDKey contextKey = "trace_id"

func TraceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := uuid.New().String()

		ctx := context.WithValue(r.Context(), TraceIDKey, traceID)

		w.Header().Set("X-Trace-ID", traceID)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
