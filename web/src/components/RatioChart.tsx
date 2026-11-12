import { formatRatio } from '../views/SessionList'
import { axisLabel } from './ActivityChart'
import type { ActivityGrain } from './ActivityChart'

const W = 600
const H = 140

/**
 * A ratio (0 to 1) per interval as a line on a fixed 0 to 100 percent scale.
 * An interval without a value (no input tokens) breaks the line, so a gap is
 * never drawn as 0. Each point has a marker and a tooltip, and the axis has
 * labelled gridlines, so the values do not depend on colour. Plain SVG.
 */
export function RatioChart({
  slots,
  values,
  grain,
  end,
  label,
}: {
  slots: number[]
  values: (number | null)[]
  grain: ActivityGrain
  end: number
  /** Names the measure, for example "Cache hit ratio". */
  label: string
}) {
  const step = slots.length > 0 ? W / slots.length : W
  const x = (i: number) => i * step + step / 2
  const y = (v: number) => H - 2 - Math.min(1, Math.max(0, v)) * (H - 4)
  const runs: string[][] = []
  let run: string[] = []
  values.forEach((v, i) => {
    if (v === null) {
      if (run.length > 0) runs.push(run)
      run = []
    } else {
      run.push(`${x(i).toFixed(1)},${y(v).toFixed(1)}`)
    }
  })
  if (run.length > 0) runs.push(run)
  const known = values.filter((v): v is number => v !== null)
  const summary =
    known.length === 0
      ? 'no values'
      : `${known.length} of ${slots.length} intervals have a value, from ${formatRatio(Math.min(...known))} to ${formatRatio(Math.max(...known))}`
  return (
    <figure className="ratio-chart">
      <div className="chart-frame">
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={`${label} per ${grain}: ${summary}`}>
          {[0, 0.5, 1].map((g) => (
            <line key={g} className="gridline" x1={0} x2={W} y1={y(g)} y2={y(g)} vectorEffect="non-scaling-stroke" />
          ))}
          {runs.map((points) =>
            points.length > 1 ? (
              <polyline key={points[0]} points={points.join(' ')} vectorEffect="non-scaling-stroke" />
            ) : null,
          )}
          {values.map((v, i) =>
            v === null ? null : (
              <rect key={slots[i]} className="point" x={x(i) - 1.5} y={y(v) - 1.5} width={3} height={3}>
                <title>{`${new Date(slots[i]).toLocaleString()}: ${formatRatio(v)}`}</title>
              </rect>
            ),
          )}
        </svg>
        <span className="axis-y axis-y-top" aria-hidden="true">100%</span>
        <span className="axis-y axis-y-mid" aria-hidden="true">50%</span>
        <span className="axis-y axis-y-bottom" aria-hidden="true">0%</span>
      </div>
      <div className="volume-axis" aria-hidden="true">
        <span>{slots.length > 0 ? axisLabel(slots[0], grain) : ''}</span>
        <span>{axisLabel(end, grain)}</span>
      </div>
    </figure>
  )
}
