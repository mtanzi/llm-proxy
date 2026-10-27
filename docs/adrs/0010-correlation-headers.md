# ADR 0010: Correlation headers

Status: Completed

Date: 2026-10-21

## Context

Sessions, agent trees and per-prompt cost depend on correlation fields. Claude Code sends session and agent ids on every request and sends more (hint headers) behind a custom base URL only when `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`. The proxy never parses the body for correlation.

## Decision

One table-driven function reads headers into the call record:

- `session_key` from `x-claude-code-session-id`. A call without that header has no session: `session_key` is null. `agent_id` and `parent_agent_id` from `x-claude-code-agent-id` and `x-claude-code-parent-agent-id`.
- Hint headers: `agent_type`, `prompt_id`, `request_class` (`main`, `subagent`, `workflow`, `compaction`, `auxiliary`), `compaction`, `context_compacted` (both booleans; the UI compaction marker uses `context_compacted`), and `prev_tool_durations` (raw value).
- `trace_id` from W3C `traceparent`.
- `client` is the `User-Agent` normalised to its product token (`claude-cli/2.1.290` becomes `claude-cli`); `client_raw` keeps the raw value.
- Correlation headers are forwarded upstream unchanged.
- There is no session derivation in v1. The extractor hashes no tools, system prompt or first user message.

Crewmates set the hint-header variable (Accepted ADR 0042, D5). This ADR parses the hint headers whenever they are present.

## Consequences

- A call without a session header shows in the call log and in call detail but in no session.
- Session headers of other harnesses (Codex, OpenCode) are v2.
- The sub-agent to `Task` call link is not inferred in v1.

## Work required

- Implement the header table and the extraction function in `proxy`, with unknown or malformed values stored as null rather than failing the request.
- Implement `User-Agent` normalisation with table tests (including odd and empty values).
- Test that correlation never reads or logs credentials, and that a request without `x-claude-code-session-id` gets a null `session_key`.
- Store a Claude Code header set with and without hint headers as a test fixture (ADR 0038). The fixture is written from the header names in Anthropic's gateway compatibility guide. The real Claude Code capture, with and without hint headers, is done under ADR 0040's end-to-end verification and replaces this fixture under ADR 0038.

## Depends on

- ADR 0003, ADR 0006.

## References

- architecture/backend.md, "Correlation".
- architecture/upstream-connection.md, "Header handling".
- architecture/README.md, D5 and C8.
