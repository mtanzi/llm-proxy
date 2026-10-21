# ADR 0004: Configuration file and secret references

Status: Accepted

Date: 2026-10-21

## Context

Providers, pricing and virtual keys live in one YAML config file (decisions D7, C3, D15). The database holds call records only. Secrets are never inlined; they are referenced by environment variable or file. The loader must reject a bad file whole so a mistaken edit never leaves the proxy half-configured.

## Decision

- One YAML file; path from `--config` or `LLM_PROXY_CONFIG`.
- Every key can be overridden by an environment variable named `LLM_PROXY_<SECTION>_<KEY>`.
- Sections: `listen` (`data`, `admin`, optional `tls`), `database` (`url`, `max_conns`), `providers`, `keys`, `pricing`, `retention`, `server` (`drain_timeout`, `max_request_bytes`), `queue_capacity`, `log`.
- Secret references are `{ env: NAME }` or `{ file: PATH }`. A secret is read at load and again on reload.
- Batch size, batch interval, the SSE event bound and the shutdown flush timeout are constants in code. `queue_capacity` is the one writer setting.
- Validation rejects: a non-loopback `listen.admin`, a duplicate key name, a duplicate key hash, a malformed hash (not 64 lowercase hex), an unknown provider kind or `auth_mode`, an unreadable secret reference, and overlapping or malformed pricing entries.
- Duration and size syntax: `10m`, `120s`, `400d`, `32MiB`.

## Consequences

- The file holds hashes, not secrets, so it can sit in version control. Anyone who can edit it can add a key: write access needs the usual deploy-config care.
- Listener addresses need a restart; everything else reloads (ADR 0005).
- A new setting must justify itself against the "constants in code until measured" rule.

## Work required

- Define the typed config structs in `internal/config` and the YAML decoder with strict unknown-field rejection.
- Implement duration and size parsing, including the `d` suffix for days.
- Implement the environment override layer and the secret reference resolver, with redaction so a resolved secret never reaches logs or the admin API.
- Implement validation for every rule above, with table tests that cover each rejection and a golden valid file.
- Ship an example `config.example.yaml` that matches deployment.md, with placeholder hashes marked as such.
- Surface the validated `providers`, `pricing` and key names (never hashes) as read-only data for `GET /admin/config` (ADR 0023).

## Depends on

- ADR 0001, ADR 0003.

## References

- architecture/deployment.md, "Configuration", "Secrets".
- architecture/upstream-connection.md, "Provider configuration".
- architecture/backend.md, "Configuration".
- architecture/README.md, C3, D7, D15, C7.
