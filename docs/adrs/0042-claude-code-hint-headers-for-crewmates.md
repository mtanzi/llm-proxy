# ADR 0042: Claude Code hint headers for firstmate crewmates (D5)

Status: Proposed

Date: 2026-10-21

## Context

Behind a custom base URL, Claude Code sends the session and agent ids always, but sends request class, agent type, prompt id, tool run times and compaction markers only when `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` is set. Without them the session tree has no agent types or prompt grouping, and tool durations are only estimated from the gap between halves.

## Decision

Open (D5). The documents use the recommended option.

- **Recommended:** set `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` for firstmate crewmates, so the proxy receives request class, agent type, prompt id and tool run times.
- **Alternative:** leave the hint headers off and derive less: no agent type, no prompt id, estimated tool durations, no compaction markers.

The proxy parses the hint headers whenever present (ADR 0010), so the decision changes client configuration only.

## Consequences

- With the hint headers: richer sessions, header-sourced tool durations, compaction markers.
- Without them: V2 and V4 show less, and per-prompt cost is not available.

## Work required

- Confirm by a capture test (ADR 0038) that the hint headers arrive with the pinned Claude Code version and record the header names.
- Set the variable in the documented client settings (ADR 0012, ADR 0037) and in the firstmate dispatch environment (ADR 0041).
- Update this ADR to Accepted with the outcome, and remove the pending note from the architecture documents.

## Depends on

- ADR 0010, ADR 0040.

## References

- architecture/README.md, "Open decisions" (D5).
- architecture/backend.md, "Correlation".
- architecture/upstream-connection.md, "Claude Code with a proxy-held API key".
