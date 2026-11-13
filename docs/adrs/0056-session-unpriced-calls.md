# ADR 0056: Unpriced calls per session

Status: Proposed

Date: 2026-11-13

## Context

A session summary has a `cost_micro_usd` that is null only when no call of the session has a known cost. When some calls are priced and others are not, the value sums the priced calls and looks complete. ADR 0052 sets the rule that unknown is not zero, and the usage buckets already follow it: each bucket carries `unpriced_calls`. The session list, the session view and the agent-facing CLI (ADR 0055) cannot follow the rule for a session, because the summary has no count of its unpriced calls.

The count is cheap to add. The session aggregate is already one SQL select over the calls of each session (`sessionColumns` in `internal/store/postgres/sessions.go`), and the usage query already computes the same number as `count(*) FILTER (WHERE c.cost_usd IS NULL)`.

The alternative is a client-side join: ADR 0055 as first written had `sessions list` make one more `GET /admin/usage?group_by=session` request over the same window with no call filters, sum the buckets per session, and join them to the listed rows by key. That needs a paragraph to explain why it is correct, adds a request per list, and leaves the UI with the same hidden partial cost. A new field removes all three.

This ADR amends no Completed behaviour in a breaking way: it adds one field to a response. ADR 0024 (session endpoints) and ADR 0033 (the session views) are Completed and are not edited. This ADR names them as the ones it extends.

## Decision

- **`SessionSummary` gains `unpriced_calls`**, an integer: the number of calls of the session in the same scope as the other counts of the summary, whose `cost_usd` is null. A call counts as unpriced exactly when the usage buckets of ADR 0021 count it, so a session's `unpriced_calls` equals the sum of the `unpriced_calls` of its usage buckets over the same calls.
- **It is computed in the session aggregate**, with the expression the usage query uses: `count(*) FILTER (WHERE c.cost_usd IS NULL)`, added to `sessionColumns` and to the field lists that read it back. Sessions and the session detail summary both carry it. It is derived at read time, with no new column and no migration (ADR 0020).
- **The meaning of `cost_micro_usd` does not change.** It stays null only when no call is priced. A reader that wants to know whether a cost is partial reads `unpriced_calls > 0`.
- **The field is required in the OpenAPI schema** of the session summary, as the other counts are, and is `0` when every call is priced.
- **Out of scope:** a `providers` field on the summary, which a per-session API-equivalent label would need (ADR 0052). It is less pressing and can be its own change.

## Consequences

- **The CLI needs no join.** `sessions list` and the home view of ADR 0055 read the count from the session rows.
- **The UI can stop hiding a partial session cost.** The session list and session view may show a marker beside a cost whose `unpriced_calls` is above zero. That UI change is a follow-up and is not part of this ADR's work.
- **The change is additive.** An older client ignores the new field. A CLI built against the spec requires it, so the CLI and the server it talks to must be of a version that has it.
- **The session query reads one more aggregate.** The cost is one filtered count in a select that already scans the same rows, with no new index.

## Work required

- `internal/core/result.go`: add `UnpricedCalls int` to `SessionSummary`, with a comment that it counts the calls whose cost is nil.
- `internal/store/postgres/sessions.go`: add `count(*) FILTER (WHERE c.cost_usd IS NULL)` to `sessionColumns`, the matching name to `sessionFields` and `sessionSelect`, and the scan in `scanSession`.
- `internal/api/wire.go`: add `UnpricedCalls int` with the JSON name `unpriced_calls` to `sessionSummaryJSON`, and map it in `toSessionSummary`.
- `api/openapi.yaml`: add `unpriced_calls` (integer) to the session summary schema, and to its `required` list.
- `internal/store/storetest`: one case in the session group with a session of priced and unpriced calls, checking the count and that `cost_micro_usd` still sums only the priced calls, and a session with no unpriced calls checking `0`. The JSON-Schema conformance test in `internal/api` covers the wire shape.
- `web/src/api/schema.gen.ts`: regenerate with `make ui-gen`, so `make ui-check` and the OpenAPI check pass.
- No change to ADR 0055 is needed: it already reads the count from this field.
- Mark this ADR Completed in the change that lands the work above.

## Depends on

- ADR 0020 (sessions derived at read time), ADR 0021 (the usage count of unpriced calls), ADR 0024 (session endpoints), ADR 0052 (unknown is not zero).

## References

- `internal/store/postgres/sessions.go`, `usageSums` and `costSum` (the session aggregate); `internal/store/postgres/usage.go` (the usage count of unpriced calls).
- `api/openapi.yaml`, the session summary schema.
- ADR 0055 (the CLI that uses the field), "Unknown is not zero" and gap 7.
