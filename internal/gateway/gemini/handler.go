package gemini

import (
	"bufio"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/logger"
	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/vertex"
)

// Gemini endpoints are passthrough-style. We accept Gemini request JSON (which is close to Vertex InnerReq),
// wrap it into Cloud Code API Request envelope, then forward.

type GeminiRequest struct {
	Contents          []vertex.Content          `json:"contents"`
	SystemInstruction *vertex.SystemInstruction `json:"systemInstruction,omitempty"`
	GenerationConfig  *GeminiGenerationConfig   `json:"generationConfig,omitempty"`
	Tools             []vertex.Tool             `json:"tools,omitempty"`
	ToolConfig        *vertex.ToolConfig        `json:"toolConfig,omitempty"`
	SafetySettings    []any                     `json:"safetySettings,omitempty"`
}

type GeminiGenerationConfig struct {
	CandidateCount  int                `json:"candidateCount,omitempty"`
	StopSequences   []string           `json:"stopSequences,omitempty"`
	MaxOutputTokens int                `json:"maxOutputTokens,omitempty"`
	Temperature     *float64           `json:"temperature,omitempty"`
	TopP            *float64           `json:"topP,omitempty"`
	TopK            int                `json:"topK,omitempty"`
	ThinkingConfig  *GeminiThinkingCfg `json:"thinkingConfig,omitempty"`
}

