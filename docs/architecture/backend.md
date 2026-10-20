# Backend architecture (Go)

Part of the [architecture set](README.md). Upstream credentials and Claude subscription routing are in [upstream-connection.md](upstream-connection.md). The UI that consumes the admin API is in [frontend.md](frontend.md). How the binary is deployed is in [deployment.md](deployment.md).

## Shape

One Go module, one static binary, `llm-proxy`, with subcommands:

| Subcommand | Purpose |
|---|---|
| `serve` | Runs the data plane, the admin plane, the journal writer and, by default, the projector and the background jobs |
| `migrate up` / `migrate status` | Applies the embedded Postgres migrations |
| `project` | Runs only the projector, for a deployment that separates it from `serve` |
| `reproject --from <segment>` | Re-runs the projector over old journal segments after a normalization rule changes |
| `keys create` / `keys revoke` | Bootstrap virtual keys and admin tokens from the command line, before the UI exists |

The binary has two listeners, so the data plane and the admin plane can be exposed and authenticated separately (design doc 6.1, gap review R8):

| Listener | Default address | Serves |
|---|---|---|
| Data plane | `:8080` | `/v1/*` provider-compatible endpoints, `HEAD /api/hello`, `/healthz`, `/readyz` |
| Admin plane | `:9090` | `/admin/*` JSON API and SSE, `/ui/*` embedded React SPA, `/metrics`, `/healthz`, `/readyz` |

## Component diagram

```mermaid
flowchart TB
    subgraph clients[Clients]
        cc[Claude Code]
        oc[OpenAI-compatible clients]
        br[Browser: React UI]
    end

    subgraph bin[llm-proxy serve]
        subgraph dp[Data plane listener]
            mux[Listener and router]
            auth[Auth: virtual keys]
            corr[Correlation]
            route[Route resolver]
            rp[ReverseProxy with<br/>retrying RoundTripper]
            tap[Tap and idle watchdog]
        end
        post[Post-stream summarizer]
        q[[Bounded exchange queue]]
        jw[Journal writer]
        live[Live broadcaster]
        budget[Budget counters]
        cache[Key and config cache]

        subgraph bg[Background workers]
            proj[Projector]
            join[Tool-call joiner]
            roll[Rollup job]
            part[Partition and retention job]
            alerts[Alert evaluator]
            otel[OTel exporter]
        end

        subgraph ap[Admin plane listener]
            api[Admin and query API]
            sse[SSE stream]
            ui[Embedded UI assets]
            metrics[metrics endpoint]
        end
    end

    up[Provider APIs]
    jv[(Journal volume)]
    pg[(Postgres)]
    col[OTLP collector]

    cc --> mux
    oc --> mux
    mux --> auth --> corr --> route --> rp
    auth -.reads.-> cache
    auth -.pre-check.-> budget
    rp <--> up
    rp --- tap
    tap -->|on Close| post
    post --> budget
    post --> live
    post -->|non-blocking send| q
    q --> jw --> jv
    jv --> proj
    proj --> join
    proj --> pg
    join --> pg
    roll --> pg
    part --> pg
    part --> jv
    alerts --> pg
    proj --> otel --> col
    pg -.LISTEN/NOTIFY.-> cache
    br --> ui
    br --> api
    br --> sse
    api --> pg
    api -->|bodies by pointer| jv
    live --> sse
```

Two properties hold for every arrow out of the data plane:

- **The hot path never waits on storage.** Nothing between the client and the upstream reads or writes Postgres. Auth and routing read an in-memory cache. Budget checks read an in-process counter.
- **Logging never fails a request.** A full queue drops the exchange and increments a counter. A failed journal write is logged and counted. A Postgres outage only delays the projector.

## Components

### Listener and router

- `net/http` server with the standard `ServeMux` and method patterns (Go 1.22 and later). No web framework (gap review B10, approaches report section 8).
- Server timeouts: set `ReadHeaderTimeout` and `IdleTimeout`. **Do not set a global `WriteTimeout`**: it cuts long SSE streams. The approaches report measured this (section 6, finding 4). Each streaming request clears its own write deadline with `http.NewResponseController(w).SetWriteDeadline(time.Time{})`.
- Request size limit: 32 MiB per request body (gap review R15), enforced with `http.MaxBytesReader`.
- Every response carries `x-request-id`, a UUIDv7 that is also the primary id of the call. A client-supplied `x-request-id` is kept as `client_request_id`, never reused as the id (gap review R17).
- Middleware order: recover, request id, access log (`log/slog`, with header redaction in one place), auth, correlation, route.
- Graceful shutdown on SIGTERM: stop accepting connections, wait for in-flight streams up to a drain deadline (default 10 minutes, gap review R14), close the queue, flush and seal the journal segment. The projector resumes from its checkpoint on the next start.

