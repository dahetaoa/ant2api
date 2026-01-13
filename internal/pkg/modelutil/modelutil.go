package modelutil

import (
	"sort"
	"strconv"
	"strings"

	"anti2api-golang/refactor/internal/vertex"
)

// CanonicalModelID 返回用于内部判定/路由的模型 ID：
// - 去除首尾空白
// - 去除 "models/" 前缀（Gemini 兼容）
// 注意：该函数不强制转小写；若用于前缀判断请配合 strings.ToLower。
func CanonicalModelID(model string) string {
	m := strings.TrimSpace(model)
	m = strings.TrimPrefix(m, "models/")
	return strings.TrimSpace(m)
}

func canonicalLower(model string) string {
	return strings.ToLower(CanonicalModelID(model))
}

// BackendModelID 将对外暴露的（可能包含虚拟前缀/别名的）model 映射为发送到 Vertex 的后端 model id。
// 若无需映射，则返回规范化后的模型 ID 本身。
func BackendModelID(model string) string {
	// 先处理已知的虚拟模型映射（可能会返回不同的后端 id）。
	if _, backendModel, ok := Gemini3FlashThinkingConfig(model); ok {
		return backendModel
	}
	if _, backendModel, ok := ClaudeOpus45ThinkingConfig(model); ok {
		return backendModel
	}
	// 默认仅做 canonical 化（去掉 models/ 等）。
	return CanonicalModelID(model)
}

// IsClaude 判断是否为 Claude 系列模型。
func IsClaude(model string) bool { return strings.HasPrefix(canonicalLower(model), "claude-") }

// IsGemini 判断是否为 Gemini 系列模型。
func IsGemini(model string) bool { return strings.HasPrefix(canonicalLower(model), "gemini-") }

// IsGemini3 判断是否为 Gemini 3 系列模型（含 gemini-3-* 与 gemini-3）。
func IsGemini3(model string) bool {
	m := canonicalLower(model)
	return strings.HasPrefix(m, "gemini-3-") || strings.HasPrefix(m, "gemini-3")
}

// IsGemini25 判断是否为 Gemini 2.5 系列模型。
func IsGemini25(model string) bool {
	m := canonicalLower(model)
	return strings.HasPrefix(m, "gemini-2.5-") || strings.HasPrefix(m, "gemini-2.5")
}

// IsClaudeThinking 判断是否为 Claude 的 “thinking” 变体。
// 兼容诸如 "claude-xxx-thinking-latest" 这类带后缀的名称。
func IsClaudeThinking(model string) bool {
	m := canonicalLower(model)
	if !strings.HasPrefix(m, "claude-") {
		return false
	}
	return strings.HasSuffix(m, "-thinking") || strings.Contains(m, "-thinking-")
}

// IsImageModel 以项目现有约定判断是否为图像相关模型（保持历史逻辑：包含 "image" 即视为图像模型）。
func IsImageModel(model string) bool { return strings.Contains(canonicalLower(model), "image") }

// ForcedThinkingConfig 返回由模型名称强制决定的 ThinkingConfig（忽略客户端参数）。
// 目前包含：
// - Gemini 3 Flash（含虚拟 "-thinking"）
// - Claude Sonnet 4.5 / Claude Opus 4.5（含虚拟映射）
func ForcedThinkingConfig(model string) (*vertex.ThinkingConfig, bool) {
	if level, _, ok := Gemini3FlashThinkingConfig(model); ok {
		if level == "high" {
			return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high", ThinkingBudget: 0}, true
		}
		// gemini-3-flash（非 "-thinking"）：强制 thinkingBudget=0。
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: 0}, true
	}
	if budget, ok := ClaudeSonnet45ThinkingBudget(model); ok {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget}, true
	}
	if budget, _, ok := ClaudeOpus45ThinkingConfig(model); ok {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget}, true
	}
	return nil, false
}

// ThinkingConfigFromOpenAI 根据 OpenAI 兼容入参（reasoning_effort）生成 Vertex ThinkingConfig。
// 该逻辑为项目历史行为的单一事实来源（SSoT）。
func ThinkingConfigFromOpenAI(model, reasoningEffort string) *vertex.ThinkingConfig {
	if tc, ok := ForcedThinkingConfig(model); ok {
		return tc
	}

	effort := strings.ToLower(strings.TrimSpace(reasoningEffort))

	// 如果调用方显式选择 Claude “-thinking” 模型且未传 reasoning_effort，则默认开启 thinking。
	if effort == "" && IsClaudeThinking(model) {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: DefaultClaudeThinkingBudgetTokens}
	}

	// Gemini 3（非 Flash）在 OpenAI 兼容语义下默认开启 thinking_level=high。
	if IsGemini3(model) && !IsGemini3Flash(model) {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high", ThinkingBudget: 0}
	}

	if effort == "" {
		return nil
	}

	if IsClaudeThinking(model) || IsGemini25(model) {
		// 支持数字 effort 作为直接预算覆盖（budget-based 模型）。
		if n, err := strconv.Atoi(effort); err == nil && n > 0 {
			return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: n}
		}
		if IsClaudeThinking(model) {
			return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: mapEffortToBudget(effort)}
		}
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: mapGemini25EffortToBudget(effort)}
	}

	return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: effort}
}

