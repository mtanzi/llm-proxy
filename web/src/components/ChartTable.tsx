import { chartRows } from '../views/costData'
import { axisLabel } from './ActivityChart'
import type { ActivityGrain } from './ActivityChart'

/**
 * The table fallback of a chart: the same numbers as text, one row per
 * interval that has a value. The chart is a picture and its colours carry no
 * meaning alone, so this table is how a reader who cannot see the colours, or
 * who uses a screen reader, reaches every value (ADR 0035).
 */
export function ChartTable({
  caption,
  slots,
  series,
  grain,
  format,
  total,
  keepZero = false,
}: {
  caption: string
  slots: number[]
  series: { name: string; values: (number | null)[] }[]
  grain: ActivityGrain
  format: (value: number | null) => string
  /** Add a total column (for stacked series); a ratio chart has none. */
  total?: (values: (number | null)[]) => string
  /** A zero is a value to list (a ratio of 0%), not an empty interval. */
  keepZero?: boolean
}) {
  const rows = chartRows(slots, series, keepZero)
  return (
    <details className="chart-table">
      <summary>Show as table</summary>
      {rows.length === 0 ? (
        <p className="notice">No values in this range.</p>
      ) : (
        <div className="table-frame chart-table-frame">
          <table className="grid-table">
            <caption className="sr-only">{caption}</caption>
            <thead>
              <tr>
                <th scope="col">Interval start</th>
                {series.map((s) => (
                  <th key={s.name} scope="col" className="num">
                    {s.name}
                  </th>
                ))}
                {total && (
                  <th scope="col" className="num">
                    Total
                  </th>
                )}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.start}>
                  <th scope="row">
                    <time dateTime={new Date(r.start).toISOString()}>{axisLabel(r.start, grain)}</time>
                  </th>
                  {r.values.map((v, i) => (
                    <td key={series[i].name} className="num">
                      {format(v)}
                    </td>
                  ))}
                  {total && <td className="num">{total(r.values)}</td>}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </details>
  )
}
