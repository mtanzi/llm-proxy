package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mtanzi/llm-proxy/internal/fakeupstream"
)

const fixtures = "../../testdata/fixtures"

func TestPlaceholderKeepsByteLength(t *testing.T) {
	for _, s := range []string{"", "a", "short", "exactly eleven", "héllo wörld, ünïcode ✓ and more text", strings.Repeat("é", 40)} {
		got := placeholder(s)
		if len(got) != len(s) {
			t.Errorf("placeholder(%q) has %d bytes, want %d", s, len(got), len(s))
		}
		if s != "" && strings.ContainsAny(got, "éöüï✓") {
			t.Errorf("placeholder(%q) = %q keeps content", s, got)
		}
	}
}

func TestScrubJSONKeepsStructureAndDropsText(t *testing.T) {
	in := `{"model":"claude-opus-4-8","system":"you are Marco's agent","messages":[{"role":"user","content":[{"type":"text","text":"my password is hunter2"}]}],"tools":[{"name":"Read","description":"reads private files","input_schema":{"type":"object"}}],"max_tokens":1024}`
	out, err := scrubJSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, leak := range []string{"Marco", "hunter2", "private files", "password"} {
		if strings.Contains(s, leak) {
			t.Errorf("scrubbed JSON still has %q:\n%s", leak, s)
		}
	}
	for _, keep := range []string{`"claude-opus-4-8"`, `"role": "user"`, `"name": "Read"`, `"max_tokens": 1024`, `"type": "text"`} {
		if !strings.Contains(s, keep) {
			t.Errorf("scrubbed JSON lost %s:\n%s", keep, s)
		}
	}
}

