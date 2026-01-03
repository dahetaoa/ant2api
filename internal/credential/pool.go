package credential

import (
	"errors"
	"time"
)

type Pool struct {
	store *Store
}

func NewPool(store *Store) *Pool {
	if store == nil {
		store = GetStore()
	}
	return &Pool{store: store}
}

func (p *Pool) Get() (*Account, error) {
	return p.store.GetToken()
}

func (p *Pool) GetForProject(projectID string) (*Account, error) {
	if projectID == "" {
		return nil, errors.New("projectId is required")
	}
	return p.store.GetTokenByProjectID(projectID)
}

func (p *Pool) RefreshIfNeeded(account *Account) error {
	if account == nil {
		return errors.New("account is nil")
	}
	if !account.Enable {
		return errors.New("account disabled")
	}
	if !account.IsExpired(time.Now().UnixMilli()) {
		return nil
	}
	return RefreshToken(account)
}

