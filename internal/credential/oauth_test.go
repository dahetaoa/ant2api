package credential

import "testing"

func TestParseOAuthURL_AllowsMissingScheme(t *testing.T) {
	code, state, err := ParseOAuthURL("localhost:8045/oauth-callback?state=s1&code=c1")
	if err != nil {
		t.Fatalf("ParseOAuthURL error: %v", err)
	}
	if code != "c1" || state != "s1" {
		t.Fatalf("unexpected parse result: code=%q state=%q", code, state)
	}
}

func TestParseOAuthURL_AllowsPathOnly(t *testing.T) {
	code, state, err := ParseOAuthURL("/oauth-callback?state=s2&code=c2")
	if err != nil {
		t.Fatalf("ParseOAuthURL error: %v", err)
	}
	if code != "c2" || state != "s2" {
		t.Fatalf("unexpected parse result: code=%q state=%q", code, state)
	}
}

func TestParseOAuthURL_MissingCode(t *testing.T) {
	_, _, err := ParseOAuthURL("http://localhost:8045/oauth-callback?state=s3")
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}
