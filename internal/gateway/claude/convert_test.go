package claude

import (
	"testing"

	"anti2api-golang/refactor/internal/config"
)

func TestBuildGenerationConfig_GeminiProImageVirtual_ForcesImageSize(t *testing.T) {
	req := &MessagesRequest{Model: "GEMINI-3-PRO-IMAGE-2K"}
	cfg := buildGenerationConfig(req)
	if cfg == nil || cfg.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set for virtual model")
	}
	if cfg.ImageConfig.ImageSize != "2K" {
		t.Fatalf("imageSize mismatch: got %q want %q", cfg.ImageConfig.ImageSize, "2K")
	}
}

func TestBuildGenerationConfig_GeminiProImageBase_DoesNotSetImageConfig(t *testing.T) {
	req := &MessagesRequest{Model: "gemini-3-pro-image"}
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
	c.Gemini3MediaResolution = "Medium"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &MessagesRequest{Model: "gemini-3-flash"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.MediaResolution != "MEDIA_RESOLUTION_MEDIUM" {
		t.Fatalf("mediaResolution mismatch: got %q want %q", cfg.MediaResolution, "MEDIA_RESOLUTION_MEDIUM")
	}
}

func TestBuildGenerationConfig_Gemini3Image_DoesNotApplyGlobalMediaResolution(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "high"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &MessagesRequest{Model: "gemini-3-pro-image"}
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
	c.Gemini3MediaResolution = "low"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	req := &MessagesRequest{Model: "gemini-2.5-pro"}
	cfg := buildGenerationConfig(req)
	if cfg == nil {
		t.Fatalf("expected cfg != nil")
	}
	if cfg.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty, got %q", cfg.MediaResolution)
	}
}
