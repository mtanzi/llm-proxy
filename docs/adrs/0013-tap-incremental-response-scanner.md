# ADR 0013: Tap: incremental response scanner

Status: Completed

Date: 2026-10-21

## Context

The proxy must learn usage, timings, stop reason, error type and tool-use blocks from the response without delaying it, and without keeping the bytes. The request side needs no tap because the body is already in memory.

## Decision

- `ModifyResponse` wraps the upstream response body in a tap. `Read` passes each chunk to an incremental scanner, forwards it unchanged, then resets the idle watchdog.
- For SSE the scanner splits events on blank lines and keeps only the facts: usage fields, stop reason, error type, and for each `tool_use` block the call id, tool name and argument size. For a non-streaming JSON response it buffers up to a fixed bound, then parses once with `gjson`.
- Buffers are bounded: 1 MiB for one SSE event, a fixed bound for a non-streaming body. Past a bound the tap stops reading that part, keeps forwarding, and sets `extraction_partial = true`. Fields it could not read stay null.
- `Close` is the "exchange finished" hook. `ReverseProxy` closes the upstream body on success, upstream error and client abort.
- Timings: time to first byte, TTFT (first content event), total duration, end timestamp.
- The idle-between-chunks watchdog uses the provider's `idle_timeout`. Before the response headers it cancels the upstream request and the proxy sends its own 504. After the headers the status line has already gone, so the stream ends and the record gets `error_class = timeout`; the proxy sends no 504.
- No body is written to disk or to the store.

## Consequences

- The cost of incremental scanning is unmeasured (the measured 54 µs covered copying only); ADR 0039 measures it.
- A `sync.Pool` for tap buffers is a later item if profiling shows allocation pressure.
- The scanner is part of the `family/anthropic` knowledge; `proxy` owns only the `Read`/`Close` wiring.

## Work required

- Implement the SSE event splitter and the Anthropic event interpreter in `family/anthropic` as pure code over byte chunks (no `net/http` server types).
- Implement the tap `io.ReadCloser` in `proxy`: chunk pass-through, scanner calls, bounds, `Close` hook that hands the collected facts to the extractor.
- Implement the idle watchdog: on silence past `idle_timeout`, cancel the upstream request and mark the outcome `timeout`. Generate the 504 only before response headers; after headers, end the stream.
- Tests: golden SSE fixtures (normal, thinking pauses with `ping`, tool use, error event mid-stream, truncated stream), events split across chunk boundaries at every byte offset, oversize event sets `extraction_partial`, client abort still fires `Close`, an idle timeout before headers gives a 504, an idle timeout after headers ends the stream with `error_class = timeout`.
- Fuzz the event splitter with random chunking.

## Depends on

- ADR 0011.

## References

- architecture/backend.md, "Tap", "Extractor", "Sequence: a streaming request".
