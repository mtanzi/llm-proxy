package storetest

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// usageWin covers the usage dataset: three UTC days from the start of Base's day.
var usageWin = core.CallFilter{From: day0, To: day0.Add(72 * time.Hour)}

var day0 = time.Date(Base.Year(), Base.Month(), Base.Day(), 0, 0, 0, 0, time.UTC)

var usageCases = []testCase{
	{"usage_sums_match_for_every_group_by_and_grain", func(t *testing.T, e *env) {
		calls := usageDataset(e.gen)
		e.write(t, calls...)
		for _, grp := range []core.UsageGroup{
			core.GroupNone, core.GroupKey, core.GroupModel, core.GroupClient,
			core.GroupProvider, core.GroupSession, core.GroupErrorClass,
		} {
			for _, grain := range []core.Grain{core.GrainMinute, core.GrainHour, core.GrainDay} {
				t.Run(fmt.Sprintf("%s_%s", grp, grain), func(t *testing.T) {
					got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, GroupBy: grp, Grain: grain})
					if err != nil {
						t.Fatal(err)
					}
					want := expectedBuckets(calls, grp, grain)
					if len(want) < 2 {
						t.Fatalf("dataset gives %d buckets: it does not exercise the case", len(want))
					}
					if len(got) != len(want) {
						t.Fatalf("%d buckets, want %d\n got %+v\nwant %+v", len(got), len(want), got, want)
					}
					for i := range want {
						if !sameBucket(got[i], want[i]) {
							t.Errorf("bucket %d\n got %+v\nwant %+v", i, got[i], want[i])
						}
					}
				})
			}
		}
	}},
	{"usage_buckets_are_ordered_by_start_then_group", func(t *testing.T, e *env) {
		e.write(t, usageDataset(e.gen)...)
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, GroupBy: core.GroupKey, Grain: core.GrainHour})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.IsSortedFunc(got, func(a, b core.UsageBucket) int {
			if c := a.Start.Compare(b.Start); c != 0 {
				return c
			}
			return compareStr(a.Group, b.Group)
		}) {
			t.Fatalf("buckets out of order: %+v", got)
		}
	}},
	{"usage_unknown_cost_and_usage_are_counted_not_zeroed", func(t *testing.T, e *env) {
		g := e.gen
		ts := Base
		u := core.Usage{InputUncached: 100, CacheRead: 300, Output: 50}
		e.write(t,
			g.Call(g.At(ts), WithUsage(u), WithCost(2_000)),
			g.Call(g.At(ts), WithUsage(u), WithoutCost()),
			g.Call(g.At(ts), WithUsage(u), WithCost(0), WithEstimatedUsage()),
			g.Call(g.At(ts), WithUsage(core.Usage{}), WithoutUsageRaw(), WithoutCost(), WithError(499, "client_abort")),
		)
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, Grain: core.GrainDay})
		if err != nil || len(got) != 1 {
			t.Fatalf("buckets %+v, err %v", got, err)
		}
		b := got[0]
		if b.Calls != 4 || b.Errors != 1 {
			t.Errorf("calls %d errors %d, want 4 and 1", b.Calls, b.Errors)
		}
		// A known zero cost counts as priced; an unknown cost does not.
		if b.Cost != 2_000 || b.UnpricedCalls != 2 {
			t.Errorf("cost %d unpriced %d, want 2000 and 2", b.Cost, b.UnpricedCalls)
		}
		if b.EstimatedCalls != 1 || b.NoUsageCalls != 1 {
			t.Errorf("estimated %d no-usage %d, want 1 and 1", b.EstimatedCalls, b.NoUsageCalls)
		}
		if ratio, ok := b.CacheHitRatio(); !ok || math.Abs(ratio-0.75) > 1e-12 {
			t.Errorf("cache hit ratio %v ok %v, want 0.75", ratio, ok)
		}
	}},
	{"usage_cache_hit_ratio_is_unknown_without_input", func(t *testing.T, e *env) {
		e.write(t, e.gen.Call(WithUsage(core.Usage{Output: 10})))
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, Grain: core.GrainDay})
		if err != nil || len(got) != 1 {
			t.Fatalf("buckets %+v, err %v", got, err)
		}
		if _, ok := got[0].CacheHitRatio(); ok {
			t.Error("ratio is known for a bucket with no input tokens")
		}
	}},
	{"usage_percentiles_equal_the_exact_value", func(t *testing.T, e *env) {
		g := e.gen
		// Durations 1..100 ms in one bucket: percentile_cont(p) is 1 + 99p.
		var calls []core.CallRecord
		for i := 1; i <= 100; i++ {
			calls = append(calls, g.Call(g.At(Base.Add(time.Duration(i)*time.Second)), WithDuration(time.Duration(i)*time.Millisecond)))
		}
		// Calls with no duration are left out; a second bucket holds only such calls.
		calls = append(calls, g.Call(g.At(Base.Add(2*time.Hour)), WithDuration(0)))
		e.write(t, calls...)
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, Grain: core.GrainHour})
		if err != nil || len(got) != 2 {
			t.Fatalf("buckets %+v, err %v", got, err)
		}
		ms := func(v float64) time.Duration { return time.Duration(math.Round(v * 1e6)) }
		if p := got[0]; p.LatencyP50 != ms(50.5) || p.LatencyP95 != ms(95.05) || p.LatencyP99 != ms(99.01) {
			t.Errorf("p50 %v p95 %v p99 %v, want 50.5ms 95.05ms 99.01ms", p.LatencyP50, p.LatencyP95, p.LatencyP99)
		}
		if p := got[1]; p.Calls != 1 || p.LatencyP50 != 0 || p.LatencyP95 != 0 || p.LatencyP99 != 0 {
			t.Errorf("bucket with no durations: %+v, want zero latency", p)
		}
	}},
	{"usage_empty_range_and_gaps_return_no_buckets", func(t *testing.T, e *env) {
		g := e.gen
		ctx := context.Background()
		q := core.UsageQuery{Filter: usageWin, Grain: core.GrainHour}
		got, err := e.Usage(ctx, q)
		if err != nil || len(got) != 0 {
			t.Fatalf("empty store: %+v, err %v", got, err)
		}
		e.write(t, g.Call(g.At(day0.Add(time.Hour))), g.Call(g.At(day0.Add(5*time.Hour+59*time.Minute))))
		got, err = e.Usage(ctx, q)
		if err != nil || len(got) != 2 {
			t.Fatalf("two calls five hours apart: %+v, err %v", got, err)
		}
		if got[0].Start != day0.Add(time.Hour) || got[1].Start != day0.Add(5*time.Hour) {
			t.Errorf("starts %v and %v, want the hours of the calls and nothing between", got[0].Start, got[1].Start)
		}
		q.Filter = core.CallFilter{From: day0.Add(10 * time.Hour), To: day0.Add(11 * time.Hour)}
		if got, err = e.Usage(ctx, q); err != nil || len(got) != 0 {
			t.Fatalf("range with no calls: %+v, err %v", got, err)
		}
	}},
	{"usage_buckets_align_in_utc_for_any_input_zone", func(t *testing.T, e *env) {
		g := e.gen
		tokyo := time.FixedZone("JST", 9*3600)
		// 23:30 UTC on day 0 and 00:30 UTC on day 1, both written in zone +9.
		before := day0.Add(23*time.Hour + 30*time.Minute)
		after := day0.Add(24*time.Hour + 30*time.Minute)
		e.write(t, g.Call(g.At(before.In(tokyo))), g.Call(g.At(after.In(tokyo))))
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: usageWin, Grain: core.GrainDay})
		if err != nil || len(got) != 2 {
			t.Fatalf("buckets %+v, err %v", got, err)
		}
		for i, b := range got {
			if want := day0.Add(time.Duration(i) * 24 * time.Hour); !b.Start.Equal(want) || b.Start.Location() != time.UTC {
				t.Errorf("bucket %d starts %v, want %v in UTC", i, b.Start, want)
			}
		}
	}},
	{"usage_applies_the_call_filter", func(t *testing.T, e *env) {
		g := e.gen
		e.write(t, g.Call(WithKey("alice")), g.Call(WithKey("bob")), g.Call(WithKey("bob")))
		f := usageWin
		f.Keys = []string{"bob"}
		got, err := e.Usage(context.Background(), core.UsageQuery{Filter: f, Grain: core.GrainDay})
		if err != nil || len(got) != 1 || got[0].Calls != 2 {
			t.Fatalf("buckets %+v, err %v", got, err)
		}
	}},
	{"usage_rejects_a_bad_query", func(t *testing.T, e *env) {
		for _, q := range []core.UsageQuery{
			{Filter: core.CallFilter{}, Grain: core.GrainDay},
			{Filter: usageWin, Grain: ""},
			{Filter: usageWin, Grain: "week"},
			{Filter: usageWin, Grain: core.GrainDay, GroupBy: "key_name; DROP TABLE calls"},
		} {
			if _, err := e.Usage(context.Background(), q); !errors.Is(err, core.ErrInvalidFilter) {
				t.Errorf("Usage(%+v) err = %v", q, err)
			}
		}
	}},
}

