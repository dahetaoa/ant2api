package credential

import "time"

type Account struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int       `json:"expires_in"`
	Timestamp    int64     `json:"timestamp"`
	ProjectID    string    `json:"projectId,omitempty"`
	Email        string    `json:"email,omitempty"`
	Enable       bool      `json:"enable"`
	CreatedAt    time.Time `json:"created_at"`
	SessionID    string    `json:"-"`
}

func (a *Account) IsExpired(nowMs int64) bool {
	if a.Timestamp == 0 || a.ExpiresIn == 0 {
		return true
	}
	expiresAt := a.Timestamp + int64(a.ExpiresIn*1000)
	return nowMs >= expiresAt-300000
}

