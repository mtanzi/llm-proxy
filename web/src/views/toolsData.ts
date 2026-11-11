import type { SortingState } from '@tanstack/react-table'
import type { ResponseOf } from '../api/client'
import {
  DEFAULT_TOOL_SORT,
  TOOL_SUMMARY_SORTS,
  defaultSortOrder,
} from '../search/routes'
import type { ToolSummarySort, ToolsSearch } from '../search/routes'

// Data helpers of the tool-call explorer (ADR 0034). The page shows names,
// sizes, durations and outcomes only: no argument or result text is stored
// (ADR 0043), so none is read here.

export type ToolCall = ResponseOf<'/admin/tool-calls'>['tool_calls'][number]
export type ToolStat = ResponseOf<'/admin/tool-calls/summary'>['tools'][number]
export type PairingStatus = ToolCall['status']

/** The search parameters that are filters of both tool-call endpoints. */
export const TOOL_FILTER_KEYS = [
  'session',
  'tool',
  'status',
  'is_error',
  'duration_ms_gte',
  'arg_bytes_gte',
  'result_bytes_gte',
] as const satisfies readonly (keyof ToolsSearch)[]

type ToolFilters = Pick<ToolsSearch, (typeof TOOL_FILTER_KEYS)[number]>

/** The API filters of a search: names match the query parameters one to one. */
export function toolFilterQuery(search: ToolsSearch): ToolFilters {
  const out: Record<string, unknown> = {}
  for (const key of TOOL_FILTER_KEYS) {
    if (search[key] !== undefined) out[key] = search[key]
  }
  return out as ToolFilters
}

export function activeToolFilterCount(search: ToolsSearch): number {
  return TOOL_FILTER_KEYS.filter((k) => search[k] !== undefined).length
}

/** The unmatched halves: a use with no result after an hour, and a result with no use. */
export const UNMATCHED: readonly PairingStatus[] = ['use_only', 'result_only']

/** Whether the pairing filter is exactly the two unmatched halves. */
export function isUnmatchedOnly(status: readonly PairingStatus[] | undefined): boolean {
  return status !== undefined && status.length === UNMATCHED.length && UNMATCHED.every((s) => status.includes(s))
}

/** The summary sort in the URL as TanStack sorting state. */
export function summarySorting(search: Pick<ToolsSearch, 'sort' | 'order'>): SortingState {
  const id = search.sort ?? DEFAULT_TOOL_SORT
  return [{ id, desc: (search.order ?? defaultSortOrder(id)) === 'desc' }]
}

/** TanStack sorting state back to the URL; defaults are left out. */
export function sortingSearch(sorting: SortingState): Pick<ToolsSearch, 'sort' | 'order'> {
  const first = sorting[0]
  const id = (TOOL_SUMMARY_SORTS as readonly string[]).includes(first?.id ?? '')
    ? (first.id as ToolSummarySort)
    : DEFAULT_TOOL_SORT
  const order = first?.desc === false ? 'asc' : first ? 'desc' : defaultSortOrder(id)
  return {
    sort: id === DEFAULT_TOOL_SORT ? undefined : id,
    order: order === defaultSortOrder(id) ? undefined : order,
  }
}

/**
 * A summary row's duration figures. The API reads zero when no tool call of
 * the row has a duration (only unmatched halves, for example), so a row whose
 * figures are all zero shows them as unknown rather than as 0 ms.
 */
export function knownDurations(s: ToolStat): { p50: number | null; p95: number | null; total: number | null } {
  const none = s.duration_p50_ms === 0 && s.duration_p95_ms === 0 && s.total_duration_ms === 0
  return none
    ? { p50: null, p95: null, total: null }
    : { p50: s.duration_p50_ms, p95: s.duration_p95_ms, total: s.total_duration_ms }
}

export interface ToolTotals {
  calls: number
  errors: number
  errorRate: number | null
  tools: number
}

/** Totals over the summary rows: tool calls, errors and distinct tool names. */
export function summaryTotals(rows: ToolStat[]): ToolTotals {
  const calls = rows.reduce((sum, r) => sum + r.count, 0)
  const errors = rows.reduce((sum, r) => sum + r.errors, 0)
  const names = new Set(rows.map((r) => r.name).filter((n) => n !== ''))
  return { calls, errors, errorRate: calls > 0 ? errors / calls : null, tools: names.size }
}

/** The time of a tool call: the use, or the result of a result-only call. */
export function toolCallTime(t: ToolCall): string | null {
  return t.use_ts ?? t.result_ts
}

/** The call a row opens in V3: the call that requested the tool, or the one that carried the result. */
export function toolCallTarget(t: ToolCall): string | null {
  return t.use_call_id ?? t.result_call_id
}

/**
 * Stable, unique row ids. A provider tool-call id is unique, but a result can
 * be carried again by a retried request, so the two calls are part of the id,
 * and a repeat across pages gets a suffix.
 */
export function toolCallRows(pages: ToolCall[][]): { id: string; call: ToolCall }[] {
  const seen = new Map<string, number>()
  return pages.flat().map((call) => {
    const base = `${call.tool_call_id}|${call.use_call_id ?? ''}|${call.result_call_id ?? ''}`
    const n = seen.get(base) ?? 0
    seen.set(base, n + 1)
    return { id: n === 0 ? base : `${base}#${n}`, call }
  })
}

/** Text of the pairing state for a call that is not paired; null when paired. */
export function pairingLabel(status: PairingStatus): string | null {
  switch (status) {
    case 'pending':
      return 'Pending'
    case 'use_only':
      return 'No result'
    case 'result_only':
      return 'No use'
    default:
      return null
  }
}
