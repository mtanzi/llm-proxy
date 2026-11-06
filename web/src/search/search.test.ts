import { describe, expect, test } from 'vitest'
import {
  validateCallDetailSearch,
  validateCallsSearch,
  validateCostSearch,
  validateNoSearch,
  validateSessionDetailSearch,
  validateToolsSearch,
} from './routes'
import { resolveTimeRange, validateTimeRange } from './timeRange'

describe('time range', () => {
  test('keeps a known preset and drops the default', () => {
    expect(validateTimeRange({ range: '7d' })).toEqual({ range: '7d' })
    expect(validateTimeRange({ range: '24h' })).toEqual({})
  })

  test('unknown preset degrades to the default', () => {
    expect(validateTimeRange({ range: '99y' })).toEqual({})
    expect(validateTimeRange({ range: 5 })).toEqual({})
  })

  test('absolute window wins over a preset and is normalised', () => {
    expect(
      validateTimeRange({ range: '7d', from: '2026-01-01T00:00:00+01:00', to: '2026-01-02T00:00:00Z' }),
    ).toEqual({ from: '2025-12-31T23:00:00.000Z', to: '2026-01-02T00:00:00.000Z' })
  })

  test('rejects malformed, reversed and empty windows', () => {
    expect(validateTimeRange({ from: 'yesterday' })).toEqual({})
    expect(validateTimeRange({ from: '2026-01-02T00:00:00Z', to: '2026-01-01T00:00:00Z' })).toEqual({})
    expect(validateTimeRange({ from: '2026-01-01T00:00:00Z', to: '2026-01-01T00:00:00Z' })).toEqual({})
    expect(validateTimeRange({ from: '2026-01-01' })).toEqual({})
  })

  test('half-open windows are kept', () => {
    expect(validateTimeRange({ from: '2026-01-01T00:00:00Z' })).toEqual({
      from: '2026-01-01T00:00:00.000Z',
    })
  })

  test('resolves a preset against now, and an absolute window as given', () => {
    const now = new Date('2026-03-10T12:00:00Z')
    expect(resolveTimeRange({}, now)).toEqual({
      from: '2026-03-09T12:00:00.000Z',
      to: '2026-03-10T12:00:00.000Z',
    })
    expect(resolveTimeRange({ range: '1h' }, now).from).toBe('2026-03-10T11:00:00.000Z')
    expect(resolveTimeRange({ from: '2026-01-01T00:00:00.000Z' }, now)).toEqual({
      from: '2026-01-01T00:00:00.000Z',
      to: now.toISOString(),
    })
  })
})

describe('calls search', () => {
  test('empty and non-object input give defaults', () => {
    expect(validateCallsSearch({})).toEqual({})
    expect(validateCallsSearch(undefined)).toEqual({})
    expect(validateCallsSearch('x')).toEqual({})
  })

  test('drops unknown parameters', () => {
    expect(validateCallsSearch({ nope: 1, model: 'm' })).toEqual({ model: ['m'] })
  })

  test('a single value becomes a list; duplicates and blanks are removed', () => {
    expect(validateCallsSearch({ model: 'a', key: ['k', 'k', ' ', 3] })).toEqual({
      model: ['a'],
      key: ['k'],
    })
  })

  test('status accepts HTTP codes only', () => {
    expect(validateCallsSearch({ status: [200, '429', 99, 600, 'x', -1] })).toEqual({
      status: [200, 429],
    })
  })

  test('booleans and thresholds', () => {
    expect(
      validateCallsSearch({ streamed: 'true', tool_error: false, latency_ms_gte: '250', cost_micro_usd_gte: -5 }),
    ).toEqual({ streamed: true, tool_error: false, latency_ms_gte: 250 })
    expect(validateCallsSearch({ streamed: 'maybe', latency_ms_gte: 1.5 })).toEqual({})
  })

  test('group accepts session only; live is kept only when true', () => {
    expect(validateCallsSearch({ group: 'session', live: true })).toEqual({ group: 'session', live: true })
    expect(validateCallsSearch({ group: 'model', live: false })).toEqual({})
  })

  test('cursor is kept as an opaque string', () => {
    expect(validateCallsSearch({ cursor: 'abc==' })).toEqual({ cursor: 'abc==' })
    expect(validateCallsSearch({ cursor: '' })).toEqual({})
  })

  test('validation is idempotent', () => {
    const once = validateCallsSearch({ range: '7d', model: 'a', status: ['500'], group: 'session' })
    expect(validateCallsSearch(once)).toEqual(once)
  })
})

describe('other routes', () => {
  test('tab and mode reject unknown values and drop defaults', () => {
    expect(validateCallDetailSearch({ tab: 'usage' })).toEqual({ tab: 'usage' })
    expect(validateCallDetailSearch({ tab: 'overview' })).toEqual({})
    expect(validateCallDetailSearch({ tab: 'raw' })).toEqual({})
    expect(validateSessionDetailSearch({ mode: 'time' })).toEqual({ mode: 'time' })
    expect(validateSessionDetailSearch({ mode: 'tree' })).toEqual({})
    expect(validateSessionDetailSearch({ mode: 'x' })).toEqual({})
  })

  test('tools search checks enums', () => {
    expect(
      validateToolsSearch({ source: ['client', 'bogus'], status: 'pending', is_error: 'true', tool: ['Read'] }),
    ).toEqual({ source: ['client'], status: ['pending'], is_error: true, tool: ['Read'] })
    expect(validateToolsSearch({ source: 'x', status: ['y'] })).toEqual({})
  })

  test('cost search checks group_by and grain', () => {
    expect(validateCostSearch({ group_by: 'model', grain: 'day' })).toEqual({ group_by: 'model', grain: 'day' })
    expect(validateCostSearch({ group_by: 'tool', grain: 'week' })).toEqual({})
  })

  test('config page keeps nothing', () => {
    expect(validateNoSearch()).toEqual({})
  })
})
