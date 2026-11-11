import { DEFAULT_RANGE, validateTimeRange } from './timeRange'
import type { TimeRangeSearch } from './timeRange'
import {
  asRaw,
  bool,
  compact,
  cursor,
  enumList,
  int,
  oneOf,
  statusList,
  textList,
} from './validate'

// Search parameters per route. Names match the API query parameters, so a
// view passes the validated search to the client with few changes. Views add
// their own parameters in later ADRs; the validators here are the shared part.

/** Call-log criteria (ADR 0031): the filters of GET /admin/calls. */
export interface CallFilterSearch {
  key?: string[]
  client?: string[]
  provider?: string[]
  model?: string[]
  endpoint?: string[]
  status?: number[]
  error_class?: string[]
  session?: string[]
  agent?: string[]
  agent_type?: string[]
  prompt?: string[]
  request_class?: string[]
  tool?: string[]
  streamed?: boolean
  tool_error?: boolean
  latency_ms_gte?: number
  cost_micro_usd_gte?: number
}

const CALL_TEXT_FILTERS = [
  'key',
  'client',
  'provider',
  'model',
  'endpoint',
  'error_class',
  'session',
  'agent',
  'agent_type',
  'prompt',
  'request_class',
  'tool',
] as const

export function validateCallFilters(search: unknown): CallFilterSearch {
  const raw = asRaw(search)
  const out: CallFilterSearch = compact({
    status: statusList(raw.status),
    streamed: bool(raw.streamed),
    tool_error: bool(raw.tool_error),
    latency_ms_gte: int(raw.latency_ms_gte),
    cost_micro_usd_gte: int(raw.cost_micro_usd_gte),
  })
  for (const name of CALL_TEXT_FILTERS) {
    const list = textList(raw[name])
    if (list) out[name] = list
  }
  return out
}

export interface CallsSearch extends TimeRangeSearch, CallFilterSearch {
  cursor?: string
  group?: 'session'
  live?: boolean
}

export function validateCallsSearch(search: unknown): CallsSearch {
  const raw = asRaw(search)
  return compact({
    ...validateTimeRange(raw),
    ...validateCallFilters(raw),
    cursor: cursor(raw.cursor),
    group: oneOf(raw.group, ['session'] as const),
    // Off is the default: only an explicit true is kept.
    live: bool(raw.live) === true ? true : undefined,
  })
}

export const CALL_TABS = ['overview', 'tools', 'usage'] as const
export type CallTab = (typeof CALL_TABS)[number]
export interface CallDetailSearch {
  tab?: CallTab
}
export function validateCallDetailSearch(search: unknown): CallDetailSearch {
  const tab = oneOf(asRaw(search).tab, CALL_TABS)
  return tab === undefined || tab === 'overview' ? {} : { tab }
}

export interface SessionsSearch extends TimeRangeSearch {
  key?: string[]
  client?: string[]
  provider?: string[]
  model?: string[]
  error_class?: string[]
  cost_micro_usd_gte?: number
  cursor?: string
}
export function validateSessionsSearch(search: unknown): SessionsSearch {
  const raw = asRaw(search)
  return compact({
    ...validateTimeRange(raw),
    key: textList(raw.key),
    client: textList(raw.client),
    provider: textList(raw.provider),
    model: textList(raw.model),
    error_class: textList(raw.error_class),
    cost_micro_usd_gte: int(raw.cost_micro_usd_gte),
    cursor: cursor(raw.cursor),
  })
}

export const SESSION_MODES = ['tree', 'time'] as const
export type SessionMode = (typeof SESSION_MODES)[number]
export interface SessionDetailSearch {
  mode?: SessionMode
}
export function validateSessionDetailSearch(search: unknown): SessionDetailSearch {
  const mode = oneOf(asRaw(search).mode, SESSION_MODES)
  return mode === undefined || mode === 'tree' ? {} : { mode }
}

export const TOOL_SOURCES = ['client', 'server'] as const
export const TOOL_CALL_STATUSES = ['paired', 'pending', 'use_only', 'result_only'] as const
export interface ToolsSearch extends TimeRangeSearch {
  session?: string
  agent?: string
  tool?: string[]
  source?: (typeof TOOL_SOURCES)[number][]
  status?: (typeof TOOL_CALL_STATUSES)[number][]
  is_error?: boolean
  duration_ms_gte?: number
  arg_bytes_gte?: number
  result_bytes_gte?: number
  cursor?: string
}
export function validateToolsSearch(search: unknown): ToolsSearch {
  const raw = asRaw(search)
  return compact({
    ...validateTimeRange(raw),
    session: textList(raw.session)?.[0],
    agent: textList(raw.agent)?.[0],
    tool: textList(raw.tool),
    source: enumList(raw.source, TOOL_SOURCES),
    status: enumList(raw.status, TOOL_CALL_STATUSES),
    is_error: bool(raw.is_error),
    duration_ms_gte: int(raw.duration_ms_gte),
    arg_bytes_gte: int(raw.arg_bytes_gte),
    result_bytes_gte: int(raw.result_bytes_gte),
    cursor: cursor(raw.cursor),
  })
}

export const GROUP_BY = ['key', 'model', 'client', 'provider', 'session', 'error_class'] as const
export const GRAINS = ['minute', 'hour', 'day'] as const
export interface CostSearch extends TimeRangeSearch, CallFilterSearch {
  group_by?: (typeof GROUP_BY)[number]
  grain?: (typeof GRAINS)[number]
}
export function validateCostSearch(search: unknown): CostSearch {
  const raw = asRaw(search)
  return compact({
    ...validateTimeRange(raw),
    ...validateCallFilters(raw),
    group_by: oneOf(raw.group_by, GROUP_BY),
    grain: oneOf(raw.grain, GRAINS),
  })
}

/** The overview dashboard (ADR 0052): a preset range and the activity chart metric. */
export const DASHBOARD_RANGES = ['1h', '24h', '7d', '30d'] as const
export type DashboardRange = (typeof DASHBOARD_RANGES)[number]
export const ACTIVITY_METRICS = ['calls', 'cost'] as const
export type ActivityMetric = (typeof ACTIVITY_METRICS)[number]
export interface DashboardSearch {
  range?: DashboardRange
  metric?: ActivityMetric
}
export function validateDashboardSearch(search: unknown): DashboardSearch {
  const raw = asRaw(search)
  const range = oneOf(raw.range, DASHBOARD_RANGES)
  const metric = oneOf(raw.metric, ACTIVITY_METRICS)
  // Defaults stay out of the URL: 24h and calls.
  return compact({
    range: range === DEFAULT_RANGE ? undefined : range,
    metric: metric === 'calls' ? undefined : metric,
  })
}

/** The config page has no search parameters; anything in the URL is dropped. */
export function validateNoSearch(): Record<string, never> {
  return {}
}

export { DEFAULT_RANGE }
