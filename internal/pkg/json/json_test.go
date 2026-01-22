package json

import (
	"strings"
	"testing"
	"unsafe"
)

func TestUnmarshalStringCopiesStrings(t *testing.T) {
	type payload struct {
		S string `json:"s"`
	}

	want := strings.Repeat("a", 1<<20)
	src := `{"s":"` + want + `"}`

	var out payload
	if err := UnmarshalString(src, &out); err != nil {
		t.Fatalf("UnmarshalString error: %v", err)
	}
	if out.S != want {
		t.Fatalf("decoded mismatch: got len=%d want len=%d", len(out.S), len(want))
	}

	inStart := uintptr(unsafe.Pointer(unsafe.StringData(src)))
	inEnd := inStart + uintptr(len(src))
	outStart := uintptr(unsafe.Pointer(unsafe.StringData(out.S)))
	if outStart >= inStart && outStart < inEnd {
		t.Fatalf("decoded string references input buffer; CopyString expected")
	}
}
