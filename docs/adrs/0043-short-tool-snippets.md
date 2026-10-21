# ADR 0043: Short tool snippets (D11)

Status: Proposed

Date: 2026-10-21

## Context

The strict reading of "no bodies" keeps no tool argument text, no tool result text and no error snippet. That makes the tool explorer show names, sizes, durations and outcomes, but not what ran.

## Decision

Open (D11). The documents use the recommended option.

- **Recommended (strict default):** keep nothing. Tool calls carry name, ids, sizes, duration and success or error only.
- **Alternative:** keep a short snippet, for example the first 200 bytes of tool arguments and of error output, so the tool explorer can show what ran. This stores content that may include secrets, paths or prompt text. It would need redaction rules, a retention decision and a UI warning, and it weakens the "no bodies" guarantee tested in ADR 0018 and ADR 0038.

## Consequences

- Strict: the storage contract's no-bodies check stays absolute.
- Snippets: the check becomes "no text beyond the snippet bound", the schema gains columns, and V3 and V4 gain fields.

## Work required

- Decide and update this ADR with the outcome.
- If snippets are chosen: add columns by migration (ADR 0017), extend the extractor (ADR 0014) and the call record, redact known secret patterns before storing, change the no-bodies tests, extend the API and OpenAPI document, update V3 and V4 (ADRs 0032, 0034) and the privacy notes.
- If strict is confirmed: mark Accepted and remove the pending note from the architecture documents.

## Depends on

- ADR 0014, ADR 0034.

## References

- architecture/README.md, "Open decisions" (D11).
- architecture/backend.md, "Extractor".
- architecture/frontend.md, "V4 Tool-call explorer".
