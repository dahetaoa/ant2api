package middleware

import (
	"net/http"
	"time"

	"anti2api-golang/refactor/internal/logger"
)

func Logging(next http.Handler) http.Handler {
	level := logger.GetLevel()
	if level == logger.LogOff {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Match original behavior: request line log (e.g. [GET] /v1/models ...)
		// is emitted after handler completes, and only the handler prints
		// client/backend request/response blocks.
		if r.URL.Path == "/favicon.ico" {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(sw, r)
		logger.Request(r.Method, r.URL.Path, sw.statusCode, time.Since(start))
	})
}

// statusWriter captures status codes, matching the original project's middleware.
type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
