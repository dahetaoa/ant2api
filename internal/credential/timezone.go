package credential

import "time"

// 中国时区 (UTC+8)
var ChinaTimezone = time.FixedZone("CST", 8*3600)

// FormatExpiresAt 格式化过期时间为中国时间
func (a *Account) FormatExpiresAt() string {
	if a.Timestamp == 0 || a.ExpiresIn == 0 {
		return "-"
	}
	// Timestamp 是毫秒，ExpiresIn 是秒
	expiresAtMs := a.Timestamp + int64(a.ExpiresIn)*1000
	expiresAt := time.UnixMilli(expiresAtMs).In(ChinaTimezone)
	return expiresAt.Format("2006-01-02 15:04:05")
}

// FormatCreatedAt 格式化创建时间为中国时间
func (a *Account) FormatCreatedAt() string {
	if a.CreatedAt.IsZero() {
		return "-"
	}
	return a.CreatedAt.In(ChinaTimezone).Format("2006-01-02 15:04:05")
}
