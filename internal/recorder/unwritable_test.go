package recorder

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// storeCause is an unwritable store error that names its cause, as the
// Postgres adapter's does.
type storeCause struct{ code, constraint, column string }

func (e *storeCause) Error() string      { return "store rejects record (SQLSTATE " + e.code + ")" }
func (e *storeCause) Unwrap() error      { return core.ErrUnwritable }
func (e *storeCause) SQLState() string   { return e.code }
func (e *storeCause) Constraint() string { return e.constraint }
func (e *storeCause) Column() string     { return e.column }

// poisonWriter stores a batch unless it holds a rejected record, in which
// case it fails the whole batch with an ErrUnwritable error, as an atomic
// store does. Records are named by Status. transient picks calls that fail
// with a transient error first; block makes calls of blockSize records wait
// on release (or ctx).
type poisonWriter struct {
	mu        sync.Mutex
	reject    map[int]bool
	stored    map[int]int // Status -> times stored
	calls     int
	transient func(calls []core.CallRecord) bool
	onCall    func(n int) // called with w.mu held, before the call is served
	blockSize int
	block     chan struct{}
	entered   chan struct{}
}

func newPoisonWriter(reject ...int) *poisonWriter {
	w := &poisonWriter{stored: map[int]int{}}
	w.setReject(reject...)
	return w
}

func (w *poisonWriter) setReject(reject ...int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reject = map[int]bool{}
	for _, n := range reject {
		w.reject[n] = true
	}
}

func (w *poisonWriter) WriteBatch(ctx context.Context, calls []core.CallRecord) error {
	w.mu.Lock()
	w.calls++
	if w.onCall != nil {
		w.onCall(w.calls)
	}
	if w.transient != nil && w.transient(calls) {
		w.mu.Unlock()
		return errors.New("store down")
	}
	block := w.block != nil && len(calls) == w.blockSize
	if block && w.entered != nil {
		select {
		case w.entered <- struct{}{}:
		default:
		}
	}
	w.mu.Unlock()
	if block {
		select {
		case <-w.block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, c := range calls {
		if w.reject[c.Status] {
			return fmt.Errorf("write batch: %w", &storeCause{code: "23514", constraint: "calls_check"})
		}
	}
	for _, c := range calls {
		w.stored[c.Status]++
	}
	return nil
}

// storedCount is the number of distinct records stored.
func (w *poisonWriter) storedCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.stored)
}

func (w *poisonWriter) isStored(n int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stored[n] > 0
}

func (w *poisonWriter) callCount() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.calls
}

// sendBatch queues records 0..n-1 as one batch: the batch interval is an hour,
// so a batch closes at BatchSize records or at Close.
func sendBatch(r *Recorder, n int) {
	for i := range n {
		r.Finished(rec(i))
	}
}

func wantDrops(t *testing.T, r *Recorder, unwritable, shutdown uint64) {
	t.Helper()
	if got := r.CallsDropped(ReasonUnwritable); got != unwritable {
		t.Errorf("unwritable drops = %d, want %d", got, unwritable)
	}
	if got := r.CallsDropped(ReasonShutdown); got != shutdown {
		t.Errorf("shutdown drops = %d, want %d", got, shutdown)
	}
	if got := r.CallsDropped(ReasonQueueFull); got != 0 {
		t.Errorf("queue_full drops = %d, want 0", got)
	}
}

func closeOK(t *testing.T, r *Recorder) {
	t.Helper()
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestUnwritableRecordIsolated(t *testing.T) {
	cases := []struct {
		name   string
		size   int
		reject []int
	}{
		{"one_in_500", BatchSize, []int{137}},
		{"several", BatchSize, []int{3, 4, 250, 498}},
		{"first_and_last", BatchSize, []int{0, BatchSize - 1}},
		{"batch_of_one", 1, []int{0}},
		{"all_of_2", 2, []int{0, 1}},
		{"all_of_7", SystematicMinRecords - 1, []int{0, 1, 2, 3, 4, 5, 6}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newPoisonWriter(c.reject...)
			r := New(w, fast(Options{batchInterval: time.Hour}))
			sendBatch(r, c.size)
			closeOK(t, r) // flushes the batch
			if got, want := w.storedCount(), c.size-len(c.reject); got != want {
				t.Errorf("stored = %d, want %d", got, want)
			}
			for _, n := range c.reject {
				if w.isStored(n) {
					t.Errorf("rejected record %d stored", n)
				}
			}
			wantDrops(t, r, uint64(len(c.reject)), 0)
			if r.SystematicFailure() {
				t.Error("systematic failure set for a batch with dropped records")
			}
			// Every failed call, whole batch and halves, is a writer error.
			if got, calls := r.WriterErrors(), uint64(w.callCount()); got > calls || got == 0 {
				t.Errorf("writer errors = %d of %d calls", got, calls)
			}
		})
	}
}

