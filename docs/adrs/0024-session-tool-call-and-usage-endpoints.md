# ADR 0024: Session, tool-call and usage endpoints

Status: Completed

Date: 2026-10-21

## Context

Views V2, V4 and V5 need session lists and detail, tool-call lists and summaries, and usage time series. The reader methods exist (ADRs 0020 and 0021); this ADR exposes them with the same conventions as the call log.

## Decision

- `GET /admin/sessions` and `GET /admin/sessions/{key}`: summaries, prompts, agents and call metadata.
- `GET /admin/tool-calls` (with `?session=`) and `GET /admin/tool-calls/summary`.
- `GET /admin/usage` with `group_by`, `grain` and the call-log filters, computed from raw rows.
- Same list conventions as ADR 0023: time bound, forward keyset cursor, repeated filter parameters, `count` and `count_capped` where applicable.
- Duration fields report their source (`header` or `estimated`). Unmatched halves are a filter value.

## Consequences

- The session key is the header value as sent and may contain characters such as `:` or `/`; path encoding must be tested. Calls without a session header have no session and are not listed.
- Long usage ranges can take seconds (accepted in v1).

## Work required

- Extend `api/openapi.yaml` with the endpoints, parameters and response schemas.
- Implement the parameter mappers for `SessionFilter`, `ToolCallFilter` and `UsageQuery` and the four handler groups.
- Validate `group_by` and `grain` against whitelists with typed errors.
- Tests with a fake `CallReader`: parameter mapping, cursor handling, error mapping, session key path encoding, response conformance with the OpenAPI document.
- Regenerate the TypeScript types in `web/` (ADR 0030) after the spec changes.

## Depends on

- ADR 0020, ADR 0021, ADR 0023.

## References

- architecture/backend.md, "Admin and query API" (endpoint table).
- architecture/frontend.md, "Views".
