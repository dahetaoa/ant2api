package openai

import (
	"context"
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
		httppkg.WriteOpenAIError(w, status, lastErr.Error())
		return
	}

	ids := modelutil.BuildSortedModelIDs(vm.Models)

	items := make([]ModelItem, 0, len(ids))
	for _, mid := range ids {
		owned := "google"
		if strings.HasPrefix(mid, "claude-") {
			owned = "anthropic"
		} else if strings.HasPrefix(mid, "gpt-") {
			owned = "openai"
		}
		items = append(items, ModelItem{ID: mid, Object: "model", OwnedBy: owned})
	}

	out := ModelsResponse{Object: "list", Data: items}
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
}

func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		httppkg.WriteOpenAIError(w, http.StatusBadRequest, "读取请求体失败，请检查请求是否正确发送。")
		return
	}

	if logger.IsClientLogEnabled() {
		logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)
	}

	var req ChatRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		httppkg.WriteOpenAIError(w, http.StatusBadRequest, "请求 JSON 解析失败，请检查请求体格式。")
		return
	}

	placeholder := &gwcommon.AccountContext{ProjectID: id.ProjectID(), SessionID: id.SessionID()}
	vreq, requestID, err := ToVertexRequest(&req, placeholder)
	if err != nil {
		httppkg.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	store := credential.GetStore()
	attempts := store.EnabledCount()
	if attempts < 1 {
		attempts = 1
	}

	if req.Stream {
		handleStreamWithRetry(w, ctx, &req, vreq, requestID, store, attempts)
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

		vresp, err = vertex.GenerateContent(ctx, vreq, acc.AccessToken)
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
		httppkg.WriteOpenAIError(w, status, lastErr.Error())
		return
	}

	out := ToChatCompletion(vresp, req.Model, requestID)
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
}

func handleStreamWithRetry(w http.ResponseWriter, ctx context.Context, req *ChatRequest, vreq *vertex.Request, requestID string, store *credential.Store, attempts int) {
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

		resp, err = vertex.GenerateContentStream(ctx, vreq, acc.AccessToken)
		if err == nil {
			break
		}
		if !gwcommon.ShouldRetryWithNextToken(err) {
			break
		}
	}
	if err != nil {
		httppkg.SetSSEHeaders(w)
		WriteSSEError(w, err.Error())
		return
	}

	httppkg.SetSSEHeaders(w)
	writer := NewStreamWriter(w, id.ChatCompletionID(), time.Now().Unix(), req.Model, requestID)

	streamResult, _ := vertex.ParseStreamWithResult(resp, func(data *vertex.StreamData) error {
		if len(data.Response.Candidates) == 0 {
			return nil
		}
		c := data.Response.Candidates[0]
		for _, p := range c.Content.Parts {
			if err := writer.ProcessPart(StreamDataPart{Text: p.Text, FunctionCall: p.FunctionCall, InlineData: p.InlineData, Thought: p.Thought, ThoughtSignature: p.ThoughtSignature}); err != nil {
				return err
			}
		}
		if c.FinishReason != "" {
			_ = writer.FlushToolCalls()
		}
		return nil
	})

	duration := time.Since(startTime)
	if logger.IsBackendLogEnabled() {
		logger.BackendStreamResponse(http.StatusOK, duration, streamResult.MergedResponse)
	}
	if logger.IsClientLogEnabled() {
		logger.ClientStreamResponse(http.StatusOK, duration, writer.GetMergedResponse())
	}

	finish := "stop"
	if streamResult.FinishReason != "" {
		finish = streamResult.FinishReason
	}
	writer.WriteFinish(finish, ConvertUsage(streamResult.Usage))
}
