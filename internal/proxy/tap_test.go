package proxy

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/family/anthropic"
)

// fixture reads a recorded exchange of the anthropic family.
func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "family", "anthropic", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fakeExtractor keeps what the tap hands over at Close.
type fakeExtractor struct {
	mu      sync.Mutex
	calls   int
	facts   anthropic.Facts
	request string
	status  int // the status the record had when Extract ran
}

func (f *fakeExtractor) Extract(rec *core.CallRecord, request []byte, facts anthropic.Facts) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.facts = facts
	f.request = string(request)
	f.status = rec.Status
}

func (f *fakeExtractor) get() (int, anthropic.Facts, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.facts, f.request
}

func withExtractor(x Extractor) func(*env) { return func(e *env) { e.extractor = x } }
func withIdle(d string) func(*env)         { return func(e *env) { e.idle = d } }

// sseUpstream writes body in chunks of size bytes, flushing each.
func sseUpstream(body string, size int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for len(body) > 0 {
			n := min(size, len(body))
			_, _ = io.WriteString(w, body[:n])
			w.(http.Flusher).Flush()
			body = body[n:]
		}
	}
}

func TestTapScansStreamAndForwardsUnchanged(t *testing.T) {
	stream := fixture(t, "sse_tool_use.sse")
	ex := &fakeExtractor{}
	e := newEnv(t, sseUpstream(stream, 13), nil, withExtractor(ex))

	resp := e.do(t, "POST", "/v1/messages", `{"stream":true}`, auth()...)
	if got := readAll(t, resp.Body); got != stream {
		t.Errorf("the client got different bytes than the upstream sent")
	}
	rec := e.rec.next(t)

	n, facts, request := ex.get()
	if n != 1 || request != `{"stream":true}` {
		t.Errorf("extractor ran %d times with request %q", n, request)
	}
	if facts.StopReason != "tool_use" || !facts.Complete || facts.Partial || len(facts.ToolUses) != 3 {
		t.Errorf("facts = %+v", facts)
	}
	if rec.ExtractionPartial || rec.ErrorClass != "" {
		t.Errorf("record partial %v, error class %q", rec.ExtractionPartial, rec.ErrorClass)
	}
	if rec.TTFT <= 0 || rec.TTFT > rec.Duration || rec.TTFB <= 0 || rec.TTFB > rec.TTFT {
		t.Errorf("timings: ttfb %v, ttft %v, duration %v", rec.TTFB, rec.TTFT, rec.Duration)
	}
	if rec.EndedAt.Before(rec.TS) {
		t.Errorf("ended at %v before the start %v", rec.EndedAt, rec.TS)
	}
}

func TestTapScansJSONBody(t *testing.T) {
	body := fixture(t, "body_message.json")
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}), nil, withExtractor(ex))

	resp := e.do(t, "POST", "/v1/messages", `{}`, auth()...)
	if got := readAll(t, resp.Body); got != body {
		t.Errorf("body changed: %q", got)
	}
	rec := e.rec.next(t)
	_, facts, _ := ex.get()
	if facts.StopReason != "tool_use" || len(facts.ToolUses) != 1 || !facts.Complete {
		t.Errorf("facts = %+v", facts)
	}
	if rec.TTFT != 0 || rec.ExtractionPartial {
		t.Errorf("ttft %v, partial %v", rec.TTFT, rec.ExtractionPartial)
	}
}

func TestTapUpstreamErrorBody(t *testing.T) {
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, fixture(t, "body_error.json"))
	}), nil, withExtractor(ex))
	_ = readAll(t, e.do(t, "POST", "/v1/messages", `{}`, auth()...).Body)
	rec := e.rec.next(t)
	_, facts, _ := ex.get()
	if facts.ErrorType != "rate_limit_error" || rec.ErrorClass != "rate_limited" {
		t.Errorf("error type %q, class %q", facts.ErrorType, rec.ErrorClass)
	}
}

