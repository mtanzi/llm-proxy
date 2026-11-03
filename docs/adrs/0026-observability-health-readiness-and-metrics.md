# ADR 0026: Observability: health, readiness and Prometheus metrics

Status: Completed

Date: 2026-10-21

## Context

The proxy's own health is visible through `/healthz`, `/readyz` and `/metrics` (gap review R16). v1 has no alert feed, so dropped calls and writer errors show only in metrics. The data plane must keep working when Postgres is down.

## Decision

- `/healthz` on both listeners: the process is alive.
- `/readyz` on the data plane: config loaded. It is **not** tied to Postgres. On the admin plane: Postgres reachable (`Store.Ping`).
- During shutdown both `/readyz` endpoints report not-ready first.
- Prometheus metrics at `/metrics` on the admin listener, each collector declared in the package that updates it:
  - `llm_proxy_requests_total{family,provider,status,error_class}`
  - `llm_proxy_request_duration_seconds`, `llm_proxy_ttft_seconds`
  - `llm_proxy_proxy_overhead_seconds` (target p99 under 5 ms)
  - `llm_proxy_inflight_streams`
  - `llm_proxy_call_queue_depth`, `llm_proxy_calls_dropped_total{reason}`
  - `llm_proxy_writer_errors_total`, `llm_proxy_live_events_dropped_total`
  - `llm_proxy_config_reloads_total{result}`
  - `llm_proxy_retention_deleted_total`, `llm_proxy_retention_last_run_timestamp_seconds`
- Logs: `log/slog` JSON to stdout, one line per request with the call id. `traceparent` is propagated.

## Consequences

- No label carries a key name, session or model with unbounded values, to keep cardinality bounded. Per-key detail comes from the call log.
- OTLP export and Grafana dashboards over rollups are deferred.

## Work required

- Add the Prometheus registry, the `/metrics` handler on the admin mux and the metric collectors owned by `proxy` (requests, durations, overhead, in-flight).
- Implement the two readiness checks and the shutdown flag.
- Implement the `llm_proxy_proxy_overhead_seconds` measurement: time from request arrival to the upstream request being sent.
- Review label sets for cardinality and document the metric list in the runbook (ADR 0037).
- Tests: readiness while the store is down (data plane ready, admin not ready), readiness flips on shutdown, metric names and labels golden test.

## Depends on

- ADR 0006, ADR 0016, ADR 0017.

## References

- architecture/deployment.md, "Observability".
- architecture/backend.md, "Metrics", "Defaults taken for open gaps" (R16).
