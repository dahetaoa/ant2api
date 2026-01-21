package openai

import (
	"testing"

	"anti2api-golang/refactor/internal/config"
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
