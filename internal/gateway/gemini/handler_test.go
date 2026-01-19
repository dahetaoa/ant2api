package gemini

import (
	"testing"

	"anti2api-golang/refactor/internal/config"
)

func strptr(s string) *string { return &s }

func TestToVertexGenerationConfig_GeminiProImage_Base_OmitsWhenUnset(t *testing.T) {
	out := toVertexGenerationConfig("gemini-3-pro-image", nil)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.ImageConfig != nil {
		t.Fatalf("expected ImageConfig to be nil, got %#v", out.ImageConfig)
	}
}

func TestToVertexGenerationConfig_GeminiProImage_Base_PassThroughAspectRatioOnly(t *testing.T) {
	cfg := &GeminiGenerationConfig{CandidateCount: 1, ImageConfig: &GeminiImageCfg{AspectRatio: "16:9"}}
	out := toVertexGenerationConfig("gemini-3-pro-image", cfg)
	if out == nil || out.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set")
	}
	if out.ImageConfig.AspectRatio != "16:9" {
		t.Fatalf("aspectRatio mismatch: got %q want %q", out.ImageConfig.AspectRatio, "16:9")
	}
	if out.ImageConfig.ImageSize != "" {
		t.Fatalf("expected imageSize to be empty, got %q", out.ImageConfig.ImageSize)
	}
}

func TestToVertexGenerationConfig_GeminiProImage_Base_PassThroughImageSizeOnly(t *testing.T) {
	cfg := &GeminiGenerationConfig{CandidateCount: 1, ImageConfig: &GeminiImageCfg{ImageSize: "2K"}}
	out := toVertexGenerationConfig("gemini-3-pro-image", cfg)
	if out == nil || out.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set")
	}
	if out.ImageConfig.ImageSize != "2K" {
		t.Fatalf("imageSize mismatch: got %q want %q", out.ImageConfig.ImageSize, "2K")
	}
	if out.ImageConfig.AspectRatio != "" {
		t.Fatalf("expected aspectRatio to be empty, got %q", out.ImageConfig.AspectRatio)
	}
}

func TestToVertexGenerationConfig_GeminiProImage_Base_IgnoresEmptyImageConfig(t *testing.T) {
	cfg := &GeminiGenerationConfig{CandidateCount: 1, ImageConfig: &GeminiImageCfg{AspectRatio: "  ", ImageSize: ""}}
	out := toVertexGenerationConfig("gemini-3-pro-image", cfg)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.ImageConfig != nil {
		t.Fatalf("expected ImageConfig to be nil when all fields are empty, got %#v", out.ImageConfig)
	}
}

func TestToVertexGenerationConfig_GeminiProImage_Virtual_ForcesImageSizeEvenWithoutCfg(t *testing.T) {
	out := toVertexGenerationConfig("gemini-3-pro-image-1k", nil)
	if out == nil || out.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set for virtual model")
	}
	if out.ImageConfig.ImageSize != "1K" {
		t.Fatalf("imageSize mismatch: got %q want %q", out.ImageConfig.ImageSize, "1K")
	}
}

func TestToVertexGenerationConfig_GeminiProImage_Virtual_OverridesClientImageSize(t *testing.T) {
	cfg := &GeminiGenerationConfig{CandidateCount: 1, ImageConfig: &GeminiImageCfg{AspectRatio: "1:1", ImageSize: "4K"}}
	out := toVertexGenerationConfig("gemini-3-pro-image-1k", cfg)
	if out == nil || out.ImageConfig == nil {
		t.Fatalf("expected ImageConfig to be set for virtual model")
	}
	if out.ImageConfig.ImageSize != "1K" {
		t.Fatalf("imageSize mismatch: got %q want %q", out.ImageConfig.ImageSize, "1K")
	}
	if out.ImageConfig.AspectRatio != "1:1" {
		t.Fatalf("aspectRatio mismatch: got %q want %q", out.ImageConfig.AspectRatio, "1:1")
	}
}

func TestToVertexGenerationConfig_NonImage_IgnoresImageConfig(t *testing.T) {
	cfg := &GeminiGenerationConfig{CandidateCount: 1, ImageConfig: &GeminiImageCfg{AspectRatio: "1:1", ImageSize: "1K"}}
	out := toVertexGenerationConfig("gemini-3-flash", cfg)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.ImageConfig != nil {
		t.Fatalf("expected ImageConfig to be nil for non-image model, got %#v", out.ImageConfig)
	}
}

func TestToVertexGenerationConfig_Gemini3_AppliesGlobalMediaResolution_WhenClientUnset(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "Medium"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	out := toVertexGenerationConfig("gemini-3-pro", nil)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.MediaResolution != "MEDIA_RESOLUTION_MEDIUM" {
		t.Fatalf("mediaResolution mismatch: got %q want %q", out.MediaResolution, "MEDIA_RESOLUTION_MEDIUM")
	}
}

func TestToVertexGenerationConfig_Gemini3_ClientMediaResolution_OverridesGlobal(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "low"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	cfg := &GeminiGenerationConfig{CandidateCount: 1, MediaResolution: strptr("HIGH")}
	out := toVertexGenerationConfig("gemini-3-pro", cfg)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.MediaResolution != "MEDIA_RESOLUTION_HIGH" {
		t.Fatalf("mediaResolution mismatch: got %q want %q", out.MediaResolution, "MEDIA_RESOLUTION_HIGH")
	}
}

func TestToVertexGenerationConfig_Gemini3_ClientMediaResolution_Empty_DisablesGlobal(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "high"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	cfg := &GeminiGenerationConfig{CandidateCount: 1, MediaResolution: strptr("")}
	out := toVertexGenerationConfig("gemini-3-pro", cfg)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty, got %q", out.MediaResolution)
	}
}

func TestToVertexGenerationConfig_Gemini3_ClientMediaResolution_Invalid_DisablesGlobal(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "low"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	cfg := &GeminiGenerationConfig{CandidateCount: 1, MediaResolution: strptr("ultra_high")}
	out := toVertexGenerationConfig("gemini-3-pro", cfg)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty, got %q", out.MediaResolution)
	}
}

func TestToVertexGenerationConfig_NonGemini3_DoesNotApplyGlobalMediaResolution(t *testing.T) {
	c := config.Get()
	old := c.Gemini3MediaResolution
	c.Gemini3MediaResolution = "high"
	t.Cleanup(func() { c.Gemini3MediaResolution = old })

	out := toVertexGenerationConfig("gemini-2.5-pro", nil)
	if out == nil {
		t.Fatalf("expected out != nil")
	}
	if out.MediaResolution != "" {
		t.Fatalf("expected mediaResolution to be empty, got %q", out.MediaResolution)
	}
}
