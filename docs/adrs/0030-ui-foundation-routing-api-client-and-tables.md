# ADR 0030: UI foundation: routing, typed API client, query state and tables

Status: Accepted

Date: 2026-10-21

## Context

Every view shares the same plumbing: URL state, a typed client, a query cache and virtualised tables. A filtered view must be a shareable link. The URL and the query cache are the only state.

## Decision

- **TanStack Router** with typed search parameters. Every filter, the time range, the cursor, the grouping toggle and the active tab are search parameters. Validation rejects unknown values, so a stale URL degrades to defaults.
- **Server state** in the TanStack Query cache, keyed by endpoint and parameters. No separate global store.
- **Typed client:** TypeScript types generated from `api/openapi.yaml` with `openapi-typescript`, and a thin typed `fetch` wrapper. A breaking API change fails the UI type check in CI.
- **Pagination:** forward keyset cursor with `useInfiniteQuery` over `next_cursor`. No page numbers.
- **Caching rules:** call detail is cached without expiry for the session; lists refetch on focus and when the live toggle is off.
- **Errors:** API errors carry a stable `type`; the UI maps them to inline messages.
- **TanStack Table** for columns, sorting of loaded rows, column visibility and sizing; **TanStack Virtual** for row virtualisation.
- A shared layout, navigation, time-range picker, filter menu primitives and a single "Content is not stored" notice used where a reader might expect content.

## Consequences

- Views (ADRs 0031 to 0035) only add routes, queries and columns.
- Search over text does not exist, so there is no search route.

## Work required

- Add the router setup with the route tree for `/ui/calls`, `/ui/calls/:id`, `/ui/sessions`, `/ui/sessions/:key`, `/ui/tools`, `/ui/cost`, `/ui/admin/config` and a redirect from `/ui/` to `/ui/calls`.
- Add the generated types step to the build (`npm run gen:api`) and the CI check that generated output is current.
- Implement the typed `fetch` wrapper, the query key helpers, the error mapper, and the infinite query helper.
- Implement the shared table component (columns, virtualised rows, loading and empty states), the time-range and filter primitives, and the layout.
- Add the "Content is not stored" notice component and the unknown-value formatter (null shows unknown, not zero).
- Tests: search-parameter validation, query key stability, table virtualisation with thousands of rows, accessibility checks for the layout.

## Depends on

- ADR 0023, ADR 0029.

## References

- architecture/frontend.md, "Routing and URL state", "State and data fetching", "Table virtualization", "Rendering", "What the UI can show without bodies".
