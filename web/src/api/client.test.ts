import { afterEach, describe, expect, test, vi } from 'vitest'
import { apiGet, buildPath, buildQuery } from './client'
import { ApiError, apiErrorFromBody, errorMessage, isRetryable } from './errors'

function mockFetch(status: number, body: unknown) {
  const fn = vi.fn(async () => new Response(typeof body === 'string' ? body : JSON.stringify(body), { status }))
  vi.stubGlobal('fetch', fn)
  return fn
}

afterEach(() => vi.unstubAllGlobals())

describe('request building', () => {
  test('path parameters are percent-encoded, including / and :', () => {
    expect(buildPath('/admin/sessions/{key}', { key: 'a/b:c d' })).toBe('/admin/sessions/a%2Fb%3Ac%20d')
  })

  test('a missing path parameter throws', () => {
    expect(() => buildPath('/admin/calls/{id}', {})).toThrow('missing path parameter id')
  })

  test('arrays repeat the key; undefined and null are skipped', () => {
    expect(buildQuery({ model: ['a', 'b'], streamed: false, cursor: undefined, x: null })).toBe(
      '?model=a&model=b&streamed=false',
    )
    expect(buildQuery({})).toBe('')
  })
})

describe('apiGet', () => {
  test('returns the parsed body', async () => {
    const fetchFn = mockFetch(200, { calls: [], next_cursor: null, count: 0, count_capped: false })
    const out = await apiGet('/admin/calls', { query: { model: ['m'], limit: 5 } })
    expect(out.count).toBe(0)
    expect((fetchFn.mock.calls as unknown as [string][])[0][0]).toBe('/admin/calls?model=m&limit=5')
  })

  test('maps an error body to a typed ApiError', async () => {
    mockFetch(400, { error: { type: 'invalid_parameter', message: 'bad', parameter: 'limit' } })
    const err = await apiGet('/admin/calls').catch((e: unknown) => e)
    expect(err).toBeInstanceOf(ApiError)
    expect(err).toMatchObject({ status: 400, type: 'invalid_parameter', parameter: 'limit' })
  })

  test('a failed fetch is a network error', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => Promise.reject(new TypeError('failed'))))
    await expect(apiGet('/admin/config')).rejects.toMatchObject({ type: 'network' })
  })

  test('a non-JSON error from a proxy still maps', async () => {
    mockFetch(502, '<html>bad gateway</html>')
    await expect(apiGet('/admin/config')).rejects.toMatchObject({ status: 502, type: 'internal' })
  })

  test('an unknown error type degrades to unknown', () => {
    expect(apiErrorFromBody(418, { error: { type: 'teapot', message: 'x' } }).type).toBe('unknown')
  })
})

describe('error messages', () => {
  test('each stable type has its own message and ignores server text', () => {
    const types = ['invalid_parameter', 'invalid_cursor', 'invalid_id', 'not_found', 'internal', 'network', 'unknown'] as const
    const messages = types.map((t) => errorMessage(new ApiError(400, t, 'SERVER TEXT')))
    expect(new Set(messages).size).toBe(types.length)
    expect(messages.join(' ')).not.toContain('SERVER TEXT')
  })

  test('names the parameter at fault', () => {
    expect(errorMessage(new ApiError(400, 'invalid_parameter', 'x', 'limit'))).toContain('limit')
  })

  test('only transient errors are retried', () => {
    expect(isRetryable(new ApiError(0, 'network', 'x'))).toBe(true)
    expect(isRetryable(new ApiError(500, 'internal', 'x'))).toBe(true)
    expect(isRetryable(new ApiError(404, 'not_found', 'x'))).toBe(false)
    expect(isRetryable(new Error('x'))).toBe(false)
  })
})
