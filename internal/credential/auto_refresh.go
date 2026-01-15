package credential

import (
	"time"

	"anti2api-golang/refactor/internal/logger"
)

// StartAutoRefresh 启动后台自动刷新任务
// 每分钟检查一次，在过期前5分钟自动刷新 token
func StartAutoRefresh() {
	go func() {
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		logger.Info("自动刷新任务已启动，每分钟检查一次")

		for range ticker.C {
			refreshExpiring()
		}
	}()
}

// refreshExpiring 刷新即将过期的账号（过期前5分钟）
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

		// 计算距离过期的剩余时间
		if account.Timestamp == 0 || account.ExpiresIn == 0 {
			continue
		}

		expiresAtMs := account.Timestamp + int64(account.ExpiresIn)*1000
		remainingMs := expiresAtMs - nowMs

		// 如果剩余时间在 0-5 分钟之间，则刷新
		if remainingMs > 0 && remainingMs <= 5*60*1000 {
			if err := RefreshToken(account); err != nil {
				logger.Warn("自动刷新失败 [%s]: %v", account.Email, err)
				failed++
			} else {
				logger.Info("自动刷新成功 [%s]，距过期还有 %.1f 分钟", account.Email, float64(remainingMs)/60000)
				refreshed++
			}
		}
	}

	if refreshed > 0 || failed > 0 {
		_ = store.saveUnlocked()
		logger.Info("自动刷新完成: 成功 %d, 失败 %d", refreshed, failed)
	}
}
