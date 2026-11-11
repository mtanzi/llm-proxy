# ADR 0052: UI overview dashboard as the landing page

Status: Accepted

Date: 2026-11-11

## Context

`/ui` redirected to the call log (`/ui/calls`, ADR 0031). The call log opens on a table. Its top has a five-total summary strip and a single-series volume chart. To see how the proxy is doing, a reader had to read totals without a comparison, then visit the session list and the call log filtered by error class. ADR 0035 plans the cost and cache deep-dive at `/ui/cost`. That page answers "who spent what", not "is anything wrong right now".

## Decision

- **Overview dashboard at `/ui`**, the landing page. The redirect to `/calls` is removed. An unknown path falls back to the overview. The sidebar lists "Overview" first. The call log stays one click away in the sidebar, and every other route is unchanged.
- **Time range:** presets 1h, 24h (default), 7d and 30d, in the `range` search parameter. There is no absolute window. The activity chart metric (`metric=cost`; calls is the default) is also a search parameter.
- **Headline numbers:** calls, cost, tokens in, tokens out, error rate, cache hit ratio, p50 and p95 latency. Each has a sparkline and the change against the previous equal period (the same length, ending where the range starts). Counts, cost and latency change as a percentage; error rate and cache hit change in percentage points. A change carries a glyph, a sign and words, so it does not rely on colour.
- **Activity:** calls or cost per interval, stacked by model (the top models, the rest folded into "other"), with errors per interval drawn over the bars as a line on their own scale. Every series has a named legend entry and every bar a tooltip.
- **Live feed:** the latest finished calls (time, model, status, tokens, cost) from `GET /admin/calls`, with new calls from `GET /admin/stream` through the shared live hook.
- **Recent sessions:** the latest sessions of the range with duration, calls, cost and an error badge, linking to the session view.
- **Problems:** errors of the range grouped by error class (ADR 0007), each linking to the call log filtered to that class and range. No errors gives a calm empty state.
- **Breakdowns:** cost ranked by model, client and key, and the top 5 sessions by cost. Each links to the cost page (ADR 0035), the call log or the session view.
- **Frontend only.** Every number comes from the existing API: `GET /admin/usage` (ungrouped for the current and the previous period, and grouped by model, client, key, session, error class and provider), `/admin/sessions`, `/admin/calls`, `/admin/stream` and `/admin/config`. There is no API change.
- **Unknown is not zero.** Cost is unknown when every call of a group is unpriced. A cost that includes calls through a `client_passthrough` provider carries the "API-equivalent" label. The provider comes from `GET /admin/config` and from the usage series grouped by provider.
- Not included: alert thresholds or banners, and the command palette (issue 65).

## Consequences

- **p50 and p95 are approximate over several buckets.** The usage API gives percentiles per bucket only, and percentiles cannot be summed. The headline shows the call-weighted mean of the bucket percentiles, marked "≈" with a tooltip. An exact percentile over the range needs an API change, for example an ungrouped single-bucket mode of `/admin/usage`.
- The problems panel groups by error class, not by HTTP status. The usage API has no `group_by=status`, so each class label names its usual statuses (for example "Rate limited (429)", "Upstream 5xx (500, 529 overloaded)").
- The API-equivalent label covers the headline cost, the feed rows and a note on the breakdowns. Session and per-key rows carry no per-row label, because their summaries do not say which provider served them.
- The page sends about ten small requests per range. Each usage query sums raw call rows (ADR 0021), so a 30-day range takes longer than a short one.
- ADR 0031 called the call log the home view. This ADR moves that role to the overview and leaves the call log unchanged.

## Work required

- Route `/` with its search validator, the sidebar entry, and the fallback for unknown paths.
- The dashboard view, the sparkline and the stacked activity chart, and the pure helpers for totals, the previous period, changes, grouping and ranking.
- Tests: the previous-period comparison, error grouping and its links, unknown cost, the API-equivalent label, the route change, and an axe check.

## Depends on

- ADR 0024, ADR 0025, ADR 0030, ADR 0031, ADR 0033.

## References

- architecture/frontend.md, "Views" (Overview).
- ADR 0035 (cost and cache deep-dive), ADR 0012 and ADR 0047 (API-equivalent cost label).
