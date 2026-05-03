package api

import (
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/MertDalbudak/mcsm/internal/ids"
)

// statusRecorder wraps ResponseWriter to capture the status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// chain composes middlewares in argument order: chain(a, b, c)(h) → a(b(c(h))).
func chain(mws ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for i := len(mws) - 1; i >= 0; i-- {
			h = mws[i](h)
		}
		return h
	}
}

// requestID assigns a trace id, exposes it on the response header, and
// puts it on the context for handlers / loggers / error envelopes.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = ids.NewTraceID()
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(WithTraceID(r.Context(), id)))
	})
}

// recoverer turns panics into 500s without taking the process down.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic in handler",
					"panic", rec,
					"trace_id", TraceIDFromContext(r.Context()),
					"path", r.URL.Path,
					"stack", string(debug.Stack()),
				)
				WriteError(w, r, http.StatusInternalServerError, CodeInternal, "internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// accessLog emits one structured log per request after it completes.
func accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		slog.Info("http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", clientIP(r),
			"trace_id", TraceIDFromContext(r.Context()),
		)
	})
}

func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return v
	}
	return r.RemoteAddr
}
