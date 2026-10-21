# ADR 0044: Provider error message text (D12)

Status: Proposed

Date: 2026-10-21

## Context

When the provider returns an error, the body carries a message string. It helps debugging but may echo prompt content. The strict default keeps only the HTTP status and the provider error `type`.

## Decision

Open (D12). The documents use the recommended option.

- **Recommended (strict default):** keep error type and HTTP status only; drop the message text.
- **Alternative:** keep the provider's error message. It is useful for debugging but can contain fragments of the request, so it would need a length bound, redaction and a retention decision.

## Consequences

- Strict: call detail shows `error_type` and status. Unusual errors need the client's own logs.
- Keeping the text: new column, privacy review, UI display in V3.

## Work required

- Decide and update this ADR with the outcome.
- If kept: add a bounded column by migration (ADR 0017), extract it in ADR 0014, update the no-bodies tests, the API schema and V3 (ADR 0032).
- If strict is confirmed: mark Accepted and remove the pending note from the architecture documents.

## Depends on

- ADR 0014, ADR 0032.

## References

- architecture/README.md, "Open decisions" (D12).
- architecture/backend.md, "Extractor".
- architecture/frontend.md, "V3 Call detail".
