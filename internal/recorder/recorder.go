package recorder

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

const (
	// BatchSize closes a batch when it holds this many records.
	BatchSize = 500
	// BatchInterval closes a batch this long after its first record.
	BatchInterval = 500 * time.Millisecond

	// DefaultQueueCapacity is the default for Options.QueueCapacity.
	DefaultQueueCapacity = 10_000
	// DefaultCloseTimeout is the flush deadline of Close when its context has none.
	DefaultCloseTimeout = 30 * time.Second

	// SystematicMinRecords is the smallest batch whose failure is taken as
	// systematic when every record in it is unwritable: the batch is kept and
	// retried instead of dropped (ADR 0051).
	SystematicMinRecords = 8

	subscriberBuffer = 256
	retryBase        = 100 * time.Millisecond
	retryMax         = 10 * time.Second
)

// Drop reasons, the values of the reason label of MetricCallsDropped.
const (
	ReasonQueueFull  = "queue_full"
	ReasonShutdown   = "shutdown"
	ReasonUnwritable = "unwritable"
)

// Options configures a Recorder. The zero value uses the defaults.
type Options struct {
	QueueCapacity int           // default DefaultQueueCapacity
	CloseTimeout  time.Duration // Close deadline when its context has none; default DefaultCloseTimeout
	Logger        *slog.Logger  // default slog.Default()

	// Test hooks. Zero means the package default.
	batchInterval time.Duration
	retryBase     time.Duration
	retryMax      time.Duration
}

// Event is one finished call on the live tail.
type Event struct {
	Call core.CallRecord
}

type subscriber struct {
	ch     chan Event
	filter func(core.CallRecord) bool
}

// Recorder implements the proxy's Recorder seam. Create it with New and stop
// it with Close.
type Recorder struct {
	writer core.CallWriter
	log    *slog.Logger
	opts   Options

	mu     sync.Mutex
	closed bool
	subs   map[*subscriber]struct{}
	queue  chan core.CallRecord

	abort     context.CancelFunc
	abortCtx  context.Context
	done      chan struct{}
	closeOnce sync.Once

	droppedFull       atomic.Uint64
	droppedShutdown   atomic.Uint64
	droppedUnwritable atomic.Uint64
	writerErrors      atomic.Uint64
	liveDropped       atomic.Uint64
	systematic        atomic.Bool
}

// New starts a Recorder that writes batches to w.
func New(w core.CallWriter, opts Options) *Recorder {
	if opts.QueueCapacity <= 0 {
		opts.QueueCapacity = DefaultQueueCapacity
	}
	if opts.CloseTimeout <= 0 {
		opts.CloseTimeout = DefaultCloseTimeout
	}
	if opts.batchInterval <= 0 {
		opts.batchInterval = BatchInterval
	}
	if opts.retryBase <= 0 {
		opts.retryBase = retryBase
	}
	if opts.retryMax <= 0 {
		opts.retryMax = retryMax
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	r := &Recorder{
		writer: w,
		log:    log,
		opts:   opts,
		subs:   make(map[*subscriber]struct{}),
		queue:  make(chan core.CallRecord, opts.QueueCapacity),
		done:   make(chan struct{}),
	}
	r.abortCtx, r.abort = context.WithCancel(context.Background())
	go r.run()
	return r
}

// Finished fans the call out to subscribers and queues it for the writer. It
// never blocks: a full queue or a closed recorder drops the record and counts it.
func (r *Recorder) Finished(c core.CallRecord) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		r.droppedShutdown.Add(1)
		return
	}
	for s := range r.subs {
		if s.filter != nil && !s.filter(c) {
			continue
		}
		select {
		case s.ch <- Event{Call: c}:
		default:
			r.liveDropped.Add(1)
		}
	}
	select {
	case r.queue <- c:
	default:
		r.droppedFull.Add(1)
	}
}

