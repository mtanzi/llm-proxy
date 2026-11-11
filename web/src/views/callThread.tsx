import { useState } from 'react'
import type { ResponseOf } from '../api/client'
import { StatusBadge } from '../components/Badge'
import { UNKNOWN, formatBytes, formatCount, formatText, zeroAsUnknown } from '../lib/format'

type CallDetail = ResponseOf<'/admin/calls/{id}'>

export type BlockRole = 'system' | 'user' | 'assistant' | 'tool-call' | 'tool-result'

export interface ThreadBlock {
  key: string
  role: BlockRole
  /** Accessible name of the block, such as "Response" or "Tool call Read". */
  name: string
  title: string
  error: boolean
  fields: [string, React.ReactNode][]
}

const ROLE_LABELS: Record<BlockRole, string> = {
  system: 'System',
  user: 'User',
  assistant: 'Assistant',
  'tool-call': 'Tool call',
  'tool-result': 'Tool result',
}

/** A run of tool blocks longer than this shows its first COLLAPSED_SHOWN blocks and a button for the rest. */
export const COLLAPSE_AFTER = 4
export const COLLAPSED_SHOWN = 3

function text(value: string | null | undefined): React.ReactNode {
  const shown = formatText(value)
  return shown === UNKNOWN ? <span className="unknown">{UNKNOWN}</span> : shown
}

function count(value: number | null): React.ReactNode {
  return value === null ? <span className="unknown">{UNKNOWN}</span> : formatCount(value)
}

function mono(value: string): React.ReactNode {
  return value === '' ? <span className="unknown">{UNKNOWN}</span> : <code>{value}</code>
}

/**
 * The call as a chat-style thread, in request then response order. Only
 * stored metadata is shown (ADR 0002): what the request carried (prompt id,
 * input tokens, the tool results it sent back) and what the response
 * returned (outcome, output tokens, the tools it asked for). A system block
 * notes a compaction request or a context compacted before this call.
 */
export function buildThread(call: CallDetail): ThreadBlock[] {
  const u = call.usage
  const blocks: ThreadBlock[] = []
  if (call.compaction) {
    blocks.push({ key: 'compaction', role: 'system', name: 'Compaction request', title: 'Compaction request', error: false, fields: [] })
  }
  if (call.context_compacted) {
    blocks.push({ key: 'compacted', role: 'system', name: 'Context compacted', title: 'Context compacted before this call', error: false, fields: [] })
  }
  blocks.push({
    key: 'request',
    role: 'user',
    name: 'Request',
    title: 'Request',
    error: false,
    fields: [
      ['Prompt id', mono(call.prompt_id)],
      ['Tokens in', count(zeroAsUnknown(u.input_uncached + u.cache_read + u.cache_write_5m + u.cache_write_1h))],
    ],
  })
  call.tool_results.forEach((r, i) =>
    blocks.push({
      key: `result-${i}-${r.tool_call_id}`,
      role: 'tool-result',
      name: `Tool result ${r.tool_call_id}`,
      title: r.tool_call_id,
      error: r.is_error,
      fields: [
        ['Result size', formatBytes(r.result_bytes)],
        ['Outcome', r.is_error ? <span className="badge badge-error">Error</span> : <span className="badge badge-ok">Success</span>],
      ],
    }),
  )
  const failed = call.error_class !== '' || call.status >= 400
  blocks.push({
    key: 'response',
    role: 'assistant',
    name: 'Response',
    title: 'Response',
    error: failed,
    fields: [
      ['Status', <StatusBadge key="s" status={call.status} errorClass={call.error_class} />],
      ['Error type', text(call.error_type)],
      ['Stop reason', text(call.stop_reason)],
      ['Tokens out', count(zeroAsUnknown(u.output))],
      ['Reasoning', count(zeroAsUnknown(u.reasoning))],
    ],
  })
  call.tool_uses.forEach((t, i) =>
    blocks.push({
      key: `use-${i}-${t.tool_call_id}`,
      role: 'tool-call',
      name: `Tool call ${formatText(t.name)}`,
      title: formatText(t.name),
      error: false,
      fields: [
        ['Call id', mono(t.tool_call_id)],
        ['Argument size', formatBytes(t.arg_bytes)],
      ],
    }),
  )
  return blocks
}

type Run = { role: BlockRole; blocks: ThreadBlock[] }

/** Consecutive blocks of one tool role form a run; other blocks are runs of one. */
export function threadRuns(blocks: ThreadBlock[]): Run[] {
  const runs: Run[] = []
  for (const b of blocks) {
    const last = runs[runs.length - 1]
    const tool = b.role === 'tool-call' || b.role === 'tool-result'
    if (tool && last?.role === b.role) last.blocks.push(b)
    else runs.push({ role: b.role, blocks: [b] })
  }
  return runs
}

function Block({ block }: { block: ThreadBlock }) {
  return (
    <article className={`block block-${block.role}${block.error ? ' has-error' : ''}`} aria-label={block.name}>
      <div className="block-head">
        <span className="kind">{ROLE_LABELS[block.role]}</span>
        <span className={block.role === 'tool-result' ? 'title mono' : 'title'}>{block.title}</span>
      </div>
      {block.fields.length > 0 && (
        <dl className="fields">
          {block.fields.map(([name, value]) => (
            <div key={name}>
              <dt>{name}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      )}
    </article>
  )
}

function RunBlocks({ run }: { run: Run }) {
  const [open, setOpen] = useState(false)
  const long = run.blocks.length > COLLAPSE_AFTER
  const shown = long && !open ? run.blocks.slice(0, COLLAPSED_SHOWN) : run.blocks
  const hidden = run.blocks.length - COLLAPSED_SHOWN
  const noun = run.role === 'tool-call' ? 'tool calls' : 'tool results'
  return (
    <>
      {shown.map((b) => (
        <li key={b.key}>
          <Block block={b} />
        </li>
      ))}
      {long && (
        <li className="thread-more">
          <button type="button" aria-expanded={open} onClick={() => setOpen(!open)}>
            {open ? `Show fewer ${noun}` : `Show ${hidden} more ${noun}`}
          </button>
        </li>
      )}
    </>
  )
}

/** The Exchange section of the call detail overview. */
export function Thread({ call }: { call: CallDetail }) {
  const runs = threadRuns(buildThread(call))
  return (
    <section aria-label="Exchange">
      <h2>Exchange</h2>
      <ol className="thread">
        {runs.map((run) => (
          <RunBlocks key={run.blocks[0].key} run={run} />
        ))}
      </ol>
    </section>
  )
}
