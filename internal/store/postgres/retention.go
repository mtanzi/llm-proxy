package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"
)

// Retention defaults. A zero field in Retention takes the default.
const (
	DefaultBatchSize  = 10000
	DefaultBatchPause = 100 * time.Millisecond
)

// The daily job starts at retentionRunAt UTC plus a random delay up to
// retentionJitter.
const (
	retentionRunAt  = 3 * time.Hour // 03:00 UTC
	retentionJitter = 30 * time.Minute
)

// Retention holds the retention job settings. cmd fills Keep from the
// retention section of the config file; the other fields keep their defaults
// outside tests.
type Retention struct {
	Keep       time.Duration // calls and tool_events older than this are deleted; zero or less disables the job
	BatchSize  int           // rows per DELETE; each batch is its own transaction
	BatchPause time.Duration // wait between two batches
}

func (r Retention) withDefaults() Retention {
	if r.BatchSize <= 0 {
		r.BatchSize = DefaultBatchSize
	}
	if r.BatchPause <= 0 {
		r.BatchPause = DefaultBatchPause
	}
	return r
}

// RetentionStats are the retention job values that ADR 0026 exports as
// spyglass_retention_deleted_total and
// spyglass_retention_last_run_timestamp_seconds.
type RetentionStats struct {
	Deleted int64     // rows deleted since the store opened (spyglass_retention_deleted_total)
	Batches int64     // DELETE batches that removed rows
	LastRun time.Time // end of the last run that finished without error; zero before one (spyglass_retention_last_run_timestamp_seconds)
}

// retentionState is the job state kept by a Store.
type retentionState struct {
	deleted     atomic.Int64
	batches     atomic.Int64
	lastRunUnix atomic.Int64 // zero until a run finishes

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// RetentionStats returns the job counters. It is safe to call at any time.
func (s *Store) RetentionStats() RetentionStats {
	st := RetentionStats{Deleted: s.ret.deleted.Load(), Batches: s.ret.batches.Load()}
	if u := s.ret.lastRunUnix.Load(); u != 0 {
		st.LastRun = time.Unix(0, u)
	}
	return st
}

// BatchSize returns the number of rows one DELETE batch removes at most.
func (s *Store) BatchSize() int { return s.retention.BatchSize }

// DeleteBefore deletes the rows of table whose ts is before cutoff, in
// batches of the configured size, each in its own transaction, with a pause
// between batches. table is "calls" or "tool_events". It returns the number
// of rows deleted, which on error or cancellation is the number deleted so
// far. Running it again with the same cutoff deletes nothing.
func (s *Store) DeleteBefore(ctx context.Context, table string, cutoff time.Time) (int64, error) {
	switch table {
	case "calls", "tool_events":
	default:
		return 0, fmt.Errorf("postgres: delete before: unknown table %q", table)
	}
	// ctid names the physical row, so the one statement fits both tables.
	query := fmt.Sprintf(
		"DELETE FROM %[1]s WHERE ctid IN (SELECT ctid FROM %[1]s WHERE ts < $1 LIMIT $2)", table)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, fmt.Errorf("postgres: delete before: %w", err)
		}
		tag, err := s.pool.Exec(ctx, query, cutoff, s.retention.BatchSize)
		if err != nil {
			return total, fmt.Errorf("postgres: delete before: %w", err)
		}
		n := tag.RowsAffected()
		total += n
		if n > 0 {
			s.ret.deleted.Add(n)
			s.ret.batches.Add(1)
		}
		if n < int64(s.retention.BatchSize) {
			return total, nil
		}
		select {
		case <-ctx.Done():
			return total, fmt.Errorf("postgres: delete before: %w", ctx.Err())
		case <-time.After(s.retention.BatchPause):
		}
	}
}

// RunRetention runs the job once: it deletes the rows of calls and tool_events
// older than the retention period, logs one line and records the run time. It
// returns the rows deleted from each table. It is what the scheduler calls
// and is safe to call again at any time.
func (s *Store) RunRetention(ctx context.Context) (calls, toolEvents int64, err error) {
	if s.retention.Keep <= 0 {
		return 0, 0, errors.New("postgres: retention: no retention period configured")
	}
	start := time.Now()
	cutoff := start.Add(-s.retention.Keep)
	log := slog.Default()
	calls, err = s.DeleteBefore(ctx, "calls", cutoff)
	if err == nil {
		toolEvents, err = s.DeleteBefore(ctx, "tool_events", cutoff)
	}
	if err != nil {
		log.Error("retention run failed", "error", err, "calls_deleted", calls,
			"tool_events_deleted", toolEvents, "cutoff", cutoff, "duration", time.Since(start))
		return calls, toolEvents, err
	}
	s.ret.lastRunUnix.Store(time.Now().UnixNano())
	log.Info("retention run", "calls_deleted", calls, "tool_events_deleted", toolEvents,
		"cutoff", cutoff, "duration", time.Since(start))
	return calls, toolEvents, nil
}

// StartRetention starts the daily job in a goroutine owned by the store. The
// job runs at retentionRunAt UTC plus a random delay up to retentionJitter,
// every day, until ctx ends or StopRetention or Close is called. A run in
// progress when that happens is cancelled. It returns an error if the job is
// already running or no retention period is configured.
func (s *Store) StartRetention(ctx context.Context) error {
	if s.retention.Keep <= 0 {
		return errors.New("postgres: retention: no retention period configured")
	}
	s.ret.mu.Lock()
	defer s.ret.mu.Unlock()
	if s.ret.cancel != nil {
		return errors.New("postgres: retention: already started")
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	s.ret.cancel, s.ret.done = cancel, done
	go func() {
		defer close(done)
		for {
			wait := untilNextRun(time.Now(), rand.N(retentionJitter+1))
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
			_, _, _ = s.RunRetention(ctx) // logged by RunRetention; the next day retries
		}
	}()
	return nil
}

// StopRetention stops the job and waits for its goroutine to end, or for ctx
// to end, whichever comes first. It does nothing if the job is not running.
func (s *Store) StopRetention(ctx context.Context) error {
	s.ret.mu.Lock()
	cancel, done := s.ret.cancel, s.ret.done
	s.ret.cancel, s.ret.done = nil, nil
	s.ret.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("postgres: retention: stop: %w", ctx.Err())
	}
}

// untilNextRun returns how long to wait from now for the next occurrence of
// retentionRunAt plus jitter. A run is never scheduled for the occurrence of
// retentionRunAt that has already started today.
func untilNextRun(now time.Time, jitter time.Duration) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(retentionRunAt)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next.Add(jitter).Sub(now)
}
