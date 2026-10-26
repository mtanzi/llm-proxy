# ADR 0043: Short tool snippets (D11)

Status: Accepted

Date: 2026-10-21

## Context

The strict reading of "no bodies" keeps no tool argument text, no tool result text and no error snippet. That makes the tool explorer show names, sizes, durations and outcomes, but not what ran.

## Decision

Accepted (D11), with the strict default: keep nothing. Tool calls carry name, ids, sizes, duration and success or error only.

Rejected option: keep a short snippet, for example the first 200 bytes of tool arguments and of error output, so the tool explorer can show what ran. This stores content that may include secrets, paths or prompt text. It would need redaction rules, a retention decision and a UI warning, and it weakens the "no bodies" guarantee tested in ADR 0018 and ADR 0038.

## Consequences

- The storage contract's no-bodies check stays absolute.
- The tool explorer shows names, sizes, durations and outcomes, not what ran.

## Work required

- Keep the no-bodies tests absolute (ADR 0018, ADR 0038): no tool argument text, tool result text or error snippet in any stored value.
- Show names, sizes, durations and outcomes in V4 (ADR 0034), with no content fields.

## Depends on

- ADR 0014, ADR 0034.

## References

- architecture/README.md, "Resolved decisions" (D11).
- architecture/backend.md, "Extractor".
- architecture/frontend.md, "V4 Tool-call explorer".
