package openai

import (
	"regexp"
	"strings"

	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

func ToVertexRequest(req *ChatRequest, account *AccountContext) (*vertex.Request, string, error) {
	modelName := req.Model
	requestID := id.RequestID()

	vreq := &vertex.Request{
		Project:   account.ProjectID,
		Model:     modelName,
		RequestID: requestID,
		Request: vertex.InnerReq{
			Contents:  nil,
			SessionID: account.SessionID,
		},
	}

	if sys := extractSystem(req.Messages); sys != "" {
		vreq.Request.SystemInstruction = &vertex.SystemInstruction{Parts: []vertex.Part{{Text: sys}}}
	}

	if len(req.Tools) > 0 {
		vreq.Request.Tools = toVertexTools(req.Tools)
		vreq.Request.ToolConfig = &vertex.ToolConfig{FunctionCallingConfig: &vertex.FunctionCallingConfig{Mode: "AUTO"}}
	}

	vreq.Request.GenerationConfig = buildGenerationConfig(req)
	vreq.Request.Contents = toVertexContents(req, requestID)

	return vreq, requestID, nil
}

type AccountContext struct {
	ProjectID   string
	SessionID   string
	AccessToken string
}

func extractSystem(messages []Message) string {
	var parts []string
	for _, m := range messages {
		if m.Role != "system" {
			continue
		}
		if t := getTextContent(m.Content); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "\n\n")
}

func toVertexContents(req *ChatRequest, requestID string) []vertex.Content {
	var out []vertex.Content
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			continue
		case "user":
			out = append(out, vertex.Content{Role: "user", Parts: extractUserParts(m.Content)})
		case "assistant":
			parts := make([]vertex.Part, 0, 2+len(m.ToolCalls))
			if m.Reasoning != "" {
				parts = append(parts, vertex.Part{Text: m.Reasoning, Thought: true})
			}
			if t := getTextContent(m.Content); t != "" {
				parts = append(parts, vertex.Part{Text: t})
			}
			for i, tc := range m.ToolCalls {
				args := parseArgs(tc.Function.Arguments)
				sig := ""
				// Do not parse client-provided signatures; rely on tool_call_id indexed store only.
				if s, ok := signature.GetManager().LookupByToolCallID(tc.ID); ok {
					sig = s
				}
				if i != 0 {
					sig = ""
				}
				parts = append(parts, vertex.Part{
					FunctionCall:     &vertex.FunctionCall{ID: tc.ID, Name: tc.Function.Name, Args: args},
					ThoughtSignature: sig,
				})
			}
			if len(parts) > 0 {
				out = append(out, vertex.Content{Role: "model", Parts: parts})
			}
		case "tool":
			funcName := findFunctionName(out, m.ToolCallID)
			p := vertex.Part{FunctionResponse: &vertex.FunctionResponse{ID: m.ToolCallID, Name: funcName, Response: map[string]any{"output": getTextContent(m.Content)}}}
			appendFunctionResponse(&out, p)
		}
	}
	return out
}

func buildGenerationConfig(req *ChatRequest) *vertex.GenerationConfig {
	cfg := &vertex.GenerationConfig{CandidateCount: 1}
	if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.TopP != nil {
		cfg.TopP = req.TopP
	}
	if req.ReasoningEffort != "" {
		cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: strings.ToLower(req.ReasoningEffort)}
	}
	return cfg
}

func toVertexTools(tools []Tool) []vertex.Tool {
	var out []vertex.Tool
	for _, t := range tools {
		params := t.Function.Parameters
		if params != nil {
			delete(params, "$schema")
		}
		out = append(out, vertex.Tool{FunctionDeclarations: []vertex.FunctionDeclaration{{Name: t.Function.Name, Description: t.Function.Description, Parameters: params}}})
	}
	return out
}

func extractUserParts(content any) []vertex.Part {
	var out []vertex.Part
	switch v := content.(type) {
	case string:
		if v != "" {
			out = append(out, vertex.Part{Text: v})
		}
	case []any:
		for _, it := range v {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			switch typ {
			case "text":
				if t, ok := m["text"].(string); ok && t != "" {
					out = append(out, vertex.Part{Text: t})
				}
			case "image_url":
				img, ok := m["image_url"].(map[string]any)
				if !ok {
					continue
				}
				urlStr, _ := img["url"].(string)
				if inline := parseImageURL(urlStr); inline != nil {
					out = append(out, vertex.Part{InlineData: inline})
				}
			}
		}
	}
	return out
}

func parseImageURL(urlStr string) *vertex.InlineData {
	re := regexp.MustCompile(`^data:image/(\w+);base64,(.+)$`)
	if matches := re.FindStringSubmatch(urlStr); len(matches) == 3 {
		return &vertex.InlineData{MimeType: "image/" + matches[1], Data: matches[2]}
	}
	return nil
}

func getTextContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var texts []string
		for _, it := range v {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "text" {
				if t, ok := m["text"].(string); ok {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n")
	}
	return ""
}

func parseArgs(args string) map[string]any {
	var out map[string]any
	if args == "" {
		return map[string]any{}
	}
	if err := jsonpkg.UnmarshalString(args, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func findFunctionName(contents []vertex.Content, toolCallID string) string {
	for i := len(contents) - 1; i >= 0; i-- {
		for _, p := range contents[i].Parts {
			if p.FunctionCall != nil && p.FunctionCall.ID == toolCallID {
				return p.FunctionCall.Name
			}
		}
	}
	return ""
}

func appendFunctionResponse(contents *[]vertex.Content, part vertex.Part) {
	if len(*contents) > 0 && (*contents)[len(*contents)-1].Role == "model" {
		*contents = append(*contents, vertex.Content{Role: "user", Parts: []vertex.Part{part}})
		return
	}
	if len(*contents) > 0 && (*contents)[len(*contents)-1].Role == "user" {
		(*contents)[len(*contents)-1].Parts = append((*contents)[len(*contents)-1].Parts, part)
		return
	}
	*contents = append(*contents, vertex.Content{Role: "user", Parts: []vertex.Part{part}})
}
