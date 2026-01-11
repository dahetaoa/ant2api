package signature

import (
	"sync"
	"time"

	"anti2api-golang/refactor/internal/config"
)

type Manager struct {
	cache *LRU
	store *Store
}

var (
	managerOnce sync.Once
	managerInst *Manager
)

func GetManager() *Manager {
	managerOnce.Do(func() {
		cfg := config.Get()
		cache := NewLRU(50_000)
		store := NewStore(cfg.DataDir, cache)
		store.LoadRecent(3)
		store.Start()
		managerInst = &Manager{cache: cache, store: store}
	})
	return managerInst
}

func (m *Manager) Save(requestID, toolCallID, signature, reasoning, model string) {
	if requestID == "" || toolCallID == "" || signature == "" {
		return
	}

	now := time.Now()
	e := Entry{
		Signature:  signature,
		Reasoning:  reasoning,
		RequestID:  requestID,
		ToolCallID: toolCallID,
		Model:      model,
		CreatedAt:  now,
		LastAccess: now,
	}

	m.cache.Put(e)
	m.store.Enqueue(e)
}

func (m *Manager) Lookup(requestID, toolCallID string) (Entry, bool) {
	e, ok := m.cache.Get(requestID, toolCallID)
	if !ok || e.Signature == "" {
		return Entry{}, false
	}
	return e, true
}

func (m *Manager) LookupByToolCallID(toolCallID string) (Entry, bool) {
	e, ok := m.cache.GetByToolCallID(toolCallID)
	if !ok || e.Signature == "" {
		return Entry{}, false
	}
	return e, true
}
