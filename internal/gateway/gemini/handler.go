package gemini

import (
	"bufio"
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/credential"
	gwcommon "anti2api-golang/refactor/internal/gateway/common"
	"anti2api-golang/refactor/internal/logger"
	httppkg "anti2api-golang/refactor/internal/pkg/http"
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
	ImageConfig     *GeminiImageCfg    `json:"imageConfig,omitempty"`
}

type GeminiThinkingCfg struct {
	IncludeThoughts bool   `json:"includeThoughts"`
	ThinkingBudget  int    `json:"thinkingBudget,omitempty"`
	ThinkingLevel   string `json:"thinkingLevel,omitempty"`
}

type GeminiImageCfg struct {
	AspectRatio string `json:"aspectRatio,omitempty"`
	ImageSize   string `json:"imageSize,omitempty"`
}

type GeminiResponse struct {
	Candidates    []vertex.Candidate    `json:"candidates"`
	UsageMetadata *vertex.UsageMetadata `json:"usageMetadata,omitempty"`
}

func toVertexGenerationConfig(model string, cfg *GeminiGenerationConfig) *vertex.GenerationConfig {
	model = strings.TrimSpace(model)
	isClaude := modelutil.IsClaude(model)
	isGemini := modelutil.IsGemini(model)
	forcedThinking, forced := modelutil.ForcedThinkingConfig(model)
	isGeminiProImage := modelutil.IsGeminiProImage(model)
	forcedImageSize, _, forcedImage := modelutil.GeminiProImageSizeConfig(model)

	if cfg == nil {
		if isClaude {
			out := &vertex.GenerationConfig{CandidateCount: 1, MaxOutputTokens: modelutil.ClaudeMaxOutputTokens}
			if forced {
				out.ThinkingConfig = forcedThinking
			}
			return out
		}
		if isGemini {
			out := &vertex.GenerationConfig{CandidateCount: 1, MaxOutputTokens: modelutil.GeminiMaxOutputTokens}
			if forced {
				out.ThinkingConfig = forcedThinking
			}
			if isGeminiProImage && forcedImage {
				out.ImageConfig = &vertex.ImageConfig{ImageSize: forcedImageSize}
			}
			return out
		}
		return nil
	}
	out := &vertex.GenerationConfig{CandidateCount: cfg.CandidateCount, StopSequences: cfg.StopSequences, MaxOutputTokens: cfg.MaxOutputTokens, TopK: cfg.TopK}
	out.Temperature = cfg.Temperature
	out.TopP = cfg.TopP
	if forced {
		// Gemini 3 Flash / Claude 4.5：忽略客户端 thinking 参数，由模型名强制决定。
		out.ThinkingConfig = forcedThinking
	} else if cfg.ThinkingConfig != nil {
		if cfg.ThinkingConfig.IncludeThoughts {
			out.ThinkingConfig = modelutil.ThinkingConfigFromGemini(model, true, cfg.ThinkingConfig.ThinkingBudget, cfg.ThinkingConfig.ThinkingLevel)
		} else {
			// 保持原行为：客户端显式传 includeThoughts=false 时也透传该结构。
			out.ThinkingConfig = &vertex.ThinkingConfig{
				IncludeThoughts: false,
				ThinkingBudget:  cfg.ThinkingConfig.ThinkingBudget,
				ThinkingLevel:   cfg.ThinkingConfig.ThinkingLevel,
			}
		}
	}

	// Claude models: maxOutputTokens is fixed at 64000.
	if isClaude {
		out.MaxOutputTokens = modelutil.ClaudeMaxOutputTokens
	}
	// Gemini models: maxOutputTokens is fixed at 65535.
	if isGemini {
		out.MaxOutputTokens = modelutil.GeminiMaxOutputTokens
	}

	// When thinkingBudget is used, ensure it's compatible with maxOutputTokens.
	if out.ThinkingConfig != nil && out.ThinkingConfig.IncludeThoughts {
		if out.MaxOutputTokens <= 0 {
			if isClaude {
				out.MaxOutputTokens = modelutil.ClaudeMaxOutputTokens
			} else if isGemini {
				out.MaxOutputTokens = modelutil.GeminiMaxOutputTokens
			} else if out.ThinkingConfig.ThinkingBudget > 0 {
				out.MaxOutputTokens = out.ThinkingConfig.ThinkingBudget + modelutil.ThinkingMaxOutputTokensOverheadTokens
			} else {
				out.MaxOutputTokens = 8192
			}
		}
		if out.ThinkingConfig.ThinkingBudget > 0 {
			if isClaude {
				maxBudget := out.MaxOutputTokens - modelutil.ThinkingBudgetHeadroomTokens
				if maxBudget < modelutil.ThinkingBudgetMinTokens {
					maxBudget = modelutil.ThinkingBudgetMinTokens
				}
				if out.ThinkingConfig.ThinkingBudget > maxBudget {
					out.ThinkingConfig.ThinkingBudget = maxBudget
				}
			} else if isGemini && out.MaxOutputTokens <= out.ThinkingConfig.ThinkingBudget {
				maxBudget := out.MaxOutputTokens - modelutil.ThinkingBudgetHeadroomTokens
				if maxBudget < modelutil.ThinkingBudgetMinTokens {
					maxBudget = modelutil.ThinkingBudgetMinTokens
				}
				out.ThinkingConfig.ThinkingBudget = maxBudget
			} else if out.MaxOutputTokens <= out.ThinkingConfig.ThinkingBudget {
				out.MaxOutputTokens = out.ThinkingConfig.ThinkingBudget + modelutil.ThinkingMaxOutputTokensOverheadTokens
			}
		}
	}

	if isGeminiProImage {
		var aspectRatio string
		var imageSize string
		if cfg.ImageConfig != nil {
			aspectRatio = strings.TrimSpace(cfg.ImageConfig.AspectRatio)
			imageSize = strings.TrimSpace(cfg.ImageConfig.ImageSize)
		}
		if forcedImage {
			imageSize = forcedImageSize
		}

		if aspectRatio != "" || imageSize != "" {
			out.ImageConfig = &vertex.ImageConfig{}
			if aspectRatio != "" {
				out.ImageConfig.AspectRatio = aspectRatio
			}
			if imageSize != "" {
				out.ImageConfig.ImageSize = imageSize
			}
		}
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
			httppkg.WriteJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": map[string]any{"message": "不支持的请求方法，请使用 GET。"}})
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
	store := credential.GetStore()
	attempts := store.EnabledCount()
	if attempts < 1 {
		attempts = 1
	}

	var vm *vertex.AvailableModelsResponse
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		acc, err := store.GetToken()
		if err != nil {
			lastErr = err
			break
		}
		projectID := acc.ProjectID
		if projectID == "" {
			projectID = id.ProjectID()
		}
		vm, err = vertex.FetchAvailableModels(r.Context(), projectID, acc.AccessToken)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if lastErr != nil || vm == nil {
		status := gwcommon.StatusFromVertexError(lastErr)
		if _, ok := lastErr.(*vertex.APIError); !ok {
			status = http.StatusServiceUnavailable
		}
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(status, time.Since(startTime), lastErr.Error())
		}
		httppkg.WriteJSON(w, status, map[string]any{"error": map[string]any{"message": lastErr.Error()}})
		return
	}
	ids := modelutil.BuildSortedModelIDs(vm.Models)
	models := make([]GeminiModel, 0, len(ids))
	for _, modelID := range ids {
		desc := "Model provided by google"
		if _, ok := vm.Models[modelID]; !ok {
			switch strings.ToLower(strings.TrimSpace(modelID)) {
			case "gemini-3-flash-thinking":
				desc = "Virtual model provided by google (gemini-3-flash with thinkingLevel=high)"
			case "claude-opus-4-5":
				desc = "Virtual model provided by anthropic (claude-opus-4-5-thinking with thinkingBudget=0)"
			}
		}
		models = append(models, GeminiModel{
			Name:        "models/" + modelID,
			DisplayName: modelID,
			Description: desc,
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
	httppkg.WriteJSON(w, http.StatusOK, out)
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
		httppkg.WriteJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "未找到对应的模型或接口。"}})
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httppkg.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "读取请求体失败，请检查请求是否正确发送。"}})
		return
	}

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}
	var req GeminiRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		httppkg.WriteJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "请求 JSON 解析失败，请检查请求体格式。"}})
		return
	}

	store := credential.GetStore()
	attempts := store.EnabledCount()
	if attempts < 1 {
		attempts = 1
	}

	backendModel := modelutil.BackendModelID(model)
	vreq := &vertex.Request{
		Project:   id.ProjectID(),
		Model:     backendModel,
		RequestID: id.RequestID(),
		Request: vertex.InnerReq{
			Contents:          vertex.SanitizeContents(req.Contents),
			SystemInstruction: req.SystemInstruction,
			GenerationConfig:  toVertexGenerationConfig(model, req.GenerationConfig),
			Tools:             req.Tools,
			ToolConfig:        req.ToolConfig,
			SessionID:         id.SessionID(),
		},
	}
	vreq.RequestType = "agent"
	vreq.UserAgent = "antigravity"
	overrideSessionID := false
	if sid := strings.TrimSpace(r.Header.Get("X-Session-ID")); sid != "" {
		overrideSessionID = true
		vreq.Request.SessionID = sid
	}
	if rid := strings.TrimSpace(r.Header.Get("X-Request-ID")); rid != "" {
		vreq.RequestID = rid
	}
	isImageModel := modelutil.IsImageModel(model)
	isGemini3Flash := modelutil.IsGemini3Flash(model)
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}
	if vreq.Request.SystemInstruction != nil && vreq.Request.SystemInstruction.Role == "" {
		vreq.Request.SystemInstruction.Role = "user"
	}

	startTime := time.Now()
	var resp *vertex.Response
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		acc, err := store.GetToken()
		if err != nil {
			lastErr = err
			break
		}
		projectID := acc.ProjectID
		if projectID == "" {
			projectID = id.ProjectID()
		}
		vreq.Project = projectID
		if !overrideSessionID {
			vreq.Request.SessionID = acc.SessionID
		}

		resp, err = vertex.GenerateContent(r.Context(), vreq, acc.AccessToken)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if lastErr != nil || resp == nil {
		status := gwcommon.StatusFromVertexError(lastErr)
		if _, ok := lastErr.(*vertex.APIError); !ok {
			status = http.StatusServiceUnavailable
		}
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(status, time.Since(startTime), lastErr.Error())
		}
		httppkg.WriteJSON(w, status, map[string]any{"error": map[string]any{"message": lastErr.Error()}})
		return
	}

	out := &GeminiResponse{Candidates: resp.Response.Candidates, UsageMetadata: resp.Response.UsageMetadata}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
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

	store := credential.GetStore()
	attempts := store.EnabledCount()
	if attempts < 1 {
		attempts = 1
	}

	backendModel := modelutil.BackendModelID(model)
	vreq := &vertex.Request{
		Project:   id.ProjectID(),
		Model:     backendModel,
		RequestID: id.RequestID(),
		Request: vertex.InnerReq{
			Contents:          vertex.SanitizeContents(req.Contents),
			SystemInstruction: req.SystemInstruction,
			GenerationConfig:  toVertexGenerationConfig(model, req.GenerationConfig),
			Tools:             req.Tools,
			ToolConfig:        req.ToolConfig,
			SessionID:         id.SessionID(),
		},
	}
	vreq.RequestType = "agent"
	vreq.UserAgent = "antigravity"
	overrideSessionID := false
	if sid := strings.TrimSpace(r.Header.Get("X-Session-ID")); sid != "" {
		overrideSessionID = true
		vreq.Request.SessionID = sid
	}
	if rid := strings.TrimSpace(r.Header.Get("X-Request-ID")); rid != "" {
		vreq.RequestID = rid
	}
	isImageModel := modelutil.IsImageModel(model)
	isGemini3Flash := modelutil.IsGemini3Flash(model)
	shouldSkipSystemPrompt := isImageModel || isGemini3Flash
	if !shouldSkipSystemPrompt {
		vreq.Request.SystemInstruction = vertex.InjectAgentSystemPrompt(vreq.Request.SystemInstruction)
	}
	if vreq.Request.SystemInstruction != nil && vreq.Request.SystemInstruction.Role == "" {
		vreq.Request.SystemInstruction.Role = "user"
	}

	startTime := time.Now()
	var resp *http.Response
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		acc, err := store.GetToken()
		if err != nil {
			lastErr = err
			break
		}
		projectID := acc.ProjectID
		if projectID == "" {
			projectID = id.ProjectID()
		}
		vreq.Project = projectID
		if !overrideSessionID {
			vreq.Request.SessionID = acc.SessionID
		}

		resp, err = vertex.GenerateContentStream(r.Context(), vreq, acc.AccessToken)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if lastErr != nil || resp == nil {
		vertex.SetStreamHeaders(w)
		vertex.WriteStreamError(w, lastErr.Error())
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

// JSON 输出统一由 internal/pkg/http 处理。
