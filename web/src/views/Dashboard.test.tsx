import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { createMemoryHistory } from '@tanstack/react-router'
import axe from 'axe-core'
import { afterEach, beforeEach, expect, test, vi } from 'vitest'
import { App } from '../App'
import { createQueryClient } from '../queryClient'
import { createAppRouter } from '../router'
import { call } from './callLogRows.test'
import type { CallSummary } from './callLogRows'
import { bucket } from './dashboardFixtures.test-util'
import type { UsageBucket } from './dashboardData'

class FakeEventSource {
  static CLOSED = 2
  static instances: FakeEventSource[] = []
  readyState = 0
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  private listeners = new Map<string, ((e: MessageEvent<string>) => void)[]>()
  constructor(public url: string) {
    FakeEventSource.instances.push(this)
  }
  addEventListener(type: string, fn: (e: MessageEvent<string>) => void) {
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), fn])
  }
  close() {}
  open() {
    act(() => this.onopen?.())
  }
  finish(c: CallSummary) {
    act(() => {
      for (const fn of this.listeners.get('call.finished') ?? []) {
        fn(new MessageEvent('call.finished', { data: JSON.stringify(c), lastEventId: c.id }))
      }
    })
  }
}

const NOW = '2026-11-01T12:00:00Z'
// The default 24h window and the 24h before it.
const CURRENT_FROM = '2026-10-31T12:00:00.000Z'

interface Fixture {
  current: UsageBucket[]
  previous: UsageBucket[]
  byGroup: Record<string, UsageBucket[]>
  calls: CallSummary[]
  sessions: object[]
  authMode: string
}

const defaults: Fixture = {
  current: [
    bucket({ start: '2026-11-01T10:00:00Z', calls: 30, errors: 3, cost_micro_usd: 3_000_000 }),
    bucket({ start: '2026-11-01T11:00:00Z', calls: 30, errors: 0, cost_micro_usd: 3_000_000 }),
  ],
  previous: [bucket({ start: '2026-10-31T10:00:00Z', calls: 40, errors: 0, cost_micro_usd: 4_000_000 })],
  byGroup: {
    model: [
      bucket({ group: 'claude-opus', calls: 40, errors: 3, cost_micro_usd: 5_000_000 }),
      bucket({ group: 'claude-haiku', calls: 20, errors: 0, cost_micro_usd: 1_000_000 }),
    ],
    error_class: [
      bucket({ group: '', calls: 55, errors: 0 }),
      bucket({ group: 'rate_limited', calls: 2, errors: 2 }),
      bucket({ group: 'upstream_5xx', calls: 1, errors: 1 }),
    ],
    session: [
      bucket({ group: 's-big', cost_micro_usd: 4_000_000 }),
      bucket({ group: '', cost_micro_usd: 9_000_000 }),
    ],
    provider: [bucket({ group: 'anthropic', cost_micro_usd: 6_000_000 })],
  },
  authMode: 'proxy_key',
  calls: [call('c1', { ts: '2026-11-01T11:59:00Z', model: 'claude-opus', status: 429, error_class: 'rate_limited' })],
  sessions: [
    {
      session_key: 's-1',
      key: 'k',
      client: 'claude-code',
      first_ts: '2026-11-01T11:00:00Z',
      last_ts: '2026-11-01T11:30:00Z',
      ended_at: '2026-11-01T11:31:00Z',
      calls: 12,
      prompts: 2,
      tool_calls: 4,
      errors: 2,
      agent_count: 1,
      models: ['claude-opus'],
      usage: { input_uncached: 1, cache_read: 0, cache_write_5m: 0, cache_write_1h: 0, output: 1, reasoning: 0 },
      cost_micro_usd: null,
    },
  ],
}

