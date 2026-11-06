# ADR 0025: Live tail (`GET /admin/stream`)

Status: Completed

Date: 2026-10-21

## Context

The call log has a Live toggle that shows new calls as they finish. v1 runs one replica, so the recorder's in-process fan-out carries every call. Calls finish out of order and `Finished` is published before the batch commits, so an exact catch-up after a reconnect is not possible without a commit sequence number.

## Decision

- `GET /admin/stream` subscribes to the recorder, applies `core.CallFilter.Match` to each event, and writes SSE with the same filter parameters as the call list.
- One event type: `call.finished`. The stream carries finished calls only; there is no `call.started` and no in-flight row. Each event has an `id` (the call's UUIDv7) for the UI's de-duplication.
- No `Last-Event-ID` catch-up. On reconnect the UI refetches the first page of the list.
- The handler clears its own write deadline (ADR 0006), sends periodic comment keep-alives, and ends when the client disconnects or the server shuts down.
- Slow subscribers lose events, counted by the recorder.

## Consequences

- The stream works on one replica only. A `LiveFeed` port (`NOTIFY` after commit) is a later item ([backend.md, Later versions](../architecture/backend.md#storage-and-scale)).
- If an exact catch-up is ever needed, it needs a commit sequence number, not the call id.

## Work required

- Implement the SSE handler in `api` using `http.NewResponseController` for flush and deadline control, with the event encoding and keep-alive interval.
- Reuse the filter parameter parser from ADR 0023 and `CallFilter.Match` from ADR 0003.
- Add the `call.finished` schema to the OpenAPI document as documented extension content.
- Tests: events pass the filter or are dropped, a slow subscriber does not block the recorder, unsubscribe on disconnect leaks no goroutine, shutdown ends open streams.
- Verify through a real HTTP test that proxies in front do not buffer (header `Cache-Control: no-cache`, `X-Accel-Buffering: no`).

## Depends on

- ADR 0003, ADR 0016, ADR 0023.

## References

- architecture/backend.md, "Live tail", "Recorder".
- architecture/frontend.md, "Live tail".
- architecture/backend.md, "Later versions" (multi-replica tier, in-flight rows).
