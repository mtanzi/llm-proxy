import { asRaw, compact, oneOf } from './validate'
import type { Raw } from './validate'

export const RANGE_PRESETS = ['15m', '1h', '6h', '24h', '7d', '30d'] as const
export type RangePreset = (typeof RANGE_PRESETS)[number]
export const DEFAULT_RANGE: RangePreset = '24h'

const PRESET_MS: Record<RangePreset, number> = {
  '15m': 15 * 60_000,
  '1h': 60 * 60_000,
  '6h': 6 * 60 * 60_000,
  '24h': 24 * 60 * 60_000,
  '7d': 7 * 24 * 60 * 60_000,
  '30d': 30 * 24 * 60 * 60_000,
}

/**
 * The time range in the URL: a preset (`range`) or an absolute window
 * (`from` and `to`, RFC 3339). An absolute window wins over a preset.
 */
export interface TimeRangeSearch {
  range?: RangePreset
  from?: string
  to?: string
}

function instant(value: unknown): string | undefined {
  if (typeof value !== 'string') return undefined
  // RFC 3339 needs a date, a time and an offset; Date.parse alone accepts more.
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?(Z|[+-]\d{2}:\d{2})$/.test(value)) {
    return undefined
  }
  const ms = Date.parse(value)
  return Number.isNaN(ms) ? undefined : new Date(ms).toISOString()
}

export function validateTimeRange(search: unknown): TimeRangeSearch {
  const raw: Raw = asRaw(search)
  const from = instant(raw.from)
  const to = instant(raw.to)
  if (from !== undefined && to !== undefined) {
    // An empty or reversed window is not a range: fall back to the default.
    return from < to ? { from, to } : {}
  }
  if (from !== undefined || to !== undefined) {
    return compact({ from, to })
  }
  const range = oneOf(raw.range, RANGE_PRESETS)
  return range === undefined || range === DEFAULT_RANGE ? {} : { range }
}

/** The `from` and `to` API parameters for a time range, relative to `now`. */
export function resolveTimeRange(
  search: TimeRangeSearch,
  now: Date = new Date(),
): { from: string; to: string } {
  const to = search.to ?? now.toISOString()
  const from =
    search.from ??
    new Date(
      Date.parse(to) - PRESET_MS[search.range ?? DEFAULT_RANGE],
    ).toISOString()
  return { from, to }
}
