package lazyimage

type ImageRef struct {
	src       []byte
	dataStart int
	dataEnd   int
	mimeType  string
	sigKey    string
}

func newImageRef(src []byte, mimeType string, dataStart, dataEnd int) *ImageRef {
	r := &ImageRef{
		src:       src,
		dataStart: dataStart,
		dataEnd:   dataEnd,
		mimeType:  mimeType,
	}
	r.sigKey = signatureKeyFromBytes(r.DataBytes())
	return r
}

func (r *ImageRef) DataBytes() []byte {
	if r == nil || r.dataStart < 0 || r.dataEnd < r.dataStart || r.dataEnd > len(r.src) {
		return nil
	}
	return r.src[r.dataStart:r.dataEnd]
}

func (r *ImageRef) DataString() string { return string(r.DataBytes()) }

func (r *ImageRef) SignatureKey() string { return r.sigKey }

func (r *ImageRef) MimeType() string { return r.mimeType }

func signatureKeyFromBytes(b []byte) string {
	if len(b) > 50 {
		b = b[:50]
	}
	return string(b)
}
