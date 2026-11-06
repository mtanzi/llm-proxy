import { QueryClient, QueryClientProvider, useInfiniteQuery } from '@tanstack/react-query'
import { renderHook, waitFor } from '@testing-library/react'
import { afterEach, expect, test, vi } from 'vitest'
import { detailQueryOptions, infiniteListOptions, readQueryOptions } from './queries'

afterEach(() => vi.unstubAllGlobals())

test('detail is cached without expiry; lists refetch on focus unless live', () => {
  const detail = detailQueryOptions('/admin/calls/{id}', { path: { id: 'x' } })
  expect(detail.staleTime).toBe(Infinity)
  expect(detail.gcTime).toBe(Infinity)
  expect(infiniteListOptions('/admin/calls').refetchOnWindowFocus).toBe(true)
  expect(infiniteListOptions('/admin/calls', {}, { live: true }).refetchOnWindowFocus).toBe(false)
  expect(readQueryOptions('/admin/config').refetchOnWindowFocus).toBe(true)
})

test('infinite list follows next_cursor and stops when it is null', async () => {
  const pages = [
    { calls: [{ id: 'a' }], next_cursor: 'c1', count: 2, count_capped: false },
    { calls: [{ id: 'b' }], next_cursor: null, count: 2, count_capped: false },
  ]
  const urls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      urls.push(url)
      return new Response(JSON.stringify(pages[urls.length - 1]))
    }),
  )
  const client = new QueryClient()
  const { result } = renderHook(() => useInfiniteQuery(infiniteListOptions('/admin/calls', { model: ['m'] })), {
    wrapper: ({ children }) => <QueryClientProvider client={client}>{children}</QueryClientProvider>,
  })
  await waitFor(() => expect(result.current.data?.pages).toHaveLength(1))
  expect(result.current.hasNextPage).toBe(true)
  await result.current.fetchNextPage()
  await waitFor(() => expect(result.current.data?.pages).toHaveLength(2))
  expect(result.current.hasNextPage).toBe(false)
  expect(urls).toEqual(['/admin/calls?model=m', '/admin/calls?model=m&cursor=c1'])
})
