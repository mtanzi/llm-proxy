package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/store/postgres/pgtest"
)

func TestMain(m *testing.M) { pgtest.Main(m) }

const upstreamReply = `{"id":"msg_1","type":"message","role":"assistant","model":"claude-test","content":[],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":5}}`

// fakeUpstream answers every request like the Anthropic messages endpoint.
func fakeUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamReply)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// emptyCollector stands in for the store's retention collector.
type emptyCollector struct{}

func (emptyCollector) Describe(chan<- *prometheus.Desc) {}
func (emptyCollector) Collect(chan<- prometheus.Metric) {}

// wiringStore is a storeAdapter in memory: no database. Reader methods are the
// nil embedded interface; a test that calls one fails loudly.
type wiringStore struct {
	core.CallReader

	mu        sync.Mutex
	calls     []core.CallRecord
	pingErr   error
	retention string // "", "started" or "stopped"
	closed    bool
}

func (f *wiringStore) WriteBatch(_ context.Context, calls []core.CallRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, calls...)
	return nil
}
func (f *wiringStore) Ping(context.Context) error { return f.pingErr }
func (f *wiringStore) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
}
func (f *wiringStore) StartRetention(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retention = "started"
	return nil
}
func (f *wiringStore) StopRetention(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retention = "stopped"
	return nil
}
func (f *wiringStore) RetentionCollector() prometheus.Collector { return emptyCollector{} }

// running is an app serving on loopback listeners.
type running struct {
	a        *app
	data     string // base URL of the data plane
	admin    string // base URL of the admin plane
	term     chan os.Signal
	hup      chan os.Signal
	finished chan error
}

// start serves a and returns once both listeners are bound.
func start(t *testing.T, a *app) *running {
	t.Helper()
	r := &running{
		a:        a,
		term:     make(chan os.Signal, 2),
		hup:      make(chan os.Signal, 1),
		finished: make(chan error, 1),
	}
	dataLn, adminLn := listen(t), listen(t)
	r.data, r.admin = "http://"+dataLn.Addr().String(), "http://"+adminLn.Addr().String()
	go func() { r.finished <- a.run(dataLn, adminLn, r.term, r.hup) }()
	return r
}

// stop sends SIGTERM and waits for run to return.
func (r *running) stop(t *testing.T) error {
	t.Helper()
	r.term <- syscall.SIGTERM
	select {
	case err := <-r.finished:
		return err
	case <-time.After(30 * time.Second):
		t.Fatal("run did not return after SIGTERM")
		return nil
	}
}

// post sends one messages request with the test key and returns the response
// status and request id.
func post(t *testing.T, base string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/v1/messages", strings.NewReader(`{"model":"claude-test","max_tokens":8,"messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Authorization", "Bearer "+testClientKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK && string(body) != upstreamReply {
		t.Errorf("client got %q, want the upstream reply unchanged", body)
	}
	return resp.StatusCode, resp.Header.Get("x-request-id")
}

