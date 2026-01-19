package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// WebUISettings represents the configurable settings that can be modified via WebUI
type WebUISettings struct {
	APIKey                 string `json:"apiKey"`
	WebUIPassword          string `json:"webuiPassword"`
	Debug                  string `json:"debug"`
	UserAgent              string `json:"userAgent"`
	Gemini3MediaResolution string `json:"gemini3MediaResolution"`
}

var settingsMu sync.RWMutex

// GetWebUISettings returns the current settings from the loaded config
func GetWebUISettings() WebUISettings {
	cfg := Get()
	mr := strings.ToLower(strings.TrimSpace(cfg.Gemini3MediaResolution))
	if mr != "" && mr != "low" && mr != "medium" && mr != "high" {
		mr = ""
	}
	return WebUISettings{
		APIKey:                 cfg.APIKey,
		WebUIPassword:          cfg.AdminPassword,
		Debug:                  cfg.Debug,
		UserAgent:              cfg.UserAgent,
		Gemini3MediaResolution: mr,
	}
}

// UpdateWebUISettings updates both the in-memory config and the .env file
func UpdateWebUISettings(s WebUISettings) error {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	mr := strings.ToLower(strings.TrimSpace(s.Gemini3MediaResolution))
	if mr != "" && mr != "low" && mr != "medium" && mr != "high" {
		mr = ""
	}
	s.Gemini3MediaResolution = mr

	// Update in-memory config
	cfg := Get()
	cfg.APIKey = s.APIKey
	cfg.AdminPassword = s.WebUIPassword
	cfg.Debug = s.Debug
	cfg.UserAgent = s.UserAgent
	cfg.Gemini3MediaResolution = s.Gemini3MediaResolution

	// Also update environment variables so they persist in the current process
	_ = os.Setenv("API_KEY", s.APIKey)
	_ = os.Setenv("WEBUI_PASSWORD", s.WebUIPassword)
	_ = os.Setenv("DEBUG", s.Debug)
	_ = os.Setenv("API_USER_AGENT", s.UserAgent)
	_ = os.Setenv("GEMINI3_MEDIA_RESOLUTION", s.Gemini3MediaResolution)

	// Write to .env file
	return updateDotEnvFile(map[string]string{
		"API_KEY":                  s.APIKey,
		"WEBUI_PASSWORD":           s.WebUIPassword,
		"DEBUG":                    s.Debug,
		"API_USER_AGENT":           s.UserAgent,
		"GEMINI3_MEDIA_RESOLUTION": s.Gemini3MediaResolution,
	})
}

// updateDotEnvFile updates specific keys in the .env file
func updateDotEnvFile(updates map[string]string) error {
	dotEnvPath, ok := findDotEnvPath()
	if !ok {
		// Try to create .env in current working directory
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("无法获取工作目录: %w", err)
		}
		dotEnvPath = filepath.Join(cwd, ".env")
	}

	// Read existing file content
	lines, err := readDotEnvLines(dotEnvPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("无法读取 .env 文件: %w", err)
	}

	// Track which keys we've updated
	updatedKeys := make(map[string]bool)

	// Update existing lines
	for i, line := range lines {
		key, _, ok := parseDotEnvLine(line)
		if !ok {
			continue
		}
		if newValue, exists := updates[key]; exists {
			// Update this line
			lines[i] = formatEnvLine(key, newValue)
			updatedKeys[key] = true
		}
	}

	// Add any new keys that weren't found
	for key, value := range updates {
		if !updatedKeys[key] {
			lines = append(lines, formatEnvLine(key, value))
		}
	}

	// Write back to file
	return writeDotEnvFile(dotEnvPath, lines)
}

// readDotEnvLines reads all lines from a .env file
func readDotEnvLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

// formatEnvLine formats a key-value pair for .env file
// Wraps values containing spaces in quotes
func formatEnvLine(key, value string) string {
	if strings.ContainsAny(value, " \t\"'") || value == "" {
		return fmt.Sprintf("%s=\"%s\"", key, value)
	}
	return fmt.Sprintf("%s=%s", key, value)
}

// writeDotEnvFile writes lines to a .env file
func writeDotEnvFile(path string, lines []string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("无法写入 .env 文件: %w", err)
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			return err
		}
	}
	return writer.Flush()
}
