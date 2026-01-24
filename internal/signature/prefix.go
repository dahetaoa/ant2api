package signature

const signaturePrefixLen = 50

func signaturePrefix(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= signaturePrefixLen {
		return s
	}
	return s[:signaturePrefixLen]
}
