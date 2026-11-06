import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryHistory } from '@tanstack/react-router'
import axe from 'axe-core'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { App } from '../App'
import { createQueryClient } from '../queryClient'
import { createAppRouter } from '../router'
import { call } from './callLogRows.test'
import type { CallSummary } from './callLogRows'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>()
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
  }
  close() {
    this.closed = true
  }
  open() {
    act(() => this.onopen?.())
  }
  fail() {
    act(() => this.onerror?.())
  }
  finish(c: CallSummary) {
    act(() => {
      for (const fn of this.listeners.get('call.finished') ?? []) {
        fn(new MessageEvent('call.finished', { data: JSON.stringify(c), lastEventId: c.id }))
      }
    })
  }
}

const usageBody = {
  group_by: null,
  grain: 'hour',
  buckets: [
    {
      start: '2026-11-01T10:00:00Z',
      group: '',
      calls: 3,
      errors: 1,
      usage: { input_uncached: 10, cache_read: 20, cache_write_5m: 0, cache_write_1h: 0, output: 5, reasoning: 0 },
      cost_micro_usd: 2_500_000,
      unpriced_calls: 1,
      estimated_calls: 0,
      no_usage_calls: 0,
      latency_p50_ms: 1,
      latency_p95_ms: 1,
      latency_p99_ms: 1,
      cache_hit_ratio: 0.5,
    },
  ],
}

function list(calls: CallSummary[], over: object = {}) {
  return { calls, next_cursor: null, count: calls.length, count_capped: false, ...over }
}

function stubApi(respond: (url: string) => Response | Promise<Response>) {
  const urls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      urls.push(url)
      if (url.startsWith('/admin/usage')) return new Response(JSON.stringify(usageBody))
      return respond(url)
    }),
  )
  return urls
}

async function renderAt(path: string) {
  const queryClient = createQueryClient()
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }))
  const view = render(<App queryClient={queryClient} router={router} />)
  await waitFor(() => expect(screen.getByRole('main')).toBeTruthy())
  return { router, ...view }
}

const search = (router: { state: { location: { search: unknown } } }) =>
  router.state.location.search as Record<string, unknown>
const listUrls = (urls: string[]) => urls.filter((u) => u.startsWith('/admin/calls'))
const tableRows = () => within(screen.getByRole('table', { name: 'Calls' })).getAllByRole('row').slice(1)

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(480)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(1200)
  vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({
    width: 1200, height: 480, top: 0, left: 0, right: 1200, bottom: 480, x: 0, y: 0, toJSON: () => ({}),
  })
})
afterEach(() => {
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

test('shows rows, summary strip and capped count; a row opens V3 and a session opens V2', async () => {
  stubApi(() =>
    new Response(JSON.stringify(list([call('c1', { session_key: 'sess/1', cost_micro_usd: null })], { count: 10000, count_capped: true }))),
  )
  const { router, container } = await renderAt('/ui/calls')
  await screen.findByText('claude-code')
  const strip = screen.getByRole('group', { name: 'Summary' })
  expect(within(strip).getByText('10,000+')).toBeTruthy()
  expect(within(strip).getByText('$2.50')).toBeTruthy()
  expect(within(strip).getByText(/1 calls unpriced/)).toBeTruthy()
  expect(screen.getByRole('img', { name: /3 calls, 1 with errors/ })).toBeTruthy()
  // Unknown cost is not zero.
  expect(within(tableRows()[0]).getAllByText('unknown').length).toBeGreaterThan(0)
  expect((await axe.run(container)).violations.filter((v) => v.impact === 'critical' || v.impact === 'serious')).toEqual([])

  const link = screen.getByRole('link', { name: 'sess/1' })
  expect(link.getAttribute('href')).toBe('/ui/sessions/sess%2F1')
  fireEvent.click(tableRows()[0])
  await waitFor(() => expect(router.state.location.pathname).toBe('/calls/c1'))
})

test('filters round trip through the URL search parameters', async () => {
  const urls = stubApi(() => new Response(JSON.stringify(list([call('c1')]))))
  const { router } = await renderAt('/ui/calls?model=claude-x&status=429&streamed=true&latency_ms_gte=250')
  await screen.findByText('claude-code')
  // URL to request.
  const first = new URL(listUrls(urls)[0], 'http://x')
  expect(first.searchParams.getAll('model')).toEqual(['claude-x'])
  expect(first.searchParams.getAll('status')).toEqual(['429'])
  expect(first.searchParams.get('streamed')).toBe('true')
  expect(first.searchParams.get('latency_ms_gte')).toBe('250')
  expect(first.searchParams.get('cursor')).toBeNull()

  // Menu to URL.
  fireEvent.click(screen.getByRole('button', { name: /Filters \(4\)/ }))
  fireEvent.change(screen.getByRole('textbox', { name: 'Client' }), { target: { value: 'cursor' } })
  fireEvent.click(screen.getAllByRole('button', { name: 'Add' })[1])
  await waitFor(() => expect(search(router).client).toEqual(['cursor']))
  fireEvent.change(screen.getByLabelText('Tool error'), { target: { value: 'true' } })
  await waitFor(() => expect(search(router).tool_error).toBe(true))
  fireEvent.click(screen.getByRole('button', { name: 'Remove Model claude-x' }))
  await waitFor(() => expect(search(router).model).toBeUndefined())
  expect(search(router)).toMatchObject({ client: ['cursor'], status: [429], streamed: true, tool_error: true })
  await waitFor(() => {
    const last = new URL(listUrls(urls).at(-1)!, 'http://x')
    expect(last.searchParams.getAll('client')).toEqual(['cursor'])
    expect(last.searchParams.get('model')).toBeNull()
  })
  fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }))
  await waitFor(() => expect(search(router)).toEqual({}))
})

