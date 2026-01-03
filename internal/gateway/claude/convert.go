package claude

import (
	"errors"
	"strings"

	"anti2api-golang/refactor/internal/pkg/id"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

type AccountContext struct {
	ProjectID   string
	SessionID   string
	AccessToken string
}

func ToVertexRequest(req *MessagesRequest, account *AccountContext) (*vertex.Request, string, error) {
	if req == nil {
		return nil, "", errors.New("nil request")
	}
	if len(req.Messages) == 0 {
		return nil, "", errors.New("messages is required")
	}

	requestID := id.RequestID()
	vreq := &vertex.Request{
		Project:   account.ProjectID,
		Model:     req.Model,
		RequestID: requestID,
		Request: vertex.InnerReq{
			Contents:  nil,
			SessionID: account.SessionID,
		},
	}

	if sys := extractSystem(req.System); sys != "" {
		vreq.Request.SystemInstruction = &vertex.SystemInstruction{Parts: []vertex.Part{{Text: sys}}}
	}

	if len(req.Tools) > 0 {
		vreq.Request.Tools = toVertexTools(req.Tools)
		vreq.Request.ToolConfig = &vertex.ToolConfig{FunctionCallingConfig: &vertex.FunctionCallingConfig{Mode: "AUTO"}}
	}

	vreq.Request.GenerationConfig = buildGenerationConfig(req)
	vreq.Request.Contents = toVertexContents(req.Messages)

	return vreq, requestID, nil
}

func buildGenerationConfig(req *MessagesRequest) *vertex.GenerationConfig {
	cfg := &vertex.GenerationConfig{CandidateCount: 1, MaxOutputTokens: req.MaxTokens}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.TopP != nil {
		cfg.TopP = req.TopP
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = append(cfg.StopSequences, req.StopSequences...)
	}

	if req.Thinking != nil && strings.ToLower(req.Thinking.Type) == "enabled" {
		cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true}
		if req.Thinking.Budget > 0 {
			cfg.ThinkingConfig.ThinkingBudget = req.Thinking.Budget
		}
		if req.Thinking.Level != "" {
			cfg.ThinkingConfig.ThinkingLevel = req.Thinking.Level
			cfg.ThinkingConfig.ThinkingBudget = 0
		}
	}
	return cfg
}

func toVertexContents(messages []Message) []vertex.Content {
	var out []vertex.Content
	for _, m := range messages {
		switch m.Role {
		case "user":
			parts := extractContentParts(m.Content, out)
			if len(parts) > 0 {
				out = append(out, vertex.Content{Role: "user", Parts: parts})
			}
		case "assistant":
			parts := extractContentParts(m.Content, out)
			if len(parts) > 0 {
				out = append(out, vertex.Content{Role: "model", Parts: parts})
			}
		}
	}
	return out
}

func extractContentParts(content any, contentsSoFar []vertex.Content) []vertex.Part {
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
			case "thinking":
				thinking, _ := m["thinking"].(string)
				// Ignore client-provided signature; signatures are managed by tool_use id indexing.
				out = append(out, vertex.Part{Text: thinking, Thought: true})
			case "tool_use":
				idv, _ := m["id"].(string)
				if idv == "" {
					idv = id.ToolCallID()
				}
				name, _ := m["name"].(string)
				input, _ := m["input"].(map[string]any)
				// Ignore client-provided signature; only tool_call_id based lookup.
				sig := ""
				if s, ok := signature.GetManager().LookupByToolCallID(idv); ok {
					sig = s
				}
				out = append(out, vertex.Part{FunctionCall: &vertex.FunctionCall{ID: idv, Name: name, Args: input}, ThoughtSignature: sig})
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				toolUseID = strings.TrimSpace(toolUseID)
				if toolUseID == "" {
					// Preserve request semantics: a tool_result must reference a prior tool_use.
					return out
				}
				name := findFunctionName(contentsSoFar, toolUseID)
				name = strings.TrimSpace(name)
				if name == "" {
					return out
				}
				resultText := extractToolResultContent(m["content"])
				out = append(out, vertex.Part{FunctionResponse: &vertex.FunctionResponse{ID: toolUseID, Name: name, Response: map[string]any{"output": resultText}}})
			}
		}
	}
	return out
}

func extractToolResultContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var b strings.Builder
		for _, it := range v {
			m, ok := it.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] == "text" {
				if t, ok := m["text"].(string); ok {
					b.WriteString(t)
				}
			}
		}
		return b.String()
	}
	return ""
}

func findToolName(parts []vertex.Part, toolUseID string) string {
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i].FunctionCall != nil && parts[i].FunctionCall.ID == toolUseID {
			return parts[i].FunctionCall.Name
		}
	}
	return ""
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

func extractSystem(system any) string {
	switch v := system.(type) {
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
				if t, ok := m["text"].(string); ok && t != "" {
					texts = append(texts, t)
				}
			}
		}
		return strings.Join(texts, "\n\n")
	}
	return ""
}

func toVertexTools(tools []Tool) []vertex.Tool {
	var out []vertex.Tool
	for _, t := range tools {
		out = append(out, vertex.Tool{FunctionDeclarations: []vertex.FunctionDeclaration{{Name: t.Name, Description: t.Description, Parameters: t.InputSchema}}})
	}
	return out
}
