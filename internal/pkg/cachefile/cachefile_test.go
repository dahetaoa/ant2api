package cachefile

import (
	"bytes"
	"os"
	"testing"
)

func TestWriterReader_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	date := "2026-01-24"

	w, err := NewCacheWriter(dir, date)
	if err != nil {
		t.Fatalf("NewCacheWriter: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })

	data1 := []byte("hello")
	if err := w.Write("r:call_1", data1); err != nil {
		t.Fatalf("Write #1: %v", err)
	}
	data2 := bytes.Repeat([]byte{0x42}, 128*1024)
	if err := w.Write("r:call_2", data2); err != nil {
		t.Fatalf("Write #2: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	r, err := NewCacheReader(dir, date)
	if err != nil {
		t.Fatalf("NewCacheReader: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })

	if !r.Exists("r:call_1") || !r.Exists("r:call_2") {
		t.Fatalf("expected records to exist")
	}

	got1, err := r.Load("r:call_1")
	if err != nil {
		t.Fatalf("Load #1: %v", err)
	}
	if !bytes.Equal(got1, data1) {
		t.Fatalf("Load #1 mismatch")
	}

	got2, err := r.Load("r:call_2")
	if err != nil {
		t.Fatalf("Load #2: %v", err)
	}
	if !bytes.Equal(got2, data2) {
		t.Fatalf("Load #2 mismatch")
	}
}

func TestReader_DetectsLengthPrefixMismatch(t *testing.T) {
	dir := t.TempDir()
	date := "2026-01-24"

	w, err := NewCacheWriter(dir, date)
	if err != nil {
		t.Fatalf("NewCacheWriter: %v", err)
	}
	if err := w.Write("r:call_1", []byte("hello")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close writer: %v", err)
	}

	datPath, _ := paths(dir, date)
	f, err := os.OpenFile(datPath, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open data file: %v", err)
	}
	_, _ = f.WriteAt([]byte{0, 0, 0, 0}, 0)
	_ = f.Close()

	r, err := NewCacheReader(dir, date)
	if err != nil {
		t.Fatalf("NewCacheReader: %v", err)
	}
	defer r.Close()

	if _, err := r.Load("r:call_1"); err == nil {
		t.Fatalf("expected error")
	}
}