func TestUnwritableSplitCostIsLogarithmic(t *testing.T) {
	w := newPoisonWriter(137)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	closeOK(t, r)
	// One whole-batch call, then two calls per level of the split (9 levels).
	if got := w.callCount(); got > 1+2*9 {
		t.Fatalf("WriteBatch calls = %d, want at most 19", got)
	}
}

func TestUnwritableDropLogNamesCauseWithoutBody(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	log := slog.New(slog.NewJSONHandler(&lockedWriter{w: &buf, mu: &mu}, nil))
	w := newPoisonWriter(0)
	o := fast(Options{batchInterval: time.Hour})
	o.Logger = log
	r := New(w, o)
	c := rec(0)
	c.ID = core.NewID()
	c.KeyName = "alice"
	c.Model = "BODY-SECRET-MODEL"
	r.Finished(c)
	closeOK(t, r)
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	var line string
	for l := range strings.SplitSeq(out, "\n") {
		if strings.Contains(l, "dropped unwritable call") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no drop log line in:\n%s", out)
	}
	for _, want := range []string{`"level":"ERROR"`, `"call_id":"` + c.ID.String() + `"`, `"key_name":"alice"`, `"sqlstate":"23514"`, `"constraint":"calls_check"`} {
		if !strings.Contains(line, want) {
			t.Errorf("drop log lacks %s: %s", want, line)
		}
	}
	if strings.Contains(out, "BODY-SECRET-MODEL") {
		t.Errorf("log carries a record field: %s", out)
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestTransientErrorInsideHalf(t *testing.T) {
	w := newPoisonWriter(10)
	failed := false
	w.transient = func(calls []core.CallRecord) bool {
		if len(calls) == BatchSize/2 && !failed {
			failed = true
			return true
		}
		return false
	}
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	closeOK(t, r)
	if !failed {
		t.Fatal("no transient error was injected")
	}
	if got := w.storedCount(); got != BatchSize-1 {
		t.Fatalf("stored = %d, want %d", got, BatchSize-1)
	}
	wantDrops(t, r, 1, 0)
}

func TestTransientErrorNeverDrops(t *testing.T) {
	w := newPoisonWriter()
	n := 0
	w.transient = func([]core.CallRecord) bool { n++; return n <= 5 }
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	closeOK(t, r)
	if got := w.storedCount(); got != BatchSize {
		t.Fatalf("stored = %d, want %d", got, BatchSize)
	}
	if got := w.callCount(); got != 6 {
		t.Fatalf("WriteBatch calls = %d, want 6: a transient error retries the whole batch", got)
	}
	wantDrops(t, r, 0, 0)
}

func allRecords(n int) []int {
	ids := make([]int, n)
	for i := range ids {
		ids[i] = i
	}
	return ids
}

func TestSystematicFailureKeepsBatchThenIsolatesAfterFix(t *testing.T) {
	const detection = 2*BatchSize - 1 // calls to split an all-fail batch to its leaves
	w := newPoisonWriter(allRecords(BatchSize)...)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	var heldAtRetry, heldAtFix bool
	var storedBeforeFix int
	w.onCall = func(n int) {
		switch n {
		case detection + 1: // the first retry of the whole batch
			heldAtRetry = r.SystematicFailure()
		case 2*detection + 1: // the second retry: the fix leaves one record bad
			heldAtFix = r.SystematicFailure()
			storedBeforeFix = len(w.stored)
			w.reject = map[int]bool{42: true}
		}
	}
	sendBatch(r, BatchSize)
	eventually(t, "isolation after the fix", func() bool { return r.CallsDropped(ReasonUnwritable) == 1 })
	closeOK(t, r)
	if !heldAtRetry || !heldAtFix {
		t.Fatalf("systematic failure at retries = %v, %v, want true", heldAtRetry, heldAtFix)
	}
	if storedBeforeFix != 0 {
		t.Fatalf("stored = %d during the systematic failure, want 0", storedBeforeFix)
	}
	if got := w.storedCount(); got != BatchSize-1 {
		t.Fatalf("stored = %d, want %d", got, BatchSize-1)
	}
	if w.isStored(42) {
		t.Fatal("rejected record stored")
	}
	if r.SystematicFailure() {
		t.Fatal("systematic failure still set after a commit")
	}
	wantDrops(t, r, 1, 0)
}

func TestSystematicThreshold(t *testing.T) {
	// SystematicMinRecords all-fail records are kept until the Close deadline.
	w := newPoisonWriter(allRecords(SystematicMinRecords)...)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, SystematicMinRecords)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
	if !r.SystematicFailure() {
		t.Fatal("systematic failure not set")
	}
	if got := w.callCount(); got <= 2*SystematicMinRecords-1 {
		t.Fatalf("WriteBatch calls = %d, want a retry after the first detection", got)
	}
	wantDrops(t, r, 0, SystematicMinRecords)
}

func TestAbortDuringSplitCountsWholeBatch(t *testing.T) {
	w := newPoisonWriter(400)
	w.blockSize = BatchSize / 2
	w.block = make(chan struct{})
	w.entered = make(chan struct{}, 1)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	<-w.entered // the first half is in flight
	for i := range 5 {
		r.Finished(rec(BatchSize + i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
	if got := w.storedCount(); got != 0 {
		t.Fatalf("stored = %d, want 0", got)
	}
	wantDrops(t, r, 0, BatchSize+5)
}

func TestAbortAfterLeafFailureCountsItAsShutdown(t *testing.T) {
	// Record 0 fails alone in the first quarter; the abort comes while the
	// second half is in flight, so record 0 is not yet dropped as unwritable.
	w := newPoisonWriter(0)
	w.blockSize = BatchSize / 2
	w.block = make(chan struct{})
	w.entered = make(chan struct{}, 1)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	<-w.entered           // first half
	w.block <- struct{}{} // release it: it splits and stores all but record 0
	<-w.entered           // second half
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
	stored := w.storedCount()
	if stored != BatchSize/2-1 {
		t.Fatalf("stored = %d, want %d", stored, BatchSize/2-1)
	}
	wantDrops(t, r, 0, uint64(BatchSize-stored))
}

func TestAbortDuringSystematicRetryCountsWholeBatch(t *testing.T) {
	w := newPoisonWriter(allRecords(BatchSize)...)
	r := New(w, fast(Options{batchInterval: time.Hour}))
	sendBatch(r, BatchSize)
	eventually(t, "systematic failure", r.SystematicFailure)
	for i := range 3 {
		r.Finished(rec(BatchSize + i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
	wantDrops(t, r, 0, BatchSize+3)
}

func TestCollectorExportsEveryReasonAndGauge(t *testing.T) {
	r := New(&fakeWriter{}, fast(Options{}))
	t.Cleanup(func() { closeOK(t, r) })
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(r.Collector())
	// gather returns the drop counter by reason and the systematic-failure
	// gauge, or -1 when the gauge is missing.
	gather := func() (map[string]float64, float64) {
		fams, err := reg.Gather()
		if err != nil {
			t.Fatal(err)
		}
		reasons, gauge := map[string]float64{}, -1.0
		for _, f := range fams {
			for _, m := range f.GetMetric() {
				switch f.GetName() {
				case MetricCallsDropped:
					reasons[m.GetLabel()[0].GetValue()] = m.GetCounter().GetValue()
				case MetricSystematicFailure:
					if m.GetGauge() != nil && len(m.GetLabel()) == 0 {
						gauge = m.GetGauge().GetValue()
					}
				}
			}
		}
		return reasons, gauge
	}
	reasons, gauge := gather()
	for _, reason := range []string{ReasonQueueFull, ReasonShutdown, ReasonUnwritable} {
		if v, ok := reasons[reason]; !ok || v != 0 {
			t.Errorf("reason %q = %v (exported %v), want 0", reason, v, ok)
		}
	}
	if gauge != 0 {
		t.Errorf("%s = %v, want an unlabelled gauge at 0", MetricSystematicFailure, gauge)
	}
	r.systematic.Store(true)
	if _, gauge := gather(); gauge != 1 {
		t.Errorf("%s = %v, want 1", MetricSystematicFailure, gauge)
	}
}
