# ADR 0029: UI scaffold, embedding and serving

Status: Accepted

Date: 2026-10-21

## Context

The captain chose a React single-page application embedded in the Go binary, with a Go JSON API and SSE live tail. The build output is static files embedded with `go:embed`, so there is one binary and one container, and Node is needed only at build time.

## Decision

- `web/`: React and Vite, TypeScript, no Next.js and no server-side rendering. Pinned versions, committed lockfile, `base: '/ui/'`, a dev proxy for `/admin` to `localhost:9090`.
- `internal/ui`: `//go:embed all:dist` and the SPA handler. `web/dist` is copied to `internal/ui/dist` by the build; `dist` is not committed.
- Served by the admin listener under `/ui/`.
- Serving rules: `/ui/assets/*` gets `Cache-Control: public, max-age=31536000, immutable`; `index.html` gets `no-cache`; any other `/ui/*` path that is not a file returns `index.html`.
- Content Security Policy on `/ui/*`: `default-src 'self'`, no inline scripts, no third-party origins. Stored strings render as text, never HTML.
- A build tag `noui` replaces the embed with a stub page, for Go-only development and tests without Node.
- Build steps: `npm ci && npm run build`, copy `dist`, `go build`.

## Consequences

- npm dependency upkeep is the main maintenance cost of choosing React. Pin versions and update on a routine.
- A fresh clone builds the Go binary with `noui` and no Node.

## Work required

- Create `web/` with the Vite React TypeScript template, the lint and type-check configuration, the test runner and a placeholder app that loads at `/ui/`.
- Implement `internal/ui` with the embed, the handler, the cache headers, the SPA fallback and the `noui` stub; mount it on the admin mux.
- Add the CSP header middleware for `/ui/*`.
- Add Make targets (`ui-build`, `ui-dev`, `build`) and a CI job for `npm ci`, lint, type check, unit tests and build.
- Add a dependency-update routine (Dependabot or Renovate config).
- Tests: deep link and reload return `index.html`; asset caching headers; `noui` build compiles; CSP header present.

## Depends on

- ADR 0001, ADR 0006, ADR 0023.

## References

- architecture/frontend.md, "Decision and shape", "Build and embedding", "Admin API access".
