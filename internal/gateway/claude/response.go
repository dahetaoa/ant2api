package claude

import (
	"anti2api-golang/refactor/internal/pkg/id"
	sigpkg "anti2api-golang/refactor/internal/signature"
	"anti2api-golang/refactor/internal/vertex"
)

type MessagesResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type TokenCountResponse struct {
	InputTokens int `json:"input_tokens"`
	TokenCount  int `json:"token_count"`
	Tokens      int `json:"tokens"`
}

func ToMessagesResponse(resp *vertex.Response, requestID string, model string, inputTokens int) *MessagesResponse {
	out := &MessagesResponse{
		ID:         "msg_" + requestID,
		Type:       "message",
		Role:       "assistant",
		Model:      model,
		StopReason: "end_turn",
		Usage: Usage{
			InputTokens:  inputTokens,
			OutputTokens: 0,
		},
	}

	if len(resp.Response.Candidates) == 0 {
		return out
	}
	parts := resp.Response.Candidates[0].Content.Parts

	var text string
	var thinking string
	var thinkingSignature string
	var toolUses []ContentBlock
	var pendingSignature string

	sigMgr := sigpkg.GetManager()
	for _, p := range parts {
		if p.Thought {
			thinking += p.Text
			continue
		}
		if p.Text != "" {
			text += p.Text
			continue
		}
		if p.ThoughtSignature != "" {
			thinkingSignature = p.ThoughtSignature
			pendingSignature = p.ThoughtSignature
		}
		if p.FunctionCall != nil {
			idv := p.FunctionCall.ID
			if idv == "" {
				idv = "toolu_" + id.RequestID()
			}
			sig := p.ThoughtSignature
			if sig == "" {
				sig = pendingSignature
			}
			if sig != "" {
				sigMgr.Save(requestID, idv, sig, model)
				pendingSignature = ""
			}
			toolUses = append(toolUses, ContentBlock{Type: "tool_use", ID: idv, Name: p.FunctionCall.Name, Input: p.FunctionCall.Args})
			out.StopReason = "tool_use"
		}
	}

	blocks := make([]ContentBlock, 0, 2+len(toolUses))
	if thinking != "" {
		blocks = append(blocks, ContentBlock{Type: "thinking", Thinking: thinking, Signature: thinkingSignature})
	}
	if text != "" {
		blocks = append(blocks, ContentBlock{Type: "text", Text: text})
	}
	blocks = append(blocks, toolUses...)
	out.Content = blocks

	if out.Usage.InputTokens < 0 {
		out.Usage.InputTokens = 0
	}
	if resp.Response.UsageMetadata != nil {
		out.Usage.OutputTokens = resp.Response.UsageMetadata.CandidatesTokenCount
	}

	return out
}