func TestTapOversizeEventSetsExtractionPartial(t *testing.T) {
	huge := "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" +
		strings.Repeat("x", anthropic.MaxEventBytes) + "\"}}\n\n"
	stream := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n" + huge +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":3}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	ex := &fakeExtractor{}
	e := newEnv(t, sseUpstream(stream, 64<<10), nil, withExtractor(ex))

	resp := e.do(t, "POST", "/v1/messages", `{}`, auth()...)
	if got := readAll(t, resp.Body); got != stream {
		t.Errorf("the client got %d bytes, want the whole %d", len(got), len(stream))
	}
	rec := e.rec.next(t)
	_, facts, _ := ex.get()
	if !rec.ExtractionPartial {
		t.Error("extraction_partial is not set")
	}
	if facts.StopReason != "end_turn" || !facts.Complete || facts.Usage.InputTokens == nil {
		t.Errorf("facts around the oversize event were lost: %+v", facts)
	}
}

func TestTapOversizeBodySetsExtractionPartial(t *testing.T) {
	body := `{"usage":{"input_tokens":1},"pad":"` + strings.Repeat("x", anthropic.MaxBodyBytes) + `"}`
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}), nil)
	resp := e.do(t, "POST", "/v1/messages", `{}`, auth()...)
	if got := readAll(t, resp.Body); got != body {
		t.Errorf("the client got %d bytes, want %d", len(got), len(body))
	}
	if rec := e.rec.next(t); !rec.ExtractionPartial {
		t.Error("extraction_partial is not set")
	}
}

func TestTapEncodedBodyPassesUnscanned(t *testing.T) {
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = io.WriteString(w, "\x1f\x8bnot really gzip")
	}), nil, withExtractor(ex))
	req, _ := http.NewRequest("POST", e.proxy.URL+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+clientKey)
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := (&http.Client{Transport: &http.Transport{DisableCompression: true}}).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := readAll(t, resp.Body); got != "\x1f\x8bnot really gzip" {
		t.Errorf("body changed: %q", got)
	}
	rec := e.rec.next(t)
	if _, facts, _ := ex.get(); !rec.ExtractionPartial || !facts.Partial {
		t.Errorf("an unreadable body is not marked partial: %+v", facts)
	}
}

func TestTapCloseFiresOnClientAbort(t *testing.T) {
	started := make(chan struct{})
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, fixture(t, "sse_truncated.sse"))
		w.(http.Flusher).Flush()
		close(started)
		<-r.Context().Done()
	}), nil, withExtractor(ex))

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", e.proxy.URL+"/v1/messages", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+clientKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	cancel()
	_ = resp.Body.Close()

	rec := e.rec.next(t)
	n, facts, _ := ex.get()
	if n != 1 {
		t.Errorf("Close handed facts to the extractor %d times, want 1", n)
	}
	if facts.Complete || len(facts.ToolUses) != 1 {
		t.Errorf("facts of the aborted stream = %+v", facts)
	}
	if rec.ErrorClass != string(core.ErrorClassClientAbort) {
		t.Errorf("error class %q, want client_abort", rec.ErrorClass)
	}
}

func TestIdleTimeoutBeforeHeadersIs504(t *testing.T) {
	cancelled := make(chan struct{})
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // the server notices a closed connection once the body is read
		<-r.Context().Done()
		close(cancelled)
	}), nil, withIdle("100ms"), withExtractor(ex))

	start := time.Now()
	resp := e.do(t, "POST", "/v1/messages", `{"stream":true}`, auth()...)
	body := readAll(t, resp.Body)
	if resp.StatusCode != http.StatusGatewayTimeout || !strings.Contains(body, "timeout_error") {
		t.Errorf("got %d %s", resp.StatusCode, body)
	}
	if d := time.Since(start); d < 100*time.Millisecond || d > 3*time.Second {
		t.Errorf("answered after %v", d)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream request was not cancelled")
	}
	rec := e.rec.next(t)
	if rec.Status != http.StatusGatewayTimeout || rec.ErrorClass != string(core.ErrorClassTimeout) {
		t.Errorf("record %d %q", rec.Status, rec.ErrorClass)
	}
	if n, _, _ := ex.get(); n != 0 {
		t.Errorf("extractor ran %d times for a call with no response", n)
	}
}

