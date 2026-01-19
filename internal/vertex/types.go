package vertex

import "encoding/json"

// Request is the Vertex AI Cloud Code API wrapper request.
// It matches the format used by antigravity Cloud Code endpoints.
type Request struct {
	Project     string   `json:"project"`
	Model       string   `json:"model"`
	RequestID   string   `json:"requestId"`
	RequestType string   `json:"requestType,omitempty"`
	UserAgent   string   `json:"userAgent,omitempty"`
	Request     InnerReq `json:"request"`
}

type InnerReq struct {
	Contents          []Content          `json:"contents"`
	SystemInstruction *SystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GenerationConfig  `json:"generationConfig,omitempty"`
	Tools             []Tool             `json:"tools,omitempty"`
	ToolConfig        *ToolConfig        `json:"toolConfig,omitempty"`
	SessionID         string             `json:"sessionId"`
}

type Content struct {
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text             string            `json:"text,omitempty"`
	FunctionCall     *FunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *FunctionResponse `json:"functionResponse,omitempty"`
	InlineData       *InlineData       `json:"inlineData,omitempty"`
	Thought          bool              `json:"thought,omitempty"`
	ThoughtSignature string            `json:"thoughtSignature,omitempty"`
}

type FunctionCall struct {
	ID   string         `json:"id,omitempty"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type FunctionResponse struct {
	ID       string         `json:"id,omitempty"`
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

type InlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type SystemInstruction struct {
	Role  string `json:"role,omitempty"`
	Parts []Part `json:"parts"`
}

type Tool struct {
	FunctionDeclarations []FunctionDeclaration `json:"functionDeclarations,omitempty"`
}

type FunctionDeclaration struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type ToolConfig struct {
	FunctionCallingConfig *FunctionCallingConfig `json:"functionCallingConfig,omitempty"`
}

type FunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
}

type GenerationConfig struct {
	CandidateCount  int             `json:"candidateCount,omitempty"`
	StopSequences   []string        `json:"stopSequences,omitempty"`
	MaxOutputTokens int             `json:"maxOutputTokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"topP,omitempty"`
	TopK            int             `json:"topK,omitempty"`
	ThinkingConfig  *ThinkingConfig `json:"thinkingConfig,omitempty"`
	ImageConfig     *ImageConfig    `json:"imageConfig,omitempty"`
	MediaResolution string          `json:"mediaResolution,omitempty"`
}

type ThinkingConfig struct {
	IncludeThoughts bool   `json:"includeThoughts"`
	ThinkingBudget  int    `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type ImageConfig struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

func (t ThinkingConfig) MarshalJSON() ([]byte, error) {
	// Preserve existing "omitempty" behavior for thinkingBudget when thinkingLevel is set,
	// but allow callers to emit thinkingBudget=0 when thinkingLevel is empty (e.g. gemini-3-flash).
	type wire struct {
		IncludeThoughts bool   `json:"includeThoughts"`
		ThinkingBudget  *int   `json:"thinkingBudget,omitempty"`
		ThinkingLevel   string `json:"thinkingLevel,omitempty"`
	}
	w := wire{
		IncludeThoughts: t.IncludeThoughts,
		ThinkingLevel:   t.ThinkingLevel,
	}
	if t.ThinkingBudget != 0 || t.ThinkingLevel == "" {
		b := t.ThinkingBudget
		w.ThinkingBudget = &b
	}
	return json.Marshal(w)
}

type Response struct {
	Response struct {
		Candidates    []Candidate    `json:"candidates"`
		UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	} `json:"response"`
}

type Candidate struct {
	Content      Content `json:"content"`
	FinishReason string  `json:"finishReason,omitempty"`
	Index        int     `json:"index"`
}

type UsageMetadata struct {
	PromptTokenCount     int `json:"promptTokenCount"`
	CandidatesTokenCount int `json:"candidatesTokenCount"`
	TotalTokenCount      int `json:"totalTokenCount"`
	ThoughtsTokenCount   int `json:"thoughtsTokenCount,omitempty"`
}
