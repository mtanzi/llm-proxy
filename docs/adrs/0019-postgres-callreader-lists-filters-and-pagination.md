# ADR 0019: Postgres `CallReader`: lists, filters, pagination and call detail

Status: Accepted

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

## Depends on

- ADR 0017, ADR 0018.

## References

- architecture/backend.md, "Admin and query API" (List conventions), "Ports", "Contract test suite".
- architecture/frontend.md, "V1 Call log", "V3 Call detail".
