import { useInfiniteQuery, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useMemo } from 'react'
import type { ColumnDef } from '@tanstack/react-table'
import { infiniteListOptions, readQueryOptions } from '../api/queries'
import { listQueryKey } from '../api/keys'
import { DataTable } from '../components/DataTable'
import {
  BooleanFilter,
  FilterMenu,
  NumberFilter,
  TextFilter,
  TextListFilter,
} from '../components/FilterMenu'
import { ErrorMessage } from '../components/states'
import { TimeRangePicker } from '../components/TimeRangePicker'
import { VolumeChart } from '../components/VolumeChart'
import { useLiveCalls } from '../hooks/useLiveCalls'
import {
  formatCappedCount,
  formatCostMicroUsd,
  formatCount,
  formatDurationMs,
  formatText,
  zeroAsUnknown,
} from '../lib/format'
import type { CallsSearch } from '../search/routes'
import { DEFAULT_RANGE, resolveTimeRange } from '../search/timeRange'
import {
  activeFilterCount,
  filterQuery,
  groupBySession,
  mergeRows,
  plainRows,
  streamQuery,
  tokensIn,
} from './callLogRows'
import type { CallSummary, LogRow } from './callLogRows'

const PAGE_SIZE = 100

type Grain = 'minute' | 'hour' | 'day'

function grainFor(from: string, to: string): Grain {
  const span = Date.parse(to) - Date.parse(from)
  if (span <= 2 * 3_600_000) return 'minute'
  if (span <= 3 * 86_400_000) return 'hour'
  return 'day'
}

function sumKnown(values: (number | null)[]): number | null {
  const known = values.filter((v): v is number => v !== null)
  return known.length === 0 ? null : known.reduce((a, b) => a + b, 0)
}

function worstStatus(calls: CallSummary[]): number {
  return calls.find((c) => c.error_class !== '' || c.status >= 400)?.status ?? calls[0].status
}

function Unknown() {
  return <span className="unknown">unknown</span>
}

function Maybe({ text }: { text: string }) {
  return text === 'unknown' ? <Unknown /> : <>{text}</>
}

function turnsLabel(row: LogRow): string | null {
  return row.kind === 'group' ? `${row.calls.length} turns` : null
}

const rowCalls = (row: LogRow): CallSummary[] => (row.kind === 'group' ? row.calls : [row.call])
const first = (row: LogRow): CallSummary => rowCalls(row)[0]

const columns: ColumnDef<LogRow>[] = [
  {
    id: 'time',
    header: 'Time',
    size: 170,
    enableSorting: false,
    cell: ({ row }) => <time dateTime={first(row.original).ts}>{new Date(first(row.original).ts).toLocaleString()}</time>,
  },
  {
    id: 'session',
    header: 'Session',
    size: 220,
    enableSorting: false,
    cell: ({ row }) => {
      const session = first(row.original).session_key
      if (session === '') return <Unknown />
      const turns = turnsLabel(row.original)
      return (
        <>
          <Link
            to="/sessions/$key"
            params={{ key: session }}
            onClick={(e) => e.stopPropagation()}
          >
            {session}
          </Link>
          {turns && <span className="badge">{turns}</span>}
        </>
      )
    },
  },
  {
    id: 'agent',
    header: 'Agent',
    size: 120,
    enableSorting: false,
    cell: ({ row }) => <Maybe text={formatText(first(row.original).agent_id)} />,
  },
  {
    id: 'client',
    header: 'Client',
    size: 120,
    enableSorting: false,
    cell: ({ row }) => <Maybe text={formatText(first(row.original).client)} />,
  },
  {
    id: 'model',
    header: 'Model',
    size: 180,
    enableSorting: false,
    cell: ({ row }) => <Maybe text={formatText(first(row.original).model)} />,
  },
  {
    id: 'status',
    header: 'Status',
    size: 80,
    enableSorting: false,
    cell: ({ row }) => {
      const calls = rowCalls(row.original)
      const status = row.original.kind === 'group' ? worstStatus(calls) : calls[0].status
      return status === 0 ? <Unknown /> : <>{status}</>
    },
  },
  {
    id: 'latency',
    header: 'Latency',
    size: 100,
    accessorFn: (r) => sumKnown(rowCalls(r).map((c) => zeroAsUnknown(c.duration_ms))),
    cell: ({ getValue }) => <Maybe text={formatDurationMs(getValue<number | null>())} />,
  },
  {
    id: 'tokens_in',
    header: 'Tokens in',
    size: 100,
    accessorFn: (r) => sumKnown(rowCalls(r).map((c) => zeroAsUnknown(tokensIn(c)))),
    cell: ({ getValue }) => <Maybe text={formatCount(getValue<number | null>())} />,
  },
  {
    id: 'tokens_out',
    header: 'Tokens out',
    size: 100,
    accessorFn: (r) => sumKnown(rowCalls(r).map((c) => zeroAsUnknown(c.usage.output))),
    cell: ({ getValue }) => <Maybe text={formatCount(getValue<number | null>())} />,
  },
  {
    id: 'cost',
    header: 'Cost',
    size: 100,
    accessorFn: (r) => sumKnown(rowCalls(r).map((c) => c.cost_micro_usd)),
    cell: ({ getValue }) => <Maybe text={formatCostMicroUsd(getValue<number | null>())} />,
  },
  {
    id: 'tools',
    header: 'Tool calls',
    size: 90,
    accessorFn: (r) => rowCalls(r).reduce((sum, c) => sum + c.tool_count, 0),
    cell: ({ getValue }) => <>{formatCount(getValue<number>())}</>,
  },
]

