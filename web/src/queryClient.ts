import { QueryClient } from '@tanstack/react-query'

/** One client for the session. The URL and this cache are the only state. */
export function createQueryClient() {
  return new QueryClient({
    defaultOptions: { queries: { refetchOnReconnect: true } },
  })
}
