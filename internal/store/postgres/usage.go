package postgres

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// groupColumns is the whitelist of group-by columns. Only these strings are
// ever put in the SQL text. A NULL group value reads as the empty string, which
// is how the core types say "not known".
var groupColumns = map[core.UsageGroup]string{
	core.GroupNone:       `''`,
	core.GroupKey:        `c.key_name`,
	core.GroupModel:      `coalesce(c.model, '')`,
	core.GroupClient:     `coalesce(c.client, '')`,
	core.GroupProvider:   `c.provider`,
	core.GroupSession:    `coalesce(c.session_key, '')`,
	core.GroupErrorClass: `coalesce(c.error_class, '')`,
}

// grainUnits maps a grain to its date_trunc unit.
var grainUnits = map[core.Grain]string{
	core.GrainMinute: "minute",
	core.GrainHour:   "hour",
	core.GrainDay:    "day",
}

// Usage aggregates the calls matching q.Filter into buckets of q.Grain, one
// per group value, oldest first and by group inside a bucket. Buckets are
// aligned in UTC whatever the session time zone is. A range with no calls
// returns no buckets.
//
// Cost sums only the calls with a known cost; the Unpriced, Estimated and
// NoUsage counters report the calls that are not fully known. Latency
// percentiles use percentile_cont over the raw duration_ms values, and a call
// with no duration is left out.
func (s *Store) Usage(ctx context.Context, q core.UsageQuery) ([]core.UsageBucket, error) {
	if err := q.Validate(); err != nil {
		return nil, err
	}
	group, unit := groupColumns[q.GroupBy], grainUnits[q.Grain]
	if group == "" || unit == "" {
		return nil, core.ErrInvalidFilter
	}
	w := buildWhere(q.Filter)
	sql := `SELECT date_trunc('` + unit + `', c.ts, 'UTC') AS bucket, ` + group + ` AS grp,
		count(*),
		count(*) FILTER (WHERE c.error_class IS NOT NULL),
		sum(c.input_uncached)::bigint, sum(c.cache_read)::bigint,
		sum(c.cache_write_5m)::bigint, sum(c.cache_write_1h)::bigint,
		sum(c.output)::bigint, sum(c.reasoning)::bigint,
		coalesce(sum(c.cost_usd * 1000000), 0)::bigint,
		count(*) FILTER (WHERE c.cost_usd IS NULL),
		count(*) FILTER (WHERE c.usage_estimated),
		count(*) FILTER (WHERE c.usage_raw IS NULL),
		percentile_cont(ARRAY[0.5, 0.95, 0.99]) WITHIN GROUP (ORDER BY c.duration_ms)
	FROM calls c WHERE ` + w.sql() + ` GROUP BY 1, 2 ORDER BY 1, 2`
	rows, err := s.pool.Query(ctx, sql, w.args...)
	if err != nil {
		return nil, fmt.Errorf("postgres: usage: %w", err)
	}
	out, err := pgx.CollectRows(rows, scanBucket)
	if err != nil {
		return nil, fmt.Errorf("postgres: usage: %w", err)
	}
	return out, nil
}

func scanBucket(r pgx.CollectableRow) (core.UsageBucket, error) {
	var (
		b    core.UsageBucket
		cost int64
		pct  []float64 // NULL when no call has a duration
	)
	u := &b.Usage
	err := r.Scan(&b.Start, &b.Group, &b.Calls, &b.Errors,
		&u.InputUncached, &u.CacheRead, &u.CacheWrite5m, &u.CacheWrite1h, &u.Output, &u.Reasoning,
		&cost, &b.UnpricedCalls, &b.EstimatedCalls, &b.NoUsageCalls, &pct)
	if err != nil {
		return core.UsageBucket{}, err
	}
	b.Start, b.Cost = b.Start.UTC(), core.MicroUSD(cost)
	if len(pct) == 3 {
		b.LatencyP50, b.LatencyP95, b.LatencyP99 = msFloat(pct[0]), msFloat(pct[1]), msFloat(pct[2])
	}
	return b, nil
}

// msFloat converts a fractional number of milliseconds to the nearest nanosecond.
func msFloat(v float64) time.Duration {
	return time.Duration(math.Round(v * float64(time.Millisecond)))
}
