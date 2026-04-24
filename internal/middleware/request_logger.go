package middleware

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

const logContextKey contextKey = "log_context"

type LogContext struct {
	Error  error
	Events []LogEvent
}

type LogEvent struct {
	Step      string `json:"step"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	Error     string `json:"error,omitempty"`
	Timestamp string `json:"timestamp"`
}

func (l *LogContext) AddEvent(
	step string,
	status string,
	err error,
) {
	event := LogEvent{
		Step:      step,
		Status:    status,
		Timestamp: time.Now().Format(time.RFC3339),
	}

	if err != nil {
		event.Error = err.Error()
		l.Error = err
	}

	l.Events = append(l.Events, event)
}

func GetLogContext(ctx context.Context) *LogContext {
	logCtx, ok := ctx.Value(logContextKey).(*LogContext)
	if !ok {
		return &LogContext{}
	}
	return logCtx
}

type spyReadCloser struct {
	io.ReadCloser
	bytesRead int
}

type spyResponseWriter struct {
	http.ResponseWriter
	bytesWritten int
	statusCode   int
}

func (r *spyReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.bytesRead += n
	return n, err
}

func (w *spyResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}

	n, err := w.ResponseWriter.Write(p)
	w.bytesWritten += n
	return n, err
}

func (w *spyResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func RequestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logCtx := &LogContext{
				Events: []LogEvent{},
			}

			ctx := context.WithValue(r.Context(), logContextKey, logCtx)
			r = r.WithContext(ctx)

			// Track request body size
			spyReader := &spyReadCloser{
				ReadCloser: r.Body,
			}
			r.Body = spyReader

			// Track response info
			spyWriter := &spyResponseWriter{
				ResponseWriter: w,
			}

			start := time.Now()

			// Execute request
			next.ServeHTTP(spyWriter, r)

			duration := time.Since(start)
			redactedIP := redactIP(r.RemoteAddr)

			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"client_ip", redactedIP,
				slog.Duration("duration", duration),
				"request_body_bytes", spyReader.bytesRead,
				"response_status", spyWriter.statusCode,
				"response_body_bytes", spyWriter.bytesWritten,
				"events", logCtx.Events,
			}

			if logCtx.Error != nil {
				attrs = append(attrs, "error", logCtx.Error)
			}

			if traceID := r.Header.Get("X-Trace-ID"); traceID != "" {
				attrs = append(attrs, "trace_id", traceID)
			}

			// Emit ONE final structured log
			logger.Info("request completed", attrs...)
		})
	}
}

func redactIP(address string) string {
	// Strip port first
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return address
	}

	// Redact IPv4 only
	if ip4 := ip.To4(); ip4 != nil {
		parts := strings.Split(host, ".")
		if len(parts) == 4 {
			parts[3] = "x"
			return strings.Join(parts, ".")
		}
	}

	// Return IPv6 unchanged
	return host
}
