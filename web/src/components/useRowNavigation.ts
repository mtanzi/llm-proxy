import { useLayoutEffect, useRef, useState } from 'react'
import type { RefObject } from 'react'

const PAGE = 10

/**
 * Keyboard navigation of virtualised rows with a roving tab index: one row is
 * in the tab order, and ArrowUp/ArrowDown (or k/j), PageUp/PageDown, Home and
 * End move focus. Rows carry `data-index`; a row that is not rendered yet is
 * scrolled into view first and focused once it is.
 */
export function useRowNavigation({
  count,
  containerRef,
  scrollToIndex,
}: {
  count: number
  containerRef: RefObject<HTMLElement | null>
  scrollToIndex: (index: number) => void
}) {
  const [active, setActive] = useState(0)
  const pending = useRef<number | null>(null)
  const current = count === 0 ? -1 : Math.min(active, count - 1)

  // Runs after every render until the pending row exists and has focus.
  useLayoutEffect(() => {
    if (pending.current === null) return
    const el = containerRef.current?.querySelector<HTMLElement>(`[data-index="${pending.current}"]`)
    if (el) {
      el.focus()
      pending.current = null
    }
  })

  const focusRow = (index: number) => {
    if (count === 0) return
    const next = Math.max(0, Math.min(count - 1, index))
    setActive(next)
    pending.current = next
    scrollToIndex(next)
    // The row may already be rendered: focus it now.
    const el = containerRef.current?.querySelector<HTMLElement>(`[data-index="${next}"]`)
    if (el) {
      el.focus()
      pending.current = null
    }
  }

  /** Handles a navigation key on the row at `index`. Returns whether it was handled. */
  const onRowKeyDown = (e: React.KeyboardEvent, index: number): boolean => {
    if (e.altKey || e.ctrlKey || e.metaKey) return false
    let next: number | null = null
    switch (e.key) {
      case 'ArrowDown':
      case 'j':
        next = index + 1
        break
      case 'ArrowUp':
      case 'k':
        next = index - 1
        break
      case 'PageDown':
        next = index + PAGE
        break
      case 'PageUp':
        next = index - PAGE
        break
      case 'Home':
        next = 0
        break
      case 'End':
        next = count - 1
        break
    }
    if (next === null) return false
    e.preventDefault()
    focusRow(next)
    return true
  }

  return { active: current, setActive, focusRow, onRowKeyDown }
}
