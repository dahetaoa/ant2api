package credential

import (
	"crypto/rand"
	"encoding/base64"
	"sync"
	"time"
)

const oauthStateTTL = 10 * time.Minute

type oauthStateManager struct {
	mu     sync.Mutex
	states map[string]time.Time
}

var oauthStates = &oauthStateManager{
	states: make(map[string]time.Time),
}

func GenerateState() (string, error) {
	return oauthStates.generate()
}

func ValidateState(state string) bool {
	return oauthStates.validate(state)
}

func (m *oauthStateManager) generate() (string, error) {
	stateBytes := make([]byte, 32)
	if _, err := rand.Read(stateBytes); err != nil {
		return "", err
	}

	state := base64.RawURLEncoding.EncodeToString(stateBytes)
	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.purgeExpiredLocked(now)
	m.states[state] = now.Add(oauthStateTTL)
	return state, nil
}

func (m *oauthStateManager) validate(state string) bool {
	if state == "" {
		return false
	}

	now := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	m.purgeExpiredLocked(now)

	expiresAt, ok := m.states[state]
	if !ok {
		return false
	}

	delete(m.states, state)
	return now.Before(expiresAt)
}

func (m *oauthStateManager) purgeExpiredLocked(now time.Time) {
	for state, expiresAt := range m.states {
		if !now.Before(expiresAt) {
			delete(m.states, state)
		}
	}
}
