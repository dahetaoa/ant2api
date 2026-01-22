package openai

import (
	"regexp"
	"strings"

	"anti2api-golang/refactor/internal/config"
	gwcommon "anti2api-golang/refactor/internal/gateway/common"
	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

const imageModelFallbackSignature = "context_engineering_is_the_way_to_go"

func ToVertexRequest(req *ChatRequest, account *gwcommon.AccountContext) (*vertex.Request, string, error) {
	modelName := req.Model
	model := strings.TrimSpace(req.Model)
	isImageModel := modelutil.IsImageModel(model)
	isGemini3Flash := modelutil.IsGemini3Flash(model)
	requestID := id.RequestID()

	vertexModel := modelutil.BackendModelID(modelName)

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

	if sys := gwcommon.ExtractSystemFromMessages(req.Messages, func(m Message) string { return m.Role }, func(m Message) any { return m.Content }); sys != "" {
		vreq.Request.SystemInstruction = &vertex.SystemInstruction{Role: "user", Parts: []vertex.Part{{Text: sys}}}
	}

	if len(req.Tools) > 0 {
		vreq.Request.Tools = toVertexTools(req.Tools)
		vreq.Request.ToolConfig = &vertex.ToolConfig{FunctionCallingConfig: &vertex.FunctionCallingConfig{Mode: "AUTO"}}
	}

	vreq.Request.GenerationConfig = buildGenerationConfig(req)
	vreq.Request.Contents = vertex.SanitizeContents(toVertexContents(req, requestID))
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}

	return vreq, requestID, nil
}

