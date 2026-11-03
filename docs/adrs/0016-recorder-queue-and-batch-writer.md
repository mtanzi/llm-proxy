# ADR 0016: Recorder: live fan-out, bounded queue and batch writer

Status: Completed

Date: 2026-10-21

## Context

Logging must never add latency to a request or fail one. The recorder is the single use case for a finished call: it receives `Finished` events and does three things, none of them blocking. It is the only caller of `CallWriter`. There is no durable buffer (decision D2).

## Decision

1. **Live fan-out.** Each subscriber of the live tail gets a bounded channel. A slow subscriber loses events and the loss is counted (`llm_proxy_live_events_dropped_total`).
2. **Bounded queue.** `Finished` goes to the queue with a non-blocking `select`. When full, the record is dropped and `llm_proxy_calls_dropped_total{reason}` is incremented (`queue_full`, `shutdown`).
3. **Batch writer.** One goroutine drains the queue and calls `CallWriter.WriteBatch`. A batch closes at 500 records or 500 ms, whichever comes first. Both are constants in code. `queue_capacity` (default 10,000) is the one setting.

Failure: a failed `WriteBatch` is retried with backoff and counted (`llm_proxy_writer_errors_total`). While the store is down the queue fills, then new records are dropped and counted. The data plane never notices. A crash loses at most `queue_capacity` records.

The fan-out happens at `Finished`, before the commit, as an in-process event. The writer does no joins and no updates. The recorder contains no SQL.

## Consequences

- The call id is a UUIDv7 assigned at request start, so a retried batch inserts nothing twice (the adapter is idempotent).
- The live tail shows finished calls only. There is no `Started` event and no in-flight row.
- Multi-replica live tail (a `LiveFeed` port) is a later item ([backend.md, Later versions](../architecture/backend.md#storage-and-scale)).

## Work required

- Implement `recorder.New(CallWriter, opts)` with a `Finished(...)` method that never blocks.
- Implement `Subscribe(filter)` returning an event channel and an unsubscribe function, with per-subscriber bounded buffers and drop counting.
- Implement the bounded queue and batch writer with the 500/500 ms constants, retry with capped exponential backoff and jitter, and a `Close(ctx)` that stops intake and flushes the remainder within a deadline (default 30 s), counting records still queued afterwards as dropped and logging their number.
- Declare the metrics in this package: `llm_proxy_call_queue_depth`, `llm_proxy_calls_dropped_total{reason}`, `llm_proxy_writer_errors_total`, `llm_proxy_live_events_dropped_total`.
- Tests with a fake `CallWriter`: batching by size and by time, drop on a full queue, no blocking while the writer is stuck, retry then success, flush on close, drop counts after the deadline, race-free under `-race`.

## Depends on

- ADR 0003, ADR 0014.

## References

- architecture/backend.md, "Recorder", "Live tail", "Sequence: persistence and live tail".
- architecture/deployment.md, "Observability".
- architecture/README.md, D2.
