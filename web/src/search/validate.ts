// Search-parameter validation (ADR 0030). Each parser takes an unknown value
// from the URL and returns a valid value or undefined, so a stale or
// hand-edited URL degrades to defaults and never raises an error.

export type Raw = Record<string, unknown>

export function oneOf<T extends string>(value: unknown, allowed: readonly T[]): T | undefined {
  return typeof value === 'string' && (allowed as readonly string[]).includes(value)
    ? (value as T)
    : undefined
}

const MAX_LIST = 50
const MAX_TEXT = 512

export function text(value: unknown): string | undefined {
  return typeof value === 'string' && value.trim() !== '' && value.length <= MAX_TEXT
    ? value
    : undefined
}

/** A repeated filter: one string or an array of strings. Invalid items are dropped. */
export function textList(value: unknown): string[] | undefined {
  const items = (Array.isArray(value) ? value : [value])
    .map(text)
    .filter((v): v is string => v !== undefined)
  const unique = [...new Set(items)].slice(0, MAX_LIST)
  return unique.length > 0 ? unique : undefined
}

export function enumList<T extends string>(value: unknown, allowed: readonly T[]): T[] | undefined {
  const items = (Array.isArray(value) ? value : [value])
    .map((v) => oneOf(v, allowed))
    .filter((v): v is T => v !== undefined)
  const unique = [...new Set(items)]
  return unique.length > 0 ? unique : undefined
}

/** HTTP status codes. */
export function statusList(value: unknown): number[] | undefined {
  const items = (Array.isArray(value) ? value : [value])
    .map(int)
    .filter((v): v is number => v !== undefined && v >= 100 && v <= 599)
  const unique = [...new Set(items)].slice(0, MAX_LIST)
  return unique.length > 0 ? unique : undefined
}

export function bool(value: unknown): boolean | undefined {
  if (typeof value === 'boolean') return value
  if (value === 'true') return true
  if (value === 'false') return false
  return undefined
}

/** A non-negative safe integer. */
export function int(value: unknown): number | undefined {
  const n = typeof value === 'string' && /^\d+$/.test(value) ? Number(value) : value
  return typeof n === 'number' && Number.isSafeInteger(n) && n >= 0 ? n : undefined
}

/** An opaque keyset cursor. */
export function cursor(value: unknown): string | undefined {
  return text(value)
}

/** Drops keys whose value is undefined, so equal searches compare equal. */
export function compact<T extends object>(value: T): T {
  return Object.fromEntries(Object.entries(value).filter(([, v]) => v !== undefined)) as T
}

export function asRaw(search: unknown): Raw {
  return typeof search === 'object' && search !== null ? (search as Raw) : {}
}

/**
 * Makes a validator drop what it rejects. The router merges a route's
 * validated search over the raw URL search, so a key the validator leaves out
 * would survive with its raw value. This sets every raw key to undefined
 * first, which removes unknown keys and rejected values.
 */
export function strict<T extends object>(validate: (search: unknown) => T) {
  return (search: unknown): T => {
    const dropped = Object.fromEntries(Object.keys(asRaw(search)).map((k) => [k, undefined]))
    return { ...dropped, ...validate(search) } as T
  }
}
