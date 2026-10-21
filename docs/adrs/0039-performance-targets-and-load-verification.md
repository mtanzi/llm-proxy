# ADR 0039: Performance targets and load verification

Status: Accepted

Date: 2026-10-21

## Context

The design states targets (gap review R15): added p99 overhead under 5 ms before the first byte, 200 concurrent streams on one small VM, and a 32 MiB maximum body. Several figures are estimates: the cost of incremental scanning, row size, query times at volume. They need measuring before the sizing table can be trusted.

## Decision

- Build a small load tool that drives the proxy with synthetic streams against the fake upstream (ADR 0038).
- Verify: p99 `llm_proxy_proxy_overhead_seconds` under 5 ms; 200 concurrent streams on a 1 vCPU, 512 MB VM without dropped records; memory with many 32 MiB bodies bounded; writer lag stays flat well above the expected 0.14 to 0.58 calls per second.
- Measure database figures with generated data: bytes per `calls` and `tool_events` row including indexes, 24-hour list time, 30-day aggregate over about 1.5 million rows, retention batch time.
- Record results in the sizing section of the deployment documents and in a `docs/performance.md` note. Add a CI benchmark guard for the tap and extractor.
- Triggers for the deferred items (`CopyFrom` staging, rollups, `sync.Pool`) stay as documented; this ADR only produces the numbers.

## Consequences

- A failed target blocks the v1 release or is recorded as a known limit.
- Estimates in the sizing table are replaced by measured values.

## Work required

- Write the load tool (Go) with configurable stream count, event rate and body size.
- Write Go benchmarks for the SSE scanner and extractor with allocation counts.
- Write the data generator and measurement script for the database figures.
- Run the measurements, update the sizing table, and file follow-ups if a target fails.
- Add a regression benchmark job (non-blocking at first).

## Depends on

- ADR 0011, ADR 0013, ADR 0038.

## References

- architecture/backend.md, "Defaults taken for open gaps" (R15), "Tap".
- architecture/deployment.md, "Sizing".
