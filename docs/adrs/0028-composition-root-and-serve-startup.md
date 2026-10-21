# ADR 0028: Composition root and `serve` startup

Status: Accepted

Date: 2026-10-21

## Context

Only `cmd/llm-proxy` knows concrete types. It builds every adapter from the config, passes each consumer the port it needs, applies migrations, opens the listeners and starts the retention job. Tests wire fakes the same way.

## Decision

Startup order for `serve`:

1. Parse flags and load the config (ADR 0004); build the first snapshot (ADR 0005) including the keyring.
2. Set up logging with redaction.
3. Open the Postgres store from config; run migrations under goose's lock; fail start if migrations fail.
4. Build the recorder with the store as `CallWriter`; start the batch writer.
5. Build the proxy handler with the keyring, recorder and snapshot holder.
6. Build the admin plane: `api` with the store as `CallReader` and the recorder for the live tail; `ui` handler; metrics.
7. Start the retention job.
8. Open the listeners (data plane, admin plane) only after migrations succeed; check the loopback rule first.
9. Install `SIGHUP` and `SIGTERM` handlers (ADRs 0005, 0027).

A dependency-injection framework is not used; the wiring is plain constructor calls.

## Consequences

- The data plane does not start serving until the store has migrated. A database outage after start does not stop the data plane (the recorder absorbs it).
- If Postgres is unreachable at start, `serve` retries for a bounded time and then exits with a clear message, so an orchestrator restarts it.

## Work required

- Implement `serve` in `cmd/llm-proxy`, with the startup sequence and clear failure messages for each step.
- Implement bounded startup retry for the first database connection.
- Add an in-process integration test that wires the real proxy, recorder and a fake upstream to a test Postgres (ADR 0018 harness) and verifies a request ends up as a row.
- Add a wiring test with fakes only (no database) so the data plane is exercised in unit tests.
- Verify with the import check (ADR 0001) that only `cmd` imports `store/postgres`.

## Depends on

- ADR 0005, ADR 0008, ADR 0011, ADR 0016, ADR 0017, ADR 0022, ADR 0023, ADR 0026, ADR 0027.

## References

- architecture/backend.md, "Shape", "Package layout" (dependency rules).
- architecture/deployment.md, "What runs", "Migrations".
