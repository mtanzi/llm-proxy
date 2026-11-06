import { infiniteQueryOptions, queryOptions } from '@tanstack/react-query'
import { apiGet } from './client'
import type { GetPath, QueryOf, RequestArgs, ResponseOf } from './client'
import { isRetryable } from './errors'
import { listQueryKey, queryKey } from './keys'

/** Paths whose response is one page of a keyset-paginated list. */
export type ListPath = {
  [P in GetPath]: ResponseOf<P> extends { next_cursor: string | null } ? P : never
}[GetPath]

// Retry transient failures only; a 4xx answer is final.
const retry = (count: number, error: unknown) => count < 2 && isRetryable(error)

/**
 * Caching rules (ADR 0030). A finished call never changes, so detail is kept
 * for the session. Lists refetch on window focus, except while the live
 * toggle is on, because the stream keeps the list current.
 */
export function detailQueryOptions<P extends GetPath>(path: P, args?: RequestArgs<P>) {
  return queryOptions({
    queryKey: queryKey(path, args),
    queryFn: ({ signal }) => apiGet(path, ...([args, { signal }] as never)),
    staleTime: Infinity,
    gcTime: Infinity,
    retry,
  })
}

/** A non-paginated read that stays current: usage, summaries, config. */
export function readQueryOptions<P extends GetPath>(
  path: P,
  args?: RequestArgs<P>,
  { live = false }: { live?: boolean } = {},
) {
  return queryOptions({
    queryKey: queryKey(path, args),
    queryFn: ({ signal }) => apiGet(path, ...([args, { signal }] as never)),
    staleTime: 0,
    refetchOnWindowFocus: !live,
    retry,
  })
}

/** Forward keyset pagination over `next_cursor`. Page numbers do not exist. */
export function infiniteListOptions<P extends ListPath>(
  path: P,
  query?: Omit<QueryOf<P>, 'cursor'>,
  { live = false }: { live?: boolean } = {},
) {
  return infiniteQueryOptions({
    queryKey: listQueryKey(path, query as Record<string, unknown> | undefined),
    queryFn: ({ pageParam, signal }) =>
      apiGet(path, ...([{ query: { ...query, cursor: pageParam } }, { signal }] as never)) as Promise<
        ResponseOf<P> & { next_cursor: string | null }
      >,
    initialPageParam: undefined as string | undefined,
    getNextPageParam: (last) => last.next_cursor ?? undefined,
    staleTime: 0,
    refetchOnWindowFocus: !live,
    retry,
  })
}
