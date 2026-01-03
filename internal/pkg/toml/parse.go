package toml

import (
	"strconv"
	"strings"
)

// Parse parses a tiny subset of TOML needed by this project.
func Parse(input string) (map[string]any, error) {
	result := make(map[string]any)
	var currentArrayName string
	var currentObj map[string]any

	lines := strings.Split(input, "\n")
	for _, rawLine := range lines {
		line := stripInlineComment(strings.TrimSpace(rawLine))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[[") && strings.HasSuffix(line, "]]" ) {
			section := strings.TrimSpace(line[2 : len(line)-2])
			if currentObj != nil && currentArrayName != "" {
				arr, _ := result[currentArrayName].([]map[string]any)
				arr = append(arr, currentObj)
				result[currentArrayName] = arr
			}
			currentArrayName = section
			currentObj = make(map[string]any)
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			if currentObj != nil && currentArrayName != "" {
				arr, _ := result[currentArrayName].([]map[string]any)
				arr = append(arr, currentObj)
				result[currentArrayName] = arr
			}
			currentArrayName = ""
			currentObj = nil
			continue
		}

		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			value := parseValue(strings.TrimSpace(line[idx+1:]))
			if currentObj != nil {
				currentObj[key] = value
			} else {
				result[key] = value
			}
		}
	}

	if currentObj != nil && currentArrayName != "" {
		arr, _ := result[currentArrayName].([]map[string]any)
		arr = append(arr, currentObj)
		result[currentArrayName] = arr
	}

	return result, nil
}

func stripInlineComment(line string) string {
	inQuote := false
	for i, c := range line {
		if c == '"' {
			inQuote = !inQuote
			continue
		}
		if c == '#' && !inQuote {
			return strings.TrimSpace(line[:i])
		}
	}
	return line
}

func parseValue(raw string) any {
	raw = strings.TrimSpace(raw)

	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		return raw[1 : len(raw)-1]
	}
	if strings.HasPrefix(raw, `'`) && strings.HasSuffix(raw, `'`) {
		return raw[1 : len(raw)-1]
	}
	if raw == "true" {
		return true
	}
	if raw == "false" {
		return false
	}
	if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(raw, 64); err == nil {
		return f
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		return parseArray(raw[1 : len(raw)-1])
	}
	return raw
}

func parseArray(content string) []any {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	parts := strings.Split(content, ",")
	result := make([]any, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		result = append(result, parseValue(p))
	}
	return result
}