### Auth (virtual keys)

- **Virtual keys** look like `lp_<base62 of 32 random bytes>`. Postgres stores only an HMAC-SHA256 of the key with a server pepper, plus a display prefix of `lp_` and 4 characters (gap review R1). The plaintext is shown once at creation.
- **Where the key is read from.** In the default `proxy_key` mode the client sends its virtual key as its API credential: `Authorization: Bearer lp_…` or `x-api-key: lp_…`. In `client_passthrough` mode the client's own credential occupies `Authorization`, so the virtual key travels in a separate `x-llm-proxy-key` header. [upstream-connection.md](upstream-connection.md#credential-modes) explains both modes. The proxy consumes `x-llm-proxy-key` and never forwards it.
- **Lookup** is an in-memory map from HMAC to key record, loaded at start and refreshed by Postgres `LISTEN/NOTIFY` on key changes, with a 30-second TTL backstop. Revocation therefore reaches every replica within 30 seconds at worst.
- **Checks**, in order: key exists, not expired, not revoked, alias or model allowed, requests-per-minute and concurrency limits, budget pre-check. A failure returns a protocol-correct error body for the client's family (Anthropic or OpenAI shape) with a stable `type` per error class (gap review R3), and `retry-after` in integer seconds on 429.
- **Budget enforcement is soft** (gap review B7). The post-stream summarizer adds each call's cost to an in-process atomic counter per key. The pre-check reads that counter, so overshoot is bounded by the calls in flight. In the multi-replica tier the counter becomes a Postgres counter row; see [deployment.md](deployment.md#shared-state-in-the-multi-replica-tier).
- **Admin plane auth** is separate: admin tokens with scope `admin` or `read`, stored hashed like virtual keys (gap review R8). The browser exchanges a token for a session cookie; see [frontend.md](frontend.md#authentication-to-the-admin-api).

### Correlation

The correlation step reads harness headers into the exchange record. It never parses the body.

| Field | Source, in priority order |
|---|---|
| `session_key` | `x-claude-code-session-id`; then Codex and OpenCode session headers (`session-id`, `x-session-affinity`, `x-session-id`, `thread-id`, `conversation_id`); then a generic `x-session-id`; otherwise empty, and the projector derives one |
| `agent_id`, `parent_agent_id` | `x-claude-code-agent-id`, `x-claude-code-parent-agent-id` |
| `agent_type` | `x-claude-code-agent-type` (hint header) |
| `prompt_id` | `x-claude-code-prompt-id` (hint header) |
| `request_class` | `x-claude-code-request-class`: `main`, `subagent`, `workflow`, `compaction`, `auxiliary` (hint header) |
| `compaction`, `context_compacted` | `x-claude-code-compaction`, `x-claude-code-context-compacted` (hint headers) |
| `prev_tool_durations` | `x-claude-code-prev-tool-durations`, raw value; parsed by the projector (hint header) |
| `trace_id` | W3C `traceparent` |
| `client`, `client_raw` | `User-Agent` normalized to its product token (`claude-cli/2.1.290` becomes `claude-cli`), raw value kept (gap review R17) |

- The Claude Code header names and semantics come from Anthropic's [gateway compatibility guide](https://code.claude.com/docs/en/llm-gateway-protocol). The session and agent IDs are always sent. The rows marked "hint header" arrive behind a custom base URL only when the client sets `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`. **Pending decision D5** in the [README](README.md#open-decisions): set it for firstmate crewmates (recommended).
- The Codex and OpenCode header names are known only from Bifrost's source, not from vendor docs (approaches report 2.3). Each needs a capture test before it is relied on. **Pending decision D4.**
- Correlation headers are forwarded upstream unchanged, so the proxy stays transparent.
- Session derivation, when no session header is present, happens in the projector, not on the hot path: hash of (key, client, tools and system segment hash, first user message), with a 30-minute idle timeout (gap review R5).

### Route resolver

- The client's `model` value is looked up as an alias. An alias name may equal a vendor model id, so `claude-opus-4-8` can be an alias (gap review B3). Unmapped names are rejected with a protocol-correct 404, unless the key has an opt-in passthrough provider.
- An alias maps to an ordered list of routes `(provider, model, params)`. Every route of an alias must be in the client's protocol family (**pending decision D3**: same-family only in v1).
- The route list is snapshotted per request, so an alias edit never changes a request in flight (gap review R2).
- When the route's model differs from the client's value, the proxy splices the new value into the body bytes with `tidwall/sjson` `SetBytes`. It never decodes and re-encodes the body. Re-encoding would break prompt caching, the attribution-block strip and the preserved-thinking checks that Anthropic's gateway guide describes.
- In `client_passthrough` mode the resolver allows only routes to the provider the client's credential belongs to. Alias rewriting and fallback are off there; see [upstream-connection.md](upstream-connection.md#what-the-proxy-must-and-must-not-do-in-passthrough-mode).

### Proxy passthrough and streaming

Built on `net/http/httputil.ReverseProxy`. The approaches report verified the relevant behavior with an experiment (section 6): immediate flushing of `text/event-stream`, cancellation propagation, body closing on client abort, and no measurable added latency.

- **`Rewrite`** sets the upstream URL from the route, applies the credential mode (replace the client credential with the provider key, or pass it through), removes `x-llm-proxy-key`, and splices `model` when needed. All other headers and body bytes are forwarded unchanged. `anthropic-version`, `anthropic-beta` and every other `anthropic-*` header are forwarded as open lists, as Anthropic's guide requires.
- **`Transport`** is a retrying `http.RoundTripper`. Before the commit point it may retry the same route or move to the next one on 429, 5xx, a connect timeout or a response-header timeout. Every try is recorded as an attempt with its upstream request id.
- **Commit point:** the first upstream response with a 2xx status, before any byte is written to the client (gap review B4, approaches report 7.2). After commit there is no retry. A mid-stream upstream error passes through to the client as it arrives.
- When every route fails, the client receives the last upstream status and body unmodified, plus `x-request-id` (gap review R3). Claude Code's retry logic matches on the upstream's error wording, so error bodies are never wrapped.
- **Response headers** pass through unchanged, in particular `content-type`, `retry-after`, `x-should-retry` and `anthropic-ratelimit-unified-*`. The upstream's own request id header is stored as `upstream_request_id`.
- **OpenAI-family streams:** the proxy injects `stream_options.include_usage=true` when the client did not set it, and strips the extra usage chunk from the client stream in that case (gap review B6, LiteLLM precedent). This is the only hot-path body edit, and only for OpenAI-family requests.
- **Upstream timeouts:** a `net.Dialer` timeout, `Transport.ResponseHeaderTimeout` for time to headers, and an idle-between-chunks watchdog in the tap. SSE `ping` events are never stripped: during long thinking pauses they are the only bytes on the stream.
- **Client abort:** the upstream request uses the client's context, so an abort cancels upstream (gap review R4). The exchange is still logged, with partial bytes, `error_class = client_abort` and `usage_estimated = true`.
- **Upstream transport:** HTTP/2 to providers by default, keep-alives on, connection pool sized per provider.

### Tap

- `ModifyResponse` wraps the upstream response body in a tap. `Read` copies bytes into a per-exchange buffer and resets the idle watchdog. It never parses.
- The request body is captured the same way, through a tee on the outgoing body.
- Each exchange buffer is bounded (default 32 MiB per direction). Past the bound the tap stops copying, keeps forwarding, and sets `truncated = true` on the record.
- Buffers come from a `sync.Pool` and return to it after the journal writer is done with them.
- `Close` is the "exchange finished" hook. `ReverseProxy` closes the upstream body on success, on upstream error and on client abort, so the hook is reliable.
- The tap measured 54 µs in total for a whole 5-event stream in the approaches report's experiment.

### Post-stream summarizer

Runs in its own goroutine after `Close`, off the hot path.

1. Parses only what the live feed and the budget need from the captured bytes: final usage, stop reason, tool names requested, status.
2. Adds the cost to the key's budget counter.
3. Publishes `call.finished` to the live broadcaster.
4. Sends the exchange to the bounded queue with a non-blocking `select`. When the queue is full it drops the exchange and increments `llm_proxy_exchanges_dropped_total`.

Full parsing happens later, in the projector.

### Capture journal

The journal is the "complete log". **Pending decision D2**: journal-first storage (recommended). **Pending decision D1**: bodies captured by default, with per-key opt-out (recommended). With D1 option (b), the journal record for a key without body capture holds headers, timings and the parsed usage only.

- **Writer:** one goroutine owns the open segment and receives from the queue.
- **Segment files:** `journal/<journal_id>/<UUIDv7>.seg`, rotated by size (default 256 MiB) or age (default 1 hour), whichever comes first. `journal_id` is stable per replica.
- **Record framing:** each record is its own zstd frame, preceded by a fixed header `(magic, length, crc32)`. Self-contained frames give random access by `(segment, offset, length)`, which the call-detail view needs (UI report gap G5).
- **Record content:** `{id, ts, key_id, client, client_raw, correlation fields, route, attempts[], request headers (redacted), request bytes, response status, response headers (redacted), response bytes, ttfb, ttft, end, outcome, truncated, capture_mode}`. Bytes are stored exactly as they passed through.
- **Redaction:** `Authorization`, `x-api-key`, `x-llm-proxy-key`, cookies and any configured header are replaced by their scheme only (for example `Bearer [redacted]`) before the record is written. In `client_passthrough` mode the client's OAuth token is never written anywhere; see [upstream-connection.md](upstream-connection.md#what-the-proxy-must-and-must-not-do-in-passthrough-mode).
- **Durability:** `fsync` on rotation and, if configured, every N milliseconds. A crash loses at most the unsynced tail of the open segment.
- **Failure:** a write error is logged and counted (`llm_proxy_journal_write_errors_total`), and the request is unaffected. The readiness probe reports not-ready after repeated write errors, so an orchestrator can move traffic.
- **Permissions:** directory `0700`, files `0600`, owned by the service user. The journal holds prompts and outputs.
- **Retention:** sealed segments older than the retention window are deleted by the partition and retention job. Defaults and sizing are in [deployment.md](deployment.md#journal-storage-and-retention).

### Projector

The only component that understands provider formats. It turns journal records into Postgres rows.

- Pure functions from one record to a set of rows, per protocol family (`anthropic`, `openai`), golden-tested against recorded SSE fixtures.
- Tails the active segment up to the last complete record, then continues into newer segments. Target lag: under 2 seconds.
- For each record it:
  - decodes the request (`encoding/json` with structs per event type, unknown fields kept as `json.RawMessage`) and rebuilds the response from the SSE events;
  - normalizes usage into disjoint columns and keeps the raw provider usage JSON (gap review B6);
  - computes cost from the versioned `pricing` table, snapshotting the pricing version;
  - hashes cumulative prompt segments for cache diagnosis (design doc 5.1; canonical form per gap review R6);
  - finds the first new segment by comparing prefix hashes with the session's previous call, and extracts the **delta text**: the response text, the new tool results (first 64 KB each), the new user message (UI report 7.1);
  - emits `tool_use` and `tool_result` halves to the tool-call joiner;
  - derives the session when no header supplied one, and updates `sessions` and `agents`, including the inferred link from a sub-agent to the `Task` tool call that spawned it (UI report 3.2, gap G2).
- **Write path:** `pgx` `CopyFrom` into per-batch staging tables, then `INSERT … ON CONFLICT DO NOTHING` into the partitioned tables, then the checkpoint update, all in one transaction. Re-running over the same records is a no-op, so delivery from journal to Postgres is at-least-once with an idempotent effect (gap review B5).
- **Checkpoint:** `projector_checkpoints(journal_id, segment, offset)`.
- **Reprojection:** `llm-proxy reproject --from <segment>` truncates the affected partitions for the time range and replays. This is how a fixed normalization rule reaches history.

### Tool-call joiner

A proxy never sees a tool run. It sees the model request the tool in response N, and the client return the result in request N+1, linked by the tool-call id (approaches report section 3).

- A `tool_use` block (Anthropic) or `tool_calls[]` entry (OpenAI) inserts a `tool_calls` row: id, name, arguments, requesting call, agent, prompt, `status = pending`.
- A `tool_result` block or `role: "tool"` message in a later request updates that row with the result head, `is_error`, the resolving call id and the duration. The update is keyed by `(session_key, tool_call_id)` and only touches rows still pending, so it is idempotent.
- **Duration:** from `x-claude-code-prev-tool-durations` when present (`duration_source = header`). Otherwise the gap between the end of response N and the start of request N+1 (`duration_source = estimated`). Parallel tool calls each get their own reported time.
- **Unmatched halves** are kept and flagged: a use with no result (session ended, interrupted) becomes `unmatched = use_only` once the session is idle; a result with no use (history compacted, or projected before its use) is inserted as `unmatched = result_only` and merged if the use arrives later.
- **Server-side tools** (Anthropic web search, code execution, OpenAI hosted tools) appear complete inside one response and are recorded with `source = server`.
- **MCP tools** reach the proxy only through the later MCP gateway (design doc 3.6). Its records land in the same table with `source = mcp` (UI report gap G11).

### Admin and query API

Served on the admin listener under `/admin/`. JSON over HTTP, described by an OpenAPI 3.1 document at `/admin/openapi.json`, from which the UI generates its TypeScript types.

| Method and path | Purpose | Gap |
|---|---|---|
| `GET /admin/requests` | Call log: filters, keyset cursor, `limit` | G1, G8, G9 |
| `GET /admin/requests/{id}` | One call's metadata, attempts and tool calls | |
| `GET /admin/requests/{id}/body?part=request\|response&range=` | Raw captured bytes from the journal, by byte range | G4, G5 |
| `GET /admin/requests/{id}/conversation` | Parsed delta: new messages, response blocks, tool pairs | G4 |
| `GET /admin/requests/{id}/sse` | Response as a list of SSE events with relative timestamps | |
| `GET /admin/sessions` | Session list with the same filters and cursor | G3 |
| `GET /admin/sessions/{key}` | Summary, prompts, agents and calls (metadata only) | G2, G3 |
| `GET /admin/sessions/{key}/tool-calls` | Tool calls of a session | G3 |
| `GET /admin/sessions/{key}/cache-analysis` | Cache diagnosis as a list of request pairs (moved under `/admin`) | R5, R8 |
| `GET /admin/tool-calls` | Tool-call explorer list | G6 |
| `GET /admin/tool-calls/summary` | Per-tool count, error rate, p50 and p95 duration | G6 |
| `GET /admin/search` | Full-text search over delta text and tool arguments | G10 |
| `GET /admin/usage` | Time series from rollups, `group_by` and `grain` | G7 |
| `GET /admin/stream` | SSE live tail with the call-log filters | G13 |
| `GET /admin/alerts?since=` | Alert feed for the UI and the firstmate adapter | R9 |
| `POST/DELETE /admin/keys…`, `GET/PUT /admin/aliases/{name}`, `GET /admin/providers`, `GET/PUT /admin/pricing` | Configuration (design doc 6.1) | |
| `POST /admin/session`, `DELETE /admin/session` | Browser sign-in and sign-out | R8 |

List conventions:

- **Filters** are query parameters, repeated for multiple values: `model=a&model=b`, `status=429`, `latency_ms_gte=60000`, `tool=Bash&tool_error=true`, `text=…`. The full filter list is the one in UI report 5.1.
- **Keyset pagination** on `(ts, id)`: `WHERE (ts, id) < ($ts, $id) ORDER BY ts DESC, id DESC LIMIT $n`. The cursor is opaque (base64 of `ts` and `id`). Responses carry `next_cursor` and `prev_cursor`. No offset pagination (UI report 6.3).
- **No exact totals.** Responses carry `approx_count`, from rollups when the filters map onto rollup dimensions, otherwise from the planner estimate, otherwise `null`.
- Every list query is bounded by a time range (default last 24 hours), so partition pruning applies.
- **Search** uses `websearch_to_tsquery('simple', …)` over `call_text.tsv` for words and phrases, and `ILIKE` or `~` over `tool_calls.args_text` with `pg_trgm` for substrings and regular expressions. Snippets use `ts_headline` on the matched rows only. Measured costs are in UI report 7.3: phrase queries under 1 ms, substring 10 to 50 ms.
- **Body reads** keep an LRU of open segment readers. A `range` read decodes one zstd frame and returns the slice.

### Live tail

- The post-stream summarizer and the request start publish `call.started` and `call.finished` events to an in-process broadcaster.
- Each subscriber gets a bounded channel. A slow subscriber loses events, and the loss is counted (`llm_proxy_live_events_dropped_total`).
- `GET /admin/stream` applies the call-log filters to the event stream and writes SSE. Each event has an `id` (the call's UUIDv7). A reconnecting client sends `Last-Event-ID` and receives the finished calls it missed from Postgres.
- In-flight calls appear only in the live tail; the persisted log shows finished calls.
- **Multi-replica, pending decision D8:** the projector sends `NOTIFY llm_proxy_calls` with the call id after each commit, and every replica's admin plane relays those events. In-flight calls are visible only on the replica serving them. Alternative: the UI subscribes to every replica. See [deployment.md](deployment.md#topology-b-multi-replica-team-or-production).

### OTel export

- The proxy's own health is exposed as Prometheus metrics at `/metrics` on the admin listener (gap review R16). The full list is in [deployment.md](deployment.md#observability).
- Call data is exported by the projector, not by the hot path, as OTLP spans: one `chat` span per call and one `execute_tool` span per tool call, grouped by session and nested by agent.
- Attributes follow the OpenTelemetry GenAI semantic conventions, pinned to one schema version, because the conventions are still in development and moved to their own repository (approaches report 4.10). Inclusive input-token counts are derived at export time from the disjoint stored columns.
- Content capture in spans is off by default. OTel is one output among several (design doc 6.3), not the primary interface.
- Incoming `traceparent` is recorded and forwarded upstream.

### Alerts

- An alert evaluator runs every minute over Postgres (gap review R9).
- v1 alert types: budget at 80% and 100% per key, cache hit ratio below 0.5 over 1 hour on a key with at least 20 calls (gap review R7), fallback rate above a threshold, error-rate spike, journal or projector lag above a threshold, dropped exchanges.
- Alerts are rows in an append-only `alerts` table with a `bigserial` id. `GET /admin/alerts?since=<id>` returns newer alerts. This is what the firstmate process-event adapter polls (design doc 8.3 b).
- Deduplication: at most one alert per (key, type) per hour.

### Background jobs

| Job | Interval | Work |
|---|---|---|
| Partition and retention | hourly | Create daily partitions 7 days ahead; drop partitions past each table's retention; delete sealed journal segments past journal retention |
| Rollups | 1 minute | Recompute hour buckets for a sliding 2-hour window, so late rows are absorbed; day buckets recomputed from raw rows, not from hour rows (gap review R11) |
| Session idle close | 5 minutes | Mark pending tool uses in idle sessions as `use_only` |
| Alert evaluator | 1 minute | See above |

In the multi-replica tier, jobs that write shared tables take a Postgres advisory lock, so only one replica runs each at a time.

### Configuration and key cache

- Static configuration comes from one YAML file plus environment variables; secrets come from files or environment variables referenced by name. The layout is in [deployment.md](deployment.md#configuration).
- **Pending decision D7:** providers and their credential references live in the config file; virtual keys, aliases, routes and pricing live in Postgres and are edited through the admin API (recommended). Alternative: everything in Postgres with an encrypted credential column.
- The in-memory cache holds keys, aliases, routes and pricing. It refreshes on `NOTIFY llm_proxy_config` and on a 30-second TTL.

## Sequence: a streaming request with fallback

```mermaid
sequenceDiagram
    autonumber
    participant C as Client (Claude Code)
    participant S as Data plane
    participant A as Auth, correlation, route
    participant T as Retrying RoundTripper
    participant U1 as Upstream route 1
    participant U2 as Upstream route 2
    participant P as Tap
    participant X as Post-stream summarizer
    participant Q as Exchange queue
    participant L as Live broadcaster

    C->>S: POST /v1/messages, stream: true
    S->>A: resolve key from cache, limits, budget pre-check
    A->>A: read harness headers, resolve alias to routes
    A-->>S: identity, correlation, route list
    S->>L: call.started (non-blocking)
    S->>S: clear write deadline for this stream
    S->>T: forward: credential applied, model spliced if needed
    T->>U1: attempt 1
    U1-->>T: 529 overloaded
    Note over T: before commit: record attempt, move to next route
    T->>U2: attempt 2
    U2-->>T: 200 text/event-stream
    Note over T,S: commit point: first 2xx, nothing sent to client yet
    T-->>S: response headers and body
    S-->>C: 200 headers (forwarded unchanged, plus x-request-id)
    loop each chunk until message_stop
        U2-->>P: SSE bytes (including ping)
        P->>P: copy into bounded buffer, reset idle watchdog
        P-->>C: same bytes, flushed immediately
    end
    P->>X: Close: exchange finished
    X->>X: parse usage, stop reason, tool names
    X->>X: add cost to key budget counter
    X->>L: call.finished
    X->>Q: non-blocking send (drop and count if full)
```

A client abort follows the same path from the abort onward: the context cancels upstream, `Close` fires, and the exchange is logged with partial bytes and `error_class = client_abort`.

## Sequence: journal and projection

```mermaid
sequenceDiagram
    autonumber
    participant Q as Exchange queue
    participant J as Journal writer
    participant F as Segment files
    participant P as Projector
    participant TJ as Tool-call joiner
    participant PG as Postgres
    participant N as Admin planes

    Q->>J: exchange
    J->>J: redact secret headers
    J->>F: append record: header + one zstd frame
    J->>F: rotate by size or age: fsync, seal, open next
    P->>PG: read checkpoint (journal_id, segment, offset)
    loop until caught up, then poll
        P->>F: read next complete record
        P->>P: parse request and SSE response for the family
        P->>P: disjoint usage, cost, segment hashes, delta text
        P->>TJ: tool_use and tool_result halves
        P->>PG: batch: COPY into staging tables
        P->>PG: INSERT ... ON CONFLICT DO NOTHING into requests, request_attempts, request_segments, call_text
        TJ->>PG: insert pending tool calls, resolve earlier ones by (session_key, tool_call_id)
        P->>PG: upsert sessions and agents
        P->>PG: update checkpoint (same transaction)
        P->>N: NOTIFY llm_proxy_calls (multi-replica live tail)
    end
```

If Postgres is down, the projector retries with backoff and the journal keeps growing. When Postgres returns, the projector catches up from its checkpoint. Nothing on the data plane notices.

## Data model changes

The design doc's section 4 is the starting point. This architecture needs the following changes. Column lists are indicative; the migrations are the source of truth once they exist.

| Change | Reason |
|---|---|
| `requests` primary key becomes `(id, ts)`, ids are UUIDv7 | The design doc's `id` primary key fails on a table partitioned by `ts` (gap review B1) |
| `request_attempts` and `request_segments` gain `ts` and are partitioned like `requests` | Retention by `DROP PARTITION` (B1) |
| Rollup dimensions use a sentinel `'unknown'` instead of NULL | NULL breaks the rollup primary key (B1) |
| `requests` gains `session_key text`, `agent_id`, `parent_agent_id`, `agent_type`, `prompt_id`, `request_class`, `compaction`, `client_raw`, `client_request_id`, `capture_mode`, `truncated`, `usage_estimated`, `journal_id`, `journal_segment`, `journal_offset`, `journal_length` | Correlation filters (G1) and body reads by pointer (G5) |
| Usage columns become disjoint: `input_uncached`, `cache_read`, `cache_write_5m`, `cache_write_1h`, `output` (including reasoning), `reasoning` (informational), plus `usage_raw jsonb` | One meaning per column; cost is the sum of column times price; raw kept for recomputation (B6) |
| `request_attempts` gains `upstream_request_id` and usage columns | Vendor support tickets (R17); billed tokens on failed attempts (B6) |
| New `providers` view of the config file, `admin_tokens`, `alerts`, `projector_checkpoints` | B8, R8, R9 |
| New `agents(id, session_key, parent_agent_id, agent_type, spawned_by_tool_call_id, link_confidence, first_ts, last_ts)` | Session tree (G2) |
| New `tool_calls` (below) | Tool-call explorer and filters (G6) |
| New `call_text` (below) | Body search (G10) |
| New indexes: `requests (ts DESC, id DESC)`; `requests (ts DESC) WHERE error_class IS NOT NULL`; `requests (agent_id, ts) WHERE agent_id IS NOT NULL`; `requests (prompt_id)` | Keyset pagination and the measured filters (G8, G9, UI report 6.4) |
| `usage_rollups` gains `session_key`, `agent_type`, `alias`, `provider`, `fallbacks`, `reasoning` dimensions or a second rollup by session | Cost views and the call-log summary strip (G7, R11) |
| `request_bodies` is not used | Bodies stay in the journal; Postgres holds a pointer and the searchable delta text |

Sketch of the two new fact tables, both partitioned by day:

```sql
CREATE TABLE tool_calls (
  id                    uuid NOT NULL,          -- UUIDv7
  ts                    timestamptz NOT NULL,   -- time of the tool_use
  session_key           text,
  tool_call_id          text NOT NULL,          -- provider id: toolu_..., call_...
  tool_name             text NOT NULL,
  source                text NOT NULL,          -- 'client' | 'server' | 'mcp'
  requested_by_call_id  uuid,                   -- call whose response held the tool_use
  resolved_by_call_id   uuid,                   -- call whose request held the tool_result
  agent_id              text,
  prompt_id             text,
  args_json             jsonb,
  args_text             text,                   -- flattened, for pg_trgm
  result_head           text,                   -- first 64 KB
  result_bytes          int,
  is_error              boolean,
  duration_ms           int,
  duration_source       text,                   -- 'header' | 'estimated'
  status                text NOT NULL,          -- 'pending' | 'resolved' | 'use_only' | 'result_only'
  PRIMARY KEY (id, ts)
) PARTITION BY RANGE (ts);
CREATE INDEX ON tool_calls (tool_name, ts DESC);
CREATE INDEX ON tool_calls (session_key, tool_call_id);
CREATE INDEX ON tool_calls USING gin (args_text gin_trgm_ops);

CREATE TABLE call_text (
  request_id    uuid NOT NULL,
  ts            timestamptz NOT NULL,
  part          text NOT NULL,     -- 'response' | 'user' | 'tool_result' | 'tool_args'
  tool_call_id  text,
  body          text NOT NULL,
  tsv           tsvector GENERATED ALWAYS AS (to_tsvector('simple', left(body, 250000))) STORED
) PARTITION BY RANGE (ts);
CREATE INDEX ON call_text USING gin (tsv);
```

The `simple` text-search configuration avoids stemming code identifiers. The `left(…, 250000)` cap stays under the `tsvector` size limit. System prompts and tool definitions are indexed once per distinct segment hash, not per call (UI report 7.1).

Retention defaults: `requests` and `request_attempts` 400 days, `request_segments` 14 days, `call_text` 14 days (captain's choice), `tool_calls` 400 days with `result_head` cleared after the journal retention. Deployment sizing is in [deployment.md](deployment.md#journal-storage-and-retention).

## Package layout

```
llm-proxy/
├── cmd/
│   └── llm-proxy/              main: subcommand dispatch (serve, migrate, project, reproject, keys)
├── internal/
│   ├── config/                 YAML + env loading, secret references, validation
│   ├── server/                 listeners, mux, middleware chain, graceful shutdown
│   ├── auth/                   virtual keys, admin tokens, HMAC, limits, key cache
│   ├── budget/                 per-key counters (in-process; Postgres row in team tier)
│   ├── correlate/              harness header extraction, client normalization
│   ├── route/                  aliases, routes, provider registry, model splice
│   ├── proxy/                  ReverseProxy setup, Rewrite, retrying RoundTripper, commit point,
│   │                           include_usage inject and strip, protocol-correct errors
│   ├── tap/                    byte tap, idle watchdog, exchange assembly, post-stream summarizer
│   ├── journal/                record format, segment writer, reader with random access, retention
│   ├── project/                projector loop, checkpoints, batching, reprojection
│   │   ├── anthropic/          Messages request and SSE parsing, usage
│   │   ├── openai/             chat completions and Responses parsing, usage
│   │   ├── segments/           canonical segment hashing, delta extraction
│   │   └── toolcalls/          tool-call joiner
│   ├── store/                  pgx pool, sqlc-generated queries, embedded goose migrations
│   ├── query/                  read side: filters, keyset cursors, search, approx counts
│   ├── adminapi/               HTTP handlers, OpenAPI document, browser session cookies
│   ├── live/                   broadcaster, SSE writer, Postgres LISTEN relay
│   ├── jobs/                   partitions, retention, rollups, session idle close
│   ├── alerts/                 evaluator and rules
│   ├── otelexport/             GenAI span mapping, pinned semantic-convention version
│   ├── metrics/                Prometheus collectors for proxy internals
│   └── ui/                     //go:embed all:dist, SPA handler (see frontend.md)
├── migrations/                 goose SQL files, embedded by internal/store
├── api/
│   └── openapi.yaml            admin API contract, source for the UI's TypeScript types
├── web/                        React + Vite source (see frontend.md)
├── testdata/
│   └── fixtures/               recorded SSE exchanges per provider, golden projector outputs
└── docs/
```

Dependency rules:

- `proxy`, `tap`, `auth`, `route` and `correlate` (the hot path) never import `store`, `query` or `project`.
- `project` is the only package that decodes provider body formats.
- `adminapi` reads through `query` and `journal`; it never writes fact tables.

Later executables from design doc 9 (the firstmate alert adapter, the ledger reporter) go under `cmd/` in the same module (gap review N9).

## Libraries

| Concern | Choice |
|---|---|
| HTTP, proxying, SSE | `net/http`, `net/http/httputil` (no fasthttp: no HTTP/2 client, different streaming model) |
| Hot-path JSON | `github.com/tidwall/gjson` to read, `github.com/tidwall/sjson` to splice |
| Journal compression | `github.com/klauspost/compress/zstd` |
| Postgres | `github.com/jackc/pgx/v5`, `sqlc`, goose migrations |
| Logging | `log/slog`, JSON handler |
| Metrics | Prometheus Go client |
| OTel | `go.opentelemetry.io/otel` SDK with the OTLP exporter |
| Provider SDKs (`anthropic-sdk-go`, `openai-go`) | Tests only, as clients against the proxy. Never in the data path |
| Go version | Latest two Go releases; today 1.26 and 1.27 (approaches report section 8) |

## Defaults taken for open gaps

These follow the gap review's suggestions. All are part of **pending decision D9 and D10** in the [README](README.md#open-decisions).

| Gap | Default used here |
|---|---|
| B1 schema | `PRIMARY KEY (id, ts)`, UUIDv7, `ts` on child tables, sentinel rollup dimensions, no foreign keys on fact tables by design |
| B2 protocol scope | Same-family passthrough (D3) |
| B3 model names | Any string may be an alias; unmapped names rejected; opt-in passthrough per key |
| B4 fallback | Commit at first 2xx before any byte to the client; per-alias `max_attempts`, per-class timeouts, `fallback_on: [5xx, 429, timeout]`; per-replica circuit breaker |
| B5 queue | In-process bounded channel; journal is the durable buffer; at-least-once, idempotent projection; drops counted |
| B6 usage | Disjoint columns plus `usage_raw`; inclusive values derived at export; `include_usage` injected for OpenAI streams |
| B7 budgets | Soft enforcement from an in-process counter; Postgres counter row in the team tier; UTC calendar periods |
| B8 providers | Config-file providers with credential references (D7) |
| B10 Go baseline | As in [Libraries](#libraries); one static binary with subcommands; distroless image |
| R1 keys | `lp_` keys, HMAC-SHA256 with pepper, NOTIFY plus 30 s TTL cache |
| R8 admin auth | Hashed admin tokens with `admin` and `read` scopes; browser session cookie |
| R10 retention | See [Data model changes](#data-model-changes) and [deployment.md](deployment.md#journal-storage-and-retention) |
| R14 multi-replica | v1 targets one replica; team tier described in [deployment.md](deployment.md#topology-b-multi-replica-team-or-production); drain timeout 10 minutes |
| R15 targets | Added p99 overhead under 5 ms before the first byte; 200 concurrent streams on one small VM; 32 MiB maximum body |
| R16 self-observability | `/healthz`, `/readyz`, `/metrics`, OTLP spans with content off, `traceparent` propagated |
