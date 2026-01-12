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

	// FilePath is the JSONL file containing the entry.
	// Offset is the byte offset (from beginning of file) where the JSON object starts.
	// For hot (not-yet-flushed) entries, FilePath may be empty and Offset < 0.
	FilePath string `json:"filePath,omitempty"`
	Offset   int64  `json:"offset,omitempty"`
}

func (i EntryIndex) Key() string {
	if i.RequestID == "" || i.ToolCallID == "" {
		return ""
	}
	return i.RequestID + ":" + i.ToolCallID
}
