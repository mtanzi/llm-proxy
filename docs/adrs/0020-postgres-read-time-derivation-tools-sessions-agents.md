# ADR 0020: Postgres read-time derivation: tool pairing, sessions and agents

Status: Accepted

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

## Depends on

- ADR 0019.

## References

- architecture/backend.md, "Tool calls, sessions and agents at read time", "Contract test suite".
- architecture/frontend.md, "V2 Session view", "V4 Tool-call explorer".
