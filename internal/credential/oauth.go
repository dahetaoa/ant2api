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
	data := url.Values{
		"code":          {code},
		"client_id":     {config.ClientID()},
		"client_secret": {config.ClientSecret()},
		"redirect_uri":  {redirectURI},
		"grant_type":    {"authorization_code"},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("token exchange failed: " + string(body))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, err
	}

	return &tokenResp, nil
}

func RefreshToken(account *Account) error {
	if account.RefreshToken == "" {
		return errors.New("no refresh token")
	}

	data := url.Values{
		"client_id":     {config.ClientID()},
		"client_secret": {config.ClientSecret()},
		"grant_type":    {"refresh_token"},
		"refresh_token": {account.RefreshToken},
	}

	resp, err := http.PostForm("https://oauth2.googleapis.com/token", data)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return errors.New("token refresh failed")
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

	logger.Info("Token refreshed for %s", account.Email)

	return nil
}

func GetUserInfo(accessToken string) (*UserInfo, error) {
	req, err := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to get user info")
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
		return "", "", errors.New("no code in URL")
	}
	return code, state, nil
}
