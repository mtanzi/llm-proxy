# ADR 0021: Postgres usage aggregation

Status: Accepted

Date: 2026-10-21

## Context

The dashboard and the cost view need time series of tokens, cost and latency grouped by key, model, client or session. At 12,000 to 50,000 calls per day, plain aggregation over raw rows is fast enough. Rollup tables are deferred until usage p95 passes 1 s or rows pass about 20 million.

## Decision

- `Usage(ctx, UsageQuery)` returns `UsageBucket` values computed from raw `calls` rows.
- `UsageQuery` has a time range, `GroupBy` (key, model, client, provider, session, error class) and `Grain` (minute, hour, day).
- Buckets sum the disjoint token columns and cost, count calls and errors, and report latency percentiles with `percentile_cont` over raw rows. The cache hit ratio is derived from `cache_read` over the input columns.
- Per-key usage is `GROUP BY key_name`.
- Long ranges (months) can take seconds; this is accepted in v1.

## Consequences

- No rollup table, no rollup job, no sentinel dimensions, no planner-estimate counts.
- The same port can later be backed by rollups without changing consumers.

## Work required

- Implement `Usage` with `date_bin` or `date_trunc` for grains and a safe whitelist for group-by columns.
- Implement the percentile and ratio calculations and make null cost, null usage and `usage_estimated` rows visible, not counted as zero.
- Add contract cases (ADR 0018): sums of disjoint columns and cost for each `GroupBy` and `Grain`, percentiles equal to the exact value on a known dataset, empty buckets, time zone handling (UTC buckets).
- Measure a 30-day aggregate over about 1.5 million generated rows and record the result in the sizing notes (ADR 0039).

## Depends on

- ADR 0019.

## References

- architecture/backend.md, "Ports", "Adapters" (Percentiles), "Later versions" (Storage and scale).
- architecture/deployment.md, "Sizing".
- architecture/frontend.md, "V5 Cost and cache".
