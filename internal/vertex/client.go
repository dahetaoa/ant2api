package vertex

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/logger"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type Client struct {
	httpClient *http.Client
	config     *config.Config
}

type APIError struct {
	Status       int
	Message      string
	RetryDelay   time.Duration
	DisableToken bool
}

func (e *APIError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.Status, e.Message)
}

func NewClient() *Client {
	cfg := config.Get()

	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     false,
	}

	if cfg.Proxy != "" {
		proxyURL, err := url.Parse(cfg.Proxy)
		if err == nil {
			transport.Proxy = http.ProxyURL(proxyURL)
		}
	}

	return &Client{
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   time.Duration(cfg.TimeoutMs) * time.Millisecond,
		},
		config: cfg,
	}
}

func (c *Client) BuildHeaders(accessToken string, endpoint config.Endpoint) http.Header {
	return http.Header{
		"Host":            {endpoint.Host},
		"User-Agent":      {c.config.UserAgent},
		"Authorization":   {"Bearer " + accessToken},
		"Content-Type":    {"application/json"},
		"Accept-Encoding": {"gzip"},
	}
}

func (c *Client) BuildStreamHeaders(accessToken string, endpoint config.Endpoint) http.Header {
	return http.Header{
		"Host":          {endpoint.Host},
		"User-Agent":    {c.config.UserAgent},
		"Authorization": {"Bearer " + accessToken},
		"Content-Type":  {"application/json"},
	}
}

func (c *Client) SendRequest(ctx context.Context, req *Request, accessToken string) (*Response, error) {
	endpoint := config.GetEndpointManager().GetActiveEndpoint()
	reqURL := endpoint.NoStreamURL()

	body, err := jsonpkg.Marshal(req)
	if err != nil {
		return nil, err
	}

	logger.BackendRequest(http.MethodPost, reqURL, body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for key, values := range c.BuildHeaders(accessToken, endpoint) {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}

	startTime := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	logger.BackendResponse(resp.StatusCode, time.Since(startTime), string(respBody))

	if resp.StatusCode != http.StatusOK {
		return nil, ExtractErrorDetails(resp, respBody)
	}

	var out Response
	if err := jsonpkg.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}

	logger.BackendResponse(resp.StatusCode, time.Since(startTime), &out)

	return &out, nil
}

func (c *Client) SendStreamRequest(ctx context.Context, req *Request, accessToken string) (*http.Response, error) {
	endpoint := config.GetEndpointManager().GetActiveEndpoint()
	reqURL := endpoint.StreamURL()

	body, err := jsonpkg.Marshal(req)
	if err != nil {
		return nil, err
	}

	logger.BackendRequest(http.MethodPost, reqURL, body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for key, values := range c.BuildStreamHeaders(accessToken, endpoint) {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}
	startTime := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()

		var reader io.Reader = resp.Body
		if resp.Header.Get("Content-Encoding") == "gzip" {
			gzReader, err := gzip.NewReader(resp.Body)
			if err != nil {
				return nil, &APIError{Status: resp.StatusCode, Message: "failed to decompress response"}
			}
			defer gzReader.Close()
			reader = gzReader
		}
		respBody, _ := io.ReadAll(reader)
		logger.BackendResponse(resp.StatusCode, time.Since(startTime), string(respBody))
		return nil, ExtractErrorDetails(resp, respBody)
	}
	_ = startTime

	return resp, nil
}