// ThinkingConfigFromClaude 根据 Claude/Anthropic 兼容入参（thinking 对象）生成 Vertex ThinkingConfig。
// thinkingType 需为 "enabled" 才会生效。
func ThinkingConfigFromClaude(model, thinkingType string, budget, budgetTokens int) *vertex.ThinkingConfig {
	if tc, ok := ForcedThinkingConfig(model); ok {
		return tc
	}
	if strings.ToLower(strings.TrimSpace(thinkingType)) != "enabled" {
		return nil
	}

	tc := &vertex.ThinkingConfig{IncludeThoughts: true}
	if IsClaude(model) {
		// Claude thinking 模型需要非零 thinkingBudget 才能输出 thoughts。
		b := budget
		if b <= 0 {
			b = budgetTokens
		}
		if b <= 0 {
			b = DefaultClaudeThinkingBudgetTokens
		}
		tc.ThinkingBudget = b
		return tc
	}

	if IsGemini3(model) {
		// Gemini 3（非 Flash）在请求 thinking 时强制使用 thinking_level=high。
		tc.ThinkingLevel = "high"
		tc.ThinkingBudget = 0
		return tc
	}

	// 其他模型：优先使用 budget/budget_tokens（若为 0 则不写出）。
	b := budget
	if b <= 0 {
		b = budgetTokens
	}
	if b > 0 {
		tc.ThinkingBudget = b
	}
	return tc
}

// ThinkingConfigFromGemini 根据 Gemini generationConfig.thinkingConfig 生成 Vertex ThinkingConfig。
// includeThoughts=false 时返回 nil（除非模型强制 thinking）。
func ThinkingConfigFromGemini(model string, includeThoughts bool, thinkingBudget int, thinkingLevel string) *vertex.ThinkingConfig {
	if tc, ok := ForcedThinkingConfig(model); ok {
		return tc
	}
	if !includeThoughts {
		return nil
	}

	tc := &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: thinkingBudget, ThinkingLevel: thinkingLevel}

	// Gemini 3（非 Flash）在开启 thinking 时强制 thinking_level=high，且预算为 0。
	if IsGemini3(model) && !IsGemini3Flash(model) {
		tc.ThinkingLevel = "high"
		tc.ThinkingBudget = 0
	}

	// Claude：开启 thinking 时必须提供非零预算；thinkingLevel 需清空。
	if IsClaude(model) {
		tc.ThinkingLevel = ""
		if tc.ThinkingBudget <= 0 {
			tc.ThinkingBudget = DefaultClaudeThinkingBudgetTokens
		}
	}

	return tc
}

// BuildSortedModelIDs 将 Vertex 返回的 models map key 规范化、去重、注入虚拟模型，并按字典序排序返回。
func BuildSortedModelIDs(models map[string]any) []string {
	ids := make([]string, 0, len(models)+2)
	seen := make(map[string]struct{}, len(models)+2)

	hasGemini3Flash := false
	hasClaudeOpus45 := false
	hasClaudeOpus45Thinking := false

	for k := range models {
		idv := strings.TrimSpace(k)
		if idv == "" {
			continue
		}
		if strings.EqualFold(idv, "gemini-3-flash") {
			hasGemini3Flash = true
		}
		lower := strings.ToLower(idv)
		if strings.HasPrefix(lower, "claude-opus-4-5-thinking") {
			hasClaudeOpus45Thinking = true
		} else if strings.HasPrefix(lower, "claude-opus-4-5") {
			hasClaudeOpus45 = true
		}

		if _, ok := seen[idv]; ok {
			continue
		}
		seen[idv] = struct{}{}
		ids = append(ids, idv)
	}

	// Virtual model injection: only add gemini-3-flash-thinking when gemini-3-flash exists.
	if hasGemini3Flash {
		const virtual = "gemini-3-flash-thinking"
		if _, ok := seen[virtual]; !ok {
			ids = append(ids, virtual)
		}
	}
	// Virtual model injection: add claude-opus-4-5 when only claude-opus-4-5-thinking* exists.
	if hasClaudeOpus45Thinking && !hasClaudeOpus45 {
		const virtual = "claude-opus-4-5"
		if _, ok := seen[virtual]; !ok {
			ids = append(ids, virtual)
		}
	}

	sort.Strings(ids)
	return ids
}

func mapEffortToBudget(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low":
		return ClaudeThinkingEffortLowTokens
	case "medium":
		return ClaudeThinkingEffortMediumTokens
	case "high", "max":
		return ClaudeThinkingEffortHighTokens
	default:
		return ClaudeThinkingEffortHighTokens
	}
}

func mapGemini25EffortToBudget(effort string) int {
	_ = effort
	// Keep conservative by default: Gemini 2.5 examples commonly use small budgets (e.g. 1024).
	return ClaudeThinkingEffortLowTokens
}
