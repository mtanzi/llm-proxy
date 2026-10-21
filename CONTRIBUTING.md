# Contributing

Design and decisions live in [docs/](docs/): start with [docs/llm-proxy-design.md](docs/llm-proxy-design.md), the [architecture documents](docs/architecture/) (the dependency rule is in `backend.md`, "Layers and the dependency rule") and the [ADRs](docs/adrs/README.md).

## Layout

One Go module, one static binary `llm-proxy` built from `cmd/llm-proxy`. Everything else is under `internal/`:

| Package | Role | May import |
|---|---|---|
| `core` | Domain types, pure rules, storage ports | standard library only |
| `family/anthropic` | Anthropic protocol knowledge (pure) | `core`, `gjson` |
| `recorder`, `keyring` | Application use cases | `core` |
| `proxy` | Data-plane adapter | `core`, application packages, `family/*`, `config`; no `store` package |
| `api` | Admin-plane adapter | `core`, `recorder`; no database driver |
| `ui` | Embedded web UI | `core`, application packages |
| `config` | Configuration | `core` |
| `store/postgres` | Postgres adapter | `core`, drivers (`pgx`, goose) |
| `store/storetest` | Shared store contract tests | `core` |

Only `cmd/llm-proxy` imports `store/postgres`. A new package needs a reason.

## Checks

```
make build          # static binary in bin/
make vet
make test
make lint           # golangci-lint, including depguard rules (.golangci.yml)
make check-imports  # scripts/check-imports.sh, same rules via go list
```

The `depguard` rules in `.golangci.yml` and `scripts/check-imports.sh` encode the same table; change both together. CI runs all of them on the latest two Go releases.

## Version

`make build` sets `main.version` with `-ldflags "-X main.version=<version>"`. Override with `make build VERSION=v1.2.3`.
