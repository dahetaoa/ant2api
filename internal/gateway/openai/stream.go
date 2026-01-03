package openai

import (
	"fmt"
	"net/http"
	"sync"
	"unicode/utf8"

	"anti2api-golang/refactor/internal/pkg/id"
	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

type StreamDataPart struct {
	Text             string
	FunctionCall     *vertex.FunctionCall
	Thought          bool
	ThoughtSignature string
}

type StreamWriter struct {
	w               http.ResponseWriter
	id              string
	created         int64
	model           string
	requestID       string
	sentRole        bool
	contentBuf      []byte
	reasoningBuf    []byte
	toolCalls       []ToolCall
	collectedEvents []map[string]any
	mu              sync.Mutex
}

func NewStreamWriter(w http.ResponseWriter, id string, created int64, model string, requestID string, sessionID string) *StreamWriter {
	SetSSEHeaders(w)
	_ = sessionID
	return &StreamWriter{w: w, id: id, created: created, model: model, requestID: requestID}
}

func SetSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func WriteSSEError(w http.ResponseWriter, msg string) {
	_ = writeSSEData(w, map[string]any{"error": map[string]any{"message": msg, "type": "server_error"}})
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
}

func (sw *StreamWriter) ProcessPart(part StreamDataPart) error {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	if part.Thought {
		return sw.writeReasoningLocked(part.Text)
	}
	if part.Text != "" {
		return sw.writeContentLocked(part.Text)
	}
	if part.FunctionCall != nil {
		toolCallID := part.FunctionCall.ID
		if toolCallID == "" {
			toolCallID = id.ToolCallID()
		}
		extra := (*ToolExtra)(nil)
		if part.ThoughtSignature != "" {
			extra = &ToolExtra{Google: &GoogleExtra{ThoughtSignature: part.ThoughtSignature}}
			signature.GetManager().Save(sw.requestID, toolCallID, part.ThoughtSignature, sw.model)
		}
		args := "{}"
		if part.FunctionCall.Args != nil {
			if s, err := jsonpkg.MarshalString(part.FunctionCall.Args); err == nil {
				args = s
			}
		}
		sw.toolCalls = append(sw.toolCalls, ToolCall{ID: toolCallID, Type: "function", Function: FunctionCall{Name: part.FunctionCall.Name, Arguments: args}, ExtraContent: extra})
	}
	return nil
}

func (sw *StreamWriter) FlushToolCalls() error {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	if len(sw.toolCalls) == 0 {
		return nil
	}
	if err := sw.writeToolCallsLocked(sw.toolCalls); err != nil {
		return err
	}
	sw.toolCalls = nil
	return nil
}

func (sw *StreamWriter) WriteFinish(finishReason string, usage *Usage) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	_ = sw.writeRoleLocked()
	_ = sw.writeSSEChunkLocked(&Delta{}, &finishReason, usage)
	_, _ = sw.w.Write([]byte("data: [DONE]\n\n"))
}

func (sw *StreamWriter) writeRoleLocked() error {
	if sw.sentRole {
		return nil
	}
	sw.sentRole = true
	return sw.writeSSEChunkLocked(&Delta{Role: "assistant"}, nil, nil)
}

func (sw *StreamWriter) writeContentLocked(s string) error {
	_ = sw.writeRoleLocked()
	sw.contentBuf = append(sw.contentBuf, []byte(s)...)
	valid, rest := extractValidUTF8(sw.contentBuf)
	sw.contentBuf = rest
	if valid == "" {
		return nil
	}
	return sw.writeSSEChunkLocked(&Delta{Content: valid}, nil, nil)
}

func (sw *StreamWriter) writeReasoningLocked(s string) error {
	_ = sw.writeRoleLocked()
	sw.reasoningBuf = append(sw.reasoningBuf, []byte(s)...)
	valid, rest := extractValidUTF8(sw.reasoningBuf)
	sw.reasoningBuf = rest
	if valid == "" {
		return nil
	}
	return sw.writeSSEChunkLocked(&Delta{Reasoning: valid}, nil, nil)
}

