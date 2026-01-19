package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

type Config struct {
	Host string
	Port int

	OAuthRedirectPort int

	UserAgent string
	TimeoutMs int
	Proxy     string

	APIKey string

	RetryStatusCodes []int
	RetryMaxAttempts int

	Debug string

	EndpointMode string

	GoogleClientID     string
	GoogleClientSecret string

	DataDir                string
	AdminPassword          string
	Gemini3MediaResolution string
}

var (
	cfg  *Config
	once sync.Once
)

const (
	DefaultClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	DefaultClientSecret = "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"
)

func Load() *Config {
	once.Do(func() {
		loadDotEnv()

		port := getEnvInt("PORT", 8045)

		cfg = &Config{
			Host:                   getEnv("HOST", "0.0.0.0"),
			Port:                   port,
			OAuthRedirectPort:      getEnvInt("OAUTH_REDIRECT_PORT", port),
			UserAgent:              getEnv("API_USER_AGENT", "antigravity/1.11.3 windows/amd64"),
			TimeoutMs:              getEnvInt("TIMEOUT", 180000),
			Proxy:                  getEnv("PROXY", ""),
			APIKey:                 getEnv("API_KEY", ""),
			RetryStatusCodes:       getEnvIntSlice("RETRY_STATUS_CODES", []int{429, 500}),
			RetryMaxAttempts:       getEnvInt("RETRY_MAX_ATTEMPTS", 3),
			Debug:                  getEnv("DEBUG", "off"),
			EndpointMode:           getEnv("ENDPOINT_MODE", "daily"),
			GoogleClientID:         getEnv("GOOGLE_CLIENT_ID", ""),
			GoogleClientSecret:     getEnv("GOOGLE_CLIENT_SECRET", ""),
			DataDir:                getEnv("DATA_DIR", "./data"),
			AdminPassword:          getEnv("WEBUI_PASSWORD", ""),
			Gemini3MediaResolution: getEnv("GEMINI3_MEDIA_RESOLUTION", ""),
		}

		if cfg.OAuthRedirectPort <= 0 {
			cfg.OAuthRedirectPort = cfg.Port
		}

		for i, arg := range os.Args[1:] {
			if arg == "-debug" && i+1 < len(os.Args[1:]) {
				cfg.Debug = os.Args[i+2]
			}
		}
	})

	return cfg
}

func Get() *Config {
	if cfg == nil {
		return Load()
	}
	return cfg
}

func ClientID() string {
	if Get().GoogleClientID != "" {
		return Get().GoogleClientID
	}
	return DefaultClientID
}

func ClientSecret() string {
	if Get().GoogleClientSecret != "" {
		return Get().GoogleClientSecret
	}
	return DefaultClientSecret
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvIntSlice(key string, defaultValue []int) []int {
	if value := os.Getenv(key); value != "" {
		parts := strings.Split(value, ",")
		result := make([]int, 0, len(parts))
		for _, p := range parts {
			if i, err := strconv.Atoi(strings.TrimSpace(p)); err == nil {
				result = append(result, i)
			}
		}
		if len(result) > 0 {
			return result
		}
	}
	return defaultValue
}
