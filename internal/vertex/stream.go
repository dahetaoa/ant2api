package vertex

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"strings"

	jsonpkg "anti2api-golang/refactor/internal/pkg/json"
)

type StreamData struct {
	Response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text             string        `json:"text,omitempty"`
					FunctionCall     *FunctionCall `json:"functionCall,omitempty"`
					Thought          bool          `json:"thought,omitempty"`
					ThoughtSignature string        `json:"thoughtSignature,omitempty"`
				} `json:"parts"`
			} `json:"content"`
			FinishReason string        `json:"finishReason,omitempty"`
		} `json:"candidates"`
		UsageMetadata *UsageMetadata `json:"usageMetadata,omitempty"`
	} `json:"response"`
}

type StreamResult struct {
	RawChunks       []map[string]any `json:"-"`
	MergedResponse  map[string]any   `json:"-"`
	Text            string           `json:"-"`
	Thinking        string           `json:"-"`
	FinishReason    string           `json:"-"`
	Usage           *UsageMetadata   `json:"-"`
	ToolCalls       []ToolCallInfo   `json:"-"`
	ThoughtSignature string          `json:"-"`
}

type ToolCallInfo struct {
	ID               string         `json:"id"`
	Name             string         `json:"name"`
	Args             map[string]any `json:"args"`
	ThoughtSignature string         `json:"thoughtSignature,omitempty"`
}

func ParseStreamWithResult(resp *http.Response, receiver func(data *StreamData) error) (*StreamResult, error) {
	defer resp.Body.Close()

	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return &StreamResult{}, err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	bufReader := bufio.NewReaderSize(reader, 4*1024)

	result := &StreamResult{}
	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder

	var rawChunks []map[string]any
	var mergedParts []any
	var lastFinishReason string
	var lastUsage any

	for {
		line, err := bufReader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			result.Text = textBuilder.String()
			result.Thinking = thinkingBuilder.String()
			return result, err
		}

		line = strings.TrimSuffix(line, "\n")
		line = strings.TrimSuffix(line, "\r")

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		jsonData := line[6:]
		if jsonData == "[DONE]" {
			break
		}

		var rawChunk map[string]any
		if err := jsonpkg.UnmarshalString(jsonData, &rawChunk); err != nil {
			continue
		}
		rawChunks = append(rawChunks, rawChunk)

		var data StreamData
		if err := jsonpkg.UnmarshalString(jsonData, &data); err != nil {
			continue
		}

		if data.Response.UsageMetadata != nil {
			result.Usage = data.Response.UsageMetadata
			if respMap, ok := rawChunk["response"].(map[string]any); ok {
				if usage, ok := respMap["usageMetadata"]; ok {
					lastUsage = usage
				}
			}
		}

		if len(data.Response.Candidates) > 0 {
			candidate := data.Response.Candidates[0]
			if candidate.FinishReason != "" {
				result.FinishReason = candidate.FinishReason
				lastFinishReason = candidate.FinishReason
			}

			if respMap, ok := rawChunk["response"].(map[string]any); ok {
				if candidates, ok := respMap["candidates"].([]any); ok && len(candidates) > 0 {
					if cand, ok := candidates[0].(map[string]any); ok {
						if content, ok := cand["content"].(map[string]any); ok {
							if parts, ok := content["parts"].([]any); ok {
								mergedParts = append(mergedParts, parts...)
							}
						}
					}
				}
			}

			for _, part := range candidate.Content.Parts {
				if part.ThoughtSignature != "" {
					result.ThoughtSignature = part.ThoughtSignature
				}
				if part.Thought {
					thinkingBuilder.WriteString(part.Text)
				} else if part.Text != "" {
					textBuilder.WriteString(part.Text)
				} else if part.FunctionCall != nil {
					result.ToolCalls = append(result.ToolCalls, ToolCallInfo{
						ID:               part.FunctionCall.ID,
						Name:             part.FunctionCall.Name,
						Args:             part.FunctionCall.Args,
						ThoughtSignature: part.ThoughtSignature,
					})
				}
			}
		}

		if err := receiver(&data); err != nil {
			result.Text = textBuilder.String()
			result.Thinking = thinkingBuilder.String()
			return result, err
		}
	}

	result.Text = textBuilder.String()
	result.Thinking = thinkingBuilder.String()
	result.RawChunks = rawChunks

	result.MergedResponse = map[string]any{
		"response": map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"role":  "model",
						"parts": mergeParts(mergedParts),
					},
					"finishReason": lastFinishReason,
				},
			},
			"usageMetadata": lastUsage,
		},
	}

	return result, nil
}