func TestNonStreamingWaitForHeadersOutlastsIdleTimeout(t *testing.T) {
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(400 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"type":"message","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":5}}`)
	}), nil, withIdle("100ms"))

	resp := e.do(t, "POST", "/v1/messages", `{"stream":false}`, auth()...)
	body := readAll(t, resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "end_turn") {
		t.Errorf("got %d %s, want the upstream's 200", resp.StatusCode, body)
	}
	if rec := e.rec.next(t); rec.Status != http.StatusOK || rec.ErrorClass != "" {
		t.Errorf("record %d %q, want 200 with no error", rec.Status, rec.ErrorClass)
	}
}

func TestIdleTimeoutAfterHeadersEndsStream(t *testing.T) {
	cancelled := make(chan struct{})
	ex := &fakeExtractor{}
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":4}}}\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}), nil, withIdle("100ms"), withExtractor(ex))

	resp := e.do(t, "POST", "/v1/messages", `{"stream":true}`, auth()...)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d: the headers had already gone, so it must stay 200", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Errorf("the stream ended with %v, want a clean end", err)
	}
	if !strings.HasPrefix(string(b), "event: message_start") || strings.Contains(string(b), "timeout") {
		t.Errorf("the client got %q: the proxy must add no body", b)
	}
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream request was not cancelled")
	}
	rec := e.rec.next(t)
	if rec.Status != http.StatusOK || rec.ErrorClass != string(core.ErrorClassTimeout) {
		t.Errorf("record %d %q, want 200 timeout", rec.Status, rec.ErrorClass)
	}
	n, facts, _ := ex.get()
	if n != 1 || facts.Usage.InputTokens == nil || *facts.Usage.InputTokens != 4 || facts.Complete {
		t.Errorf("extractor ran %d times, facts %+v", n, facts)
	}
}

func TestChunksKeepTheStreamAlive(t *testing.T) {
	// Seven pings 60ms apart outlast the 150ms idle timeout in total, but no
	// silence does.
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 7 {
			_, _ = io.WriteString(w, "event: ping\ndata: {\"type\":\"ping\"}\n\n")
			w.(http.Flusher).Flush()
			time.Sleep(60 * time.Millisecond)
		}
		_, _ = io.WriteString(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
	}), nil, withIdle("150ms"))
	resp := e.do(t, "POST", "/v1/messages", `{}`, auth()...)
	if got := readAll(t, resp.Body); strings.Count(got, "ping") != 14 || !strings.Contains(got, "message_stop") {
		t.Errorf("the stream was cut: %q", got)
	}
	if rec := e.rec.next(t); rec.ErrorClass != "" {
		t.Errorf("error class %q for a healthy stream", rec.ErrorClass)
	}
}

func TestSlowClientDoesNotTripTheWatchdog(t *testing.T) {
	chunk := strings.Repeat("event: ping\ndata: {}\n\n", 4<<10)
	e := newEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for range 64 {
			_, _ = io.WriteString(w, chunk)
			w.(http.Flusher).Flush()
		}
	}), nil, withIdle("100ms"))
	resp := e.do(t, "POST", "/v1/messages", `{}`, auth()...)
	defer func() { _ = resp.Body.Close() }()
	// Stall the client far past the idle timeout while the proxy waits on it.
	time.Sleep(400 * time.Millisecond)
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatal(err)
	}
	if rec := e.rec.next(t); rec.ErrorClass != "" {
		t.Errorf("error class %q: client back-pressure counted as upstream silence", rec.ErrorClass)
	}
}

func TestWatchdog(t *testing.T) {
	cause := make(chan error, 1)
	ctx, cancel := context.WithCancelCause(context.Background())
	w := newWatchdog(50*time.Millisecond, cancel)
	defer w.Disarm()

	// Disarmed, it never fires.
	w.Disarm()
	time.Sleep(120 * time.Millisecond)
	if w.Fired() || ctx.Err() != nil {
		t.Fatal("a disarmed watchdog fired")
	}

	// Armed and left alone, it fires once and cancels with its cause.
	go func() { <-ctx.Done(); cause <- context.Cause(ctx) }()
	w.Arm()
	select {
	case err := <-cause:
		if err != errIdleTimeout || !w.Fired() {
			t.Errorf("cause %v, fired %v", err, w.Fired())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the watchdog did not fire")
	}
}
