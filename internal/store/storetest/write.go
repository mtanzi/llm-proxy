package storetest

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/mtanzi/llm-proxy/internal/core"
)

var writeCases = []testCase{
	{"first_write_stores_calls_and_halves", func(t *testing.T, e *env) {
		use, result := e.gen.ToolPair("toolu_1", "s1")
		e.write(t, use, result, e.gen.Call())
		e.wantCounts(t, 3, 2)
	}},
	{"empty_batch_stores_nothing", func(t *testing.T, e *env) {
		e.write(t)
		e.wantCounts(t, 0, 0)
	}},
	{"replayed_batch_inserts_nothing", func(t *testing.T, e *env) {
		g := e.gen
		a := g.Call(g.WithUse("toolu_a", "Bash"), g.WithResult("toolu_z", true))
		b := g.Call(g.WithUse("toolu_b", "Grep"))
		c := g.Call()
		e.write(t, a, b)
		e.wantCounts(t, 2, 3)
		// The same batch, an overlapping batch, and a duplicate inside one batch.
		e.write(t, a, b)
		e.write(t, b, c)
		e.write(t, c, c, a)
		e.wantCounts(t, 3, 3)
	}},
	{"concurrent_writers_store_each_call_once", func(t *testing.T, e *env) {
		g := e.gen
		batch := []core.CallRecord{
			g.Call(g.WithUse("toolu_a", "Bash")),
			g.Call(g.WithResult("toolu_a", false)),
			g.Call(),
		}
		var wg sync.WaitGroup
		errs := make([]error, 6)
		for i := range errs {
			wg.Go(func() { errs[i] = e.WriteBatch(context.Background(), batch) })
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
		e.wantCounts(t, 3, 2)
	}},
	{"failed_batch_stores_nothing", func(t *testing.T, e *env) {
		g := e.gen
		before := g.Call(g.WithUse("toolu_a", "Bash"))
		e.write(t, before)

		good := g.Call(g.WithUse("toolu_b", "Grep"))
		// A tool half that overflows its column fails the batch after the
		// call rows are inserted.
		bad := g.Call(g.WithUse("toolu_c", "Edit"))
		bad.ToolUses[0].ArgBytes = 1 << 40
		err := e.WriteBatch(context.Background(), []core.CallRecord{good, bad})
		if err == nil {
			t.Fatal("WriteBatch accepted a record the store cannot hold")
		}
		// The recorder isolates and drops such a record (ADR 0051).
		if !errors.Is(err, core.ErrUnwritable) {
			t.Fatalf("WriteBatch error = %v, want one that matches core.ErrUnwritable", err)
		}
		e.wantCounts(t, 1, 1)

		// The same batch, fixed, is written in full.
		bad.ToolUses[0].ArgBytes = 10
		e.write(t, good, bad)
		e.wantCounts(t, 3, 3)
	}},
	{"halves_in_any_order", func(t *testing.T, e *env) {
		// The result call may reach the store before the use call, in the same
		// batch or another, and may be replayed. Each order stores the pair
		// once. Every order has its own tool call id, in the one store.
		orders := []struct {
			name    string
			batches [][]int // indexes into the pair: 0 is the use call, 1 the result call
		}{
			{"use_first", [][]int{{0}, {1}}},
			{"result_first", [][]int{{1}, {0}}},
			{"one_batch", [][]int{{1, 0}}},
			{"replays", [][]int{{1}, {1}, {0}, {1, 0}}},
		}
		for i, o := range orders {
			use, result := e.gen.ToolPair("toolu_"+o.name, "s-"+o.name)
			pair := []core.CallRecord{use, result}
			for _, b := range o.batches {
				var calls []core.CallRecord
				for _, j := range b {
					calls = append(calls, pair[j])
				}
				e.write(t, calls...)
			}
			e.wantCounts(t, 2*(i+1), 2*(i+1))
		}
	}},
}
