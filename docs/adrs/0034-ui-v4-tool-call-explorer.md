# ADR 0034: UI view V4: tool-call explorer

Status: Completed

Date: 2026-10-21

## Context

A proxy sees tool requests and results as metadata only. The explorer shows which tools run, how often, how long and how often they fail, without argument or result text.

## Decision

- Route `/ui/tools`.
- **Summary table** per tool: count, error rate, p50 and p95 duration, total time, split by agent type.
- **List:** time, session and agent, tool name, argument size, result size, success or error, duration with its source (header or estimated).
- Filters: name, error, duration, sizes, session and unmatched halves (`use_only`, `result_only`) as their own filter.
- Argument and result content is not shown (decision D11, Accepted ADR 0043).

## Consequences

- At very large volumes the join gets slower; a materialised pairing table is a later item.
- MCP tools (`source = mcp`) arrive with the MCP gateway.

## Work required

- Implement the summary and list queries with the shared table and filter primitives.
- Add the duration-source indicator and the unmatched filter.
- Link rows to V3 and V2.
- Tests: summary sorting, filter round trips, unmatched states, empty state.

## Depends on

- ADR 0024, ADR 0030.

## References

- architecture/frontend.md, "Views" (V4), "V4 Tool-call explorer".
- architecture/backend.md, "Tool calls, sessions and agents at read time".
