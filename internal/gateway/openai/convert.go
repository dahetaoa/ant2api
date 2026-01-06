package openai

import (
	"regexp"
	"strconv"
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
	vreq.Request.Contents = vertex.SanitizeContents(toVertexContents(req, requestID))

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
	model := strings.TrimSpace(req.Model)
	isClaudeThinking := strings.HasPrefix(model, "claude-") && strings.HasSuffix(model, "-thinking")
	isGemini := strings.HasPrefix(model, "gemini-")
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			continue
		case "user":
			out = append(out, vertex.Content{Role: "user", Parts: extractUserParts(m.Content)})
		case "assistant":
			parts := make([]vertex.Part, 0, 2+len(m.ToolCalls))
			thinkingText := strings.TrimSpace(m.Reasoning)
			if thinkingText == "" {
				thinkingText = strings.TrimSpace(m.ReasoningContent)
			}

			firstToolSig := ""
			if len(m.ToolCalls) > 0 {
				if s, ok := signature.GetManager().LookupByToolCallID(m.ToolCalls[0].ID); ok {
					firstToolSig = s
				}
			}

			// Claude thinking models: only include thought parts when we can also attach a thoughtSignature.
			// Some backend responses may omit thinking/signature; in that case, do NOT insert synthetic
			// thought blocks (otherwise Vertex will reject the request).
			if isClaudeThinking {
				if firstToolSig != "" {
					// IMPORTANT: If the client did not send a thinking chain for this turn, do not
					// synthesize a thought block (even if we have a cached signature). Vertex history
					// must reflect the client-provided content.
					if thinkingText != "" {
						parts = append(parts, vertex.Part{Text: thinkingText, Thought: true, ThoughtSignature: firstToolSig})
					}
				}
			} else if thinkingText != "" {
				parts = append(parts, vertex.Part{Text: thinkingText, Thought: true})
			}

			if t := getTextContent(m.Content); t != "" {
				parts = append(parts, vertex.Part{Text: t})
			}
			for i, tc := range m.ToolCalls {
				args := parseArgs(tc.Function.Arguments)
				sig := ""
				if isGemini {
					// Gemini: signature is attached to the first functionCall part.
					// Claude: signature must not be placed on functionCall parts.
					if s, ok := signature.GetManager().LookupByToolCallID(tc.ID); ok {
						sig = s
					}
					if i != 0 {
						sig = ""
					}
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
	model := strings.TrimSpace(req.Model)
	isClaude := strings.HasPrefix(model, "claude-")
	isGemini := strings.HasPrefix(model, "gemini-")
	cfg := &vertex.GenerationConfig{CandidateCount: 1}
	// Gemini models: maxOutputTokens is fixed at 65535.
	if isGemini {
		cfg.MaxOutputTokens = 65535
	} else if req.MaxTokens > 0 && !isClaude {
		cfg.MaxOutputTokens = req.MaxTokens
	}
	if req.Temperature != nil {
		cfg.Temperature = req.Temperature
	}
	if req.TopP != nil {
		cfg.TopP = req.TopP
	}

	// Enable thinking output when requested. Cloud Code API differs per model family:
	// - Gemini 3: thinkingLevel
	// - Gemini 2.5: thinkingBudget
	// - Claude thinking: thinkingBudget
	if tc := buildThinkingConfig(req.Model, req.ReasoningEffort); tc != nil {
		cfg.ThinkingConfig = tc
	}

	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		cfg.MaxOutputTokens = 64000
	}

	// When thinkingBudget is used, ensure it is compatible with maxOutputTokens.
	if cfg.ThinkingConfig != nil && cfg.ThinkingConfig.ThinkingBudget > 0 {
		if cfg.MaxOutputTokens <= 0 {
			cfg.MaxOutputTokens = cfg.ThinkingConfig.ThinkingBudget + 4096
		}
		if isClaude {
			maxBudget := cfg.MaxOutputTokens - 1024
			if maxBudget < 1024 {
				maxBudget = 1024
			}
			if cfg.ThinkingConfig.ThinkingBudget > maxBudget {
				cfg.ThinkingConfig.ThinkingBudget = maxBudget
			}
		} else if isGemini && cfg.MaxOutputTokens <= cfg.ThinkingConfig.ThinkingBudget {
			maxBudget := cfg.MaxOutputTokens - 1024
			if maxBudget < 1024 {
				maxBudget = 1024
			}
			cfg.ThinkingConfig.ThinkingBudget = maxBudget
		} else if cfg.MaxOutputTokens <= cfg.ThinkingConfig.ThinkingBudget {
			cfg.MaxOutputTokens = cfg.ThinkingConfig.ThinkingBudget + 4096
		}
	}
	return cfg
}

func buildThinkingConfig(model, reasoningEffort string) *vertex.ThinkingConfig {
	model = strings.TrimSpace(model)
	effort := strings.ToLower(strings.TrimSpace(reasoningEffort))

	isClaude := strings.HasPrefix(model, "claude-")
	isClaudeThinking := isClaude && strings.HasSuffix(model, "-thinking")
	isGemini3 := strings.HasPrefix(model, "gemini-3-") || strings.HasPrefix(model, "gemini-3")
	isGemini25 := strings.HasPrefix(model, "gemini-2.5-") || strings.HasPrefix(model, "gemini-2.5")

	// If the caller explicitly selects a Claude "-thinking" model, opt-in by default.
	// This matches the project's docs: includeThoughts is required for thoughts/signature.
	if effort == "" && isClaudeThinking {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: 32000}
	}

	// Gemini 3 models: always use thinkingLevel=high.
	// Gemini 3 models default to thinking mode, similar to Claude -thinking models.
	if isGemini3 {
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high"}
	}

	if effort == "" {
		return nil
	}

	if isClaudeThinking || isGemini25 {
		// Support numeric effort as a direct thinking budget override for budget-based models.
		if n, err := strconv.Atoi(effort); err == nil && n > 0 {
			return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: n}
		}
		if isClaudeThinking {
			return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: mapEffortToBudget(effort)}
		}
		return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: mapGemini25EffortToBudget(effort)}
	}

	return &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: effort}
}

func mapEffortToBudget(effort string) int {
	switch strings.ToLower(strings.TrimSpace(effort)) {
	case "minimal", "low":
		return 1024
	case "medium":
		return 4096
	case "high", "max":
		return 32000
	default:
		return 32000
	}
}

func mapGemini25EffortToBudget(effort string) int {
	_ = effort
	// Keep conservative by default: Gemini 2.5 examples commonly use small budgets (e.g. 1024).
	return 1024
}

func toVertexTools(tools []Tool) []vertex.Tool {
	var out []vertex.Tool
	for _, t := range tools {
		params := vertex.SanitizeFunctionParametersSchema(t.Function.Parameters)
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
