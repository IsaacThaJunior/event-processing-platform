package sender

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"runtime"
	"strings"

	pkgErr "github.com/pkg/errors"
)

type contextKey string

const logContextKey contextKey = "log_context"

type LogContext struct {
	Error error
}

func RespondWithError(ctx context.Context, w http.ResponseWriter, status int, err error) {

	pc, _, line, ok := runtime.Caller(1) // caller of respondWithError
	funcName := "unknown"
	if ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			// Strip package path, keep only function name
			fullName := fn.Name()
			parts := strings.Split(fullName, ".")
			funcName = parts[len(parts)-1]
		}
	}

	// Log concise error
	log.Printf("Error in %s:%d: %v\n", funcName, line, err)

	if logCtx, ok := ctx.Value(logContextKey).(*LogContext); ok {
		logCtx.Error = pkgErr.WithStack(err)
	}

	var message string
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError:
		// Use generic status text for these error types
		message = http.StatusText(status)
	default:
		// Use the original error text for other status codes
		if err != nil {
			message = err.Error()
		} else {
			message = http.StatusText(status)
		}
	}

	// Respond to Client
	http.Error(w, message, status)
}

func RespondWithJSON(w http.ResponseWriter, code int, payload any) {
	response, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, "An error occured during marshalling", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write(response)
}
