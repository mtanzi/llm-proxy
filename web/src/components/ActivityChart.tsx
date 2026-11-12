import { formatCostMicroUsd, formatCount } from '../lib/format'
import type { StackSeries } from '../views/dashboardData'

export type ActivityGrain = 'minute' | 'hour' | 'day'

/** The neutral grey is kept for "other", so the top models keep their colours. */
const tone = (s: StackSeries, i: number) => (s.other ? 5 : i)

const W = 600
const H = 140

export function axisLabel(t: number, grain: ActivityGrain): string {
  const d = new Date(t)
  return grain === 'day'
    ? d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
    : d.toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

/**
 * Calls or cost per interval as bars stacked by model, with the errors per
 * interval drawn over them as a line on their own scale (right). Each series
 * has a named legend entry and each bar a tooltip, so colour is never the
 * only cue. Plain SVG, no chart library, as the volume chart.
 */
export function ActivityChart({
  slots,
  series,
  errors,
  metric,
  grain,
  end,
  dimension = 'model',
  showErrors = true,
}: {
  slots: number[]
  series: StackSeries[]
  errors: number[]
  metric: 'calls' | 'cost'
  grain: ActivityGrain
  end: number
  /** What the stacks are grouped by: the label of the chart and its legend. */
  dimension?: string
  /** The errors line is part of the overview; the cost view draws stacks only. */
  showErrors?: boolean
}) {
  const format = metric === 'calls' ? formatCount : formatCostMicroUsd
  const stackTotals = slots.map((_, i) => series.reduce((sum, s) => sum + s.values[i], 0))
  const max = Math.max(0, ...stackTotals)
  const scale = max > 0 ? max : 1
  const maxErrors = showErrors ? Math.max(0, ...errors) : 0
  const totalErrors = errors.reduce((a, b) => a + b, 0)
  const step = slots.length > 0 ? W / slots.length : W
  const bar = Math.max(1, step - 1)
  const total = stackTotals.reduce((a, b) => a + b, 0)
  const errorY = (e: number) => H - (e / (maxErrors || 1)) * (H - 4)
  // One line per run of intervals with errors; an interval without errors breaks the line.
  const errorRuns: string[] = []
  let run: string[] = []
  errors.forEach((e, i) => {
    if (e > 0) run.push(`${(i * step + step / 2).toFixed(1)},${errorY(e).toFixed(1)}`)
    if ((e === 0 || i === errors.length - 1) && run.length > 0) {
      if (run.length > 1) errorRuns.push(run.join(' '))
      run = []
    }
  })
  const label =
    `${metric === 'calls' ? 'Calls' : 'Cost'} per ${grain} by ${dimension}: ${format(total)} in ${slots.length} intervals` +
    (series.length > 0 ? `, ${dimension}s ${series.map((s) => s.name).join(', ')}` : '') +
    (showErrors ? `; ${formatCount(totalErrors)} errors` : '')
  return (
    <figure className="activity-chart">
      <figcaption>
        <ul className="chart-legend">
          {series.map((s, i) => (
            <li key={s.name}>
              <span className={`swatch swatch-${tone(s, i)}`} aria-hidden="true" />
              <span className="mono">{s.name}</span>
            </li>
          ))}
          {showErrors && (
            <li className="legend-errors">
              <span className="swatch swatch-error" aria-hidden="true" />
              errors (right scale)
            </li>
          )}
        </ul>
      </figcaption>
      <div className="chart-frame">
        <span className="chart-max" aria-hidden="true">{format(max)}</span>
        {showErrors && (
          <span className="chart-max chart-max-errors" aria-hidden="true">{`${formatCount(maxErrors)} errors`}</span>
        )}
        <svg viewBox={`0 0 ${W} ${H}`} preserveAspectRatio="none" role="img" aria-label={label}>
          {slots.map((t, i) => {
            if (stackTotals[i] === 0 && (!showErrors || errors[i] === 0)) return null
            let y = H
            return (
              <g key={t}>
                <title>
                  {`${new Date(t).toLocaleString()}: ${format(stackTotals[i])}` +
                    series
                      .filter((s) => s.values[i] > 0)
                      .map((s) => `, ${s.name} ${format(s.values[i])}`)
                      .join('') +
                    (showErrors ? `; ${formatCount(errors[i])} errors` : '')}
                </title>
                {series.map((s, si) => {
                  const v = s.values[i]
                  if (v <= 0) return null
                  const h = (v / scale) * H
                  y -= h
                  return <rect key={s.name} className={`fill-${tone(s, si)}`} x={i * step} y={y} width={bar} height={h} />
                })}
              </g>
            )
          })}
          {maxErrors > 0 && (
            <g className="error-overlay">
              {errorRuns.map((points) => (
                <polyline key={points} points={points} vectorEffect="non-scaling-stroke" />
              ))}
              {errors.map((e, i) =>
                e > 0 ? (
                  <rect
                    key={slots[i]}
                    x={i * step + step / 2 - Math.max(1.5, bar / 4)}
                    y={errorY(e) - 1.5}
                    width={Math.max(3, bar / 2)}
                    height={3}
                  />
                ) : null,
              )}
            </g>
          )}
        </svg>
      </div>
      <div className="volume-axis" aria-hidden="true">
        <span>{slots.length > 0 ? axisLabel(slots[0], grain) : ''}</span>
        <span>{axisLabel(end, grain)}</span>
      </div>
    </figure>
  )
}
