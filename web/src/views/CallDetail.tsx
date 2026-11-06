import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useSearch } from '@tanstack/react-router'
import { detailQueryOptions, readQueryOptions } from '../api/queries'
import type { ResponseOf } from '../api/client'
import { ContentNotStored } from '../components/ContentNotStored'
import { ErrorMessage, LoadingState } from '../components/states'
import {
  UNKNOWN,
  formatBytes,
  formatCostMicroUsd,
  formatCount,
  formatDurationMs,
  formatText,
  zeroAsUnknown,
} from '../lib/format'
import { CALL_TABS } from '../search/routes'
import type { CallTab } from '../search/routes'

export type CallDetail = ResponseOf<'/admin/calls/{id}'>
export type ToolCall = ResponseOf<'/admin/tool-calls'>['tool_calls'][number]

const TAB_LABELS: Record<CallTab, string> = {
  overview: 'Overview',
  tools: 'Tools',
  usage: 'Usage and cost',
}

/** V3 call detail (ADR 0032): stored numbers, names and hashes, never content. */
export function CallDetailView() {
  const { id } = useParams({ from: '/calls/$id' })
  const { tab = 'overview' } = useSearch({ from: '/calls/$id' })
  // A finished call never changes, so the query is cached for the session.
  const query = useQuery(detailQueryOptions('/admin/calls/{id}', { path: { id } }))

  return (
    <section aria-labelledby="view-title">
      <h1 id="view-title">Call</h1>
      {query.isPending && <LoadingState label="Loading call" />}
      {query.isError && <ErrorMessage error={query.error} onRetry={() => void query.refetch()} />}
      {query.data && (
        <>
          <p>
            <code>{query.data.id}</code>
            {query.data.usage_estimated && <Badge>Usage estimated</Badge>}
            {query.data.extraction_partial && <Badge>Extraction partial</Badge>}
          </p>
          <nav aria-label="Call detail tabs" className="tabs">
            {CALL_TABS.map((t) => (
              <Link
                key={t}
                to="/calls/$id"
                params={{ id }}
                search={t === 'overview' ? {} : { tab: t }}
                aria-current={t === tab ? 'page' : undefined}
              >
                {TAB_LABELS[t]}
              </Link>
            ))}
          </nav>
          {tab === 'overview' && <Overview call={query.data} />}
          {tab === 'tools' && <Tools call={query.data} />}
          {tab === 'usage' && <UsageAndCost call={query.data} />}
          <ContentNotStored />
        </>
      )}
    </section>
  )
}

function Badge({ children }: { children: React.ReactNode }) {
  return <span className="badge">{children}</span>
}

function Value({ value }: { value: string | null | undefined }) {
  const shown = formatText(value)
  return shown === UNKNOWN ? <span className="unknown">{UNKNOWN}</span> : <>{shown}</>
}

