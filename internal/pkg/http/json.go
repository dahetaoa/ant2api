package http

import (
	"net/http"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

// WriteJSON 将 v 以 JSON 写入响应体，并设置状态码与 Content-Type。
// 该方法使用项目统一的 JSON 编码器（sonic），以保持性能与输出一致性。
func WriteJSON(w http.ResponseWriter, status int, v any) {
	b, err := jsonpkg.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
