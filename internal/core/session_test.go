package core

import (
	"strings"
	"testing"
)

func TestSessionKeyDeterministic(t *testing.T) {
	ts, fu := DigestOf([]byte("tools+system")), DigestOf([]byte("first message"))
	a := SessionKey("marco-laptop", "claude-cli", ts, fu)
	for range 10 {
		if b := SessionKey("marco-laptop", "claude-cli", ts, fu); b != a {
			t.Fatalf("SessionKey = %q then %q for the same input", a, b)
		}
	}
	if !strings.HasPrefix(a, "derived-") || len(a) != len("derived-")+32 {
		t.Errorf("SessionKey = %q, want derived- plus 32 hex", a)
	}
}

func TestSessionKeyEveryInputMatters(t *testing.T) {
	ts, fu := DigestOf([]byte("tools+system")), DigestOf([]byte("first message"))
	base := SessionKey("k", "c", ts, fu)
	other := DigestOf([]byte("other"))
	for name, got := range map[string]string{
		"key":    SessionKey("k2", "c", ts, fu),
		"client": SessionKey("k", "c2", ts, fu),
		"tools":  SessionKey("k", "c", other, fu),
		"first":  SessionKey("k", "c", ts, other),
		"swap":   SessionKey("k", "c", fu, ts),
	} {
		if got == base {
			t.Errorf("changing %s did not change the key", name)
		}
	}
}

func TestSessionKeyFieldBoundaries(t *testing.T) {
	ts, fu := DigestOf(nil), DigestOf(nil)
	if SessionKey("ab", "c", ts, fu) == SessionKey("a", "bc", ts, fu) {
		t.Error("field boundary is ambiguous")
	}
}
