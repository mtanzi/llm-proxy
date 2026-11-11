# Spyglass

Spyglass shows you what your AI coding agents do with the model. It is a self-hosted, observe-only proxy for LLM APIs. Point Claude Code at it, and every call passes through to the Anthropic API unchanged. Spyglass records the facts about each call and shows them in a web UI. Prompts and responses are never stored.

> Spyglass is the product name. The binary, the commands, the config file, the Docker image and this repository are still named `llm-proxy`, so every command below says `llm-proxy`.

## What it shows you

- **Every call:** time, model, client, key, status, latency, time to first token, tokens in and out, cache reads and writes, and cost.
- **Sessions as a tree:** each Claude Code session, split into prompts, agents and sub-agents, calls and tool calls, beside a waterfall on a shared time axis. Idle gaps collapse, so the work stays visible.
- **Tool calls:** the name, ids, argument and result sizes, duration, and success or error of each tool call. Their content is not stored.
- **Cost and health:** cost per model, client, key and session; error rate and error classes; cache hit ratio; p50 and p95 latency. Each number is compared with the previous period.

## Why run it

- See where tokens and money go, per session and per sub-agent, instead of one monthly total.
- Find the failing or slow calls (rate limits, overloads, long tool runs) and the session they belong to.
- Check whether prompt caching works for your setup.
- Keep the record on your own machine or server. Spyglass stores metadata only, so the database never holds your code or conversations.

## Features

- Transparent passthrough with streaming. Claude Code works as before.
- Works with an Anthropic API key held by the proxy, or, on a personal machine, with the sign-in of your own Claude Code. The proxy forwards that login to Anthropic and never stores it.
- Virtual keys per person, machine or agent, stored as hashes in the config file.
- Live tail of new calls in the UI.
- Postgres storage with a retention job, Prometheus metrics, and health and readiness checks.
- One static Go binary with the UI embedded, or one container beside Postgres.
- Dark and light themes that follow the system setting, with a toggle in the sidebar.

## UI pages

| Page | Path | What it shows |
|---|---|---|
| Overview | `/ui/` | Headline numbers against the previous period, activity by model, a live feed, recent sessions, problems and cost breakdowns |
| Calls | `/ui/calls` | Every call, with filters, grouping by session, a volume chart and a live toggle |
| Call detail | `/ui/calls/:id` | Who made the call, timings, usage and cost, outcome, and the tools requested and resolved |
| Sessions | `/ui/sessions` | One row per session with duration, calls, tool calls, cost, cache hit and errors |
| Session view | `/ui/sessions/:key` | The tree and the time waterfall of prompts, agents, calls and tool calls |
| Tools, Cost, Config | `/ui/tools`, `/ui/cost`, `/ui/admin/config` | Placeholder pages, not linked in the sidebar yet: the tool-call explorer, the cost and cache page and the read-only config page are planned ([ADR 0034](docs/adrs/0034-ui-v4-tool-call-explorer.md), [ADR 0035](docs/adrs/0035-ui-v5-cost-and-cache-and-config-page.md)) |

## Quick start: personal local setup

One person, one machine, Claude Code signed in with their own account. The proxy forwards that sign-in and never stores it. You need Docker, Go, Node 22, `curl` and `openssl`. The full procedure, with troubleshooting, is in [docs/personal-local-setup.md](docs/personal-local-setup.md).

```sh
# 1. Postgres on 127.0.0.1:5432
chmod 700 secrets && openssl rand -hex 24 > secrets/pg_password && chmod 644 secrets/pg_password
docker compose -f docker-compose.yml -f docker-compose.personal.yml up -d --wait postgres

# 2. Build the binary with the UI embedded
make ui-build build

# 3. Config and a virtual key (put the printed entry under keys: in config.yaml)
cp config.personal.example.yaml config.yaml
bin/llm-proxy keygen --name me-laptop

# 4. Run it: calls on 127.0.0.1:8080, UI and admin API on 127.0.0.1:9090
export LLM_PROXY_DATABASE_URL="postgres://llmproxy:$(cat secrets/pg_password)@127.0.0.1:5432/llmproxy?sslmode=disable"
bin/llm-proxy serve --config config.yaml
```

## Route Claude Code through it

Add this to your user settings, `~/.claude/settings.json`, with the key that `keygen` printed. Do not put it in a project's committed settings: the key is a credential.

