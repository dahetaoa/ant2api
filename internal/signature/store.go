package signature

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
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
}

func NewStore(dataDir string, cache *LRU) *Store {
	return &Store{
		dataDir: dataDir,
		cache:   cache,
		queue:   make(chan Entry, 1024),
		stopCh:  make(chan struct{}),
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

	w := bufio.NewWriterSize(f, 64*1024)
	for _, e := range entries {
		b, err := jsonpkg.Marshal(e)
		if err != nil {
			continue
		}
		_, _ = w.Write(b)
		_ = w.WriteByte('\n')
	}
	return w.Flush()
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

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var e Entry
		if jsonpkg.UnmarshalString(line, &e) != nil {
			continue
		}
		if e.Signature == "" || e.ToolCallID == "" || e.RequestID == "" {
			continue
		}
		s.cache.Put(e)
	}
}
