# ADR 0037: Deployment guide: compose file, runbook and personal local setup

Status: Accepted

Date: 2026-10-21

## Context

v1 builds the single-container tier: `llm-proxy`, Postgres, an optional TLS front and optional Prometheus. Operators need a working compose file, a runbook for the recurring tasks, and the personal local setup for one person's own Claude login (ADR 0012).

## Decision

Ship these as repository files, tested where possible:

- A `docker-compose.yml` and `config.example.yaml` matching the deployment sketch: Postgres 16 with a volume and healthcheck, `llm-proxy` with the config mounted read-only, secrets as files, the data plane published on `127.0.0.1:8080`, `stop_grace_period: 10m`.
- A runbook covering: generating keys (`keygen` and the `openssl` route), adding, revoking and rotating keys with `SIGHUP`, rotating provider keys and the database password, upgrades (back up with `pg_dump`, pull, restart, expand-only migrations, one-version rollback), backups and a restore test each release cycle, TLS and network exposure (Caddy, `tailscale serve`, WAF exemption for `/v1/messages`, no response buffering and idle timeouts above 10 minutes), reaching the loopback admin UI by an SSH tunnel, and the metrics list.
- The personal local setup guide: loopback config with `auth_mode: client_passthrough`, `~/.claude/settings.json` with `ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` and the hint-header variable, `/login`, and the must and must-not rules and terms notes.
- Sizing notes updated from measurements (ADR 0039).

## Consequences

- The compose file does not expose the admin plane (loopback inside the container). The UI is reached by running `serve` on the host or with host networking on Linux.
- Multi-replica deployment is documented as later and not built.

## Work required

- Write `docker-compose.yml`, `config.example.yaml` and example secret files with placeholders.
- Write `docs/runbook.md` and `docs/personal-local-setup.md` from the architecture documents without restating the design.
- Test the compose file in CI: start, wait for health, send a request to a fake upstream, check a row and the UI.
- Add a restore test script: dump, restore into a fresh Postgres, start `serve`, open a recent call.
- Review all docs for the no-sign-in warning on the admin plane.

## Depends on

- ADR 0009, ADR 0012, ADR 0036.

## References

- architecture/deployment.md (all sections).
- architecture/upstream-connection.md, "Claude Code with a proxy-held API key", "Claude Code with a Claude subscription".
