// Package recorder is the one use case for a finished call (ADR 0016).
//
// Finished never blocks. It fans the record out to live-tail subscribers
// through bounded channels, and puts it on a bounded queue with a non-blocking
// send. One goroutine drains the queue and calls core.CallWriter.WriteBatch.
// A batch closes at BatchSize records or BatchInterval, whichever comes first.
// The package holds no SQL and no store import.
//
// The four metrics are plain counters and a gauge with accessors on Recorder.
// Their exported names are the Metric* constants, for the exporter (ADR 0026).
package recorder
