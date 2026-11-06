import { useCallback, useEffect, useRef, useState } from 'react'
import { buildQuery } from '../api/client'
import { insertLive } from '../views/callLogRows'
import type { CallSummary, CallsQuery } from '../views/callLogRows'

export type LiveStatus = 'off' | 'connecting' | 'live' | 'reconnecting'

interface Held {
  url: string
  rows: CallSummary[]
}

/**
 * Live tail (ADR 0031): an EventSource on /admin/stream with the list's
 * filters. `call.finished` puts the call at the top; at most 500 rows are
 * kept. The server does not replay, so after a reconnect `onReconnect` runs
 * (the caller refetches the first page) and the caller de-duplicates by id.
 * Rows are tied to the stream URL: another filter starts with an empty list.
 */
export function useLiveCalls(
  enabled: boolean,
  query: CallsQuery,
  onReconnect: () => void,
): { rows: CallSummary[]; status: LiveStatus } {
  const url = '/admin/stream' + buildQuery(query as Record<string, unknown>)
  const [held, setHeld] = useState<Held>({ url, rows: [] })
  const [connection, setConnection] = useState<{ url: string; status: LiveStatus }>({
    url,
    status: 'connecting',
  })
  const reconnect = useRef(onReconnect)
  useEffect(() => {
    reconnect.current = onReconnect
  })

  const push = useCallback((call: CallSummary, forUrl: string) => {
    setHeld((prev) => {
      const rows = prev.url === forUrl ? prev.rows : []
      return { url: forUrl, rows: insertLive(rows, call) }
    })
  }, [])

  useEffect(() => {
    if (!enabled) return
    const source = new EventSource(url)
    let failed = false
    source.onopen = () => {
      setConnection({ url, status: 'live' })
      if (failed) {
        failed = false
        reconnect.current()
      }
    }
    source.onerror = () => {
      // The browser reconnects by itself; the next open triggers the refetch.
      failed = true
      setConnection({ url, status: 'reconnecting' })
    }
    source.addEventListener('call.finished', (event) => {
      try {
        push(JSON.parse((event as MessageEvent<string>).data) as CallSummary, url)
      } catch {
        // A frame that is not a call is skipped.
      }
    })
    return () => source.close()
  }, [enabled, url, push])

  if (!enabled) return { rows: [], status: 'off' }
  return {
    rows: held.url === url ? held.rows : [],
    status: connection.url === url ? connection.status : 'connecting',
  }
}
