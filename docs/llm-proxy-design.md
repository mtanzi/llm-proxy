# Self-Hosted LLM Proxy: Technical Design

Status: draft
Based on: "If you don't run your own LLM proxy, how the fuck do you even know whats going on?" (jrdsgl.com, 18 Oct 2026), plus an integration analysis for [firstmate](https://github.com/kunchenguid/firstmate).

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

- Cost computation with versioned pricing
- Budgets and rate limits per virtual key
- Fallback, retry and circuit-breaker routing
- OTel export as one output among several
- Retention controls for sensitive payloads

### 1.3 Non-goals

- Being a general-purpose API gateway for non-LLM traffic
- Prompt management, evals or a playground UI
- Hosting models (local backends are just another provider)

---

## 2. Architecture

```
Clients (TUI, IDE agents, scripts, apps)
        │  virtual key + OpenAI- or Anthropic-compatible request
        ▼
┌───────────────────────── Proxy ─────────────────────────┐
│ 1. Edge: TLS, protocol adapters (OpenAI / Anthropic)    │
│ 2. Auth: virtual key → identity, limits, allowed aliases│
│ 3. Router: alias → provider/model, fallback, retries    │
│ 4. Provider adapters: translate, stream, normalize usage│
│ 5. Telemetry tap: wraps the stream, emits events        │
└───────┬───────────────────────────────┬─────────────────┘
        ▼                               ▼
  Providers (Anthropic, OpenAI,    Event queue → writer → Postgres
  local vLLM/Ollama, ...)                │
                                  Admin API · dashboard · OTel export
        Optional: MCP gateway (custom tool endpoints, same auth + logging)
```

### 2.1 Request lifecycle

1. Client sends a request with `Authorization: Bearer <virtual key>` to the proxy.
2. Auth resolves the key hash to a `virtual_keys` row and checks expiry, revocation, rate limit, budget and allowed aliases.
3. The router resolves the requested alias to an ordered route list.
4. The provider adapter translates the request, attaches the real provider credential and sends it upstream.
5. The response is streamed back without buffering. The telemetry tap observes chunks and captures time-to-first-token, final usage and status.
6. On a retryable failure (5xx, 429, timeout), the router moves to the next route and records an attempt row.
7. On completion, one event is pushed to the queue. The writer batches inserts. The request path never waits on storage.

---

## 3. Component design

### 3.1 Edge and protocol adapters

- Accept both the OpenAI (chat completions / responses) and Anthropic (messages) formats, so clients change only a base URL.
- Support SSE streaming end to end. Do not buffer streams.
- Return an `x-request-id` header on every response, matching `requests.id`.
- Identify the client from `User-Agent` or an optional `x-client` header.

### 3.2 Auth and virtual keys

- Store only a hash of each key plus a short display prefix. The plaintext is shown once at creation.
- Each key carries: owner, purpose label (for example `claude-code-laptop`, `ci-agent`, or a task id), allowed alias list, requests-per-minute limit, spend budget and period, expiry and revocation time.
- Real provider keys live in a secrets manager or an encrypted table that no API ever returns.
- Issue one key per harness, agent or task so logs answer "who called what".
- Budget enforcement reads a fast counter (Redis, or an in-process cache backed by the rollup table), never raw rows.

### 3.3 Router

- Clients request a stable alias such as `coding-main`. An `alias_routes` table maps each alias to an ordered list of `(provider, model, params)`.
- Pivoting providers is a config change with no client changes (R5).
- Policies: ordered fallback on 5xx, 429 and timeouts; bounded retries with jitter; per-provider circuit breakers; optional weighted or cost-based selection.
- Routing is deterministic and logged. Each request records the alias requested, the target that served it and the reason for any fallback.

### 3.4 Provider adapters

- Translate request and response formats per vendor.
- Normalize usage into one schema: `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_write_tokens`, `reasoning_tokens`. The cache fields matter most for R7.
- Map vendor errors into a small controlled `error_class` vocabulary: `rate_limited`, `upstream_5xx`, `timeout`, `auth`, `client_abort`, `invalid_request`, `internal`.

### 3.5 Telemetry tap

- Wraps the response stream and captures TTFT, total latency, usage from the final chunk and the status.
- Emits an event to an async queue. If the queue is unavailable, spill to a local append-only file and replay later.
- Logging must never add latency to, or fail, a request.

### 3.6 MCP gateway (R8)

- A separate component beside the proxy that exposes custom tool endpoints.
- Uses the same virtual-key auth and writes to the same audit trail, so tool calls appear alongside model calls (`endpoint = 'mcp'`).

---

## 4. Data model

Principle: **append-only facts, mutable config, derived rollups.** Config tables change. The request log never does. Dashboards read rollups.

### 4.1 Entity overview

```
virtual_keys ──< requests >── request_attempts
      │              │
      │              ├──< request_segments   (cache diagnosis)
      │              └──1 request_bodies     (optional, short retention)
      │
sessions ────────────┘
aliases ──< alias_routes        pricing (versioned)      usage_rollups (derived)
```

### 4.2 Config tables (mutable)

```sql
CREATE TABLE virtual_keys (
  id              uuid PRIMARY KEY,
  key_hash        bytea NOT NULL UNIQUE,      -- hash only; plaintext shown once
  key_prefix      text NOT NULL,              -- first ~8 chars, for UI identification
  label           text NOT NULL,              -- 'claude-code-laptop', 'ci-agent'
  owner           text,
  allowed_aliases text[],                     -- null = all
  rpm_limit       int,
  budget_usd      numeric(12,4),              -- per budget_period
  budget_period   text,                       -- 'day' | 'month'
  log_bodies      boolean NOT NULL DEFAULT false,
  expires_at      timestamptz,
  revoked_at      timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE aliases (
  id    uuid PRIMARY KEY,
  name  text NOT NULL UNIQUE                  -- 'coding-main'
);

CREATE TABLE alias_routes (
  alias_id  uuid REFERENCES aliases(id),
  position  int NOT NULL,                     -- 0 = primary, 1+ = fallbacks
  provider  text NOT NULL,
  model     text NOT NULL,
  params    jsonb NOT NULL DEFAULT '{}',
  enabled   boolean NOT NULL DEFAULT true,
  PRIMARY KEY (alias_id, position)
);

CREATE TABLE pricing (
  provider              text NOT NULL,
  model                 text NOT NULL,
  effective_from        date NOT NULL,
  input_per_mtok        numeric(12,6),
  output_per_mtok       numeric(12,6),
  cache_read_per_mtok   numeric(12,6),
  cache_write_per_mtok  numeric(12,6),
  PRIMARY KEY (provider, model, effective_from)
);
```

### 4.3 Fact tables (append-only)

`requests`: one row per client request. The table most queries hit.

```sql
CREATE TABLE requests (
  id               uuid PRIMARY KEY,           -- also returned as x-request-id
  ts               timestamptz NOT NULL,       -- partition key
  virtual_key_id   uuid NOT NULL,
  session_id       uuid,
  client           text,
  endpoint         text NOT NULL,              -- 'messages','chat.completions','mcp'

  -- routing: requested vs actual
  alias            text NOT NULL,
  final_provider   text,
  final_model      text,
  attempts         smallint NOT NULL DEFAULT 1,

  -- outcome
  status_code      smallint,
  error_class      text,
  streamed         boolean NOT NULL,
  ttft_ms          int,
  latency_ms       int,

  -- normalized usage
  input_tokens         int,
  output_tokens        int,
  cache_read_tokens    int,
  cache_write_tokens   int,
  reasoning_tokens     int,
  cost_usd             numeric(12,6),
  pricing_version      date,                   -- which pricing row was used

  -- cache diagnosis summary
  prompt_bytes         int,
  segment_count        smallint,
  cache_hit_ratio      real                    -- cache_read / (input + cache_read)
) PARTITION BY RANGE (ts);

CREATE INDEX ON requests (virtual_key_id, ts DESC);
CREATE INDEX ON requests (session_id, ts);
CREATE INDEX ON requests (final_model, ts DESC);
```

Partition by day or week so retention is a cheap `DROP PARTITION`.

`request_attempts`: one row per upstream try. Makes fallback behavior auditable.

```sql
CREATE TABLE request_attempts (
  request_id      uuid NOT NULL,
  attempt_no      smallint NOT NULL,
  provider        text NOT NULL,
  model           text NOT NULL,
  status_code     smallint,
  error_class     text,
  latency_ms      int,
  fallback_reason text,        -- 'circuit_open','429','timeout'; null on first try
  PRIMARY KEY (request_id, attempt_no)
);
```

`sessions`: groups consecutive requests from one conversation or coding session. Cache analysis compares request N to N-1 within a session. Clients rarely send a session id, so derive one: use an explicit header if present, otherwise a hash of (key, client, first-segment hash) with an idle timeout.

```sql
CREATE TABLE sessions (
  id              uuid PRIMARY KEY,
  virtual_key_id  uuid NOT NULL,
  client          text,
  started_at      timestamptz NOT NULL,
  last_seen_at    timestamptz NOT NULL,
  request_count   int NOT NULL DEFAULT 0
);
```

`request_segments`: the heart of cache-miss diagnosis. Stores hashes of cumulative prompt boundaries, never content.

```sql
CREATE TABLE request_segments (
  request_id     uuid NOT NULL,
  seg_idx        smallint NOT NULL,   -- 0 = tools, 1 = system, 2+ = messages in order
  kind           text NOT NULL,       -- 'tools' | 'system' | 'message'
  role           text,
  segment_hash   bytea NOT NULL,      -- hash of this segment alone
  prefix_hash    bytea NOT NULL,      -- hash of everything up to and including it
  token_estimate int,
  PRIMARY KEY (request_id, seg_idx)
);
```

`request_bodies`: optional, off by default, separate retention.

```sql
CREATE TABLE request_bodies (
  request_id    uuid PRIMARY KEY,
  ts            timestamptz NOT NULL,
  request_json  jsonb,     -- redacted
  response_json jsonb,
  expires_at    timestamptz NOT NULL
);
```

Keeping bodies in their own table means the sensitive data can be dropped without touching analytics.

### 4.4 Derived rollups

```sql
CREATE TABLE usage_rollups (
  bucket             timestamptz NOT NULL,   -- hour or day
  grain              text NOT NULL,          -- 'hour' | 'day'
  virtual_key_id     uuid NOT NULL,
  client             text,
  final_model        text,
  requests           int,
  errors             int,
  input_tokens       bigint,
  output_tokens      bigint,
  cache_read_tokens  bigint,
  cache_write_tokens bigint,
  cost_usd           numeric(14,6),
  p50_latency_ms     int,
  p95_latency_ms     int,
  PRIMARY KEY (bucket, grain, virtual_key_id, client, final_model)
);
```

A background job fills this incrementally. On TimescaleDB, continuous aggregates do the same natively.

### 4.5 Design decisions

- **Requested vs actual routing are stored separately.** `alias` is what the client asked for. `final_provider/model` is what served it. The attempts table explains the gap.
- **Cost is snapshotted, not recomputed.** `cost_usd` plus `pricing_version` keeps historical numbers stable when prices change.
- **Usage is normalized at write time**, so queries never branch on provider.
- **The hot path is write-only.** Nothing in the request path reads these tables, except the budget counter.
- **`error_class` is a small controlled vocabulary.** Free-text errors go in attempt rows or logs.

### 4.6 Scale

Plain Postgres with partitioning comfortably handles a home lab or small team (millions of rows per month). Beyond hundreds of millions of events, move `requests`, `request_attempts` and `request_segments` to ClickHouse and keep config tables in Postgres. The schema transfers because it is already append-only.

---

## 5. Cache-miss diagnosis (R7)

Prompt caches are prefix-based, so a miss means the prefix changed or expired.

### 5.1 Capture

- For each request, hash cumulative prompt segments in order: tools, system prompt, then each message boundary. Store the hash list in `request_segments`, never the content.
- Record the time gap since the previous request in the same session.

### 5.2 Analysis

Join each request to the previous one in its session and find the first `seg_idx` where `prefix_hash` differs. `kind` tells you what changed.

| Observation | Likely cause |
|---|---|
| First difference at `tools` | Tool definitions changed or were reordered |
| First difference at `system` | System prompt changed (often a timestamp or dynamic field) |
| First difference at a `message` | History was edited, truncated or compacted |
| No difference, large time gap | Cache TTL expired |
| No difference, different `final_model` | Model switched or fallback fired |
| No difference, short gap, same model | Provider-side eviction or a cache boundary placement issue |

### 5.3 API

```
GET /sessions/{id}/cache-analysis
→ {
    "miss": true,
    "cause": "system_prompt_changed",
    "first_diff_segment": 1,
    "gap_seconds": 42,
    "request_id": "…",
    "previous_request_id": "…"
  }
```

### 5.4 Alerting

Track a rolling cache hit ratio per key and per session, and alert when it drops below a threshold, since each miss costs money. This is also the natural source for the firstmate alert adapter in section 8.

### 5.5 Retention

Retain `request_segments` for recent days only. Keep the summary columns (`cache_hit_ratio`, `segment_count`) on `requests` indefinitely.

---

## 6. Admin API and UI

### 6.1 REST surface (sketch)

| Method and path | Purpose |
|---|---|
| `POST /admin/keys` | Create a virtual key (returns plaintext once) |
| `DELETE /admin/keys/{id}` | Revoke |
| `POST /admin/keys/{id}/rotate` | Rotate |
| `GET/PUT /admin/aliases/{name}` | Read or edit alias routes |
| `GET /admin/usage?group_by=key,model,client&from=&to=&grain=` | Time-series usage |
| `GET /admin/requests/{id}` | Single request trace including attempts |
| `GET /sessions/{id}/cache-analysis` | Section 5.3 |
| `GET /admin/alerts?since=` | Alert feed (used by the firstmate adapter) |

The admin plane uses separate auth from the data plane.

### 6.2 Dashboard

Token time series, cost, latency percentiles, error rates, cache-hit ratio, top consumers, fallback frequency.

### 6.3 OTel export

Emit spans and metrics so the data also lands in existing observability stacks. This is one output, not the primary interface.

---

## 7. Deployment and security

### 7.1 Deployment

- Home or small team: one container plus Postgres.
- Team or production: stateless proxy replicas behind a load balancer, shared Postgres and a shared event queue.
- Keep it on a private network or behind a tunnel (WireGuard, Tailscale). Put TLS and rate limiting on anything public.
- Back up `virtual_keys`, `aliases`, `alias_routes` and `pricing`. The event log is expendable by comparison.

### 7.2 Security

- Prompts and outputs are sensitive. Default to metadata-only logging, make body capture opt-in per key, and enforce retention.
- Encrypt provider secrets at rest. Redact auth headers from all logs.
- Per-key spend caps bound the damage from a leaked virtual key.
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

**(a) Per-task virtual keys.** Issue one key per crewmate, labeled with the firstmate task id, so proxy logs answer which task spent what on which model.
- Open question: how firstmate injects environment into crewmates. Worker-launch grants are out of scope for the extension system, so check its dispatch-profile configuration or propose a core change.

**(b) A process-event adapter for alerts.** This is the one thing the plugin boundary supports.
- Package: a small executable plus `firstmate-extension.json`, with capability `process-event-adapter` version 1 and required consent `network`.
- `source.poll` calls `GET /admin/alerts?since=<cursor>` on the proxy and returns `{status: "result", output: "<alert text>"}` or `{status: "no-result", output: ""}` before the host timeout.
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

| Phase | Deliverable | Depends on |
|---|---|---|
| 1 | Proxy core: edge, auth, router, one provider adapter, event writer, `requests` table | none |
| 2 | Second provider, fallback and circuit breaker, `request_attempts`, pricing and cost | 1 |
| 3 | Rollups, admin API, dashboard, OTel export | 2 |
| 4 | Cache diagnosis: segments, sessions, analysis endpoint, hit-ratio alerts | 2 |
| 5 | Route one firstmate harness through the proxy and write the ledger-join reporter | 3 |
| 6 | Firstmate alert adapter package | 4 |
| 7 | Per-task key injection (may require firstmate core changes) | 5 |
| 8 | MCP gateway | 3 |

---

## 10. Open questions

1. Home lab or team use? This changes storage and high-availability choices.
2. Which providers and harnesses are required on day one?
3. Build or adopt? A comparison of candidate projects against R1 to R8 would settle this.
4. Store prompt and response bodies, or metadata only?
5. Which firstmate harnesses can be routed through a custom base URL, and how does firstmate pass environment to crewmates?

---

## Sources

- [If you don't run your own LLM proxy, how the fuck do you even know whats going on?](https://jrdsgl.com/if-you-dont-run-your-own-llm-proxy-how-the-fuck-do-you-even-know-whats-going-on/)
- [firstmate README](https://github.com/kunchenguid/firstmate)
- [firstmate extension bindings](https://github.com/kunchenguid/firstmate/blob/main/docs/extension-bindings.md)
- [firstmate fleet ledger](https://github.com/kunchenguid/firstmate/blob/main/docs/fleet-ledger.md)
