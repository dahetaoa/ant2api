package http

import (
	"net/http"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

// WriteOpenAIError 以 OpenAI 兼容的错误结构写入 JSON 响应。
// 注意：为保证兼容性，错误结构与当前实现保持一致。
func WriteOpenAIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"message":`))
	b, _ := jsonpkg.MarshalString(msg)
	_, _ = w.Write([]byte(b))
	_, _ = w.Write([]byte(`,"type":"server_error"}}`))
}

// WriteClaudeError 以 Claude/Anthropic 兼容的错误结构写入 JSON 响应。
// 注意：为保证兼容性，错误结构与当前实现保持一致。
func WriteClaudeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	encoded, _ := jsonpkg.MarshalString(msg)
	_, _ = w.Write([]byte(`{"type":"error","error":{"type":"api_error","message":` + encoded + `}}`))
}
