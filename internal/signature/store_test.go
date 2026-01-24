package signature

import (
	"testing"
)

func TestStoreLoadByIndex_ValidatesSignaturePrefix(t *testing.T) {
	cache := NewLRU(10)
	s := NewStore(t.TempDir(), cache)
	s.PutHot(Entry{Signature: "abcdef", RequestID: "r", ToolCallID: "t"})

	if _, ok := s.LoadByIndex(EntryIndex{RequestID: "r", ToolCallID: "t", SignaturePrefix: "abc"}); !ok {
		t.Fatalf("expected ok=true for matching prefix")
	}
	if _, ok := s.LoadByIndex(EntryIndex{RequestID: "r", ToolCallID: "t", SignaturePrefix: "zzz"}); ok {
		t.Fatalf("expected ok=false for mismatching prefix")
	}
}
