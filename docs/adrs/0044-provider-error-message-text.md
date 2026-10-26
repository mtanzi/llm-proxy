# ADR 0044: Provider error message text (D12)

Status: Accepted

Date: 2026-10-21

## Context

When the provider returns an error, the body carries a message string. It helps debugging but may echo prompt content. The strict default keeps only the HTTP status and the provider error `type`.

## Decision

Accepted (D12), with the strict default: keep error type and HTTP status only; drop the message text.

Rejected option: keep the provider's error message. It is useful for debugging but can contain fragments of the request, so it would need a length bound, redaction and a retention decision.

## Consequences

- Call detail shows `error_type` and status. Unusual errors need the client's own logs.

## Work required

- Store `error_type` and the status, never the message text (ADR 0014), and keep that in the no-bodies tests (ADR 0018, ADR 0038).
- Show `error_type` and status in V3 (ADR 0032), with no message field.

## Depends on

- ADR 0014, ADR 0032.

## References

- architecture/README.md, "Resolved decisions" (D12).
- architecture/backend.md, "Extractor".
- architecture/frontend.md, "V3 Call detail".
