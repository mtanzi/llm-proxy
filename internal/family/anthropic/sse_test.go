package anthropic

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

// got is an Event with its data copied out of the splitter's buffer.
type got struct {
	Name     string
	Data     string
	Oversize bool
}

// split feeds the chunks to a splitter and returns its events.
func split(max int, chunks ...string) []got {
	var out []got
	s := NewSSESplitter(func(e Event) {
		out = append(out, got{e.Name, string(e.Data), e.Oversize})
	})
	if max > 0 {
		s.max = max
	}
	for _, c := range chunks {
		s.Write([]byte(c))
	}
	return out
}

func TestSplitterEvents(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []got
	}{
		{"one event", "event: ping\ndata: {}\n\n", []got{{"ping", "{}", false}}},
		{"two events", "event: a\ndata: 1\n\nevent: b\ndata: 2\n\n", []got{{"a", "1", false}, {"b", "2", false}}},
		{"data lines join", "data: a\ndata: b\n\n", []got{{"", "a\nb", false}}},
		{"no space after colon", "data:x\n\n", []got{{"", "x", false}}},
		{"only one space stripped", "data:  x\n\n", []got{{"", " x", false}}},
		{"comments and unknown fields", ": hi\nid: 7\nretry: 1\ndata: x\n\n", []got{{"", "x", false}}},
		{"crlf", "event: a\r\ndata: 1\r\n\r\ndata: 2\r\n\r\n", []got{{"a", "1", false}, {"", "2", false}}},
		{"bare cr", "data: 1\r\rdata: 2\r\r", []got{{"", "1", false}, {"", "2", false}}},
		{"extra blank lines", "\n\n\ndata: 1\n\n\n\n", []got{{"", "1", false}}},
		{"name without data", "event: ping\n\n", []got{{"ping", "", false}}},
		{"unterminated event is not emitted", "data: 1\n\ndata: 2\n", []got{{"", "1", false}}},
		{"unterminated line is not emitted", "data: 1\n\ndata: 2", []got{{"", "1", false}}},
		{"empty data line", "data:\n\n", []got{{"", "", false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if g := split(0, tt.input); !reflect.DeepEqual(g, tt.want) {
				t.Errorf("got %q, want %q", g, tt.want)
			}
		})
	}
}

// TestSplitterEveryByteOffset cuts each input at every offset, and into single
// bytes, and wants the events of the uncut input.
func TestSplitterEveryByteOffset(t *testing.T) {
	inputs := []string{
		"event: message_start\ndata: {\"a\":1}\n\nevent: ping\ndata: {}\n\n",
		"event: a\r\ndata: x\r\n\r\ndata: y\r\rdata: z\n\n",
		"data: one\ndata: two\n\n: comment\n\nevent: e\n\n",
		"data: ✓ wörld\n\n",
	}
	for _, in := range inputs {
		want := split(0, in)
		for i := 0; i <= len(in); i++ {
			if g := split(0, in[:i], in[i:]); !reflect.DeepEqual(g, want) {
				t.Errorf("cut at %d of %q: got %q, want %q", i, in, g, want)
			}
		}
		bytewise := make([]string, len(in))
		for i := 0; i < len(in); i++ {
			bytewise[i] = in[i : i+1]
		}
		if g := split(0, bytewise...); !reflect.DeepEqual(g, want) {
			t.Errorf("one byte at a time %q: got %q, want %q", in, g, want)
		}
	}
}

func TestSplitterOversizeEvent(t *testing.T) {
	big := "data: " + strings.Repeat("x", 100) + "\n\n"
	t.Run("skipped and resynchronised", func(t *testing.T) {
		g := split(32, "data: small\n\n"+big+"data: after\n\n")
		want := []got{{"", "small", false}, {"", "", true}, {"", "after", false}}
		if !reflect.DeepEqual(g, want) {
			t.Errorf("got %q, want %q", g, want)
		}
	})
	t.Run("many small lines add up", func(t *testing.T) {
		g := split(32, strings.Repeat("data: yyyy\n", 10)+"\ndata: after\n\n")
		want := []got{{"", "", true}, {"", "after", false}}
		if !reflect.DeepEqual(g, want) {
			t.Errorf("got %q, want %q", g, want)
		}
	})
	t.Run("one endless line", func(t *testing.T) {
		g := split(32, "data: ", strings.Repeat("z", 1000), strings.Repeat("z", 1000), "\n\ndata: after\n\n")
		want := []got{{"", "", true}, {"", "after", false}}
		if !reflect.DeepEqual(g, want) {
			t.Errorf("got %q, want %q", g, want)
		}
	})
	t.Run("an event at the bound is kept", func(t *testing.T) {
		in := "data: " + strings.Repeat("x", 24) + "\n" // 6+24+1 = 31 bytes
		if g := split(32, in+"\n"); len(g) != 1 || g[0].Oversize {
			t.Errorf("got %q", g)
		}
	})
	t.Run("the real bound is 1 MiB", func(t *testing.T) {
		s := NewSSESplitter(func(Event) {})
		if s.max != 1<<20 {
			t.Errorf("max = %d", s.max)
		}
	})
}

func TestSplitterBoundsMemory(t *testing.T) {
	s := NewSSESplitter(func(Event) {})
	s.max = 1 << 10
	chunk := []byte(strings.Repeat("d", 512))
	s.Write([]byte("data: "))
	for range 10000 {
		s.Write(chunk)
	}
	if cap(s.line) > 4<<10 || cap(s.data) > 4<<10 {
		t.Errorf("buffers grew past the bound: line %d, data %d", cap(s.line), cap(s.data))
	}
}

// randomCuts splits in at random points.
func randomCuts(rng *rand.Rand, in string) []string {
	var out []string
	for len(in) > 0 {
		n := 1 + rng.Intn(len(in))
		out = append(out, in[:n])
		in = in[n:]
	}
	return out
}

func FuzzSplitterRandomChunking(f *testing.F) {
	f.Add("event: ping\ndata: {}\n\n", int64(1))
	f.Add("data: a\r\ndata: b\r\n\r\n: c\n\nevent: x\n\n", int64(2))
	f.Add("data: 1\r\rdata: 2\n\n\n", int64(3))
	f.Add("data: ✓ wörld\n\n", int64(5))
	f.Add(strings.Repeat("data: yyyy\n", 40)+"\ndata: after\n\n", int64(4))
	f.Fuzz(func(t *testing.T, in string, seed int64) {
		const bound = 64
		run := func(chunks []string) []got {
			var out []got
			s := NewSSESplitter(func(e Event) { out = append(out, got{e.Name, string(e.Data), e.Oversize}) })
			s.max = bound
			for _, c := range chunks {
				s.Write([]byte(c))
			}
			if cap(s.line) > 2*bound+64 || cap(s.data) > 2*bound+64 {
				t.Fatalf("buffers past the bound: line %d, data %d", cap(s.line), cap(s.data))
			}
			return out
		}
		want := run([]string{in})
		got := run(randomCuts(rand.New(rand.NewSource(seed)), in))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("chunking changed the events\n got: %q\nwant: %q", got, want)
		}
	})
}

func (g got) String() string {
	if g.Oversize {
		return "{oversize}"
	}
	return "{" + g.Name + "|" + g.Data + "}"
}
