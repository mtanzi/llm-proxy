# ADR 0003: Core domain types and storage ports

Status: Accepted

Date: 2026-10-21

## Context

Storage sits behind two ports defined by use case (decision D14, revised by C2). The ports and the domain types they speak belong to `core`, which imports only the standard library, so that `recorder`, `proxy` and `api` can be tested without a database and a second adapter needs no consumer changes.

## Decision

`internal/core` holds:

- Entities: `CallRecord` (with its tool halves), `ToolUse`, `ToolResult`, `Usage`, `Key`.
- Pure rules: `Cost`, `SessionKey` derivation, `CallFilter.Match`.
- Ports: `CallWriter` (`WriteBatch(ctx, []CallRecord) error`, idempotent on call id, atomic per batch) and `CallReader` (`ListCalls`, `CountCalls`, `GetCall`, `ListSessions`, `GetSession`, `ListToolCalls`, `ToolSummary`, `Usage`).
- Filter, page and cursor types: `CallFilter`, `SessionFilter`, `ToolCallFilter`, `UsageQuery`, `Page`, `Cursor{TS, ID}`. Every filter has an explicit time range and typed optional fields, no free-form query strings.
- Result types: `CallSummary`, `CallDetail`, `SessionSummary`, `SessionDetail`, `ToolCall`, `ToolStat`, `UsageBucket`.

Rules: ports follow use cases, not tables; row structs never leave the adapter; no `Upstream` port (the standard `http.RoundTripper` is used); no query service layer, so `api` maps parameters to filter types and calls `CallReader` directly. Lifecycle (`Open`, `Migrate`, `Ping`, `DeleteBefore`, `Close`) is on the concrete Postgres adapter, not on a port.

## Consequences

- The Go interfaces in `core` become the source of truth for the shapes in backend.md, which are only a sketch.
- A new filter adds a field here, a `Match` case, an adapter case and a contract test.
- The live tail is not a storage port: `recorder` fans out in process.

## Work required

- Define the entity and result types, with `uuid.UUID` ids (UUIDv7) and `decimal.Decimal` for cost.
- Define the two port interfaces and the filter, page and cursor types as sketched in backend.md.
- Implement `CallFilter.Match` with table tests covering every field alone and combined; the live tail (ADR 0025) and the contract suite (ADR 0018) reuse it.
- Implement the pure `SessionKey` derivation and `Cost` computation here or with the ADRs that name them (0009, 0015), keeping them in `core`.
- Write the small fakes used by consumer tests: a `CallWriter` that appends to a slice and a `CallReader` that returns canned values (in a `coretest` helper or per consumer).
- Add a package test that fails if `core` imports anything outside the standard library.

## Depends on

- ADR 0001, ADR 0002.

## References

- architecture/backend.md, "Layers and the dependency rule", "Storage ports and adapters" (Ports), "Tests and fakes".
- architecture/README.md, D14 and C2.
