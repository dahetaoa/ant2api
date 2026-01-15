package credential

import (
	"testing"
	"time"
)

func TestStoreGetToken_RoundRobinSequential(t *testing.T) {
	now := time.Now().UnixMilli()
	s := &Store{
		accounts: []Account{
			{AccessToken: "t1", ExpiresIn: 3600, Timestamp: now, Enable: true},
			{AccessToken: "t2", ExpiresIn: 3600, Timestamp: now, Enable: true},
			{AccessToken: "t3", ExpiresIn: 3600, Timestamp: now, Enable: true},
		},
	}

	got := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		acc, err := s.GetToken()
		if err != nil {
			t.Fatalf("GetToken error: %v", err)
		}
		got = append(got, acc.AccessToken)
	}

	want := []string{"t1", "t2", "t3", "t1", "t2"}
	if len(got) != len(want) {
		t.Fatalf("unexpected result length: got %d want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("round robin mismatch at %d: got %q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

func TestStoreGetToken_SkipsDisabled(t *testing.T) {
	now := time.Now().UnixMilli()
	s := &Store{
		accounts: []Account{
			{AccessToken: "t1", ExpiresIn: 3600, Timestamp: now, Enable: true},
			{AccessToken: "t2", ExpiresIn: 3600, Timestamp: now, Enable: false},
			{AccessToken: "t3", ExpiresIn: 3600, Timestamp: now, Enable: true},
		},
	}

	got := make([]string, 0, 4)
	for i := 0; i < 4; i++ {
		acc, err := s.GetToken()
		if err != nil {
			t.Fatalf("GetToken error: %v", err)
		}
		got = append(got, acc.AccessToken)
	}

	want := []string{"t1", "t3", "t1", "t3"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("skip disabled mismatch at %d: got %q want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