function Fields({ label, rows }: { label: string; rows: [string, React.ReactNode][] }) {
  return (
    <section aria-label={label}>
      <h2>{label}</h2>
      <dl className="fields">
        {rows.map(([name, value]) => (
          <div key={name}>
            <dt>{name}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  )
}

function unknownOr(value: string | number | null | undefined, text: string) {
  return value == null || value === '' || value === 0 ? <span className="unknown">{UNKNOWN}</span> : text
}

function Overview({ call }: { call: CallDetail }) {
  const status = zeroAsUnknown(call.status)
  return (
    <>
      <Fields
        label="Who"
        rows={[
          ['Key', <Value key="k" value={call.key} />],
          ['Client', <Value key="c" value={call.client} />],
          [
            'Session',
            call.session_key ? (
              <Link to="/sessions/$key" params={{ key: call.session_key }}>
                {call.session_key}
              </Link>
            ) : (
              <span className="unknown">{UNKNOWN}</span>
            ),
          ],
          ['Agent', <Value key="a" value={call.agent_id} />],
          ['Sub-agent of', <Value key="p" value={call.parent_agent_id} />],
          ['Prompt id', <Value key="pi" value={call.prompt_id} />],
        ]}
      />
      <Fields
        label="Call"
        rows={[
          ['Time', <Value key="t" value={call.ts} />],
          ['Model', <Value key="m" value={call.model} />],
          ['Provider', <Value key="pr" value={call.provider} />],
          ['Upstream request id', <Value key="u" value={call.upstream_request_id} />],
        ]}
      />
      <Fields
        label="Outcome"
        rows={[
          ['Status', unknownOr(status, String(status))],
          ['Error type', <Value key="et" value={call.error_type} />],
          ['Stop reason', <Value key="sr" value={call.stop_reason} />],
          ['Time to first token', formatDurationMs(zeroAsUnknown(call.ttft_ms))],
          ['Duration', formatDurationMs(zeroAsUnknown(call.duration_ms))],
        ]}
      />
    </>
  )
}

/** Window of the tool-call query: uses of this call, and results it carries for earlier uses. */
const RESULT_LOOKBACK_MS = 60 * 60 * 1000

export function toolCallWindow(ts: string): { from: string; to: string } {
  const t = Date.parse(ts)
  return {
    from: new Date(t - RESULT_LOOKBACK_MS).toISOString(),
    to: new Date(t + 1000).toISOString(),
  }
}

export interface ToolRow {
  key: string
  role: 'requested' | 'resolved'
  name: string
  toolCallId: string
  argBytes: number | null
  resultBytes: number | null
  duration: number | null
  durationSource: string | null
  isError: boolean | null
  otherCallId: string | null
  status: ToolCall['status'] | null
}

/**
 * Rows of the Tools tab: each tool use of this call beside the call that
 * resolved it, and each result this call carries beside the call that used
 * it. A half the pairing query did not return reads as unknown.
 */
export function buildToolRows(call: CallDetail, pairs: ToolCall[] | undefined): ToolRow[] {
  const find = (toolCallId: string, side: 'use_call_id' | 'result_call_id') =>
    pairs?.find((p) => p.tool_call_id === toolCallId && p[side] === call.id)
  const uses = call.tool_uses.map((u, i): ToolRow => {
    const p = find(u.tool_call_id, 'use_call_id')
    return {
      key: `use-${i}-${u.tool_call_id}`,
      role: 'requested',
      name: u.name,
      toolCallId: u.tool_call_id,
      argBytes: u.arg_bytes,
      resultBytes: p && p.result_call_id ? p.result_bytes : null,
      duration: p?.duration_ms ?? null,
      durationSource: p?.duration_source ?? null,
      isError: p?.is_error ?? null,
      otherCallId: p?.result_call_id ?? null,
      status: p?.status ?? null,
    }
  })
  const results = call.tool_results.map((r, i): ToolRow => {
    const p = find(r.tool_call_id, 'result_call_id')
    return {
      key: `result-${i}-${r.tool_call_id}`,
      role: 'resolved',
      name: p?.name ?? '',
      toolCallId: r.tool_call_id,
      argBytes: p && p.use_call_id ? p.arg_bytes : null,
      resultBytes: r.result_bytes,
      duration: p?.duration_ms ?? null,
      durationSource: p?.duration_source ?? null,
      isError: r.is_error,
      otherCallId: p?.use_call_id ?? null,
      status: p?.status ?? null,
    }
  })
  return [...uses, ...results]
}

function outcome(row: ToolRow): React.ReactNode {
  if (row.isError === true) return 'Error'
  if (row.isError === false) return 'Success'
  if (row.status === 'pending') return 'Pending'
  if (row.status === 'use_only') return 'No result'
  return <span className="unknown">{UNKNOWN}</span>
}

function Tools({ call }: { call: CallDetail }) {
  const hasTools = call.tool_uses.length + call.tool_results.length > 0
  const enabled = hasTools && call.session_key !== ''
  const pairs = useQuery({
    ...readQueryOptions('/admin/tool-calls', {
      query: { session: call.session_key, limit: 500, ...toolCallWindow(call.ts) },
    }),
    enabled,
  })
  if (!hasTools) return <p className="state">This call carries no tool uses or results.</p>
  const rows = buildToolRows(call, pairs.data?.tool_calls)
  return (
    <section aria-label="Tools">
      <h2>Tools</h2>
      {enabled && pairs.isPending && <LoadingState label="Loading tool pairs" />}
      {pairs.isError && <ErrorMessage error={pairs.error} onRetry={() => void pairs.refetch()} />}
      <table>
        <thead>
          <tr>
            <th>Tool</th>
            <th>Call id</th>
            <th>In this call</th>
            <th>Argument size</th>
            <th>Result size</th>
            <th>Duration</th>
            <th>Outcome</th>
            <th>Other call</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((row) => (
            <tr key={row.key}>
              <td>
                <Value value={row.name} />
              </td>
              <td>
                <code>{row.toolCallId}</code>
              </td>
              <td>{row.role === 'requested' ? 'Requested' : 'Result'}</td>
              <td>{formatBytes(row.argBytes)}</td>
              <td>{formatBytes(row.resultBytes)}</td>
              <td>
                {formatDurationMs(row.duration)}
                {row.durationSource && ` (${row.durationSource})`}
              </td>
              <td>{outcome(row)}</td>
              <td>
                {row.otherCallId ? (
                  <Link to="/calls/$id" params={{ id: row.otherCallId }}>
                    {row.role === 'requested' ? 'Resolving call' : 'Requesting call'}
                  </Link>
                ) : (
                  <span className="unknown">{row.status === 'pending' ? 'none yet' : UNKNOWN}</span>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}

function UsageAndCost({ call }: { call: CallDetail }) {
  const u = call.usage
  const columns: [string, number][] = [
    ['Input (uncached)', u.input_uncached],
    ['Cache read', u.cache_read],
    ['Cache write 5m', u.cache_write_5m],
    ['Cache write 1h', u.cache_write_1h],
    ['Output', u.output],
    ['Reasoning (part of output)', u.reasoning],
  ]
  return (
    <section aria-label="Usage and cost">
      <h2>Usage and cost</h2>
      <table>
        <thead>
          <tr>
            {columns.map(([name]) => (
              <th key={name}>{name}</th>
            ))}
          </tr>
        </thead>
        <tbody>
          <tr>
            {columns.map(([name, value]) => (
              <td key={name}>{formatCount(value)}</td>
            ))}
          </tr>
        </tbody>
      </table>
      <dl className="fields">
        <div>
          <dt>Cost</dt>
          <dd>{formatCostMicroUsd(call.cost_micro_usd)}</dd>
        </div>
        <div>
          <dt>Pricing version</dt>
          <dd>
            <Value value={call.pricing_version} />
          </dd>
        </div>
      </dl>
      <details>
        <summary>Raw usage</summary>
        {call.usage_raw == null ? (
          <p className="unknown">{UNKNOWN}</p>
        ) : (
          <pre>{JSON.stringify(call.usage_raw, null, 2)}</pre>
        )}
      </details>
    </section>
  )
}
