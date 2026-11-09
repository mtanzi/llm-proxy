import { expect, test } from 'vitest'
import { axisTicks } from './SessionView'
import { GAP_MARKER_SHARE, IDLE_GAP_SHARE, buildTimeAxis, mergeIntervals } from './timeAxis'

test('merges overlapping and touching intervals', () => {
  expect(
    mergeIntervals([
      { start: 10, end: 20 },
      { start: 0, end: 5 },
      { start: 15, end: 30 },
      { start: 30, end: 31 },
      { start: 50, end: 40 },
    ]),
  ).toEqual([
    { start: 0, end: 5 },
    { start: 10, end: 31 },
    { start: 50, end: 50 },
  ])
})

test('no activity gives an empty axis', () => {
  const axis = buildTimeAxis([])
  expect(axis.segments).toEqual([])
  expect(axis.position(123)).toBe(0)
})

test('one instant gives a degenerate axis without NaN', () => {
  const axis = buildTimeAxis([{ start: 100, end: 100 }])
  expect(axis.start).toBe(100)
  expect(axis.end).toBe(100)
  expect(axis.position(100)).toBe(0)
  expect(axis.gaps).toEqual([])
})

// Span 0..1000, so the threshold is 50 ms (5%).
const around = (gap: number) => [
  { start: 0, end: 100 },
  { start: 100 + gap, end: 1000 },
]

test('a gap of exactly 5% of the session stays linear', () => {
  const axis = buildTimeAxis(around(1000 * IDLE_GAP_SHARE))
  expect(axis.gaps).toHaveLength(0)
  expect(axis.position(500)).toBeCloseTo(0.5)
})

test('a gap just wider than 5% collapses to the fixed marker', () => {
  const axis = buildTimeAxis(around(51))
  expect(axis.gaps).toHaveLength(1)
  const [gap] = axis.gaps
  expect(gap.start).toBe(100)
  expect(gap.end).toBe(151)
  // Active time 949 ms; the marker is 2% of it.
  const active = 949
  const marker = active * GAP_MARKER_SHARE
  expect(gap.to - gap.from).toBeCloseTo(marker / (active + marker))
})

test('a wide gap takes the same fixed marker as a narrow collapsed one', () => {
  const narrow = buildTimeAxis([
    { start: 0, end: 100 },
    { start: 200, end: 300 },
  ])
  const wide = buildTimeAxis([
    { start: 0, end: 100 },
    { start: 1_000_000, end: 1_000_100 },
  ])
  const width = (a: typeof narrow) => a.gaps[0].to - a.gaps[0].from
  expect(narrow.gaps).toHaveLength(1)
  expect(wide.gaps).toHaveLength(1)
  expect(width(wide)).toBeCloseTo(width(narrow))
  // The activity on both sides of a collapsed gap keeps nearly all of the axis.
  expect(wide.position(100)).toBeCloseTo(0.5 / (1 + GAP_MARKER_SHARE))
})

test('positions are monotonic and span 0 to 1 across collapsed gaps', () => {
  const axis = buildTimeAxis([
    { start: 0, end: 10 },
    { start: 5000, end: 5010 },
    { start: 5011, end: 5020 },
    { start: 90_000, end: 90_050 },
  ])
  expect(axis.gaps).toHaveLength(2)
  let last = -1
  for (let t = -10; t <= 90_060; t += 37) {
    const p = axis.position(t)
    expect(p).toBeGreaterThanOrEqual(last)
    expect(p).toBeGreaterThanOrEqual(0)
    expect(p).toBeLessThanOrEqual(1)
    last = p
  }
  expect(axis.position(0)).toBe(0)
  expect(axis.position(90_050)).toBe(1)
  // Inside a gap, an instant maps into its marker.
  const gap = axis.gaps[0]
  const mid = axis.position((gap.start + gap.end) / 2)
  expect(mid).toBeGreaterThan(gap.from)
  expect(mid).toBeLessThan(gap.to)
})

test('ticks mark the session start and each stretch after a collapsed gap, never crowded', () => {
  const axis = buildTimeAxis([
    { start: 0, end: 100 },
    { start: 120, end: 200 },
    { start: 10_000, end: 10_100 },
    { start: 50_000, end: 50_001 },
    { start: 90_000, end: 90_100 },
  ])
  const ticks = axisTicks(axis)
  // Start, then after each collapsed gap; the stretch at 50 s is an instant,
  // so the stretch at 90 s opens right after it and its label is skipped.
  expect(axis.gaps).toHaveLength(3)
  expect(ticks.map((t) => t.offset)).toEqual([0, 10_000, 50_000])
  for (let i = 1; i < ticks.length; i++) expect(ticks[i].at - ticks[i - 1].at).toBeGreaterThanOrEqual(0.08)
})
