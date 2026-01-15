package credential

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/config"
	"anti2api-golang/refactor/internal/logger"
)

var OAuthScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/experimentsandconfigs",
}

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
}

type UserInfo struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func BuildAuthURL(redirectURI, state string) string {
	params := url.Values{
		"access_type":   {"offline"},
		"client_id":     {config.ClientID()},
		"prompt":        {"consent"},
		"redirect_uri":  {redirectURI},
		"response_type": {"code"},
		"scope":         {strings.Join(OAuthScopes, " ")},
		"state":         {state},
	}
	return "https://accounts.google.com/o/oauth2/v2/auth?" + params.Encode()
}

func ExchangeCodeForToken(code, redirectURI string) (*TokenResponse, error) {
	code = strings.TrimSpace(code)
	redirectURI = strings.TrimSpace(redirectURI)
	if code == "" {
		return nil, errors.New("回调 URL 中缺少 code 参数")
	}
	if redirectURI == "" {
		return nil, errors.New("缺少 redirect_uri")
	}

	data := url.Values{
		"code":          {code},
		"client_id":     {config.ClientID()},
		"client_secret": {config.ClientSecret()},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}

	cfg := config.Get()
	req.Host = "oauth2.googleapis.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", cfg.UserAgent)

	resp, err := getOAuthHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		logger.Warn("OAuth 交换 token 失败（HTTP %d）：%s", resp.StatusCode, string(body))
		return nil, errors.New("交换 Token 失败：请确认授权码未过期，且 redirect_uri 与发起授权时一致")
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func RefreshToken(account *Account) error {
	if account.RefreshToken == "" {
		return errors.New("缺少 refresh_token")
	}

	data := url.Values{
		"client_id":     {config.ClientID()},
		"client_secret": {config.ClientSecret()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {account.RefreshToken},
	}

	req, err := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}

	cfg := config.Get()
	req.Host = "oauth2.googleapis.com"
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", cfg.UserAgent)

	resp, err := getOAuthHTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		logger.Warn("OAuth 刷新 token 失败（HTTP %d）：%s", resp.StatusCode, string(body))
		return errors.New("刷新 Token 失败")
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return err
	}

	account.AccessToken = tokenResp.AccessToken
	account.ExpiresIn = tokenResp.ExpiresIn
	account.Timestamp = time.Now().UnixMilli()
	if tokenResp.RefreshToken != "" {
		account.RefreshToken = tokenResp.RefreshToken
	}

	logger.Info("已刷新 Token：%s", account.Email)

	return nil
}

func GetUserInfo(accessToken string) (*UserInfo, error) {
	accessToken = strings.TrimSpace(accessToken)
	if accessToken == "" {
		return nil, errors.New("缺少 access_token")
	}

	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Host = "www.googleapis.com"
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", config.Get().UserAgent)

	resp, err := getOAuthHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		logger.Warn("获取用户信息失败（HTTP %d）：%s", resp.StatusCode, string(body))
		return nil, errors.New("获取用户信息失败")
	}

	var userInfo UserInfo
	if err := json.Unmarshal(body, &userInfo); err != nil {
		return nil, err
	}
	return &userInfo, nil
}

func ParseOAuthURL(oauthURL string) (code, state string, err error) {
	u, err := url.Parse(oauthURL)
	if err != nil {
		return "", "", err
	}

	query := u.Query()
	code = query.Get("code")
	state = query.Get("state")
	if code == "" {
		return "", "", errors.New("回调 URL 中缺少 code 参数")
	}
	return code, state, nil
}
