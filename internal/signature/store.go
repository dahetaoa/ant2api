package signature

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"anti2api-golang/refactor/internal/pkg/cachefile"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type Store struct {
	cacheDir string
	cache    *LRU

	mu      sync.Mutex
	queue   chan Entry
	stopCh  chan struct{}
	stopped bool

	hotMu         sync.RWMutex
	hotByKey      map[string]Entry
	hotByToolCall map[string]string

	writerMu   sync.RWMutex
	writerDate string
	writer     *cachefile.Writer

	readers *cachefile.Manager
}

func NewStore(dataDir string, cache *LRU) *Store {
	cacheDir := filepath.Join(dataDir, "signatures")
	return &Store{
		cacheDir:      cacheDir,
		cache:         cache,
		queue:         make(chan Entry, 1024),
		stopCh:        make(chan struct{}),
		hotByKey:      make(map[string]Entry, 1024),
		hotByToolCall: make(map[string]string, 1024),
		readers:       cachefile.NewCacheManager(cacheDir),
	}
}

func (s *Store) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	go s.loop()
}

func (s *Store) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	s.stopped = true
	close(s.stopCh)
}

func (s *Store) Enqueue(e Entry) {
	select {
	case <-s.stopCh:
		return
	case s.queue <- e:
	}
}

func (s *Store) PutHot(e Entry) {
	key := e.Key()
	if key == "" || e.ToolCallID == "" || e.Signature == "" {
		return
	}

	s.hotMu.Lock()
	s.hotByKey[key] = e
	s.hotByToolCall[e.ToolCallID] = key
	s.hotMu.Unlock()
}

func (s *Store) loop() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var batch []Entry
	flushBlocked := false
	flush := func() {
		if len(batch) == 0 {
			flushBlocked = false
			return
		}

		persisted, err := s.persist(batch)
		if persisted > 0 {
			clear(batch[:persisted])
			batch = batch[persisted:]
			if len(batch) == 0 {
				batch = nil
			}
		}

		if err != nil {
			flushBlocked = true
			return
		}
		flushBlocked = false
	}

	for {
		readCh := s.queue
		if flushBlocked {
			readCh = nil
		}
		select {
		case <-s.stopCh:
			for {
				select {
				case e := <-s.queue:
					batch = append(batch, e)
				default:
					if len(batch) > 0 {
						_, _ = s.persist(batch)
						clear(batch)
						batch = nil
					}
					s.writerMu.Lock()
					if s.writer != nil {
						_ = s.writer.Close()
						s.writer = nil
						s.writerDate = ""
					}
					s.writerMu.Unlock()
					_ = s.readers.Close()
					return
				}
			}
		case e := <-readCh:
			batch = append(batch, e)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func marshalEntryJSON(e Entry) ([]byte, error) {
	b, err := jsonpkg.Marshal(e)
	if err == nil {
		return b, nil
	}
	b, err2 := json.Marshal(e)
	if err2 == nil {
		return b, nil
	}
	return nil, errors.Join(err, err2)
}

func (s *Store) LoadRecent(days int) {
	if days <= 0 {
		days = 1
	}

	entries, err := os.ReadDir(s.cacheDir)
	if err != nil {
		return
	}

	var files []string
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".idx") {
			continue
		}
		files = append(files, filepath.Join(s.cacheDir, name))
	}
	sort.Strings(files)
	if len(files) > days {
		files = files[len(files)-days:]
	}

	for _, fp := range files {
		date, ok := strings.CutSuffix(filepath.Base(fp), ".idx")
		if !ok || date == "" {
			continue
		}
		_ = cachefile.ScanIndex(fp, func(recordID string, _ cachefile.IndexEntry) error {
			requestID, toolCallID, ok := splitRecordID(recordID)
			if !ok {
				return nil
			}
			s.cache.Put(EntryIndex{
				RequestID:  requestID,
				ToolCallID: toolCallID,
				Date:       date,
			})
			return nil
		})
	}
}