func toVertexContents(req *ChatRequest, requestID string) []vertex.Content {
	var out []vertex.Content
	model := strings.TrimSpace(req.Model)
	isClaudeThinking := modelutil.IsClaudeThinking(model)
	isGemini := modelutil.IsGemini(model)
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			continue
		case "user":
			out = append(out, vertex.Content{Role: "user", Parts: extractUserParts(m.Content, model)})
		case "assistant":
			parts := make([]vertex.Part, 0, 2+len(m.ToolCalls))
			thinkingText := strings.TrimSpace(m.Reasoning)
			if thinkingText == "" {
				thinkingText = strings.TrimSpace(m.ReasoningContent)
			}

			firstToolSig := ""
			firstToolReasoning := ""
			if len(m.ToolCalls) > 0 {
				if e, ok := signature.GetManager().LookupByToolCallID(m.ToolCalls[0].ID); ok {
					firstToolSig = strings.TrimSpace(e.Signature)
					firstToolReasoning = e.Reasoning
				}
			}

			// Claude thinking models: Vertex requires a thoughtSignature-carrying thought part before tool calls.
			// Many clients don't persist thinking text, so we reconstruct it server-side (client > cache > dummy).
			if isClaudeThinking {
				injectedText := thinkingText
				if injectedText == "" {
					injectedText = strings.TrimSpace(firstToolReasoning)
				}
				injectedSig := firstToolSig
				if injectedSig != "" && injectedText == "" && len(m.ToolCalls) > 0 {
					injectedText = "[missing thought text]"
				}
				if injectedSig == "" && len(m.ToolCalls) > 0 {
					injectedSig = "context_engineering_is_the_way_to_go"
					if injectedText == "" {
						injectedText = "[missing thought text]"
					}
				}
				if injectedSig != "" && injectedText != "" {
					parts = append(parts, vertex.Part{Text: injectedText, Thought: true, ThoughtSignature: injectedSig})
				}
			} else if thinkingText != "" {
				parts = append(parts, vertex.Part{Text: thinkingText, Thought: true})
			}

			if t := gwcommon.ExtractTextFromContent(m.Content, "\n", false); t != "" {
				images := parseMarkdownImages(t, model)
				if len(images) == 0 {
					parts = append(parts, vertex.Part{Text: t})
				} else {
					last := 0
					for _, img := range images {
						if img.start > last {
							if seg := t[last:img.start]; seg != "" {
								parts = append(parts, vertex.Part{Text: seg})
							}
						}
						parts = append(parts, vertex.Part{
							InlineData:       &vertex.InlineData{MimeType: img.mimeType, Data: img.data},
							ThoughtSignature: img.signature,
						})
						last = img.end
					}
					if last < len(t) {
						if seg := t[last:]; seg != "" {
							parts = append(parts, vertex.Part{Text: seg})
						}
					}
				}
			}
			for i, tc := range m.ToolCalls {
				args := parseArgs(tc.Function.Arguments)
				sig := ""
				if isGemini {
					// Gemini: signature is attached to the first functionCall part.
					// Claude: signature must not be placed on functionCall parts.
					if e, ok := signature.GetManager().LookupByToolCallID(tc.ID); ok {
						sig = strings.TrimSpace(e.Signature)
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
			funcName := gwcommon.FindFunctionName(out, m.ToolCallID)
			p := vertex.Part{FunctionResponse: &vertex.FunctionResponse{ID: m.ToolCallID, Name: funcName, Response: map[string]any{"output": gwcommon.ExtractTextFromContent(m.Content, "\n", false)}}}
			appendFunctionResponse(&out, p)
		}
	}
	return out
}

func buildGenerationConfig(req *ChatRequest) *vertex.GenerationConfig {
	model := strings.TrimSpace(req.Model)
	isClaude := modelutil.IsClaude(model)
	isGemini := modelutil.IsGemini(model)
	isImageModel := modelutil.IsImageModel(model)
	cfg := &vertex.GenerationConfig{CandidateCount: 1}
	// Gemini models: maxOutputTokens is fixed at 65535.
	if isGemini {
		cfg.MaxOutputTokens = modelutil.GeminiMaxOutputTokens
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
	if tc := modelutil.ThinkingConfigFromOpenAI(req.Model, req.ReasoningEffort); tc != nil {
		cfg.ThinkingConfig = tc
	}

	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		cfg.MaxOutputTokens = modelutil.ClaudeMaxOutputTokens
	}

	// When thinkingBudget is used, ensure it is compatible with maxOutputTokens.
	if cfg.ThinkingConfig != nil && cfg.ThinkingConfig.ThinkingBudget > 0 {
		if cfg.MaxOutputTokens <= 0 {
			cfg.MaxOutputTokens = cfg.ThinkingConfig.ThinkingBudget + modelutil.ThinkingMaxOutputTokensOverheadTokens
		}
		if isClaude {
			maxBudget := cfg.MaxOutputTokens - modelutil.ThinkingBudgetHeadroomTokens
			if maxBudget < modelutil.ThinkingBudgetMinTokens {
				maxBudget = modelutil.ThinkingBudgetMinTokens
			}
			if cfg.ThinkingConfig.ThinkingBudget > maxBudget {
				cfg.ThinkingConfig.ThinkingBudget = maxBudget
			}
		} else if isGemini && cfg.MaxOutputTokens <= cfg.ThinkingConfig.ThinkingBudget {
			maxBudget := cfg.MaxOutputTokens - modelutil.ThinkingBudgetHeadroomTokens
			if maxBudget < modelutil.ThinkingBudgetMinTokens {
				maxBudget = modelutil.ThinkingBudgetMinTokens
			}
			cfg.ThinkingConfig.ThinkingBudget = maxBudget
		} else if cfg.MaxOutputTokens <= cfg.ThinkingConfig.ThinkingBudget {
			cfg.MaxOutputTokens = cfg.ThinkingConfig.ThinkingBudget + modelutil.ThinkingMaxOutputTokensOverheadTokens
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

func toVertexTools(tools []Tool) []vertex.Tool {
	var out []vertex.Tool
	for _, t := range tools {
		params := vertex.SanitizeFunctionParametersSchema(t.Function.Parameters)
		out = append(out, vertex.Tool{FunctionDeclarations: []vertex.FunctionDeclaration{{Name: t.Function.Name, Description: t.Function.Description, Parameters: params}}})
	}
	return out
}

func extractUserParts(content any, model string) []vertex.Part {
	var out []vertex.Part
	isImageModel := modelutil.IsImageModel(model)
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
					imageKey := inline.Data
					if len(imageKey) > 20 {
						imageKey = imageKey[:20]
					}
					sig := ""
					if e, ok := signature.GetManager().LookupByToolCallID(imageKey); ok {
						sig = e.Signature
					} else if isImageModel {
						sig = imageModelFallbackSignature
					}
					out = append(out, vertex.Part{InlineData: inline, ThoughtSignature: sig})
				}
			}
		}
	}
	return out
}

var markdownImageRe = regexp.MustCompile(`!\[image\]\(data:([^;]+);base64,([^)]+)\)`)

type markdownImage struct {
	mimeType  string
	data      string
	signature string
	start     int
	end       int
}

func parseMarkdownImages(content string, model string) []markdownImage {
	matches := markdownImageRe.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return nil
	}

	isImageModel := modelutil.IsImageModel(model)
	out := make([]markdownImage, 0, len(matches))
	for _, m := range matches {
		if len(m) != 6 {
			continue
		}
		mimeType := content[m[2]:m[3]]
		base64Data := content[m[4]:m[5]]

		imageKey := base64Data
		if len(imageKey) > 20 {
			imageKey = imageKey[:20]
		}
		sig := ""
		if e, ok := signature.GetManager().LookupByToolCallID(imageKey); ok {
			sig = e.Signature
		} else if isImageModel {
			sig = imageModelFallbackSignature
		}

		out = append(out, markdownImage{
			mimeType:  mimeType,
			data:      base64Data,
			signature: sig,
			start:     m[0],
			end:       m[1],
		})
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