function stubApi(over: Partial<Fixture> = {}) {
  const f = { ...defaults, ...over, byGroup: { ...defaults.byGroup, ...over.byGroup } }
  const urls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (url: string) => {
      urls.push(url)
      const q = new URL(url, 'http://x').searchParams
      const json = (body: unknown) => new Response(JSON.stringify(body))
      if (url.startsWith('/admin/usage')) {
        const group = q.get('group_by')
        const buckets = group ? (f.byGroup[group] ?? []) : q.get('from') === CURRENT_FROM ? f.current : f.previous
        return json({ group_by: group, grain: q.get('grain'), buckets })
      }
      if (url.startsWith('/admin/config')) {
        return json({ providers: [{ name: 'anthropic', kind: 'anthropic', base_url: '', auth_mode: f.authMode, idle_timeout: '10m' }], pricing: [], keys: [] })
      }
      if (url.startsWith('/admin/sessions')) return json({ sessions: f.sessions, next_cursor: null })
      if (url.startsWith('/admin/calls')) {
        return json({ calls: f.calls, next_cursor: null, count: f.calls.length, count_capped: false })
      }
      return new Response('{}', { status: 404 })
    }),
  )
  return urls
}

async function renderAt(path: string) {
  const queryClient = createQueryClient()
  const router = createAppRouter(queryClient, createMemoryHistory({ initialEntries: [path] }))
  const view = render(<App queryClient={queryClient} router={router} />)
  await waitFor(() => expect(screen.getByRole('heading', { level: 1, name: 'Overview' })).toBeTruthy())
  return { router, ...view }
}

const tile = (label: string) => screen.getByText(label, { selector: 'dt' }).parentElement as HTMLElement

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
  vi.useFakeTimers({ toFake: ['Date'] })
  vi.setSystemTime(new Date(NOW))
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

test('/ui/ is the overview, current in the sidebar', async () => {
  stubApi()
  const { router } = await renderAt('/ui/')
  expect(router.state.location.pathname).toBe('/')
  const nav = screen.getByRole('navigation', { name: 'Main' })
  await waitFor(() => expect(within(nav).getByRole('link', { name: 'Overview' }).getAttribute('aria-current')).toBe('page'))
  // The overview link is exact: the call log is one click away and not marked current.
  expect(within(nav).getByRole('link', { name: 'Calls' }).getAttribute('aria-current')).toBeNull()
})

test('headline numbers compare with the previous equal period', async () => {
  const urls = stubApi()
  await renderAt('/ui/')
  await waitFor(() => expect(within(tile('Calls')).getByText('60')).toBeTruthy())
  // 60 calls against 40: +50%. Cost $6.00 against $4.00.
  expect(tile('Calls').textContent).toContain('+50%')
  expect(tile('Calls').textContent).toContain('vs previous 24h')
  expect(tile('Cost').textContent).toContain('$6.00')
  expect(tile('Cost').querySelector('.change')?.getAttribute('data-tone')).toBe('bad')
  // 3 errors in 60 calls against none: a rise in points, marked with a glyph and words.
  expect(tile('Error rate').textContent).toContain('5.0%')
  expect(tile('Error rate').textContent).toContain('+5.0 pts')
  expect(tile('Cache hit').textContent).toContain('40.0%')
  expect(tile('Cache hit').textContent).toContain('no change')
  // Two buckets: the percentiles are marked approximate.
  expect(tile('Latency p95').textContent).toContain('≈ 400 ms')
  // The previous window is the 24h before the current one.
  expect(urls.some((u) => u.includes('from=2026-10-30T12%3A00%3A00.000Z') && u.includes('to=2026-10-31T12%3A00%3A00.000Z'))).toBe(true)
})

test('unknown cost shows as unknown, not zero', async () => {
  stubApi({ current: [bucket({ calls: 5, unpriced_calls: 5, cost_micro_usd: 0 })] })
  await renderAt('/ui/')
  await waitFor(() => expect(within(tile('Calls')).getByText('5')).toBeTruthy())
  expect(tile('Cost').querySelector('.kpi-value')?.textContent).toBe('unknown')
  expect(tile('Cost').textContent).not.toContain('$0.00')
})

test('client_passthrough cost carries the API-equivalent label', async () => {
  const urls = stubApi({ authMode: 'client_passthrough' })
  await renderAt('/ui/')
  await waitFor(() => expect(tile('Cost').textContent).toContain('API-equivalent'))
  const feed = await screen.findByRole('table', { name: 'Latest finished calls' })
  expect(within(feed).getByText(/API-equivalent/)).toBeTruthy()
  expect(urls.some((u) => u.includes('group_by=provider'))).toBe(true)
})

