package vertex

import (
	"bytes"
	"io"
	"testing"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/pkg/lazyimage"
)

func BenchmarkEncodeRequest_LazyImages_3x1MB(b *testing.B) {
	const imgSize = 1 << 20
	body := buildBodyWithImages(imgSize)
	refs := lazyimage.Extract(body)
	if len(refs) < 3 {
		b.Fatalf("expected >=3 refs, got %d", len(refs))
	}

	req := &Request{
		Project:   "p",
		Model:     "m",
		RequestID: "r",
		Request: InnerReq{
			SessionID: "s",
			Contents: []Content{
				{
					Role: "user",
					Parts: []Part{
						{InlineData: NewInlineDataFromRef(refs[0])},
						{InlineData: NewInlineDataFromRef(refs[1])},
						{InlineData: NewInlineDataFromRef(refs[2])},
					},
				},
			},
		},
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc := jsonpkg.NewEncoder(io.Discard)
		if err := enc.Encode(req); err != nil {
			b.Fatalf("Encode error: %v", err)
		}
	}
}

func buildBodyWithImages(imgSize int) []byte {
	imgA := bytes.Repeat([]byte("A"), imgSize)
	imgB := bytes.Repeat([]byte("B"), imgSize)
	imgC := bytes.Repeat([]byte("C"), imgSize)

	var out []byte
	out = append(out, `{"a":"data:image/png;base64,`...)
	out = append(out, imgA...)
	out = append(out, `","b":"data:image/png;base64,`...)
	out = append(out, imgB...)
	out = append(out, `","c":"data:image/png;base64,`...)
	out = append(out, imgC...)
	out = append(out, `"}`...)
	return out
}
