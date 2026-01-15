package manager

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/gateway/manager/views"
	"anti2api-golang/refactor/internal/logger"
	"anti2api-golang/refactor/internal/pkg/id"
)

const sessionCookieName = "grok_admin_session"

func ManagerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check cookie
		if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value == "authenticated" {
			next.ServeHTTP(w, r)
			return
		}

		// If API request, return 401
		if strings.HasPrefix(r.URL.Path, "/manager/api") {
			http.Error(w, "未登录或会话已过期，请先登录管理面板", http.StatusUnauthorized)
			return
		}

		// Otherwise redirect to login
		// If it is the login page itself, don't redirect (handled by mux usually, but let's be safe if this is applied globally to /manager)
		// In our router we will apply this to /manager and others but not login.
		http.Redirect(w, r, "/login", http.StatusFound)
	})
}

func HandleLoginView(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect to manager
	if cookie, err := r.Cookie(sessionCookieName); err == nil && cookie.Value == "authenticated" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	views.Login("").Render(r.Context(), w)
}

func HandleLogin(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		views.Login("无效的请求").Render(r.Context(), w)
		return
	}

	// 如果未设置密码，拒绝登录
	adminPassword := config.Get().AdminPassword
	if adminPassword == "" {
		views.Login("管理密码未配置，请设置 WEBUI_PASSWORD 环境变量").Render(r.Context(), w)
		return
	}

	password := r.FormValue("password")
	if password == adminPassword {
		http.SetCookie(w, &http.Cookie{
			Name:     sessionCookieName,
			Value:    "authenticated",
			Path:     "/",
			HttpOnly: true,
			Expires:  time.Now().Add(24 * time.Hour),
		})
		// HTMX redirect
		w.Header().Set("HX-Redirect", "/")
		w.Write([]byte("登录成功"))
		return
	}

	views.Login("密码错误").Render(r.Context(), w)
}

func HandleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func findIndexBySessionID(id string) int {
	store := credential.GetStore()
	accounts := store.GetAll()
	for i, acc := range accounts {
		if acc.SessionID == id {
			return i
		}
	}
	return -1
}

func calculateStats(accounts []credential.Account) map[string]int {
	total := len(accounts)
	active := 0
	expired := 0
	now := time.Now().UnixMilli()

	for _, acc := range accounts {
		if acc.Enable && !acc.IsExpired(now) {
			active++
		} else {
			expired++
		}
	}

	return map[string]int{
		"total":   total,
		"active":  active,
		"expired": expired,
	}
}

func HandleDashboard(w http.ResponseWriter, r *http.Request) {
	store := credential.GetStore()
	accounts := store.GetAll()
	stats := calculateStats(accounts)
	views.Dashboard(accounts, stats).Render(r.Context(), w)
}

func HandleStats(w http.ResponseWriter, r *http.Request) {
	store := credential.GetStore()
	accounts := store.GetAll()
	stats := calculateStats(accounts)
	views.StatsCards(stats).Render(r.Context(), w)
}

func HandleList(w http.ResponseWriter, r *http.Request) {
	store := credential.GetStore()
	accounts := store.GetAll()

	status := r.URL.Query().Get("status")

	filtered := make([]credential.Account, 0)
	now := time.Now().UnixMilli()

	for _, acc := range accounts {
		if status != "all" && status != "" {
			isExpired := acc.IsExpired(now)
			if status == "active" {
				if !acc.Enable || isExpired {
					continue
				}
			}
			if status == "expired" {
				if !isExpired {
					continue
				} // Show actual expired
			}
			if status == "disabled" && acc.Enable {
				continue
			}
		}
		filtered = append(filtered, acc)
	}
	// Reverse order to show newest first? Store appends to end.
	// Let's reverse
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}

	views.TokenList(filtered).Render(r.Context(), w)
}

func HandleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	idx := findIndexBySessionID(id)
	if idx != -1 {
		credential.GetStore().Delete(idx)
		w.Write([]byte(""))
	} else {
		http.Error(w, "未找到", http.StatusNotFound)
	}
}

func HandleToggle(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	idx := findIndexBySessionID(id)
	if idx != -1 {
		store := credential.GetStore()
		accounts := store.GetAll()
		newState := !accounts[idx].Enable
		store.SetEnable(idx, newState)

		// Return updated card
		// Need to refetch account because SetEnable modifies store but we have a copy `accounts`
		// Actually better to get it fresh
		updatedAccounts := store.GetAll()
		if idx < len(updatedAccounts) { // Safety check
			views.TokenCard(updatedAccounts[idx]).Render(r.Context(), w)
		}
	}
}

func HandleRefresh(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	idx := findIndexBySessionID(id)
	if idx != -1 {
		store := credential.GetStore()
		err := store.RefreshAccount(idx)

		if err != nil {
			logger.Error("刷新失败：%v", err)
		}

		updatedAccounts := store.GetAll()
		if idx < len(updatedAccounts) {
			views.TokenCard(updatedAccounts[idx]).Render(r.Context(), w)
		}
	}
}

