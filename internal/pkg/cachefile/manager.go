package cachefile

import (
	"errors"
	"os"
	"sync"
	"time"
)

type Manager struct {
	baseDir string
	maxOpen int

	mu      sync.Mutex
	closed  bool
	readers map[string]*readerSlot
}

type readerSlot struct {
	r        *Reader
	refs     int
	lastUsed time.Time
}

type readerHandle struct {
	mgr    *Manager
	date   string
	slot   *readerSlot
	closed bool
}

func NewCacheManager(baseDir string) *Manager {
	return &Manager{
		baseDir: baseDir,
		maxOpen: 16,
		readers: make(map[string]*readerSlot, 16),
	}
}

func (m *Manager) GetReader(date string) (CacheReader, error) {
	if err := validateDate(date); err != nil {
		return nil, err
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, errors.New("cache manager closed")
	}

	now := time.Now()
	slot, ok := m.readers[date]
	if ok && slot.r != nil {
		slot.refs++
		slot.lastUsed = now
		h := &readerHandle{mgr: m, date: date, slot: slot}
		toClose := m.evictLocked(now)
		m.mu.Unlock()
		for _, r := range toClose {
			_ = r.Close()
		}
		return h, nil
	}

	r, err := NewCacheReader(m.baseDir, date)
	if err != nil {
		m.mu.Unlock()
		return nil, err
	}
	slot = &readerSlot{r: r, refs: 1, lastUsed: now}
	m.readers[date] = slot
	h := &readerHandle{mgr: m, date: date, slot: slot}
	toClose := m.evictLocked(now)
	m.mu.Unlock()

	for _, r := range toClose {
		_ = r.Close()
	}
	return h, nil
}

func (m *Manager) DeleteDate(date string) error {
	if err := validateDate(date); err != nil {
		return err
	}

	datPath, idxPath := paths(m.baseDir, date)

	var toClose *Reader

	m.mu.Lock()
	if slot, ok := m.readers[date]; ok {
		toClose = slot.r
		delete(m.readers, date)
	}
	m.mu.Unlock()

	if toClose != nil {
		_ = toClose.Close()
	}

	var errs []error
	if err := os.Remove(datPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if err := os.Remove(idxPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	slots := m.readers
	m.readers = nil
	m.mu.Unlock()

	var errs []error
	for _, slot := range slots {
		if slot == nil || slot.r == nil {
			continue
		}
		if err := slot.r.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (h *readerHandle) Exists(recordID string) bool {
	if h == nil {
		return false
	}
	h.mgr.mu.Lock()
	if h.closed || h.mgr.closed {
		h.mgr.mu.Unlock()
		return false
	}
	r := h.slot.r
	h.slot.lastUsed = time.Now()
	h.mgr.mu.Unlock()
	if r == nil {
		return false
	}
	return r.Exists(recordID)
}

func (h *readerHandle) Load(recordID string) ([]byte, error) {
	if h == nil {
		return nil, ErrNotFound
	}
	h.mgr.mu.Lock()
	if h.closed || h.mgr.closed {
		h.mgr.mu.Unlock()
		return nil, errors.New("cache reader closed")
	}
	r := h.slot.r
	h.slot.lastUsed = time.Now()
	h.mgr.mu.Unlock()
	if r == nil {
		return nil, errors.New("cache reader closed")
	}
	return r.Load(recordID)
}

func (h *readerHandle) Close() error {
	if h == nil {
		return nil
	}

	var toClose []*Reader
	now := time.Now()

	h.mgr.mu.Lock()
	if h.closed {
		h.mgr.mu.Unlock()
		return nil
	}
	h.closed = true
	if h.slot.refs > 0 {
		h.slot.refs--
	}
	h.slot.lastUsed = now
	toClose = h.mgr.evictLocked(now)
	h.mgr.mu.Unlock()

	for _, r := range toClose {
		_ = r.Close()
	}
	return nil
}

func (m *Manager) evictLocked(now time.Time) []*Reader {
	if m.maxOpen <= 0 || len(m.readers) <= m.maxOpen {
		return nil
	}

	var toClose []*Reader
	for len(m.readers) > m.maxOpen {
		var oldestDate string
		var oldestTime time.Time
		for date, slot := range m.readers {
			if slot == nil || slot.r == nil || slot.refs != 0 {
				continue
			}
			if oldestDate == "" || slot.lastUsed.Before(oldestTime) {
				oldestDate = date
				oldestTime = slot.lastUsed
			}
		}
		if oldestDate == "" {
			break
		}
		slot := m.readers[oldestDate]
		delete(m.readers, oldestDate)
		if slot != nil && slot.r != nil {
			toClose = append(toClose, slot.r)
		}
	}
	return toClose
}
