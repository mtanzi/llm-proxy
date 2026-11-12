import { useQuery } from '@tanstack/react-query'
import { readQueryOptions } from '../api/queries'
import { apiGet } from '../api/client'
import type { QueryOf, ResponseOf } from '../api/client'
import { UNKNOWN } from '../lib/format'

type CallDetail = ResponseOf<'/admin/calls/{id}'>
export type ToolCall = ResponseOf<'/admin/tool-calls'>['tool_calls'][number]

/**
 * Window of the tool-call query, on the use time. A use is timed at response
 * end; a result pairs with a use up to 24 hours older (core.ToolResultLookahead).
 */
const TOOL_RESULT_LOOKAHEAD_MS = 24 * 60 * 60 * 1000

export function toolCallWindow(call: Pick<CallDetail, 'ts' | 'ended_at'>): { from: string; to: string } {
  return {
    from: new Date(Date.parse(call.ts) - TOOL_RESULT_LOOKAHEAD_MS).toISOString(),
    to: new Date(Date.parse(call.ended_at ?? call.ts) + 1000).toISOString(),
  }
}

type ToolCallList = ResponseOf<'/admin/tool-calls'>

/**
 * Pages through the pairs of the window, newest first, until every tool use
 * and result of this call has its pair or the pages run out. Keeps only the
 * pairs of this call.
 */
export async function fetchToolPairs(
  call: CallDetail,
  query: QueryOf<'/admin/tool-calls'>,
  signal?: AbortSignal,
): Promise<ToolCallList> {
  const missing = new Set([
    ...call.tool_uses.map((u) => `use:${u.tool_call_id}`),
    ...call.tool_results.map((r) => `result:${r.tool_call_id}`),
  ])
  const toolCalls: ToolCall[] = []
  let cursor: string | undefined
  do {
    const page = await apiGet('/admin/tool-calls', { query: { ...query, cursor } }, { signal })
    for (const p of page.tool_calls) {
      const mine = p.use_call_id === call.id || p.result_call_id === call.id
      if (p.use_call_id === call.id) missing.delete(`use:${p.tool_call_id}`)
      if (p.result_call_id === call.id) missing.delete(`result:${p.tool_call_id}`)
      if (mine) toolCalls.push(p)
    }
    cursor = page.next_cursor ?? undefined
  } while (cursor !== undefined && missing.size > 0)
  return { tool_calls: toolCalls, next_cursor: null }
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

export function ToolOutcome({ row }: { row: Pick<ToolRow, 'isError' | 'status'> }): React.ReactNode {
  return outcome(row)
}

function outcome(row: Pick<ToolRow, 'isError' | 'status'>): React.ReactNode {
  if (row.isError === true) return <span className="badge badge-error">Error</span>
  if (row.isError === false) return <span className="badge badge-ok">Success</span>
  if (row.status === 'pending') return <span className="badge">Pending</span>
  if (row.status === 'use_only') return <span className="badge">No result</span>
  return <span className="unknown">{UNKNOWN}</span>
}

/**
 * Pairs of this call's tool uses and results, from the tool-call query.
 * Shared by the Overview thread and the Tools tab; both read one cached query.
 */
export function useToolPairs(call: CallDetail) {
  const hasTools = call.tool_uses.length + call.tool_results.length > 0
  const enabled = hasTools && call.session_key !== ''
  const query = { session: call.session_key, limit: 500, ...toolCallWindow(call) }
  const pairs = useQuery({
    ...readQueryOptions('/admin/tool-calls', { query }),
    queryFn: ({ signal }) => fetchToolPairs(call, query, signal),
    enabled,
  })
  return { pairs, enabled, hasTools }
}
