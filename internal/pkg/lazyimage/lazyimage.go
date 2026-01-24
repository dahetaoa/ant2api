package lazyimage

import "bytes"

var (
	dataImagePrefix = []byte("data:image/")
	base64Marker    = []byte(";base64,")
)

// Extract scans a JSON body and returns references to base64 image data segments.
//
// It is intentionally lenient: malformed patterns are skipped without returning an error.
// The returned ImageRef instances reference the provided body slice; callers must ensure
// the body remains alive while ImageRefs are in use.
func Extract(body []byte) []*ImageRef {
	if len(body) == 0 || !bytes.Contains(body, dataImagePrefix) {
		return nil
	}

	out := make([]*ImageRef, 0, 4)
	searchStart := 0
	for {
		i := bytes.Index(body[searchStart:], dataImagePrefix)
		if i < 0 {
			break
		}
		start := searchStart + i

		mimeStart := start + len("data:")
		if mimeStart >= len(body) {
			break
		}
		markerRel := bytes.Index(body[mimeStart:], base64Marker)
		if markerRel < 0 {
			// Continue searching for other occurrences.
			searchStart = start + len(dataImagePrefix)
			continue
		}
		markerAbs := mimeStart + markerRel
		mimeTypeBytes := body[mimeStart:markerAbs]
		if len(mimeTypeBytes) == 0 {
			searchStart = start + len(dataImagePrefix)
			continue
		}

		dataStart := markerAbs + len(base64Marker)
		if dataStart > len(body) {
			searchStart = start + len(dataImagePrefix)
			continue
		}

		dataEnd, ok := scanBase64End(body, dataStart)
		if ok {
			ref := newImageRef(body, string(mimeTypeBytes), dataStart, dataEnd)
			out = append(out, ref)
		}

		// Continue searching after this "data:image/" to avoid re-matching inside the same segment.
		searchStart = start + len(dataImagePrefix)
	}
	return out
}

func scanBase64End(body []byte, dataStart int) (end int, ok bool) {
	end = dataStart
	for end < len(body) {
		c := body[end]
		if isBase64Byte(c) || c == '=' {
			end++
			continue
		}
		// A backslash likely indicates JSON escaping (e.g. "\/") which would require unescaping.
		// Skip optimization for such inputs to avoid producing incorrect output.
		if c == '\\' {
			return 0, false
		}
		break
	}
	if end < dataStart {
		return 0, false
	}
	return end, true
}

func isBase64Byte(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z':
		return true
	case c >= 'a' && c <= 'z':
		return true
	case c >= '0' && c <= '9':
		return true
	case c == '+' || c == '/' || c == '-' || c == '_':
		return true
	default:
		return false
	}
}
