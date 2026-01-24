package claude

import (
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
	"anti2api-golang/refactor/internal/pkg/memory"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/vertex"
)

type ModelListResponse struct {
	Data []ModelItem `json:"data"`
}

type ModelItem struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	DisplayName string `json:"display_name,omitempty"`
}

func HandleMessages(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httppkg.WriteClaudeError(w, http.StatusBadRequest, "读取请求体失败，请检查请求是否正确发送。")
		return
	}
	defer memory.AfterLargeRequest(int64(len(body)))

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}

	var req MessagesRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		httppkg.WriteClaudeError(w, http.StatusBadRequest, "请求 JSON 解析失败，请检查请求体格式。")
		return
	}

	placeholder := &gwcommon.AccountContext{ProjectID: id.ProjectID(), SessionID: id.SessionID()}
	vreq, requestID, err := ToVertexRequest(&req, placeholder)
	if err != nil {
		httppkg.WriteClaudeError(w, http.StatusBadRequest, err.Error())
		return
	}

	inputTokens := estimateTokens(body)
	store := credential.GetStore()
	attempts := store.EnabledCount()
	if attempts < 1 {
		attempts = 1
	}
	if req.Stream {
		handleStreamWithRetry(w, r, &req, vreq, requestID, inputTokens, store, attempts)
		return
	}

	startTime := time.Now()
	var vresp *vertex.Response
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
		vreq.Request.SessionID = acc.SessionID

		vresp, err = vertex.GenerateContent(r.Context(), vreq, acc.AccessToken)
		if err == nil {
			lastErr = nil
			break
		}
		lastErr = err
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if lastErr != nil || vresp == nil {
		status := gwcommon.StatusFromVertexError(lastErr)
		if _, ok := lastErr.(*vertex.APIError); !ok {
			status = http.StatusServiceUnavailable
		}
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(status, time.Since(startTime), lastErr.Error())
		}
		httppkg.WriteClaudeError(w, status, lastErr.Error())
		return
	}

	out := ToMessagesResponse(vresp, requestID, req.Model, inputTokens)
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
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
		httppkg.WriteClaudeError(w, status, lastErr.Error())
		return
	}

	ids := modelutil.BuildSortedModelIDs(vm.Models)

	items := make([]ModelItem, 0, len(ids))
	for _, mid := range ids {
		items = append(items, ModelItem{ID: mid, Type: "model", DisplayName: mid})
	}

	out := ModelListResponse{Data: items}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
}

func HandleCountTokens(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httppkg.WriteClaudeError(w, http.StatusBadRequest, "读取请求体失败，请检查请求是否正确发送。")
		return
	}

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}
	// Use same request schema.
	var req MessagesRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		httppkg.WriteClaudeError(w, http.StatusBadRequest, "请求 JSON 解析失败，请检查请求体格式。")
		return
	}
	startTime := time.Now()
	count := estimateTokens(body)
	out := TokenCountResponse{InputTokens: count, TokenCount: count, Tokens: count}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
}

func handleStreamWithRetry(w http.ResponseWriter, r *http.Request, req *MessagesRequest, vreq *vertex.Request, requestID string, inputTokens int, store *credential.Store, attempts int) {
	startTime := time.Now()
	var resp *http.Response
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		acc, accErr := store.GetToken()
		if accErr != nil {
			err = accErr
			break
		}
		projectID := acc.ProjectID
		if projectID == "" {
			projectID = id.ProjectID()
		}
		vreq.Project = projectID
		vreq.Request.SessionID = acc.SessionID

		resp, err = vertex.GenerateContentStream(r.Context(), vreq, acc.AccessToken)
		if err == nil {
			break
		}
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if err != nil {
		httppkg.SetSSEHeaders(w)
		_ = writeSSEError(w, err.Error())
		return
	}

	httppkg.SetSSEHeaders(w)
	emitter := NewSSEEmitter(w, requestID, req.Model, inputTokens)
	_ = emitter.Start()

	streamResult, _ := vertex.ParseStreamWithResult(resp, func(data *vertex.StreamData) error {
		if len(data.Response.Candidates) == 0 {
			return nil
		}
		c := data.Response.Candidates[0]
		for _, p := range c.Content.Parts {
			// Claude extended thinking signatures belong to thinking blocks (not tool_use).
			if p.Thought && p.ThoughtSignature != "" {
				_ = emitter.SetSignature(p.ThoughtSignature)
			}
		}
		for _, p := range c.Content.Parts {
			if err := emitter.ProcessPart(StreamDataPart{Text: p.Text, FunctionCall: p.FunctionCall, Thought: p.Thought, ThoughtSignature: p.ThoughtSignature}); err != nil {
				return err
			}
		}
		return nil
	})

	duration := time.Since(startTime)
	if logger.IsBackendLogEnabled() {
		logger.BackendStreamResponse(http.StatusOK, duration, streamResult.MergedResponse)
	}
	if logger.IsClientLogEnabled() {
		logger.ClientStreamResponse(http.StatusOK, duration, emitter.GetMergedResponse())
	}

	stopReason := "end_turn"
	if len(streamResult.ToolCalls) > 0 {
		stopReason = "tool_use"
	}
	_ = emitter.Finish(outputTokens(streamResult.Usage), stopReason)
}

func outputTokens(usage *vertex.UsageMetadata) int {
	if usage == nil {
		return 0
	}
	return usage.CandidatesTokenCount
}

func estimateTokens(body []byte) int {
	// simple heuristic compatible with existing project behavior
	if len(body) == 0 {
		return 0
	}
	c := len(string(body)) / 4
	if c < 1 {
		return 1
	}
	return c
}

func writeSSEError(w http.ResponseWriter, msg string) error {
	encoded, _ := jsonpkg.MarshalString(msg)
	_, err := w.Write([]byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"api_error\",\"message\":" + strings.Trim(encoded, "\"") + "}}\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return err
}
