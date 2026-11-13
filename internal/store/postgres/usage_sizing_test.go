package postgres_test

import (
	"context"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/mtanzi/spyglass/internal/core"
)

// SizingRowsEnv names the variable that turns on TestUsageSizing: the number of
// generated calls. The test measures the 30-day aggregates that
// docs/architecture/deployment.md, "Sizing", quotes. It is off by default
// because it takes minutes and a few GB of disk.
const sizingRowsEnv = "SPYGLASS_SIZING_ROWS"

// legacySizingRowsEnv is the deprecated 0.1.0 name of sizingRowsEnv, read when
// it is unset (ADR 0054).
const legacySizingRowsEnv = "LLM_PROXY_SIZING_ROWS"

func TestUsageSizing(t *testing.T) {
	rows := os.Getenv(sizingRowsEnv)
	if rows == "" {
		if rows = os.Getenv(legacySizingRowsEnv); rows != "" {
			t.Logf("%s is deprecated; set %s instead", legacySizingRowsEnv, sizingRowsEnv)
		}
	}
	n, _ := strconv.Atoi(rows)
	if n <= 0 {
		t.Skipf("set %s (for example 1500000) to measure", sizingRowsEnv)
	}
	s, conn := newStore(t)
	ctx := context.Background()
	to := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	from := to.AddDate(0, 0, -30)

	// Rows spread evenly over the 30 days, with the spread of keys, models,
	// sessions, durations, usage and cost of real traffic. About 3% fail and
	// 2% have no price.
	seed := time.Now()
	if _, err := conn.Exec(ctx, `
		INSERT INTO calls (id, ts, key_name, client, session_key, family, endpoint, auth_mode, provider, model,
			status, error_class, streamed, duration_ms, ttft_ms, ended_at, compaction, context_compacted,
			input_uncached, cache_read, cache_write_5m, cache_write_1h, output, reasoning,
			usage_raw, usage_estimated, extraction_partial, cost_usd)
		SELECT gen_random_uuid(), $2::timestamptz + (($3::timestamptz - $2::timestamptz) / $1::int) * i,
			'key' || (i % 8), 'claude-code', 'session-' || (i % 4000), 'anthropic', '/v1/messages', 'proxy_key', 'anthropic',
			(ARRAY['claude-opus-5-5','claude-sonnet-5-5','claude-haiku-5-5'])[1 + i % 3],
			CASE WHEN i % 33 = 0 THEN 529 ELSE 200 END, CASE WHEN i % 33 = 0 THEN 'overloaded' END,
			true, (200 + (i * 7919) % 30000)::int, 300, $2::timestamptz, false, false,
			(i * 31) % 2000, (i * 17) % 90000, (i * 13) % 4000, (i * 5) % 1000, (i * 11) % 3000, (i * 3) % 500,
			'{"input_tokens":1}', i % 50 = 0, false,
			CASE WHEN i % 50 = 1 THEN NULL ELSE ((i * 37) % 500000)::numeric / 1000000 END
		FROM generate_series(0::bigint, $1::bigint - 1) AS i`, n, from, to); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx, `VACUUM ANALYZE calls`); err != nil {
		t.Fatal(err)
	}
	var size, tableSize string
	if err := conn.QueryRow(ctx, `SELECT pg_size_pretty(pg_total_relation_size('calls')), pg_size_pretty(pg_relation_size('calls'))`).Scan(&size, &tableSize); err != nil {
		t.Fatal(err)
	}
	t.Logf("seeded %d rows in %v; calls is %s with indexes, %s heap", n, time.Since(seed).Round(time.Second), size, tableSize)

	for _, c := range []struct {
		group core.UsageGroup
		grain core.Grain
	}{
		{core.GroupNone, core.GrainDay},
		{core.GroupNone, core.GrainHour},
		{core.GroupKey, core.GrainDay},
		{core.GroupModel, core.GrainDay},
		{core.GroupSession, core.GrainDay},
	} {
		q := core.UsageQuery{Filter: core.CallFilter{From: from, To: to}, GroupBy: c.group, Grain: c.grain}
		var runs []time.Duration
		buckets := 0
		for range 5 {
			start := time.Now()
			got, err := s.Usage(ctx, q)
			if err != nil {
				t.Fatal(err)
			}
			runs = append(runs, time.Since(start))
			buckets = len(got)
		}
		slices.Sort(runs)
		t.Logf("group=%-11q grain=%-6s buckets=%-6d min %v median %v max %v",
			c.group, c.grain, buckets, runs[0].Round(time.Millisecond), runs[2].Round(time.Millisecond), runs[4].Round(time.Millisecond))
	}
}
