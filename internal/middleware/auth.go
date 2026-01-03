package middleware

import (
	"net/http"
	"strings"

	"anti2api-golang/refactor/internal/config"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
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

		if key == "" {
			writeUnauthorized(w, r, "缺少 API_KEY：请在请求头 x-api-key / x-goog-api-key，或 Authorization: Bearer <key> 中提供。", "missing_api_key")
			return
		}
		if key != cfg.APIKey {
			writeUnauthorized(w, r, "API_KEY 无效或不匹配：请确认客户端传入的 key 与服务端配置的 API_KEY 一致。", "invalid_api_key")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeUnauthorized(w http.ResponseWriter, r *http.Request, msg string, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	encodedMsg, _ := jsonpkg.MarshalString(msg)
	encodedCode, _ := jsonpkg.MarshalString(code)

	path := r.URL.Path
	switch {
	case strings.HasPrefix(path, "/v1/messages"):
		_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":` + encodedMsg + `}}`))
	case strings.HasPrefix(path, "/v1beta/"):
		_, _ = w.Write([]byte(`{"error":{"message":` + encodedMsg + `}}`))
	default:
		_, _ = w.Write([]byte(`{"error":{"message":` + encodedMsg + `,"type":"invalid_request_error","code":` + encodedCode + `}}`))
	}
}
