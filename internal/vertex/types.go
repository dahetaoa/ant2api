package vertex

import (
	"encoding"
	"unsafe"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/pkg/lazyimage"
)

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
	// Data is kept for compatibility with response processing paths (OpenAI markdown reconstruction, etc.).
	// It is not used for request JSON serialization when DataText is present.
	Data     string     `json:"-"`
	DataText base64Text `json:"data"`

	ref *lazyimage.ImageRef `json:"-"`
}

func NewInlineData(mimeType string, data string) *InlineData {
	return &InlineData{MimeType: mimeType, Data: data, DataText: base64Text{str: data}}
}

func NewInlineDataFromRef(ref *lazyimage.ImageRef) *InlineData {
	if ref == nil {
		return nil
	}
	return &InlineData{MimeType: ref.MimeType(), DataText: base64Text{ref: ref}, ref: ref}
}

func (i *InlineData) SignatureKey() string {
	if i == nil {
		return ""
	}
	if i.ref != nil {
		return i.ref.SignatureKey()
	}
	if len(i.Data) > 50 {
		// 使用 string([]byte(...)) 创建独立副本，断开与原始图片数据的引用
		// 避免子字符串切片导致整个大尺寸base64数据无法被GC回收
		return string([]byte(i.Data[:50]))
	}
	return i.Data
}

func (i *InlineData) IsLazy() bool { return i != nil && i.ref != nil }

func (i *InlineData) UnmarshalJSON(data []byte) error {
	// Preserve existing wire format while allowing custom request serialization.
	// Note: We only store Data field during response deserialization to avoid
	// double memory allocation. DataText is only used for request serialization.
	type wire struct {
		MimeType string `json:"mimeType"`
		Data     string `json:"data"`
	}
	var w wire
	// Use our jsonpkg (sonic with CopyString: true) instead of standard library.
	// This ensures decoded strings are independent copies, allowing the original
	// response buffer to be garbage collected after request processing completes.
	if err := jsonpkg.Unmarshal(data, &w); err != nil {
		return err
	}
	i.MimeType = w.MimeType
	i.Data = w.Data
	// Don't set DataText.str here - it would duplicate the entire base64 string.
	// DataText is only used for request serialization via NewInlineData/NewInlineDataFromRef.
	i.DataText = base64Text{}
	i.ref = nil
	return nil
}

type base64Text struct {
	str string
	ref *lazyimage.ImageRef
}

var _ encoding.TextMarshaler = base64Text{}

func (b base64Text) MarshalText() ([]byte, error) {
	if b.ref != nil {
		return b.ref.DataBytes(), nil
	}
	if b.str == "" {
		return nil, nil
	}
	// Avoid allocating []byte(str) for large payloads; encoder treats the returned bytes as read-only.
	return unsafe.Slice(unsafe.StringData(b.str), len(b.str)), nil
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
	return jsonpkg.Marshal(w)
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

// ClearLargeData clears large data references from the response to allow GC to reclaim memory.
// Call this after response processing is complete (e.g., after ToChatCompletion and WriteJSON).
func (r *Response) ClearLargeData() {
	if r == nil {
		return
	}
	for i := range r.Response.Candidates {
		for j := range r.Response.Candidates[i].Content.Parts {
			p := &r.Response.Candidates[i].Content.Parts[j]
			if p.InlineData != nil {
				p.InlineData.Data = ""
				p.InlineData.DataText = base64Text{}
			}
			p.Text = ""
			p.ThoughtSignature = ""
		}
	}
}
