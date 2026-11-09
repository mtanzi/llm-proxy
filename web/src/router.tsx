import { QueryClient } from '@tanstack/react-query'
import {
  Outlet,
  createRootRouteWithContext,
  createRoute,
  createRouter,
  redirect,
} from '@tanstack/react-router'
import type { RouterHistory } from '@tanstack/react-router'
import { Layout } from './components/Layout'
import { Placeholder } from './routes/Placeholder'
import { CallDetailView } from './views/CallDetail'
import { CallLogView } from './views/CallLog'
import { SessionListView } from './views/SessionList'
import { SessionView } from './views/SessionView'
import { strict } from './search/validate'
import {
  validateCallDetailSearch,
  validateCallsSearch,
  validateCostSearch,
  validateNoSearch,
  validateSessionDetailSearch,
  validateSessionsSearch,
  validateToolsSearch,
} from './search/routes'

export interface RouterContext {
  queryClient: QueryClient
}

const rootRoute = createRootRouteWithContext<RouterContext>()({ component: Layout })

// The SPA is mounted at /ui/ (ADR 0029); the router's basepath is /ui, so
// these paths carry no prefix. Views replace the placeholder components.
const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '/',
  beforeLoad: () => {
    throw redirect({ to: '/calls', replace: true })
  },
})

const callsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'calls',
  component: Outlet,
})
const callsIndexRoute = createRoute({
  getParentRoute: () => callsRoute,
  path: '/',
  validateSearch: strict(validateCallsSearch),
  component: CallLogView,
})
const callDetailRoute = createRoute({
  getParentRoute: () => callsRoute,
  path: '$id',
  validateSearch: strict(validateCallDetailSearch),
  component: CallDetailView,
})

const sessionsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'sessions',
  component: Outlet,
})
const sessionsIndexRoute = createRoute({
  getParentRoute: () => sessionsRoute,
  path: '/',
  validateSearch: strict(validateSessionsSearch),
  component: SessionListView,
})
const sessionDetailRoute = createRoute({
  getParentRoute: () => sessionsRoute,
  path: '$key',
  validateSearch: strict(validateSessionDetailSearch),
  component: SessionView,
})

const toolsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'tools',
  validateSearch: strict(validateToolsSearch),
  component: () => <Placeholder title="Tools" />,
})

const costRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'cost',
  validateSearch: strict(validateCostSearch),
  component: () => <Placeholder title="Cost" />,
})

const configRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: 'admin/config',
  validateSearch: strict(validateNoSearch),
  component: () => <Placeholder title="Config" />,
})

const notFoundRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: '$',
  beforeLoad: () => {
    throw redirect({ to: '/calls', replace: true })
  },
})

export const routeTree = rootRoute.addChildren([
  indexRoute,
  callsRoute.addChildren([callsIndexRoute, callDetailRoute]),
  sessionsRoute.addChildren([sessionsIndexRoute, sessionDetailRoute]),
  toolsRoute,
  costRoute,
  configRoute,
  notFoundRoute,
])

export function createAppRouter(queryClient: QueryClient, history?: RouterHistory) {
  return createRouter({
    routeTree,
    history,
    basepath: '/ui',
    context: { queryClient },
    defaultPreload: 'intent',
  })
}

declare module '@tanstack/react-router' {
  interface Register {
    router: ReturnType<typeof createAppRouter>
  }
}
