package modelutil

import "strings"

// ClaudeSonnet45ThinkingBudget returns a forced thinkingBudget for claude-sonnet-4-5 variants.
//
// Model matching is case-insensitive and ignores leading/trailing whitespace.
// If the model is prefixed with "models/", that prefix is ignored.
//
// Rules:
// - claude-sonnet-4-5-thinking* => thinkingBudget=32000
// - claude-sonnet-4-5*         => thinkingBudget=0
func ClaudeSonnet45ThinkingBudget(model string) (budget int, ok bool) {
	m := strings.TrimSpace(model)
	m = strings.TrimPrefix(m, "models/")
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return 0, false
	}

	const base = "claude-sonnet-4-5"
	const thinking = "claude-sonnet-4-5-thinking"

	// Check "-thinking" first to avoid false matches.
	if strings.HasPrefix(m, thinking) {
		return 32000, true
	}
	if strings.HasPrefix(m, base) {
		return 0, true
	}
	return 0, false
}
