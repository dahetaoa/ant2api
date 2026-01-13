package common

import (
	"net/http"

	"anti2api-golang/refactor/internal/vertex"
)

func StatusFromVertexError(err error) int {
	if apiErr, ok := err.(*vertex.APIError); ok {
		return apiErr.Status
	}
	return http.StatusInternalServerError
}

func FindFunctionName(contents []vertex.Content, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	for i := len(contents) - 1; i >= 0; i-- {
		for _, p := range contents[i].Parts {
			if p.FunctionCall == nil {
				continue
			}
			if p.FunctionCall.ID == toolCallID {
				return p.FunctionCall.Name
			}
		}
	}
	return ""
}
