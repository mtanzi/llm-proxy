import { ApiError, apiErrorFromBody } from './errors'
import type { paths } from './schema.gen'

type JsonGet = { responses: { 200: { content: { 'application/json': unknown } } } }

/** Every path of the spec that has a GET returning JSON. */
export type GetPath = {
  [P in keyof paths]: paths[P] extends { get: JsonGet } ? P : never
}[keyof paths]

type Operation<P extends GetPath> = paths[P]['get' & keyof paths[P]]

export type QueryOf<P extends GetPath> = Exclude<
  Operation<P> extends { parameters: { query?: infer Q } } ? Q : never,
  undefined
>

export type PathParamsOf<P extends GetPath> = Exclude<
  Operation<P> extends { parameters: { path?: infer Q } } ? Q : never,
  undefined
>

export type ResponseOf<P extends GetPath> = Operation<P> extends {
  responses: { 200: { content: { 'application/json': infer R } } }
}
  ? R
  : never

export interface RequestOptions {
  signal?: AbortSignal
}

/** Path and query arguments. Path parameters are required when the path has any. */
export type RequestArgs<P extends GetPath> = ([PathParamsOf<P>] extends [never]
  ? { path?: never }
  : { path: PathParamsOf<P> }) &
  ([QueryOf<P>] extends [never] ? { query?: never } : { query?: QueryOf<P> })

type ArgsTuple<P extends GetPath> = Record<never, never> extends RequestArgs<P>
  ? [args?: RequestArgs<P>, options?: RequestOptions]
  : [args: RequestArgs<P>, options?: RequestOptions]

/** Fills `{name}` in a path template. Values are percent-encoded, including `/` and `:`. */
export function buildPath(template: string, params: Record<string, unknown> = {}): string {
  return template.replace(/\{([^}]+)\}/g, (_, name: string) => {
    const value = params[name]
    if (value === undefined || value === null) {
      throw new Error(`missing path parameter ${name}`)
    }
    return encodeURIComponent(String(value))
  })
}

/** Serialises query values: an array repeats the key, undefined and null are skipped. */
export function buildQuery(query: Record<string, unknown> = {}): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(query)) {
    for (const item of Array.isArray(value) ? value : [value]) {
      if (item !== undefined && item !== null) {
        search.append(key, String(item))
      }
    }
  }
  const text = search.toString()
  return text === '' ? '' : `?${text}`
}

/** A typed GET against the admin API. Throws ApiError on any failure. */
export async function apiGet<P extends GetPath>(
  path: P,
  ...[args, options]: ArgsTuple<P>
): Promise<ResponseOf<P>> {
  const a = (args ?? {}) as { path?: Record<string, unknown>; query?: Record<string, unknown> }
  const url = buildPath(path, a.path) + buildQuery(a.query)
  let response: Response
  try {
    response = await fetch(url, {
      headers: { Accept: 'application/json' },
      signal: options?.signal,
    })
  } catch (cause) {
    if (options?.signal?.aborted) {
      throw cause
    }
    throw new ApiError(0, 'network', cause instanceof Error ? cause.message : 'network error')
  }
  let body: unknown
  try {
    body = await response.json()
  } catch {
    body = undefined
  }
  if (!response.ok) {
    throw apiErrorFromBody(response.status, body)
  }
  if (body === undefined) {
    throw new ApiError(response.status, 'unknown', 'response is not JSON')
  }
  return body as ResponseOf<P>
}
