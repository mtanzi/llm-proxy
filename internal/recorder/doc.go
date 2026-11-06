// Package recorder is the one use case for a finished call (ADR 0016).
//
// Finished never blocks. It fans the record out to live-tail subscribers
// through bounded channels, and puts it on a bounded queue with a non-blocking
// send. One goroutine drains the queue and calls core.CallWriter.WriteBatch.
// A batch closes at BatchSize records or BatchInterval, whichever comes first.
// The package holds no SQL and no store import.
//
// A transient write error retries the batch with backoff. An error that
// matches core.ErrUnwritable splits the batch until each rejected record
// stands alone, and the rejected records are dropped and counted, unless every
// record of a batch of at least SystematicMinRecords failed: that batch is
// kept and retried, and SystematicFailure reports it (ADR 0051).
//
// The metrics keep their plain counters and accessors on Recorder. The
// collector from Recorder.Collector exports them to Prometheus when scraped;
// the Metric* constants are their names (ADR 0026).
package recorder
