package coretest

import (
	"context"
	"sync"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// Writer is a core.CallWriter that appends to a slice. Like the real port it is
// idempotent on call id. Set Err to make every WriteBatch fail with nothing stored.
type Writer struct {
	mu      sync.Mutex
	calls   []core.CallRecord
	seen    map[core.ID]bool
	batches int
	Err     error
}

var _ core.CallWriter = (*Writer)(nil)

// WriteBatch stores the calls whose ids it has not seen.
func (w *Writer) WriteBatch(_ context.Context, calls []core.CallRecord) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.Err != nil {
		return w.Err
	}
	if w.seen == nil {
		w.seen = make(map[core.ID]bool)
	}
	w.batches++
	for _, c := range calls {
		if w.seen[c.ID] {
			continue
		}
		w.seen[c.ID] = true
		w.calls = append(w.calls, c)
	}
	return nil
}

// Calls returns a copy of the stored calls in write order.
func (w *Writer) Calls() []core.CallRecord {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]core.CallRecord(nil), w.calls...)
}

// Batches returns the number of successful WriteBatch calls.
func (w *Writer) Batches() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.batches
}
