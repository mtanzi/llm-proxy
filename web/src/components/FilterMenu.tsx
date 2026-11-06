import { useId, useState } from 'react'

/** A disclosure that holds filter fields. `activeCount` shows on the button. */
export function FilterMenu({
  activeCount = 0,
  onClear,
  children,
}: {
  activeCount?: number
  onClear?: () => void
  children: React.ReactNode
}) {
  const [open, setOpen] = useState(false)
  const panelId = useId()
  return (
    <div className="filter-menu">
      <button type="button" aria-expanded={open} aria-controls={panelId} onClick={() => setOpen(!open)}>
        Filters{activeCount > 0 ? ` (${activeCount})` : ''}
      </button>
      {onClear && activeCount > 0 && (
        <button type="button" onClick={onClear}>
          Clear filters
        </button>
      )}
      <div id={panelId} role="group" aria-label="Filters" hidden={!open}>
        {children}
      </div>
    </div>
  )
}

/** A repeated text filter: shows values as removable chips and adds one on Enter or Add. */
export function TextListFilter({
  label,
  values = [],
  onChange,
}: {
  label: string
  values?: string[]
  onChange: (next: string[] | undefined) => void
}) {
  const id = useId()
  const [draft, setDraft] = useState('')
  const add = () => {
    const v = draft.trim()
    if (v !== '' && !values.includes(v)) onChange([...values, v])
    setDraft('')
  }
  const remove = (v: string) => {
    const next = values.filter((x) => x !== v)
    onChange(next.length > 0 ? next : undefined)
  }
  return (
    <div className="filter-field">
      <label htmlFor={id}>{label}</label>
      <ul aria-label={`${label} values`}>
        {values.map((v) => (
          <li key={v}>
            {v}{' '}
            <button type="button" aria-label={`Remove ${label} ${v}`} onClick={() => remove(v)}>
              ×
            </button>
          </li>
        ))}
      </ul>
      <input
        id={id}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            add()
          }
        }}
      />
      <button type="button" onClick={add}>
        Add
      </button>
    </div>
  )
}

/** A yes/no/any filter for a boolean API parameter. */
export function BooleanFilter({
  label,
  value,
  onChange,
}: {
  label: string
  value: boolean | undefined
  onChange: (next: boolean | undefined) => void
}) {
  const id = useId()
  return (
    <div className="filter-field">
      <label htmlFor={id}>{label}</label>
      <select
        id={id}
        value={value === undefined ? '' : String(value)}
        onChange={(e) => onChange(e.target.value === '' ? undefined : e.target.value === 'true')}
      >
        <option value="">Any</option>
        <option value="true">Yes</option>
        <option value="false">No</option>
      </select>
    </div>
  )
}

/** A non-negative integer threshold, such as "latency at least". */
export function NumberFilter({
  label,
  value,
  onChange,
}: {
  label: string
  value: number | undefined
  onChange: (next: number | undefined) => void
}) {
  const id = useId()
  return (
    <div className="filter-field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        type="number"
        min={0}
        step={1}
        value={value ?? ''}
        onChange={(e) => {
          const n = e.target.valueAsNumber
          onChange(Number.isSafeInteger(n) && n >= 0 ? n : undefined)
        }}
      />
    </div>
  )
}
