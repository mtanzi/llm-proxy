import { useState } from 'react'
import type { ResponseOf } from '../api/client'
import { StatusBadge, isErrorStatus } from '../components/Badge'
import { ToolOutcome } from './toolPairs'
import type { ToolRow } from './toolPairs'
import { UNKNOWN, formatBytes, formatCount, formatText, zeroAsUnknown } from '../lib/format'

type CallDetail = ResponseOf<'/admin/calls/{id}'>

export type BlockRole = 'system' | 'user' | 'assistant' | 'tool-call'

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

function unknownBytes(value: number | null): React.ReactNode {
  const shown = formatBytes(value)
  return shown === UNKNOWN ? <span className="unknown">{UNKNOWN}</span> : shown
}

function mono(value: string): React.ReactNode {
  return value === '' ? <span className="unknown">{UNKNOWN}</span> : <code>{value}</code>
}

/**
 * The call as a chat-style thread, in request then response order. Only
 * stored metadata is shown (ADR 0002): what the request carried (prompt id,
 * input tokens) and what the response returned (outcome, output tokens, the
 * tools it asked for). Each tool call carries the outcome and size of its
 * result, from the pairing rows of the Tools tab. A system block
 * notes a compaction request or a context compacted before this call.
 */
export function buildThread(call: CallDetail, rows: ToolRow[] = []): ThreadBlock[] {
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
  const failed = isErrorStatus(call.status, call.error_class)
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
  call.tool_uses.forEach((t, i) => {
    const key = `use-${i}-${t.tool_call_id}`
    const row = rows.find((r) => r.key === key)
    blocks.push({
      key,
      role: 'tool-call',
      name: `Tool call ${formatText(t.name)}`,
      title: formatText(t.name),
      error: row?.isError === true,
      fields: [
        ['Outcome', <ToolOutcome key="o" row={row ?? { isError: null, status: null }} />],
        ['Result size', unknownBytes(row?.resultBytes ?? null)],
        ['Argument size', formatBytes(t.arg_bytes)],
        ['Call id', mono(t.tool_call_id)],
      ],
    })
  })
  return blocks
}

type Run = { role: BlockRole; blocks: ThreadBlock[] }

/** Consecutive tool call blocks form a run; other blocks are runs of one. */
export function threadRuns(blocks: ThreadBlock[]): Run[] {
  const runs: Run[] = []
  for (const b of blocks) {
    const last = runs[runs.length - 1]
    if (b.role === 'tool-call' && last?.role === b.role) last.blocks.push(b)
    else runs.push({ role: b.role, blocks: [b] })
  }
  return runs
}

function Block({ block }: { block: ThreadBlock }) {
  return (
    <article className={`block block-${block.role}${block.error ? ' has-error' : ''}`} aria-label={block.name}>
      <div className="block-head">
        <span className="kind">{ROLE_LABELS[block.role]}</span>
        <span className="title">{block.title}</span>
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
  const noun = 'tool calls'
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
export function Thread({ call, rows }: { call: CallDetail; rows?: ToolRow[] }) {
  const runs = threadRuns(buildThread(call, rows))
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
