import { describe, expect, test } from 'vitest'
import { validateToolsSearch } from '../search/routes'
import {
  activeToolFilterCount,
  isUnmatchedOnly,
  knownDurations,
  pairingLabel,
  sortingSearch,
  summarySorting,
  summaryTotals,
  toolCallRows,
  toolCallTarget,
  toolCallTime,
  toolFilterQuery,
} from './toolsData'
import type { ToolCall, ToolStat } from './toolsData'

export function toolCall(id: string, over: Partial<ToolCall> = {}): ToolCall {
  return {
    tool_call_id: id,
    name: 'Bash',
    source: 'client',
    status: 'paired',
    session_key: 's1',
    agent_id: 'main',
    use_call_id: '0190a1b2-0000-7000-8000-000000000001',
    result_call_id: '0190a1b2-0000-7000-8000-000000000002',
    use_ts: '2026-11-01T10:00:00Z',
    result_ts: '2026-11-01T10:00:02Z',
    duration_ms: 1500,
    duration_source: 'header',
    is_error: false,
    arg_bytes: 120,
    result_bytes: 2048,
    ...over,
  }
}

export function toolStat(name: string, over: Partial<ToolStat> = {}): ToolStat {
  return {
    name,
    agent_type: '',
    count: 10,
    errors: 0,
    error_rate: 0,
    duration_p50_ms: 100,
    duration_p95_ms: 900,
    total_duration_ms: 2000,
    ...over,
  }
}

describe('filters', () => {
  test('the filter query keeps the API filters and drops view state', () => {
    const search = validateToolsSearch({
      range: '7d',
      tool: ['Read'],
      status: ['use_only', 'result_only'],
      is_error: 'true',
      duration_ms_gte: '500',
      cursor: 'abc',
      sort: 'p95',
    })
    expect(toolFilterQuery(search)).toEqual({
      tool: ['Read'],
      status: ['use_only', 'result_only'],
      is_error: true,
      duration_ms_gte: 500,
    })
    expect(activeToolFilterCount(search)).toBe(4)
  })

  test('unmatched only means exactly the two unmatched halves', () => {
    expect(isUnmatchedOnly(['use_only', 'result_only'])).toBe(true)
    expect(isUnmatchedOnly(['result_only', 'use_only'])).toBe(true)
    expect(isUnmatchedOnly(['use_only'])).toBe(false)
    expect(isUnmatchedOnly(['use_only', 'result_only', 'pending'])).toBe(false)
    expect(isUnmatchedOnly(undefined)).toBe(false)
  })
})

describe('summary sort', () => {
  test('the default is count, most first, and stays out of the URL', () => {
    expect(summarySorting({})).toEqual([{ id: 'count', desc: true }])
    expect(sortingSearch([{ id: 'count', desc: true }])).toEqual({ sort: undefined, order: undefined })
    expect(sortingSearch([])).toEqual({ sort: undefined, order: undefined })
  })

  test('text columns sort ascending first, numbers descending first', () => {
    expect(summarySorting({ sort: 'name' })).toEqual([{ id: 'name', desc: false }])
    expect(summarySorting({ sort: 'p95' })).toEqual([{ id: 'p95', desc: true }])
    expect(sortingSearch([{ id: 'name', desc: false }])).toEqual({ sort: 'name', order: undefined })
    expect(sortingSearch([{ id: 'name', desc: true }])).toEqual({ sort: 'name', order: 'desc' })
    expect(sortingSearch([{ id: 'count', desc: false }])).toEqual({ sort: undefined, order: 'asc' })
  })

  test('URL and table state round trip', () => {
    for (const raw of [{ sort: 'total' }, { sort: 'agent_type', order: 'desc' }, { order: 'asc' }, {}]) {
      const search = validateToolsSearch(raw)
      expect(validateToolsSearch(sortingSearch(summarySorting(search)))).toEqual(search)
    }
  })

  test('the validator drops unknown sorts and default orders', () => {
    expect(validateToolsSearch({ sort: 'cost', order: 'up' })).toEqual({})
    expect(validateToolsSearch({ sort: 'count', order: 'desc' })).toEqual({})
    expect(validateToolsSearch({ sort: 'name', order: 'asc' })).toEqual({ sort: 'name' })
    expect(validateToolsSearch({ sort: 'p50', order: 'asc' })).toEqual({ sort: 'p50', order: 'asc' })
  })
})

describe('summary rows', () => {
  test('a row with no duration at all reads unknown, not zero', () => {
    const none = toolStat('Task', { duration_p50_ms: 0, duration_p95_ms: 0, total_duration_ms: 0 })
    expect(knownDurations(none)).toEqual({ p50: null, p95: null, total: null })
    expect(knownDurations(toolStat('Read'))).toEqual({ p50: 100, p95: 900, total: 2000 })
  })

  test('totals add counts and errors and count distinct names', () => {
    const totals = summaryTotals([
      toolStat('Bash', { count: 6, errors: 3 }),
      toolStat('Bash', { agent_type: 'reviewer', count: 4, errors: 1 }),
      toolStat('', { count: 2 }),
    ])
    expect(totals).toEqual({ calls: 12, errors: 4, errorRate: 4 / 12, tools: 1 })
    expect(summaryTotals([]).errorRate).toBeNull()
  })
})

describe('list rows', () => {
  test('a result with no use is timed and opened by its result', () => {
    const t = toolCall('r1', {
      status: 'result_only',
      name: '',
      use_call_id: null,
      use_ts: null,
      duration_ms: null,
      duration_source: null,
    })
    expect(toolCallTime(t)).toBe('2026-11-01T10:00:02Z')
    expect(toolCallTarget(t)).toBe('0190a1b2-0000-7000-8000-000000000002')
    expect(toolCallTarget(toolCall('u1'))).toBe('0190a1b2-0000-7000-8000-000000000001')
  })

  test('unmatched states have their own label; a paired call has none', () => {
    expect(pairingLabel('paired')).toBeNull()
    expect(pairingLabel('pending')).toBe('Pending')
    expect(pairingLabel('use_only')).toBe('No result')
    expect(pairingLabel('result_only')).toBe('No use')
  })

  test('row ids are unique across pages', () => {
    const rows = toolCallRows([[toolCall('a'), toolCall('b')], [toolCall('a')]])
    expect(new Set(rows.map((r) => r.id)).size).toBe(3)
  })
})