// Scrubbing the committed streamed fixtures keeps every event, every event name
// and the byte size of every text and argument fragment.
func TestScrubSSEKeepsEventsAndSizes(t *testing.T) {
	for _, name := range []string{"short_chat", "parallel_tool_calls", "long_thinking", "midstream_error"} {
		f, err := fakeupstream.Load(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		out, err := scrubSSE(f.Response)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		inLines, outLines := strings.Split(string(f.Response), "\n"), strings.Split(string(out), "\n")
		if len(inLines) != len(outLines) {
			t.Fatalf("%s: %d lines in, %d out", name, len(inLines), len(outLines))
		}
		for i := range inLines {
			if strings.HasPrefix(inLines[i], "data:") != strings.HasPrefix(outLines[i], "data:") ||
				(!strings.HasPrefix(inLines[i], "data:") && inLines[i] != outLines[i]) {
				t.Errorf("%s line %d changed shape: %q -> %q", name, i, inLines[i], outLines[i])
			}
		}
		if bytes.Contains(out, []byte("Zaphod")) {
			t.Errorf("%s: scrubbed stream still has response text", name)
		}
	}
}

func TestScrubRequestHeaders(t *testing.T) {
	raw := `{"Authorization":["[redacted]"],"X-Api-Key":["[redacted]"],"User-Agent":["claude-cli/2.1.290 (external, cli)"],
	"X-Claude-Code-Session-Id":["6f1c2d3e-8a4b-4c5d-9e0f-1a2b3c4d5e6f"],"X-Claude-Code-Agent-Id":["real-agent"],
	"X-Claude-Code-Agent-Type":["Explore"],"Cookie":["a=b"],"Traceparent":["00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"]}`
	a, err := scrubRequestHeaders([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := scrubRequestHeaders([]byte(raw))
	if !bytes.Equal(a, b) {
		t.Error("scrubbing is not deterministic")
	}
	s := string(a)
	for _, leak := range []string{"redacted", "Authorization", "Cookie", "real-agent", "6f1c2d3e", "4bf92f35"} {
		if strings.Contains(s, leak) {
			t.Errorf("scrubbed headers still have %q:\n%s", leak, s)
		}
	}
	for _, keep := range []string{"claude-cli/2.1.290", "Explore", "X-Claude-Code-Session-Id", "session-"} {
		if !strings.Contains(s, keep) {
			t.Errorf("scrubbed headers lost %q:\n%s", keep, s)
		}
	}
}

func TestCheckLeaksRefusesCredentialsAndPaths(t *testing.T) {
	for _, bad := range []string{"sk-ant-api03-abcdefghijkl", "Bearer abcdefghijklmnop", "lp_abcdefghijklmnopqrstuvwxyz", "someone@example.com", "/Users/marco/code/x.go", `"x-api-key": "abc"`} {
		if err := checkLeaks("f", []byte(bad)); err == nil {
			t.Errorf("checkLeaks accepted %q", bad)
		}
	}
	for _, f := range []string{"short_chat", "error_429", "tool_results_next_request"} {
		for _, name := range []string{"request.json", "response.sse", "response.json"} {
			b, err := os.ReadFile(filepath.Join(fixtures, f, name))
			if err != nil {
				continue
			}
			if err := checkLeaks(name, b); err != nil {
				t.Errorf("the committed fixture %s trips the leak check: %v", f, err)
			}
		}
	}
}

// The whole path: a client talks to the recorder, the recorder forwards to a
// fake upstream and writes a raw capture without credentials, and scrub turns
// the capture into a fixture the fake upstream can replay.
func TestRecordThenScrub(t *testing.T) {
	chat, err := fakeupstream.Load(filepath.Join(fixtures, "short_chat"))
	if err != nil {
		t.Fatal(err)
	}
	up := fakeupstream.New(t)
	up.Enqueue(fakeupstream.Reply{Fixture: chat, Headers: map[string]string{"Request-Id": "req_rec"}})
	target, _ := url.Parse(up.URL)

	raw := t.TempDir()
	rec := &recorder{out: raw, log: io.Discard}
	srv := httptest.NewServer(rec.handler(target))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL+"/v1/messages", bytes.NewReader([]byte(`{"model":"claude-opus-4-8","stream":true,"messages":[{"role":"user","content":"my secret plan"}]}`)))
	req.Header.Set("Authorization", "Bearer sk-ant-realcredential123")
	req.Header.Set("X-Claude-Code-Session-Id", "real-session")
	req.Header.Set("User-Agent", "claude-cli/2.1.290 (external, cli)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if !bytes.Equal(body, chat.Response) {
		t.Fatal("the recorder changed the response the client got")
	}
	if got := up.Requests()[0].Header.Get("Authorization"); got != "Bearer sk-ant-realcredential123" {
		t.Errorf("upstream saw Authorization %q, the recorder must pass credentials through", got)
	}

	dir := filepath.Join(raw, "0001")
	rawHeaders, err := os.ReadFile(filepath.Join(dir, "request.headers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rawHeaders), "realcredential") {
		t.Errorf("the raw capture holds a credential: %s", rawHeaders)
	}

	out := filepath.Join(t.TempDir(), "recorded")
	if err := scrubExchange(dir, out); err != nil {
		t.Fatal(err)
	}
	f, err := fakeupstream.Load(out)
	if err != nil {
		t.Fatalf("the scrubbed fixture does not load: %v", err)
	}
	if !f.Meta.Streamed || f.Meta.Status != 200 || f.Replay.Headers["Request-Id"] != "req_rec" {
		t.Errorf("fixture meta %+v replay %+v", f.Meta, f.Replay)
	}
	for name, b := range map[string][]byte{"request": f.Request, "response": f.Response} {
		for _, leak := range []string{"secret plan", "Zaphod"} {
			if bytes.Contains(b, []byte(leak)) {
				t.Errorf("scrubbed %s still has %q", name, leak)
			}
		}
	}
	h, _ := os.ReadFile(filepath.Join(out, "request.headers.json"))
	if strings.Contains(string(h), "real-session") || strings.Contains(string(h), "Authorization") {
		t.Errorf("scrubbed headers: %s", h)
	}
}

func TestScrubWritesNothingOnLeak(t *testing.T) {
	in := t.TempDir()
	write := func(name, data string) {
		if err := os.WriteFile(filepath.Join(in, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("meta.json", `{"endpoint":"/v1/messages","status":200,"streamed":false}`)
	write("request.json", `{"model":"m","messages":[]}`)
	// "id" is a kept key, so a credential-shaped id survives the walk and trips the check.
	write("response.json", `{"id":"sk-ant-api03-abcdefghijkl","type":"message"}`)
	out := filepath.Join(t.TempDir(), "fx")
	if err := scrubExchange(in, out); err == nil {
		t.Error("scrub accepted a response that still holds a credential")
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("scrub wrote output despite the leak")
	}
}

func TestRefuseRepoPath(t *testing.T) {
	if err := refuseRepoPath(filepath.Join(fixtures, "raw")); err == nil {
		t.Error("an output directory inside the checkout was accepted")
	}
	if err := refuseRepoPath(t.TempDir()); err != nil {
		t.Errorf("a temp directory was refused: %v", err)
	}
}
