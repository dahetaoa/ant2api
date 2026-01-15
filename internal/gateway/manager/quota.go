package manager

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/pkg/id"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/vertex"
)

const (
	quotaGroupClaudeGPT       = "Claude/GPT"
	quotaGroupGemini3Pro      = "Gemini 3 Pro"
	quotaGroupGemini3Flash    = "Gemini 3 Flash"
	quotaGroupGemini3ProImage = "Gemini 3 Pro Image"
	quotaGroupGemini25        = "Gemini 2.5 Pro/Flash/Lite"
)

type QuotaGroup struct {
	GroupName         string   `json:"groupName"`
	RemainingFraction *float64 `json:"remainingFraction,omitempty"`
	ResetTime         string   `json:"resetTime,omitempty"`
	ModelList         []string `json:"modelList,omitempty"`
}

type AccountQuota struct {
	SessionID string       `json:"sessionId"`
	Groups    []QuotaGroup `json:"groups"`
	FetchedAt time.Time    `json:"fetchedAt"`
}

type modelQuota struct {
	RemainingFraction *float64
	ResetTime         string
}

func FetchAccountQuota(ctx context.Context, account credential.Account) (*AccountQuota, error) {
	projectID := strings.TrimSpace(account.ProjectID)
	if projectID == "" {
		projectID = id.ProjectID()
	}
	accessToken := strings.TrimSpace(account.AccessToken)
	if accessToken == "" {
		return nil, errors.New("缺少 access_token")
	}

	vm, err := vertex.FetchAvailableModels(ctx, projectID, accessToken)
	if err != nil {
		return nil, err
	}

	groups := groupQuotaGroups(vm.Models)
	return &AccountQuota{
		SessionID: account.SessionID,
		Groups:    groups,
		FetchedAt: time.Now(),
	}, nil
}

func groupQuotaKey(modelID string) string {
	m := strings.ToLower(modelutil.CanonicalModelID(modelID))
	switch {
	case strings.HasPrefix(m, "claude-") || strings.HasPrefix(m, "gpt-"):
		return quotaGroupClaudeGPT
	case strings.HasPrefix(m, "gemini-3-pro-high"):
		return quotaGroupGemini3Pro
	case strings.HasPrefix(m, "gemini-3-flash"):
		return quotaGroupGemini3Flash
	case strings.HasPrefix(m, "gemini-3-pro-image"):
		return quotaGroupGemini3ProImage
	default:
		return quotaGroupGemini25
	}
}

func groupQuotaGroups(models map[string]any) []QuotaGroup {
	groups := make(map[string]*QuotaGroup, 5)

	for modelID, modelData := range models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}

		groupName := groupQuotaKey(modelID)
		g, ok := groups[groupName]
		if !ok {
			g = &QuotaGroup{GroupName: groupName}
			groups[groupName] = g
		}

		g.ModelList = append(g.ModelList, modelutil.CanonicalModelID(modelID))
		mq := parseModelQuota(modelData)
		if g.RemainingFraction == nil && mq.RemainingFraction != nil {
			v := *mq.RemainingFraction
			g.RemainingFraction = &v
		}
		if g.ResetTime == "" && mq.ResetTime != "" {
			g.ResetTime = mq.ResetTime
		}
	}

	order := []string{quotaGroupClaudeGPT, quotaGroupGemini3Pro, quotaGroupGemini3Flash, quotaGroupGemini3ProImage, quotaGroupGemini25}
	out := make([]QuotaGroup, 0, len(groups))

	seen := make(map[string]struct{}, len(groups))
	for _, name := range order {
		g, ok := groups[name]
		if !ok {
			continue
		}
		sort.Strings(g.ModelList)
		out = append(out, *g)
		seen[name] = struct{}{}
	}

	// Safety: stable output even if backend adds new groups.
	rest := make([]string, 0, len(groups))
	for name := range groups {
		if _, ok := seen[name]; ok {
			continue
		}
		rest = append(rest, name)
	}
	sort.Strings(rest)
	for _, name := range rest {
		g := groups[name]
		sort.Strings(g.ModelList)
		out = append(out, *g)
	}

	return out
}

func parseModelQuota(v any) modelQuota {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return modelQuota{}
	}

	if mq := parseModelQuotaMap(m); mq.RemainingFraction != nil || mq.ResetTime != "" {
		return mq
	}

	if qi, ok := m["quotaInfo"].(map[string]any); ok && qi != nil {
		if mq := parseModelQuotaMap(qi); mq.RemainingFraction != nil || mq.ResetTime != "" {
			return mq
		}
	}

	if q, ok := m["quota"].(map[string]any); ok && q != nil {
		return parseModelQuotaMap(q)
	}

	return modelQuota{}
}

func parseModelQuotaMap(m map[string]any) modelQuota {
	out := modelQuota{}

	if rf, ok := anyToFloat64(m["remainingFraction"]); ok {
		out.RemainingFraction = &rf
	}
	if rt, ok := m["resetTime"].(string); ok {
		out.ResetTime = strings.TrimSpace(rt)
	}
	return out
}

func anyToFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return clamp01(n), true
	case float32:
		return clamp01(float64(n)), true
	case int:
		return clamp01(float64(n)), true
	case int64:
		return clamp01(float64(n)), true
	case uint64:
		return clamp01(float64(n)), true
	case string:
		s := strings.TrimSpace(n)
		if s == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, false
		}
		return clamp01(f), true
	default:
		return 0, false
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
