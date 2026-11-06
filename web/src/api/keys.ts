import type { GetPath, RequestArgs } from './client'

/**
 * Canonical form of a params object: undefined and null dropped, object keys
 * sorted, arrays of scalars sorted (a repeated filter is a set). Two params
 * objects that mean the same request give the same value.
 */
export function normalizeParams(value: unknown): unknown {
  if (Array.isArray(value)) {
    const items = value.filter((v) => v !== undefined && v !== null).map(normalizeParams)
    return items.every((v) => typeof v !== 'object') ? [...items].sort() : items
  }
  if (typeof value === 'object' && value !== null) {
    const out: Record<string, unknown> = {}
    for (const key of Object.keys(value).sort()) {
      const v = (value as Record<string, unknown>)[key]
      if (v !== undefined && v !== null) {
        out[key] = normalizeParams(v)
      }
    }
    return out
  }
  return value
}

/** Query key for one endpoint call: path template, path params, query params. */
export function queryKey<P extends GetPath>(path: P, args?: RequestArgs<P>) {
  const a = (args ?? {}) as { path?: unknown; query?: unknown }
  return [path, normalizeParams(a.path ?? {}), normalizeParams(a.query ?? {})] as const
}

/**
 * Query key of a paginated list. The cursor is left out: pages of one list
 * live in one infinite query, so the cursor must not split the cache.
 */
export function listQueryKey<P extends GetPath>(path: P, query?: Record<string, unknown>) {
  const rest = { ...query }
  delete rest.cursor
  return ['list', ...queryKey(path, { query: rest } as unknown as RequestArgs<P>)] as const
}
