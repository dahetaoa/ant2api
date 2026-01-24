package signature

import (
	"strings"
	"sync"
	"time"

	"anti2api-golang/refactor/internal/config"
)

type Manager struct {
	cache *LRU
	store *Store
}

const defaultSignatureLRUCapacity = 50_000 // 默认签名索引缓存容量（LRU 条目数）。

var (
	managerOnce sync.Once
	managerInst *Manager
)

func GetManager() *Manager {
	managerOnce.Do(func() {
		cfg := config.Get()
		cache := NewLRU(defaultSignatureLRUCapacity)
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

	m.store.PutHot(e)
	m.cache.Put(EntryIndex{
		RequestID:       requestID,
		ToolCallID:      toolCallID,
		Model:           model,
		CreatedAt:       now,
		LastAccess:      now,
		SignaturePrefix: signaturePrefix(signature),
	})
	m.store.Enqueue(e)
}

func (m *Manager) Lookup(requestID, toolCallID string) (Entry, bool) {
	idx, ok := m.cache.Get(requestID, toolCallID)
	if !ok {
		return Entry{}, false
	}
	e, ok := m.store.LoadByIndex(idx)
	if !ok || e.Signature == "" {
		return Entry{}, false
	}
	e.LastAccess = idx.LastAccess
	return e, true
}

func (m *Manager) LookupByToolCallID(toolCallID string) (Entry, bool) {
	idx, ok := m.cache.GetByToolCallID(toolCallID)
	if !ok {
		return Entry{}, false
	}
	e, ok := m.store.LoadByIndex(idx)
	if !ok || e.Signature == "" {
		return Entry{}, false
	}
	e.LastAccess = idx.LastAccess
	return e, true
}

// LookupByToolCallIDAndSignaturePrefix expands a short signature prefix (index) into the full signature.
// It is designed for clients that persist only a small prefix of thoughtSignature to reduce payload size.
func (m *Manager) LookupByToolCallIDAndSignaturePrefix(toolCallID string, sigPrefix string) (Entry, bool) {
	sigPrefix = strings.TrimSpace(sigPrefix)
	if toolCallID == "" || sigPrefix == "" {
		return Entry{}, false
	}

	idx, ok := m.cache.GetByToolCallID(toolCallID)
	if !ok {
		return Entry{}, false
	}
	if idx.SignaturePrefix != "" && !strings.HasPrefix(idx.SignaturePrefix, sigPrefix) {
		return Entry{}, false
	}

	e, ok := m.store.LoadByIndex(idx)
	if !ok || e.Signature == "" {
		return Entry{}, false
	}
	if !strings.HasPrefix(e.Signature, sigPrefix) {
		return Entry{}, false
	}
	e.LastAccess = idx.LastAccess
	return e, true
}