func get(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// The data plane, recorder and admin plane wired together with fakes only: a
// request through the proxy reaches the store as a record, retention starts and
// stops, and SIGTERM ends run.
func TestWiringWithFakes(t *testing.T) {
	upstream := fakeUpstream(t)
	st := &wiringStore{}
	a, err := buildApp(loadedHolder(t, upstream.URL), st, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if st.retention != "started" {
		t.Errorf("retention = %q after buildApp, want started", st.retention)
	}
	r := start(t, a)

	if code, _ := get(t, r.data+"/readyz"); code != http.StatusOK {
		t.Errorf("data /readyz = %d, want 200", code)
	}
	if code, _ := get(t, r.admin+"/readyz"); code != http.StatusOK {
		t.Errorf("admin /readyz = %d, want 200", code)
	}
	code, id := post(t, r.data)
	if code != http.StatusOK {
		t.Fatalf("forwarded call = %d, want 200", code)
	}
	code, body := get(t, r.admin+"/admin/config")
	if code != http.StatusOK {
		t.Fatalf("/admin/config = %d", code)
	}
	var view struct {
		Providers []struct{ Name string }
		Keys      []struct{ Name string }
	}
	if err := json.Unmarshal(body, &view); err != nil || len(view.Providers) != 1 || len(view.Keys) != 1 || view.Keys[0].Name != "alice" {
		t.Errorf("/admin/config = %s (err %v), want the running config", body, err)
	}
	if bytes.Contains(body, []byte("provider-secret")) {
		t.Error("/admin/config leaks the provider credential")
	}

	if err := r.stop(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if len(st.calls) != 1 || st.calls[0].ID.String() != id || st.calls[0].KeyName != "alice" || st.calls[0].Status != 200 {
		t.Errorf("store has %+v, want one record of call %s for key alice", st.calls, id)
	}
	if st.retention != "stopped" || !st.closed {
		t.Errorf("after shutdown: retention %q, closed %v; want stopped and closed", st.retention, st.closed)
	}
}

// The live tail is wired to the recorder (ADR 0025): a call forwarded on the
// data plane reaches an open GET /admin/stream on the real admin listener as a
// call.finished event, unbuffered, and SIGTERM ends the stream and run.
func TestWiringLiveTail(t *testing.T) {
	upstream := fakeUpstream(t)
	a, err := buildApp(loadedHolder(t, upstream.URL), &wiringStore{}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	r := start(t, a)
	if code, _ := get(t, r.admin+"/readyz"); code != http.StatusOK {
		t.Fatalf("admin /readyz = %d, want 200", code)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.admin+"/admin/stream?key=alice", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream answered %d with headers %v", resp.StatusCode, resp.Header)
	}

	code, id := post(t, r.data)
	if code != http.StatusOK {
		t.Fatalf("forwarded call = %d, want 200", code)
	}
	br := bufio.NewReader(resp.Body)
	var frame []string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v (so far %q)", err, frame)
		}
		if line == "\n" {
			break
		}
		frame = append(frame, strings.TrimSuffix(line, "\n"))
	}
	if len(frame) != 3 || frame[0] != "id: "+id || frame[1] != "event: call.finished" || !strings.Contains(frame[2], `"key":"alice"`) {
		t.Errorf("frame %q, want call.finished for call %s of key alice", frame, id)
	}

	if err := r.stop(t); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := io.ReadAll(br); err != nil {
		t.Errorf("stream did not end cleanly at shutdown: %v", err)
	}
}

// With the store down, the admin plane is not ready and the data plane keeps
// forwarding (ADR 0028, Consequences).
func TestWiringStoreDownDataPlaneStillForwards(t *testing.T) {
	upstream := fakeUpstream(t)
	st := &wiringStore{pingErr: errors.New("down")}
	a, err := buildApp(loadedHolder(t, upstream.URL), st, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	r := start(t, a)
	if code, _ := post(t, r.data); code != http.StatusOK {
		t.Errorf("forwarded call = %d, want 200", code)
	}
	if code, _ := get(t, r.admin+"/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("admin /readyz = %d, want 503", code)
	}
	if err := r.stop(t); err != nil {
		t.Fatal(err)
	}
}

func TestBuildAppRefusesNonLoopbackAdmin(t *testing.T) {
	h := loadedHolder(t, "http://127.0.0.1:1")
	h.Current().Config().Listen.Admin = "0.0.0.0:9090"
	if _, err := buildApp(h, &wiringStore{}, testLogger()); err == nil || !strings.Contains(err.Error(), "loopback") {
		t.Errorf("err = %v, want a loopback refusal", err)
	}
}

// A listener that cannot serve stops the process through the same shutdown.
func TestRunStopsWhenAListenerFails(t *testing.T) {
	st := &wiringStore{}
	a, err := buildApp(loadedHolder(t, "http://127.0.0.1:1"), st, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	bad, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = bad.Close() // Serve on a closed listener fails at once
	term := make(chan os.Signal, 2)
	done := make(chan error, 1)
	go func() { done <- a.run(bad, listen(t), term, make(chan os.Signal)) }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "data listener") {
			t.Errorf("run = %v, want the data listener error", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("run did not stop")
	}
	if !st.closed {
		t.Error("store not closed after the listener failure")
	}
}

func TestWaitForDatabaseRetriesThenSucceeds(t *testing.T) {
	var now time.Time
	var slept []time.Duration
	r := startupRetry{
		Timeout: 30 * time.Second, Initial: time.Second, Max: 4 * time.Second,
		Now:   func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error { slept = append(slept, d); now = now.Add(d); return nil },
	}
	n := 0
	ping := func(context.Context) error {
		if n++; n < 5 {
			return errors.New("refused")
		}
		return nil
	}
	if err := waitForDatabase(context.Background(), ping, testLogger(), r); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 4 * time.Second}
	if len(slept) != len(want) {
		t.Fatalf("pauses = %v, want %v", slept, want)
	}
	for i := range want {
		if slept[i] != want[i] {
			t.Errorf("pauses = %v, want %v", slept, want)
			break
		}
	}
}

func TestWaitForDatabaseGivesUp(t *testing.T) {
	var now time.Time
	r := startupRetry{
		Timeout: 10 * time.Second, Initial: time.Second, Max: 4 * time.Second,
		Now:   func() time.Time { return now },
		Sleep: func(_ context.Context, d time.Duration) error { now = now.Add(d); return nil },
	}
	err := waitForDatabase(context.Background(), func(context.Context) error { return errors.New("connection refused") }, testLogger(), r)
	if err == nil || !strings.Contains(err.Error(), "not reachable") || !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("err = %v, want a not-reachable error that keeps the last cause", err)
	}
	if now.Sub(time.Time{}) > r.Timeout {
		t.Errorf("waited %v, past the %v bound", now.Sub(time.Time{}), r.Timeout)
	}
}

func TestWaitForDatabaseStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := defaultStartupRetry()
	err := waitForDatabase(ctx, func(context.Context) error { return errors.New("refused") }, testLogger(), r)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestLoggerRedactsCredentialAttributes(t *testing.T) {
	var buf bytes.Buffer
	log := newLogger(config.Log{Level: "info", Format: "json"}, &buf)
	log.Info("x", "Authorization", "Bearer lp_secret", "x-api-key", "sk-secret", "model", "claude")
	out := buf.String()
	if strings.Contains(out, "lp_secret") || strings.Contains(out, "sk-secret") {
		t.Errorf("log leaks a credential: %s", out)
	}
	if !strings.Contains(out, `"model":"claude"`) {
		t.Errorf("log lost a plain attribute: %s", out)
	}
	buf.Reset()
	newLogger(config.Log{Level: "warn", Format: "text"}, &buf).Info("quiet")
	if buf.Len() != 0 {
		t.Errorf("info logged at level warn: %s", buf.String())
	}
}