func mergeParts(parts []any) []any {
	if len(parts) == 0 {
		return parts
	}

	var merged []any
	var textBuilder strings.Builder
	var thinkingBuilder strings.Builder
	var textExtraFields map[string]any
	var thinkingExtraFields map[string]any

	extractExtraFields := func(part map[string]any) map[string]any {
		extra := make(map[string]any)
		for k, v := range part {
			if k != "text" && k != "thought" {
				extra[k] = v
			}
		}
		if len(extra) == 0 {
			return nil
		}
		return extra
	}

	mergeExtraFields := func(existing, newFields map[string]any) map[string]any {
		if newFields == nil {
			return existing
		}
		if existing == nil {
			existing = make(map[string]any)
		}
		for k, v := range newFields {
			existing[k] = v
		}
		return existing
	}

	buildPart := func(text string, thought bool, extra map[string]any) map[string]any {
		result := map[string]any{"text": text}
		if thought {
			result["thought"] = true
		}
		for k, v := range extra {
			result[k] = v
		}
		return result
	}

	for _, p := range parts {
		part, ok := p.(map[string]any)
		if !ok {
			merged = append(merged, p)
			continue
		}

		thought, isThought := part["thought"].(bool)
		if text, hasText := part["text"].(string); hasText && text != "" {
			extra := extractExtraFields(part)
			if isThought && thought {
				if textBuilder.Len() > 0 {
					merged = append(merged, buildPart(textBuilder.String(), false, textExtraFields))
					textBuilder.Reset()
					textExtraFields = nil
				}
				thinkingBuilder.WriteString(text)
				thinkingExtraFields = mergeExtraFields(thinkingExtraFields, extra)
			} else {
				if thinkingBuilder.Len() > 0 {
					merged = append(merged, buildPart(thinkingBuilder.String(), true, thinkingExtraFields))
					thinkingBuilder.Reset()
					thinkingExtraFields = nil
				}
				textBuilder.WriteString(text)
				textExtraFields = mergeExtraFields(textExtraFields, extra)
			}
			continue
		}

		if textBuilder.Len() > 0 {
			merged = append(merged, buildPart(textBuilder.String(), false, textExtraFields))
			textBuilder.Reset()
			textExtraFields = nil
		}
		if thinkingBuilder.Len() > 0 {
			merged = append(merged, buildPart(thinkingBuilder.String(), true, thinkingExtraFields))
			thinkingBuilder.Reset()
			thinkingExtraFields = nil
		}
		merged = append(merged, part)
	}

	if thinkingBuilder.Len() > 0 {
		merged = append(merged, buildPart(thinkingBuilder.String(), true, thinkingExtraFields))
	}
	if textBuilder.Len() > 0 {
		merged = append(merged, buildPart(textBuilder.String(), false, textExtraFields))
	}

	return merged
}

// MergeParts merges consecutive text/thinking parts in Vertex raw parts slices.
// It is exported for endpoint handlers that need to build merged responses for logs.
func MergeParts(parts []any) []any {
	return mergeParts(parts)
}

func SetStreamHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
}

func WriteStreamData(w http.ResponseWriter, data any) error {
	jsonBytes, err := jsonpkg.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "data: %s\n\n", jsonBytes); err != nil {
		return err
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	return nil
}

func WriteStreamDone(w http.ResponseWriter) {
	_, _ = w.Write([]byte("data: [DONE]\n\n"))
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func WriteStreamError(w http.ResponseWriter, errMsg string) {
	errResp := map[string]any{
		"error": map[string]any{
			"message": errMsg,
			"type":    "server_error",
		},
	}
	_ = WriteStreamData(w, errResp)
	WriteStreamDone(w)
}
