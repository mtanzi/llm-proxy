# ADR 0010: Correlation headers and session derivation

Status: Accepted

Date: 2026-10-21

## Context

Sessions, agent trees and per-prompt cost depend on correlation fields. Claude Code sends session and agent ids on every request and sends more (hint headers) behind a custom base URL only when `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`. The proxy never parses the body for correlation.

## Decision

One table-driven function reads headers into the call record:

- `session_key` from `x-claude-code-session-id`; `agent_id` and `parent_agent_id` from `x-claude-code-agent-id` and `x-claude-code-parent-agent-id`.
- Hint headers: `agent_type`, `prompt_id`, `request_class` (`main`, `subagent`, `workflow`, `compaction`, `auxiliary`), `compaction`, `context_compacted`, and `prev_tool_durations` (raw value).
- `trace_id` from W3C `traceparent`.
- `client` is the `User-Agent` normalised to its product token (`claude-cli/2.1.290` becomes `claude-cli`); `client_raw` keeps the raw value.
- Correlation headers are forwarded upstream unchanged.
- **Session derivation** is the fallback when no session header exists: a pure function in `core` over (key, client, hash of tools and system prompt, hash of the first user message). It keeps no idle state and no timeout, and gives the same result on every replica. Only hashes are used.

Whether to ask crewmates to set the hint-header variable is Proposed ADR 0042 (D5). This ADR parses the hint headers whenever they are present.

## Consequences

- Header-less clients with identical openings merge into one session. That is the accepted cost.
- Session headers of other harnesses (Codex, OpenCode) are v2.
- The sub-agent to `Task` call link is not inferred in v1.

## Work required

- Implement the header table and the extraction function in `proxy`, with unknown or malformed values stored as null rather than failing the request.
- Implement `User-Agent` normalisation with table tests (including odd and empty values).
- Implement the pure `SessionKey` derivation in `core`. The extractor supplies the two body hashes (ADR 0014); the hashing happens on the in-memory request bytes and the bytes are not kept.
- Test that correlation never reads or logs credentials and that the same inputs always give the same derived key.
- Capture a real Claude Code request with and without hint headers and store the header set as a test fixture (ADR 0038).

## Depends on

- ADR 0003, ADR 0006.

## References

- architecture/backend.md, "Correlation".
- architecture/upstream-connection.md, "Header handling".
- architecture/README.md, D5.
