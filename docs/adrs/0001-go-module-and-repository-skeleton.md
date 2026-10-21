# ADR 0001: Go module and repository skeleton

Status: Accepted

Date: 2026-10-21

## Context

llm-proxy has no code yet. Every later ADR adds packages, so the module layout, the toolchain and the CI checks that guard the layering must exist first. The backend document fixes the package layout and the dependency rules; this ADR turns them into an empty but buildable repository.

## Decision

- One Go module, one static binary `llm-proxy` built from `cmd/llm-proxy`.
- Packages under `internal/`: `core`, `family/anthropic`, `proxy`, `keyring`, `recorder`, `api`, `config`, `store/postgres`, `store/storetest`, `ui`. Plus `api/openapi.yaml`, `web/`, `testdata/fixtures/` and `docs/`.
- Go version: the latest two Go releases (1.26 and 1.27 today).
- Libraries: `net/http` and `httputil`, `tidwall/gjson`, `jackc/pgx/v5` and goose (inside `store/postgres` only), `log/slog`, the Prometheus Go client. No web framework, no ORM, no `fasthttp`.
- An import check in CI enforces the dependency rules (a `depguard` rule or a `go list -deps` script).
- Only `cmd/llm-proxy` may import `store/postgres`.

## Consequences

- Each later ADR lands in a package that already exists, with the import rules already enforced.
- The CI job that needs a database (ADR 0018) is added when the first store code exists, not here.
- The package count stays small (10 under `internal/`). A new package needs a reason.

## Work required

- Run `go mod init`, add `cmd/llm-proxy/main.go` with a stub, and create the empty package directories with a `doc.go` each.
- Add a `Makefile` (or `justfile`) with `build`, `test`, `lint` and `check-imports` targets.
- Add `golangci-lint` configuration, including `depguard` rules that encode the ring table: `core` imports the standard library only; `family/*` imports `core` and `gjson`; `recorder`, `keyring` and `api` never import `store/postgres` or a database driver; `store/postgres` imports `core` and drivers only; `proxy` imports no `store` package.
- Add a CI workflow that runs build, vet, lint, unit tests and the import check.
- Add the `-X main.version` link flag convention used by the `version` subcommand (ADR 0009).
- Add `.gitignore` entries for `internal/ui/dist/`, `web/dist/` and `node_modules/`.
- Document the package layout and the rules in a short `CONTRIBUTING.md` or the repository README, pointing at the architecture documents.

## Depends on

None.

## References

- architecture/backend.md, "Package layout", "Layers and the dependency rule", "Libraries".
- architecture/frontend.md, "Build and embedding".
