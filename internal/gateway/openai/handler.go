package openai

import (
	"context"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"anti2api-golang/refactor/internal/credential"
	"anti2api-golang/refactor/internal/logger"
	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/vertex"
)

func HandleListModels(w http.ResponseWriter, r *http.Request) {
	logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, nil)
	startTime := time.Now()
	acc, err := credential.GetStore().GetToken()
	if err != nil {
		logger.ClientResponse(http.StatusServiceUnavailable, time.Since(startTime), err.Error())
		writeOpenAIError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	vm, err := vertex.FetchAvailableModels(r.Context(), acc.ProjectID, acc.AccessToken)
	if err != nil {
		logger.ClientResponse(statusFromVertexErr(err), time.Since(startTime), err.Error())
		writeOpenAIError(w, statusFromVertexErr(err), err.Error())
		return
	}

	ids := make([]string, 0, len(vm.Models))
	for k := range vm.Models {
		k = strings.TrimSpace(k)
		if k != "" {
			ids = append(ids, k)
		}
	}
	sort.Strings(ids)

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
	logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	writeJSON(w, http.StatusOK, out)
}

func HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "读取请求体失败，请检查请求是否正确发送。")
		return
	}

	logger.ClientRequestWithHeaders(r.Method, r.URL.Path, r.Header, body)

	var req ChatRequest
	if err := jsonpkg.Unmarshal(body, &req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "请求 JSON 解析失败，请检查请求体格式。")
		return
	}

	acc, err := credential.GetStore().GetToken()
	if err != nil {
		writeOpenAIError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	acct := &AccountContext{ProjectID: acc.ProjectID, SessionID: acc.SessionID, AccessToken: acc.AccessToken}
	vreq, requestID, err := ToVertexRequest(&req, acct)
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = requestID

	ctx := r.Context()
	if req.Stream {
		handleStream(w, ctx, r, &req, vreq, requestID, acct)
		return
	}

	startTime := time.Now()
	vresp, err := vertex.GenerateContent(ctx, vreq, acc.AccessToken)
	if err != nil {
		logger.ClientResponse(statusFromVertexErr(err), time.Since(startTime), err.Error())
		writeOpenAIError(w, statusFromVertexErr(err), err.Error())
		return
	}

	out := ToChatCompletion(vresp, req.Model, requestID, "")
	logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	writeJSON(w, http.StatusOK, out)
}

func handleStream(w http.ResponseWriter, ctx context.Context, r *http.Request, req *ChatRequest, vreq *vertex.Request, requestID string, acct *AccountContext) {
	startTime := time.Now()
	resp, err := vertex.GenerateContentStream(ctx, vreq, acct.AccessToken)
	if err != nil {
		SetSSEHeaders(w)
		WriteSSEError(w, err.Error())
		return
	}

	SetSSEHeaders(w)
	writer := NewStreamWriter(w, id.ChatCompletionID(), time.Now().Unix(), req.Model, requestID, "")

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
	logger.BackendStreamResponse(http.StatusOK, duration, streamResult.MergedResponse)
	logger.ClientStreamResponse(http.StatusOK, duration, writer.GetMergedResponse())

	finish := "stop"
	if streamResult.FinishReason != "" {
		finish = streamResult.FinishReason
	}
	writer.WriteFinish(finish, ConvertUsage(streamResult.Usage))
}

func statusFromVertexErr(err error) int {
	if apiErr, ok := err.(*vertex.APIError); ok {
		return apiErr.Status
	}
	return http.StatusInternalServerError
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

func writeOpenAIError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"error":{"message":`))
	b, _ := jsonpkg.MarshalString(msg)
	_, _ = w.Write([]byte(b))
	_, _ = w.Write([]byte(`,"type":"server_error"}}`))
}
