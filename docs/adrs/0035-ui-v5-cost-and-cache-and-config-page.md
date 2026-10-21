# ADR 0035: UI view V5: cost and cache, and the read-only config page

Status: Accepted

Date: 2026-10-21

## Context

The cost view answers who spent what and how well the cache works. The config page shows what the config file says, read-only, because keys, providers and pricing change by editing the file and sending `SIGHUP`.

## Decision

- **Cost and cache** at `/ui/cost`: cost by key, model, client and session; top sessions by cost; cost per prompt; cost per firstmate task (key name `fm:<task-id>`); cache hit ratio over time. Numbers come from `GET /admin/usage` and `GET /admin/requests`. Long ranges can take seconds.
- The cache-miss view (cache strip, cache tab, misses with cause) is deferred.
- **Config page** at `/ui/admin/config`: providers, pricing and key names with `disabled`, from `GET /admin/config`. No hashes, no credentials. No key management and no editing; the page explains how to change keys (config edit and `SIGHUP`).
- Unknown cost (no pricing entry) shows as unknown, not zero; subscription calls show an API-equivalent label.

## Consequences

- Grafana dashboards over rollups are later items.
- Per-key usage is grouped by key name, so renamed keys split history.

## Work required

- Implement the charts (time series by group, cache ratio) with accessible colours and a table fallback.
- Implement the top lists and the per-task cost grouping by key-name prefix.
- Implement the config page with the read-only tables.
- Tests: grouping and formatting, unknown cost handling, loading states, no hash rendered.

## Depends on

- ADR 0024, ADR 0030.

## References

- architecture/frontend.md, "Views" (V5, Admin), "V5 Cost and cache".
- architecture/backend.md, "Admin and query API" (`/admin/config`).
