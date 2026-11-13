package coretest

import (
	"context"
	"errors"
	"testing"

	"github.com/mtanzi/spyglass/internal/core"
)

func TestWriterIdempotentOnID(t *testing.T) {
	w := &Writer{}
	a, b := core.CallRecord{ID: core.NewID()}, core.CallRecord{ID: core.NewID()}
	for _, batch := range [][]core.CallRecord{{a, b}, {a}, {b, a}} {
		if err := w.WriteBatch(context.Background(), batch); err != nil {
			t.Fatal(err)
		}
	}
	if got := w.Calls(); len(got) != 2 || got[0].ID != a.ID || got[1].ID != b.ID {
		t.Fatalf("calls = %v, want a then b once each", got)
	}
	if w.Batches() != 3 {
		t.Fatalf("batches = %d, want 3", w.Batches())
	}
}

func TestWriterErrStoresNothing(t *testing.T) {
	boom := errors.New("boom")
	w := &Writer{Err: boom}
	if err := w.WriteBatch(context.Background(), []core.CallRecord{{ID: core.NewID()}}); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if len(w.Calls()) != 0 || w.Batches() != 0 {
		t.Fatal("failed batch left state behind")
	}
}

func TestReaderCanned(t *testing.T) {
	id := core.NewID()
	r := &Reader{
		Calls:   []core.CallSummary{{ID: id}},
		Details: map[core.ID]core.CallDetail{id: {CallRecord: core.CallRecord{ID: id}}},
	}
	got, _, err := r.ListCalls(context.Background(), core.CallFilter{}, core.Page{})
	if err != nil || len(got) != 1 || got[0].ID != id {
		t.Fatalf("ListCalls = %v, %v", got, err)
	}
	if d, err := r.GetCall(context.Background(), id); err != nil || d.ID != id {
		t.Fatalf("GetCall = %v, %v", d, err)
	}
	if _, err := r.GetCall(context.Background(), core.NewID()); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetCall unknown err = %v, want ErrNotFound", err)
	}
	if _, err := r.GetSession(context.Background(), "x"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("GetSession unknown err = %v, want ErrNotFound", err)
	}
	r.Err = errors.New("down")
	if _, err := r.Usage(context.Background(), core.UsageQuery{}); err == nil {
		t.Fatal("Usage ignored Err")
	}
}
