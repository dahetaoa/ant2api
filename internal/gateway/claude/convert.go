package claude

import (
	"errors"
	"strings"

	"anti2api-golang/refactor/internal/pkg/id"
	"anti2api-golang/refactor/internal/pkg/modelutil"
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

	model := strings.TrimSpace(req.Model)
	isClaudeModel := strings.HasPrefix(model, "claude-")
	isImageModel := strings.Contains(strings.ToLower(model), "image")
	isGemini3Flash := modelutil.IsGemini3Flash(model)

	requestID := id.RequestID()
	vertexModel := req.Model
	if _, backendModel, ok := modelutil.Gemini3FlashThinkingConfig(req.Model); ok {
		// Virtual Gemini 3 Flash models map to the same backend model id.
		vertexModel = backendModel
	}
	if _, backendModel, ok := modelutil.ClaudeOpus45ThinkingConfig(req.Model); ok {
		// Virtual Claude Opus 4.5 (non "-thinking") maps to the "-thinking" backend model id.
		vertexModel = backendModel
	}
	vreq := &vertex.Request{
		Project:   account.ProjectID,
		Model:     vertexModel,
		RequestID: requestID,
		Request: vertex.InnerReq{
			Contents:  nil,
			SessionID: account.SessionID,
		},
	}
	vreq.RequestType = "agent"
	vreq.UserAgent = "antigravity"

	if sys := extractSystem(req.System); sys != "" {
		vreq.Request.SystemInstruction = &vertex.SystemInstruction{Role: "user", Parts: []vertex.Part{{Text: sys}}}
	}

	if len(req.Tools) > 0 {
		vreq.Request.Tools = toVertexTools(req.Tools)
		vreq.Request.ToolConfig = &vertex.ToolConfig{FunctionCallingConfig: &vertex.FunctionCallingConfig{Mode: "AUTO"}}
	}

	vreq.Request.GenerationConfig = buildGenerationConfig(req)
	contents, err := toVertexContents(req.Messages, isClaudeModel)
	if err != nil {
		return nil, "", err
	}
	vreq.Request.Contents = contents
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}

	return vreq, requestID, nil
}

func buildGenerationConfig(req *MessagesRequest) *vertex.GenerationConfig {
	model := strings.TrimSpace(req.Model)
	isClaude := strings.HasPrefix(model, "claude-")
	isGemini := strings.HasPrefix(model, "gemini-")
	isGemini3 := strings.HasPrefix(model, "gemini-3-") || strings.HasPrefix(model, "gemini-3")

	cfg := &vertex.GenerationConfig{CandidateCount: 1}
	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		cfg.MaxOutputTokens = 64000
	} else if isGemini {
		// Gemini models: maxOutputTokens is fixed at 65535.
		cfg.MaxOutputTokens = 65535
	} else if req.MaxTokens > 0 {
		cfg.MaxOutputTokens = req.MaxTokens
	} else {
		cfg.MaxOutputTokens = 8192
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.TopP != nil {
		cfg.TopP = req.TopP
	}
	if len(req.StopSequences) > 0 {
		cfg.StopSequences = append(cfg.StopSequences, req.StopSequences...)
	}

	// Claude Sonnet 4.5: thinkingBudget is determined solely by the model name.
	// Always ignore client-provided thinking params for these models.
	if budget, ok := modelutil.ClaudeSonnet45ThinkingBudget(model); ok {
		cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget}
	} else if budget, _, ok := modelutil.ClaudeOpus45ThinkingConfig(model); ok {
		// Claude Opus 4.5: thinkingBudget is determined solely by the model name.
		// Always ignore client-provided thinking params for these models.
		cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget}
	} else if level, _, ok := modelutil.Gemini3FlashThinkingConfig(model); ok {
		// Gemini 3 Flash: thinkingLevel is determined solely by the model name.
		// Always ignore client-provided thinking params for these models.
		if level == "high" {
			cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high", ThinkingBudget: 0}
		} else {
			cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: 0}
		}
	} else if req.Thinking != nil && strings.ToLower(req.Thinking.Type) == "enabled" {
		cfg.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true}
		if isClaude {
			// Claude thinking models require a non-zero thinkingBudget to output thoughts.
			budget := req.Thinking.Budget
			if budget <= 0 {
				budget = req.Thinking.BudgetTokens
			}
			if budget <= 0 {
				budget = 32000
			}
			cfg.ThinkingConfig.ThinkingBudget = budget
		} else if isGemini3 {
			// Gemini 3 non-Flash models (e.g. gemini-3-pro): always use thinking_level=high when thinking is requested.
			cfg.ThinkingConfig.ThinkingLevel = "high"
			cfg.ThinkingConfig.ThinkingBudget = 0
		} else {
			budget := req.Thinking.Budget
			if budget <= 0 {
				budget = req.Thinking.BudgetTokens
			}
			if budget > 0 {
				cfg.ThinkingConfig.ThinkingBudget = budget
			}
		}
	}

	if cfg.ThinkingConfig != nil && cfg.ThinkingConfig.ThinkingBudget > 0 {
		maxBudget := cfg.MaxOutputTokens - 1024
		if maxBudget < 1024 {
			maxBudget = 1024
		}
		if cfg.ThinkingConfig.ThinkingBudget > maxBudget {
			cfg.ThinkingConfig.ThinkingBudget = maxBudget
		}
	}
	return cfg
}

