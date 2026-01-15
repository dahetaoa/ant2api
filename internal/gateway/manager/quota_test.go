package manager

import (
	"testing"
)

func TestGroupQuotaGroups_OrderAndMapping(t *testing.T) {
	models := map[string]any{
		"claude-3-sonnet": map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.5, "resetTime": "2026-01-15T14:40:24Z"}},
		"gpt-4o":          map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.5, "resetTime": "2026-01-15T14:40:24Z"}},

		// Use int64 to match sonic's UseInt64 behavior.
		"gemini-3-pro-high":     map[string]any{"quotaInfo": map[string]any{"remainingFraction": int64(1), "resetTime": "2026-01-15T00:00:00Z"}},
		"models/gemini-3-flash": map[string]any{"quota": map[string]any{"remainingFraction": 0.8, "resetTime": "2026-01-15T01:00:00Z"}},
		"gemini-3-pro-image":    map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.0, "resetTime": "2026-01-15T02:00:00Z"}},

		"gemini-2.5-flash": map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.3, "resetTime": "2026-01-15T03:00:00Z"}},
		"gemini-2.5-cu":    map[string]any{"quotaInfo": map[string]any{"remainingFraction": 0.3, "resetTime": "2026-01-15T03:00:00Z"}},
	}

	groups := groupQuotaGroups(models)
	if len(groups) != 5 {
		t.Fatalf("expected 5 groups, got %d", len(groups))
	}

	wantOrder := []string{"Claude/GPT", "Gemini 3 Pro", "Gemini 3 Flash", "Gemini 3 Pro Image", "Gemini 2.5 Pro/Flash/Lite"}
	for i, want := range wantOrder {
		if groups[i].GroupName != want {
			t.Fatalf("group[%d] name mismatch: want %q, got %q", i, want, groups[i].GroupName)
		}
	}

	// Gemini 3 Flash should canonicalize models/ prefix and parse nested quota.
	flash := groups[2]
	if len(flash.ModelList) != 1 || flash.ModelList[0] != "gemini-3-flash" {
		t.Fatalf("gemini-3-flash model list mismatch: %#v", flash.ModelList)
	}
	if flash.RemainingFraction == nil || abs(*flash.RemainingFraction-0.8) > 1e-9 {
		t.Fatalf("gemini-3-flash remainingFraction mismatch: %#v", flash.RemainingFraction)
	}
	if flash.ResetTime != "2026-01-15T01:00:00Z" {
		t.Fatalf("gemini-3-flash resetTime mismatch: %q", flash.ResetTime)
	}

	other := groups[4]
	if len(other.ModelList) != 2 || other.ModelList[0] != "gemini-2.5-cu" || other.ModelList[1] != "gemini-2.5-flash" {
		t.Fatalf("other models list mismatch: %#v", other.ModelList)
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
