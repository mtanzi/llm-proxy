# ADR 0031: UI view V1: call log and live toggle

Status: Accepted

Date: 2026-10-21

## Context

The call log is the home view: a paginated, filterable list of every LLM call with a volume chart and a summary strip. A Live toggle prepends new calls from the SSE stream.

## Decision

- Route `/ui/calls`. Columns: time, session and prompt, agent, client, model, status, latency, tokens in and out, cost, number of tool calls requested.
- A row click opens V3 (ADR 0032); a session cell opens V2 (ADR 0033).
- The filter menu covers every logged criterion: time, session, prompt, agent, agent type, request class, client, key name, provider, model, endpoint, status, error class, streamed, latency, TTFT, tokens, cost, cache hit, tool name, tool error. No text filter.
- "Group by session" folds consecutive calls of a session into one row with "N turns"; kept in the URL.
- Counts are exact up to 10,000 and show "10,000+" above.
- **Live toggle:** opens an `EventSource` on `/admin/stream` with the same filter parameters. `call.started` inserts an in-flight row (spinner, elapsed time); `call.finished` replaces it. At most 500 live rows are kept. On reconnect the UI refetches the first page and de-duplicates live rows by call id.
- Volume chart and summary strip come from `GET /admin/usage`.

## Consequences

- There is no "newer" button; the user reloads page one.
- In-flight calls appear only in the live tail.

## Work required

- Implement the route, table, filter menu, grouping toggle, volume chart and summary strip.
- Implement the live hook: `EventSource` lifecycle, event merge, row cap, reconnect refetch and de-duplication.
- Keep all filters, the grouping and the live flag in the URL search parameters.
- Tests: filter-to-URL round trip, group by session, live merge and cap, reconnect de-duplication, virtualised scrolling with 5,000 rows, empty and error states.

## Depends on

- ADR 0024, ADR 0025, ADR 0030.

## References

- architecture/frontend.md, "Views" (V1), "V1 Call log", "Live tail".