func ExtractErrorDetails(resp *http.Response, body []byte) *APIError {
	apiErr := &APIError{Status: resp.StatusCode, Message: "Unknown error"}

	var errorResp struct {
		Error struct {
			Code    any    `json:"code"`
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				Type       string `json:"@type"`
				RetryDelay string `json:"retryDelay"`
			} `json:"details"`
		} `json:"error"`
	}

	if jsonpkg.Unmarshal(body, &errorResp) == nil {
		apiErr.Message = errorResp.Error.Message

		switch v := errorResp.Error.Code.(type) {
		case string:
			switch strings.ToUpper(v) {
			case "RESOURCE_EXHAUSTED":
				apiErr.Status = http.StatusTooManyRequests
			case "INTERNAL":
				apiErr.Status = http.StatusInternalServerError
			case "UNAUTHENTICATED":
				apiErr.Status = http.StatusUnauthorized
				apiErr.DisableToken = true
			}
		case float64:
			apiErr.Status = int(v)
		}

		for _, detail := range errorResp.Error.Details {
			if strings.Contains(detail.Type, "RetryInfo") {
				re := regexp.MustCompile(`(\d+(?:\.\d+)?)s`)
				if matches := re.FindStringSubmatch(detail.RetryDelay); len(matches) > 1 {
					if seconds, err := strconv.ParseFloat(matches[1], 64); err == nil {
						apiErr.RetryDelay = time.Duration(seconds * float64(time.Second))
					}
				}
			}
		}
	}

	return apiErr
}

func (c *Client) WithRetry(ctx context.Context, operation func() error) error {
	var lastErr error

	for attempt := 0; attempt < c.config.RetryMaxAttempts; attempt++ {
		err := operation()
		if err == nil {
			return nil
		}

		lastErr = err
		apiErr, ok := err.(*APIError)
		if !ok {
			return err
		}

		if apiErr.Status == http.StatusUnauthorized {
			return err
		}

		shouldRetry := false
		for _, code := range c.config.RetryStatusCodes {
			if apiErr.Status == code {
				shouldRetry = true
				break
			}
		}

		if !shouldRetry || attempt == c.config.RetryMaxAttempts-1 {
			return err
		}

		delay := apiErr.RetryDelay
		if delay == 0 {
			ms := min(1000*(attempt+1), 5000)
			delay = time.Duration(ms) * time.Millisecond
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
	}

	return lastErr
}

var apiClient *Client

func GetClient() *Client {
	if apiClient == nil {
		apiClient = NewClient()
	}
	return apiClient
}

func GenerateContent(ctx context.Context, req *Request, accessToken string) (*Response, error) {
	client := GetClient()
	var result *Response
	var err error

	retryErr := client.WithRetry(ctx, func() error {
		result, err = client.SendRequest(ctx, req, accessToken)
		return err
	})
	if retryErr != nil {
		return nil, retryErr
	}
	return result, nil
}

func GenerateContentStream(ctx context.Context, req *Request, accessToken string) (*http.Response, error) {
	client := GetClient()
	var result *http.Response
	var err error

	retryErr := client.WithRetry(ctx, func() error {
		result, err = client.SendStreamRequest(ctx, req, accessToken)
		return err
	})
	if retryErr != nil {
		return nil, retryErr
	}
	return result, nil
}

type AvailableModelsResponse struct {
	Models map[string]any `json:"models"`
}

func FetchAvailableModels(ctx context.Context, project, accessToken string) (*AvailableModelsResponse, error) {
	client := GetClient()
	endpoint := config.GetEndpointManager().GetActiveEndpoint()
	urlStr := endpoint.FetchAvailableModelsURL()

	body, err := jsonpkg.Marshal(map[string]string{"project": project})
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, urlStr, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	for key, values := range client.BuildHeaders(accessToken, endpoint) {
		for _, value := range values {
			httpReq.Header.Add(key, value)
		}
	}
	logger.BackendRequest(httpReq.Method, httpReq.URL.String(), body)

	startTime := time.Now()
	resp, err := client.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	respBody, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	logger.BackendResponse(resp.StatusCode, time.Since(startTime), string(respBody))

	if resp.StatusCode != http.StatusOK {
		return nil, ExtractErrorDetails(resp, respBody)
	}

	var out AvailableModelsResponse
	if err := jsonpkg.Unmarshal(respBody, &out); err != nil {
		return nil, err
	}
	logger.BackendResponse(resp.StatusCode, time.Since(startTime), &out)
	return &out, nil
}

func IsRetryableError(err error) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}

	for _, code := range config.Get().RetryStatusCodes {
		if apiErr.Status == code {
			return true
		}
	}
	return false
}

func ShouldDisableToken(err error) bool {
	apiErr, ok := err.(*APIError)
	if !ok {
		return false
	}
	return apiErr.DisableToken
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
