# ADR 0019: Postgres `CallReader`: lists, filters, pagination and call detail

Status: Completed

Date: 2026-10-21

## Context

The call log (view V1) and call detail (V3) read through `CallReader`. Lists need many optional filters, stable keyset pagination, and a bounded count. Handlers never see SQL.

## Decision

- **Filter builder:** one dynamic `WHERE` builder in `store/postgres` maps `core.CallFilter` fields to SQL. Every query is time-bounded by `From` and `To` (half-open `[From, To)`).
- **Keyset pagination, forward only,** on `(ts, id)`, ordered `ts DESC, id DESC`: `WHERE (ts, id) < ($ts, $id) ORDER BY ts DESC, id DESC LIMIT $n`. The adapter clamps `Limit` to a maximum. No offset, no backward cursor.
- **Capped exact counts:** `CountCalls(ctx, f, max)` is exact up to `max` (10,001 in the API) and reports `capped` above it.
- **Filters:** `Endpoints` and `Streamed` are filter fields beside the existing ones (ADR 0048).
- **Tool filters:** `Tools` and `ToolError` match calls that contain a tool call with that name or error, through `tool_events`.
- **Detail:** `GetCall` returns the call row and its tool halves. Numbers and names only.
- Percentiles and aggregates are separate (ADR 0021).

## Consequences

- Adding a filter means a field in `core`, a branch in the builder, a `Match` case (ADR 0003) and a contract case.
- To see newer calls the UI reloads page one, and the live tail covers the rest.
- Exact counts above 10,000 are not available.

## Work required

- Implement the filter builder with parameterised SQL only (no string-built values) and tests that every `CallFilter` field produces the expected predicate.
- Implement `ListCalls`, `CountCalls` and `GetCall` and the row-to-`core` mappers.
- Check the plans of the main queries with `EXPLAIN` against a generated dataset (for example 5 million rows) and confirm the indexes from ADR 0017 are used; add an index only through a new migration.
- Add contract cases (ADR 0018): total order including equal `ts`, no duplicates or gaps while inserting, time bound, count cap, each filter, detail with tool halves.
- Keep row structs private to the adapter.

## Plan check

Checked by hand with `EXPLAIN ANALYZE` on Postgres 16 against a generated, `ANALYZE`d dataset: 5,000,000 calls over 30 days (about 167,000 a day), with 1,666,666 use and 833,333 result `tool_events`. The generator is not in the repository. No new index or migration was needed.

- Unfiltered pages (day, month, after a cursor): `calls_ts_id_idx`, 0.2-0.5 ms.
- Key, session and error filters: `calls_key_name_ts_idx`, `calls_session_key_ts_idx` and `calls_errors_ts_idx`, under an Incremental Sort where the planner picks them, 0.2-4 ms.
- Tool filters on a list: `EXISTS` probes `tool_events_pkey`, and `tool_events_tool_call_id_idx` for `ToolError`, 0.6-14 ms.
- Counts capped at 10,002 rows: 1-2 ms, by `calls_ts_id_idx` or `calls_key_name_ts_idx`, or by a sequential scan of `calls` that stops at the cap for an unfiltered month.
- A sparse `ToolError` count over one day does not use the `tool_events` indexes. It reads the whole day, with hash joins over sequential scans of `tool_events`, and takes about 0.56 s. This is the slowest query checked.
- `GetCall`: `calls_pkey` and `tool_events_pkey`, 0.03 ms each.

The check found one defect. The builder first emitted `col = ANY('{x}')` for a one-value list, and the planner then sorted about 1 million rows (4.3 s for a month with a key, 1 s for a month of errors). The builder now emits `col = $n` for one value.

These plans ran with a later-removed `r.ts` upper bound on the `ToolError` join. The bound was only a row filter on `r`, not an index condition, and `tool_events` is not partitioned, so the access paths above are expected to hold. The plans were not re-run after the removal.

## Depends on

- ADR 0017, ADR 0018.

## References

- architecture/backend.md, "Admin and query API" (List conventions), "Ports", "Contract test suite".
- architecture/frontend.md, "V1 Call log", "V3 Call detail".
