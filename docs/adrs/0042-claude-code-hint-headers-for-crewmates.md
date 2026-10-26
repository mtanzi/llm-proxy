# ADR 0042: Claude Code hint headers for firstmate crewmates (D5)

Status: Accepted

Date: 2026-10-21

## Context

Behind a custom base URL, Claude Code sends the session and agent ids always, but sends request class, agent type, prompt id, tool run times and compaction markers only when `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` is set. Without them the session tree has no agent types or prompt grouping, and tool durations are only estimated from the gap between halves.

## Decision

Accepted (D5), with the recommended option: set `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1` for firstmate crewmates, so the proxy receives request class, agent type, prompt id, compaction flags and tool run times.

Rejected option: leave the hint headers off. The session tree would have no agent types, no prompt ids, no compaction markers and only estimated tool durations.

The proxy parses the hint headers whenever present (ADR 0010), so the decision changes client configuration only.

## Consequences

- Richer sessions, header-sourced tool durations and compaction markers for crewmates.
- A client that does not set the variable still works: the proxy parses the hint headers whenever present, and V2 and V4 show less for it.

## Work required

- Confirm by a capture test (ADR 0038) that the hint headers arrive with the pinned Claude Code version and record the header names.
- Set the variable in the documented client settings (ADR 0012, ADR 0037) and in the firstmate crewmate launch environment, which the operator sets by hand in v1 (ADR 0040).

## Depends on

- ADR 0010, ADR 0040.

## References

- architecture/README.md, "Resolved decisions" (D5).
- architecture/backend.md, "Correlation".
- architecture/upstream-connection.md, "Claude Code with a proxy-held API key".
