package storetest

import (
	"bytes"
	"testing"
)

func TestGenIDsForSameTSSortInCreationOrder(t *testing.T) {
	g := NewGen()
	prev := g.ID(Base)
	for i := range 100 {
		id := g.ID(Base)
		if bytes.Compare(prev[:], id[:]) >= 0 {
			t.Fatalf("id %d (%s) does not sort after previous id (%s)", i+1, id, prev)
		}
		if id.String()[14] != '7' {
			t.Fatalf("id %s is not version 7", id)
		}
		prev = id
	}
}
