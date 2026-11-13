package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/mtanzi/spyglass/internal/core"
)

// cutoff is the retention cutoff the cases use, one hour after Base.
var cutoff = Base.Add(time.Hour)

// old returns n calls, with one tool use each, that started before the cutoff.
func old(e *env, n int) []core.CallRecord {
	return e.gen.Calls(n, e.gen.WithUse("toolu_old", "Bash"))
}

// fresh returns n calls, with one tool use each, that started at or after the
// cutoff.
func fresh(e *env, n int) []core.CallRecord {
	out := make([]core.CallRecord, n)
	for i := range out {
		out[i] = e.gen.Call(e.gen.At(cutoff.Add(time.Duration(i)*time.Second)), e.gen.WithUse("toolu_new", "Bash"))
	}
	return out
}

func (e *env) deleteBefore(t *testing.T, table string) int64 {
	t.Helper()
	n, err := e.DeleteBefore(context.Background(), table, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

var retentionCases = []testCase{
	{"rows_before_cutoff_go_and_nothing_else", func(t *testing.T, e *env) {
		e.write(t, old(e, 3)...)
		e.write(t, fresh(e, 2)...)
		e.wantCounts(t, 5, 5)
		if n := e.deleteBefore(t, "calls"); n != 3 {
			t.Fatalf("deleted %d calls, want 3", n)
		}
		// Each table is deleted on its own: the halves stay until asked for.
		e.wantCounts(t, 2, 5)
		if n := e.deleteBefore(t, "tool_events"); n != 3 {
			t.Fatalf("deleted %d tool halves, want 3", n)
		}
		e.wantCounts(t, 2, 2)
	}},
	{"row_at_cutoff_stays", func(t *testing.T, e *env) {
		g := e.gen
		e.write(t, g.Call(g.At(cutoff.Add(-time.Second))), g.Call(g.At(cutoff)), g.Call(g.At(cutoff.Add(time.Second))))
		if n := e.deleteBefore(t, "calls"); n != 1 {
			t.Fatalf("deleted %d calls, want 1", n)
		}
		e.wantCounts(t, 2, 0)
	}},
	{"batches_respect_the_size", func(t *testing.T, e *env) {
		size := e.BatchSize()
		n := 2*size + size/2 + 1 // three full-or-partial batches, the last one short
		e.write(t, old(e, n)...)
		e.write(t, fresh(e, 1)...)
		before := e.RetentionStats()
		if got := e.deleteBefore(t, "calls"); got != int64(n) {
			t.Fatalf("deleted %d calls, want %d", got, n)
		}
		if got := e.RetentionStats().Batches - before.Batches; got != 3 {
			t.Fatalf("used %d batches for %d rows of size %d, want 3", got, n, size)
		}
		if got := e.RetentionStats().Deleted - before.Deleted; got != int64(n) {
			t.Fatalf("counted %d deleted rows, want %d", got, n)
		}
		e.wantCounts(t, 1, n+1)
	}},
	{"whole_batches_delete_everything", func(t *testing.T, e *env) {
		n := 2 * e.BatchSize() // the last batch is full, so one more finds nothing
		e.write(t, old(e, n)...)
		if got := e.deleteBefore(t, "calls"); got != int64(n) {
			t.Fatalf("deleted %d calls, want %d", got, n)
		}
		e.wantCounts(t, 0, n)
	}},
	{"second_run_deletes_nothing", func(t *testing.T, e *env) {
		e.write(t, old(e, e.BatchSize()+1)...)
		e.write(t, fresh(e, 2)...)
		for _, table := range []string{"calls", "tool_events"} {
			if e.deleteBefore(t, table) == 0 {
				t.Fatalf("first run on %s deleted nothing", table)
			}
		}
		e.wantCounts(t, 2, 2)
		before := e.RetentionStats()
		for _, table := range []string{"calls", "tool_events"} {
			if n := e.deleteBefore(t, table); n != 0 {
				t.Fatalf("second run on %s deleted %d rows", table, n)
			}
		}
		e.wantCounts(t, 2, 2)
		if after := e.RetentionStats(); after.Deleted != before.Deleted || after.Batches != before.Batches {
			t.Fatalf("a run that deleted nothing changed the counters: %+v then %+v", before, after)
		}
	}},
	{"empty_store_deletes_nothing", func(t *testing.T, e *env) {
		if n := e.deleteBefore(t, "calls") + e.deleteBefore(t, "tool_events"); n != 0 {
			t.Fatalf("deleted %d rows from an empty store", n)
		}
	}},
	{"cancelled_context_stops_the_delete", func(t *testing.T, e *env) {
		e.write(t, old(e, 3)...)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		n, err := e.DeleteBefore(ctx, "calls", cutoff)
		if err == nil {
			t.Fatal("DeleteBefore ignored a cancelled context")
		}
		if n != 0 {
			t.Fatalf("deleted %d rows after cancel, want 0", n)
		}
		e.wantCounts(t, 3, 3)
	}},
	{"unknown_table_is_refused", func(t *testing.T, e *env) {
		if _, err := e.DeleteBefore(context.Background(), "goose_db_version", cutoff); err == nil {
			t.Fatal("DeleteBefore accepted a table the store does not retain")
		}
	}},
}