func HandleRefreshAll(w http.ResponseWriter, r *http.Request) {
	store := credential.GetStore()
	_, _ = store.RefreshAll()

	w.Header().Set("HX-Trigger", "refreshStats, refreshList")
	w.Write([]byte(""))
}

func HandleOAuthURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}

	state, err := credential.GenerateState()
	if err != nil {
		logger.Error("生成 OAuth state 失败：%v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "生成 OAuth state 失败"})
		return
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/oauth-callback", config.Get().OAuthRedirectPort)
	authURL := credential.BuildAuthURL(redirectURI, state)

	writeJSON(w, http.StatusOK, map[string]any{"url": authURL})
}

func HandleOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1.0" />
  <title>OAuth 授权回调</title>
  <script src="https://cdn.tailwindcss.com"></script>
</head>
<body class="bg-slate-50 text-slate-900 min-h-screen flex items-center justify-center p-6">
  <div class="w-full max-w-xl bg-white rounded-2xl border border-slate-100 shadow-sm p-6">
    <h1 class="text-xl font-bold text-slate-900 mb-2">授权回调已到达本地</h1>
    <p class="text-sm text-slate-600 mb-4">请复制当前浏览器地址栏中的完整 URL（包含 code 与 state），回到管理面板粘贴并提交。</p>
    <div class="bg-slate-50 border border-slate-200 rounded-lg p-3 text-xs text-slate-600 break-all" id="fullUrl"></div>
    <div class="flex gap-3 mt-4">
      <button class="px-4 py-2 bg-blue-600 text-white rounded-lg hover:bg-blue-700" id="copyBtn">一键复制完整 URL</button>
      <a class="px-4 py-2 bg-white border border-slate-200 text-slate-700 rounded-lg hover:bg-slate-50" href="/">返回管理面板</a>
    </div>
    <p class="text-xs text-slate-400 mt-4">提示：本页面不会自动提交授权信息，必须手动复制 URL 回到管理面板。</p>
  </div>
  <script>
    const full = window.location.href;
    document.getElementById('fullUrl').textContent = full;
    document.getElementById('copyBtn').addEventListener('click', async () => {
      try {
        await navigator.clipboard.writeText(full);
        alert('已复制');
      } catch (e) {
        alert('复制失败，请手动复制地址栏');
      }
    });
  </script>
</body>
</html>`))
}

type oauthParseURLRequest struct {
	URL                  string `json:"url"`
	CustomProjectID      string `json:"customProjectId"`
	AllowRandomProjectID bool   `json:"allowRandomProjectId"`
}

func HandleOAuthParseURL(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "不支持的请求方法", http.StatusMethodNotAllowed)
		return
	}

	var req oauthParseURLRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体不是有效的 JSON"})
		return
	}

	pastedURL := strings.TrimSpace(req.URL)
	if pastedURL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请粘贴回调 URL"})
		return
	}

	code, state, err := credential.ParseOAuthURL(pastedURL)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	if strings.TrimSpace(state) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "回调 URL 中缺少 state 参数"})
		return
	}
	if !credential.ValidateState(state) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "state 校验失败或已过期，请重新发起 OAuth 授权"})
		return
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/oauth-callback", config.Get().OAuthRedirectPort)

	logger.Info("开始 OAuth 交换 Token...")
	tokenResp, err := credential.ExchangeCodeForToken(code, redirectURI)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}

	email := ""
	if tokenResp.AccessToken != "" {
		if ui, err := credential.GetUserInfo(tokenResp.AccessToken); err == nil && ui != nil {
			email = strings.TrimSpace(ui.Email)
		} else if err != nil {
			logger.Warn("获取用户邮箱失败：%v", err)
		}
	}

	projectID := strings.TrimSpace(req.CustomProjectID)
	if projectID != "" {
		logger.Info("使用用户自定义项目ID：%s", projectID)
	} else if tokenResp.AccessToken != "" {
		if pid, err := credential.FetchProjectID(tokenResp.AccessToken); err == nil {
			projectID = strings.TrimSpace(pid)
			if projectID != "" {
				logger.Info("自动获取到项目ID：%s", projectID)
			}
		} else {
			logger.Warn("自动获取项目ID失败：%v", err)
		}
	}

	if projectID == "" && !req.AllowRandomProjectID {
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error": "无法自动获取 Google 项目 ID，可能会导致部分接口 403。请填写自定义项目ID，或勾选“允许使用随机项目ID”。",
		})
		return
	}
	if projectID == "" && req.AllowRandomProjectID {
		projectID = id.ProjectID()
		logger.Info("使用随机生成的项目ID：%s", projectID)
	}

	now := time.Now()
	account := credential.Account{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresIn:    tokenResp.ExpiresIn,
		Timestamp:    now.UnixMilli(),
		ProjectID:    projectID,
		Email:        email,
		Enable:       true,
		CreatedAt:    now,
	}

	if err := credential.GetStore().Add(account); err != nil {
		logger.Error("保存账号失败：%v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "保存账号失败"})
		return
	}

	logger.Info("OAuth 登录成功：%s", email)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
