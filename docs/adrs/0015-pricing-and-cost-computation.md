# ADR 0015: Pricing and cost computation

Status: Accepted

Date: 2026-10-21

## Context

Cost per call must stay stable when prices change, and prices live in the config file (decision C3). Usage columns are disjoint, so cost is the sum of each column times its price. In subscription passthrough the plan bills the person, but an API-equivalent figure is still useful for analysis.

## Decision

- `pricing` entries in the config file are versioned by effective date: `provider`, `model`, `from`, and per-million-token prices for `input`, `output`, `cache_read`, `cache_write_5m`, `cache_write_1h`.
- Cost is computed once, when the call finishes, from the pricing snapshot in effect: the sum of each disjoint usage column times its price. The result is stored as `cost_usd` with the `pricing_version` that was used.
- Cost is not recomputed for old rows when prices change.
- A model with no matching pricing entry gets a null cost, not zero. The UI shows unknown, not zero.
- In subscription passthrough the record is labelled `billing_mode = subscription` and the cost is an API-equivalent figure.
- The `Cost` function lives in `core` and is pure.
- The UI page for pricing is read-only; editing pricing is a config edit plus `SIGHUP`.

## Consequences

- Historical numbers stay stable. A pricing mistake is fixed for new calls only, unless `usage_raw` is used to recompute with a one-off script.
- No budgets, caps or enforcement use this cost in v1.

## Work required

- Add the pricing schema and validation to config (ADR 0004): no overlapping effective ranges for one model, non-negative prices.
- Implement the pure `Cost(usage, price)` and the price selection by provider, model and `ts` in `core`.
- Compute `cost_usd` and `pricing_version` in the `proxy` finish step after extraction (ADR 0014).
- Handle `billing_mode` and null cost cases.
- Tests: table tests for each usage mix (cache reads, 5m and 1h writes, reasoning not double counted), a price change between two calls, an unknown model, exactness of decimal arithmetic.
- Expose the pricing list read-only through `GET /admin/config` (ADR 0023).

## Depends on

- ADR 0003, ADR 0004.

## References

- architecture/deployment.md, "Configuration" (`pricing`).
- architecture/backend.md, "Extractor", "Data model" notes.
- architecture/upstream-connection.md, "Technical limits in this mode".
- architecture/README.md, C3.
