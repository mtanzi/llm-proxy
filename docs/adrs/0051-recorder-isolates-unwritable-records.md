# ADR 0051: Recorder isolates unwritable records

Status: Completed

Date: 2026-11-05

## Context

A batch that Postgres keeps rejecting blocks the recorder, and recording stops without a visible failure to the data plane. The evidence is in the landed code.

- `postgres.WriteBatch` (`internal/store/postgres/write.go`) runs the whole batch in one transaction. One bad row aborts the transaction, so nothing from the batch is stored. The port contract is "idempotent on call id, atomic per batch" (`core.CallWriter`).
- `Recorder.flush` (`internal/recorder/recorder.go`) retries a failed batch with capped exponential backoff (100 ms doubling to 10 s, with jitter) until it succeeds or `Close` aborts the writer. It does not look at the error. There is no retry limit.
- There is one writer goroutine. While `flush` loops on one batch, nothing else is written. The queue (default 10,000) fills in about 5 to 20 hours at 12,000 to 50,000 calls per day, then `Finished` drops every new record as `queue_full`.
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
- **Unknown defaults to transient.** A database outage must never drop records, so a driver error that nobody classified retries. The cost is that an unclassified poison error still blocks the writer, but that is the status quo, and it stays visible in `llm_proxy_writer_errors_total`. A class 42 error (undefined column or table) means a schema or code bug that hits every record, so it is transient too: dropping the whole queue would hide the bug. A systematic bug can also show as a permanent class, for example a migration that adds a `NOT NULL` or `CHECK` column the writer does not fill (`23502`, `23514`) or a mapping bug that writes an out-of-range value (`22003`). The systematic-failure rule below keeps such a bug from draining the queue when batches reach its threshold. Below the threshold such a bug is dropped per record as `unwritable`.

### Recorder behaviour

- `flush` writes the batch. On a transient error it retries the same slice with the existing backoff, unchanged.
- On an `ErrUnwritable` error with more than one record, it splits the slice in two halves and writes the first, then the second, each through the same logic. Each half is a smaller atomic batch. A half with only good records commits. The write is idempotent on call id, so a half that was never committed and a half that is rewritten cannot duplicate rows.
- On an `ErrUnwritable` error with one record, it marks that record as failed. Leaf drops are decided only after the split of the whole original batch is done, so the systematic-failure rule below can still keep the batch. When the split is done and the rule does not apply, the recorder drops each failed record, increments `llm_proxy_calls_dropped_total{reason="unwritable"}` and logs one error line with the call id, the key name, the SQLSTATE and the constraint or column name when the adapter provides them. The log carries no body field, as the no-bodies rule requires (ADR 0002). The writer then goes on to the next batch.
- **Systematic failure.** If the split of a batch ends with every record failing with `ErrUnwritable`, and the batch holds at least `recorder.SystematicMinRecords` records (8), the failure is systematic, not record-specific. The recorder drops nothing. It keeps the whole batch and retries it with the normal backoff, as for a transient error. Each retry writes the whole batch first and splits again only on a new `ErrUnwritable` error, so a fix that leaves only some records bad goes back to isolation and dropping. While the writer holds such a batch, the gauge `llm_proxy_writer_systematic_failure` is 1, and each detection logs one error line with the batch size, the SQLSTATE and the constraint or column name of the first failing record, with no body field. The gauge goes back to 0 when a write commits. Isolation and dropping stay unchanged for a batch where only some records fail.
- An all-fail batch below that threshold, including a batch of one record, is isolated and dropped per record like any other. A small batch cannot tell poison records from a systematic bug. An agent loop that sends two turns with the same poison history within the 500 ms batch window puts two poison records in one batch, and blocking on that batch would bring back the stop this ADR removes.
- A transient error inside a half retries that half with backoff, as before. An outage still blocks the writer and fills the queue, and the existing `queue_full` accounting applies.
- Splitting needs no retry counter. The classification already says the same batch will fail again, so the first permanent error is enough to split.
- `Close` and abort behave as today. A drop at the deadline stays `reason="shutdown"`. On abort, including abort during a split or during a systematic-failure retry, the recorder counts as `reason="shutdown"` every record of the original batch that is not yet committed or dropped, plus every record still in the queue. Example: abort while the first 250-record half of a 500-record batch is in flight counts 500 plus the queue.

