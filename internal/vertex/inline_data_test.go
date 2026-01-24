package vertex

import (
	"testing"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/pkg/lazyimage"
)

func TestInlineData_Marshal_Legacy(t *testing.T) {
	in := NewInlineData("image/png", "QUJDRA==")
	b, err := jsonpkg.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(b) != `{"mimeType":"image/png","data":"QUJDRA=="}` {
		t.Fatalf("json mismatch: got %s", string(b))
	}
}

func TestInlineData_Marshal_Lazy(t *testing.T) {
	body := []byte(`{"url":"data:image/png;base64,QUJDRA=="}`)
	refs := lazyimage.Extract(body)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	in := NewInlineDataFromRef(refs[0])
	if in == nil || !in.IsLazy() {
		t.Fatalf("expected lazy InlineData")
	}

	b, err := jsonpkg.Marshal(in)
	if err != nil {
		t.Fatalf("Marshal error: %v", err)
	}
	if string(b) != `{"mimeType":"image/png","data":"QUJDRA=="}` {
		t.Fatalf("json mismatch: got %s", string(b))
	}
	if in.SignatureKey() != "QUJDRA==" {
		t.Fatalf("signatureKey mismatch: got %q", in.SignatureKey())
	}
}

func TestInlineData_Unmarshal_PreservesDataField(t *testing.T) {
	var in InlineData
	if err := jsonpkg.Unmarshal([]byte(`{"mimeType":"image/png","data":"QUJDRA=="}`), &in); err != nil {
		t.Fatalf("Unmarshal error: %v", err)
	}
	if in.MimeType != "image/png" || in.Data != "QUJDRA==" {
		t.Fatalf("decoded mismatch: got mimeType=%q data=%q", in.MimeType, in.Data)
	}
	if in.IsLazy() {
		t.Fatalf("unexpected lazy InlineData after Unmarshal")
	}
}
