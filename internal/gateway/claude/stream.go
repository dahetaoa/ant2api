package claude

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

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

type SSEEmitter struct {
	w                        http.ResponseWriter
	requestID                string
	model                    string
	inputTokens              int
	nextIndex                int
	textBlockIndex           *int
	thinkingBlockIndex       *int
	collectedEvents          []map[string]any
	pendingThinkingSignature string
	enableThinkingSignature  bool
	mu                       sync.Mutex
}

func SetSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func NewSSEEmitter(w http.ResponseWriter, requestID string, model string, inputTokens int, sessionID string) *SSEEmitter {
	_ = sessionID
	return &SSEEmitter{
		w:                       w,
		requestID:               requestID,
		model:                   model,
		inputTokens:             inputTokens,
		enableThinkingSignature: strings.HasPrefix(strings.TrimSpace(model), "claude-"),
	}
}

func (e *SSEEmitter) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.writeSSE("message_start", map[string]any{
		"type": "message_start",
		"message": map[string]any{
			"id":            "msg_" + e.requestID,
			"type":          "message",
			"role":          "assistant",
			"model":         e.model,
			"stop_sequence": nil,
			"usage": map[string]any{
				"input_tokens":  e.inputTokens,
				"output_tokens": 0,
			},
			"content":     []any{},
			"stop_reason": nil,
		},
	})
}

func (e *SSEEmitter) SetSignature(signature string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.enableThinkingSignature {
		return nil
	}
	s := strings.TrimSpace(signature)
	if s != "" {
		e.pendingThinkingSignature = s
	}
	return nil
}

func (e *SSEEmitter) ProcessPart(part StreamDataPart) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if part.Thought {
		return e.sendThinkingLocked(part.Text)
	}
	if part.Text != "" {
		return e.sendTextLocked(part.Text)
	}
	if part.FunctionCall != nil {
		return e.sendToolCallLocked(part.FunctionCall, part.ThoughtSignature)
	}
	return nil
}

func (e *SSEEmitter) Finish(outputTokens int, stopReason string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// If the stream ends while a thinking block is open (no tool call consumed the signature),
	// flush signature to the thinking block so clients can reconstruct it.
	if e.thinkingBlockIndex != nil && e.enableThinkingSignature && e.pendingThinkingSignature != "" {
		_ = e.sendSignatureDeltaLocked(*e.thinkingBlockIndex, e.pendingThinkingSignature)
		e.pendingThinkingSignature = ""
	}
	_ = e.closeThinkingBlockLocked()
	_ = e.closeTextBlockLocked()

	_ = e.writeSSE("message_delta", map[string]any{
		"type": "message_delta",
		"delta": map[string]any{
			"stop_reason":   stopReason,
			"stop_sequence": nil,
		},
		"usage": map[string]any{
			"output_tokens": outputTokens,
		},
	})

	return e.writeSSE("message_stop", map[string]any{"type": "message_stop"})
}

// GetMergedResponse returns a merged view of collected SSE event JSON objects,
// matching the original project's logging output.
func (e *SSEEmitter) GetMergedResponse() []any {
	e.mu.Lock()
	defer e.mu.Unlock()

	var result []any
	var pendingThinking string
	var pendingText string
	var pendingIndex int

	flushPending := func() {
		if pendingThinking != "" {
			result = append(result, map[string]any{
				"type":  "content_block_delta",
				"index": pendingIndex,
				"delta": map[string]any{"type": "thinking_delta", "thinking": pendingThinking},
			})
			pendingThinking = ""
		}
		if pendingText != "" {
			result = append(result, map[string]any{
				"type":  "content_block_delta",
				"index": pendingIndex,
				"delta": map[string]any{"type": "text_delta", "text": pendingText},
			})
			pendingText = ""
		}
	}

	for _, event := range e.collectedEvents {
		eventType, _ := event["type"].(string)
		if eventType == "content_block_delta" {
			delta, _ := event["delta"].(map[string]any)
			deltaType, _ := delta["type"].(string)
			index, _ := event["index"].(float64)
			switch deltaType {
			case "thinking_delta":
				thinking, _ := delta["thinking"].(string)
				if pendingText != "" {
					flushPending()
				}
				if pendingThinking == "" {
					pendingIndex = int(index)
				}
				pendingThinking += thinking
				continue
			case "text_delta":
				text, _ := delta["text"].(string)
				if pendingThinking != "" {
					flushPending()
				}
				if pendingText == "" {
					pendingIndex = int(index)
				}
				pendingText += text
				continue
			}
		}

		flushPending()
		result = append(result, event)
	}

	flushPending()
	return result
}

