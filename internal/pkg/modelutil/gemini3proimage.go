package modelutil

import "strings"

// IsGeminiProImage returns true if the model name contains "gemini-3-pro-image" (case-insensitive).
func IsGeminiProImage(model string) bool {
	return strings.Contains(canonicalLower(model), "gemini-3-pro-image")
}

// GeminiProImageSizeConfig returns a forced imageSize and the backend model id for
// gemini-3-pro-image virtual size variants.
//
// Rules:
// - gemini-3-pro-image-1k => imageSize="1K", backendModel="gemini-3-pro-image"
// - gemini-3-pro-image-2k => imageSize="2K", backendModel="gemini-3-pro-image"
// - gemini-3-pro-image-4k => imageSize="4K", backendModel="gemini-3-pro-image"
// - gemini-3-pro-image    => ok=false
func GeminiProImageSizeConfig(model string) (imageSize string, backendModel string, ok bool) {
	m := canonicalLower(model)
	if m == "" {
		return "", "", false
	}

	const base = "gemini-3-pro-image"
	switch m {
	case base + "-1k":
		return "1K", base, true
	case base + "-2k":
		return "2K", base, true
	case base + "-4k":
		return "4K", base, true
	default:
		return "", "", false
	}
}