Cost: a batch of 500 with one bad record costs at most about `2 * log2(500)`, which is 18, extra `WriteBatch` calls, and the good 499 records are stored. Several bad records in one batch cost that much for each of them. Postgres sees small transactions only. A systematic failure costs about `2N - 1` calls for a batch of N records each time it is detected (999 for 500 records), and then the normal backoff, capped at 10 s, applies before the next attempt.

### Metric

`llm_proxy_calls_dropped_total{reason}` gains a third value, `unwritable`. It follows ADR 0026 naming: the existing counter, the existing `reason` label, no new metric name. `recorder.ReasonUnwritable` joins `ReasonQueueFull` and `ReasonShutdown`, and the collector exports all three at zero from start. The help text keeps "Should stay zero". `llm_proxy_writer_errors_total` still counts every failed `WriteBatch` call, including each failing half. A rate above zero on `reason="unwritable"` is the alert condition: it points to a bug that ADR 0017 did not sanitize.

`llm_proxy_writer_systematic_failure` is a new gauge, owned by `recorder`, with no labels. It follows ADR 0026 naming: the `llm_proxy_` prefix and no `_total` suffix, because it is a state and not a count. A value of 1 is the alert condition for a systematic bug: the writer is blocked on a batch where every record fails permanently.

## Options considered

1. **Classify, then split the batch to the failing record (chosen).** Loses only the records Postgres rejects and costs extra writes only when a batch fails permanently. Needs a classifier in the adapter and a core sentinel. A database outage does not drop anything, because only permanent errors split.
2. **Per-record fallback inserts after N failures.** After N failed batch attempts, write each of the 500 records alone. It works without a core change if "N failures" is the only trigger, but then an outage of N attempts triggers 500 single inserts at the moment the database is weakest, and every record that fails during the outage is a candidate for a drop. Without classification it cannot tell an outage from a poison record. With classification it is option 1 with a worse cost: 500 round trips where 18 would do, and each is its own transaction.
3. **Drop the whole batch after N failures.** Simplest, and it unblocks the writer. It loses up to 499 good records for each bad one. If N is small, a short outage drops the batch. If N is large, the writer stays blocked for the same time as today. It also hides the poison record because nobody can say which record caused the drop.
4. **Status quo with an alert on `llm_proxy_writer_errors_total`.** Stays as the fallback for an error nobody classified. It detects the stop but does not recover from it without a restart that loses the queue.

For a systematic bug that shows as a permanent class, these alternatives were considered:

- **Systematic-failure rule (chosen).** A batch of at least `SystematicMinRecords` (8) records where every record fails permanently is kept and retried with backoff, with a gauge and an error log. For such a batch the bug blocks the writer and is visible, the same as a class 42 error. Smaller batches are still dropped per record.
- **Accept the drain and state it in Consequences.** No new behaviour. A systematic bug in class 22, 23 or 54 drops the whole stream as `reason="unwritable"`. The drop is counted, but the records are lost, which is the outcome this ADR rejects for class 42.
- **Narrow the permanent set.** Keep only the SQLSTATEs that a single record can cause and that a schema change cannot, for example `22021` and `22P05`. Class 23 and most of class 22 then stay transient. This avoids the drain, but a real poison record in those classes blocks the writer again, which is the problem this ADR solves.

Open question for review: whether the systematic-failure rule (keep and retry a batch where every record fails permanently, only when the batch holds at least `SystematicMinRecords` = 8 records) is the right answer for a systematic bug, instead of accepting the drain or narrowing the permanent set, and whether 8 is the right threshold. A source-based rule, for example one that also requires records from at least two distinct key names or agents, is a possible refinement left for review.

Resolution (2026-11-06): accepted as written, with the systematic-failure rule and the threshold of 8.

## Consequences

