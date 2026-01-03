package middleware

import (
	"net/http"

	"anti2api-golang/refactor/internal/logger"
)

func Recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				logger.Error("panic: %v", v)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"error":{"message":"服务器内部错误，请查看服务端日志。","type":"server_error"}}`))
			}
		}()
		next.ServeHTTP(w, r)
	})
}
