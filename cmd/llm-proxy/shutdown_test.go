package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/core/coretest"
	"github.com/mtanzi/llm-proxy/internal/httpmw"
	"github.com/mtanzi/llm-proxy/internal/recorder"
)

// syncBuf is a log sink safe for the goroutines of a test.
type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// eventLog records the order of shutdown steps.
type eventLog struct {
	mu sync.Mutex
	ev []string
}

func (l *eventLog) add(e string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ev = append(l.ev, e)
}

func (l *eventLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.ev...)
}

type fakeRecorder struct{ ev *eventLog }

func (f fakeRecorder) Close(context.Context) error { f.ev.add("recorder.close"); return nil }
func (f fakeRecorder) CallsDropped(string) uint64  { return 0 }

type fakeRetention struct{ ev *eventLog }

func (f fakeRetention) StopRetention(context.Context) error { f.ev.add("retention.stop"); return nil }

type fakeStore struct{ ev *eventLog }

func (f fakeStore) Close() { f.ev.add("store.close") }

// shutdownEnv is a Servers pair with one streaming route on the data plane.
// A request to /stream sends one chunk, announces itself on started, waits on
// release and sends a second chunk.
type shutdownEnv struct {
	srv      *Servers
	dataAddr string
	admin    string
	started  chan struct{}
	release  chan struct{}
	inflight atomic.Int64
	logs     *syncBuf
	log      *slog.Logger
	// onClose runs, if set, when the data listener is closed.
	onClose atomic.Pointer[func()]
}

// closeHookListener calls the env's onClose hook when the listener closes,
// which http.Server.Shutdown does synchronously before it waits for streams.
type closeHookListener struct {
	net.Listener
	e *shutdownEnv
}

func (l closeHookListener) Close() error {
	if f := l.e.onClose.Load(); f != nil {
		(*f)()
	}
	return l.Listener.Close()
}

func newShutdownEnv(t *testing.T) *shutdownEnv {
	t.Helper()
	e := &shutdownEnv{started: make(chan struct{}, 8), release: make(chan struct{}), logs: &syncBuf{}}
	e.log = slog.New(slog.NewTextHandler(e.logs, nil))
	drain := &httpmw.Drain{}

	data := http.NewServeMux()
	data.HandleFunc("/stream", func(w http.ResponseWriter, r *http.Request) {
		e.inflight.Add(1)
		defer e.inflight.Add(-1)
		_, _ = io.WriteString(w, "chunk1\n")
		w.(http.Flusher).Flush()
		e.started <- struct{}{}
		select {
		case <-e.release:
		case <-r.Context().Done():
			return
		}
		_, _ = io.WriteString(w, "chunk2\n")
	})
	admin := http.NewServeMux()
	admin.Handle("/readyz", httpmw.Readyz(drain, func(context.Context) error { return nil }))

	e.srv = &Servers{
		Data:     &http.Server{Handler: data},
		Admin:    &http.Server{Handler: admin},
		Drain:    drain,
		Inflight: e.inflight.Load,
	}
	dln, aln := listen(t), listen(t)
	e.dataAddr, e.admin = dln.Addr().String(), aln.Addr().String()
	go func() { _ = e.srv.ServeData(closeHookListener{dln, e}) }()
	go func() { _ = e.srv.ServeAdmin(aln) }()
	t.Cleanup(func() {
		select {
		case <-e.release:
		default:
			close(e.release)
		}
		_ = e.srv.Data.Close()
		_ = e.srv.Admin.Close()
	})
	return e
}

