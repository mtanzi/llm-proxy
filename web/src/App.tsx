import { QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider } from '@tanstack/react-router'
import type { QueryClient } from '@tanstack/react-query'
import type { createAppRouter } from './router'

export function App({
  queryClient,
  router,
}: {
  queryClient: QueryClient
  router: ReturnType<typeof createAppRouter>
}) {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}
