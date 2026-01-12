package modelutil

import "strings"

// Gemini3FlashThinkingConfig returns a forced thinking hint for the Gemini 3 Flash family,
// and the backend model id that should be sent to Vertex.
//
// Model matching is case-insensitive and ignores leading/trailing whitespace.
// If the model is prefixed with "models/", that prefix is ignored.
//
// Rules:
// - gemini-3-flash-thinking* => thinkingLevel="high", backend model strips "-thinking"
// - gemini-3-flash*          => non-thinking model, backend model unchanged
func Gemini3FlashThinkingConfig(model string) (thinkingLevel string, backendModel string, ok bool) {
	m := strings.TrimSpace(model)
	m = strings.TrimPrefix(m, "models/")
	m = strings.ToLower(strings.TrimSpace(m))
	if m == "" {
		return "", "", false
	}

	const base = "gemini-3-flash"
	const thinking = "gemini-3-flash-thinking"

	// Check "-thinking" first to avoid false matches.
	if strings.HasPrefix(m, thinking) {
		suffix := strings.TrimPrefix(m, thinking) // "" or e.g. "-latest"
		return "high", base + suffix, true
	}
	if strings.HasPrefix(m, base) {
		return "", m, true
	}
	return "", "", false
}

func IsGemini3Flash(model string) bool {
	_, _, ok := Gemini3FlashThinkingConfig(model)
	return ok
}