type GeminiThinkingCfg struct {
	IncludeThoughts bool   `json:"includeThoughts"`
	ThinkingBudget  int    `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type GeminiResponse struct {
	Candidates    []vertex.Candidate    `json:"candidates"`
	UsageMetadata *vertex.UsageMetadata `json:"usageMetadata,omitempty"`
}

func toVertexGenerationConfig(model string, cfg *GeminiGenerationConfig) *vertex.GenerationConfig {
	model = strings.TrimSpace(model)
	isClaude := strings.HasPrefix(model, "claude-")
	isGemini3 := strings.HasPrefix(model, "gemini-3-") || strings.HasPrefix(model, "gemini-3")
	isGemini := strings.HasPrefix(model, "gemini-")
	flashLevel, _, isGemini3Flash := modelutil.Gemini3FlashThinkingConfig(model)
	sonnet45Budget, isClaudeSonnet45 := modelutil.ClaudeSonnet45ThinkingBudget(model)
	opus45Budget, _, isClaudeOpus45 := modelutil.ClaudeOpus45ThinkingConfig(model)
	forcedClaudeBudget := isClaudeSonnet45 || isClaudeOpus45

	if cfg == nil {
		if isClaude {
			out := &vertex.GenerationConfig{CandidateCount: 1, MaxOutputTokens: 64000}
			if isClaudeSonnet45 {
				out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: sonnet45Budget}
			}
			if isClaudeOpus45 {
				out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: opus45Budget}
			}
			return out
		}
		if isGemini {
			out := &vertex.GenerationConfig{CandidateCount: 1, MaxOutputTokens: 65535}
			if isGemini3Flash {
				// Gemini 3 Flash: ignore client thinking params; force by model name.
				if flashLevel == "high" {
					out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high", ThinkingBudget: 0}
				} else {
					out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: 0}
				}
			}
			return out
		}
		return nil
	}
	out := &vertex.GenerationConfig{CandidateCount: cfg.CandidateCount, StopSequences: cfg.StopSequences, MaxOutputTokens: cfg.MaxOutputTokens, TopK: cfg.TopK}
	out.Temperature = cfg.Temperature
	out.TopP = cfg.TopP
	if forcedClaudeBudget {
		// Claude Sonnet 4.5: ignore client thinking params; force by model name.
		// Claude Opus 4.5: ignore client thinking params; force by model name.
		budget := sonnet45Budget
		if isClaudeOpus45 {
			budget = opus45Budget
		}
		out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: budget}
	} else if !isGemini3Flash && cfg.ThinkingConfig != nil {
		out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: cfg.ThinkingConfig.IncludeThoughts, ThinkingBudget: cfg.ThinkingConfig.ThinkingBudget, ThinkingLevel: cfg.ThinkingConfig.ThinkingLevel}
	}

	// Gemini 3 models: always use thinking_level=high when thinking is requested.
	if isGemini3 && !isGemini3Flash && out.ThinkingConfig != nil && out.ThinkingConfig.IncludeThoughts {
		out.ThinkingConfig.ThinkingLevel = "high"
		out.ThinkingConfig.ThinkingBudget = 0
	}

	// Claude thinking models require a non-zero thinkingBudget to output thoughts.
	if isClaude && !forcedClaudeBudget && out.ThinkingConfig != nil && out.ThinkingConfig.IncludeThoughts {
		out.ThinkingConfig.ThinkingLevel = ""
		if out.ThinkingConfig.ThinkingBudget <= 0 {
			out.ThinkingConfig.ThinkingBudget = 32000
		}
	}

	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		out.MaxOutputTokens = 64000
	}
	// Gemini models: maxOutputTokens is fixed at 65535.
	if isGemini {
		out.MaxOutputTokens = 65535
	}

	// When thinkingBudget is used, ensure it's compatible with maxOutputTokens.
	if out.ThinkingConfig != nil && out.ThinkingConfig.IncludeThoughts {
		if out.MaxOutputTokens <= 0 {
			if isClaude {
				out.MaxOutputTokens = 64000
			} else if isGemini {
				out.MaxOutputTokens = 65535
			} else if out.ThinkingConfig.ThinkingBudget > 0 {
				out.MaxOutputTokens = out.ThinkingConfig.ThinkingBudget + 4096
			} else {
				out.MaxOutputTokens = 8192
			}
		}
		if out.ThinkingConfig.ThinkingBudget > 0 {
			if isClaude {
				maxBudget := out.MaxOutputTokens - 1024
				if maxBudget < 1024 {
					maxBudget = 1024
				}
				if out.ThinkingConfig.ThinkingBudget > maxBudget {
					out.ThinkingConfig.ThinkingBudget = maxBudget
				}
			} else if isGemini && out.MaxOutputTokens <= out.ThinkingConfig.ThinkingBudget {
				maxBudget := out.MaxOutputTokens - 1024
				if maxBudget < 1024 {
					maxBudget = 1024
				}
				out.ThinkingConfig.ThinkingBudget = maxBudget
			} else if out.MaxOutputTokens <= out.ThinkingConfig.ThinkingBudget {
				out.MaxOutputTokens = out.ThinkingConfig.ThinkingBudget + 4096
			}
		}
	}

	// Gemini 3 Flash: ignore any client-provided thinkingConfig; force by model name.
	if isGemini3Flash {
		if flashLevel == "high" {
			out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingLevel: "high", ThinkingBudget: 0}
		} else {
			out.ThinkingConfig = &vertex.ThinkingConfig{IncludeThoughts: true, ThinkingBudget: 0}
		}
	}

	return out
}

func sanitizeContents(contents []vertex.Content) []vertex.Content {
	if len(contents) == 0 {
		return contents
	}

	out := make([]vertex.Content, 0, len(contents))
	for _, c := range contents {
		parts := make([]vertex.Part, 0, len(c.Parts))
		for _, p := range c.Parts {
			// Vertex requires each part to have oneof data set; drop empty parts.
			if p.FunctionCall != nil || p.FunctionResponse != nil || p.InlineData != nil {
				parts = append(parts, p)
				continue
			}
			if p.Text != "" {
				parts = append(parts, p)
				continue
			}
			// thoughtSignature-only / empty text parts are invalid for Vertex; skip.
		}
		if len(parts) == 0 {
			continue
		}
		c.Parts = parts
		out = append(out, c)
	}
	return out
}

type GeminiModelsResponse struct {
	Models []GeminiModel `json:"models"`
}

type GeminiModel struct {
	Name                       string   `json:"name"`
	Version                    string   `json:"version,omitempty"`
	DisplayName                string   `json:"displayName"`
	Description                string   `json:"description,omitempty"`
	InputTokenLimit            int      `json:"inputTokenLimit,omitempty"`
	OutputTokenLimit           int      `json:"outputTokenLimit,omitempty"`
	SupportedGenerationMethods []string `json:"supportedGenerationMethods,omitempty"`
}

func transformGeminiStreamLine(line string) string {
	if !strings.HasPrefix(line, "data: ") {
		return line
	}

	jsonData := strings.TrimSpace(line[6:])
	if jsonData == "" || jsonData == "[DONE]" {
		return line
	}

	var data map[string]any
	if err := jsonpkg.UnmarshalString(jsonData, &data); err != nil {
		return line
	}

	if resp, ok := data["response"].(map[string]any); ok {
		b, err := jsonpkg.Marshal(resp)
		if err != nil {
			return line
		}
		return "data: " + string(b)
	}

	return line
}

func HandleModels(w http.ResponseWriter, r *http.Request) {
	// Routes:
	// - GET  /v1beta/models
	// - POST /v1beta/models/{model}:generateContent
	// - POST /v1beta/models/{model}:streamGenerateContent
	const prefix = "/v1beta/models/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		http.NotFound(w, r)
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, prefix)
	if rest == "" || rest == "/" {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "不支持的请求方法，请使用 GET。"}})
			return
		}
		HandleListModels(w, r)
		return
	}

	if strings.Contains(rest, ":streamGenerateContent") {
		HandleStreamGenerateContent(w, r)
		return
	}
	if strings.Contains(rest, ":generateContent") {
		HandleGenerateContent(w, r)
		return
	}

	http.NotFound(w, r)
}

func HandleListModels(w http.ResponseWriter, r *http.Request) {
	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, nil)
	}
	startTime := time.Now()
	acc, err := credential.GetStore().GetToken()
	if err != nil {
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(http.StatusServiceUnavailable, time.Since(startTime), err.Error())
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	vm, err := vertex.FetchAvailableModels(r.Context(), acc.ProjectID, acc.AccessToken)
	if err != nil {
		status := http.StatusInternalServerError
		if apiErr, ok := err.(*vertex.APIError); ok {
			status = apiErr.Status
		}
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(status, time.Since(startTime), err.Error())
		}
		writeJSON(w, status, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	models := make([]GeminiModel, 0, len(vm.Models))
	hasGemini3Flash := false
	hasGemini3FlashThinking := false
	hasClaudeOpus45 := false
	hasClaudeOpus45Thinking := false
	for modelID := range vm.Models {
		modelID = strings.TrimSpace(modelID)
		if modelID == "" {
			continue
		}
		lower := strings.ToLower(modelID)
		if strings.Contains(lower, "gemini-3-flash") {
			hasGemini3Flash = true
		}
		if strings.HasPrefix(lower, "gemini-3-flash-thinking") {
			hasGemini3FlashThinking = true
		}
		if strings.HasPrefix(lower, "claude-opus-4-5-thinking") {
			hasClaudeOpus45Thinking = true
		} else if strings.HasPrefix(lower, "claude-opus-4-5") {
			hasClaudeOpus45 = true
		}
		models = append(models, GeminiModel{
			Name:        "models/" + modelID,
			DisplayName: modelID,
			Description: "Model provided by google",
			SupportedGenerationMethods: []string{
				"generateContent",
				"streamGenerateContent",
			},
		})
	}
	// Virtual model injection: add "models/gemini-3-flash-thinking" when any Gemini 3 Flash model exists.
	if hasGemini3Flash && !hasGemini3FlashThinking {
		models = append(models, GeminiModel{
			Name:        "models/gemini-3-flash-thinking",
			DisplayName: "gemini-3-flash-thinking",
			Description: "Virtual model provided by google (gemini-3-flash with thinkingLevel=high)",
			SupportedGenerationMethods: []string{
				"generateContent",
				"streamGenerateContent",
			},
		})
	}
	// Virtual model injection: add "models/claude-opus-4-5" when only claude-opus-4-5-thinking* exists.
	if hasClaudeOpus45Thinking && !hasClaudeOpus45 {
		models = append(models, GeminiModel{
			Name:        "models/claude-opus-4-5",
			DisplayName: "claude-opus-4-5",
			Description: "Virtual model provided by anthropic (claude-opus-4-5-thinking with thinkingBudget=0)",
			SupportedGenerationMethods: []string{
				"generateContent",
				"streamGenerateContent",
			},
		})
	}
	out := GeminiModelsResponse{Models: models}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	writeJSON(w, http.StatusOK, out)
}

func modelFromPath(r *http.Request) (string, bool) {
	// Parse from URL path (compatible with Go 1.21 ServeMux).
	const prefix = "/v1beta/models/"
	p := r.URL.Path
	if !strings.HasPrefix(p, prefix) {
		return "", false
	}
	rest := strings.TrimPrefix(p, prefix)
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		rest = rest[:i]
	}
	rest = strings.TrimSuffix(rest, "/")
	rest = strings.TrimPrefix(rest, "models/")
	if rest == "" {
		return "", false
	}
	return rest, true
}

func HandleGenerateContent(w http.ResponseWriter, r *http.Request) {
	model, ok := modelFromPath(r)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "未找到对应的模型或接口。"}})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "读取请求体失败，请检查请求是否正确发送。"}})
		return
	}

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}
	var req GeminiRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "请求 JSON 解析失败，请检查请求体格式。"}})
		return
	}

	acc, err := credential.GetStore().GetToken()
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	backendModel := model
	if _, bm, ok := modelutil.Gemini3FlashThinkingConfig(model); ok {
		backendModel = bm
	}
	if _, bm, ok := modelutil.ClaudeOpus45ThinkingConfig(model); ok {
		backendModel = bm
	}
	vreq := &vertex.Request{
		Project:   acc.ProjectID,
		Model:     backendModel,
		RequestID: id.RequestID(),
		Request: vertex.InnerReq{
			Contents:          sanitizeContents(req.Contents),
			SystemInstruction: req.SystemInstruction,
			GenerationConfig:  toVertexGenerationConfig(model, req.GenerationConfig),
			Tools:             req.Tools,
			ToolConfig:        req.ToolConfig,
			SessionID:         acc.SessionID,
		},
	}
	vreq.RequestType = "agent"
	vreq.UserAgent = "antigravity"
	if sid := strings.TrimSpace(r.Header.Get("X-Session-ID")); sid != "" {
		vreq.Request.SessionID = sid
	}
	if rid := strings.TrimSpace(r.Header.Get("X-Request-ID")); rid != "" {
		vreq.RequestID = rid
	}
	isImageModel := strings.Contains(strings.ToLower(strings.TrimSpace(model)), "image")
	isGemini3Flash := modelutil.IsGemini3Flash(model)
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}
	if vreq.Request.SystemInstruction != nil && vreq.Request.SystemInstruction.Role == "" {
		vreq.Request.SystemInstruction.Role = "user"
	}

	startTime := time.Now()
	resp, err := vertex.GenerateContent(r.Context(), vreq, acc.AccessToken)
	if err != nil {
		status := http.StatusInternalServerError
		if apiErr, ok := err.(*vertex.APIError); ok {
			status = apiErr.Status
		}
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(status, time.Since(startTime), err.Error())
		}
		writeJSON(w, status, map[string]any{"error": map[string]any{"message": err.Error()}})
		return
	}

	out := &GeminiResponse{Candidates: resp.Response.Candidates, UsageMetadata: resp.Response.UsageMetadata}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	writeJSON(w, http.StatusOK, out)
}

func HandleStreamGenerateContent(w http.ResponseWriter, r *http.Request) {
	model, ok := modelFromPath(r)
	if !ok {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, "未找到对应的模型或接口。")
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, "读取请求体失败，请检查请求是否正确发送。")
		return
	}

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}
	var req GeminiRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, "请求 JSON 解析失败，请检查请求体格式。")
		return
	}

	acc, err := credential.GetStore().GetToken()
	if err != nil {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, err.Error())
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	backendModel := model
	if _, bm, ok := modelutil.Gemini3FlashThinkingConfig(model); ok {
		backendModel = bm
	}
	if _, bm, ok := modelutil.ClaudeOpus45ThinkingConfig(model); ok {
		backendModel = bm
	}
	vreq := &vertex.Request{
		Project:   acc.ProjectID,
		Model:     backendModel,
		RequestID: id.RequestID(),
		Request: vertex.InnerReq{
			Contents:          sanitizeContents(req.Contents),
			SystemInstruction: req.SystemInstruction,
			GenerationConfig:  toVertexGenerationConfig(model, req.GenerationConfig),
			Tools:             req.Tools,
			ToolConfig:        req.ToolConfig,
			SessionID:         acc.SessionID,
		},
	}
	vreq.RequestType = "agent"
	vreq.UserAgent = "antigravity"
	if sid := strings.TrimSpace(r.Header.Get("X-Session-ID")); sid != "" {
		vreq.Request.SessionID = sid
	}
	if rid := strings.TrimSpace(r.Header.Get("X-Request-ID")); rid != "" {
		vreq.RequestID = rid
	}
	isImageModel := strings.Contains(strings.ToLower(strings.TrimSpace(model)), "image")
	isGemini3Flash := modelutil.IsGemini3Flash(model)
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}
	if vreq.Request.SystemInstruction != nil && vreq.Request.SystemInstruction.Role == "" {
		vreq.Request.SystemInstruction.Role = "user"
	}

	startTime := time.Now()
	resp, err := vertex.GenerateContentStream(r.Context(), vreq, acc.AccessToken)
	if err != nil {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, err.Error())
		return
	}
	defer resp.Body.Close()

	vertex.SetStreamHeaders(w)

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			vertex.WriteStreamError(w, err.Error())
			return
		}
		defer gzReader.Close()
		reader = gzReader
	}

	scanner := bufio.NewScanner(reader)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 16*1024*1024)

	buildMerged := logger.IsBackendLogEnabled() || logger.IsClientLogEnabled()
	var mergedParts []any
	var lastFinishReason string
	var lastUsage any

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			jsonData := strings.TrimSpace(line[6:])
			if jsonData != "[DONE]" && jsonData != "" {
				if buildMerged {
					var rawChunk map[string]any
					if jsonpkg.UnmarshalString(jsonData, &rawChunk) == nil {
						if respMap, ok := rawChunk["response"].(map[string]any); ok {
							if usage, ok := respMap["usageMetadata"]; ok {
								lastUsage = usage
							}
							if candidates, ok := respMap["candidates"].([]any); ok && len(candidates) > 0 {
								if cand, ok := candidates[0].(map[string]any); ok {
									if fr, ok := cand["finishReason"].(string); ok && fr != "" {
										lastFinishReason = fr
									}
									if content, ok := cand["content"].(map[string]any); ok {
										if parts, ok := content["parts"].([]any); ok {
											mergedParts = append(mergedParts, parts...)
										}
									}
								}
							}
						}
					}
				}
			}

			transformed := transformGeminiStreamLine(line)
			_, _ = io.WriteString(w, transformed+"\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}

	duration := time.Since(startTime)
	if err := scanner.Err(); err != nil {
		logger.Error("Stream scan error: %v", err)
	}

	if buildMerged {
		mergedResp := map[string]any{
			"response": map[string]any{
				"candidates": []any{map[string]any{
					"content":      map[string]any{"role": "model", "parts": vertex.MergeParts(mergedParts)},
					"finishReason": lastFinishReason,
				}},
				"usageMetadata": lastUsage,
			},
		}
		if logger.IsBackendLogEnabled() {
			logger.BackendStreamResponse(http.StatusOK, duration, mergedResp)
		}
		if logger.IsClientLogEnabled() {
			logger.ClientStreamResponse(http.StatusOK, duration, mergedResp)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	b, err := jsonpkg.Marshal(v)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}
