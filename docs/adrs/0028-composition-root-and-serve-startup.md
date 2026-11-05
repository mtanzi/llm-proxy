# ADR 0028: Composition root and `serve` startup

Status: Completed

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

## Implementation note

Landed in `cmd/llm-proxy`:

- `serve.go` is the composition root. `runServe` runs steps 1 to 3 and 8 to 9; `buildApp` wires steps 4 to 7 from a loaded config holder and a store, with no listener; `(*app).run` serves both planes and runs the shutdown of ADR 0027. Each failing step prints `llm-proxy serve: <step>: <cause>` and exits 1.
- `BuildServers` takes finished handlers and the listener and TLS settings (`ServerOptions`). It builds no handler itself, and it keeps the loopback check on `listen.admin`.
- The first database connection is retried for up to 30 seconds (pauses of 250 ms doubling to 4 s, each ping bounded to 5 s). Then `serve` exits with the last ping error. SIGINT or SIGTERM during the wait stops it. Migrations run after the first successful ping and a failure ends the start.
- Logging uses `log.level` and `log.format`; an attribute named after a credential header is redacted (`httpmw.IsCredentialHeader`). The config file is read once to configure the logger and again by the holder.
- `SIGHUP` reloads through `Holder.WatchReload`; `SIGTERM` and `SIGINT` start the shutdown, and a second signal forces the exit. A failing listener starts the same shutdown.
- The admin plane gets the store as `CallReader`. The recorder is not passed to `api`: the live tail (ADR 0025) is not built. The UI mounts at `/ui/` in `api.NewMux` (ADR 0029).
- Tests: `serve_test.go` wires the data plane, recorder and admin plane with an in-memory store and a fake upstream; `serve_postgres_test.go` does the same against a test Postgres (ADR 0018) and reads the row back. `make check-imports` shows that only `cmd` imports `store/postgres`.

## Depends on

- ADR 0005, ADR 0008, ADR 0011, ADR 0016, ADR 0017, ADR 0022, ADR 0023, ADR 0026, ADR 0027.

## References

- architecture/backend.md, "Shape", "Package layout" (dependency rules).
- architecture/deployment.md, "What runs", "Migrations".
