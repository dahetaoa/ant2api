package config


import (
	"os"
	"path/filepath"
	"sync"
	"time"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type Endpoint struct {
	Key   string
	Label string
	Host  string
}

var (
	APIEndpoints = map[string]Endpoint{
		"daily": {
			Key:   "daily",
			Label: "Daily (Sandbox)",
			Host:  "daily-cloudcode-pa.sandbox.googleapis.com",
		},
		"autopush": {
			Key:   "autopush",
			Label: "Autopush (Sandbox)",
			Host:  "autopush-cloudcode-pa.sandbox.googleapis.com",
		},
		"production": {
			Key:   "production",
			Label: "Production",
			Host:  "cloudcode-pa.googleapis.com",
		},
	}

	RoundRobinEndpoints   = []string{"daily", "autopush", "production"}
	RoundRobinDpEndpoints = []string{"daily", "production"}
)

func (e Endpoint) StreamURL() string {
	return "https://" + e.Host + "/v1internal:streamGenerateContent?alt=sse"
}

func (e Endpoint) NoStreamURL() string {
	return "https://" + e.Host + "/v1internal:generateContent"
}

func (e Endpoint) FetchAvailableModelsURL() string {
	return "https://" + e.Host + "/v1internal:fetchAvailableModels"
}

type EndpointManager struct {
	mu                sync.Mutex
	mode              string
	roundRobinIndex   int
	roundRobinDpIndex int
	settingsPath      string
}

type Settings struct {
	EndpointMode    string    `json:"endpointMode"`
	CurrentEndpoint string    `json:"currentEndpoint"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

var (
	endpointMgr     *EndpointManager
	endpointMgrOnce sync.Once
)

func GetEndpointManager() *EndpointManager {
	endpointMgrOnce.Do(func() {
		cfg := Get()
		endpointMgr = &EndpointManager{
			mode:         cfg.EndpointMode,
			settingsPath: filepath.Join(cfg.DataDir, "settings.json"),
		}
		endpointMgr.loadSettings()
	})
	return endpointMgr
}

func (m *EndpointManager) loadSettings() {
	data, err := os.ReadFile(m.settingsPath)
	if err != nil {
		return
	}

	var settings Settings
	if err := jsonpkg.Unmarshal(data, &settings); err != nil {
		return
	}

	if os.Getenv("ENDPOINT_MODE") == "" && settings.EndpointMode != "" {
		m.mode = settings.EndpointMode
	}
}

func (m *EndpointManager) saveSettings() error {
	settings := Settings{
		EndpointMode:    m.mode,
		CurrentEndpoint: m.getCurrentEndpointKey(),
		UpdatedAt:       time.Now(),
	}

	data, err := jsonpkg.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.settingsPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	return os.WriteFile(m.settingsPath, data, 0o644)
}

func (m *EndpointManager) getCurrentEndpointKey() string {
	switch m.mode {
	case "round-robin":
		idx := m.roundRobinIndex
		if idx < 0 {
			idx = 0
		}
		return RoundRobinEndpoints[idx%len(RoundRobinEndpoints)]
	case "round-robin-dp":
		idx := m.roundRobinDpIndex
		if idx < 0 {
			idx = 0
		}
		return RoundRobinDpEndpoints[idx%len(RoundRobinDpEndpoints)]
	default:
		return m.mode
	}
}

func (m *EndpointManager) GetActiveEndpoint() Endpoint {
	m.mu.Lock()
	defer m.mu.Unlock()

	switch m.mode {
	case "round-robin":
		key := RoundRobinEndpoints[m.roundRobinIndex]
		m.roundRobinIndex = (m.roundRobinIndex + 1) % len(RoundRobinEndpoints)
		return APIEndpoints[key]
	case "round-robin-dp":
		key := RoundRobinDpEndpoints[m.roundRobinDpIndex]
		m.roundRobinDpIndex = (m.roundRobinDpIndex + 1) % len(RoundRobinDpEndpoints)
		return APIEndpoints[key]
	default:
		if ep, ok := APIEndpoints[m.mode]; ok {
			return ep
		}
		return APIEndpoints["daily"]
	}
}

func (m *EndpointManager) GetMode() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mode
}

func (m *EndpointManager) SetMode(mode string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	validModes := map[string]bool{
		"daily": true, "autopush": true, "production": true,
		"round-robin": true, "round-robin-dp": true,
	}
	if !validModes[mode] {
		return nil
	}

	m.mode = mode
	return m.saveSettings()
}
