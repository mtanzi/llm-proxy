import { formatCount } from '../lib/format'

export interface VolumePoint {
  start: string
  calls: number
  errors: number
}

export type Grain = 'minute' | 'hour' | 'day'

const GRAIN_MS: Record<Grain, number> = { minute: 60_000, hour: 3_600_000, day: 86_400_000 }

const W = 600
const H = 64

/**
 * One slot per interval of the window, empty intervals included, so the x
 * axis is time. Slots start at UTC interval boundaries, as the usage buckets do.
 */
export function volumeSlots(points: VolumePoint[], from: string, to: string, grain: Grain): VolumePoint[] {
  const step = GRAIN_MS[grain]
  const byStart = new Map(points.map((p) => [Date.parse(p.start), p]))
  const end = Date.parse(to)
  const slots: VolumePoint[] = []
  for (let t = Math.floor(Date.parse(from) / step) * step; t < end; t += step) {
    slots.push(byStart.get(t) ?? { start: new Date(t).toISOString(), calls: 0, errors: 0 })
  }
  return slots
}

function axisLabel(iso: string, grain: Grain): string {
  const d = new Date(iso)
  return grain === 'day'
    ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    : d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/**
 * Calls per interval as bars in the palette's neutral grey; the error part
 * of each bar is drawn on top in the error colour. Plain SVG, no chart library.
 */
export function VolumeChart({
  points,
  from,
  to,
  grain,
}: {
  points: VolumePoint[]
  from: string
  to: string
  grain: Grain
}) {
  const slots = volumeSlots(points, from, to, grain)
  const total = slots.reduce((sum, p) => sum + p.calls, 0)
  const errors = slots.reduce((sum, p) => sum + p.errors, 0)
  const max = Math.max(1, ...slots.map((p) => p.calls))
  const step = slots.length > 0 ? W / slots.length : W
  const bar = Math.max(1, step - 1)
  return (
    <figure className="volume-chart">
      <figcaption>
        <span>Calls per {grain}</span>
        <span className="volume-legend" aria-hidden="true">
          <span>calls</span>
          <span className="legend-error">errors</span>
        </span>
      </figcaption>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={`Calls per ${grain}: ${formatCount(total)} calls, ${formatCount(errors)} with errors, in ${slots.length} intervals`}
      >
        {slots.map((p, i) => {
          if (p.calls === 0) return null
          const h = (p.calls / max) * H
          const eh = (p.errors / max) * H
          return (
            <g key={p.start}>
              <title>{`${new Date(p.start).toLocaleString()}: ${formatCount(p.calls)} calls, ${formatCount(p.errors)} errors`}</title>
              <rect className="bar" x={i * step} y={H - h} width={bar} height={h} />
              {p.errors > 0 && <rect className="bar-error" x={i * step} y={H - eh} width={bar} height={eh} />}
            </g>
          )
        })}
      </svg>
      <div className="volume-axis" aria-hidden="true">
        <span>{axisLabel(from, grain)}</span>
        <span>{axisLabel(to, grain)}</span>
      </div>
    </figure>
  )
}
