package credential

import (
	"time"

	"anti2api-golang/refactor/internal/logger"
)

// StartAutoRefresh 启动后台自动刷新任务
// 每 30 分钟检查一次，刷新即将过期（5分钟内）的 token
func StartAutoRefresh() {
	go func() {
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()

		logger.Info("自动刷新任务已启动，每 30 分钟检查一次")

		for range ticker.C {
			refreshExpiring()
		}
	}()
}

// refreshExpiring 刷新即将过期的账号
func refreshExpiring() {
	store := GetStore()
	store.mu.Lock()
	defer store.mu.Unlock()

	nowMs := time.Now().UnixMilli()
	refreshed := 0
	failed := 0

	for i := range store.accounts {
		account := &store.accounts[i]
		if !account.Enable {
			continue
		}

		// 检查是否即将过期（5分钟内）或已过期
		if account.IsExpired(nowMs) {
			if err := RefreshToken(account); err != nil {
				logger.Warn("自动刷新失败 [%s]: %v", account.Email, err)
				failed++
			} else {
				logger.Info("自动刷新成功 [%s]", account.Email)
				refreshed++
			}
		}
	}

	if refreshed > 0 || failed > 0 {
		_ = store.saveUnlocked()
		logger.Info("自动刷新完成: 成功 %d, 失败 %d", refreshed, failed)
	}
}
