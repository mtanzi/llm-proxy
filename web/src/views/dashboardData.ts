import type { ResponseOf } from '../api/client'
import type { DashboardRange } from '../search/routes'

// Pure helpers of the overview dashboard (ADR 0052). Every number comes from
// GET /admin/usage, /admin/sessions, /admin/calls and /admin/config. Unknown
// is not zero: a value the API does not give is null and shows as "unknown".

export type UsageBucket = ResponseOf<'/admin/usage'>['buckets'][number]
export type Grain = 'minute' | 'hour' | 'day'
export interface Window {
  from: string
  to: string
}

/** Bucket width per range: about 24 to 168 points, enough for a sparkline. */
export const RANGE_GRAIN: Record<DashboardRange, Grain> = {
  '1h': 'minute',
  '24h': 'hour',
  '7d': 'hour',
  '30d': 'day',
}

const GRAIN_MS: Record<Grain, number> = { minute: 60_000, hour: 3_600_000, day: 86_400_000 }

/** The equal period just before a window: same length, ending where the window starts. */
export function previousWindow(window: Window): Window {
  const span = Date.parse(window.to) - Date.parse(window.from)
  return { from: new Date(Date.parse(window.from) - span).toISOString(), to: window.from }
}

/** Slot starts of a window, one per interval, aligned in UTC as the usage buckets are. */
export function timeSlots(window: Window, grain: Grain): number[] {
  const step = GRAIN_MS[grain]
  const end = Date.parse(window.to)
  const slots: number[] = []
  for (let t = Math.floor(Date.parse(window.from) / step) * step; t < end; t += step) slots.push(t)
  return slots
}

export function inputTokens(usage: UsageBucket['usage']): number {
  return usage.input_uncached + usage.cache_read + usage.cache_write_5m + usage.cache_write_1h
}

export interface Totals {
  calls: number
  errors: number
  tokensIn: number
  tokensOut: number
  /** Known cost; null when every call is unpriced. */
  cost: number | null
  unpriced: number
  /** Errors over calls; null without calls. */
  errorRate: number | null
  /** cache_read over all input columns; null without input tokens. */
  cacheHit: number | null
  /** Call-weighted mean of the bucket percentiles; null when no call has a duration. */
  p50: number | null
  p95: number | null
  /** The percentiles are exact only when one bucket holds every call with a duration. */
  latencyExact: boolean
}

/** Totals over buckets. Percentiles cannot be summed, so p50 and p95 are approximate over several buckets. */
export function totals(buckets: UsageBucket[]): Totals {
  let calls = 0, errors = 0, tokensIn = 0, tokensOut = 0, cost = 0, unpriced = 0, cacheRead = 0
  let timed = 0, p50 = 0, p95 = 0, timedBuckets = 0
  for (const b of buckets) {
    calls += b.calls
    errors += b.errors
    tokensIn += inputTokens(b.usage)
    tokensOut += b.usage.output
    cacheRead += b.usage.cache_read
    cost += b.cost_micro_usd
    unpriced += b.unpriced_calls
    // A zero percentile means no call of the bucket has a duration.
    if (b.latency_p50_ms > 0 && b.calls > 0) {
      timed += b.calls
      p50 += b.latency_p50_ms * b.calls
      p95 += b.latency_p95_ms * b.calls
      timedBuckets++
    }
  }
  return {
    calls,
    errors,
    tokensIn,
    tokensOut,
    cost: calls > 0 && unpriced === calls ? null : cost,
    unpriced,
    errorRate: calls > 0 ? errors / calls : null,
    cacheHit: tokensIn > 0 ? cacheRead / tokensIn : null,
    p50: timed > 0 ? Math.round(p50 / timed) : null,
    p95: timed > 0 ? Math.round(p95 / timed) : null,
    latencyExact: timedBuckets <= 1,
  }
}

export type Measure = 'calls' | 'cost' | 'tokensIn' | 'tokensOut' | 'errorRate' | 'cacheHit' | 'p50' | 'p95'

