# ADR 0009: CLI subcommands: `keygen` and `version`

Status: Accepted

Date: 2026-10-21

## Context

Keys are generated outside the proxy so the proxy keeps nothing and the database holds no key material. `openssl` and `sha256sum` work, but a helper removes the manual steps and the `lp_` format mistakes. The binary has three subcommands: `serve`, `keygen` and `version`.

## Decision

- `llm-proxy keygen --name <name>` draws 32 bytes from the system random source, prints the plaintext key once (`lp_` plus base62), and prints the `keys` entry to paste (`name` and `sha256`). It touches no config file and no database.
- `llm-proxy version` prints the build version set with `-X main.version`.
- `llm-proxy serve` is defined by ADR 0028.
- A key name must be non-empty and may contain `:` (for example `fm:<task-id>`).

## Consequences

- The proxy cannot show a key again. The operator hands the plaintext to the client and keeps it nowhere else.
- The helper and the manual `openssl` route produce interchangeable keys because the proxy hashes any presented string.

## Work required

- Implement subcommand dispatch in `cmd/llm-proxy` (standard library `flag`, no CLI framework).
- Implement `keygen`: base62 encoding of 32 random bytes, SHA-256 hex, output with the plaintext on one clearly marked line and the YAML entry on the following lines. Reject an empty or whitespace-containing name.
- Implement `version`.
- Tests: output format golden test, hash matches `sha256` of the printed key, two runs differ, and the printed entry passes config validation (ADR 0004).
- Document the equivalent `openssl rand -hex 32` and `sha256sum` or `shasum -a 256` commands in the runbook (ADR 0037).

## Depends on

- ADR 0001, ADR 0004, ADR 0008.

## References

- architecture/backend.md, "Shape", "Auth (virtual keys)".
- architecture/deployment.md, "Secrets".
