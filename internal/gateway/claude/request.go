package claude

type MessagesRequest struct {
	Model         string    `json:"model"`
	MaxTokens     int       `json:"max_tokens"`
	Messages      []Message `json:"messages"`
	System        any       `json:"system,omitempty"`
	Stream        bool      `json:"stream"`
	Temperature   *float64  `json:"temperature,omitempty"`
	TopP          *float64  `json:"top_p,omitempty"`
	StopSequences []string  `json:"stop_sequences,omitempty"`
	Tools         []Tool    `json:"tools,omitempty"`
	ToolChoice    any       `json:"tool_choice,omitempty"`
	Thinking      *Thinking `json:"thinking,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ContentBlock struct {
	Type      string `json:"type"`
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	ID        string `json:"id,omitempty"`
	Name      string `json:"name,omitempty"`
	Input     any    `json:"input,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Content   any    `json:"content,omitempty"`
	IsError   bool   `json:"is_error,omitempty"`
	Source    any    `json:"source,omitempty"`
}

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type Thinking struct {
	Type string `json:"type"`
	// Anthropic clients typically send budget_tokens. Accept both.
	Budget       int    `json:"budget,omitempty"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
	Level        string `json:"thinking_level,omitempty"`
}
