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
| `api` | Admin-plane adapter | `core`, `recorder`; no database driver |
| `ui` | Embedded web UI | `core`, application packages |
| `config` | Configuration | `core`, `yaml` (`go.yaml.in/yaml/v3`) |
| `store/postgres` | Postgres adapter | `core`, drivers (`pgx`, goose); its tests and `pgtest` also `testcontainers-go` |
| `store/storetest` | Shared store contract tests | `core` |

Only `cmd/llm-proxy` imports `store/postgres`. A new package needs a reason.

## Checks

```
make build          # static binary in bin/
make vet
make test
make lint           # golangci-lint, including depguard rules (.golangci.yml)
make check-imports  # depguard rules only
```

The store tests in `internal/store/...` need a real Postgres. They start a `postgres:16-alpine` container with testcontainers-go when Docker is running, or use the server in `LLM_PROXY_TEST_DATABASE_URL` (a URL whose user may create databases); without either they skip. Each test gets its own throwaway database.

The `depguard` rules in `.golangci.yml` encode the table above. CI runs build, vet and tests on the latest two Go releases, and lint and the import check once.

## Version

`make build` sets `main.version` with `-ldflags "-X main.version=<version>"`. Override with `make build VERSION=v1.2.3`.
