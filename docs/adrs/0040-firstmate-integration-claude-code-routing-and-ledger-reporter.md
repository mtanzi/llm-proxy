# ADR 0040: Firstmate integration: Claude Code routing and the ledger-join reporter

Status: Accepted

Date: 2026-10-21

## Context

The first real consumer of v1 is firstmate, which runs many Claude Code crewmates in parallel. Per-key attribution pays off there. The design plan has two v1 steps after the admin API: route one firstmate harness (Claude Code) through the proxy, and write a reporter that joins firstmate's fleet ledger with the proxy's call data. Firstmate is not a plugin target: the proxy runs as an independent service.

## Decision

- Route Claude Code through the proxy with the documented settings (`proxy_key` with an API key for fleets; passthrough only for one person's own login, ADR 0012).
- Each crewmate gets its own virtual key named `fm:<task-id>`, one config entry per key plus `SIGHUP`. Names stay stable.
- A standalone reporter under `cmd/` in the same module (for example `cmd/fleet-report`) reads `state/fleet-ledger.jsonl` (events `task.dispatched`, `task.status`, `task.pr_ready`, `task.merged`, `task.cleaned_up`) and the call data, and joins on task id (from the key name) and time window. It produces cost per task and cost per merged PR.
- The reporter reads call data through `CallReader` or the admin API, never SQL directly. It tolerates duplicate ledger records (at-least-once delivery) and treats the ledger as untrusted input.
- The alert adapter (`process-event-adapter`) is a later item because alerts are deferred.

## Consequences

- Harnesses that authenticate through a vendor subscription login may bypass the proxy. Each harness must be verified before relying on it; this is the largest integration risk.
- How firstmate injects environment into crewmates is open (Proposed ADR 0041).

## Work required

- Verify end to end that Claude Code started with the documented environment appears in the call log, with session, agent and (if enabled) hint-header fields filled. Capture the fixtures for ADR 0038.
- Write the key naming and issuing procedure for `fm:<task-id>` keys into the runbook (ADR 0037).
- Implement the reporter: ledger parser with duplicate tolerance, task-id extraction, join, and output as table, JSON and CSV.
- Test the reporter with synthetic ledgers and call data, including duplicates, tasks without calls and calls without tasks.
- Document known traffic that bypasses the proxy (fast-mode check, WebFetch domain check, telemetry).

## Depends on

- ADR 0008, ADR 0009, ADR 0037, ADR 0024.

## References

- llm-proxy-design.md, sections 8.1 to 8.4, 9 (phase 5).
- architecture/upstream-connection.md, "Claude Code with a proxy-held API key".
- architecture/backend.md, "Package layout" (later executables under `cmd/`).
