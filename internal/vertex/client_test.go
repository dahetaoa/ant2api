package vertex

import (
	"net/http"
	"testing"
	"time"

	"anti2api-golang/refactor/internal/config"
)

func TestNewClient_UsesConfigTimeoutForResponseHeaders(t *testing.T) {
	cfg := config.Get()
	c := NewClient()

	transport, ok := c.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", c.httpClient.Transport)
	}

	want := time.Duration(cfg.TimeoutMs) * time.Millisecond
	if cfg.TimeoutMs <= 0 {
		want = 0
	}

	if transport.ResponseHeaderTimeout != want {
		t.Fatalf("ResponseHeaderTimeout=%v want %v", transport.ResponseHeaderTimeout, want)
	}
	if c.httpClient.Timeout != want {
		t.Fatalf("Client.Timeout=%v want %v", c.httpClient.Timeout, want)
	}
}

