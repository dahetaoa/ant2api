package credential

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/logger"
	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type Store struct {
	mu           sync.RWMutex
	accounts     []Account
	currentIndex int
	filePath     string
}

var (
	store     *Store
	storeOnce sync.Once
)

func GetStore() *Store {
	storeOnce.Do(func() {
		cfg := config.Get()
		store = &Store{filePath: filepath.Join(cfg.DataDir, "accounts.json")}
		_ = store.Load()
	})
	return store
}

func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			s.accounts = []Account{}
			return nil
		}
		return err
	}

	if err := jsonpkg.Unmarshal(data, &s.accounts); err != nil {
		s.accounts = []Account{}
		return err
	}

	for i := range s.accounts {
		s.accounts[i].SessionID = id.SessionID()
	}
	logger.Info("Loaded %d accounts", len(s.accounts))
	return nil
}

func (s *Store) saveUnlocked() error {
	data, err := jsonpkg.MarshalIndent(s.accounts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0o644)
}

func (s *Store) Save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveUnlocked()
}

func (s *Store) GetToken() (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.accounts) == 0 {
		return nil, errors.New("没有可用的账号")
	}

	nowMs := time.Now().UnixMilli()
	for attempts := 0; attempts < len(s.accounts); attempts++ {
		account := &s.accounts[s.currentIndex]
		s.currentIndex = (s.currentIndex + 1) % len(s.accounts)

		if !account.Enable {
			continue
		}

		if account.IsExpired(nowMs) {
			if err := RefreshToken(account); err != nil {
				continue
			}
			_ = s.saveUnlocked()
		}

		copyAccount := *account
		return &copyAccount, nil
	}

	return nil, errors.New("没有可用的 token")
}

func (s *Store) GetTokenByProjectID(projectID string) (*Account, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	nowMs := time.Now().UnixMilli()
	for i := range s.accounts {
		account := &s.accounts[i]
		if account.ProjectID == projectID && account.Enable {
			if account.IsExpired(nowMs) {
				if err := RefreshToken(account); err != nil {
					return nil, err
				}
				_ = s.saveUnlocked()
			}
			copyAccount := *account
			return &copyAccount, nil
		}
	}

	return nil, errors.New("未找到指定的账号")
}

func (s *Store) GetAll() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]Account, len(s.accounts))
	copy(result, s.accounts)
	return result
}

func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.accounts)
}

func (s *Store) EnabledCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	for _, a := range s.accounts {
		if a.Enable {
			count++
		}
	}
	return count
}

func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts = []Account{}
	s.currentIndex = 0
	return s.saveUnlocked()
}

func (s *Store) Add(account Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	account.SessionID = id.SessionID()
	if account.CreatedAt.IsZero() {
		account.CreatedAt = time.Now()
	}

	for i, a := range s.accounts {
		if (account.Email != "" && a.Email == account.Email) ||
			(account.RefreshToken != "" && a.RefreshToken == account.RefreshToken) {
			account.CreatedAt = a.CreatedAt
			s.accounts[i] = account
			return s.saveUnlocked()
		}
	}

	s.accounts = append(s.accounts, account)
	return s.saveUnlocked()
}

func (s *Store) Delete(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.accounts) {
		return errors.New("索引超出范围")
	}

	s.accounts = append(s.accounts[:index], s.accounts[index+1:]...)
	if s.currentIndex >= len(s.accounts) {
		s.currentIndex = 0
	}
	return s.saveUnlocked()
}

func (s *Store) SetEnable(index int, enable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.accounts) {
		return errors.New("索引超出范围")
	}

	s.accounts[index].Enable = enable
	return s.saveUnlocked()
}

func (s *Store) RefreshAccount(index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if index < 0 || index >= len(s.accounts) {
		return errors.New("索引超出范围")
	}

	if err := RefreshToken(&s.accounts[index]); err != nil {
		return err
	}

	return s.saveUnlocked()
}

func (s *Store) RefreshAll() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	success := 0
	failed := 0
	for i := range s.accounts {
		if err := RefreshToken(&s.accounts[i]); err != nil {
			failed++
		} else {
			success++
		}
	}
	_ = s.saveUnlocked()
	return success, failed
}
