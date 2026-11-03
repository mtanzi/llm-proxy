// Package recorder is the one use case for a finished call (ADR 0016).
//
// Finished never blocks. It fans the record out to live-tail subscribers
// through bounded channels, and puts it on a bounded queue with a non-blocking
// send. One goroutine drains the queue and calls core.CallWriter.WriteBatch.
// A batch closes at BatchSize records or BatchInterval, whichever comes first.
// The package holds no SQL and no store import.
//
// The four metrics keep their plain counters and accessors on Recorder. The
// collector from Recorder.Collector exports them to Prometheus when scraped;
// the Metric* constants are their names (ADR 0026).
package recorder