// Subscribe returns a channel of finished calls for which filter reports true
// (nil matches all), and a function that ends the subscription and closes the
// channel. A subscriber that reads too slowly loses events; the loss is counted.
// After Close the channel is closed.
func (r *Recorder) Subscribe(filter func(core.CallRecord) bool) (<-chan Event, func()) {
	s := &subscriber{ch: make(chan Event, subscriberBuffer), filter: filter}
	r.mu.Lock()
	if r.closed {
		close(s.ch)
		r.mu.Unlock()
		return s.ch, func() {}
	}
	r.subs[s] = struct{}{}
	r.mu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if _, ok := r.subs[s]; ok {
				delete(r.subs, s)
				close(s.ch)
			}
		})
	}
}

// Close stops intake and flushes the queue. It waits until the writer has
// written everything or ctx ends; with no deadline on ctx it uses
// Options.CloseTimeout. Records still queued at the deadline are counted as
// dropped (reason shutdown) and their number is logged. The error is ctx's
// error when the deadline passed.
func (r *Recorder) Close(ctx context.Context) error {
	var err error
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		close(r.queue)
		for s := range r.subs {
			close(s.ch)
		}
		r.subs = nil
		r.mu.Unlock()

		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, r.opts.CloseTimeout)
			defer cancel()
		}
		select {
		case <-r.done:
		case <-ctx.Done():
			err = ctx.Err()
			r.abort() // the writer drops what is left and exits
			<-r.done
		}
	})
	return err
}

// QueueDepth is the number of records waiting for the writer (MetricQueueDepth).
func (r *Recorder) QueueDepth() int { return len(r.queue) }

// CallsDropped counts dropped calls by reason, ReasonQueueFull, ReasonShutdown
// or ReasonUnwritable (MetricCallsDropped).
func (r *Recorder) CallsDropped(reason string) uint64 {
	switch reason {
	case ReasonQueueFull:
		return r.droppedFull.Load()
	case ReasonShutdown:
		return r.droppedShutdown.Load()
	case ReasonUnwritable:
		return r.droppedUnwritable.Load()
	}
	return 0
}

// SystematicFailure reports whether the writer holds a batch of at least
// SystematicMinRecords records that all failed as unwritable
// (MetricSystematicFailure). It is cleared when a write commits.
func (r *Recorder) SystematicFailure() bool { return r.systematic.Load() }

// WriterErrors counts failed WriteBatch calls (MetricWriterErrors).
func (r *Recorder) WriterErrors() uint64 { return r.writerErrors.Load() }

// LiveEventsDropped counts events a slow subscriber lost (MetricLiveEventsDropped).
func (r *Recorder) LiveEventsDropped() uint64 { return r.liveDropped.Load() }

// run is the one writer goroutine.
func (r *Recorder) run() {
	defer close(r.done)
	defer r.abort()
	batch := make([]core.CallRecord, 0, BatchSize)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	defer timer.Stop()

	for {
		select {
		case c, ok := <-r.queue:
			if !ok {
				r.flush(batch)
				return
			}
			if len(batch) == 0 {
				timer.Reset(r.opts.batchInterval)
			}
			batch = append(batch, c)
			if len(batch) >= BatchSize {
				timer.Stop()
				r.flush(batch)
				batch = batch[:0]
			}
		case <-timer.C:
			r.flush(batch)
			batch = batch[:0]
		}
	}
}

// flush writes one batch until every record is stored or dropped, or Close
// aborts the writer. A transient error retries with capped exponential backoff
// and jitter. An unwritable error splits the batch to find the records the
// store rejects (write). When the split is done, the rejected records are
// dropped, unless every record of a batch of at least SystematicMinRecords
// failed: that failure is systematic, so the whole batch is kept and retried
// with backoff. After an abort it drops what is not yet stored and everything
// still queued.
func (r *Recorder) flush(batch []core.CallRecord) {
	if len(batch) == 0 {
		return
	}
	f := flushState{pending: len(batch)}
	delay := r.opts.retryBase
	for {
		f.failed = f.failed[:0]
		if !r.write(batch, &f) {
			r.dropRest(f.pending)
			return
		}
		if len(f.failed) == 0 {
			return
		}
		if len(f.failed) < len(batch) || len(batch) < SystematicMinRecords {
			r.dropUnwritable(&f)
			return
		}
		r.systematic.Store(true)
		first := f.failed[0]
		code, constraint, column := cause(first.err)
		r.log.Error("recorder: every record of the batch is unwritable, keeping it",
			"records", len(batch), "sqlstate", code, "constraint", constraint, "column", column, "retry_in", delay)
		if !r.sleep(delay) {
			r.dropRest(f.pending)
			return
		}
		delay = min(delay*2, r.opts.retryMax)
	}
}

