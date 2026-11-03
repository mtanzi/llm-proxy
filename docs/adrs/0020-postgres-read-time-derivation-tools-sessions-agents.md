# ADR 0020: Postgres read-time derivation: tool pairing, sessions and agents

Status: Completed

Date: 2026-10-21

## Context

A proxy never sees a tool run. It sees the model request the tool in response N and the client return the result in request N+1, linked by the tool-call id. v1 stores each half as its own append-only row and pairs them when read. Sessions and agent trees are derived from `calls` too. Every write is a plain insert, so retries, out-of-order arrival and several writers cannot corrupt anything.

## Decision

- **Pairing:** `CallReader` joins a `use` row to the `result` row with the same `tool_call_id`. The lookup for results reaches up to 24 hours past the query's `To` bound. Provider ids (`toolu_…`) are unique on their own, so the join does not need the session key. An optional `tool_calls` view captures the join in one place.
- **Duration:** the `prev_tool_durations` value on the resolving call when present (`duration_source = header`), otherwise the gap between the use row and the result row (`duration_source = estimated`). Parallel tool calls each get their own reported time.
- **Unmatched halves** are derived: a use with no result after 1 hour is `use_only`; a result with no use is `result_only`. Nothing sets these at write time, and there is no idle-close job.
- **Sessions:** `GROUP BY session_key` over `calls` in the time range. Calls with a null `session_key` belong to no session. `ListSessions` and `GetSession` return summaries, prompts, agents and call metadata.
- **Agent tree:** the distinct `(agent_id, parent_agent_id, agent_type)` set of a session. A sub-agent nests under its parent agent; the link to the spawning `Task` call is deferred.
- **Tool views:** `ListToolCalls` (also filtered by `?session=`) and `ToolSummary` (per tool: count, error rate, p50 and p95 duration, total time, split by agent type).
- Server-side tools appear complete inside one response and carry `source = server`.

## Consequences

- At very large volumes the tool explorer slows down; a materialised pairing table is a later item (trigger: tool explorer p95 above about 1 s).
- There are no `sessions` or `agents` tables to keep consistent.

## Work required

- Implement the pairing query (or view) and `ListToolCalls`, `ToolSummary` with filters on name, error, duration, sizes, unmatched halves and session.
- Implement `ListSessions` (with the session filter) and `GetSession`, including prompts and the agent tree.
- Define the two constants as documented constants, not settings: a use with no result after 1 hour reads as `use_only`, and pairing looks up to 24 hours past the query's `To` bound.
- Add contract cases (ADR 0018): a use then its result resolves, a result before its use resolves once the use arrives, a use with no result after 1 hour reads as `use_only`, a result 23 hours past `To` still pairs, a result with no use reads as `result_only`, header versus estimated duration, parallel tools, sessions group correctly, the agent tree matches the call rows.
- Check query plans and indexes on a generated dataset.

## As built

- The pairing lives in the adapter as a query, not a view: the 1 hour and 24 hour constants are `core.ToolIdleAfter` and `core.ToolResultLookahead`, and a view cannot hold a bound that depends on the query. There is no migration.
- A use with no result reads `pending` until `core.ToolIdleAfter` has passed, then `use_only`. A result with no use anywhere reads `result_only` and sits at the result's time. When several results carry one id, the earliest pairs, and the rest are ignored.
- `prev_tool_durations` is read as a JSON object of tool name to milliseconds. Any other value, a number below 0 or above one year, and any server-side tool fall back to the estimated gap.
- `ToolStat` has one row per tool and agent type (`AgentType`, `TotalDuration` added; `ErrorRate()` is derived). `ToolCallFilter` gained `MinArgBytes` and `MinResultBytes`.
- One call can carry several tool halves, so `core.Cursor` gained `Idx`. Tool calls order by `(ts, call id, idx)` of the listed half. Sessions order by their newest call in the range, and the cursor holds that call's `(ts, id)`.
- A session is listed when one of its calls in the range satisfies the call fields; its summary counts all its calls in the range. `GetSession` reads all calls of the key in one snapshot: calls newest first, prompts newest first, agents in order of first call. A call without `agent_id` is not an agent node.

## Plan check

Checked by hand with `EXPLAIN (ANALYZE, BUFFERS)` on Postgres 16 against a generated, `ANALYZE`d dataset: 5,000,000 calls over 30 days, with 1,666,667 use and 1,583,333 result `tool_events` (95% of uses answered by the next call, half of those with a `prev_tool_durations` value), about 100,000 sessions. The generator is not in the repository. No new index or migration was needed.

- A first plan joined every use of the range before the sort: 1.6 s for a day and 7.1 s for a month. The query now cuts the use branch and the result-only branch to the page size before the joins when the filter needs no pairing. A use page then reads `tool_events_ts_idx` under an Incremental Sort, probes `tool_events_tool_call_id_idx` and `calls_pkey` per row, and takes about 3 ms.
- `ListToolCalls`, no pairing filter, first page: 0.2-0.4 s for a day and 0.4 s for a month. Nearly all of it is the result-only branch: it must check every result in the range for a use, and the planner hash-anti-joins a sequential scan of the uses. This cost grows with the range.
- Name, argument size and session filters: 1-56 ms for a day, 0.2 s for a month with a session.
- Filters known only after pairing (status, error, duration, result size) read the whole range: 0.4-0.8 s for a day, 2.0 s for `IsError` over a month.
- `ToolSummary`: 1.3 s for a day, 5.6 s for a week, 6.8 s for a month. It pairs every tool call of the range.
- `ListSessions`: 0.24 s for a day, 1.5 s for a week, 2.3 s for a month, one pass over `calls` with `GROUP BY session_key`. `GetSession` reads one session by `calls_session_key_ts_idx` in under 1 ms.

The slow cases are the ones the Consequences section names: a long range in the tool explorer. The trigger for a materialised pairing table stays tool explorer p95 above about 1 s.

## Depends on

- ADR 0019.

## References

- architecture/backend.md, "Tool calls, sessions and agents at read time", "Contract test suite".
- architecture/frontend.md, "V2 Session view", "V4 Tool-call explorer".
