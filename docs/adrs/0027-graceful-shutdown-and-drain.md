# ADR 0027: Graceful shutdown and drain

Status: Completed

Date: 2026-10-21

## Context

A restart or upgrade must not cut running streams or lose the records still in the queue. Claude Code treats a cut stream as a dropped connection and retries, but a clean drain is better. The drain deadline is long (10 minutes) because streams can be long.

## Decision

On `SIGTERM`:

1. Report not-ready on both `/readyz` endpoints, so a load balancer stops sending new requests.
2. Stop accepting connections and wait for in-flight streams up to `server.drain_timeout` (default 10 minutes).
3. Close the recorder queue and let the batch writer drain it into the store within the flush deadline (default 30 seconds).
4. Records still queued after the deadline are counted as dropped (`reason=shutdown`) and their number is logged.
5. Stop the retention job, close the store, exit.

The container stop grace period must match `drain_timeout` (see ADR 0036 and 0037).

## Consequences

- A stuck stream delays exit by up to the drain timeout. A second signal forces an immediate exit.
- The new process starts with an empty queue; records queued in the old process were flushed during its drain.

## Work required

- Implement the shutdown coordinator in `cmd` wiring: ordered stop of listeners, drain wait with deadline, recorder close, store close.
- Add in-flight stream tracking in `proxy` for the wait and for `llm_proxy_inflight_streams`.
- Handle the second signal as a forced exit with a log line.
- Tests: an in-flight stream finishes during drain; new requests after the signal are refused; queued records are flushed; a writer that hangs triggers the deadline and the drop count; the readiness flip order.
- Document the stop grace period setting for Docker and systemd.

## Depends on

- ADR 0006, ADR 0011, ADR 0016, ADR 0026.

## References

- architecture/backend.md, "Listener and router" (graceful shutdown), "Recorder" (Shutdown).
- architecture/deployment.md, "Upgrades", "Observability".
