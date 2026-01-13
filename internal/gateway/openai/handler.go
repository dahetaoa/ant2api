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
	acc, err := credential.GetStore().GetToken()
	if err != nil {
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(http.StatusServiceUnavailable, time.Since(startTime), err.Error())
		}
		httppkg.WriteOpenAIError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	vm, err := vertex.FetchAvailableModels(r.Context(), acc.ProjectID, acc.AccessToken)
	if err != nil {
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(gwcommon.StatusFromVertexError(err), time.Since(startTime), err.Error())
		}
		httppkg.WriteOpenAIError(w, gwcommon.StatusFromVertexError(err), err.Error())
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

	acc, err := credential.GetStore().GetToken()
	if err != nil {
		httppkg.WriteOpenAIError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if acc.ProjectID == "" {
		acc.ProjectID = id.ProjectID()
	}

	acct := &gwcommon.AccountContext{ProjectID: acc.ProjectID, SessionID: acc.SessionID, AccessToken: acc.AccessToken}
	vreq, requestID, err := ToVertexRequest(&req, acct)
	if err != nil {
		httppkg.WriteOpenAIError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx := r.Context()
	if req.Stream {
		handleStream(w, ctx, &req, vreq, requestID, acct)
		return
	}

	startTime := time.Now()
	vresp, err := vertex.GenerateContent(ctx, vreq, acc.AccessToken)
	if err != nil {
		if logger.IsClientLogEnabled() {
			logger.ClientResponse(gwcommon.StatusFromVertexError(err), time.Since(startTime), err.Error())
		}
		httppkg.WriteOpenAIError(w, gwcommon.StatusFromVertexError(err), err.Error())
		return
	}

	out := ToChatCompletion(vresp, req.Model, requestID)
	if logger.IsClientLogEnabled() {
		logger.ClientResponse(http.StatusOK, time.Since(startTime), out)
	}
	httppkg.WriteJSON(w, http.StatusOK, out)
}

func handleStream(w http.ResponseWriter, ctx context.Context, req *ChatRequest, vreq *vertex.Request, requestID string, acct *gwcommon.AccountContext) {
	startTime := time.Now()
	resp, err := vertex.GenerateContentStream(ctx, vreq, acct.AccessToken)
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
