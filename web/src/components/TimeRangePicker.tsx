import { useId } from 'react'
import { DEFAULT_RANGE, RANGE_PRESETS } from '../search/timeRange'
import type { TimeRangeSearch } from '../search/timeRange'

function toLocalInput(iso: string | undefined): string {
  if (!iso) return ''
  const d = new Date(iso)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fromLocalInput(value: string): string | undefined {
  if (value === '') return undefined
  const d = new Date(value)
  return Number.isNaN(d.getTime()) ? undefined : d.toISOString()
}

/**
 * Time-range primitive: preset buttons plus an absolute window. It reports a
 * TimeRangeSearch; the caller writes it to the URL.
 */
export function TimeRangePicker({
  value,
  onChange,
}: {
  value: TimeRangeSearch
  onChange: (next: TimeRangeSearch) => void
}) {
  const id = useId()
  const absolute = value.from !== undefined || value.to !== undefined
  const active = value.range ?? DEFAULT_RANGE
  return (
    <fieldset className="time-range">
      <legend>Time range</legend>
      <div className="button-group">
        {RANGE_PRESETS.map((preset) => (
          <button
            key={preset}
            type="button"
            aria-pressed={!absolute && active === preset}
            onClick={() => onChange(preset === DEFAULT_RANGE ? {} : { range: preset })}
          >
            {preset}
          </button>
        ))}
      </div>
      <label htmlFor={`${id}-from`}>From</label>
      <input
        id={`${id}-from`}
        type="datetime-local"
        value={toLocalInput(value.from)}
        onChange={(e) => onChange({ from: fromLocalInput(e.target.value), to: value.to })}
      />
      <label htmlFor={`${id}-to`}>To</label>
      <input
        id={`${id}-to`}
        type="datetime-local"
        value={toLocalInput(value.to)}
        onChange={(e) => onChange({ from: value.from, to: fromLocalInput(e.target.value) })}
      />
    </fieldset>
  )
}
