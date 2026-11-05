# ADR 0051: Recorder isolates unwritable records

Status: Proposed

Date: 2026-11-05

## Context

A batch that Postgres keeps rejecting blocks the recorder, and recording stops without a visible failure to the data plane. The evidence is in the landed code.

- `postgres.WriteBatch` (`internal/store/postgres/write.go`) runs the whole batch in one transaction. One bad row aborts the transaction, so nothing from the batch is stored. The port contract is "idempotent on call id, atomic per batch" (`core.CallWriter`).
- `Recorder.flush` (`internal/recorder/recorder.go`) retries a failed batch with capped exponential backoff (100 ms doubling to 10 s, with jitter) until it succeeds or `Close` aborts the writer. It does not look at the error. There is no retry limit.
- There is one writer goroutine. While `flush` loops on one batch, nothing else is written. The queue (default 10,000) fills in minutes to hours at 12,000 to 50,000 calls per day, then `Finished` drops every new record as `queue_full`.
- The only signal is `llm_proxy_writer_errors_total` and an error log per attempt. A record that Postgres rejects every time therefore turns one bad call into total data loss until someone restarts the proxy. A restart loses the queue and does not help if the bad record is still in it.

ADR 0017 sanitizes the known causes: invalid UTF-8, NUL bytes, invalid `jsonb` in `usage_raw`, and the `tool_events.idx` overflow. That is a list of causes found so far. A new column, a new family or a Postgres change can add another. The recorder must survive a cause that nobody has found yet.

ADR 0016 is Completed. This ADR amends it and does not edit it (ADR 0049). It also amends the `WriteBatch` error contract in `core.CallWriter` (ADR 0003, Completed) and the failure paragraph of ADR 0017 (Completed). None of the three is edited.

## Decision

Recommended: **classify the error, split the batch on a permanent error, drop and count a single record that still fails.**

### Error classes

The store adapter decides which errors are record-specific. The recorder never inspects a driver error, because the recorder contains no SQL and no driver import.

- `core` gains one sentinel, `ErrUnwritable`. `CallWriter.WriteBatch` wraps it in the returned error when the failure comes from the content of the records and not from the state of the database or the connection. The contract line reads: "A returned error that matches `errors.Is(err, ErrUnwritable)` means the batch holds at least one record the store will never accept. Any other error is transient: retry the same batch."
- **Permanent** (the adapter wraps `ErrUnwritable`): a `pgconn.PgError` with SQLSTATE class 22 (data exception, for example `22021` invalid byte sequence, `22P05` untranslatable character, `22003` numeric value out of range, `22P02` invalid text representation), class 23 (integrity constraint violation: not null, check, unique, exclusion) or class 54 (program limit exceeded, for example a row or index entry too large). A client-side value encoding failure in `pgx` that happens before the statement is sent is also permanent.
- **Transient** (retry as today): everything else. This covers connection errors, a closed pool, context cancellation, timeouts, `57P01` admin shutdown, class 08 (connection exception), class 40 (serialization failure or deadlock), class 53 (insufficient resources such as disk full) and class 57 (operator intervention), and every error the adapter does not recognize.
- **Unknown defaults to transient.** A database outage must never drop records, so a driver error that nobody classified retries. The cost is that an unclassified poison error still blocks the writer, but that is the status quo, and it stays visible in `llm_proxy_writer_errors_total`. A class 42 error (undefined column or table) means a schema or code bug that hits every record, so it is transient too: dropping the whole queue would hide the bug.

### Recorder behaviour

- `flush` writes the batch. On a transient error it retries the same slice with the existing backoff, unchanged.
- On an `ErrUnwritable` error with more than one record, it splits the slice in two halves and writes the first, then the second, each through the same logic. Each half is a smaller atomic batch. A half with only good records commits. The write is idempotent on call id, so a half that was never committed and a half that is rewritten cannot duplicate rows.
- On an `ErrUnwritable` error with one record, it drops that record, increments `llm_proxy_calls_dropped_total{reason="unwritable"}` and logs one error line with the call id, the key name, the SQLSTATE and the constraint or column name when the adapter provides them. The log carries no body field, as the no-bodies rule requires (ADR 0002). The writer goes on to the next record.
- A transient error inside a half retries that half with backoff, as before. An outage still blocks the writer and fills the queue, and the existing `queue_full` accounting applies.
- Splitting needs no retry counter. The classification already says the same batch will fail again, so the first permanent error is enough to split.
- `Close` and abort behave as today. A drop at the deadline stays `reason="shutdown"`.

Cost: a batch of 500 with one bad record costs at most about `2 * log2(500)`, which is 18, extra `WriteBatch` calls, and the good 499 records are stored. Several bad records in one batch cost that much for each of them. Postgres sees small transactions only.

### Metric