// flushState tracks one original batch across its split.
type flushState struct {
	pending int       // records not yet stored or dropped
	failed  []failure // records rejected alone with ErrUnwritable, in batch order
}

type failure struct {
	call core.CallRecord
	err  error
}

// write stores recs as one atomic batch. A transient error retries the same
// slice with backoff. An unwritable error splits recs in two halves, written in
// order through the same logic; a single unwritable record is added to
// f.failed, and its drop is left to flush. It returns false when Close aborted
// the writer.
func (r *Recorder) write(recs []core.CallRecord, f *flushState) bool {
	delay := r.opts.retryBase
	for {
		if r.abortCtx.Err() != nil {
			return false
		}
		err := r.writer.WriteBatch(r.abortCtx, recs)
		if err == nil {
			f.pending -= len(recs)
			r.systematic.Store(false)
			return true
		}
		if r.abortCtx.Err() != nil {
			return false
		}
		r.writerErrors.Add(1)
		if errors.Is(err, core.ErrUnwritable) {
			if len(recs) == 1 {
				f.failed = append(f.failed, failure{call: recs[0], err: err})
				return true
			}
			mid := len(recs) / 2
			return r.write(recs[:mid], f) && r.write(recs[mid:], f)
		}
		r.log.Error("recorder: write batch failed", "err", err, "records", len(recs), "retry_in", delay)
		if !r.sleep(delay) {
			return false
		}
		delay = min(delay*2, r.opts.retryMax)
	}
}

// sleep waits about delay, with jitter. It returns false when Close aborted
// the writer.
func (r *Recorder) sleep(delay time.Duration) bool {
	select {
	case <-time.After(delay/2 + rand.N(delay/2+1)):
		return true
	case <-r.abortCtx.Done():
		return false
	}
}

// dropUnwritable drops the records in f.failed and logs each one. The log
// names the record and the cause, never a body field (ADR 0002).
func (r *Recorder) dropUnwritable(f *flushState) {
	for _, x := range f.failed {
		code, constraint, column := cause(x.err)
		r.log.Error("recorder: dropped unwritable call",
			"call_id", x.call.ID.String(), "key_name", x.call.KeyName,
			"sqlstate", code, "constraint", constraint, "column", column)
	}
	r.droppedUnwritable.Add(uint64(len(f.failed)))
	f.pending -= len(f.failed)
	f.failed = f.failed[:0]
}

// unwritableCause is implemented by a store error that names why the store
// rejected a record. The recorder imports no driver, so it reads the cause
// through this interface only.
type unwritableCause interface {
	error
	SQLState() string
	Constraint() string
	Column() string
}

// cause returns the SQLSTATE, constraint and column of err, empty when the
// store does not name them.
func cause(err error) (sqlState, constraint, column string) {
	if c, ok := errors.AsType[unwritableCause](err); ok {
		return c.SQLState(), c.Constraint(), c.Column()
	}
	return "", "", ""
}

// dropRest counts inHand records and everything left in the queue as dropped.
// The queue is closed by the time Close aborts, so the range ends.
func (r *Recorder) dropRest(inHand int) {
	n := inHand
	for range r.queue {
		n++
	}
	r.droppedShutdown.Add(uint64(n))
	r.log.Error("recorder: dropped queued calls at shutdown", "dropped", n)
}
