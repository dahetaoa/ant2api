package credential

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"anti2api-golang/refactor/internal/config"
)

type loadCodeAssistResponse struct {
	CloudAICompanionProject string `json:"cloudaicompanionProject"`
}

func FetchProjectID(accessToken string) (string, error) {
	projectID, err := fetchProjectIDFromLoadCodeAssist(accessToken)
	if err == nil && strings.TrimSpace(projectID) != "" {
		return strings.TrimSpace(projectID), nil
	}

	projectID, rmErr := fetchProjectIDFromResourceManager(accessToken)
	if rmErr == nil && strings.TrimSpace(projectID) != "" {
		return strings.TrimSpace(projectID), nil
	}

	if err == nil {
		err = rmErr
	}
	if err == nil {
		err = errors.New("未能获取 projectId")
	}
	return "", err
}

func fetchProjectIDFromLoadCodeAssist(accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("缺少 access_token")
	}

	reqBody := []byte(`{"metadata":{"ideType":"ANTIGRAVITY"}}`)
	req, err := http.NewRequest(http.MethodPost, "https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:loadCodeAssist", bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Host = "daily-cloudcode-pa.sandbox.googleapis.com"

	cfg := config.Get()
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", cfg.UserAgent)
	req.Header.Set("Content-Type", "application/json")

	resp, err := getOAuthHTTPClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("loadCodeAssist 请求失败（HTTP %d）", resp.StatusCode)
	}

	var decoded loadCodeAssistResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return "", err
	}

	return decoded.CloudAICompanionProject, nil
}

type resourceManagerProjectsResponse struct {
	Projects      []resourceManagerProject `json:"projects"`
	NextPageToken string                   `json:"nextPageToken"`
}

type resourceManagerProject struct {
	ProjectID      string `json:"projectId"`
	Name           string `json:"name"`
	LifecycleState string `json:"lifecycleState"`
}

func fetchProjectIDFromResourceManager(accessToken string) (string, error) {
	if strings.TrimSpace(accessToken) == "" {
		return "", errors.New("缺少 access_token")
	}

	cfg := config.Get()
	pageToken := ""

	for pages := 0; pages < 5; pages++ {
		reqURL, err := url.Parse("https://cloudresourcemanager.googleapis.com/v1/projects")
		if err != nil {
			return "", err
		}

		q := reqURL.Query()
		if pageToken != "" {
			q.Set("pageToken", pageToken)
		}
		reqURL.RawQuery = q.Encode()

		req, err := http.NewRequest(http.MethodGet, reqURL.String(), nil)
		if err != nil {
			return "", err
		}
		req.Host = "cloudresourcemanager.googleapis.com"
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("User-Agent", cfg.UserAgent)

		resp, err := getOAuthHTTPClient().Do(req)
		if err != nil {
			return "", err
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if readErr != nil {
			return "", readErr
		}

		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("Resource Manager 请求失败（HTTP %d）", resp.StatusCode)
		}

		var decoded resourceManagerProjectsResponse
		if err := json.Unmarshal(body, &decoded); err != nil {
			return "", err
		}

		selected := selectProjectID(decoded.Projects)
		if selected != "" {
			return selected, nil
		}

		if decoded.NextPageToken == "" {
			break
		}
		pageToken = decoded.NextPageToken
	}

	return "", errors.New("未找到可用的 ACTIVE 项目")
}

func selectProjectID(projects []resourceManagerProject) string {
	var firstActive string

	for _, p := range projects {
		if strings.ToUpper(strings.TrimSpace(p.LifecycleState)) != "ACTIVE" {
			continue
		}
		projectID := strings.TrimSpace(p.ProjectID)
		if projectID == "" {
			continue
		}

		if firstActive == "" {
			firstActive = projectID
		}

		name := strings.ToLower(strings.TrimSpace(p.Name))
		if strings.Contains(name, "default") || strings.Contains(strings.ToLower(projectID), "default") {
			return projectID
		}
	}

	return firstActive
}
