package gateway

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"anti2api-golang/refactor/internal/gateway/claude"
	"anti2api-golang/refactor/internal/gateway/gemini"
	"anti2api-golang/refactor/internal/gateway/manager"
	"anti2api-golang/refactor/internal/gateway/openai"
	"anti2api-golang/refactor/internal/middleware"
)

func NewRouter() http.Handler {
	mux := http.NewServeMux()

	// NOTE: Keep routing compatible with Go 1.21's ServeMux behavior.
	mux.HandleFunc("/health", allowMethods(handleHealth, http.MethodGet, http.MethodHead))

	// Shared path between OpenAI and Anthropic-compatible clients; select response format by headers.
	mux.HandleFunc("/v1/models", allowMethods(handleListModels, http.MethodGet, http.MethodHead))
	mux.HandleFunc("/v1/chat/completions", allowMethods(openai.HandleChatCompletions, http.MethodPost))
	mux.HandleFunc("/v1/chat/completions/", allowMethods(openai.HandleChatCompletions, http.MethodPost))

	mux.HandleFunc("/v1/messages", allowMethods(claude.HandleMessages, http.MethodPost))
	mux.HandleFunc("/v1/messages/count_tokens", allowMethods(claude.HandleCountTokens, http.MethodPost))

	// Gemini endpoints include a variable model segment.
	mux.HandleFunc("/v1beta/models/", gemini.HandleModels)
	// Provide a stable non-redirect entrypoint for list.
	mux.HandleFunc("/v1beta/models", allowMethods(gemini.HandleListModels, http.MethodGet, http.MethodHead))

	// Manager UI & API
	// Public Login
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			manager.HandleLogin(w, r)
		} else {
			manager.HandleLoginView(w, r)
		}
	})
	mux.HandleFunc("/logout", manager.HandleLogout)

	// Public OAuth callback (no auth)
	mux.HandleFunc("/oauth-callback", allowMethods(manager.HandleOAuthCallback, http.MethodGet, http.MethodHead))

	// Protected Manager Routes
	// We use a separate mux for manager routes to wrap them in ManagerAuth
	// However, since we want to mount it at root "/", we must be careful not to shadow /v1 routes
	// But ServeMux uses longest match, so /v1 will still take precedence over /

	// We can't mount a handler at "/" AND have other handlers at /v1 on the *same* mux easily if we modify the handler for "/"
	// Wait, mux.Handle("/", ...) works as catch-all.

	managerMux := http.NewServeMux()
	managerMux.HandleFunc("/", manager.HandleDashboard)
	managerMux.HandleFunc("/manager/api/list", manager.HandleList)
	managerMux.HandleFunc("/manager/api/stats", manager.HandleStats)
	managerMux.HandleFunc("/manager/api/delete", manager.HandleDelete)
	managerMux.HandleFunc("/manager/api/toggle", manager.HandleToggle)
	managerMux.HandleFunc("/manager/api/refresh", manager.HandleRefresh)
	managerMux.HandleFunc("/manager/api/refresh_all", manager.HandleRefreshAll)
	managerMux.HandleFunc("/manager/api/quota", manager.HandleQuota)
	managerMux.HandleFunc("/manager/api/quota/all", manager.HandleQuotaAll)
	managerMux.HandleFunc("/manager/api/oauth/url", manager.HandleOAuthURL)
	managerMux.HandleFunc("/manager/api/oauth/parse-url", manager.HandleOAuthParseURL)
	managerMux.HandleFunc("/manager/api/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			manager.HandleSettingsPost(w, r)
		} else {
			manager.HandleSettingsGet(w, r)
		}
	})

	// Mount the protected manager logic at root
	mux.Handle("/", manager.ManagerAuth(managerMux))

	h := middleware.Recovery(mux)
	h = middleware.Logging(h)

	base := h
	authed := middleware.Auth(base)
	// OAuth callback 必须公开可访问：即使配置了 API_KEY，也不应该拦截。
	h = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/oauth-callback" {
			base.ServeHTTP(w, r)
			return
		}
		authed.ServeHTTP(w, r)
	})

	return h
}

func handleListModels(w http.ResponseWriter, r *http.Request) {
	// Anthropic SDKs typically include this header; prefer Anthropic format when present.
	if strings.TrimSpace(r.Header.Get("anthropic-version")) != "" || strings.TrimSpace(r.Header.Get("anthropic-beta")) != "" {
		claude.HandleListModels(w, r)
		return
	}
	openai.HandleListModels(w, r)
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
