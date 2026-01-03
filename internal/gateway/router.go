package gateway

import (
	"context"
	"errors"
	"net/http"

	"anti2api-golang/refactor/internal/gateway/claude"
	"anti2api-golang/refactor/internal/gateway/gemini"
	"anti2api-golang/refactor/internal/gateway/openai"
	"anti2api-golang/refactor/internal/middleware"
)

func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// NOTE: Keep routing compatible with Go 1.21's ServeMux behavior.
	mux.HandleFunc("/health", allowMethods(handleHealth, http.MethodGet, http.MethodHead))

	mux.HandleFunc("/v1/models", allowMethods(openai.HandleListModels, http.MethodGet, http.MethodHead))
	mux.HandleFunc("/v1/chat/completions", allowMethods(openai.HandleChatCompletions, http.MethodPost))
	mux.HandleFunc("/v1/chat/completions/", allowMethods(openai.HandleChatCompletions, http.MethodPost))

	mux.HandleFunc("/v1/messages", allowMethods(claude.HandleMessages, http.MethodPost))
	mux.HandleFunc("/v1/messages/count_tokens", allowMethods(claude.HandleCountTokens, http.MethodPost))

	// Gemini endpoints include a variable model segment.
	mux.HandleFunc("/v1beta/models/", gemini.HandleModels)
	// Provide a stable non-redirect entrypoint for list.
	mux.HandleFunc("/v1beta/models", allowMethods(gemini.HandleListModels, http.MethodGet, http.MethodHead))

	h := middleware.Recovery(mux)
	h = middleware.Logging(h)
	h = middleware.Auth(h)

	return h
}

func allowMethods(h http.HandlerFunc, methods ...string) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(methods))
	for _, m := range methods {
		allowed[m] = struct{}{}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := allowed[r.Method]; ok {
			h(w, r)
			return
		}
		if errors.Is(r.Context().Err(), context.Canceled) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = w.Write([]byte(`{"error":{"message":"不支持的请求方法，请检查接口要求的 HTTP Method。","type":"invalid_request_error"}}`))
	}
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}
