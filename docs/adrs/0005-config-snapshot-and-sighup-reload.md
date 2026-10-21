# ADR 0005: Config snapshot and `SIGHUP` reload

Status: Completed

Date: 2026-10-21

## Context

A restart drains streams for up to 10 minutes, so config changes (keys, pricing, provider credentials) must apply without one. A request in flight must not see its configuration change halfway (gap review R2).

## Decision

- The config loads into an immutable snapshot. The snapshot carries the providers, resolved credentials, pricing and the key map built by `keyring`.
- The current snapshot is held behind an atomic pointer. The handler reads it once at the start of each request and keeps it for the whole request.
- `SIGHUP` re-reads the file and the referenced secrets, builds a new snapshot, validates it, and swaps it atomically. Requests in flight keep the snapshot they started with.
- A reload that fails validation is rejected whole: the proxy keeps the previous snapshot, logs the error and counts it. It does not exit.
- Settings that need a restart (listener addresses, TLS files, database URL, queue capacity) are read at start. A reload that changes them logs a warning and ignores the change.

## Consequences

- Add, revoke and rotate of virtual keys are config edits plus `SIGHUP`. A revoked key is rejected from the next request after the reload; a stream already in flight finishes.
- Provider credentials and pricing rotate the same way.
- Nothing about keys or config touches the store.

## Work required

- Implement the snapshot type and an atomic holder in `internal/config` with `Load`, `Current` and `Reload`.
- Install the `SIGHUP` handler in `cmd` (wiring in ADR 0028); log success or failure with the cause, and count both (`llm_proxy_config_reloads_total{result}`).
- Detect restart-only setting changes on reload and warn.
- Test that a request holding an old snapshot is unaffected by a swap, that a bad file leaves the old snapshot in place, and that concurrent readers during a swap are race-free (`go test -race`).
- Document the `SIGHUP` workflow (including `docker compose kill -s HUP llm-proxy`) in the runbook (ADR 0037).

## Depends on

- ADR 0004.

## References

- architecture/deployment.md, "Configuration" (`SIGHUP` paragraph), "Secrets" (rotation).
- architecture/backend.md, "Forwarding and the config snapshot", "Configuration".
