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

// EntryIndex is a lightweight pointer to an Entry stored on disk.
// It intentionally excludes large fields (Signature/Reasoning) to keep memory usage low.
type EntryIndex struct {
	RequestID  string    `json:"requestID"`
	ToolCallID string    `json:"toolCallID"`
	Model      string    `json:"model,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	LastAccess time.Time `json:"lastAccess,omitempty"`
	// SignaturePrefix stores a short prefix of Signature to help safely recover the full value from disk
	// without keeping it in memory. It is also used to validate matches when toolCallID is not globally unique.
	SignaturePrefix string `json:"signaturePrefix,omitempty"`

	// Date points to the storage shard (YYYY-MM-DD). For hot (not-yet-persisted) entries, Date is empty.
	Date string `json:"date,omitempty"`
}

func (i EntryIndex) Key() string {
	if i.RequestID == "" || i.ToolCallID == "" {
		return ""
	}
	return i.RequestID + ":" + i.ToolCallID
}
