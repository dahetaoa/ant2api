package credential

import (
	"net/http"
	"net/url"
	"sync"
	"time"

	"anti2api-golang/refactor/internal/config"
)

var (
	oauthHTTPClient     *http.Client
	oauthHTTPClientOnce sync.Once
)

func getOAuthHTTPClient() *http.Client {
	oauthHTTPClientOnce.Do(func() {
		cfg := config.Get()

		timeout := time.Duration(cfg.TimeoutMs) * time.Millisecond
		if cfg.TimeoutMs <= 0 {
			timeout = 0
		}

		transport := &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			IdleConnTimeout:       90 * time.Second,
			ResponseHeaderTimeout: timeout,
			ForceAttemptHTTP2:     false,
		}

		if cfg.Proxy != "" {
			if proxyURL, err := url.Parse(cfg.Proxy); err == nil {
				transport.Proxy = http.ProxyURL(proxyURL)
			}
		}

		oauthHTTPClient = &http.Client{
			Transport: transport,
			Timeout:   timeout,
		}
	})

	return oauthHTTPClient
}
