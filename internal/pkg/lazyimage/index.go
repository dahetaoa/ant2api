package lazyimage

type Index struct {
	refs  []*ImageRef
	byKey map[string][]*ImageRef
}

func NewIndex(body []byte) *Index {
	refs := Extract(body)
	if len(refs) == 0 {
		return nil
	}
	idx := &Index{refs: refs, byKey: make(map[string][]*ImageRef, len(refs))}
	for _, r := range refs {
		if r == nil {
			continue
		}
		key := r.SignatureKey()
		if key == "" {
			continue
		}
		idx.byKey[key] = append(idx.byKey[key], r)
	}
	return idx
}

func (idx *Index) IsEmpty() bool { return idx == nil || len(idx.refs) == 0 }

// MatchBase64String attempts to match a decoded base64 string (without the data:... prefix)
// to an ImageRef in the original body. It returns nil when no reliable match is found.
func (idx *Index) MatchBase64String(base64Data string, mimeType string) *ImageRef {
	if idx == nil || base64Data == "" {
		return nil
	}
	key := signatureKeyFromString(base64Data)
	candidates := idx.byKey[key]
	if len(candidates) == 0 {
		return nil
	}
	if len(candidates) == 1 {
		r := candidates[0]
		if mimeType != "" && r.mimeType != mimeType {
			return nil
		}
		if len(base64Data) != len(r.DataBytes()) {
			return nil
		}
		return r
	}

	// Disambiguate extremely rare signature-key collisions by checking mimeType and length,
	// then falling back to a full byte-by-byte comparison.
	for _, r := range candidates {
		if r == nil {
			continue
		}
		if mimeType != "" && r.mimeType != mimeType {
			continue
		}
		rb := r.DataBytes()
		if len(base64Data) != len(rb) {
			continue
		}
		if equalStringBytes(base64Data, rb) {
			return r
		}
	}
	return nil
}

func signatureKeyFromString(s string) string {
	if len(s) > 50 {
		s = s[:50]
	}
	return s
}

func equalStringBytes(s string, b []byte) bool {
	if len(s) != len(b) {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] != b[i] {
			return false
		}
	}
	return true
}