```json
{
  "env": {
    "ANTHROPIC_BASE_URL": "http://127.0.0.1:8080",
    "ANTHROPIC_CUSTOM_HEADERS": "x-llm-proxy-key: lp_…",
    "CLAUDE_CODE_GATEWAY_HINT_HEADERS": "1"
  }
}
```

Then start `claude` and sign in with `/login` as usual. The hint headers give Spyglass the session tree and tool durations. In this mode, do not set `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` or `apiKeyHelper`. With a proxy-held API key (the compose deployment), the client setup is in [upstream-connection.md](docs/architecture/upstream-connection.md#claude-code-with-a-proxy-held-api-key).

## Open the UI

Open <http://127.0.0.1:9090/ui/>. The newest call is also at `curl -s 'http://127.0.0.1:9090/admin/calls?limit=1'`.

> **The admin plane has no sign-in.** The UI and the `/admin/*` API show every call to anyone who can reach port 9090. They listen on loopback only, and `serve` refuses any other address. Do not forward, tunnel or proxy that port to anyone else. To reach the UI of a compose deployment, see the [runbook](docs/runbook.md#reach-the-admin-ui).

## Security

Spyglass has two listeners with different trust rules. Read this before you run it anywhere but your own machine.

- **The admin UI and the admin API (`/ui`, `/admin/*`, port 9090) have no sign-in, by design.** Anyone who can reach that port sees every recorded call. They listen on loopback only, and `serve` refuses any other address.
- **Never expose port 9090 or the admin API to a network.** Do not publish it from a container, forward or tunnel it, or put a reverse proxy in front of it.
- **The data plane (port 8080) needs a virtual key** (`x-llm-proxy-key` or `Authorization`). Treat these keys as credentials. Keep them out of committed files. Only hashes are stored in the config file.
- Spyglass stores metadata only. Prompts and responses are never stored.

## Deploy with compose

For a shared or always-on setup, run `llm-proxy` and Postgres from [docker-compose.yml](docker-compose.yml) with an Anthropic API key. The [runbook](docs/runbook.md) covers the first start, keys, secret rotation, upgrades, backups, TLS, metrics and sizing. The design behind it is in [deployment.md](docs/architecture/deployment.md).

## Configuration

- [config.example.yaml](config.example.yaml) is the compose config. [config.personal.example.yaml](config.personal.example.yaml) is the loopback config for the personal setup.
- The config file holds the listeners, the database, providers, virtual keys (as hashes), pricing, retention and queue sizes. Secrets are referenced from files or environment variables, never written inline.
- After you edit the config file, send `SIGHUP` to the `serve` process to reload it without a restart.
- Every setting and secret is described in [deployment.md, Configuration](docs/architecture/deployment.md#configuration).

## Scope

In v1:

- Forwards, records and shows calls for the Anthropic family, with Claude Code as the day-one harness.
- Stores facts about each call: who, provider, tokens, cost, timings, outcome and tool-call metadata (name, ids, sizes, duration, success or error).
- Inserts call records only. Tool-call pairing, sessions, agent trees and statistics are derived when read.
- Virtual-key auth on the data plane. The admin plane and UI are loopback-only and have no sign-in: anyone who can reach that port sees every call.

Out of v1:

- Request and response bodies are never stored, by default or by option. No search over prompt or response text.
- No retry, fallback, request rewriting or model aliases.
- No budgets, rate limits, alerts, OTel export or admin sign-in.
- No partitioning, rollups or multi-replica tier.
- No other provider family.

Each deferred item and the trigger for adding it back is in [Later versions](docs/architecture/backend.md#later-versions). The decision is in [ADR 0002](docs/adrs/0002-v1-scope-and-metadata-only-storage-model.md).

## Development and contributing

- [CONTRIBUTING.md](CONTRIBUTING.md) has the package layout, the dependency rules and the checks (`make build`, `make test`, `make lint`, `make ui-check`, `make e2e`).
- `make ui-dev` runs the UI with hot reload against a local `llm-proxy serve`.
- [docs/](docs/) holds the design: start with [llm-proxy-design.md](docs/llm-proxy-design.md), the [architecture documents](docs/architecture/) and the [ADRs](docs/adrs/README.md). The brand proposals are in [branding-proposals.html](docs/branding-proposals.html).

## License

[MIT](LICENSE), copyright (c) 2026 Marco Tanzi.
