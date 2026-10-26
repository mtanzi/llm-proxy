# ADR 0046: Adopt the gap-review defaults (D9 and D10)

Status: Accepted

Date: 2026-10-21

## Context

The requirements gap review listed blocking gaps (B1, B3 to B8, B10) and rework and risk gaps (R1 to R17). The architecture documents follow the review's suggestions, adjusted by decisions C1 to C7 (decisions D9 and D10). Most are already fixed by other ADRs, so this ADR lists where each default lands and confirms them as a set.

## Decision

Accepted (D9, D10), with the recommended option: adopt the defaults used in the documents.

| Gap | Default | ADR |
|---|---|---|
| B1 schema | Plain unpartitioned tables, `PRIMARY KEY (id)`, UUIDv7 | 0017 |
| B2 protocol scope | Anthropic-family passthrough only | 0002, 0011 |
| B3 model names | Forwarded as sent | 0011 |
| B4 fallback, B7 budgets, R9 alerts | Deferred | 0002 |
| B5 queue | Bounded channel, batch writer, idempotent insert, no durable buffer | 0016 |
| B6 usage | Disjoint columns plus `usage_raw` | 0014 |
| B8 providers | Config-file providers with credential references | 0004 |
| B10 Go baseline | Latest two releases, one static binary, distroless | 0001, 0036 |
| R1 keys | `lp_` keys, SHA-256 in the config file, no pepper | 0008 |
| R8 admin auth | None; loopback-only admin listener | 0006 |
| R10 retention | Batched `DELETE` | 0022 |
| R14 multi-replica | One replica; 10-minute drain | 0027 |
| R15 targets | p99 overhead under 5 ms, 200 streams, 32 MiB body | 0039 |
| R16 self-observability | `/healthz`, `/readyz`, `/metrics`, `traceparent` | 0026 |

Rejected option: take each gap's own alternatives from the gap review, which would change the matching ADRs.

## Consequences

- The set closes D9 and D10. The README lists both as resolved decisions.
- Changing one default changes only the ADR named in the table.

## Work required

- No code. If a default changes later, open a follow-up ADR that names the ADR in the table it changes.

## Depends on

- ADR 0002.

## References

- architecture/README.md, "Resolved decisions" (D9, D10).
- architecture/backend.md, "Defaults taken for open gaps".