type FilterPatch = Partial<CallsSearch>

/** V1 call log (ADR 0031): the home view. All state is in the URL search parameters. */
export function CallLogView() {
  const search = useSearch({ from: '/calls/' }) as CallsSearch
  const navigate = useNavigate({ from: '/calls/' })
  const queryClient = useQueryClient()

  // The window is fixed per set of search parameters, so the query key is stable.
  const rangeKey = `${search.range ?? ''}|${search.from ?? ''}|${search.to ?? ''}`
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const window = useMemo(() => resolveTimeRange(search, new Date()), [rangeKey])
  const filterKey = JSON.stringify(filterQuery(search))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const filters = useMemo(() => filterQuery(search), [filterKey])
  const live = search.live === true

  const listQuery = { ...filters, ...window, limit: PAGE_SIZE }
  const list = useInfiniteQuery(infiniteListOptions('/admin/calls', listQuery, { live }))
  const usage = useQuery(
    readQueryOptions(
      '/admin/usage',
      { query: { ...filters, ...window, grain: grainFor(window.from, window.to) } },
      { live },
    ),
  )

  const streamKey = JSON.stringify(streamQuery(search))
  // eslint-disable-next-line react-hooks/exhaustive-deps
  const streamParams = useMemo(() => streamQuery(search), [streamKey])
  const stream = useLiveCalls(
    live,
    streamParams,
    () => {
      // The stream does not replay: reload the first page after a reconnect.
      void queryClient.resetQueries({ queryKey: listQueryKey('/admin/calls', listQuery) })
    },
  )

  const paged = useMemo(() => list.data?.pages.flatMap((p) => p.calls) ?? [], [list.data])
  const calls = useMemo(() => mergeRows(stream.rows, paged), [stream.rows, paged])
  const rows = useMemo(
    () => (search.group === 'session' ? groupBySession(calls) : plainRows(calls)),
    [calls, search.group],
  )

  const update = (patch: FilterPatch) =>
    void navigate({ search: (prev) => ({ ...prev, ...patch, cursor: undefined }) })

  const firstPage = list.data?.pages[0]
  const buckets = usage.data?.buckets ?? []
  const totals = useMemo(() => summarize(usage.data?.buckets ?? []), [usage.data])
  const isDefaultRange =
    search.range === undefined && search.from === undefined && search.to === undefined

  return (
    <section aria-labelledby="view-title">
      <h1 id="view-title">Calls</h1>

      <div className="toolbar">
        <TimeRangePicker value={search} onChange={(next) => update(next)} />
        <FilterMenu
          activeCount={activeFilterCount(search)}
          onClear={() =>
            update(Object.fromEntries(Object.keys(filterQuery(search)).map((k) => [k, undefined])) as FilterPatch)
          }
        >
          <TextListFilter label="Key name" values={search.key} onChange={(v) => update({ key: v })} />
          <TextListFilter label="Client" values={search.client} onChange={(v) => update({ client: v })} />
          <TextListFilter label="Provider" values={search.provider} onChange={(v) => update({ provider: v })} />
          <TextListFilter label="Model" values={search.model} onChange={(v) => update({ model: v })} />
          <TextListFilter label="Endpoint" values={search.endpoint} onChange={(v) => update({ endpoint: v })} />
          <TextListFilter
            label="Status"
            values={search.status?.map(String)}
            onChange={(v) => update({ status: v?.map(Number).filter((n) => Number.isInteger(n) && n >= 100 && n <= 599) })}
          />
          <TextListFilter label="Error class" values={search.error_class} onChange={(v) => update({ error_class: v })} />
          <TextFilter label="Session" value={search.session?.[0]} onChange={(v) => update({ session: v ? [v] : undefined })} />
          <TextFilter label="Agent" value={search.agent?.[0]} onChange={(v) => update({ agent: v ? [v] : undefined })} />
          <TextListFilter label="Agent type" values={search.agent_type} onChange={(v) => update({ agent_type: v })} />
          <TextFilter label="Prompt" value={search.prompt?.[0]} onChange={(v) => update({ prompt: v ? [v] : undefined })} />
          <TextListFilter label="Request class" values={search.request_class} onChange={(v) => update({ request_class: v })} />
          <TextListFilter label="Tool name" values={search.tool} onChange={(v) => update({ tool: v })} />
          <BooleanFilter label="Streamed" value={search.streamed} onChange={(v) => update({ streamed: v })} />
          <BooleanFilter label="Tool error" value={search.tool_error} onChange={(v) => update({ tool_error: v })} />
          <NumberFilter label="Latency at least (ms)" value={search.latency_ms_gte} onChange={(v) => update({ latency_ms_gte: v })} />
          <NumberFilter label="Cost at least (micro-USD)" value={search.cost_micro_usd_gte} onChange={(v) => update({ cost_micro_usd_gte: v })} />
        </FilterMenu>
        <label>
          <input
            type="checkbox"
            checked={search.group === 'session'}
            onChange={(e) => update({ group: e.target.checked ? 'session' : undefined })}
          />
          Group by session
        </label>
        <label>
          <input
            type="checkbox"
            checked={live}
            onChange={(e) => update({ live: e.target.checked ? true : undefined })}
          />
          Live
        </label>
        {live && (
          <span className="state" role="status" aria-live="polite">
            {stream.status === 'live' ? 'Live' : stream.status === 'reconnecting' ? 'Reconnecting…' : 'Connecting…'}
          </span>
        )}
      </div>

      <div className="summary-strip" role="group" aria-label="Summary">
        {usage.isError ? (
          <ErrorMessage error={usage.error} onRetry={() => void usage.refetch()} />
        ) : (
          <dl>
            <Stat label="Calls" value={firstPage ? formatCappedCount(firstPage.count, firstPage.count_capped) : usage.data ? formatCount(totals.calls) : '…'} />
            <Stat label="Errors" value={usage.data ? formatCount(totals.errors) : '…'} />
            <Stat label="Tokens in" value={usage.data ? formatCount(totals.tokensIn) : '…'} />
            <Stat label="Tokens out" value={usage.data ? formatCount(totals.tokensOut) : '…'} />
            <Stat
              label="Cost"
              value={usage.data ? formatCostMicroUsd(totals.cost) : '…'}
              note={totals.unpriced > 0 ? `${formatCount(totals.unpriced)} calls unpriced` : undefined}
            />
          </dl>
        )}
      </div>

      {!usage.isError && <VolumeChart points={buckets} />}

      <DataTable<LogRow>
        label="Calls"
        columns={columns}
        data={rows}
        getRowId={(r) => r.id}
        isLoading={list.isPending}
        error={list.error}
        onRetry={() => void list.refetch()}
        emptyMessage={
          isDefaultRange && activeFilterCount(search) === 0
            ? `No calls in the last ${DEFAULT_RANGE}.`
            : 'No calls match these filters.'
        }
        onRowClick={(r) =>
          r.kind === 'group'
            ? void navigate({ to: '/sessions/$key', params: { key: r.session }, search: {} })
            : void navigate({ to: '/calls/$id', params: { id: r.call.id }, search: {} })
        }
        hasNextPage={list.hasNextPage}
        isFetchingNextPage={list.isFetchingNextPage}
        onEndReached={() => void list.fetchNextPage()}
      />
    </section>
  )
}

function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd>
        {value}
        {note && <small className="unknown"> ({note})</small>}
      </dd>
    </div>
  )
}

function summarize(buckets: { calls: number; errors: number; cost_micro_usd: number; unpriced_calls: number; usage: CallSummary['usage'] }[]) {
  let calls = 0, errors = 0, tin = 0, tout = 0, cost = 0, unpriced = 0
  for (const b of buckets) {
    calls += b.calls
    errors += b.errors
    tin += b.usage.input_uncached + b.usage.cache_read + b.usage.cache_write_5m + b.usage.cache_write_1h
    tout += b.usage.output
    cost += b.cost_micro_usd
    unpriced += b.unpriced_calls
  }
  // All calls unpriced means the cost is unknown, not zero.
  return { calls, errors, tokensIn: tin, tokensOut: tout, cost: calls > 0 && unpriced === calls ? null : cost, unpriced }
}
