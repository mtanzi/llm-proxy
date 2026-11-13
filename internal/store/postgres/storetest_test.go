package postgres_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mtanzi/spyglass/internal/core"
	"github.com/mtanzi/spyglass/internal/store/postgres"
	"github.com/mtanzi/spyglass/internal/store/storetest"
)

func TestContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) *postgres.Store {
		s, _ := newStore(t)
		return s
	})
}

// The no-bodies case passes when the scan finds nothing, so prove that the
// scan reaches text, jsonb and tool columns.
func TestStoredValuesReachesEveryTextLikeColumn(t *testing.T) {
	s, conn := newStore(t)
	ctx := context.Background()
	if err := s.WriteBatch(ctx, []core.CallRecord{fullCall()}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE calls SET usage_raw = '{"hidden":"BODY-IN-JSONB"}', prev_tool_durations = 'BODY-IN-TEXT'`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `UPDATE tool_events SET tool_name = 'BODY-IN-TOOL' WHERE kind = 'use' AND idx = 1`); err != nil {
		t.Fatal(err)
	}
	values, err := s.StoredValues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"BODY-IN-JSONB", "BODY-IN-TEXT", "BODY-IN-TOOL", "claude-opus-5-5", "toolu_a"} {
		found := false
		for _, v := range values {
			found = found || strings.Contains(v.Value, want)
		}
		if !found {
			t.Errorf("scan does not reach %q", want)
		}
	}
}

func TestCountRowsRejectsUnknownTable(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.CountRows(context.Background(), "goose_db_version; DROP TABLE calls"); err == nil {
		t.Fatal("CountRows accepted an unknown table")
	}
}
