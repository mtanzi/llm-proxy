import { describe, expect, test } from 'vitest'
import { listQueryKey, normalizeParams, queryKey } from './keys'

describe('query keys', () => {
  test('equal requests give equal keys regardless of order', () => {
    const a = queryKey('/admin/calls', { query: { model: ['b', 'a'], key: ['k'], streamed: true } })
    const b = queryKey('/admin/calls', { query: { streamed: true, key: ['k'], model: ['a', 'b'] } })
    expect(a).toEqual(b)
    expect(JSON.stringify(a)).toBe(JSON.stringify(b))
  })

  test('undefined and null params do not change the key', () => {
    expect(queryKey('/admin/calls', { query: { model: undefined } })).toEqual(queryKey('/admin/calls'))
    expect(normalizeParams({ a: null, b: 1 })).toEqual({ b: 1 })
  })

  test('different endpoints, params or path values give different keys', () => {
    expect(queryKey('/admin/calls')).not.toEqual(queryKey('/admin/sessions'))
    expect(queryKey('/admin/calls', { query: { model: ['a'] } })).not.toEqual(
      queryKey('/admin/calls', { query: { model: ['b'] } }),
    )
    expect(queryKey('/admin/calls/{id}', { path: { id: 'x' } })).not.toEqual(
      queryKey('/admin/calls/{id}', { path: { id: 'y' } }),
    )
  })

  test('the key is stable across calls', () => {
    const q = { model: ['a'], from: '2026-01-01T00:00:00.000Z' }
    expect(queryKey('/admin/calls', { query: q })).toEqual(queryKey('/admin/calls', { query: { ...q } }))
  })

  test('the list key ignores the cursor', () => {
    expect(listQueryKey('/admin/calls', { model: ['a'], cursor: 'c1' })).toEqual(
      listQueryKey('/admin/calls', { model: ['a'], cursor: 'c2' }),
    )
    expect(listQueryKey('/admin/calls', { model: ['a'] })).not.toEqual(
      listQueryKey('/admin/calls', { model: ['b'] }),
    )
  })
})
