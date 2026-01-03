package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

func loadDotEnv() {
	dotEnvPath, ok := findDotEnvPath()
	if !ok {
		return
	}

	file, err := os.Open(dotEnvPath)
	if err != nil {
		return
	}
	defer file.Close()

	apiKeyDefined := false

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		key, value, ok := parseDotEnvLine(scanner.Text())
		if !ok {
			continue
		}

		if key == "API_KEY" {
			apiKeyDefined = true
		}

		_ = os.Setenv(key, value)
	}

	if !apiKeyDefined {
		_ = os.Unsetenv("API_KEY")
	}
}

func findDotEnvPath() (string, bool) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", false
	}

	for dir := cwd; dir != ""; {
		path := filepath.Join(dir, ".env")
		if isRegularFile(path) {
			return path, true
		}

		if isRegularFile(filepath.Join(dir, "go.mod")) || isDir(filepath.Join(dir, ".git")) {
			return "", false
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", false
}

func parseDotEnvLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	line = strings.TrimPrefix(line, "export ")

	eqIdx := strings.IndexByte(line, '=')
	if eqIdx <= 0 {
		return "", "", false
	}

	key := strings.TrimSpace(line[:eqIdx])
	if key == "" {
		return "", "", false
	}

	raw := strings.TrimSpace(line[eqIdx+1:])
	if raw == "" {
		return key, "", true
	}

	if len(raw) >= 2 && ((raw[0] == '\'' && raw[len(raw)-1] == '\'') || (raw[0] == '"' && raw[len(raw)-1] == '"')) {
		return key, raw[1 : len(raw)-1], true
	}

	raw = stripInlineComment(raw)
	return key, strings.TrimSpace(raw), true
}

func stripInlineComment(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] != '#' {
			continue
		}
		if i == 0 || value[i-1] == ' ' || value[i-1] == '\t' {
			return strings.TrimSpace(value[:i])
		}
	}
	return value
}

func isRegularFile(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.Mode().IsRegular()
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.IsDir()
}