test('without a passthrough provider no cost is API-equivalent', async () => {
  const urls = stubApi()
  await renderAt('/ui/')
  await waitFor(() => expect(tile('Cost').textContent).toContain('$6.00'))
  await screen.findByRole('table', { name: 'Latest finished calls' })
  expect(screen.queryByText(/API-equivalent/)).toBeNull()
  expect(urls.some((u) => u.includes('group_by=provider'))).toBe(false)
})

test('problems group errors by class and link to the filtered call log', async () => {
  stubApi()
  const { router } = await renderAt('/ui/?range=7d')
  const list = await screen.findByRole('list', { name: 'Errors by class' })
  const items = within(list).getAllByRole('listitem')
  expect(items.map((li) => li.textContent)).toEqual(['Rate limited (429)2 calls', 'Upstream 5xx (500, 529 overloaded)1 call'])
  const link = within(items[0]).getByRole('link')
  expect(link.getAttribute('href')).toContain('/ui/calls?')
  fireEvent.click(link)
  await waitFor(() => expect(router.state.location.pathname).toBe('/calls'))
  expect(router.state.matches.at(-1)?.search).toEqual({ error_class: ['rate_limited'], range: '7d' })
})

test('no errors shows a calm empty state', async () => {
  stubApi({ byGroup: { error_class: [bucket({ group: '', errors: 0 })] } })
  await renderAt('/ui/')
  expect(await screen.findByText(/No errors in the last 24h/)).toBeTruthy()
})

test('live feed, recent sessions and breakdowns link to the other views', async () => {
  stubApi()
  await renderAt('/ui/')
  const feed = await screen.findByRole('table', { name: 'Latest finished calls' })
  expect(within(feed).getByText('429 · rate_limited')).toBeTruthy()
  FakeEventSource.instances[0].open()
  FakeEventSource.instances[0].finish(call('c2', { ts: '2026-11-01T12:00:30Z', model: 'claude-haiku' }))
  await waitFor(() => expect(within(feed).getAllByRole('row')).toHaveLength(3))
  expect(within(within(feed).getAllByRole('row')[1]).getByText('claude-haiku')).toBeTruthy()

  const sessions = await screen.findByRole('table', { name: 'Recent sessions' })
  expect(within(sessions).getByRole('link', { name: 's-1' }).getAttribute('href')).toBe('/ui/sessions/s-1')
  expect(within(sessions).getByText('2 errors')).toBeTruthy()
  expect(within(sessions).getByText('unknown')).toBeTruthy()

  const top = await screen.findByRole('list', { name: 'Top 5 sessions by cost' })
  // Calls without a session are not a session.
  expect(within(top).getAllByRole('listitem')).toHaveLength(1)
  expect(within(top).getByRole('link', { name: 's-big' })).toBeTruthy()
  const byModel = await screen.findByRole('list', { name: 'Cost by model' })
  expect(within(byModel).getAllByRole('listitem').map((li) => li.textContent)).toEqual([
    'claude-opus$5.00 · 83%',
    'claude-haiku$1.00 · 17%',
  ])
})

test('the range is in the URL and moves both windows', async () => {
  const urls = stubApi()
  const { router } = await renderAt('/ui/')
  fireEvent.click(screen.getByRole('button', { name: '7d' }))
  await waitFor(() => expect(router.state.matches.at(-1)?.search).toEqual({ range: '7d' }))
  // The sidebar marks the page current whatever its search.
  const nav = screen.getByRole('navigation', { name: 'Main' })
  expect(within(nav).getByRole('link', { name: 'Overview' }).getAttribute('aria-current')).toBe('page')
  await waitFor(() => expect(urls.some((u) => u.includes('from=2026-10-25T12%3A00%3A00.000Z'))).toBe(true))
  // Only the dashboard presets are offered, and no absolute window.
  expect(screen.queryByRole('button', { name: '15m' })).toBeNull()
  expect(screen.queryByLabelText('From')).toBeNull()
})

test('the overview has no accessibility violations', async () => {
  stubApi()
  const { container } = await renderAt('/ui/')
  await screen.findByRole('list', { name: 'Errors by class' })
  await screen.findByRole('table', { name: 'Recent sessions' })
  vi.useRealTimers()
  const results = await axe.run(container, { rules: { 'color-contrast': { enabled: false } } })
  expect(results.violations.map((v) => `${v.id}: ${v.help}`)).toEqual([])
})
