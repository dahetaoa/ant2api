package claude

import "testing"

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
