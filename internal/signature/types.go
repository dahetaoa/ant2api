package signature

import "time"

type Entry struct {
	Signature  string    `json:"signature"`
	Reasoning  string    `json:"reasoning,omitempty"`
	RequestID  string    `json:"requestID"`
	ToolCallID string    `json:"toolCallID"`
	Model      string    `json:"model"`
	CreatedAt  time.Time `json:"createdAt"`
	LastAccess time.Time `json:"lastAccess"`
}

func (e Entry) Key() string {
	if e.RequestID == "" || e.ToolCallID == "" {
		return ""
	}
	return e.RequestID + ":" + e.ToolCallID
}
