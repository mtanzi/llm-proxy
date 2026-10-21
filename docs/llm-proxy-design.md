# Self-Hosted LLM Proxy: Technical Design

Status: draft
Based on: "If you don't run your own LLM proxy, how the fuck do you even know whats going on?" (jrdsgl.com, 18 Oct 2026), plus an integration analysis for [firstmate](https://github.com/kunchenguid/firstmate).
Architecture: the backend, front end, upstream connection and deployment are detailed in [architecture/](architecture/README.md). Where they differ from this document, they are newer. In particular, request and response bodies are never stored (architecture decisions D1 and D2), so this document's body-related parts are removed. Storage sits behind two ports with a Postgres adapter in v1 (decision D14), and virtual keys live in the config file (decision D15); where this document says Postgres it means that adapter.
Scope: v1 is an observe-only logging proxy for the Anthropic family (architecture decisions C1 to C4). Where this document describes fallback, budgets, rate limits, alerts, OTel export, rollups, partitioning or the OpenAI family, those parts are later versions; see [architecture/backend.md, Later versions](architecture/backend.md#later-versions). The data model in section 4 is the v1 model.

> The article is an opinion piece with few specifics. Sections 1 and 2 separate what the article asks for from what this design adds. Everything beyond the article's list is extrapolation.

---

## 1. Goals and requirements

### 1.1 Requirements taken from the article

| # | Requirement |
|---|---|
| R1 | One endpoint you control that fronts multiple providers and models |
| R2 | Logs of which key called which model, from which client, at what time, and with what status |
| R3 | Virtual API keys, so the real provider key is never spread across work, agents and harnesses |
| R4 | Infrastructure you control (self-hosted) |
| R5 | Fast pivot of model or provider without reconfiguring every client |
| R6 | Engineer-grade usage data: token time series, not a vendor "proof of work" panel |
| R7 | Cache-miss diagnosis in seconds, with an API so it can be automated |
| R8 | Extensibility: custom MCP server endpoints and extra features |

### 1.2 Added by this design

In v1:

- Cost computation with versioned pricing (in the config file)
- No payload storage: prompts, responses, tool arguments and tool results are never stored. Retention controls apply to metadata only

Later versions, each with a trigger in [architecture/backend.md](architecture/backend.md#later-versions):

- Budgets and rate limits per virtual key
- Fallback, retry and circuit-breaker routing
- Alerts
- OTel export as one output among several
- The OpenAI family (v2)

### 1.3 Non-goals

- Being a general-purpose API gateway for non-LLM traffic
- Prompt management, evals or a playground UI
- Hosting models (local backends are just another provider)

---

## 2. Architecture

```
Clients (Claude Code, scripts, apps)
        │  virtual key + Anthropic-compatible request
        ▼
┌───────────────────────── Proxy ─────────────────────────┐
│ 1. Edge: TLS in front, Anthropic Messages endpoints     │
│ 2. Auth: virtual key → identity (key map from config)   │
│ 3. Router: alias → provider/model (static config)       │
│ 4. ReverseProxy: stream bytes through unchanged         │
│ 5. Telemetry tap: wraps the stream, extracts facts      │
└───────┬───────────────────────────────┬─────────────────┘
        ▼                               ▼
  Provider API (Anthropic)       Recorder → queue → writer → store
                                         │
                                  Admin API · dashboard
        Later: MCP gateway (custom tool endpoints, same auth + logging)
```

### 2.1 Request lifecycle

1. Client sends a request with `Authorization: Bearer <virtual key>` to the proxy.
2. Auth hashes the presented key, looks the hash up in the in-memory key map built from the config file, and checks that the key exists and is not disabled.
3. The router resolves the requested model name through the alias table in the config file. A name that is not an alias passes through unchanged.
4. The proxy attaches the real provider credential and sends the request upstream. It splices the `model` value only when an alias changes it.
5. The response is streamed back without buffering. The telemetry tap observes chunks and captures time-to-first-token, final usage and status.
6. An upstream error (4xx, 5xx) reaches the client unchanged, and the client applies its own retry rules. Retry and fallback routing are a later version.
7. On completion, one event is pushed to the queue. The writer batches inserts. The request path never waits on storage.

---

## 3. Component design

### 3.1 Edge and protocol adapters

- Accept the Anthropic (messages) format, so clients change only a base URL. The OpenAI formats (chat completions, responses) are v2.
- Support SSE streaming end to end. Do not buffer streams.
- Return an `x-request-id` header on every response, matching `calls.id`.
- Identify the client from `User-Agent` or an optional `x-client` header.

### 3.2 Auth and virtual keys

- Keys live in the `keys` section of the config file as a name and a SHA-256 hash, never plaintext. Keys are 256-bit random values, so no pepper is needed. A key is generated outside the proxy (`openssl`, or `llm-proxy keygen --name <name>`), and the plaintext is shown once, by the generator. Add, revoke and rotate are config edits plus `SIGHUP`.
- Each key carries: a unique name (for example `claude-code-laptop`, `ci-agent`, or a task id) and a `disabled` flag. The call record stores the name. Allowed aliases, request-rate limits, spend budgets and expiry are later versions.
- Real provider keys live in a secrets manager or secret files that no API ever returns.
- Issue one key per harness, agent or task so logs answer "who called what".

### 3.3 Router

- Clients request a stable alias such as `coding-main`, or a vendor model name. The alias table lives in the config file and maps each alias to one `(provider, model, params)`; a name that is not an alias passes through to the default provider of the family.
- Pivoting providers or models is a config change and a `SIGHUP`, with no client changes (R5).
- Routing is deterministic and logged. Each request records the alias requested and the target that served it.
- Later versions add ordered fallback on 5xx, 429 and timeouts, bounded retries with jitter, per-provider circuit breakers and weighted or cost-based selection. Their design and trigger are in [architecture/backend.md](architecture/backend.md#gateway-features-decision-c1).

### 3.4 Provider adapters

v1 has one pure per-family package, `family/anthropic`, that reads request and response formats and does not translate between them. A second family is a v2 package.

- Normalize usage into one schema: `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`. The cache fields matter most for R7.
- Map vendor errors into a small controlled `error_class` vocabulary: `rate_limited`, `upstream_5xx`, `timeout`, `auth`, `client_abort`, `invalid_request`, `internal`.

### 3.5 Telemetry tap

- Wraps the response stream and captures TTFT, total latency, usage from the final chunk and the status.
- Reads the stream in memory, extracts facts and discards the bytes. Emits one call record to a bounded in-memory queue. If the queue is full, drop the record and count it; there is no spill file.
- Logging must never add latency to, or fail, a request.

### 3.6 MCP gateway (R8)

- A separate component beside the proxy that exposes custom tool endpoints.
- Uses the same virtual-key auth and writes to the same audit trail, so tool calls appear alongside model calls (`endpoint = 'mcp'`).

---

## 4. Data model

Principle: **write facts, derive views.** Every table is written by insert only. Tool-call pairing, sessions, agent trees and statistics are computed when read. This section is the v1 model; the full column list and the notes are in [architecture/backend.md, Data model](architecture/backend.md#data-model).

### 4.1 Entity overview

```
calls ──< tool_events      (tool use and result halves, paired at read)
  └─────< call_segments    (cache diagnosis, one row per call)
```

Virtual keys, aliases, providers and pricing are not tables. They live in the config file and are reloaded on `SIGHUP`. `calls.key_name` holds the key's name from that file.

### 4.2 Tables

Three plain tables, not partitioned:

- `calls`: one row per client request, append-only, `PRIMARY KEY (id)` with `id` a UUIDv7 that is also returned as `x-request-id`. It holds who called (key name, client, session, agent, prompt), what ran (alias, provider, model, `upstream_request_id`), the outcome (status, `error_class`, provider `error_type`, stop reason, `streamed`, TTFT, duration), disjoint usage columns (`input_uncached`, `cache_read`, `cache_write_5m`, `cache_write_1h`, `output`, `reasoning`) plus `usage_raw jsonb`, and `cost_usd` with `pricing_version`. Indexes: `(ts DESC, id DESC)`, `(session_key, ts)`, `(key_name, ts DESC)`, and a partial index on `error_class`.
- `tool_events`: one row per tool-call half (`kind = use` or `result`), with the provider tool-call id, tool name, byte size and `is_error`. The text is never kept.
- `call_segments`: one row per call holding the ordered segment hashes (tools, system, each message boundary) as one `bytea`, plus token estimates. Short retention.

### 4.3 Design decisions

- **Requested vs actual routing are stored separately.** `alias` is what the client asked for. `provider` and `model` are what served it. There is one attempt per call in v1, so `request_attempts` is a later version with fallback.
- **Cost is snapshotted, not recomputed.** `cost_usd` plus `pricing_version` keeps historical numbers stable when prices change.
- **Usage is normalized at write time**, so queries never branch on provider. `usage_raw` keeps the original so a normalization fix can be applied to old rows.
- **The hot path is write-only.** Nothing in the request path reads these tables. Auth and routing read memory.
- **`error_class` is a small controlled vocabulary.** Provider error message text is not stored.
- **Sessions, agents and tool calls are derived.** A session is a group of `calls` by `session_key`. A tool call is a `use` row joined to a `result` row on `tool_call_id`.
- There is no table for request or response bodies. Bodies are never stored, so nothing sensitive needs separate retention.

### 4.4 Scale

At the expected volume of 12,000 to 50,000 calls per day (about 20 million rows over 400 days at the upper end), plain Postgres (the `store/postgres` adapter) with a `(ts DESC, id DESC)` index is enough. Retention is a daily batched `DELETE`. Rollup tables, daily partitioning and a ClickHouse move are later versions with triggers in [architecture/backend.md](architecture/backend.md#storage-and-scale): add rollups when usage p95 passes 1 s or rows pass about 20 million, and partitioning past about 100 million rows.

---

## 5. Cache-miss diagnosis (R7)

Prompt caches are prefix-based, so a miss means the prefix changed or expired.

### 5.1 Capture

- For each request, hash cumulative prompt segments in order: tools, system prompt, then each message boundary. Store the hash list in `call_segments`, one row per call, never the content. The first index where the cumulative hashes differ is the first index where the segment hashes differ, so one hash per segment is enough.
- The time gap since the previous request is computed at read time from the call timestamps.

### 5.2 Analysis

Join each request to the previous one **of the same agent** (same `session_key`, `agent_id` and `request_class`) and find the first segment index where the hashes differ. The segment kind tells you what changed. Pairing by session alone is wrong: Claude Code runs sub-agents inside one session, in parallel, with their own system prompts and histories, and compaction and auxiliary requests have their own prefixes, so consecutive calls of a session alternate between different prefixes. The comparison is a pure function in the domain, unit-tested without a database.

| Observation | Likely cause |
|---|---|
| First difference at `tools` | Tool definitions changed or were reordered |
| First difference at `system` | System prompt changed (often a timestamp or dynamic field) |
| First difference at a `message` | History was edited, truncated or compacted |
| No difference, large time gap | Cache TTL expired |
| No difference, different `model` | Model switched (or, in a later version, a fallback fired) |
| No difference, short gap, same model | Provider-side eviction or a cache boundary placement issue |

### 5.3 API

```
GET /admin/sessions/{key}/cache-analysis
→ {
    "miss": true,
    "cause": "system_prompt_changed",
    "first_diff_segment": 1,
    "gap_seconds": 42,
    "call_id": "…",
    "previous_call_id": "…"
  }
```

### 5.4 Alerting

A later version tracks a rolling cache hit ratio per key and per session and alerts when it drops below a threshold, since each miss costs money. This is the natural source for the firstmate alert adapter in section 8. v1 shows the hit ratio and the misses with their cause in the UI, without alerts.

### 5.5 Retention

Retain `call_segments` for recent days only (default 14). The token columns on `calls` stay for the full retention period, so the cache hit ratio can always be computed from them.

---

## 6. Admin API and UI

### 6.1 REST surface (sketch)

| Method and path | Purpose |
|---|---|
| `GET /admin/config` | Read-only view of providers, aliases, pricing and key names from the config file |
| `GET /admin/usage?group_by=key,model,client&from=&to=&grain=` | Time-series usage, computed from raw rows |
| `GET /admin/requests/{id}` | Single call trace |
| `GET /admin/sessions/{key}/cache-analysis` | Section 5.3 |

The full v1 endpoint list is in [architecture/backend.md](architecture/backend.md#admin-and-query-api). Key rotation is a config edit: add the new key, switch the client, disable the old key, `SIGHUP`. `GET /admin/alerts` and editing of aliases and pricing through the API are later versions.

The admin plane uses separate auth from the data plane.

### 6.2 Dashboard

Token time series, cost, latency percentiles, error rates, cache-hit ratio, top consumers. Fallback frequency arrives with fallback.

### 6.3 OTel export

A later version emits spans and metrics so the data also lands in existing observability stacks. This is one output, not the primary interface, and a second subscriber of the recorder. v1 exposes only the proxy's own Prometheus metrics.

---

## 7. Deployment and security

### 7.1 Deployment

- Home or small team: one container plus Postgres.
- Team or production (later): stateless proxy replicas behind a load balancer and a shared Postgres. Each replica has its own in-memory call queue. v1 builds one replica; the replica tier is described in [architecture/deployment.md](architecture/deployment.md#later-multi-replica-tier).
- Keep it on a private network or behind a tunnel (WireGuard, Tailscale). Put TLS and rate limiting on anything public.
- Back up the config file, which holds the key hashes. The event log is expendable by comparison.

### 7.2 Security

- Prompts and outputs are sensitive. The proxy never stores them: logging is metadata only, with no body capture option. Tool arguments, tool results and provider error messages are not stored either.
- Keep provider secrets in secret files or a secrets manager. Redact auth headers from all logs.
- A leaked virtual key can spend without limit until it is disabled in the config file and the proxy reloaded in v1. Mitigate with the provider console's own spend limit; per-key spend caps are a later version.
- Separate admin and data plane credentials.

### 7.3 Build or adopt

The article names no product and encourages modifying whichever you pick. Choose an open source project with an admin API and a pluggable logging layer, and check it against R1 to R8, especially cache-token fields and per-key attribution. If nothing fits, the core is small: an HTTP service, an adapter layer and an event writer.

---

## 8. Integration with firstmate

### 8.1 What firstmate is

Firstmate is an "agent distro": a repository of instructions, skills and bash helper scripts that turns a coding agent into a supervisor of parallel crewmate agents, each in its own git worktree and terminal session. It supports several harnesses (Claude Code, Codex, Pi, OpenCode, Cursor Agent CLI and others).

### 8.2 Why this should not be a plugin

Firstmate's extension mechanism is deliberately narrow. Per its own docs, the first binding is "one complete vertical capability, not a general plugin system". It supports a single capability, `process-event-adapter/1`, and explicitly excludes lifecycle hooks, worker-launch grants, instruction injection and delivery providers. New capabilities need a separately reviewed contract. A long-running HTTP service with a database does not fit that model, so the proxy runs as an independent service.

### 8.3 Where the two fit together

Firstmate runs many agents in parallel, possibly on different models and harnesses. That is the situation where per-key attribution pays off.

**(a) Per-task virtual keys.** Issue one key per crewmate, named with the firstmate task id (one config entry and a `SIGHUP` each), so proxy logs answer which task spent what on which model.
- Open question: how firstmate injects environment into crewmates. Worker-launch grants are out of scope for the extension system, so check its dispatch-profile configuration or propose a core change.

**(b) A process-event adapter for alerts (later version; v1 has no alert feed).** This is the one thing the plugin boundary supports.
- Depends on the alerts feature, which is deferred ([architecture/backend.md](architecture/backend.md#gateway-features-decision-c1)); the design below stays valid for when it is added.
- Package: a small executable plus `firstmate-extension.json`, with capability `process-event-adapter` version 1 and required consent `network`.
- `source.poll` calls `GET /admin/alerts?since=<cursor>` on the proxy (once alerts exist) and returns `{status: "result", output: "<alert text>"}` or `{status: "no-result", output: ""}` before the host timeout.
- Candidate alerts: budget cap hit on a key, cache hit ratio drop, repeated fallback, error-rate spike, spend anomaly.
- Constraints from the extension protocol: JSON envelopes up to 64 KiB, raw result up to 32 KiB, stderr up to 8 KiB, one UTF-8 JSON document in and out per invocation, no shell, minimal environment. Responses are untrusted evidence and can never authorize a merge, decision or destination.
- The adapter should be read-only against the proxy. A bound package is trusted same-user code, not sandboxed code.

Manifest sketch:

```json
{
  "schema": "firstmate.extension-manifest.v1",
  "id": "local.llm-proxy-alerts",
  "version": "0.1.0",
  "host_protocols": [1],
  "entrypoint": "bin/firstmate-extension",
  "capabilities": [
    {
      "name": "process-event-adapter",
      "versions": [1],
      "adapter_names": ["llm-proxy-alerts"]
    }
  ],
  "required_consents": ["network"]
}
```

**(c) Ledger join (no firstmate changes).** Firstmate has an opt-in, append-only `state/fleet-ledger.jsonl` with events `task.dispatched` (kind, project, harness, requested model), `task.status`, `task.pr_ready`, `task.merged` and `task.cleaned_up`. It records no tokens or cost. A standalone reporter joins the ledger with the proxy tables on task id (via the virtual key label) and time window to produce cost per task and cost per merged PR. Ledger readers should tolerate duplicate records, since delivery is at-least-once.

### 8.4 Caveats

- **Harness routing.** The proxy only sees traffic from harnesses that can be pointed at a custom base URL with an API key. Harnesses that authenticate through a vendor subscription login may bypass it. Verify each harness before committing. This is the largest risk.
- **Request-to-task mapping** is clean with per-task keys and guesswork without them.
- **Maintainer fit.** If contributing upstream rather than running locally, read firstmate's VISION.md and CONTRIBUTING.md first. The project favors flat, inspectable, deterministic mechanics with explicit consent.

---

## 9. Implementation plan

v1 phases (observe-only, Anthropic family). Items marked *later* are outside v1.

| Phase | Deliverable | Depends on |
|---|---|---|
| 1 | Proxy core: edge, auth from an in-memory key map, alias lookup, one provider, recorder and event writer, `calls` table | none |
| 2 | Cost and pricing from the config file, tool events, usage columns | 1 |
| 3 | Admin API, dashboard, usage from raw rows, `GET /admin/stream` live tail | 2 |
| 4 | Cache diagnosis: segments, session-derived views, analysis endpoint (paired by agent) | 2 |
| 5 | Route one firstmate harness (Claude Code) through the proxy and write the ledger-join reporter | 3 |
| 6 | *Later:* alerts and the firstmate alert adapter package | 4 |
| 7 | Per-task key injection (may require firstmate core changes) | 5 |
| 8 | *Later:* MCP gateway | 3 |
| Later | Fallback and circuit breaker with `request_attempts`; budgets, rate limits and key restrictions; OTel export; rollups, then partitioning; multi-replica tier; the OpenAI family (v2) | triggers in [architecture/backend.md](architecture/backend.md#later-versions) |

---

## 10. Open questions

1. Home lab or team use? This changes storage and high-availability choices.
2. Which providers and harnesses are required on day one? Resolved for v1: the Anthropic family and Claude Code. Codex CLI and OpenCode follow in v2.
3. Build or adopt? A comparison of candidate projects against R1 to R8 would settle this.
4. ~~Store prompt and response bodies, or metadata only?~~ Resolved: metadata only; bodies are never stored.
5. Which firstmate harnesses can be routed through a custom base URL, and how does firstmate pass environment to crewmates?

---

## Sources

- [If you don't run your own LLM proxy, how the fuck do you even know whats going on?](https://jrdsgl.com/if-you-dont-run-your-own-llm-proxy-how-the-fuck-do-you-even-know-whats-going-on/)
- [firstmate README](https://github.com/kunchenguid/firstmate)
- [firstmate extension bindings](https://github.com/kunchenguid/firstmate/blob/main/docs/extension-bindings.md)
- [firstmate fleet ledger](https://github.com/kunchenguid/firstmate/blob/main/docs/fleet-ledger.md)
