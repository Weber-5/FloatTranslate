// Package api wires the HTTP surface: chi routes, middlewares (local session
// auth, CORS, redacted request logging, panic recovery) and the endpoint
// handlers for the Phase 1 skeleton.
package api

import (
	"log/slog"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/Weber-5/FloatTranslate/backend/internal/apperr"
	"github.com/Weber-5/FloatTranslate/backend/internal/logging"
)

// statusRecorder captures the response status for request logging.
type statusRecorder struct {
	http.ResponseWriter
	status atomic.Int32
}

func (w *statusRecorder) WriteHeader(code int) {
	w.status.Store(int32(code))
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Status() int {
	if s := w.status.Load(); s != 0 {
		return int(s)
	}
	return http.StatusOK
}

// RequestLog logs one line per request: method, path, status and duration
// only. Query strings, headers and bodies are deliberately never logged.
// The logger itself runs behind the redaction handler (defense in depth).
func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w}
			next.ServeHTTP(rec, r)
			logging.LogRequest(logger, r.Method, r.URL.Path, rec.Status(), float64(time.Since(start).Microseconds())/1000.0)
		})
	}
}

// allowedOrigins is the frozen CORS allowlist (Tauri webview + Vite dev).
var allowedOrigins = map[string]bool{
	"http://tauri.localhost":  true,
	"https://tauri.localhost": true,
	"tauri://localhost":       true,
	"http://localhost:5173":   true,
	"http://127.0.0.1:5173":   true,
}

// CORS sets the standard CORS headers for allowed origins and short-circuits
// preflight requests with 204.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Auth enforces the local session bearer token on every mounted route.
// A missing or wrong token yields 401 UNAUTHORIZED_LOCAL_SESSION.
func Auth(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("Authorization")
			if token == "" || !constantTimeEqual(got, "Bearer "+token) {
				apperr.WriteHTTP(w, apperr.New(apperr.CodeUnauthorizedLocalSession,
					"local session authorization required", false))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// constantTimeEqual compares two strings without early-exit timing leaks.
func constantTimeEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := 0; i < len(a); i++ {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// Recover converts panics into the standard 500 error envelope so a bug in
// one handler never tears down the sidecar.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered", slog.Any("panic", rec))
					apperr.WriteHTTP(w, apperr.New(apperr.CodeInternal, "内部错误", false))
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