- At low load, a batch closes 500 ms after its first record, so most batches hold fewer than `SystematicMinRecords` (8) records. A systematic bug that shows as a permanent class is then isolated and dropped per record as `reason="unwritable"`, and when no batch of 8 or more records forms, it drains the stream. The drops are visible through `llm_proxy_calls_dropped_total{reason="unwritable"}` and the error log, and the `unwritable` rate is the alert.
- The keep-and-retry rule and its gauge apply only to an all-fail batch of at least 8 records. For such a batch a systematic bug blocks the writer, as a class 42 error does, and fills the queue instead of dropping records. The gauge and the error log make it visible.
- Poison records block the writer only if they make up a whole batch of at least 8 records. Below 8, poison records are dropped, so a few poison records from one agent loop cannot stop the writer. Eight or more poison records in one batch with no good record among them are unlikely outside a systematic bug, and the gauge shows the case if it happens.
- One unclassified-as-transient poison record can still block the writer. The mitigation is the alert on `writer_errors_total` and a test that feeds a real Postgres each known class. Whenever such a record is found, adding its SQLSTATE to the classifier is a one-line change.
- A permanent drop loses a call record for good. The loss is counted, logged with the call id and visible. The live tail already showed the call at `Finished`, so the UI may show a call the log does not hold. That gap is the cost of not stopping the recorder.
- `CallWriter` gets a documented error contract, and any new adapter (for example `store/sqlite`) must classify its own errors. The storetest suite from ADR 0018 gains a case for it when the contract can be reached without a store-specific bad record. Otherwise the case stays in the Postgres test (see Work required).
- The split writes happen in the single writer goroutine, in order. The batch order inside the database changes only for records that were already unordered in time, and no code reads insertion order: lists order by `(ts, id)`.
- ADR 0016 stays as written. The "retried with backoff and counted" sentence now applies to transient errors only. This ADR is the correction.

## Work required

- Add `core.ErrUnwritable` and update the `CallWriter.WriteBatch` doc comment with the contract above (`internal/core/ports.go`).
- Add a classifier in `internal/store/postgres` (for example `classify(err) error`) that maps `*pgconn.PgError` class 22, 23 and 54, and the `pgx` encoding failure, to an error that wraps `core.ErrUnwritable` and keeps the original error. Call it from `WriteBatch`. First find out which error type `pgx` returns for an encoding failure and test it.
- Add `recorder.ReasonUnwritable = "unwritable"`, a counter, the third reason in `CallsDropped` and in the collector loop, and the help text in `internal/recorder/metrics.go`. Add the `llm_proxy_writer_systematic_failure` gauge in the same file and register it with the recorder collectors.
- Change `Recorder.flush` to split on `ErrUnwritable` and drop a single failing record, as described. Hold leaf drops until the split of the whole original batch is done. Add `SystematicMinRecords = 8` and the systematic-failure rule: when every record of a batch of at least `SystematicMinRecords` records failed with `ErrUnwritable`, drop nothing, set the gauge to 1, log the error and retry the whole batch with the normal backoff. Set the gauge to 0 on the next commit. Keep transient retry behaviour. On abort, pass `dropRest` every record of the original batch not yet committed or dropped, not only the half in hand.
- Recorder tests with a fake `CallWriter` that rejects chosen record ids with an `ErrUnwritable` error: one bad record in a batch of 500 stores 499 and counts one drop; several bad records; a bad record in the first and last position; a batch of one is dropped; a transient error then success with no drop; a transient error inside a half; every record of a batch of 500 rejected drops nothing, sets the gauge to 1 and retries the batch; the fake then accepts all but one record, which stores 499, counts one drop and sets the gauge to 0; an all-fail batch of 8 records is kept; an all-fail batch of 7 records is dropped as 7 `unwritable` records with the gauge at 0; an all-fail batch of 2 records is dropped as 2; abort during a split of a 500-record batch while the first half is in flight counts exactly 500 plus the queue as `reason="shutdown"`; abort during a systematic-failure retry counts the whole batch plus the queue; race-free under `-race`.
- Postgres test through the ADR 0018 harness: a batch with one row that violates a constraint (for example a `NOT NULL` column set to NULL through a crafted `CallRecord` or a test-only table) returns an error that matches `core.ErrUnwritable`; a closed pool returns one that does not.
- Add the case to the storetest contract suite (ADR 0018) if the contract can be reached without a store-specific bad record. Otherwise keep it in the Postgres test.
- Add `llm_proxy_writer_systematic_failure` to the metric names and labels golden test (ADR 0026).
- Update `backend.md`, "Recorder" (Failure) and `deployment.md`, "Observability" with the new drop reason, the new gauge and the two alert hints. Do not edit ADR 0016, 0017 or 0026.

## Depends on

- ADR 0003, ADR 0016, ADR 0017, ADR 0026.

## References

- adrs/0016-recorder-queue-and-batch-writer.md, "Decision" (Failure).
- adrs/0017-postgres-store-schema-and-write-path.md, "Decision" (Write path).
- adrs/0026-observability-health-readiness-and-metrics.md, metric list.
- adrs/0049-source-precedence-and-corrections-to-completed-adrs.md, "Decision".
- architecture/backend.md, "Recorder" (Writer details).
