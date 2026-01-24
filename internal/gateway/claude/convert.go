package claude

import (
	"errors"
	"strings"

	"anti2api-golang/refactor/internal/config"
	gwcommon "anti2api-golang/refactor/internal/gateway/common"
	"anti2api-golang/refactor/internal/pkg/id"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

func ToVertexRequest(req *MessagesRequest, account *gwcommon.AccountContext) (*vertex.Request, string, error) {
	if req == nil {
		return nil, "", errors.New("nil request")
	}
	if len(req.Messages) == 0 {
		return nil, "", errors.New("messages is required")
	}

	model := strings.TrimSpace(req.Model)
	isClaudeModel := modelutil.IsClaude(model)
	isImageModel := modelutil.IsImageModel(model)
	isGemini3Flash := modelutil.IsGemini3Flash(model)

	requestID := id.RequestID()
	vertexModel := modelutil.BackendModelID(req.Model)
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

	if sys := gwcommon.ExtractClaudeSystemText(req.System); sys != "" {
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
	isClaude := modelutil.IsClaude(model)
	isGemini := modelutil.IsGemini(model)
	isImageModel := modelutil.IsImageModel(model)

	cfg := &vertex.GenerationConfig{CandidateCount: 1}
	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		cfg.MaxOutputTokens = modelutil.ClaudeMaxOutputTokens
	} else if isGemini {
		// Gemini models: maxOutputTokens is fixed at 65535.
		cfg.MaxOutputTokens = modelutil.GeminiMaxOutputTokens
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

	if req.Thinking != nil {
		cfg.ThinkingConfig = modelutil.ThinkingConfigFromClaude(model, req.Thinking.Type, req.Thinking.Budget, req.Thinking.BudgetTokens)
	} else {
		// 允许由模型名强制启用 thinking（例如 gemini-3-flash / claude 4.5）。
		cfg.ThinkingConfig, _ = modelutil.ForcedThinkingConfig(model)
	}

	if cfg.ThinkingConfig != nil && cfg.ThinkingConfig.ThinkingBudget > 0 {
		maxBudget := cfg.MaxOutputTokens - modelutil.ThinkingBudgetHeadroomTokens
		if maxBudget < modelutil.ThinkingBudgetMinTokens {
			maxBudget = modelutil.ThinkingBudgetMinTokens
		}
		if cfg.ThinkingConfig.ThinkingBudget > maxBudget {
			cfg.ThinkingConfig.ThinkingBudget = maxBudget
		}
	}

	// Gemini image size virtual models: force imageConfig.imageSize via the model name.
	if imageSize, _, ok := modelutil.GeminiProImageSizeConfig(model); ok {
		cfg.ImageConfig = &vertex.ImageConfig{ImageSize: imageSize}
	}

	// Gemini 3: apply global mediaResolution when configured.
	if modelutil.IsGemini3(model) && !isImageModel {
		if v, ok := modelutil.ToAPIMediaResolution(config.Get().Gemini3MediaResolution); ok && v != "" {
			cfg.MediaResolution = v
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
						if sig == "" {
							if e, ok := signature.GetManager().LookupByToolCallID(toolUseID); ok {
								sig = strings.TrimSpace(e.Signature)
							}
						} else if len(sig) <= 50 {
							// Clients may persist only a short prefix of the opaque signature.
							// Expand it from disk using tool_use id + signature prefix.
							if e, ok := signature.GetManager().LookupByToolCallIDAndSignaturePrefix(toolUseID, sig); ok {
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
						if data == "" {
							if e, ok := signature.GetManager().LookupByToolCallID(toolUseID); ok {
								data = strings.TrimSpace(e.Signature)
							}
						} else if len(data) <= 50 {
							if e, ok := signature.GetManager().LookupByToolCallIDAndSignaturePrefix(toolUseID, data); ok {
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
				name := gwcommon.FindFunctionName(contentsSoFar, toolUseID)
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

func toVertexTools(tools []Tool) []vertex.Tool {
	var out []vertex.Tool
	for _, t := range tools {
		params := vertex.SanitizeFunctionParametersSchema(t.InputSchema)
		out = append(out, vertex.Tool{FunctionDeclarations: []vertex.FunctionDeclaration{{Name: t.Name, Description: t.Description, Parameters: params}}})
	}
	return out
}
