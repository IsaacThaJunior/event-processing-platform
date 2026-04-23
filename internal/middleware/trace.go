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

var allowedOrigins = map[string]bool{
	"https://my-site-frontend-ten.vercel.app": true,
	"http://localhost:3000":                   true,
	"https://www.isaacjunior.dev":             true,
	"http://localhost:3001":                   true,

}

func EnableCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if allowedOrigins[origin] {
			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Pass to next handler
		next.ServeHTTP(w, r)
	})
}
