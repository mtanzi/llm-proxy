# ADR 0022: Retention job

Status: Accepted

Date: 2026-10-21

## Context

Call records are metadata only, kept for a configured period (default 400 days). Tables are plain, so retention is a batched `DELETE` plus autovacuum, not `DROP PARTITION`. The job belongs to the adapter and is started by `cmd`. It is not a port.

## Decision

- One daily job: `DELETE … WHERE ts < cutoff` in batches of 10,000 rows, for `calls` and `tool_events`. Default `retention.calls: 400d`.
- The job is the method `(*Store).DeleteBefore(table, cutoff)` on the concrete adapter. Retention settings are passed to the adapter constructor from the config file.
- No job interface, no advisory lock and no partition job in v1. With one replica a lock is not needed; the multi-replica tier adds an advisory lock later.
- Running it twice is safe.

## Consequences

- Large first runs after lowering the retention take several batches; each batch is its own transaction so autovacuum can follow.
- `tool_events` rows are deleted by their own `ts`, so a half can outlive its call by a short window; reads tolerate this because there are no foreign keys.

## Work required

- Implement `DeleteBefore` with the batch loop, a pause between batches, context cancellation and a returned count.
- Implement the daily scheduler goroutine owned by the adapter (start, stop with context), triggered at a fixed time of day plus jitter, and a log line per run with rows deleted.
- Add metrics for the last run time and rows deleted (`llm_proxy_retention_deleted_total`, `llm_proxy_retention_last_run_timestamp_seconds`).
- Add contract cases (ADR 0018): rows older than the cutoff go, nothing else goes, batches respect the size, a second run deletes nothing.
- Start the job from the composition root (ADR 0028) and stop it on shutdown (ADR 0027).

## Implementation note

Landed:

- `postgres.Config.Retention` carries the settings: `Keep`, batch size and batch pause (10,000 rows and 100 ms by default). The job runs at a fixed 03:00 UTC plus up to 30 minutes of jitter.
- `(*Store).DeleteBefore(ctx, table, cutoff)` is the batch loop, and `RunRetention` runs both tables once and logs one line.
- `StartRetention(ctx)` and `StopRetention(ctx)` start and stop the scheduler goroutine, and `Close` also stops it.
- `RetentionStats` returns the deleted-row count and last run time as plain values.
- The contract cases are in `internal/store/storetest/retention.go`.

Remaining:

- Config wiring and start: the composition root (ADR 0028) fills `Retention.Keep` from `retention.calls` and calls `StartRetention`.
- Stop on shutdown: ADR 0027 calls `StopRetention`.

## Depends on

- ADR 0004, ADR 0017, ADR 0018.

## References

- architecture/backend.md, "Background job", "Adapters".
- architecture/deployment.md, "Storage, retention and sizing".
