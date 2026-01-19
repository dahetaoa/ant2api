package openai

import "testing"

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