function bucketValue(b: UsageBucket, measure: Measure): number | null {
  switch (measure) {
    case 'calls':
      return b.calls
    case 'cost':
      return b.calls > 0 && b.unpriced_calls === b.calls ? null : b.cost_micro_usd
    case 'tokensIn':
      return inputTokens(b.usage)
    case 'tokensOut':
      return b.usage.output
    case 'errorRate':
      return b.calls > 0 ? b.errors / b.calls : null
    case 'cacheHit':
      return b.cache_hit_ratio
    case 'p50':
      return b.latency_p50_ms > 0 ? b.latency_p50_ms : null
    case 'p95':
      return b.latency_p95_ms > 0 ? b.latency_p95_ms : null
  }
}

const ADDITIVE: ReadonlySet<Measure> = new Set(['calls', 'cost', 'tokensIn', 'tokensOut'])

/**
 * One value per slot of an ungrouped series. An empty slot is 0 for a sum
 * (calls, cost, tokens) and null for a ratio or a percentile, which has no value without calls.
 */
export function slotSeries(buckets: UsageBucket[], slots: number[], measure: Measure): (number | null)[] {
  const byStart = new Map(buckets.map((b) => [Date.parse(b.start), b]))
  return slots.map((t) => {
    const b = byStart.get(t)
    if (b) return bucketValue(b, measure)
    return ADDITIVE.has(measure) ? 0 : null
  })
}

export type Change =
  | { kind: 'unknown' }
  | { kind: 'same' }
  | { kind: 'new' }
  | { kind: 'up' | 'down'; value: number }

/**
 * The change against the previous period. `relative` gives a fraction of the
 * previous value (counts, cost, latency); `points` gives the difference of two
 * ratios (error rate, cache hit). Unknown on either side is unknown.
 */
export function compare(current: number | null, previous: number | null, mode: 'relative' | 'points'): Change {
  if (current === null || previous === null) return { kind: 'unknown' }
  let diff: number
  if (mode === 'points') {
    diff = current - previous
  } else if (previous === 0) {
    return current === 0 ? { kind: 'same' } : { kind: 'new' }
  } else {
    diff = (current - previous) / previous
  }
  if (Math.abs(diff) < 0.0005) return { kind: 'same' }
  return { kind: diff > 0 ? 'up' : 'down', value: Math.abs(diff) }
}

/** The change as text: "+12.5%", "−0.4 pts", "new", "no change" or "unknown". */
export function formatChange(change: Change, mode: 'relative' | 'points'): string {
  switch (change.kind) {
    case 'unknown':
      return 'unknown'
    case 'same':
      return 'no change'
    case 'new':
      return 'new'
    default: {
      const sign = change.kind === 'up' ? '+' : '−'
      return mode === 'points'
        ? `${sign}${(change.value * 100).toFixed(1)} pts`
        : `${sign}${(change.value * 100).toFixed(change.value < 0.1 ? 1 : 0)}%`
    }
  }
}

export interface GroupTotal {
  /** The group value; empty when not known. */
  group: string
  calls: number
  errors: number
  cost: number | null
  unpriced: number
}

/** Sums a grouped series over time, one row per group. */
export function groupTotals(buckets: UsageBucket[]): GroupTotal[] {
  const by = new Map<string, GroupTotal>()
  for (const b of buckets) {
    const row = by.get(b.group) ?? { group: b.group, calls: 0, errors: 0, cost: 0, unpriced: 0 }
    row.calls += b.calls
    row.errors += b.errors
    row.cost = (row.cost ?? 0) + b.cost_micro_usd
    row.unpriced += b.unpriced_calls
    by.set(b.group, row)
  }
  return [...by.values()].map(({ group, calls, errors, cost, unpriced }) => ({
    group,
    calls,
    errors,
    // Every call unpriced: the cost is unknown, not zero.
    cost: calls > 0 && unpriced === calls ? null : cost,
    unpriced,
  }))
}

/** Ranked by cost, highest first; unknown cost after every known one, then by calls. */
export function rankByCost(rows: GroupTotal[], limit = Infinity): GroupTotal[] {
  return [...rows]
    .sort((a, b) => {
      if (a.cost === null || b.cost === null) {
        if (a.cost !== b.cost) return a.cost === null ? 1 : -1
      } else if (a.cost !== b.cost) {
        return b.cost - a.cost
      }
      return b.calls - a.calls || a.group.localeCompare(b.group)
    })
    .slice(0, limit)
}

