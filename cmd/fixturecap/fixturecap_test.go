package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/fakeupstream"
	"github.com/mtanzi/llm-proxy/internal/family/anthropic"
)

const fixtures = "../../testdata/fixtures"

func TestPlaceholderKeepsEncodedAndDecodedLength(t *testing.T) {
	for _, s := range []string{"", "a", "short", "exactly eleven", "héllo wörld, ünïcode ✓ and more text", strings.Repeat("é", 40),
		`line one\nline \"two\"\t\\ end`, `caf\u00fc \u4e2d \ud83d\ude00 \ud800 lone`} {
		lit := []byte(`"` + s + `"`)
		var want string
		if err := json.Unmarshal(lit, &want); err != nil {
			t.Fatalf("%q: %v", s, err)
		}
		got := placeholder([]byte(s))
		var dec string
		if err := json.Unmarshal([]byte(`"`+string(got)+`"`), &dec); err != nil {
			t.Fatalf("placeholder(%q) = %q is not a JSON string: %v", s, got, err)
		}
		if len(got) != len(s) || len(dec) != len(want) {
			t.Errorf("placeholder(%q) = %q: %d encoded and %d decoded bytes, want %d and %d", s, got, len(got), len(dec), len(s), len(want))
		}
		if s != "" && (strings.ContainsAny(dec, "öüï✓中") || strings.Contains(dec, "line") || strings.Contains(dec, "caf")) {
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
	for _, keep := range []string{`"claude-opus-4-8"`, `"role":"user"`, `"name":"Read"`, `"max_tokens":1024`, `"type":"text"`} {
		if !strings.Contains(s, keep) {
			t.Errorf("scrubbed JSON lost %s:\n%s", keep, s)
		}
	}
	if len(out) != len(in) {
		t.Errorf("scrubbed JSON has %d bytes, want %d", len(out), len(in))
	}
}

// The facts the proxy records from a fixture (tool argument and result sizes,
// tool names and ids) are the same before and after scrubbing it.
func TestScrubKeepsRecordedFacts(t *testing.T) {
	for _, name := range []string{"short_chat", "parallel_tool_calls", "long_thinking", "midstream_error", "server_tool_error", "tool_results_next_request", "abort"} {
		f, err := fakeupstream.Load(filepath.Join(fixtures, name))
		if err != nil {
			t.Fatal(err)
		}
		req, err := scrubJSON(f.Request)
		if err != nil {
			t.Fatalf("%s request: %v", name, err)
		}
		resp, err := scrubJSON(f.Response)
		if f.Meta.Streamed {
			resp, err = scrubSSE(f.Response)
		}
		if err != nil {
			t.Fatalf("%s response: %v", name, err)
		}
		before, after := recordedFacts(f.Request, f.Response, f.Meta.Streamed), recordedFacts(req, resp, f.Meta.Streamed)
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: facts changed by the scrub:\nbefore %+v\nafter  %+v", name, before, after)
		}
	}
}

func recordedFacts(req, resp []byte, streamed bool) core.CallRecord {
	rec := core.CallRecord{Endpoint: "/v1/messages"}
	anthropic.ExtractRequest(&rec, req)
	sc := anthropic.NewScanner(streamed)
	sc.Write(resp)
	facts := sc.Facts()
	rec.ToolUses, rec.ToolResults = facts.ToolUses, append(rec.ToolResults, facts.ToolResults...)
	rec.StopReason, rec.ErrorType = facts.StopReason, facts.ErrorType
	return rec
}

// Streamed tool arguments arrive as partial_json fragments. Scrubbed, the
// fragments keep their lengths and still join into valid JSON.
func TestScrubSSEKeepsToolArgumentsJSON(t *testing.T) {
	f, err := fakeupstream.Load(filepath.Join(fixtures, "parallel_tool_calls"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := scrubSSE(f.Response)
	if err != nil {
		t.Fatal(err)
	}
	fragments := func(sse []byte) map[int64][]string {
		m := map[int64][]string{}
		for _, line := range strings.Split(string(sse), "\n") {
			payload, ok := strings.CutPrefix(line, "data:")
			if !ok {
				continue
			}
			var ev struct {
				Index int64 `json:"index"`
				Delta struct {
					Type        string `json:"type"`
					PartialJSON string `json:"partial_json"`
				} `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err != nil {
				t.Fatal(err)
			}
			if ev.Delta.Type == "input_json_delta" {
				m[ev.Index] = append(m[ev.Index], ev.Delta.PartialJSON)
			}
		}
		return m
	}
	in, scrubbed := fragments(f.Response), fragments(out)
	if len(in) == 0 {
		t.Fatal("the fixture has no tool argument fragments")
	}
	for i, fs := range in {
		got := scrubbed[i]
		if len(got) != len(fs) {
			t.Fatalf("block %d: %d fragments, want %d", i, len(got), len(fs))
		}
		for j := range fs {
			if len(got[j]) != len(fs[j]) {
				t.Errorf("block %d fragment %d: %d bytes, want %d", i, j, len(got[j]), len(fs[j]))
			}
		}
		if joined := strings.Join(got, ""); !json.Valid([]byte(joined)) {
			t.Errorf("block %d: scrubbed arguments are not JSON: %s", i, joined)
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
