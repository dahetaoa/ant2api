package signature

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type Store struct {
	dataDir string
	cache   *LRU

	mu      sync.Mutex
	queue   chan Entry
	stopCh  chan struct{}
	stopped bool

	hotMu         sync.RWMutex
	hotByKey      map[string]Entry
	hotByToolCall map[string]string
}

func NewStore(dataDir string, cache *LRU) *Store {
	return &Store{
		dataDir:       dataDir,
		cache:         cache,
		queue:         make(chan Entry, 1024),
		stopCh:        make(chan struct{}),
		hotByKey:      make(map[string]Entry, 1024),
		hotByToolCall: make(map[string]string, 1024),
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
	case s.queue <- e:
	default:
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
	flush := func() {
		if len(batch) == 0 {
			return
		}
		_ = s.appendJSONL(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-s.stopCh:
			flush()
			return
		case e := <-s.queue:
			batch = append(batch, e)
			if len(batch) >= 256 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (s *Store) appendJSONL(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	dir := filepath.Join(s.dataDir, "signatures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	file := filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl")
	f, err := os.OpenFile(file, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return err
	}

	w := bufio.NewWriterSize(f, 64*1024)
	baseOffset := fi.Size()
	var written int64
	var persisted []EntryIndex

	for _, e := range entries {
		b, err := jsonpkg.Marshal(e)
		if err != nil {
			continue
		}
		offset := baseOffset + written
		if _, err := w.Write(b); err != nil {
			return err
		}
		if err := w.WriteByte('\n'); err != nil {
			return err
		}
		written += int64(len(b) + 1)

		persisted = append(persisted, EntryIndex{
			RequestID:  e.RequestID,
			ToolCallID: e.ToolCallID,
			Model:      e.Model,
			CreatedAt:  e.CreatedAt,
			LastAccess: e.LastAccess,
			FilePath:   file,
			Offset:     offset,
		})
	}

	if err := w.Flush(); err != nil {
		return err
	}

	for _, idx := range persisted {
		s.cache.Put(idx)
		key := idx.Key()
		if key == "" {
			continue
		}
		s.hotMu.Lock()
		delete(s.hotByKey, key)
		if idx.ToolCallID != "" {
			delete(s.hotByToolCall, idx.ToolCallID)
		}
		s.hotMu.Unlock()
	}

	return nil
}

func (s *Store) LoadRecent(days int) {
	if days <= 0 {
		days = 1
	}

	dir := filepath.Join(s.dataDir, "signatures")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	var files []string
	for _, de := range entries {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	if len(files) > days {
		files = files[len(files)-days:]
	}

	for _, fp := range files {
		s.loadFile(fp)
	}
}

func (s *Store) loadFile(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 8*1024*1024)

	var offset int64
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		lineOffset := offset
		offset += int64(len(scanner.Bytes())) + 1
		if len(line) == 0 {
			continue
		}
		idx, ok := parseEntryIndexFromJSONLine(line, path, lineOffset)
		if !ok {
			continue
		}
		s.cache.Put(idx)
	}
}

func (s *Store) LoadEntryAt(filePath string, offset int64) (Entry, bool) {
	if filePath == "" || offset < 0 {
		return Entry{}, false
	}
	f, err := os.Open(filePath)
	if err != nil {
		return Entry{}, false
	}
	defer f.Close()

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return Entry{}, false
	}

	r := bufio.NewReaderSize(f, 64*1024)
	line, err := r.ReadBytes('\n')
	if err != nil && err != io.EOF {
		return Entry{}, false
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return Entry{}, false
	}
	var e Entry
	if err := jsonpkg.Unmarshal(line, &e); err != nil {
		return Entry{}, false
	}
	if e.Signature == "" || e.RequestID == "" || e.ToolCallID == "" {
		return Entry{}, false
	}
	return e, true
}

func (s *Store) LoadByIndex(idx EntryIndex) (Entry, bool) {
	key := idx.Key()
	if key == "" || idx.ToolCallID == "" {
		return Entry{}, false
	}

	if idx.FilePath == "" || idx.Offset < 0 {
		s.hotMu.RLock()
		e, ok := s.hotByKey[key]
		s.hotMu.RUnlock()
		if !ok || e.Signature == "" {
			return Entry{}, false
		}
		return e, true
	}

	return s.LoadEntryAt(idx.FilePath, idx.Offset)
}

func parseEntryIndexFromJSONLine(line []byte, filePath string, offset int64) (EntryIndex, bool) {
	requestID, ok := extractJSONStringField(line, "requestID")
	if !ok || requestID == "" {
		return EntryIndex{}, false
	}
	toolCallID, ok := extractJSONStringField(line, "toolCallID")
	if !ok || toolCallID == "" {
		return EntryIndex{}, false
	}

	var idx EntryIndex
	idx.RequestID = requestID
	idx.ToolCallID = toolCallID
	idx.FilePath = filePath
	idx.Offset = offset

	if model, ok := extractJSONStringField(line, "model"); ok {
		idx.Model = model
	}

	if createdAt, ok := extractJSONStringField(line, "createdAt"); ok {
		if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
			idx.CreatedAt = t
		}
	}

	if lastAccess, ok := extractJSONStringField(line, "lastAccess"); ok {
		if t, err := time.Parse(time.RFC3339Nano, lastAccess); err == nil {
			idx.LastAccess = t
		}
	}

	return idx, true
}

func extractJSONStringField(line []byte, field string) (string, bool) {
	pat := []byte(`"` + field + `":`)
	i := bytes.Index(line, pat)
	if i < 0 {
		return "", false
	}
	j := i + len(pat)
	for j < len(line) && (line[j] == ' ' || line[j] == '\t') {
		j++
	}
	if j >= len(line) || line[j] != '"' {
		return "", false
	}
	j++
	start := j
	for j < len(line) {
		if line[j] == '\\' {
			j += 2
			continue
		}
		if line[j] == '"' {
			raw := line[start:j]
			unquoted, err := strconv.Unquote(`"` + string(raw) + `"`)
			if err != nil {
				return string(raw), true
			}
			return unquoted, true
		}
		j++
	}
	return "", false
}