test('group by session folds consecutive calls and is kept in the URL', async () => {
  stubApi(() =>
    new Response(
      JSON.stringify(
        list([
          call('c1', { session_key: 's1', cost_micro_usd: 1_000_000 }),
          call('c2', { session_key: 's1', cost_micro_usd: 2_000_000 }),
          call('c3', { session_key: '' }),
        ]),
      ),
    ),
  )
  const { router } = await renderAt('/ui/calls')
  await screen.findAllByText('claude-code')
  expect(tableRows()).toHaveLength(3)
  fireEvent.click(screen.getByLabelText('Group by session'))
  await waitFor(() => expect(search(router).group).toBe('session'))
  await waitFor(() => expect(tableRows()).toHaveLength(2))
  expect(screen.getByText('2 turns')).toBeTruthy()
  expect(within(tableRows()[0]).getByText('$3.00')).toBeTruthy()
  // A group row opens the session.
  fireEvent.click(tableRows()[0])
  await waitFor(() => expect(router.state.location.pathname).toBe('/sessions/s1'))
})

test('live toggle opens the stream with the filters, inserts at the top and caps at 500', async () => {
  stubApi(() => new Response(JSON.stringify(list([call('old')]))))
  const { router } = await renderAt('/ui/calls?model=claude-x&range=7d')
  await screen.findByText('claude-code')
  expect(FakeEventSource.instances).toHaveLength(0)
  fireEvent.click(screen.getByLabelText('Live'))
  await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
  expect(search(router).live).toBe(true)
  const es = FakeEventSource.instances[0]
  const url = new URL(es.url, 'http://x')
  expect(url.pathname).toBe('/admin/stream')
  expect(url.searchParams.getAll('model')).toEqual(['claude-x'])
  // A preset range is relative to now: no time bounds on the stream.
  expect(url.searchParams.get('from')).toBeNull()
  es.open()
  await screen.findByText('Live', { selector: '[role=status]' })

  es.finish(call('new1', { client: 'fresh-client' }))
  await screen.findByText('fresh-client')
  expect(within(tableRows()[0]).getByText('fresh-client')).toBeTruthy()

  for (let i = 0; i < 520; i++) es.finish(call(`live${i}`, { client: 'bulk' }))
  // 500 live rows stay; the paged row is still there below them.
  const body = screen.getByRole('table', { name: 'Calls' })
  await waitFor(() => expect(body.getAttribute('aria-rowcount')).toBe('502'))

  fireEvent.click(screen.getByLabelText('Live'))
  await waitFor(() => expect(es.closed).toBe(true))
  await waitFor(() => expect(body.getAttribute('aria-rowcount')).toBe('2'))
})

test('reconnect refetches the first page and de-duplicates live rows by call id', async () => {
  let page = [call('old')]
  const urls = stubApi(() => new Response(JSON.stringify(list(page))))
  await renderAt('/ui/calls?live=true')
  await screen.findByText('claude-code')
  await waitFor(() => expect(FakeEventSource.instances).toHaveLength(1))
  const es = FakeEventSource.instances[0]
  es.open()
  es.finish(call('missed', { client: 'dup-client' }))
  await screen.findByText('dup-client')
  const before = listUrls(urls).length

  // The stream drops, the call is also in the refetched first page, and the stream reopens.
  page = [call('missed', { client: 'dup-client' }), call('old')]
  es.fail()
  await screen.findByText('Reconnecting…')
  es.open()
  await waitFor(() => expect(listUrls(urls).length).toBe(before + 1))
  expect(new URL(listUrls(urls).at(-1)!, 'http://x').searchParams.get('cursor')).toBeNull()
  await waitFor(() => expect(screen.getByRole('table', { name: 'Calls' }).getAttribute('aria-rowcount')).toBe('3'))
  expect(screen.getAllByText('dup-client')).toHaveLength(1)
  // A first open without an error does not refetch.
  expect(FakeEventSource.instances).toHaveLength(1)
})

test('virtualises 5,000 rows', async () => {
  const many = Array.from({ length: 5000 }, (_, i) => call(`c${i}`, { client: `client-${i}` }))
  stubApi(() => new Response(JSON.stringify(list(many, { count: 5000 }))))
  await renderAt('/ui/calls')
  await screen.findByText('client-0')
  const body = screen.getByRole('table', { name: 'Calls' })
  expect(body.getAttribute('aria-rowcount')).toBe('5001')
  expect(tableRows().length).toBeLessThan(40)
  expect(screen.queryByText('client-4999')).toBeNull()
})

test('empty and error states', async () => {
  let fail = true
  stubApi(() =>
    fail
      ? new Response(JSON.stringify({ error: { type: 'internal_error', message: 'The server failed' } }), { status: 500 })
      : new Response(JSON.stringify(list([]))),
  )
  await renderAt('/ui/calls?model=none')
  const alert = await screen.findByRole('alert', {}, { timeout: 5000 })
  expect(alert.textContent).toContain('The server failed')
  fail = false
  fireEvent.click(within(alert).getByRole('button', { name: 'Retry' }))
  await screen.findByText('No calls match these filters.')
})

test('an unfiltered empty log says so', async () => {
  stubApi(() => new Response(JSON.stringify(list([]))))
  await renderAt('/ui/calls')
  await screen.findByText('No calls in the last 24h.')
})
