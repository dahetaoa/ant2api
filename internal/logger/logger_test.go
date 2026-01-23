package logger

import (
	"reflect"
	"strings"
	"testing"
)

func TestSanitizeJSONForLogContext_NoSanitizationReturnsOriginalMap(t *testing.T) {
	orig := map[string]any{
		"ok":   true,
		"nest": map[string]any{"msg": "hello"},
	}

	gotAny := sanitizeJSONForLog(orig)
	got, ok := gotAny.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", gotAny)
	}

	if reflect.ValueOf(orig).Pointer() != reflect.ValueOf(got).Pointer() {
		t.Fatalf("expected original map to be returned when no sanitization is needed")
	}
}

func TestSanitizeJSONForLogContext_InlineDataTruncatesData(t *testing.T) {
	data := strings.Repeat("A", 400)
	orig := map[string]any{
		"inlineData": map[string]any{
			"mimeType": "image/png",
			"data":     data,
		},
	}

	gotAny := sanitizeJSONForLog(orig)
	got, ok := gotAny.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", gotAny)
	}

	inlineAny, ok := got["inlineData"]
	if !ok {
		t.Fatalf("expected inlineData key")
	}
	inlineMap, ok := inlineAny.(map[string]any)
	if !ok {
		t.Fatalf("expected inlineData map[string]any, got %T", inlineAny)
	}
	truncated, _ := inlineMap["data"].(string)
	if !strings.Contains(truncated, "[TRUNCATED:") {
		t.Fatalf("expected inlineData.data to be truncated, got: %q", truncated)
	}

	// Ensure original input was not modified.
	origInline := orig["inlineData"].(map[string]any)
	if origInline["data"].(string) != data {
		t.Fatalf("expected original inlineData.data unchanged")
	}
}

func TestSanitizeJSONForLogContext_DataURLTruncates(t *testing.T) {
	data := strings.Repeat("A", 400)
	url := "data:image/png;base64," + data
	orig := map[string]any{"url": url}

	gotAny := sanitizeJSONForLog(orig)
	got := gotAny.(map[string]any)

	truncated := got["url"].(string)
	if !strings.Contains(truncated, "[TRUNCATED:") {
		t.Fatalf("expected url to be truncated, got: %q", truncated)
	}
}

func TestSanitizeJSONForLogContext_MarkdownDataURLTruncates(t *testing.T) {
	data := strings.Repeat("A", 400)
	content := "![image](data:image/png;base64," + data + ") trailing"
	orig := map[string]any{"content": content}

	gotAny := sanitizeJSONForLog(orig)
	got := gotAny.(map[string]any)

	truncated := got["content"].(string)
	if !strings.Contains(truncated, "[TRUNCATED:") {
		t.Fatalf("expected content to be truncated, got: %q", truncated)
	}
	if !strings.HasSuffix(truncated, ") trailing") {
		t.Fatalf("expected markdown suffix preserved, got: %q", truncated)
	}
}