func (e *shutdownEnv) readyz(t *testing.T) int {
	t.Helper()
	resp, err := http.Get("http://" + e.admin + "/readyz")
	if err != nil {
		t.Fatalf("GET /readyz: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

func (e *shutdownEnv) openStream(t *testing.T) *http.Response {
	t.Helper()
	resp, err := http.Get("http://" + e.dataAddr + "/stream")
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	select {
	case <-e.started:
	case <-time.After(5 * time.Second):
		t.Fatal("stream handler did not start")
	}
	return resp
}

// doAsync runs s.Do and returns the channel of its result.
func doAsync(s *Shutdown) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- s.Do(context.Background()) }()
	return ch
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func finishedRecords(n int) []core.CallRecord {
	out := make([]core.CallRecord, n)
	for i := range out {
		out[i] = core.CallRecord{ID: core.NewID()}
	}
	return out
}

func TestShutdownStreamFinishesDuringDrainAndNewRequestsAreRefused(t *testing.T) {
	e := newShutdownEnv(t)
	resp := e.openStream(t)
	defer func() { _ = resp.Body.Close() }()
	if got := e.readyz(t); got != http.StatusOK {
		t.Fatalf("readyz before shutdown = %d, want 200", got)
	}

	s := &Shutdown{Servers: e.srv, DrainTimeout: 10 * time.Second, Log: e.log}
	done := doAsync(s)

	waitFor(t, "drain flag", e.srv.Drain.Active)
	// Admin stays up and reports not-ready while the stream drains.
	if got := e.readyz(t); got != http.StatusServiceUnavailable {
		t.Errorf("readyz during drain = %d, want 503", got)
	}
	// The data listener refuses new connections.
	waitFor(t, "data listener closed", func() bool {
		c, err := net.DialTimeout("tcp", e.dataAddr, time.Second)
		if err == nil {
			_ = c.Close()
			return false
		}
		return true
	})
	select {
	case err := <-done:
		t.Fatalf("Do returned %v while a stream was open", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(e.release)
	body, err := io.ReadAll(resp.Body)
	if err != nil || string(body) != "chunk1\nchunk2\n" {
		t.Errorf("stream body = %q, err %v; want both chunks", body, err)
	}
	if err := <-done; err != nil {
		t.Errorf("Do = %v, want nil", err)
	}
}

func TestShutdownDrainDeadlineCutsStreams(t *testing.T) {
	e := newShutdownEnv(t)
	resp := e.openStream(t)
	defer func() { _ = resp.Body.Close() }()

	s := &Shutdown{Servers: e.srv, DrainTimeout: 50 * time.Millisecond, Log: e.log}
	err := s.Do(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Do = %v, want a deadline error", err)
	}
	if !strings.Contains(e.logs.String(), "inflight_streams=1") {
		t.Errorf("log does not name the cut stream:\n%s", e.logs.String())
	}
	if _, err := io.ReadAll(resp.Body); err == nil {
		t.Error("stream ended cleanly, want it cut at the drain deadline")
	}
}

func TestShutdownFlushesQueuedRecords(t *testing.T) {
	e := newShutdownEnv(t)
	w := &coretest.Writer{}
	rec := recorder.New(w, recorder.Options{Logger: e.log})
	for _, c := range finishedRecords(7) {
		rec.Finished(c)
	}
	s := &Shutdown{Servers: e.srv, Recorder: rec, DrainTimeout: time.Second, FlushTimeout: 5 * time.Second, Log: e.log}
	if err := s.Do(context.Background()); err != nil {
		t.Fatalf("Do = %v", err)
	}
	if got := len(w.Calls()); got != 7 {
		t.Errorf("store has %d records after shutdown, want 7", got)
	}
	if got := rec.CallsDropped(recorder.ReasonShutdown); got != 0 {
		t.Errorf("dropped = %d, want 0", got)
	}
}

// hangWriter blocks every write until its context ends.
type hangWriter struct{}

func (hangWriter) WriteBatch(ctx context.Context, _ []core.CallRecord) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestShutdownHungWriterHitsFlushDeadlineAndCountsDrops(t *testing.T) {
	e := newShutdownEnv(t)
	rec := recorder.New(hangWriter{}, recorder.Options{Logger: e.log})
	for _, c := range finishedRecords(5) {
		rec.Finished(c)
	}
	s := &Shutdown{Servers: e.srv, Recorder: rec, DrainTimeout: time.Second, FlushTimeout: 50 * time.Millisecond, Log: e.log}

	start := time.Now()
	err := s.Do(context.Background())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Do = %v, want a deadline error", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("Do took %v, the flush deadline did not bound it", time.Since(start))
	}
	if got := rec.CallsDropped(recorder.ReasonShutdown); got != 5 {
		t.Errorf("dropped(shutdown) = %d, want 5", got)
	}
	if !strings.Contains(e.logs.String(), "dropped=5") {
		t.Errorf("log does not carry the drop count:\n%s", e.logs.String())
	}
}

func TestShutdownOrder(t *testing.T) {
	e := newShutdownEnv(t)
	ev := &eventLog{}
	// The drain flag must be on when the data listener closes, so /readyz
	// flips before accepts stop.
	hook := func() {
		ev.add("listeners.stop drain=" + map[bool]string{true: "on", false: "off"}[e.srv.Drain.Active()])
	}
	e.onClose.Store(&hook)
	s := &Shutdown{
		Servers:      e.srv,
		Recorder:     fakeRecorder{ev},
		Retention:    fakeRetention{ev},
		Store:        fakeStore{ev},
		DrainTimeout: time.Second,
		Log:          e.log,
	}
	if err := s.Do(context.Background()); err != nil {
		t.Fatalf("Do = %v", err)
	}
	want := []string{"listeners.stop drain=on", "recorder.close", "retention.stop", "store.close"}
	if got := ev.all(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestShutdownRunSecondSignalForcesExit(t *testing.T) {
	e := newShutdownEnv(t)
	resp := e.openStream(t) // never released: the drain would take the full timeout
	defer func() { _ = resp.Body.Close() }()

	exit := make(chan int, 1)
	s := &Shutdown{
		Servers:      e.srv,
		DrainTimeout: time.Minute,
		Log:          e.log,
		Exit:         func(code int) { exit <- code },
	}
	sigs := make(chan os.Signal, 2)
	result := make(chan error, 1)
	go func() { result <- s.Run(sigs) }()

	sigs <- syscall.SIGTERM
	waitFor(t, "drain flag", e.srv.Drain.Active)
	sigs <- syscall.SIGTERM

	select {
	case code := <-exit:
		if code != 1 {
			t.Errorf("exit code = %d, want 1", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("second signal did not force an exit")
	}
	if err := <-result; !errors.Is(err, ErrForcedExit) {
		t.Errorf("Run = %v, want ErrForcedExit", err)
	}
	if !strings.Contains(e.logs.String(), "second signal") {
		t.Errorf("no log line for the forced exit:\n%s", e.logs.String())
	}
}

func TestShutdownRunReturnsAfterGracefulStop(t *testing.T) {
	e := newShutdownEnv(t)
	s := &Shutdown{Servers: e.srv, DrainTimeout: time.Second, Log: e.log, Exit: func(int) { t.Error("Exit called") }}
	sigs := make(chan os.Signal, 2)
	sigs <- syscall.SIGTERM
	if err := s.Run(sigs); err != nil {
		t.Errorf("Run = %v, want nil", err)
	}
}