// usageDataset returns calls over three UTC days that differ in every group
// dimension, grain edge, usage column, cost, error and unknown value.
func usageDataset(g *Gen) []core.CallRecord {
	type spec struct {
		at                      time.Duration
		key, model, client      string
		provider, session, errc string
		usage                   core.Usage
		cost                    *core.MicroUSD
		est, noRaw              bool
	}
	m := func(v core.MicroUSD) *core.MicroUSD { return &v }
	u := func(in, cr, w5, w1, out, reas int) core.Usage {
		return core.Usage{InputUncached: in, CacheRead: cr, CacheWrite5m: w5, CacheWrite1h: w1, Output: out, Reasoning: reas}
	}
	specs := []spec{
		{at: 9 * time.Hour, key: "alice", model: "opus", client: "claude-code", provider: "anthropic", session: "s1", usage: u(10, 20, 30, 40, 50, 5), cost: m(1_000)},
		{at: 9*time.Hour + 30*time.Second, key: "alice", model: "opus", client: "claude-code", provider: "anthropic", session: "s1", usage: u(1, 2, 3, 4, 5, 1), cost: m(250)},
		{at: 9*time.Hour + 59*time.Minute, key: "bob", model: "sonnet", client: "claude-code", provider: "anthropic", session: "s2", usage: u(100, 0, 0, 0, 500, 0), cost: m(7_777)},
		{at: 10 * time.Hour, key: "bob", model: "sonnet", provider: "anthropic", usage: u(7, 7, 7, 7, 7, 7), est: true},
		{at: 10*time.Hour + time.Minute, key: "carol", model: "", client: "sdk", provider: "bedrock", session: "s1", errc: "overloaded", usage: u(0, 0, 0, 0, 0, 0), noRaw: true},
		{at: 23*time.Hour + 59*time.Minute, key: "alice", model: "haiku", client: "sdk", provider: "bedrock", usage: u(11, 22, 33, 44, 55, 6), cost: m(31)},
		{at: 24 * time.Hour, key: "alice", model: "opus", client: "claude-code", provider: "anthropic", session: "s3", errc: "rate_limit", usage: u(5, 5, 5, 5, 5, 0), cost: m(0)},
		{at: 24*time.Hour + 90*time.Minute, key: "bob", model: "opus", client: "sdk", provider: "anthropic", session: "s3", usage: u(3, 9, 0, 0, 12, 2), cost: m(42)},
		{at: 47*time.Hour + 59*time.Minute, key: "carol", model: "haiku", client: "claude-code", provider: "bedrock", session: "s2", errc: "overloaded", usage: u(8, 16, 24, 32, 40, 4), cost: m(99)},
		{at: 48 * time.Hour, key: "carol", model: "sonnet", provider: "bedrock", usage: u(9, 18, 27, 36, 45, 3), cost: m(500_000)},
	}
	var out []core.CallRecord
	for i, s := range specs {
		opts := []CallOpt{
			g.At(day0.Add(s.at)), WithKey(s.key), WithModel(s.model), WithClient(s.client), WithProvider(s.provider),
			WithSession(s.session), WithUsage(s.usage), WithDuration(time.Duration(100*(i+1)) * time.Millisecond),
		}
		if s.cost != nil {
			opts = append(opts, WithCost(*s.cost))
		} else {
			opts = append(opts, WithoutCost())
		}
		if s.errc != "" {
			opts = append(opts, WithError(529, s.errc))
		}
		if s.est {
			opts = append(opts, WithEstimatedUsage())
		}
		if s.noRaw {
			opts = append(opts, WithoutUsageRaw())
		}
		out = append(out, g.Call(opts...))
	}
	return out
}

