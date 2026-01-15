package manager

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"anti2api-golang/refactor/internal/credential"
)

const (
	quotaCacheTTL       = 2 * time.Minute
	quotaErrorCacheTTL  = 30 * time.Second
	quotaFetchTimeout   = 20 * time.Second
	quotaMaxConcurrency = 4
)

type quotaCacheEntry struct {
	quota     *AccountQuota
	err       error
	expiresAt time.Time
}

type quotaInflight struct {
	done  chan struct{}
	quota *AccountQuota
	err   error
}

var quotaState struct {
	mu       sync.Mutex
	cache    map[string]quotaCacheEntry
	inflight map[string]*quotaInflight
}

func getQuotaStateLocked() {
	if quotaState.cache == nil {
		quotaState.cache = make(map[string]quotaCacheEntry)
	}
	if quotaState.inflight == nil {
		quotaState.inflight = make(map[string]*quotaInflight)
	}
}

func InvalidateQuotaCache(sessionID string) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return
	}
	quotaState.mu.Lock()
	getQuotaStateLocked()
	delete(quotaState.cache, sessionID)
	quotaState.mu.Unlock()
}

func GetAccountQuotaCached(ctx context.Context, account credential.Account, force bool) (*AccountQuota, bool, error) {
	sessionID := strings.TrimSpace(account.SessionID)
	if sessionID == "" {
		q, err := fetchQuotaOnce(ctx, account)
		return q, false, err
	}

	now := time.Now()

	quotaState.mu.Lock()
	getQuotaStateLocked()

	if !force {
		if entry, ok := quotaState.cache[sessionID]; ok && now.Before(entry.expiresAt) {
			quota := entry.quota
			err := entry.err
			quotaState.mu.Unlock()
			return quota, true, err
		}
	}

	if inflight, ok := quotaState.inflight[sessionID]; ok {
		quotaState.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		case <-inflight.done:
			return inflight.quota, false, inflight.err
		}
	}

	inflight := &quotaInflight{done: make(chan struct{})}
	quotaState.inflight[sessionID] = inflight
	quotaState.mu.Unlock()

	quota, err := fetchQuotaOnce(ctx, account)

	quotaState.mu.Lock()
	getQuotaStateLocked()

	delete(quotaState.inflight, sessionID)

	cacheUntil := time.Time{}
	if err == nil {
		cacheUntil = time.Now().Add(quotaCacheTTL)
	} else {
		cacheUntil = time.Now().Add(quotaErrorCacheTTL)
	}
	quotaState.cache[sessionID] = quotaCacheEntry{quota: quota, err: err, expiresAt: cacheUntil}

	inflight.quota = quota
	inflight.err = err
	close(inflight.done)
	quotaState.mu.Unlock()

	return quota, false, err
}

func fetchQuotaOnce(ctx context.Context, account credential.Account) (*AccountQuota, error) {
	cctx, cancel := context.WithTimeout(ctx, quotaFetchTimeout)
	defer cancel()
	q, err := FetchAccountQuota(cctx, account)
	if err != nil && errors.Is(ctx.Err(), context.Canceled) {
		return nil, ctx.Err()
	}
	return q, err
}
