package openai

import (
	"os"
	"sync"
	"testing"

	"anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/signature"
)

func TestBuildGenerationConfig_GeminiProImageVirtual_ForcesImageSize(t *testing.T) {
	req := &ChatRequest{Model: "gemini-3-pro-image-1k"}
	cfg := buildGenerationConfig(req)
	if cfg == nil || cfg.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set for virtual model")
	}
	if cfg.ImageConfig.ImageSize != "1K" {
		t.Fatalf("imageSize mismatch: got %q want %q", cfg.ImageConfig.ImageSize, "1K")
	}
	if cfg.ImageConfig.AspectRatio != "" {
		t.Fatalf("aspectRatio should not be set for OpenAI virtual model: got %q", cfg.ImageConfig.AspectRatio)
	}
}

func TestBuildGenerationConfig_GeminiProImageBase_DoesNotSetImageConfig(t *testing.T) {
	req := &ChatRequest{Model: "gemini-3-pro-image"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.ImageConfig != nil {
		t.Fatalf("expected ImageConfig to be nil for base model, got %#v", cfg.ImageConfig)
	}
}

func TestBuildGenerationConfig_Gemini3_AppliesGlobalMediaResolution(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "HIGH"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &ChatRequest{Model: "gemini-3-pro"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.MediaResolution != "MEDIA_RESOLUTION_HIGH" {
		t.Fatalf("mediaResolution mismatch: got %q want %q", cfg.MediaResolution, "MEDIA_RESOLUTION_HIGH")
	}
}

func TestBuildGenerationConfig_Gemini3Image_DoesNotApplyGlobalMediaResolution(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "high"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &ChatRequest{Model: "gemini-3-pro-image"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty for image model, got %q", cfg.MediaResolution)
	}
}

func TestBuildGenerationConfig_NonGemini3_DoesNotApplyGlobalMediaResolution(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "high"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &ChatRequest{Model: "gemini-2.5-pro"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty, got %q", cfg.MediaResolution)
	}
}

var initSignatureForTestsOnce sync.Once

func initSignatureForTests(t *testing.T) {
	t.Helper()
	initSignatureForTestsOnce.Do(func() {
		dir, err := os.MkdirTemp("", "ant2api-signature-test-*")
		if err != nil {
			t.Fatalf("create temp dir: %v", err)
		}
		config.Get().DataDir = dir
		signature.GetManager()
	})
}

func TestParseMarkdownImages_ImageModelMissingSignature_UsesFallback(t *testing.T) {
	initSignatureForTests(t)
	images := parseMarkdownImages("![image](data:image/png;base64,AAAA)", "gemini-3-pro-image")
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].signature != imageModelFallbackSignature {
		t.Fatalf("signature mismatch: got %q want %q", images[0].signature, imageModelFallbackSignature)
	}
}

func TestParseMarkdownImages_NonImageModelMissingSignature_LeavesEmpty(t *testing.T) {
	initSignatureForTests(t)
	images := parseMarkdownImages("![image](data:image/png;base64,BBBB)", "gemini-3-pro")
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].signature != "" {
		t.Fatalf("expected empty signature for non-image model, got %q", images[0].signature)
	}
}

func TestExtractUserParts_ImageModelMissingSignature_UsesFallback(t *testing.T) {
	initSignatureForTests(t)
	parts := extractUserParts([]any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,AAAA"}},
	}, "gemini-3-pro-image")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatalf("expected InlineData to be set")
	}
	if parts[0].ThoughtSignature != imageModelFallbackSignature {
		t.Fatalf("signature mismatch: got %q want %q", parts[0].ThoughtSignature, imageModelFallbackSignature)
	}
}

func TestExtractUserParts_NonImageModelMissingSignature_LeavesEmpty(t *testing.T) {
	initSignatureForTests(t)
	parts := extractUserParts([]any{
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "data:image/png;base64,BBBB"}},
	}, "gemini-3-pro")
	if len(parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(parts))
	}
	if parts[0].InlineData == nil {
		t.Fatalf("expected InlineData to be set")
	}
	if parts[0].ThoughtSignature != "" {
		t.Fatalf("expected empty signature for non-image model, got %q", parts[0].ThoughtSignature)
	}
}