func toVertexContents(messages []Message, isClaudeModel bool) ([]vertex.Content, error) {
	var out []vertex.Content
	for _, m := range messages {
		switch m.Role {
		case "user":
			parts, err := extractContentParts(m.Content, out, isClaudeModel)
			if err != nil {
				return nil, err
			}
			if len(parts) > 0 {
				out = append(out, vertex.Content{Role: "user", Parts: parts})
			}
		case "assistant":
			parts, err := extractContentParts(m.Content, out, isClaudeModel)
			if err != nil {
				return nil, err
			}
			if len(parts) > 0 {
				out = append(out, vertex.Content{Role: "model", Parts: parts})
			}
		}
	}
	return out, nil
}

func extractContentParts(content any, contentsSoFar []vertex.Content, isClaudeModel bool) ([]vertex.Part, error) {
	var out []vertex.Part
	switch v := content.(type) {
	case string:
		if v != "" {
			out = append(out, vertex.Part{Text: v})
		}
	case []any:
		for i := 0; i < len(v); i++ {
			it := v[i]
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
				sig, _ := m["signature"].(string)
				sig = strings.TrimSpace(sig)
				if isClaudeModel {
					// Some clients do not persist/return the thinking signature. Best-effort recovery:
					// - If a tool_use follows in the same assistant content, look up its cached signature.
					// - Otherwise, drop the thinking block to avoid sending invalid extended thinking history.
					if sig == "" {
						var toolUseID string
						for j := i + 1; j < len(v); j++ {
							m2, ok2 := v[j].(map[string]any)
							if !ok2 {
								continue
							}
							if t2, _ := m2["type"].(string); t2 != "tool_use" {
								continue
							}
							idv, _ := m2["id"].(string)
							idv = strings.TrimSpace(idv)
							if idv != "" {
								toolUseID = idv
								break
							}
						}
						if toolUseID != "" {
							if e, ok := signature.GetManager().LookupByToolCallID(toolUseID); ok {
								sig = strings.TrimSpace(e.Signature)
							}
						}
					}
					if sig == "" {
						continue
					}
					out = append(out, vertex.Part{Text: thinking, Thought: true, ThoughtSignature: sig})
					continue
				}
				out = append(out, vertex.Part{Text: thinking, Thought: true})
			case "redacted_thinking":
				// Claude may return redacted thinking blocks which must be preserved and passed back.
				data, _ := m["data"].(string)
				data = strings.TrimSpace(data)
				if isClaudeModel {
					// Some clients may drop the opaque redacted payload; try to recover from a tool_use id.
					if data == "" {
						var toolUseID string
						for j := i + 1; j < len(v); j++ {
							m2, ok2 := v[j].(map[string]any)
							if !ok2 {
								continue
							}
							if t2, _ := m2["type"].(string); t2 != "tool_use" {
								continue
							}
							idv, _ := m2["id"].(string)
							idv = strings.TrimSpace(idv)
							if idv != "" {
								toolUseID = idv
								break
							}
						}
						if toolUseID != "" {
							if e, ok := signature.GetManager().LookupByToolCallID(toolUseID); ok {
								data = strings.TrimSpace(e.Signature)
							}
						}
					}
					if data == "" {
						continue
					}
					// Cloud Code uses thoughtSignature as the opaque verification payload.
					// Keep text empty; the backend will decrypt using the opaque field.
					out = append(out, vertex.Part{Text: "", Thought: true, ThoughtSignature: data})
					continue
				}
				out = append(out, vertex.Part{Text: "", Thought: true})
			case "tool_use":
				idv, _ := m["id"].(string)
				if idv == "" {
					idv = id.ToolCallID()
				}
				name, _ := m["name"].(string)
				input, _ := m["input"].(map[string]any)
				// For Claude models, thoughtSignature should live on the thinking block only.
				// Do NOT attach it to tool_use/functionCall parts.
				sig := ""
				if !isClaudeModel {
					// Ignore client-provided signature; only tool_call_id based lookup.
					if e, ok := signature.GetManager().LookupByToolCallID(idv); ok {
						sig = e.Signature
					}
				}
				out = append(out, vertex.Part{FunctionCall: &vertex.FunctionCall{ID: idv, Name: name, Args: input}, ThoughtSignature: sig})
			case "tool_result":
				toolUseID, _ := m["tool_use_id"].(string)
				toolUseID = strings.TrimSpace(toolUseID)
				if toolUseID == "" {
					// Preserve request semantics: a tool_result must reference a prior tool_use.
					return out, nil
				}
				name := findFunctionName(contentsSoFar, toolUseID)
				name = strings.TrimSpace(name)
				if name == "" {
					return out, nil
				}
				resultText := extractToolResultContent(m["content"])
				out = append(out, vertex.Part{FunctionResponse: &vertex.FunctionResponse{ID: toolUseID, Name: name, Response: map[string]any{"output": resultText}}})
			}
		}
	}
	return out, nil
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
		params := vertex.SanitizeFunctionParametersSchema(t.InputSchema)
		out = append(out, vertex.Tool{FunctionDeclarations: []vertex.FunctionDeclaration{{Name: t.Name, Description: t.Description, Parameters: params}}})
	}
	return out
}
