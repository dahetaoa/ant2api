package modelutil

import "strings"

// ClaudeOpus45ThinkingConfig returns a forced thinkingBudget for claude-opus-4-5 variants,
// and the backend model id that should be sent to Vertex.
//
// Model matching is case-insensitive and ignores leading/trailing whitespace.
// If the model is prefixed with "models/", that prefix is ignored.
//
// Rules:
// - claude-opus-4-5-thinking* => thinkingBudget=32000, backend model unchanged
// - claude-opus-4-5*         => thinkingBudget=0, backend model is mapped to "-thinking" (+ same suffix)
func ClaudeOpus45ThinkingConfig(model string) (budget int, backendModel string, ok bool) {
	m := strings.TrimSpace(model)
	m = strings.TrimPrefix(m, "models/")
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return 0, "", false
	}

	const base = "claude-opus-4-5"
	const thinking = "claude-opus-4-5-thinking"

	// Check "-thinking" first to avoid false matches.
	if strings.HasPrefix(m, thinking) {
		return DefaultClaudeThinkingBudgetTokens, m, true
	}
	if strings.HasPrefix(m, base) {
		suffix := strings.TrimPrefix(m, base) // "" or e.g. "-latest"
		return 0, thinking + suffix, true
	}
	return 0, "", false
}
