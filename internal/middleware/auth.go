package middleware

import (
	"net/http"
	"strings"

	"anti2api-golang/refactor/internal/config"
)

func Auth(next http.Handler) http.Handler {
	cfg := config.Get()
	if cfg.APIKey == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Keep health endpoint accessible for liveness checks.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		key := ""
		if v := r.Header.Get("x-api-key"); v != "" {
			key = v
		}
		if key == "" {
			if v := r.Header.Get("x-goog-api-key"); v != "" {
				key = v
			}
		}
		if key == "" {
			auth := strings.TrimSpace(r.Header.Get("Authorization"))
			// Support both "Bearer sk-xxx" and raw "sk-xxx" (matches original behavior).
			lower := strings.ToLower(auth)
			if strings.HasPrefix(lower, "bearer ") {
				key = strings.TrimSpace(auth[7:])
			} else if auth != "" {
				key = auth
			}
		}
		if key == "" {
			key = strings.TrimSpace(r.URL.Query().Get("key"))
		}
		if key != cfg.APIKey {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"unauthorized","type":"invalid_request_error"}}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
