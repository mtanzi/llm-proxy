package recorder

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/mtanzi/spyglass/internal/core"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// fakeWriter records batches. fail makes the next n calls fail; block makes
// calls wait on release (or ctx).
type fakeWriter struct {
	mu      sync.Mutex
	batches [][]core.CallRecord
	fail    int
	calls   int
	block   chan struct{}
	entered chan struct{}
}

func (f *fakeWriter) WriteBatch(ctx context.Context, calls []core.CallRecord) error {
	f.mu.Lock()
	f.calls++
	if f.entered != nil {
		select {
		case f.entered <- struct{}{}:
		default:
		}
	}
	block := f.block
	if f.fail > 0 {
		f.fail--
		f.mu.Unlock()
		return errors.New("store down")
	}
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batches = append(f.batches, append([]core.CallRecord(nil), calls...))
	return nil
}

func (f *fakeWriter) written() (batches, records int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.batches {
		records += len(b)
	}
	return len(f.batches), records
}

func (f *fakeWriter) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func rec(n int) core.CallRecord { return core.CallRecord{Model: "m", Status: n} }

func fast(o Options) Options {
	o.Logger = quiet
	o.retryBase = time.Millisecond
	o.retryMax = 5 * time.Millisecond
	return o
}

func TestBatchBySize(t *testing.T) {
	w := &fakeWriter{}
	r := New(w, fast(Options{batchInterval: time.Hour}))
	for i := range BatchSize {
		r.Finished(rec(i))
	}
	eventually(t, "full batch", func() bool { b, n := w.written(); return b == 1 && n == BatchSize })
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, _ := w.written(); b != 1 {
		t.Fatalf("batches = %d, want 1", b)
	}
}

func TestBatchByTime(t *testing.T) {
	w := &fakeWriter{}
	r := New(w, fast(Options{}))
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	start := time.Now()
	r.Finished(rec(1))
	r.Finished(rec(2))
	eventually(t, "timed batch", func() bool { _, n := w.written(); return n == 2 })
	if d := time.Since(start); d < BatchInterval-50*time.Millisecond {
		t.Fatalf("batch closed after %v, want about %v", d, BatchInterval)
	}
	if b, _ := w.written(); b != 1 {
		t.Fatalf("batches = %d, want 1", b)
	}
}