func (e *SSEEmitter) ensureTextBlock() error {
	if e.textBlockIndex != nil {
		return nil
	}
	idx := e.nextIndex
	e.nextIndex++
	e.textBlockIndex = &idx
	return e.writeSSE("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": idx,
		"content_block": map[string]any{
			"type": "text",
			"text": "",
		},
	})
}

func (e *SSEEmitter) ensureThinkingBlock() error {
	if e.thinkingBlockIndex != nil {
		return nil
	}
	idx := e.nextIndex
	e.nextIndex++
	e.thinkingBlockIndex = &idx
	return e.writeSSE("content_block_start", map[string]any{
		"type":  "content_block_start",
		"index": idx,
		"content_block": map[string]any{
			"type":     "thinking",
			"thinking": "",
		},
	})
}

func (e *SSEEmitter) sendTextLocked(text string) error {
	// If we're switching away from a thinking block (to text), flush signature to the thinking block.
	if e.thinkingBlockIndex != nil && e.enableThinkingSignature && e.pendingThinkingSignature != "" {
		_ = e.sendSignatureDeltaLocked(*e.thinkingBlockIndex, e.pendingThinkingSignature)
		e.pendingThinkingSignature = ""
	}
	_ = e.closeThinkingBlockLocked()
	if err := e.ensureTextBlock(); err != nil {
		return err
	}
	return e.writeSSE("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": *e.textBlockIndex,
		"delta": map[string]any{"type": "text_delta", "text": text},
	})
}

func (e *SSEEmitter) sendThinkingLocked(text string) error {
	_ = e.closeTextBlockLocked()
	if err := e.ensureThinkingBlock(); err != nil {
		return err
	}
	return e.writeSSE("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": *e.thinkingBlockIndex,
		"delta": map[string]any{"type": "thinking_delta", "thinking": text},
	})
}

func (e *SSEEmitter) sendToolCallLocked(fc *vertex.FunctionCall, thoughtSignature string) error {
	_ = e.closeThinkingBlockLocked()
	_ = e.closeTextBlockLocked()
	idx := e.nextIndex
	e.nextIndex++
	toolID := fc.ID
	if toolID == "" {
		toolID = "toolu_" + id.RequestID()
		fc.ID = toolID
	}
	block := map[string]any{"type": "tool_use", "id": toolID, "name": fc.Name, "input": fc.Args}
	if err := e.writeSSE("content_block_start", map[string]any{"type": "content_block_start", "index": idx, "content_block": block}); err != nil {
		return err
	}
	sig := strings.TrimSpace(thoughtSignature)
	if sig == "" {
		sig = e.pendingThinkingSignature
	}
	if sig != "" {
		signature.GetManager().Save(e.requestID, fc.ID, sig, e.model)
		// Bind the signature to this functionCall; do not attach it to thinking blocks.
		// Keep pendingThinkingSignature so multiple tool calls in the same turn can reuse it
		// unless a new signature arrives.
	}
	return e.writeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
}

func (e *SSEEmitter) closeThinkingBlockLocked() error {
	if e.thinkingBlockIndex == nil {
		return nil
	}
	idx := *e.thinkingBlockIndex
	e.thinkingBlockIndex = nil
	return e.writeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
}

func (e *SSEEmitter) closeTextBlockLocked() error {
	if e.textBlockIndex == nil {
		return nil
	}
	idx := *e.textBlockIndex
	e.textBlockIndex = nil
	return e.writeSSE("content_block_stop", map[string]any{"type": "content_block_stop", "index": idx})
}

func (e *SSEEmitter) sendSignatureDeltaLocked(index int, signature string) error {
	if signature == "" {
		return nil
	}
	return e.writeSSE("content_block_delta", map[string]any{
		"type":  "content_block_delta",
		"index": index,
		"delta": map[string]any{"type": "signature_delta", "signature": signature},
	})
}

func (e *SSEEmitter) writeSSE(event string, data any) error {
	b, err := jsonpkg.Marshal(data)
	if err != nil {
		return err
	}

	var eventData map[string]any
	if err := jsonpkg.Unmarshal(b, &eventData); err == nil {
		e.collectedEvents = append(e.collectedEvents, eventData)
	}

	if _, err := fmt.Fprintf(e.w, "event: %s\ndata: %s\n\n", event, b); err != nil {
		return err
	}
	if f, ok := e.w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}
