# ADR 0023: Admin plane skeleton, OpenAPI contract and call-log endpoints

Status: Completed

Date: 2026-10-21

## Context

The `api` package serves the admin plane on the loopback listener. Handlers read through `CallReader` and never see SQL. The UI generates its TypeScript types from an OpenAPI document, so the contract exists before the UI. There is no sign-in in v1 and every endpoint is a read-only `GET`.

## Decision

- JSON over HTTP under `/admin/`, described by `api/openapi.yaml` (OpenAPI 3.1) and served at `/admin/openapi.json`.
- `api` owns the wire format, including the opaque cursor (base64 of `ts` and `id`). The port takes typed `core.Cursor` values.
- List conventions: filters are query parameters, repeated for multiple values (`model=a&model=b`, `status=429`, `latency_ms_gte=60000`, `tool=Bash&tool_error=true`, `endpoint=/v1/messages`, `streamed=true`); keyset pagination, forward only; responses carry `next_cursor`, `count` and `count_capped` (exact up to 10,001 rows, shown as "10,000+"); every list query is time-bounded, default last 24 hours.
- API errors carry a stable `type` the UI maps to inline messages.
- No endpoint returns prompt, response or tool content.
- This ADR delivers: `GET /admin/calls`, `GET /admin/calls/{id}`, `GET /admin/config` (providers, pricing, key names with `disabled`, never a hash or a credential), and `GET /admin/openapi.json`.
- No CORS headers are sent, so other pages cannot read the API from a browser.

## Consequences

- Handler tests use a fake `CallReader`. A contract break between API and UI fails the UI type check.
- Sessions, tool calls and usage endpoints follow in ADR 0024, the stream in ADR 0025.

## Work required

- Write the first `api/openapi.yaml` covering the endpoints above and the shared parameter, error and cursor schemas, and add a CI step that validates it.
- Implement the router for the admin mux, the parameter parser that maps query strings onto `core.CallFilter` (rejecting unknown values with a typed error), and the cursor encoder and decoder.
- Implement the three data handlers and the OpenAPI handler.
- Implement the default time range and the count cap.
- Tests: parameter parsing table tests, cursor round trip and tamper rejection, handlers against a fake `CallReader`, a test that no response includes a header value or secret from the config.
- Add a spec-conformance test that validates sample responses against the OpenAPI document.

## Depends on

- ADR 0003, ADR 0004, ADR 0006, ADR 0015, ADR 0019.

## References

- architecture/backend.md, "Admin and query API".
- architecture/frontend.md, "State and data fetching", "Admin API access".
- architecture/backend.md, "Ports" (`CallFilter`).