func TestDropOnFullQueueAndNoBlocking(t *testing.T) {
	w := &fakeWriter{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	r := New(w, fast(Options{QueueCapacity: 2, batchInterval: time.Millisecond}))
	r.Finished(rec(0))
	<-w.entered // writer holds one record and is stuck

	done := make(chan struct{})
	go func() {
		for i := range 100 {
			r.Finished(rec(i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Finished blocked while the writer was stuck")
	}
	if got := r.CallsDropped(ReasonQueueFull); got != 98 {
		t.Fatalf("queue_full drops = %d, want 98", got)
	}
	if got := r.QueueDepth(); got != 2 {
		t.Fatalf("depth = %d, want 2", got)
	}
	close(w.block)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, n := w.written(); n != 3 {
		t.Fatalf("written = %d, want 3", n)
	}
}

func TestRetryThenSuccess(t *testing.T) {
	w := &fakeWriter{fail: 3}
	r := New(w, fast(Options{batchInterval: time.Millisecond}))
	r.Finished(rec(1))
	eventually(t, "write", func() bool { _, n := w.written(); return n == 1 })
	if got := r.WriterErrors(); got != 3 {
		t.Fatalf("writer errors = %d, want 3", got)
	}
	if got := w.callCount(); got != 4 {
		t.Fatalf("WriteBatch calls = %d, want 4", got)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := r.CallsDropped(ReasonQueueFull) + r.CallsDropped(ReasonShutdown); got != 0 {
		t.Fatalf("drops = %d, want 0", got)
	}
}

func TestCloseFlushes(t *testing.T) {
	w := &fakeWriter{}
	r := New(w, fast(Options{batchInterval: time.Hour}))
	for i := range 7 {
		r.Finished(rec(i))
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b, n := w.written(); b != 1 || n != 7 {
		t.Fatalf("batches=%d records=%d, want 1 and 7", b, n)
	}
	r.Finished(rec(99))
	if got := r.CallsDropped(ReasonShutdown); got != 1 {
		t.Fatalf("shutdown drops after Close = %d, want 1", got)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCloseDeadlineDropsRemainder(t *testing.T) {
	w := &fakeWriter{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	r := New(w, fast(Options{QueueCapacity: 10, batchInterval: time.Millisecond}))
	r.Finished(rec(0))
	<-w.entered
	for i := 1; i <= 5; i++ {
		r.Finished(rec(i))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
	if got := r.CallsDropped(ReasonShutdown); got != 6 {
		t.Fatalf("shutdown drops = %d, want 6", got)
	}
	if got := r.WriterErrors(); got != 0 {
		t.Fatalf("writer errors = %d, want 0 for a write cut off by the Close abort", got)
	}
	if _, n := w.written(); n != 0 {
		t.Fatalf("written = %d, want 0", n)
	}
}

func TestCloseDeadlineWhileStoreDown(t *testing.T) {
	w := &fakeWriter{fail: 1 << 30}
	r := New(w, fast(Options{batchInterval: time.Millisecond}))
	r.Finished(rec(1))
	eventually(t, "first error", func() bool { return r.WriterErrors() > 0 })
	r.Finished(rec(2))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := r.Close(ctx); err == nil {
		t.Fatal("Close returned nil with the store down")
	}
	if got := r.CallsDropped(ReasonShutdown); got != 2 {
		t.Fatalf("shutdown drops = %d, want 2", got)
	}
}

func TestCloseTimeoutOption(t *testing.T) {
	w := &fakeWriter{block: make(chan struct{}), entered: make(chan struct{}, 1)}
	r := New(w, fast(Options{CloseTimeout: 30 * time.Millisecond, batchInterval: time.Millisecond}))
	r.Finished(rec(0))
	<-w.entered
	if err := r.Close(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Close err = %v, want deadline exceeded", err)
	}
}

func TestSubscribeFilterAndUnsubscribe(t *testing.T) {
	r := New(&fakeWriter{}, fast(Options{}))
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	all, unsubAll := r.Subscribe(nil)
	odd, unsubOdd := r.Subscribe(func(c core.CallRecord) bool { return c.Status%2 == 1 })
	for i := range 4 {
		r.Finished(rec(i))
	}
	if len(all) != 4 || len(odd) != 2 {
		t.Fatalf("buffered all=%d odd=%d, want 4 and 2", len(all), len(odd))
	}
	if ev := <-odd; ev.Call.Status != 1 {
		t.Fatalf("first odd event status = %d", ev.Call.Status)
	}
	unsubOdd()
	unsubOdd() // safe twice
	<-odd      // the one event still buffered
	if _, ok := <-odd; ok {
		t.Fatal("channel still open after unsubscribe")
	}
	r.Finished(rec(5))
	unsubAll()
}

func TestSlowSubscriberLosesEvents(t *testing.T) {
	r := New(&fakeWriter{}, fast(Options{QueueCapacity: 1000}))
	t.Cleanup(func() {
		if err := r.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	})
	ch, unsub := r.Subscribe(nil)
	defer unsub()
	n := subscriberBuffer + 10
	for i := range n {
		r.Finished(rec(i))
	}
	if got := r.LiveEventsDropped(); got != 10 {
		t.Fatalf("live drops = %d, want 10", got)
	}
	if len(ch) != subscriberBuffer {
		t.Fatalf("buffered = %d, want %d", len(ch), subscriberBuffer)
	}
}

func TestCloseClosesSubscribers(t *testing.T) {
	r := New(&fakeWriter{}, fast(Options{}))
	ch, unsub := r.Subscribe(nil)
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := <-ch; ok {
		t.Fatal("subscriber channel open after Close")
	}
	unsub()
	late, _ := r.Subscribe(nil)
	if _, ok := <-late; ok {
		t.Fatal("late subscriber channel open")
	}
}

func TestConcurrentFinishedSubscribeClose(t *testing.T) {
	w := &fakeWriter{}
	r := New(w, fast(Options{QueueCapacity: 100, batchInterval: time.Millisecond}))
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 500 {
				r.Finished(rec(i))
				if i%50 == g {
					ch, unsub := r.Subscribe(nil)
					select {
					case <-ch:
					default:
					}
					unsub()
				}
			}
		}()
	}
	wg.Wait()
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, written := w.written()
	total := uint64(written) + r.CallsDropped(ReasonQueueFull) + r.CallsDropped(ReasonShutdown)
	if total != 8*500 {
		t.Fatalf("written+dropped = %d, want %d", total, 8*500)
	}
}
