# Contributing

Design and decisions live in [docs/](docs/): start with [docs/llm-proxy-design.md](docs/llm-proxy-design.md), the [architecture documents](docs/architecture/) (the dependency rule is in `backend.md`, "Layers and the dependency rule") and the [ADRs](docs/adrs/README.md).

## Layout

One Go module, one static binary `llm-proxy` built from `cmd/llm-proxy`. Everything else is under `internal/`:

| Package | Role | May import |
|---|---|---|
| `core` | Domain types, pure rules, storage ports | standard library only |
| `core/coretest` | Fakes of the core ports for consumer tests (test support only) | `core` |
| `family/anthropic` | Anthropic protocol knowledge (pure) | `core`, `gjson` |
| `recorder`, `keyring` | Application use cases | `core` |
| `proxy` | Data-plane adapter | `core`, application packages, `family/*`, `config`; no `store` package |
| `api` | Admin-plane adapter | `core`, `recorder`, `config`; no database driver |
| `ui` | Embedded web UI | `core`, application packages |
| `config` | Configuration | `core`, `yaml` (`go.yaml.in/yaml/v3`) |
| `store/postgres` | Postgres adapter | `core`, drivers (`pgx`, goose); its tests and `pgtest` also `testcontainers-go` |
| `store/storetest` | Shared store contract tests | `core`, `store/postgres` |
| `fakeupstream` | Fake Anthropic upstream that replays fixtures (test support only) | standard library only |

Only `cmd/llm-proxy` and the test-only contract suite `store/storetest` import `store/postgres`. A new package needs a reason.

## Checks

```
make build          # static binary in bin/
make vet
make test
make lint           # golangci-lint, including depguard rules (.golangci.yml)
make check-imports  # depguard rules only
make check-openapi  # lint api/openapi.yaml with Redocly (needs Node)
make ui-build       # build the web UI into internal/ui/dist (needs Node 22+); make build then embeds it
make ui-dev         # Vite dev server, proxies /admin to localhost:9090
make ui-check       # lint, type-check and unit-test web/, and check the generated API types
make ui-gen         # regenerate web/src/api/schema.gen.ts after changing api/openapi.yaml
make goldens        # rewrite the golden call records under testdata/fixtures; read the diff
make e2e            # end-to-end tests: real wiring, fake upstream, test Postgres (needs Docker)
```

Without `internal/ui/dist`, `make build`, `make vet`, `make test` and `make lint` use the `noui` build tag, so a fresh clone needs no Node and the binary serves a stub page at `/ui/`.

The store tests in `internal/store/...` need a real Postgres. They start a `postgres:16-alpine` container with testcontainers-go when Docker is running, or use the server in `LLM_PROXY_TEST_DATABASE_URL` (a URL whose user may create databases); without either they skip. Each test gets its own throwaway database.

The contract suite `storetest.Run` runs against the Postgres adapter from `internal/store/postgres/storetest_test.go`. To run it alone, or all store tests against a local Postgres:

```
go test ./internal/store/postgres -run TestContract -v
LLM_PROXY_TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable' go test ./internal/store/...
```

CI runs it in the `postgres` job against a Postgres service container. A new filter, port method or behaviour starts as a case in a group under `internal/store/storetest`; the suite also scans every stored text and `jsonb` value for the fake prompt, response and tool text of its record generator.

The end-to-end tests (`cmd/llm-proxy/e2e_test.go`, ADR 0038) start the real wiring against the fake upstream in `internal/fakeupstream` and a test Postgres, use `anthropic-sdk-go` as the client and read results through the admin API. They find Postgres the same way the store tests do and skip without one. CI runs them in the `e2e` job, which fails if any of them skipped. `anthropic-sdk-go` and `fakeupstream` are for `_test.go` files only; depguard enforces it. Fixtures, their format and how to refresh them from real traffic are in [testdata/fixtures/README.md](testdata/fixtures/README.md).

The `depguard` rules in `.golangci.yml` encode the table above. CI runs build, vet and tests on the latest two Go releases, and lint, the import check and the OpenAPI lint once. The `web` job runs `make ui-check`, then builds and tests Go with the real UI embedded. The `image` job builds the container image, then runs `scripts/check-image.sh` (size budget and non-root user) and `scripts/smoke-image.sh` (`/healthz`, `/readyz` and `/ui/` against Postgres); both take an image name, so they also run against a local `docker build`. It then runs `scripts/compose-test.sh` (the repository's `docker-compose.yml` against a fake upstream: health, a proxied call, the call log, the UI and `SIGHUP`) and `scripts/restore-test.sh` (`pg_dump`, restore into a fresh Postgres, `serve`, open a recent call). `internal/config` tests load the example configs and check them against the compose file.

## Review tiers

Every PR carries one review label:

- `review: tier A`: no human code review needed. Docs, tests and fixtures.
- `review: tier B`: light human review.
- `review: tier C`: full human review, for example changes to CI, auth, storage or who can merge.

The `tier-a-automerge` workflow (`.github/workflows/tier-a-automerge.yml`) enables GitHub auto-merge (squash) on a PR with the `review: tier A` label, so GitHub merges it once the required CI checks pass. It disables auto-merge again when a labelled PR changes a file outside the Tier A allowlist, or when a PR without the label has auto-merge that the workflow itself enabled (for example after the `review: tier A` label is removed). It never touches auto-merge a maintainer enabled, so auto-merge on a Tier B or C PR stays on. The allowlist is a backstop, not the decision: `docs/`, `testdata/`, `*.md`, `*_test.go` and `*.test.ts(x)`. A labelled PR with other files fails the workflow and lists them. If the workflow cannot list the PR files or read its auto-merge state, it disables auto-merge and fails. Do not label `.github/` changes Tier A; the allowlist still accepts Markdown files there, so this is a labelling rule, not a workflow check. PRs from forks are skipped.

### Dependabot PRs

The `dependabot-automerge` workflow (`.github/workflows/dependabot-automerge.yml`) handles PRs authored by `dependabot[bot]`; `tier-a-automerge` skips them, because they change `go.mod`, `go.sum` and `package-lock.json`, which the Tier A allowlist rejects. It reads the update type and dependency type with `dependabot/fetch-metadata` (pinned by commit SHA). For a grouped PR, every dependency in the group must meet an auto-merge rule.

| Update | Label | Auto-merge |
| --- | --- | --- |
| Patch, any dependency | `review: tier A` | yes |
| Minor, development dependency (the `web-dev-tools` group) | `review: tier A` | yes |
| Minor, GitHub Actions | `review: tier A` | yes |
| Minor, runtime dependency (Go modules, React, TanStack) | `review: tier B` | no |
| Major, anything | `review: tier B` | no |

An auto-merged PR gets GitHub auto-merge (squash), pinned to the head commit, and merges only after the required checks on main pass. A PR that does not qualify gets the tier B label, and the workflow leaves its auto-merge state alone. An unknown update type counts as not qualifying.

`.github/dependabot.yml` adds a cooldown before Dependabot proposes a new version: 3 days for patch, 7 for minor and 7 for major. It also groups patch updates per ecosystem, so there is one weekly patch PR per ecosystem. The cooldown gives the community time to find and yank a compromised release before it reaches `main` unattended. Auto-merge is limited to low-risk update types, and the required CI checks still gate every merge.

## Version

`make build` sets `main.version` with `-ldflags "-X main.version=<version>"`. Override with `make build VERSION=v1.2.3`.
