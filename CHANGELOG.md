# Changelog

Notable changes to Spyglass. Release 0.1.0 and earlier are described in their [GitHub release notes](https://github.com/mtanzi/spyglass/releases).

## Unreleased

### Renamed from llm-proxy to Spyglass

Every technical name changes from `llm-proxy` to `spyglass` ([ADR 0054](docs/adrs/0054-rename-to-spyglass.md)). Existing deployments keep their data. Upgrade steps are in the [runbook](docs/runbook.md#upgrading-from-llm-proxy-010).

- **Breaking: Prometheus metrics are renamed** from `llm_proxy_*` to `spyglass_*`, for example `llm_proxy_requests_total` is now `spyglass_requests_total`. There is no alias. Update queries, recording rules, alerts and dashboards when you upgrade.
- **Key header: `x-spyglass-key`.** The old `x-llm-proxy-key` is deprecated. It still works, with the same checks, and the proxy logs one warning per process the first time a client uses it. When a request carries both, `x-spyglass-key` is used.
- **Environment variables: `SPYGLASS_*`**, for example `SPYGLASS_CONFIG`, `SPYGLASS_DATABASE_URL`, `SPYGLASS_LOG_LEVEL` and `SPYGLASS_IMAGE`. The old `LLM_PROXY_*` names are deprecated. Each one is still read when its `SPYGLASS_` name is unset, and `serve` logs a warning that names it. The fallback only goes from a `SPYGLASS_` name to its `LLM_PROXY_` name: when you rename an exported variable, also update any secret reference in `config.yaml` that names it, for example `{ env: LLM_PROXY_DATABASE_URL }` to `{ env: SPYGLASS_DATABASE_URL }`. The contributor test variables are renamed without a fallback: `SPYGLASS_TEST_DATABASE_URL` and `SPYGLASS_SIZING_ROWS`.
- **Image: `ghcr.io/mtanzi/spyglass`.** The 0.1.0 image stays at `ghcr.io/mtanzi/llm-proxy:0.1.0`.
- **Binary: `spyglass`.** Release binaries are named `spyglass-<tag>-<os>-<arch>`, and the commands are `spyglass serve`, `spyglass keygen` and `spyglass version`. The Go module is `github.com/mtanzi/spyglass`.
- **Compose:** the service is now `spyglass`. The project name stays `llm-proxy` and the database stays `llmproxy`, so the existing Postgres volume is kept. Run `docker compose up -d --remove-orphans` once to remove the 0.1.0 container.
- The deprecated `x-llm-proxy-key` header and `LLM_PROXY_*` variables are removed in 0.3.0.
