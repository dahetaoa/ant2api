package openai

import (
	"strings"
	"time"

	"anti2api-golang/refactor/internal/pkg/id"
	"anti2api-golang/refactor/internal/pkg/modelutil"
	"anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

type ChatCompletion struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   *Usage   `json:"usage,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message,omitempty"`
	Delta        *Delta  `json:"delta,omitempty"`
	FinishReason *string `json:"finish_reason"`
}

type Delta struct {
	Role      string     `json:"role,omitempty"`
	Content   string     `json:"content,omitempty"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	Reasoning string     `json:"reasoning,omitempty"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ModelsResponse struct {
	Object string      `json:"object"`
	Data   []ModelItem `json:"data"`
}

type ModelItem struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	OwnedBy string `json:"owned_by"`
}

func ConvertUsage(metadata *vertex.UsageMetadata) *Usage {
	if metadata == nil {
		return nil
	}
	return &Usage{
		PromptTokens:     metadata.PromptTokenCount,
		CompletionTokens: metadata.CandidatesTokenCount,
		TotalTokens:      metadata.TotalTokenCount,
	}
}

func ToChatCompletion(resp *vertex.Response, model string, requestID string) *ChatCompletion {
	out := &ChatCompletion{
		ID:      id.ChatCompletionID(),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{{Index: 0, Message: Message{Role: "assistant"}, FinishReason: ptr("stop")}},
		Usage:   ConvertUsage(resp.Response.UsageMetadata),
	}

	if len(resp.Response.Candidates) == 0 {
		return out
	}
	parts := resp.Response.Candidates[0].Content.Parts

	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var toolCalls []ToolCall

	sigMgr := signature.GetManager()
	isClaudeThinking := modelutil.IsClaudeThinking(model)
	pendingSig := ""
	var pendingReasoning strings.Builder

	for _, p := range parts {
		if p.Thought {
			reasoningBuilder.WriteString(p.Text)
			pendingReasoning.WriteString(p.Text)
			if isClaudeThinking && p.ThoughtSignature != "" {
				// Claude thinking: bind this signature to the first subsequent tool call id.
				pendingSig = p.ThoughtSignature
			}
			continue
		}
		if p.Text != "" {
			contentBuilder.WriteString(p.Text)
			continue
		}
		if p.InlineData != nil {
			// 使用 string([]byte(...)) 创建独立副本，断开与原始图片数据的引用
			// 避免子字符串切片导致整个1MB+数据无法被GC回收
			imageKey := p.InlineData.Data
			if len(imageKey) > 50 {
				imageKey = string([]byte(imageKey[:50]))
			}
			if p.ThoughtSignature != "" {
				sigMgr.Save(requestID, imageKey, p.ThoughtSignature, pendingReasoning.String(), model)
				pendingReasoning.Reset()
			}
			// CRITICAL: Create independent copy of InlineData.Data before writing to contentBuilder.
			// strings.Builder may share the underlying byte array with the input string.
			// Without this copy, the entire Response object (containing multi-MB image data)
			// cannot be garbage collected because the output 'content' string holds a reference.
			imageData := string([]byte(p.InlineData.Data))
			contentBuilder.WriteString("![image](data:")
			contentBuilder.WriteString(p.InlineData.MimeType)
			contentBuilder.WriteString(";base64,")
			contentBuilder.WriteString(imageData)
			contentBuilder.WriteString(")")
			continue
		}
		if p.FunctionCall != nil {
			tcID := p.FunctionCall.ID
			if tcID == "" {
				tcID = id.ToolCallID()
			}

			if isClaudeThinking {
				if pendingSig != "" {
					sigMgr.Save(requestID, tcID, pendingSig, pendingReasoning.String(), model)
					pendingSig = ""
					pendingReasoning.Reset()
				} else if p.ThoughtSignature != "" {
					sigMgr.Save(requestID, tcID, p.ThoughtSignature, pendingReasoning.String(), model)
					pendingReasoning.Reset()
				}
			} else if p.ThoughtSignature != "" {
				sigMgr.Save(requestID, tcID, p.ThoughtSignature, pendingReasoning.String(), model)
				pendingReasoning.Reset()
			}

			args := "{}"
			if p.FunctionCall.Args != nil {
				if s, err := jsonString(p.FunctionCall.Args); err == nil {
					args = s
				}
			}

			toolCalls = append(toolCalls, ToolCall{
				ID:   tcID,
				Type: "function",
				Function: FunctionCall{
					Name:      p.FunctionCall.Name,
					Arguments: args,
				},
			})
		}
	}

	finish := "stop"
	if len(toolCalls) > 0 {
		finish = "tool_calls"
	}
	out.Choices[0].FinishReason = &finish
	out.Choices[0].Message.Content = contentBuilder.String()
	out.Choices[0].Message.Reasoning = reasoningBuilder.String()
	out.Choices[0].Message.ToolCalls = toolCalls

	return out
}

func ptr[T any](v T) *T { return &v }
