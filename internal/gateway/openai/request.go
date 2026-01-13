package openai

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// Stop 为 OpenAI 兼容字段：当前未映射到 Vertex generationConfig.stopSequences（保持历史行为）。
	Stop  []string `json:"stop,omitempty"`
	Tools []Tool   `json:"tools,omitempty"`
	// ToolChoice 为 OpenAI 兼容字段：当前未实现 tool_choice 语义（保持历史行为）。
	ToolChoice      any    `json:"tool_choice,omitempty"`
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    any        `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	// Name 为 OpenAI 兼容字段：当前未参与请求到 Vertex 的转换（保持历史行为）。
	Name      string `json:"name,omitempty"`
	Reasoning string `json:"reasoning,omitempty"`
	// Non-standard but widely used alias; helps preserve Claude extended thinking blocks across turns.
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

type ImageURL struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ToolCall struct {
	Index    *int         `json:"index,omitempty"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
