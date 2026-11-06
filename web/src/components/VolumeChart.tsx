import { formatCount } from '../lib/format'

export interface VolumePoint {
  start: string
  calls: number
  errors: number
}

const W = 600
const H = 100

/** Calls per bucket as bars; the error part of each bar is drawn on top. Plain SVG, no chart library. */
export function VolumeChart({ points }: { points: VolumePoint[] }) {
  const total = points.reduce((sum, p) => sum + p.calls, 0)
  const errors = points.reduce((sum, p) => sum + p.errors, 0)
  const max = Math.max(1, ...points.map((p) => p.calls))
  const step = points.length > 0 ? W / points.length : W
  const bar = Math.max(1, step - 1)
  return (
    <figure className="volume-chart">
      <figcaption>Calls per interval</figcaption>
      <svg
        viewBox={`0 0 ${W} ${H}`}
        preserveAspectRatio="none"
        role="img"
        aria-label={`Calls per interval: ${formatCount(total)} calls, ${formatCount(errors)} with errors, in ${points.length} intervals`}
      >
        {points.map((p, i) => {
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
    </figure>
  )
}
