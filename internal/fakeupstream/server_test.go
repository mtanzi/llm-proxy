package fakeupstream

import (
	"bytes"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) Fixture {
	t.Helper()
	f, err := Load(filepath.Join(FixturesDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func fetch(t *testing.T, s *Server) (*http.Response, []byte, error) {
	t.Helper()
	resp, err := http.Post(s.URL+"/v1/messages", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return resp, b, err
}

func TestReplaysFixtureBodyAndHeaders(t *testing.T) {
	f := load(t, "short_chat")
	s := New(t)
	s.Enqueue(Reply{Fixture: f, Headers: map[string]string{"Request-Id": "req_x"}})
	resp, body, err := fetch(t, s)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(body, f.Response) {
		t.Errorf("body differs from the fixture")
	}
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("Request-Id") != "req_x" {
		t.Errorf("status %d, headers %v", resp.StatusCode, resp.Header)
	}
	if got := s.Requests(); len(got) != 1 || got[0].Path != "/v1/messages" || string(got[0].Body) != "{}" {
		t.Errorf("requests = %+v", got)
	}
}

func TestStatusOverrideAndErrorFixture(t *testing.T) {
	f := load(t, "error_429")
	s := New(t)
	s.Enqueue(Reply{Fixture: f}, Reply{Fixture: f, Status: 503})
	for _, want := range []int{429, 503} {
		resp, body, _ := fetch(t, s)
		if resp.StatusCode != want || !bytes.Equal(body, f.Response) {
			t.Errorf("status %d, want %d with the fixture body", resp.StatusCode, want)
		}
	}
}

func TestSplitKeepsBytes(t *testing.T) {
	f := load(t, "parallel_tool_calls")
	s := New(t)
	s.Enqueue(Reply{Fixture: f, Split: 7})
	_, body, err := fetch(t, s)
	if err != nil || !bytes.Equal(body, f.Response) {
		t.Errorf("err %v, split body differs from the fixture", err)
	}
}

func TestPingsAreInserted(t *testing.T) {
	f := load(t, "short_chat")
	s := New(t)
	s.Enqueue(Reply{Fixture: f, Pings: true})
	_, body, _ := fetch(t, s)
	if got, want := strings.Count(string(body), "event: ping"), strings.Count(string(f.Response), "event: ping")+len(splitEvents(f.Response))-1; got != want {
		t.Errorf("ping events = %d, want %d", got, want)
	}
	if !strings.HasSuffix(string(body), string(splitEvents(f.Response)[len(splitEvents(f.Response))-1])) {
		t.Error("stream no longer ends with the fixture's last event")
	}
}

func TestAbortCutsTheBody(t *testing.T) {
	f := load(t, "short_chat")
	s := New(t)
	s.Enqueue(Reply{Fixture: f, AbortAfter: 2})
	_, body, err := fetch(t, s)
	if err == nil {
		t.Error("read finished cleanly, want an unexpected EOF")
	}
	if len(body) == 0 || len(body) >= len(f.Response) {
		t.Errorf("got %d of %d bytes, want a cut body", len(body), len(f.Response))
	}
}

func TestReplayPlanSplitsChunks(t *testing.T) {
	f := Fixture{Meta: Meta{Status: 200, Streamed: true}, Response: []byte("abcdefghij"),
		Replay: Replay{Chunks: []Chunk{{Bytes: 3}, {Bytes: 4, DelayMs: 1}}}}
	got := f.chunks()
	if len(got) != 3 || string(got[0]) != "abc" || string(got[1]) != "defg" || string(got[2]) != "hij" {
		t.Errorf("chunks = %q", got)
	}
}

func TestLoadFailsOnMissingFixture(t *testing.T) {
	if _, err := Load(filepath.Join(FixturesDir, "nope")); err == nil {
		t.Error("Load of a missing fixture succeeded")
	}
}
