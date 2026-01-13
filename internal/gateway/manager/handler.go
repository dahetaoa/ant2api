package manager

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

    "anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/gateway/manager/views"
    "anti2api-golang/refactor/internal/logger"
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
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

    password := r.FormValue("password")
    if password == config.Get().AdminPassword {
        http.SetCookie(w, &http.Cookie{
            Name:     sessionCookieName,
            Value:    "authenticated",
            Path:     "/",
            HttpOnly: true,
            Expires:  time.Now().Add(24 * time.Hour),
        })
        // HTMX redirect
        w.Header().Set("HX-Redirect", "/")
        w.Write([]byte("Logged in"))
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
                if !acc.Enable || isExpired { continue }
            }
			if status == "expired" {
                if !isExpired { continue } // Show actual expired  
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

func HandleAdd(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	input := strings.TrimSpace(r.FormValue("token"))
	store := credential.GetStore()
    
    var addedCount int

	if strings.HasPrefix(input, "{") || strings.HasPrefix(input, "[") {
		// Try JSON
		var accounts []credential.Account
        // try single
        var single credential.Account
        if err := json.Unmarshal([]byte(input), &single); err == nil && single.AccessToken != "" {
            accounts = append(accounts, single)
        } else {
             _ = json.Unmarshal([]byte(input), &accounts)
        }
        
        for _, acc := range accounts {
            if acc.AccessToken != "" {
                acc.Enable = true
                if err := store.Add(acc); err == nil {
                    addedCount++
                }
            }
        }
	} else {
         // Try splitting by comma if multiple
         parts := strings.Split(input, ",")
         for _, p := range parts {
             p = strings.TrimSpace(p)
             if p == "" { continue }
             acc := credential.Account{
                 AccessToken: p,
                 Enable:      true,
                 CreatedAt:   time.Now(),
             }
             if err := store.Add(acc); err == nil {
                 addedCount++
             }
         }
    }
    
    if addedCount > 0 {
         w.Header().Set("HX-Trigger", `{"showMessage":{"message": "成功添加 ` +  string(rune(addedCount+'0')) + ` 个账号", "type": "success"}}`) // Hacky rune conversion for small numbers
    }

	// Return updated list
    // Use HandleList to render with filters if any were preserved? 
    // For simplicity, just return all sorted reverse
    HandleList(w, r)
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
             logger.Error("Refresh failed: %v", err)
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
