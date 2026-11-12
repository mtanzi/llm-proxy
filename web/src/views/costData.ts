import type { CostGroup } from '../search/routes'
import { inputTokens, timeSlots } from './dashboardData'
import type { Grain, UsageBucket, Window } from './dashboardData'
import type { CallSummary } from './callLogRows'
import { tokensIn } from './callLogRows'

// Pure helpers of the cost and cache view (ADR 0035). Numbers come from
// GET /admin/usage and GET /admin/calls. Unknown is not zero: a cost the API
// does not give is null and shows as "unknown".

export const GROUP_LABELS: Record<CostGroup, string> = {
  model: 'model',
  key: 'key',
  client: 'client',
  session: 'session',
}

/** The key-name prefix of a firstmate task key: `fm:<task-id>`. */
export const TASK_KEY_PREFIX = 'fm:'

/** The largest number of points one chart draws. A finer grain than this is coarsened. */
export const MAX_SLOTS = 400

const GRAIN_ORDER: Grain[] = ['minute', 'hour', 'day']

/**
 * The bucket width for a window. A requested grain wins unless it would give
 * more than MAX_SLOTS points, then the next coarser one is used. Without a
 * request: minutes up to 6 hours, hours up to 7 days, days beyond.
 */
export function costGrain(window: Window, requested?: Grain): Grain {
  const span = Date.parse(window.to) - Date.parse(window.from)
  let grain: Grain = requested ?? (span <= 6 * 3_600_000 ? 'minute' : span <= 7 * 86_400_000 ? 'hour' : 'day')
  while (grain !== 'day' && timeSlots(window, grain).length > MAX_SLOTS) {
    grain = GRAIN_ORDER[GRAIN_ORDER.indexOf(grain) + 1]
  }
  return grain
}

export interface CostRow {
  /** The group value; empty when not known. */
  group: string
  calls: number
  /** Known cost; null when every call is unpriced. */
  cost: number | null
  unpriced: number
  /** cache_read over all input columns; null without input tokens. */
  cacheHit: number | null
}

/** Sums a grouped series over time, one row per group, with the cache hit ratio of the group. */
export function costRows(buckets: UsageBucket[]): CostRow[] {
  const by = new Map<string, { calls: number; cost: number; unpriced: number; input: number; cacheRead: number }>()
  for (const b of buckets) {
    const row = by.get(b.group) ?? { calls: 0, cost: 0, unpriced: 0, input: 0, cacheRead: 0 }
    row.calls += b.calls
    row.cost += b.cost_micro_usd
    row.unpriced += b.unpriced_calls
    row.input += inputTokens(b.usage)
    row.cacheRead += b.usage.cache_read
    by.set(b.group, row)
  }
  return [...by.entries()].map(([group, r]) => ({
    group,
    calls: r.calls,
    // Every call unpriced: the cost is unknown, not zero.
    cost: r.calls > 0 && r.unpriced === r.calls ? null : r.cost,
    unpriced: r.unpriced,
    cacheHit: r.input > 0 ? r.cacheRead / r.input : null,
  }))
}

/** Highest known cost first; unknown cost after every known one; then by calls and name. */
export function sortByCost<T extends { group: string; calls: number; cost: number | null }>(rows: T[]): T[] {
  return [...rows].sort((a, b) => {
    if (a.cost === null || b.cost === null) {
      if (a.cost !== b.cost) return a.cost === null ? 1 : -1
    } else if (a.cost !== b.cost) {
      return b.cost - a.cost
    }
    return b.calls - a.calls || a.group.localeCompare(b.group)
  })
}

/** The known cost of the rows: the base of the share column. */
export function knownCost(rows: { cost: number | null }[]): number {
  return rows.reduce((sum, r) => sum + (r.cost ?? 0), 0)
}

/** A row's share of the known cost, as a whole percent; null for an unknown cost or no known cost. */
export function costShare(cost: number | null, total: number): number | null {
  return cost === null || total <= 0 ? null : Math.round((cost / total) * 100)
}

export interface TaskRow extends CostRow {
  /** The firstmate task id: the key name without its `fm:` prefix. */
  taskId: string
}

/** Cost per firstmate task: the rows of a series grouped by key whose name starts with `fm:`. */
export function taskRows(rows: CostRow[]): TaskRow[] {
  return sortByCost(
    rows
      .filter((r) => r.group.startsWith(TASK_KEY_PREFIX) && r.group.length > TASK_KEY_PREFIX.length)
      .map((r) => ({ ...r, taskId: r.group.slice(TASK_KEY_PREFIX.length) })),
  )
}

/** Sessions ranked by cost. Calls without a session (group "") are not a session. */
export function topSessions(rows: CostRow[], limit: number): CostRow[] {
  return sortByCost(rows.filter((r) => r.group !== '')).slice(0, limit)
}

export interface PromptRow {
  promptId: string
  sessionKey: string
  firstTs: string
  calls: number
  /** Known cost; null when every call of the prompt is unpriced. */
  cost: number | null
  unpriced: number
  tokensIn: number
  /** At least one call went through a client_passthrough provider: the cost is API-equivalent. */
  apiEquivalent: boolean
}

export interface PromptCosts {
  prompts: PromptRow[]
  /** Calls that carry no prompt id; they belong to no prompt. */
  withoutPrompt: number
}

/**
 * Cost per prompt from the calls of a page of GET /admin/calls: the calls that
 * share a prompt id, highest known cost first. The usage API cannot group by
 * prompt, so this covers the calls given and no more.
 */
export function promptCosts(calls: CallSummary[], passthrough: Set<string>): PromptCosts {
  const by = new Map<string, PromptRow & { known: number }>()
  let withoutPrompt = 0
  for (const c of calls) {
    const id = c.prompt_id ?? ''
    if (id === '') {
      withoutPrompt++
      continue
    }
    const row = by.get(id) ?? {
      promptId: id,
      sessionKey: c.session_key,
      firstTs: c.ts,
      calls: 0,
      cost: null,
      known: 0,
      unpriced: 0,
      tokensIn: 0,
      apiEquivalent: false,
    }
    row.calls++
    if (c.cost_micro_usd === null) row.unpriced++
    else row.known += c.cost_micro_usd
    row.tokensIn += tokensIn(c)
    if (c.ts < row.firstTs) row.firstTs = c.ts
    if (c.cost_micro_usd !== null && passthrough.has(c.provider)) row.apiEquivalent = true
    by.set(id, row)
  }
  const prompts = [...by.values()].map(({ known, ...row }) => ({
    ...row,
    // Every call unpriced: the cost is unknown, not zero.
    cost: row.unpriced === row.calls ? null : known,
  }))
  return {
    prompts: [...prompts].sort(
      (a, b) =>
        (a.cost === null ? 1 : 0) - (b.cost === null ? 1 : 0) ||
        (b.cost ?? 0) - (a.cost ?? 0) ||
        b.calls - a.calls ||
        a.promptId.localeCompare(b.promptId),
    ),
    withoutPrompt,
  }
}

/** One row of a chart's table fallback: the interval and one value per series. */
export interface ChartRow {
  start: number
  values: (number | null)[]
}

/** Rows for the intervals with at least one value above zero, so an empty range is not a wall of zeros. */
export function chartRows(slots: number[], series: { values: (number | null)[] }[]): ChartRow[] {
  return slots
    .map((start, i) => ({ start, values: series.map((s) => s.values[i] ?? null) }))
    .filter((r) => r.values.some((v) => v !== null && v > 0))
}
