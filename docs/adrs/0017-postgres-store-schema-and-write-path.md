# ADR 0017: Postgres store: schema, migrations and write path

Status: Accepted

Date: 2026-10-21

## Context

`store/postgres` is the only v1 adapter (decision D14). It owns its SQL and migrations: hand-written `pgx` queries, goose migrations embedded in the binary, no ORM. The schema is two plain unpartitioned tables, written by insert only.

## Decision

- **Tables:** `calls` (one row per LLM call, `PRIMARY KEY (id)`, `id` a UUIDv7) and `tool_events` (one row per tool-call half, `PRIMARY KEY (call_id, idx)`, `kind` `use` or `result`, `source` `client` or `server`). Column lists follow backend.md, "Data model", including `context_compacted`; the migrations become the source of truth.
- **Indexes:** `calls (ts DESC, id DESC)`, `calls (session_key, ts)`, `calls (key_name, ts DESC)`, partial `calls (ts DESC) WHERE error_class IS NOT NULL`; `tool_events (tool_call_id)`, `(ts DESC)`, partial `(tool_name, ts DESC) WHERE kind = 'use'`.
- **Write path:** one transaction per batch with multi-row `INSERT … ON CONFLICT DO NOTHING` into both tables. The port contract is "idempotent on call id and atomic per batch".
- **Lifecycle on the concrete adapter:** `postgres.Open(cfg)`, `(*Store).Migrate`, `(*Store).Ping`, `(*Store).DeleteBefore(table, cutoff)` (ADR 0022), `(*Store).Close`.
- **Migrations:** goose SQL files in `internal/store/postgres/migrations/`, embedded with `go:embed`, applied by `serve` at start under goose's session lock, before the listeners open. Every migration is expand then contract, so a one-version rollback needs no restore.
- Postgres 16 or later. Pool size from `database.max_conns`.

## Consequences

- Plain tables at 12,000 to 50,000 calls per day are ample. Partitioning, rollups and `CopyFrom` staging are later items with triggers.
- No foreign keys on fact tables by design, so halves can arrive in any order and a removed key leaves history readable.
- The adapter is a private detail: nothing outside it knows table names.

## Work required

- Create the first goose migration with the two tables and their indexes.
- Implement `Open`, `Migrate`, `Ping` and `Close` with the embedded migration filesystem.
- Implement `WriteBatch`: map `core.CallRecord` and its tool halves to rows, build the multi-row inserts, run both in one transaction, return after commit. Chunk very large batches under the Postgres parameter limit.
- Verify that `usage_raw` is stored as `jsonb` and that the no-bodies rule holds for every column (ADR 0018 test).
- Add a migration policy note: expand then contract, never edit a shipped migration.
- Tests run against a real Postgres through the harness in ADR 0018: first write, replay inserts nothing, failed batch leaves nothing, out-of-order halves, migrations apply from empty and are idempotent.

## Depends on

- ADR 0002, ADR 0003, ADR 0004.

## References

- architecture/backend.md, "Storage ports and adapters" (Adapters), "Recorder" (Writer details), "Data model".
- architecture/deployment.md, "Migrations".
- architecture/README.md, D14, C2.
