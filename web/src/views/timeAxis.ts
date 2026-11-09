// Compressed time axis for the session tree and waterfall (ADR 0033).
//
// Agent sessions are mostly idle time, so a linear axis would squeeze the
// work into thin slivers. Idle gaps wider than a share of the session (5%)
// collapse to a fixed marker; the active stretches share the rest of the axis
// in proportion to their real length (Langfuse's port of Sentry's approach).

/** A gap is collapsed when it is wider than this share of the session span. */
export const IDLE_GAP_SHARE = 0.05

/** Width of a collapsed gap, as a share of the active (uncollapsed) time. */
export const GAP_MARKER_SHARE = 0.02

export interface Interval {
  start: number
  end: number
}

export interface IdleGap {
  /** Real start and end of the gap, epoch ms. */
  start: number
  end: number
  /** Position of the marker on the axis, 0 to 1. */
  from: number
  to: number
}

export interface Segment {
  start: number
  end: number
  /** Position on the axis, 0 to 1. */
  from: number
  to: number
  collapsed: boolean
}

export interface TimeAxis {
  start: number
  end: number
  segments: Segment[]
  gaps: IdleGap[]
  /** Maps an epoch-ms instant to a position on the axis, 0 to 1. */
  position: (t: number) => number
}

/** Merges overlapping or touching intervals. Reversed intervals count as instants. */
export function mergeIntervals(intervals: Interval[]): Interval[] {
  const sorted = intervals
    .map((i) => ({ start: i.start, end: Math.max(i.start, i.end) }))
    .sort((a, b) => a.start - b.start || a.end - b.end)
  const out: Interval[] = []
  for (const i of sorted) {
    const last = out[out.length - 1]
    if (last && i.start <= last.end) {
      last.end = Math.max(last.end, i.end)
    } else {
      out.push({ ...i })
    }
  }
  return out
}

/**
 * Builds the axis over the given activity intervals. The span runs from the
 * first start to the last end; a stretch with no activity that is wider than
 * IDLE_GAP_SHARE of the span collapses to a marker of GAP_MARKER_SHARE of the
 * active time. A gap of exactly the threshold stays linear.
 */
export function buildTimeAxis(intervals: Interval[]): TimeAxis {
  const merged = mergeIntervals(intervals.filter((i) => Number.isFinite(i.start) && Number.isFinite(i.end)))
  if (merged.length === 0) {
    return { start: 0, end: 0, segments: [], gaps: [], position: () => 0 }
  }
  const start = merged[0].start
  const end = merged[merged.length - 1].end
  const span = end - start
  const threshold = span * IDLE_GAP_SHARE

  // Real segments in order, alternating activity and gaps.
  const real: { start: number; end: number; collapsed: boolean }[] = []
  for (let i = 0; i < merged.length; i++) {
    if (i > 0) {
      const gapStart = merged[i - 1].end
      const gapEnd = merged[i].start
      real.push({ start: gapStart, end: gapEnd, collapsed: gapEnd - gapStart > threshold })
    }
    real.push({ start: merged[i].start, end: merged[i].end, collapsed: false })
  }

  const active = real.filter((s) => !s.collapsed).reduce((sum, s) => sum + (s.end - s.start), 0)
  const collapsedCount = real.filter((s) => s.collapsed).length
  // All activity at one instant: give each collapsed gap one unit so the axis is not empty.
  const marker = active > 0 ? active * GAP_MARKER_SHARE : 1
  const total = active + collapsedCount * marker

  const segments: Segment[] = []
  let cursor = 0
  for (const s of real) {
    const length = s.collapsed ? marker : s.end - s.start
    const from = total > 0 ? cursor / total : 0
    cursor += length
    const to = total > 0 ? cursor / total : 0
    segments.push({ ...s, from, to })
  }

  const position = (t: number): number => {
    if (segments.length === 0 || t <= start) return 0
    if (t >= end) return total > 0 ? 1 : 0
    // Binary search for the first segment that ends at or after t.
    let lo = 0
    let hi = segments.length - 1
    while (lo < hi) {
      const mid = (lo + hi) >> 1
      if (segments[mid].end < t) lo = mid + 1
      else hi = mid
    }
    const s = segments[lo]
    const width = s.end - s.start
    return width > 0 ? s.from + ((t - s.start) / width) * (s.to - s.from) : s.from
  }

  return {
    start,
    end,
    segments,
    gaps: segments
      .filter((s) => s.collapsed)
      .map(({ start: gs, end: ge, from, to }) => ({ start: gs, end: ge, from, to })),
    position,
  }
}