func (s *Store) LoadByIndex(idx EntryIndex) (Entry, bool) {
	key := idx.Key()
	if key == "" || idx.ToolCallID == "" {
		return Entry{}, false
	}

	if idx.Date == "" {
		s.hotMu.RLock()
		e, ok := s.hotByKey[key]
		s.hotMu.RUnlock()
		if !ok || e.Signature == "" {
			return Entry{}, false
		}
		if idx.SignaturePrefix != "" && !strings.HasPrefix(e.Signature, idx.SignaturePrefix) {
			return Entry{}, false
		}
		return e, true
	}

	payload, err := s.loadRecord(idx.Date, key)
	if err != nil || len(payload) == 0 {
		return Entry{}, false
	}
	var e Entry
	if err := jsonpkg.Unmarshal(payload, &e); err != nil {
		return Entry{}, false
	}
	if e.Signature == "" || e.RequestID == "" || e.ToolCallID == "" {
		return Entry{}, false
	}
	if idx.SignaturePrefix != "" && !strings.HasPrefix(e.Signature, idx.SignaturePrefix) {
		return Entry{}, false
	}
	return e, true
}

func (s *Store) loadRecord(date string, recordID string) ([]byte, error) {
	s.writerMu.RLock()
	w := s.writer
	wDate := s.writerDate
	s.writerMu.RUnlock()
	if w != nil && wDate == date {
		return w.Load(recordID)
	}

	r, err := s.readers.GetReader(date)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.Load(recordID)
}

func splitRecordID(recordID string) (requestID string, toolCallID string, ok bool) {
	requestID, toolCallID, ok = strings.Cut(recordID, ":")
	if !ok || requestID == "" || toolCallID == "" {
		return "", "", false
	}
	return requestID, toolCallID, true
}

func (s *Store) persist(entries []Entry) (int, error) {
	if len(entries) == 0 {
		return 0, nil
	}

	date := time.Now().Format("2006-01-02")
	w, err := s.getWriter(date)
	if err != nil {
		return 0, err
	}

	var writeErr error
	var persisted int

	for _, e := range entries {
		recordID := e.Key()
		if recordID == "" {
			continue
		}
		b, err := marshalEntryJSON(e)
		if err != nil {
			writeErr = err
			break
		}
		if err := w.Write(recordID, b); err != nil {
			writeErr = err
			break
		}

		idx := EntryIndex{
			RequestID:       e.RequestID,
			ToolCallID:      e.ToolCallID,
			Model:           e.Model,
			CreatedAt:       e.CreatedAt,
			LastAccess:      e.LastAccess,
			SignaturePrefix: signaturePrefix(e.Signature),
			Date:            date,
		}
		s.cache.Put(idx)

		key := idx.Key()
		if key != "" {
			s.hotMu.Lock()
			if cur, ok := s.hotByKey[key]; ok && cur.CreatedAt.Equal(idx.CreatedAt) {
				delete(s.hotByKey, key)
				if idx.ToolCallID != "" {
					if mappedKey, ok := s.hotByToolCall[idx.ToolCallID]; ok && mappedKey == key {
						delete(s.hotByToolCall, idx.ToolCallID)
					}
				}
			}
			s.hotMu.Unlock()
		}

		persisted++
	}

	if writeErr != nil {
		return persisted, writeErr
	}
	return persisted, nil
}

func (s *Store) getWriter(date string) (*cachefile.Writer, error) {
	s.writerMu.Lock()
	defer s.writerMu.Unlock()

	if s.writer != nil && s.writerDate == date {
		return s.writer, nil
	}

	if s.writer != nil {
		_ = s.writer.Close()
		s.writer = nil
		s.writerDate = ""
	}

	w, err := cachefile.NewCacheWriter(s.cacheDir, date)
	if err != nil {
		return nil, err
	}
	s.writer = w
	s.writerDate = date
	return w, nil
}