`llm_proxy_calls_dropped_total{reason}` gains a third value, `unwritable`. It follows ADR 0026 naming: the existing counter, the existing `reason` label, no new metric name. `recorder.ReasonUnwritable` joins `ReasonQueueFull` and `ReasonShutdown`, and the collector exports all three at zero from start. The help text keeps "Should stay zero". `llm_proxy_writer_errors_total` still counts every failed `WriteBatch` call, including each failing half. A rate above zero on `reason="unwritable"` is the alert condition: it points to a bug that ADR 0017 did not sanitize.

## Options considered

1. **Classify, then split the batch to the failing record (chosen).** Loses only the records Postgres rejects and costs extra writes only when a batch fails permanently. Needs a classifier in the adapter and a core sentinel. A database outage does not drop anything, because only permanent errors split.
2. **Per-record fallback inserts after N failures.** After N failed batch attempts, write each of the 500 records alone. It works without a core change if "N failures" is the only trigger, but then an outage of N attempts triggers 500 single inserts at the moment the database is weakest, and every record that fails during the outage is a candidate for a drop. Without classification it cannot tell an outage from a poison record. With classification it is option 1 with a worse cost: 500 round trips where 18 would do, and each is its own transaction.
3. **Drop the whole batch after N failures.** Simplest, and it unblocks the writer. It loses up to 499 good records for each bad one. If N is small, a short outage drops the batch. If N is large, the writer stays blocked for the same time as today. It also hides the poison record because nobody can say which record caused the drop.
4. **Status quo with an alert on `llm_proxy_writer_errors_total`.** Stays as the fallback for an error nobody classified. It detects the stop but does not recover from it without a restart that loses the queue.

## Consequences

- One unclassified-as-transient poison record can still block the writer. The mitigation is the alert on `writer_errors_total` and a test that feeds a real Postgres each known class. Whenever such a record is found, adding its SQLSTATE to the classifier is a one-line change.
- A permanent drop loses a call record for good. The loss is counted, logged with the call id and visible. The live tail already showed the call at `Finished`, so the UI may show a call the log does not hold. That gap is the cost of not stopping the recorder.
- `CallWriter` gets a documented error contract, and any new adapter (for example `store/sqlite`) must classify its own errors. The storetest suite from ADR 0018 gains a case for it.
- The split writes happen in the single writer goroutine, in order. The batch order inside the database changes only for records that were already unordered in time, and no code reads insertion order: lists order by `(ts, id)`.
- ADR 0016 stays as written. The "retried with backoff and counted" sentence now applies to transient errors only. This ADR is the correction.

## Work required

- Add `core.ErrUnwritable` and update the `CallWriter.WriteBatch` doc comment with the contract above (`internal/core/ports.go`).
- Add a classifier in `internal/store/postgres` (for example `classify(err) error`) that maps `*pgconn.PgError` class 22, 23 and 54, and the `pgx` encoding failure, to an error that wraps `core.ErrUnwritable` and keeps the original error. Call it from `WriteBatch`. First find out which error type `pgx` returns for an encoding failure and test it.
- Add `recorder.ReasonUnwritable = "unwritable"`, a counter, the third reason in `CallsDropped` and in the collector loop, and the help text in `internal/recorder/metrics.go`.
- Change `Recorder.flush` to split on `ErrUnwritable` and drop a single failing record, as described. Keep transient retry and abort behaviour. Count the records in `dropRest` as before.
- Recorder tests with a fake `CallWriter` that rejects chosen record ids with an `ErrUnwritable` error: one bad record in a batch of 500 stores 499 and counts one drop; several bad records; a bad record in the first and last position; a batch of one; a transient error then success with no drop; a transient error inside a half; abort during a split; race-free under `-race`.
- Postgres test through the ADR 0018 harness: a batch with one row that violates a constraint (for example a `NOT NULL` column set to NULL through a crafted `CallRecord` or a test-only table) returns an error that matches `core.ErrUnwritable`; a closed pool returns one that does not.
- Add the case to the storetest contract suite (ADR 0018) if the contract can be reached without a store-specific bad record. Otherwise keep it in the Postgres test.
- Update `backend.md`, "Recorder" (Failure) and `deployment.md`, "Observability" with the new drop reason and the alert hint. Do not edit ADR 0016, 0017 or 0026.

## Depends on

- ADR 0003, ADR 0016, ADR 0017, ADR 0026.

## References

- adrs/0016-recorder-queue-and-batch-writer.md, "Decision" (Failure).
- adrs/0017-postgres-store-schema-and-write-path.md, "Decision" (Write path).
- adrs/0026-observability-health-readiness-and-metrics.md, metric list.
- adrs/0049-source-precedence-and-corrections-to-completed-adrs.md, "Decision".
- architecture/backend.md, "Recorder" (Writer details).
