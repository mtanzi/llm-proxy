package proxy

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mtanzi/llm-proxy/internal/core"
)

func loadFixture(t *testing.T, name string) http.Header {
	t.Helper()
	raw, err := os.ReadFile("../../testdata/fixtures/claude-code-headers.json")
	if err != nil {
		t.Fatal(err)
	}
	var sets map[string]json.RawMessage
	if err := json.Unmarshal(raw, &sets); err != nil {
		t.Fatal(err)
	}
	var kv map[string]string
	if err := json.Unmarshal(sets[name], &kv); err != nil {
		t.Fatalf("fixture %q: %v", name, err)
	}
	h := http.Header{}
	for k, v := range kv {
		h.Set(k, v)
	}
	return h
}

func TestCorrelateFixtureWithoutHints(t *testing.T) {
	var rec core.CallRecord
	Correlate(loadFixture(t, "without_hint_headers"), &rec)
	want := core.CallRecord{
		Client: "claude-cli", ClientRaw: "claude-cli/2.1.290 (external, cli)",
		SessionKey: "6f1c2d3e-8a4b-4c5d-9e0f-1a2b3c4d5e6f", AgentID: "agent-0001",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Errorf("record = %+v, want %+v", rec, want)
	}
}

func TestCorrelateFixtureWithHints(t *testing.T) {
	var rec core.CallRecord
	Correlate(loadFixture(t, "with_hint_headers"), &rec)
	want := core.CallRecord{
		Client: "claude-cli", ClientRaw: "claude-cli/2.1.290 (external, cli)",
		SessionKey: "6f1c2d3e-8a4b-4c5d-9e0f-1a2b3c4d5e6f", AgentID: "agent-0002", ParentAgentID: "agent-0001",
		AgentType: "Explore", PromptID: "prompt-0042", RequestClass: "subagent",
		Compaction: false, ContextCompacted: true,
		PrevToolDurations: `{"Bash":1200,"Read":35}`,
		TraceID:           "4bf92f3577b34da6a3ce929d0e0e4736",
	}
	if !reflect.DeepEqual(rec, want) {
		t.Errorf("record = %+v, want %+v", rec, want)
	}
}

func TestCorrelateNoSessionHeaderGivesNullSessionKey(t *testing.T) {
	h := loadFixture(t, "with_hint_headers")
	h.Del("X-Claude-Code-Session-Id")
	var rec core.CallRecord
	Correlate(h, &rec)
	if rec.SessionKey != "" {
		t.Errorf("SessionKey = %q, want empty (null)", rec.SessionKey)
	}
	if rec.AgentID == "" {
		t.Error("other correlation fields should still be read")
	}
}

func TestCorrelateMalformedValuesAreNull(t *testing.T) {
	h := http.Header{}
	h.Set("X-Claude-Code-Request-Class", "bogus")
	h.Set("X-Claude-Code-Compaction", "maybe")
	h.Set("X-Claude-Code-Context-Compacted", "1")
	h.Set("X-Claude-Code-Session-Id", strings.Repeat("a", maxCorrelationValue+1))
	h.Set("X-Claude-Code-Agent-Id", "   ")
	h.Set("Traceparent", "00-00000000000000000000000000000000-00f067aa0ba902b7-01")
	var rec core.CallRecord
	Correlate(h, &rec)
	if !reflect.DeepEqual(rec, core.CallRecord{}) {
		t.Errorf("record = %+v, want zero", rec)
	}
}

func TestCorrelateRequestClasses(t *testing.T) {
	for _, c := range []string{"main", "subagent", "workflow", "compaction", "auxiliary"} {
		h := http.Header{}
		h.Set("X-Claude-Code-Request-Class", c)
		var rec core.CallRecord
		Correlate(h, &rec)
		if rec.RequestClass != c {
			t.Errorf("RequestClass = %q, want %q", rec.RequestClass, c)
		}
	}
}

func TestIsTraceparent(t *testing.T) {
	tests := map[string]bool{
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01": true,
		"":                                    false,
		"00-4bf92f3577b34da6a3ce929d0e0e4736": false,
		"01-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01":  false,
		"00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01":  false,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-0000000000000000-01":  false,
		"00-4bf92f3577b34da6a3ce929d0e0e473z-00f067aa0ba902b7-01":  false,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-0g":  false,
		"00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01x": false,
	}
	for in, want := range tests {
		if got := isTraceparent(in); got != want {
			t.Errorf("isTraceparent(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestClientName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"claude-cli/2.1.290", "claude-cli"},
		{"claude-cli/2.1.290 (external, cli)", "claude-cli"},
		{"claude-cli", "claude-cli"},
		{"  claude-cli/2.1.290  ", "claude-cli"},
		{"curl/8.4.0", "curl"},
		{"Mozilla/5.0 (X11; Linux x86_64)", "Mozilla"},
		{"python-requests", "python-requests"},
		{"my tool 1.0", "my"},
		{"anthropic-sdk/", "anthropic-sdk"},
		{"", ""},
		{"   ", ""},
		{"/1.0", ""},
		{"/", ""},
		{" /1.0", ""},
		{"\t", ""},
		{strings.Repeat("x", maxCorrelationValue+1), ""},
	}
	for _, tt := range tests {
		if got := ClientName(tt.in); got != tt.want {
			t.Errorf("ClientName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCorrelateKeepsRawUserAgent(t *testing.T) {
	h := http.Header{}
	h.Set("User-Agent", "claude-cli/2.1.290 (external, cli)")
	var rec core.CallRecord
	Correlate(h, &rec)
	if rec.ClientRaw != "claude-cli/2.1.290 (external, cli)" {
		t.Errorf("ClientRaw = %q", rec.ClientRaw)
	}
	Correlate(http.Header{}, &rec)
	if rec.Client != "" || rec.ClientRaw != "" {
		t.Errorf("empty User-Agent: Client %q ClientRaw %q, want empty", rec.Client, rec.ClientRaw)
	}
}

// credentialHeaders hold secrets. Correlation must neither read them into the
// record nor log them, and must leave them in place for the forwarder.
var credentialHeaders = []string{"Authorization", "X-Api-Key", "X-Llm-Proxy-Key", "Proxy-Authorization", "Cookie"}

func TestCorrelateNeverReadsOrLogsCredentials(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "sk-ant-SECRET-lp_SECRET"
	h := loadFixture(t, "with_hint_headers")
	for _, name := range credentialHeaders {
		h.Set(name, secret)
	}
	before := h.Clone()

	var rec core.CallRecord
	Correlate(h, &rec)

	if b, _ := json.Marshal(rec); bytes.Contains(b, []byte(secret)) {
		t.Errorf("record holds a credential: %s", b)
	}
	if strings.Contains(logs.String(), "SECRET") {
		t.Errorf("correlation logged a credential: %s", logs.String())
	}
	for _, name := range credentialHeaders {
		if h.Get(name) != secret {
			t.Errorf("header %s changed", name)
		}
	}
	for k := range before {
		if strings.Join(h[k], ",") != strings.Join(before[k], ",") {
			t.Errorf("header %s changed: forwarded headers must stay unchanged", k)
		}
	}
}

func TestCorrelationTableReadsNoCredentialHeader(t *testing.T) {
	for _, f := range correlationFields {
		for _, c := range credentialHeaders {
			if strings.EqualFold(f.header, c) {
				t.Errorf("table reads credential header %s", c)
			}
		}
	}
}
