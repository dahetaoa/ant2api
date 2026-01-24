package lazyimage

import (
	"bytes"
	"testing"
	"unsafe"
)

func TestExtract_SingleImage_ZeroCopySlice(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,QUJDRA=="}}]}]}`)

	refs := Extract(body)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(refs))
	}
	ref := refs[0]
	if ref == nil {
		t.Fatalf("expected non-nil ref")
	}
	if ref.MimeType() != "image/png" {
		t.Fatalf("mimeType mismatch: got %q want %q", ref.MimeType(), "image/png")
	}

	want := []byte("QUJDRA==")
	got := ref.DataBytes()
	if !bytes.Equal(got, want) {
		t.Fatalf("data mismatch: got %q want %q", got, want)
	}

	start := bytes.Index(body, want)
	if start < 0 {
		t.Fatalf("failed to locate base64 data in body")
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty data")
	}
	if unsafe.Pointer(&got[0]) != unsafe.Pointer(&body[start]) {
		t.Fatalf("expected DataBytes to reference original body (zero-copy)")
	}
}

func TestExtract_MultipleImages(t *testing.T) {
	body := []byte(`{"a":"data:image/jpeg;base64,AAAA","b":"data:image/png;base64,BBBB"}`)
	refs := Extract(body)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0].MimeType() == refs[1].MimeType() {
		t.Fatalf("expected different mimeTypes, got %q and %q", refs[0].MimeType(), refs[1].MimeType())
	}
}

func TestExtract_SkipsEscapedContent(t *testing.T) {
	body := []byte(`{"url":"data:image/png;base64,AA\\/BB"}`)
	refs := Extract(body)
	if len(refs) != 0 {
		t.Fatalf("expected 0 refs, got %d", len(refs))
	}
}

func TestIndex_MatchBase64String(t *testing.T) {
	base64Data := "ABCDEFGHIJKLMNOPQRST" // 20 bytes
	body := []byte(`{"url":"data:image/png;base64,` + base64Data + `"}`)
	idx := NewIndex(body)
	if idx == nil {
		t.Fatalf("expected non-nil index")
	}
	ref := idx.MatchBase64String(base64Data, "image/png")
	if ref == nil {
		t.Fatalf("expected match")
	}
	if ref.SignatureKey() != base64Data {
		t.Fatalf("signatureKey mismatch: got %q want %q", ref.SignatureKey(), base64Data)
	}
}
