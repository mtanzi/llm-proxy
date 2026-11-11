import { useEffect, useId, useRef, useState } from 'react'

/**
 * A button that opens a panel of filter fields. `activeCount` shows on the
 * button. Escape or a click outside closes the panel.
 */
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
  const rootRef = useRef<HTMLDivElement>(null)
  const buttonRef = useRef<HTMLButtonElement>(null)

  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    document.addEventListener('pointerdown', onPointerDown)
    return () => document.removeEventListener('pointerdown', onPointerDown)
  }, [open])

  return (
    <div
      className="filter-menu"
      ref={rootRef}
      onKeyDown={(e) => {
        if (e.key === 'Escape' && open) {
          e.stopPropagation()
          setOpen(false)
          buttonRef.current?.focus()
        }
      }}
    >
      <button
        ref={buttonRef}
        type="button"
        aria-label={activeCount > 0 ? `Filters (${activeCount})` : 'Filters'}
        aria-expanded={open}
        aria-controls={panelId}
        onClick={() => setOpen(!open)}
      >
        Filters
        {activeCount > 0 && (
          <span className="filter-count" aria-hidden="true">
            {activeCount}
          </span>
        )}
      </button>
      {onClear && activeCount > 0 && (
        <button type="button" onClick={onClear}>
          Clear filters
        </button>
      )}
      <div id={panelId} className="filter-panel" role="group" aria-label="Filters" hidden={!open}>
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

/** A filter that takes one text value. */
export function TextFilter({
  label,
  value,
  onChange,
}: {
  label: string
  value: string | undefined
  onChange: (next: string | undefined) => void
}) {
  const id = useId()
  return (
    <div className="filter-field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        value={value ?? ''}
        onChange={(e) => onChange(e.target.value.trim() === '' ? undefined : e.target.value)}
      />
    </div>
  )
}

/** A repeated filter over a fixed set of values: one checkbox per value. None checked means any. */
export function EnumListFilter<T extends string>({
  label,
  options,
  values = [],
  onChange,
}: {
  label: string
  options: readonly { value: T; label: string }[]
  values?: readonly T[]
  onChange: (next: T[] | undefined) => void
}) {
  const toggle = (value: T, on: boolean) => {
    // Keep the options' order, so the URL does not depend on the click order.
    const next = options.map((o) => o.value).filter((v) => (v === value ? on : values.includes(v)))
    onChange(next.length > 0 ? next : undefined)
  }
  return (
    <fieldset className="filter-field filter-choices">
      <legend>{label}</legend>
      {options.map((o) => (
        <label key={o.value}>
          <input
            type="checkbox"
            checked={values.includes(o.value)}
            onChange={(e) => toggle(o.value, e.target.checked)}
          />
          {o.label}
        </label>
      ))}
    </fieldset>
  )
}
