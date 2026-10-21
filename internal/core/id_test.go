package core

import (
	"testing"
	"time"
)

func TestIDStringParseRoundTrip(t *testing.T) {
	for range 100 {
		id := NewID()
		got, err := ParseID(id.String())
		if err != nil || got != id {
			t.Fatalf("round trip of %s = %v, %v", id, got, err)
		}
	}
}

func TestIDLayout(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_123)
	id := NewIDAt(at)
	if id[6]>>4 != 7 {
		t.Errorf("version nibble = %x, want 7", id[6]>>4)
	}
	if id[8]>>6 != 0b10 {
		t.Errorf("variant bits = %b, want 10", id[8]>>6)
	}
	if !id.Time().Equal(at) {
		t.Errorf("Time = %v, want %v", id.Time(), at)
	}
	if s := id.String(); len(s) != 36 || s[14] != '7' {
		t.Errorf("String = %q, want canonical v7 form", s)
	}
}

func TestIDKnownVector(t *testing.T) {
	const s = "0197a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2b"
	id, err := ParseID(s)
	if err != nil {
		t.Fatal(err)
	}
	if id.String() != s {
		t.Errorf("String = %s, want %s", id, s)
	}
	if up, err := ParseID("0197A1B2-C3D4-7E5F-8A6B-7C8D9E0F1A2B"); err != nil || up != id {
		t.Errorf("uppercase parse = %v, %v", up, err)
	}
}

func TestIDOrderingFollowsTime(t *testing.T) {
	base := time.UnixMilli(1_760_000_000_000)
	var prev ID
	for i := range 50 {
		id := NewIDAt(base.Add(time.Duration(i) * time.Millisecond))
		if i > 0 && prev.Compare(id) >= 0 {
			t.Fatalf("id %d (%s) does not sort after %s", i, id, prev)
		}
		prev = id
	}
}

func TestIDUniqueWithinMillisecond(t *testing.T) {
	at := time.UnixMilli(1_760_000_000_000)
	seen := map[ID]bool{}
	for range 1000 {
		id := NewIDAt(at)
		if seen[id] {
			t.Fatalf("duplicate id %s", id)
		}
		seen[id] = true
	}
}

func TestParseIDRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"0197a1b2c3d47e5f8a6b7c8d9e0f1a2b",
		"0197a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2",
		"0197a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2bb",
		"0197a1b2_c3d4-7e5f-8a6b-7c8d9e0f1a2b",
		"0197a1b2-c3d4-7e5f-8a6b-7c8d9e0f1a2g",
	} {
		if _, err := ParseID(s); err != ErrInvalidID {
			t.Errorf("ParseID(%q) err = %v, want ErrInvalidID", s, err)
		}
	}
}

func TestIDText(t *testing.T) {
	id := NewID()
	b, err := id.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	var got ID
	if err := got.UnmarshalText(b); err != nil || got != id {
		t.Fatalf("text round trip = %v, %v", got, err)
	}
	if err := got.UnmarshalText([]byte("nope")); err == nil {
		t.Fatal("UnmarshalText accepted bad input")
	}
	if !(ID{}).IsZero() || id.IsZero() {
		t.Error("IsZero wrong")
	}
}