// expectedBuckets aggregates calls the slow way, in Go.
func expectedBuckets(calls []core.CallRecord, grp core.UsageGroup, grain core.Grain) []core.UsageBucket {
	width := map[core.Grain]time.Duration{core.GrainMinute: time.Minute, core.GrainHour: time.Hour, core.GrainDay: 24 * time.Hour}[grain]
	type k struct {
		start time.Time
		group string
	}
	by := map[k]*core.UsageBucket{}
	for _, c := range calls {
		var group string
		switch grp {
		case core.GroupKey:
			group = c.KeyName
		case core.GroupModel:
			group = c.Model
		case core.GroupClient:
			group = c.Client
		case core.GroupProvider:
			group = c.Provider
		case core.GroupSession:
			group = c.SessionKey
		case core.GroupErrorClass:
			group = c.ErrorClass
		}
		key := k{c.TS.UTC().Truncate(width), group}
		b := by[key]
		if b == nil {
			b = &core.UsageBucket{Start: key.start, Group: group}
			by[key] = b
		}
		b.Calls++
		if c.ErrorClass != "" {
			b.Errors++
		}
		b.Usage.InputUncached += c.Usage.InputUncached
		b.Usage.CacheRead += c.Usage.CacheRead
		b.Usage.CacheWrite5m += c.Usage.CacheWrite5m
		b.Usage.CacheWrite1h += c.Usage.CacheWrite1h
		b.Usage.Output += c.Usage.Output
		b.Usage.Reasoning += c.Usage.Reasoning
		if c.Cost == nil {
			b.UnpricedCalls++
		} else {
			b.Cost += *c.Cost
		}
		if c.UsageEstimated {
			b.EstimatedCalls++
		}
		if len(c.UsageRaw) == 0 {
			b.NoUsageCalls++
		}
	}
	out := make([]core.UsageBucket, 0, len(by))
	for _, b := range by {
		out = append(out, *b)
	}
	slices.SortFunc(out, func(a, b core.UsageBucket) int {
		if c := a.Start.Compare(b.Start); c != 0 {
			return c
		}
		return compareStr(a.Group, b.Group)
	})
	// Latency is checked on its own: one case compares exact percentiles.
	return out
}

// sameBucket compares everything except latency.
func sameBucket(a, b core.UsageBucket) bool {
	a.LatencyP50, a.LatencyP95, a.LatencyP99 = 0, 0, 0
	b.LatencyP50, b.LatencyP95, b.LatencyP99 = 0, 0, 0
	return a == b
}

func compareStr(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
