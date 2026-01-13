package modelutil

const (
	// ClaudeMaxOutputTokens 是 Claude 系列模型在项目历史行为下的固定 maxOutputTokens 上限。
	ClaudeMaxOutputTokens = 64000
	// GeminiMaxOutputTokens 是 Gemini 系列模型在项目历史行为下的固定 maxOutputTokens 上限。
	GeminiMaxOutputTokens = 65535

	// DefaultClaudeThinkingBudgetTokens 是 Claude thinking 在未提供预算时的默认 thinkingBudget。
	DefaultClaudeThinkingBudgetTokens = 32000

	// ThinkingBudgetHeadroomTokens 是为了避免 thinkingBudget 与 maxOutputTokens 冲突而预留的安全余量。
	ThinkingBudgetHeadroomTokens = 1024
	// ThinkingBudgetMinTokens 是项目对 thinkingBudget 的最小保守值（用于下限保护）。
	ThinkingBudgetMinTokens = 1024

	// ThinkingMaxOutputTokensOverheadTokens 用于当仅给出 thinkingBudget 时，为 maxOutputTokens 预留的额外输出空间。
	ThinkingMaxOutputTokensOverheadTokens = 4096

	// ClaudeThinkingEffortLowTokens/MediumTokens 是 OpenAI 兼容 reasoning_effort 映射到 Claude budget 的历史值。
	ClaudeThinkingEffortLowTokens    = 1024
	ClaudeThinkingEffortMediumTokens = 4096
	ClaudeThinkingEffortHighTokens   = DefaultClaudeThinkingBudgetTokens
)
