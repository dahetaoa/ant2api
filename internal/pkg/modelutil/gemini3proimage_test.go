package modelutil

import "testing"

func TestIsGeminiProImage(t *testing.T) {
	cases := []struct {
		model string
		want  bool
	}{
		{model: "gemini-3-pro-image", want: true},
		{model: "gemini-3-pro-image-1k", want: true},
		{model: "models/GEMINI-3-PRO-IMAGE-2K", want: true},
		{model: "gemini-3-flash", want: false},
		{model: "gpt-4o", want: false},
	}
	for _, tc := range cases {
		got := IsGeminiProImage(tc.model)
		if got != tc.want {
			t.Fatalf("IsGeminiProImage(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

func TestGeminiProImageSizeConfig(t *testing.T) {
	cases := []struct {
		model         string
		wantImageSize string
		wantBackend   string
		wantOK        bool
	}{
		{model: "gemini-3-pro-image-1k", wantImageSize: "1K", wantBackend: "gemini-3-pro-image", wantOK: true},
		{model: "GEMINI-3-PRO-IMAGE-2K", wantImageSize: "2K", wantBackend: "gemini-3-pro-image", wantOK: true},
		{model: "models/gemini-3-pro-image-4k", wantImageSize: "4K", wantBackend: "gemini-3-pro-image", wantOK: true},
		{model: "gemini-3-pro-image", wantOK: false},
		{model: "gemini-3-flash", wantOK: false},
	}

	for _, tc := range cases {
		gotSize, gotBackend, ok := GeminiProImageSizeConfig(tc.model)
		if ok != tc.wantOK {
			t.Fatalf("GeminiProImageSizeConfig(%q) ok=%v want %v (size=%q backend=%q)", tc.model, ok, tc.wantOK, gotSize, gotBackend)
		}
		if !ok {
			continue
		}
		if gotSize != tc.wantImageSize || gotBackend != tc.wantBackend {
			t.Fatalf("GeminiProImageSizeConfig(%q) = (size=%q backend=%q), want (size=%q backend=%q)", tc.model, gotSize, gotBackend, tc.wantImageSize, tc.wantBackend)
		}
	}
}

func TestBackendModelID_GeminiProImageVirtual(t *testing.T) {
	if got := BackendModelID("gemini-3-pro-image-1k"); got != "gemini-3-pro-image" {
		t.Fatalf("BackendModelID(gemini-3-pro-image-1k)=%q, want %q", got, "gemini-3-pro-image")
	}
}

func TestBuildSortedModelIDs_IncludesGeminiProImageVirtuals(t *testing.T) {
	models := map[string]any{
		"gpt-4o":             map[string]any{},
		"gemini-3-pro-image": map[string]any{},
	}
	got := BuildSortedModelIDs(models)
	want := []string{
		"gemini-3-pro-image",
		"gemini-3-pro-image-1k",
		"gemini-3-pro-image-2k",
		"gemini-3-pro-image-4k",
		"gpt-4o",
	}
	if len(got) != len(want) {
		t.Fatalf("BuildSortedModelIDs length mismatch: got %d want %d (all=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("BuildSortedModelIDs[%d]=%q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestBuildSortedModelIDs_NoGeminiProImage_NoVirtuals(t *testing.T) {
	models := map[string]any{
		"gpt-4o": map[string]any{},
	}
	got := BuildSortedModelIDs(models)
	want := []string{"gpt-4o"}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("BuildSortedModelIDs mismatch: got %v want %v", got, want)
	}
}
