# ADR 0018: Contract test suite and Postgres test harness

Status: Accepted

Date: 2026-10-21

## Context

There is no in-memory store adapter (decision C2): a second implementation of the read side would have to reimplement filters, keyset paging, percentiles and tool pairing in Go. The contract suite `internal/store/storetest` is the definition of correct storage behaviour, written against the ports so a later adapter can reuse it. It runs against a real Postgres.

## Decision

- `storetest.Run(t, newStore func(t *testing.T) *postgres.Store)` in v1, with a throwaway database per run.
- The test Postgres is a container started per test run (for example with `testcontainers-go`), or `LLM_PROXY_TEST_DATABASE_URL` when set. Tests need Docker or a local Postgres, and CI runs them.
- The suite covers: write (exactly-once, replay, failed batch, halves in any order); tool-call pairing at read; sessions and agents; pagination (total `(ts, id)` order, no duplicates or gaps while rows are inserted, time bound, capped counts); every `CallFilter` field alone and combined; detail with tool halves; usage sums and percentiles; retention; and **no bodies**.
- **No bodies:** the suite writes records, scans every stored value it can reach through a store-specific hook, and fails if any text from the test's fake prompt, response or tool arguments is present.
- A new filter, port method or behaviour starts as a suite case.
- Unit tests of `recorder`, `keyring` and `proxy` use small fakes of the narrow ports instead.

## Consequences

- Contributors without Docker can run unit tests only. CI is the backstop.
- The suite is the review gate for every `CallReader` change (ADRs 0019 to 0022 each add cases).

## Work required

- Add the Postgres harness: container start, database creation per test, cleanup, and the environment variable override.
- Add the CI job that provides Postgres (service container or Docker) and runs the suite.
- Implement `storetest.Run` with the structure for case groups and a record generator that produces calls, sessions, agents and tool halves with controllable timestamps and ids.
- Implement the first group (write behaviour) now; later ADRs add their groups with their code.
- Implement the no-bodies scan hook for the Postgres adapter (dump every text-like column and `jsonb` value).
- Document how to run the suite locally.

## Depends on

- ADR 0003, ADR 0017.

## References

- architecture/backend.md, "Tests and fakes", "Contract test suite".
- architecture/README.md, C2, D14.