/** Readable names of the error classes (ADR 0007), with the usual HTTP status. */
export const ERROR_CLASS_LABELS: Record<string, string> = {
  rate_limited: 'Rate limited (429)',
  upstream_5xx: 'Upstream 5xx (500, 529 overloaded)',
  timeout: 'Timeout',
  auth: 'Auth (401, 403)',
  client_abort: 'Client abort',
  invalid_request: 'Invalid request (400)',
  internal: 'Proxy internal error',
}

export interface Problem {
  errorClass: string
  label: string
  calls: number
}

/**
 * Errors of the range grouped by error class, most first, from a series with
 * group_by=error_class. Calls without an error class (group "") are not problems.
 */
export function errorGroups(buckets: UsageBucket[]): Problem[] {
  return groupTotals(buckets)
    .filter((g) => g.group !== '' && g.errors > 0)
    .map((g) => ({ errorClass: g.group, label: ERROR_CLASS_LABELS[g.group] ?? g.group, calls: g.errors }))
    .sort((a, b) => b.calls - a.calls || a.errorClass.localeCompare(b.errorClass))
}

/** The call-log search that lists the calls of one problem in the same range. */
export function problemSearch(errorClass: string, range: DashboardRange | undefined) {
  return { error_class: [errorClass], range }
}

export interface StackSeries {
  name: string
  values: number[]
  /** The fold of every model past the top ones. */
  other?: boolean
}

export const STACK_LIMIT = 5
export const OTHER = 'other'

/**
 * A series grouped by model as stacks per slot: the top models by total
 * value, the rest folded into "other". Errors are summed per slot over every model.
 */
export function stackByGroup(
  buckets: UsageBucket[],
  slots: number[],
  metric: 'calls' | 'cost',
  limit = STACK_LIMIT,
): { series: StackSeries[]; errors: number[] } {
  const index = new Map(slots.map((t, i) => [t, i]))
  const value = (b: UsageBucket) => (metric === 'calls' ? b.calls : b.cost_micro_usd)
  const totalsByGroup = new Map<string, number>()
  for (const b of buckets) totalsByGroup.set(b.group, (totalsByGroup.get(b.group) ?? 0) + value(b))
  const ranked = [...totalsByGroup.entries()]
    .filter(([, v]) => v > 0)
    .sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))
    .map(([g]) => g)
  const top = ranked.slice(0, ranked.length > limit ? limit - 1 : limit)
  const named = new Map(top.map((g) => [g, Array<number>(slots.length).fill(0)]))
  const other = Array<number>(slots.length).fill(0)
  const errors = Array<number>(slots.length).fill(0)
  for (const b of buckets) {
    const i = index.get(Date.parse(b.start))
    if (i === undefined) continue
    errors[i] += b.errors
    const row = named.get(b.group) ?? other
    row[i] += value(b)
  }
  const series: StackSeries[] = top.map((g) => ({ name: g === '' ? 'unknown' : g, values: named.get(g)! }))
  if (other.some((v) => v > 0)) series.push({ name: OTHER, values: other, other: true })
  return { series, errors }
}

export type ProviderView = ResponseOf<'/admin/config'>['providers'][number]

/** Names of the providers whose calls use the client's own credential (ADR 0012, 0047). */
export function passthroughProviders(providers: ProviderView[] | undefined): Set<string> {
  return new Set((providers ?? []).filter((p) => p.auth_mode === 'client_passthrough').map((p) => p.name))
}

/**
 * The part of a cost that is an API-equivalent figure: the known cost of the
 * calls through client_passthrough providers, from a series with group_by=provider.
 */
export function apiEquivalentCost(byProvider: UsageBucket[], passthrough: Set<string>): number {
  return byProvider.filter((b) => passthrough.has(b.group)).reduce((sum, b) => sum + b.cost_micro_usd, 0)
}