func (sw *StreamWriter) writeToolCallsLocked(calls []ToolCall) error {
	_ = sw.writeRoleLocked()
	return sw.writeSSEChunkLocked(&Delta{ToolCalls: calls}, nil, nil)
}

func (sw *StreamWriter) writeSSEChunkLocked(delta *Delta, finishReason *string, usage *Usage) error {
	chunk := &ChatCompletion{
		ID:      sw.id,
		Object:  "chat.completion.chunk",
		Created: sw.created,
		Model:   sw.model,
		Choices: []Choice{{Index: 0, Delta: delta, FinishReason: finishReason}},
		Usage:   usage,
	}
	return sw.writeSSEDataAndCollect(chunk)
}

func (sw *StreamWriter) writeSSEDataAndCollect(v any) error {
	b, err := jsonpkg.Marshal(v)
	if err != nil {
		return err
	}

	var event map[string]any
	if err := jsonpkg.Unmarshal(b, &event); err == nil {
		sw.collectedEvents = append(sw.collectedEvents, event)
	}

	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", b); err != nil {
		return err
	}
	if f, ok := sw.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func writeSSEData(w http.ResponseWriter, v any) error {
	b, err := jsonpkg.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

// GetMergedResponse returns a merged view of collected SSE events, matching the
// original project's logging output. It merges consecutive content/reasoning
// deltas into single chunk entries for readability.
func (sw *StreamWriter) GetMergedResponse() []any {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	var result []any
	var pendingContent string
	var pendingReasoning string

	flushPending := func() {
		if pendingReasoning != "" {
			result = append(result, map[string]any{
				"id":      sw.id,
				"object":  "chat.completion.chunk",
				"created": sw.created,
				"model":   sw.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"reasoning": pendingReasoning}}},
			})
			pendingReasoning = ""
		}
		if pendingContent != "" {
			result = append(result, map[string]any{
				"id":      sw.id,
				"object":  "chat.completion.chunk",
				"created": sw.created,
				"model":   sw.model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": pendingContent}}},
			})
			pendingContent = ""
		}
	}

	for _, event := range sw.collectedEvents {
		choices, ok := event["choices"].([]any)
		if !ok || len(choices) == 0 {
			flushPending()
			result = append(result, event)
			continue
		}
		choice, ok := choices[0].(map[string]any)
		if !ok {
			flushPending()
			result = append(result, event)
			continue
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			flushPending()
			result = append(result, event)
			continue
		}

		if content, ok := delta["content"].(string); ok && content != "" {
			if pendingReasoning != "" {
				flushPending()
			}
			pendingContent += content
			continue
		}

		if reasoning, ok := delta["reasoning"].(string); ok && reasoning != "" {
			if pendingContent != "" {
				flushPending()
			}
			pendingReasoning += reasoning
			continue
		}

		flushPending()
		result = append(result, event)
	}

	flushPending()
	return result
}

func extractValidUTF8(data []byte) (valid string, remaining []byte) {
	if len(data) == 0 {
		return "", nil
	}
	if utf8.Valid(data) {
		return string(data), nil
	}
	checkLen := 4
	if len(data) < checkLen {
		checkLen = len(data)
	}
	for i := 1; i <= checkLen; i++ {
		idx := len(data) - i
		b := data[idx]
		if b >= 0xC0 {
			expectedLen := 2
			if b >= 0xF0 {
				expectedLen = 4
			} else if b >= 0xE0 {
				expectedLen = 3
			}
			actualLen := len(data) - idx
			if actualLen < expectedLen {
				return string(data[:idx]), data[idx:]
			}
			break
		}
		if b >= 0x80 && b < 0xC0 {
			continue
		}
		break
	}
	for len(data) > 0 {
		if utf8.Valid(data) {
			return string(data), nil
		}
		data = data[:len(data)-1]
	}
	return "", nil
}
