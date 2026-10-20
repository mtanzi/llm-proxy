# Backend architecture (Go)

Part of the [architecture set](README.md). Storage sits behind ports; see [Storage ports and adapters](#storage-ports-and-adapters). Upstream credentials and Claude subscription routing are in [upstream-connection.md](upstream-connection.md). The UI that consumes the admin API is in [frontend.md](frontend.md). How the binary is deployed is in [deployment.md](deployment.md).

## Shape

One Go module, one static binary, `llm-proxy`, with subcommands:

| Subcommand | Purpose |
|---|---|
| `serve` | Runs the data plane, the admin plane, the batch writer and the background jobs |
| `migrate up` / `migrate status` | Applies the migrations embedded in the configured store adapter (v1: `store/postgres`) |
| `keys create` / `keys revoke` | Bootstrap virtual keys and admin tokens from the command line, before the UI exists |

**Storage model.** The proxy never stores request or response bodies. The tap reads each exchange in memory, the extractor keeps a small set of facts (who, route, tokens, cost, timings, outcome, cache hashes, tool-call metadata), and the batch writer inserts them as rows through the `CallWriter` store port, implemented in v1 by the `store/postgres` adapter. There is no capture journal and no body table. Consequences: no search over prompt or response text, call detail shows numbers and tool names but not content, and a wrong parsing rule cannot be recomputed for past rows; a fix applies to new calls only. This resolves decisions D1 and D2 ([README](README.md#resolved-decisions)).

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
        ext[Extractor]
        q[[Bounded call queue]]
        bw[Batch writer and<br/>tool-call joiner]
        live[Live broadcaster]
        budget[Budget counters]
        cache[Key and config cache]

        subgraph bg[Background workers]
            alerts[Alert evaluator]
            otel[OTel exporter]
        end

        subgraph ap[Admin plane listener]
            api[Admin and query API]
            sse[SSE stream]
            ui[Embedded UI assets]
            metrics[metrics endpoint]
        end

        subgraph ports[Store ports: internal/store]
            cw[CallWriter]
            cq[CallQuery]
            sr[StatsReader]
            cs[ConfigStore]
            lf[LiveFeed]
            al[AlertLog]
        end

        subgraph adapter[store/postgres adapter]
            pga[SQL, migrations,<br/>LISTEN/NOTIFY]
            jobs[Adapter jobs:<br/>rollups, partitions, retention]
        end
    end

    up[Provider APIs]
    pg[(Postgres)]
    col[OTLP collector]

    cc --> mux
    oc --> mux
    mux --> auth --> corr --> route --> rp
    auth -.reads.-> cache
    auth -.pre-check.-> budget
    rp <--> up
    rp --- tap
    tap -->|on Close| ext
    ext --> budget
    ext --> live
    ext -->|non-blocking send| q
    q --> bw --> cw
    cs -.snapshot and changes.-> cache
    lf -.committed calls.-> live
    alerts --> sr
    alerts --> al
    bw --> otel --> col
    br --> ui
    br --> api
    br --> sse
    api --> cq
    api --> sr
    api --> cs
    api --> al
    live --> sse
    cw --> pga
    cq --> pga
    sr --> pga
    cs --> pga
    lf --> pga
    al --> pga
    jobs --> pga
    pga --> pg
```

Everything between the Go components and Postgres goes through the store ports. Only `cmd/llm-proxy` wiring imports the `store/postgres` adapter (see [Storage ports and adapters](#storage-ports-and-adapters)).

Two properties hold for every arrow out of the data plane:

- **The hot path never waits on storage.** Nothing between the client and the upstream calls a store port. Auth and routing read an in-memory cache. Budget checks read an in-process counter.
- **Logging never fails a request.** A full queue drops the call record and increments a counter. A failed batch write is retried, logged and counted. A store outage (for example Postgres down) fills the queue, and records past its capacity are dropped and counted.

## Components

### Listener and router

- `net/http` server with the standard `ServeMux` and method patterns (Go 1.22 and later). No web framework (gap review B10, approaches report section 8).
- Server timeouts: set `ReadHeaderTimeout` and `IdleTimeout`. **Do not set a global `WriteTimeout`**: it cuts long SSE streams. The approaches report measured this (section 6, finding 4). Each streaming request clears its own write deadline with `http.NewResponseController(w).SetWriteDeadline(time.Time{})`.
- Request size limit: 32 MiB per request body (gap review R15), enforced with `http.MaxBytesReader`.
- Every response carries `x-request-id`, a UUIDv7 that is also the primary id of the call. A client-supplied `x-request-id` is kept as `client_request_id`, never reused as the id (gap review R17).
- Middleware order: recover, request id, access log (`log/slog`, with header redaction in one place), auth, correlation, route.
- Graceful shutdown on SIGTERM: stop accepting connections, wait for in-flight streams up to a drain deadline (default 10 minutes, gap review R14), close the queue, and let the batch writer drain it into the store within a flush deadline (default 30 seconds). Records still queued after the deadline are counted as dropped and their number is logged.

### Auth (virtual keys)

- **Virtual keys** look like `lp_<base62 of 32 random bytes>`. The store (`ConfigStore`) keeps only an HMAC-SHA256 of the key with a server pepper, plus a display prefix of `lp_` and 4 characters (gap review R1). The plaintext is shown once at creation.
- **Where the key is read from.** In the default `proxy_key` mode the client sends its virtual key as its API credential: `Authorization: Bearer lp_…` or `x-api-key: lp_…`. In `client_passthrough` mode the client's own credential occupies `Authorization`, so the virtual key travels in a separate `x-llm-proxy-key` header. [upstream-connection.md](upstream-connection.md#credential-modes) explains both modes. The proxy consumes `x-llm-proxy-key` and never forwards it.
- **Lookup** is an in-memory map from HMAC to key record, loaded at start from `ConfigStore` and refreshed on its change notifications (the `store/postgres` adapter uses `LISTEN/NOTIFY`), with a 30-second TTL backstop. Revocation therefore reaches every replica within 30 seconds at worst.
- **Checks**, in order: key exists, not expired, not revoked, alias or model allowed, requests-per-minute and concurrency limits, budget pre-check. A failure returns a protocol-correct error body for the client's family (Anthropic or OpenAI shape) with a stable `type` per error class (gap review R3), and `retry-after` in integer seconds on 429.
- **Budget enforcement is soft** (gap review B7). The extractor adds each call's cost to an in-process atomic counter per key. The pre-check reads that counter, so overshoot is bounded by the calls in flight. In the multi-replica tier the counter becomes a shared counter row in the store (a `ConfigStore` method; a Postgres row in the `store/postgres` adapter); see [deployment.md](deployment.md#shared-state-in-the-multi-replica-tier).
- **Admin plane auth** is separate: admin tokens with scope `admin` or `read`, stored hashed like virtual keys (gap review R8). The browser exchanges a token for a session cookie; see [frontend.md](frontend.md#authentication-to-the-admin-api).

### Correlation

The correlation step reads harness headers into the call record. It never parses the body.

| Field | Source, in priority order |
|---|---|
| `session_key` | `x-claude-code-session-id`; then Codex and OpenCode session headers (`session-id`, `x-session-affinity`, `x-session-id`, `thread-id`, `conversation_id`); then a generic `x-session-id`; otherwise empty, and the batch writer derives one |
| `agent_id`, `parent_agent_id` | `x-claude-code-agent-id`, `x-claude-code-parent-agent-id` |
| `agent_type` | `x-claude-code-agent-type` (hint header) |
| `prompt_id` | `x-claude-code-prompt-id` (hint header) |
| `request_class` | `x-claude-code-request-class`: `main`, `subagent`, `workflow`, `compaction`, `auxiliary` (hint header) |
| `compaction`, `context_compacted` | `x-claude-code-compaction`, `x-claude-code-context-compacted` (hint headers) |
| `prev_tool_durations` | `x-claude-code-prev-tool-durations`, raw value; parsed by the extractor (hint header) |
| `trace_id` | W3C `traceparent` |
| `client`, `client_raw` | `User-Agent` normalized to its product token (`claude-cli/2.1.290` becomes `claude-cli`), raw value kept (gap review R17) |

- The Claude Code header names and semantics come from Anthropic's [gateway compatibility guide](https://code.claude.com/docs/en/llm-gateway-protocol). The session and agent IDs are always sent. The rows marked "hint header" arrive behind a custom base URL only when the client sets `CLAUDE_CODE_GATEWAY_HINT_HEADERS=1`. **Pending decision D5** in the [README](README.md#open-decisions): set it for firstmate crewmates (recommended).
- The Codex and OpenCode header names are known only from Bifrost's source, not from vendor docs (approaches report 2.3). Each needs a capture test before it is relied on. **Pending decision D4.**
- Correlation headers are forwarded upstream unchanged, so the proxy stays transparent.
- Session derivation, when no session header is present, happens in the batch writer, not on the hot path: hash of (key, client, tools and system segment hash, hash of the first user message), with a 30-minute idle timeout (gap review R5). Only hashes are used; no message text is kept.

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
- **Client abort:** the upstream request uses the client's context, so an abort cancels upstream (gap review R4). The call is still logged, with the facts extracted so far, `error_class = client_abort` and `usage_estimated = true`.
- **Upstream transport:** HTTP/2 to providers by default, keep-alives on, connection pool sized per provider.

### Tap

The tap reads the stream in memory, extracts facts and discards the bytes. No body is written to disk or to the store.

- `ModifyResponse` wraps the upstream response body in a tap. `Read` passes each chunk to an incremental scanner and forwards it unchanged, then resets the idle watchdog. For SSE it splits events on blank lines and keeps only the facts below. For a non-streaming JSON response it buffers up to the size bound, then parses once.
- The request body is read the same way, through a tee into a per-exchange buffer. The extractor needs the whole request to hash prompt segments; the buffer is released as soon as extraction ends.
- Buffers are bounded (default 32 MiB for the request, 1 MiB for one SSE event). Past a bound the tap stops reading that part, keeps forwarding, and sets `extraction_partial = true` on the record. Fields it could not read stay null.
- Buffers come from a `sync.Pool` and return to it after extraction. Nothing keeps a reference to a body after that.
- `Close` is the "exchange finished" hook. `ReverseProxy` closes the upstream body on success, on upstream error and on client abort, so the hook is reliable.
- The tap measured 54 µs in total for a whole 5-event stream in the approaches report's experiment. That figure covered copying only; the cost of incremental scanning is a phase-1 measurement.

### Extractor

The only component that understands provider formats. It runs in its own goroutine after `Close`, off the hot path, and turns one exchange into one **call record**. Pure functions per protocol family (`anthropic`, `openai`), golden-tested against recorded SSE fixtures.

A call record holds:

- **Who:** virtual key, client, session key, agent and parent agent, agent type, prompt id (all from headers, see [Correlation](#correlation)).
- **What ran:** alias, route, final provider and model, attempts with upstream request ids.
- **Usage and cost:** tokens normalized into disjoint columns (input, cache read, cache write, output, reasoning), the raw provider usage JSON (gap review B6), and cost from the versioned `pricing` table with the pricing version.
- **Timings:** time to first byte, TTFT, total duration, and for tool-call duration the start and end timestamps.
- **Outcome:** HTTP status, `error_class`, the provider's error `type` (for example `overloaded_error`), stop reason, `streamed`, `usage_estimated`, `extraction_partial`. The provider's error message text is not kept.
- **Cache diagnosis:** hashes of cumulative prompt segments (design doc 5.1; canonical form per gap review R6), computed over the request in memory. Only hashes and token estimates leave the extractor.
- **Tool halves:** for each `tool_use` block (Anthropic) or `tool_calls[]` entry (OpenAI) in the response: the provider call id, tool name and argument size in bytes. For each `tool_result` block or `role: "tool"` message in the request: the call id it answers, `is_error` and the result size in bytes. Argument text and result text are read to measure them and are not kept. No snippet, no prompt text and no header value other than the correlation fields above is stored; credentials are never read into the record. In `client_passthrough` mode the client's OAuth token is never written anywhere; see [upstream-connection.md](upstream-connection.md#what-the-proxy-must-and-must-not-do-in-passthrough-mode).

**Pending decisions D11 to D13** ([README](README.md#open-decisions)) would relax the strict reading: short tool argument and error snippets (D11), provider error message text (D12), and an opt-in per-key body capture switch (D13). The default is the strict option for all three: none of that is kept.

After building the record the extractor:

1. Adds the cost to the key's budget counter.
2. Publishes `call.finished` to the live broadcaster.
3. Sends the record to the bounded queue with a non-blocking `select`. When the queue is full it drops the record and increments `llm_proxy_calls_dropped_total`.

### Batch writer

One or more goroutines receive call records from the queue and hand them to the `CallWriter` port in batches. There is no intermediate store: the queue is the only buffer. The `writer` package owns the queue, batching, retry, drop counting and shutdown drain. It contains no SQL; the adapter behind the port does the insert.

- **Batching:** a batch closes at `writer.batch_rows` records or `writer.batch_interval`, whichever comes first (defaults 1000 rows, 500 ms). The writer calls `CallWriter.WriteBatch` once per batch.
- **Write path in `store/postgres`:** `pgx` `CopyFrom` into per-batch staging tables, then `INSERT … ON CONFLICT DO NOTHING` into `requests`, `request_attempts` and `request_segments`, then the tool-call upserts and the `sessions` and `agents` updates, all in one transaction. The call id is a UUIDv7 assigned at request start, so a retried batch inserts nothing twice (gap review B5). The port contract is "idempotent on call id and atomic per batch"; the SQL is the adapter's choice.
- **Failure:** a failed `WriteBatch` is retried with backoff and counted (`llm_proxy_writer_errors_total`). While the store is down the queue fills; the extractor then drops and counts new records. The data plane never notices. A crash loses the queue contents, at most `queue_capacity` records; this is the accepted cost of having no durable buffer.
- **Drop counting:** every dropped record increments `llm_proxy_calls_dropped_total` with a `reason` label (`queue_full`, `shutdown`). The alert evaluator raises an alert when the count is above zero.
- **Session derivation:** when no session header was present, the session key is derived from hashes (see [Correlation](#correlation)). The hash function is pure and lives in `domain`. The idle-timeout lookup and the updates to `sessions` and `agents` happen inside `WriteBatch`, including the inferred link from a sub-agent to the `Task` tool call that spawned it (UI report 3.2, gap G2).
- **Shutdown:** on SIGTERM, after in-flight streams finish or the drain deadline passes, the queue is closed and the writer flushes what remains, then exits. See [Listener and router](#listener-and-router).
- **Live tail:** the writer does not notify anyone. After each commit the adapter publishes the committed call ids on the `LiveFeed` port (the `store/postgres` adapter sends `NOTIFY llm_proxy_calls`; multi-replica live tail, pending decision D8).
- **OTel:** the writer hands committed records to the OTel exporter ([OTel export](#otel-export)).

### Tool-call joiner

A proxy never sees a tool run. It sees the model request the tool in response N, and the client return the result in request N+1, linked by the tool-call id (approaches report section 3). The joiner works from the tool halves in call records and keeps metadata only. It is behaviour of `CallWriter.WriteBatch`, not a separate package: the join needs a read-modify-write in the same transaction as the insert, so each adapter implements it and the contract suite checks it ([Contract test suite](#contract-test-suite)).

- A `tool_use` half inserts a `tool_calls` row: id, tool name, provider call id, requesting call, agent, prompt, argument size, `status = pending`.
- A `tool_result` half in a later request updates that row with `is_error`, the result size, the resolving call id and the duration. The update is keyed by `(session_key, tool_call_id)` and only touches rows still pending, so it is idempotent.
- The joiner runs inside the adapter's `WriteBatch` transaction. No argument text, no result text and no snippet of either is stored (see [Data model changes](#data-model-changes)).
- **Duration:** from `x-claude-code-prev-tool-durations` when present (`duration_source = header`). Otherwise the gap between the end of response N and the start of request N+1 (`duration_source = estimated`). Parallel tool calls each get their own reported time.
- **Unmatched halves** are kept and flagged: a use with no result (session ended, interrupted) becomes `unmatched = use_only` once the session is idle; a result with no use (history compacted, or written before its use) is inserted as `unmatched = result_only` and merged if the use arrives later.
- **Server-side tools** (Anthropic web search, code execution, OpenAI hosted tools) appear complete inside one response and are recorded with `source = server`.
- **MCP tools** reach the proxy only through the later MCP gateway (design doc 3.6). Its records land in the same table with `source = mcp` (UI report gap G11).

### Admin and query API

Served on the admin listener under `/admin/`. The handlers (`adminapi`) read through the `CallQuery`, `StatsReader`, `ConfigStore` and `AlertLog` ports and never see SQL. JSON over HTTP, described by an OpenAPI 3.1 document at `/admin/openapi.json`, from which the UI generates its TypeScript types.

| Method and path | Purpose | Gap |
|---|---|---|
| `GET /admin/requests` | Call log: filters, keyset cursor, `limit` | G1, G8, G9 |
| `GET /admin/requests/{id}` | One call's metadata, attempts, cache segments and tool calls. Numbers and names only, no content | G4 |
| `GET /admin/sessions` | Session list with the same filters and cursor | G3 |
| `GET /admin/sessions/{key}` | Summary, prompts, agents and calls (metadata only) | G2, G3 |
| `GET /admin/sessions/{key}/tool-calls` | Tool calls of a session | G3 |
| `GET /admin/sessions/{key}/cache-analysis` | Cache diagnosis as a list of request pairs (moved under `/admin`) | R5, R8 |
| `GET /admin/tool-calls` | Tool-call explorer list | G6 |
| `GET /admin/tool-calls/summary` | Per-tool count, error rate, p50 and p95 duration | G6 |
| `GET /admin/usage` | Time series from rollups, `group_by` and `grain` | G7 |
| `GET /admin/stream` | SSE live tail with the call-log filters | G13 |
| `GET /admin/alerts?since=` | Alert feed for the UI and the firstmate adapter | R9 |
| `POST/DELETE /admin/keys…`, `GET/PUT /admin/aliases/{name}`, `GET /admin/providers`, `GET/PUT /admin/pricing` | Configuration (design doc 6.1) | |
| `POST /admin/session`, `DELETE /admin/session` | Browser sign-in and sign-out | R8 |

List conventions:

- **Filters** are query parameters, repeated for multiple values: `model=a&model=b`, `status=429`, `latency_ms_gte=60000`, `tool=Bash&tool_error=true`. The full filter list is the one in UI report 5.1, without its text-search filters, which are withdrawn.
- **Keyset pagination** on `(ts, id)`: the port takes a `Cursor{TS, ID}` and a limit, and orders by `ts DESC, id DESC`. The `store/postgres` adapter runs `WHERE (ts, id) < ($ts, $id) ORDER BY ts DESC, id DESC LIMIT $n`. The cursor is opaque on the wire (base64 of `ts` and `id`, encoded in `adminapi`). Responses carry `next_cursor` and `prev_cursor`. No offset pagination (UI report 6.3).
- **No exact totals.** Responses carry `approx_count`, from rollups when the filters map onto rollup dimensions, otherwise from the planner estimate, otherwise `null`.
- Every list query is bounded by a time range (default last 24 hours), so partition pruning applies.

There is no endpoint that returns prompt, response or tool content, because none is stored.

### Live tail

- The extractor and the request start publish `call.started` and `call.finished` events to an in-process broadcaster.
- Each subscriber gets a bounded channel. A slow subscriber loses events, and the loss is counted (`llm_proxy_live_events_dropped_total`).
- `GET /admin/stream` applies the call-log filters to the event stream and writes SSE. Each event has an `id` (the call's UUIDv7). A reconnecting client sends `Last-Event-ID` and receives the finished calls it missed from the store (`CallQuery.FinishedAfter`).
- In-flight calls appear only in the live tail; the persisted log shows finished calls.
- **Multi-replica, pending decision D8:** the `live` package subscribes to the `LiveFeed` port, which delivers the ids of committed calls. The `store/postgres` adapter implements it with `NOTIFY llm_proxy_calls` after each commit and `LISTEN` on every replica, and every replica's admin plane relays those events. In-flight calls are visible only on the replica serving them. Alternative: the UI subscribes to every replica. See [deployment.md](deployment.md#topology-b-multi-replica-team-or-production).

### OTel export

- The proxy's own health is exposed as Prometheus metrics at `/metrics` on the admin listener (gap review R16). The full list is in [deployment.md](deployment.md#observability).
- Call data is exported by the batch writer after commit, not by the hot path, as OTLP spans: one `chat` span per call and one `execute_tool` span per tool call, grouped by session and nested by agent.
- Attributes follow the OpenTelemetry GenAI semantic conventions, pinned to one schema version, because the conventions are still in development and moved to their own repository (approaches report 4.10). Inclusive input-token counts are derived at export time from the disjoint stored columns.
- Spans carry no content, because none is stored. OTel is one output among several (design doc 6.3), not the primary interface.
- Incoming `traceparent` is recorded and forwarded upstream.

### Alerts

- An alert evaluator runs every minute over the `StatsReader` port and appends through `AlertLog` (gap review R9).
- v1 alert types: budget at 80% and 100% per key, cache hit ratio below 0.5 over 1 hour on a key with at least 20 calls (gap review R7), fallback rate above a threshold, error-rate spike, writer queue depth or write errors above a threshold, dropped calls.
- Alerts are append-only records with a monotonically increasing id (in `store/postgres`, rows in an `alerts` table with a `bigserial` id). `GET /admin/alerts?since=<id>` returns newer alerts through `AlertLog.Since`. This is what the firstmate process-event adapter polls (design doc 8.3 b).
- Deduplication: at most one alert per (key, type) per hour, enforced by `AlertLog.Append`.

### Background jobs

Two kinds. Storage maintenance jobs belong to the adapter, because they are about its physical layout. The adapter lists them through the `Maintainer` port and `cmd` wiring runs them on the interval each job declares. The alert evaluator is store-independent and runs from the `alerts` package.

| Job | Owner | Interval | Work in `store/postgres` |
|---|---|---|---|
| Partition and retention | adapter | hourly | Create daily partitions 7 days ahead; drop partitions past each table's retention |
| Rollups | adapter | 1 minute | Recompute hour buckets for a sliding 2-hour window, so late rows are absorbed; day buckets recomputed from raw rows, not from hour rows (gap review R11) |
| Session idle close | adapter | 5 minutes | Mark pending tool uses in idle sessions as `use_only` |
| Alert evaluator | `alerts` | 1 minute | See above |

In the multi-replica tier, adapter jobs that write shared tables take a Postgres advisory lock, so only one replica runs each at a time. The lock is part of the adapter; the port only promises that running a job on several replicas is safe.

### Configuration and key cache

- Static configuration comes from one YAML file plus environment variables; secrets come from files or environment variables referenced by name. The layout is in [deployment.md](deployment.md#configuration).
- **Pending decision D7:** providers and their credential references live in the config file; virtual keys, aliases, routes and pricing live in the store (Postgres in v1) behind the `ConfigStore` port and are edited through the admin API (recommended). Alternative: everything in the store with an encrypted credential column.
- The in-memory cache holds keys, aliases, routes and pricing. It loads a `ConfigStore` snapshot and refreshes on `ConfigStore` change notifications (in `store/postgres`, `NOTIFY llm_proxy_config`) and on a 30-second TTL.

## Storage ports and adapters

Storage sits behind ports, in the ports-and-adapters (hexagonal) style. The Go components need a small set of storage capabilities. A store adapter provides them. v1 ships one adapter, `store/postgres`, plus an in-memory adapter for tests. This is resolved decision D14 ([README](README.md#resolved-decisions)).

Rules:

- **Ports follow use cases, not tables.** There is no generic `Get/Put/Query` interface and no repository per table. Each port is what one consumer needs.
- **Each adapter owns its SQL and its migrations.** No SQL is shared between adapters, and there is no ORM or query builder above the ports. The `store/postgres` adapter uses `pgx`, `sqlc` and goose, all internal to it.
- **Domain types are shared, storage types are not.** The ports speak `domain` types (call record, filters, pages, cursors). Row structs and generated query code never leave the adapter.
- **Ports sit with the consumers' vocabulary.** They are defined in `internal/store` and use only `domain` and the standard library. They import no driver.

### Ports

Six interfaces. The first five are the ones named by the design; `AlertLog` is the append-only alert feed, which needs a write and a read and fits none of the others. Signatures are a sketch of shape and responsibility; the Go source is the source of truth once it exists.

| Port | Responsibility | Used by |
|---|---|---|
| `CallWriter` | Insert a batch of call records and the tool-call, session and agent changes they imply, atomically and idempotently | `writer` |
| `CallQuery` | Filtered, keyset-paginated lists, call detail, session tree, cache-analysis pairs | `query`, `adminapi`, `live` (catch-up) |
| `StatsReader` | Usage, cost, latency and cache statistics, tool summaries, windowed numbers for alerts | `adminapi`, `alerts` |
| `ConfigStore` | Virtual keys, admin tokens, aliases and routes, pricing; change notifications; shared budget counter | `auth` and `route` caches (via the config cache), `adminapi`, `keys` subcommands |
| `LiveFeed` | Notify when calls are committed, on every replica | `live` |
| `AlertLog` | Append alerts with deduplication; read alerts after an id | `alerts`, `adminapi` |

```go
package store

// CallWriter takes batches from the writer. It owns the insert, the tool-call
// join and the session and agent updates, all in one atomic unit per batch.
type CallWriter interface {
	// WriteBatch is idempotent on CallRecord.ID: a retried batch inserts nothing twice.
	// It returns after commit. On error nothing from the batch is visible.
	WriteBatch(ctx context.Context, calls []domain.CallRecord) (WriteResult, error)
}

type WriteResult struct {
	Inserted  int // new call rows
	Duplicate int // ids already present
}

// CallQuery is the read side for lists and detail views.
type CallQuery interface {
	ListCalls(ctx context.Context, f CallFilter, p PageRequest) (Page[domain.CallSummary], error)
	GetCall(ctx context.Context, id uuid.UUID) (domain.CallDetail, error) // attempts, segments, tool calls
	ListSessions(ctx context.Context, f SessionFilter, p PageRequest) (Page[domain.SessionSummary], error)
	GetSession(ctx context.Context, key string) (domain.SessionDetail, error)
	SessionTree(ctx context.Context, key string) (domain.AgentTree, error) // agents, parent links, calls
	ListToolCalls(ctx context.Context, f ToolCallFilter, p PageRequest) (Page[domain.ToolCall], error)
	CacheAnalysis(ctx context.Context, sessionKey string) ([]domain.CachePair, error)
	// FinishedAfter returns calls committed after id that match f, oldest first.
	// It serves the Last-Event-ID catch-up of the live tail.
	FinishedAfter(ctx context.Context, id uuid.UUID, f CallFilter, limit int) ([]domain.CallSummary, error)
}

// CallFilter mirrors the call-log query parameters. Zero values mean "no constraint".
// Every query is time-bounded: From and To are required (the API defaults to the last 24 hours).
type CallFilter struct {
	From, To       time.Time // half-open [From, To)
	Keys           []string
	Clients        []string
	Providers      []string
	Models         []string // final model
	Aliases        []string
	Statuses       []int
	ErrorClasses   []string
	SessionKey     string
	AgentID        string
	AgentTypes     []string
	PromptID       string
	RequestClasses []string
	Tools          []string // calls that contain a tool call with this name
	ToolError      *bool
	Fallback       *bool
	MinLatency     time.Duration
	MinCost        decimal.Decimal
}

// SessionFilter and ToolCallFilter follow the same pattern: an explicit
// time range plus typed optional fields, no free-form query strings.

// PageRequest is keyset pagination on (ts, id), newest first.
type PageRequest struct {
	After  *Cursor // nil for the first page; items strictly older than the cursor
	Before *Cursor // set instead of After to page back toward newer items
	Limit  int     // the adapter clamps it to a maximum
}

type Cursor struct {
	TS time.Time
	ID uuid.UUID
}

type Page[T any] struct {
	Items       []T
	Next, Prev  *Cursor // nil at the end of the data
	ApproxCount *int64  // from rollups or a planner estimate; nil when unknown
}

// StatsReader serves aggregates. Time-bucketed numbers come from the adapter's
// cheapest source (rollups in store/postgres), so results are exact only to the
// grain of that source.
type StatsReader interface {
	Usage(ctx context.Context, q UsageQuery) ([]domain.UsageBucket, error) // tokens, cost, calls, errors
	Latency(ctx context.Context, q LatencyQuery) ([]domain.LatencyBucket, error) // p50, p95, p99 of TTFT and duration
	CacheStats(ctx context.Context, q CacheQuery) ([]domain.CacheBucket, error)  // hit ratio, read/write tokens
	ToolSummary(ctx context.Context, f ToolCallFilter) ([]domain.ToolStat, error) // count, error rate, p50, p95 duration
	// Window returns counts and sums over one recent window, for alert rules.
	Window(ctx context.Context, w WindowQuery) (domain.WindowStats, error)
}

type UsageQuery struct {
	From, To time.Time
	Grain    Grain     // Hour or Day
	GroupBy  []Dimension // key, client, provider, model, alias, agent_type, session ...
	Filter   CallFilter
}

// ConfigStore holds everything the admin API edits.
type ConfigStore interface {
	// Snapshot returns all keys, aliases, routes and pricing for the in-memory cache.
	Snapshot(ctx context.Context) (domain.ConfigSnapshot, error)
	// Changes signals that the config changed. A signal carries no payload; the
	// cache re-reads Snapshot. Signals may be coalesced and may be spurious.
	Changes(ctx context.Context) (<-chan struct{}, error)

	CreateKey(ctx context.Context, k domain.NewKey) (domain.KeyRecord, error) // stores the HMAC only
	RevokeKey(ctx context.Context, id string) error
	CreateAdminToken(ctx context.Context, t domain.NewAdminToken) (domain.AdminTokenRecord, error)
	LookupAdminToken(ctx context.Context, hmac []byte) (domain.AdminTokenRecord, error)
	PutAlias(ctx context.Context, a domain.Alias) error // alias with its ordered routes
	PutPricing(ctx context.Context, p domain.PricingVersion) error
	// AddSpend adds to the shared per-key, per-period budget counter (team tier).
	AddSpend(ctx context.Context, keyID string, period time.Time, cost decimal.Decimal) error
	Spend(ctx context.Context, keyID string, period time.Time) (decimal.Decimal, error)
}

// LiveFeed reports committed calls. It is the cross-replica live tail; the
// in-process broadcaster still carries in-flight call.started events.
type LiveFeed interface {
	// Subscribe delivers the ids of calls as they commit, from any replica.
	// Delivery is at-least-once and may skip under load (slow subscribers lose
	// events). Callers catch up with CallQuery.FinishedAfter.
	Subscribe(ctx context.Context) (<-chan domain.CallCommitted, error)
}

// AlertLog is the append-only alert feed.
type AlertLog interface {
	// Append stores the alert unless one with the same (key, type) exists within
	// the dedup window. It reports whether it stored the alert.
	Append(ctx context.Context, a domain.Alert) (stored bool, err error)
	Since(ctx context.Context, afterID int64, limit int) ([]domain.Alert, error)
}

// Maintainer lists the adapter's own housekeeping jobs (partitions, retention,
// rollups, idle-session close). Wiring runs them. Running one on several
// replicas at once must be safe.
type Maintainer interface {
	Jobs() []Job
}

type Job struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) error
}

// Store is what an adapter exports. Only cmd wiring uses this aggregate; every
// other package takes the one port it needs.
type Store interface {
	CallWriter
	CallQuery
	StatsReader
	ConfigStore
	LiveFeed
	AlertLog
	Maintainer
	Migrate(ctx context.Context) error // idempotent; backs `llm-proxy migrate up`
	MigrationStatus(ctx context.Context) (string, error)
	Ping(ctx context.Context) error // backs /readyz
	Close() error
}
```

Notes on the shapes:

- `WriteBatch` takes whole call records, including the tool halves, attempts and segment hashes. The caller does not know which tables exist.
- Filter and page types are explicit structs. The admin API maps query parameters onto them; the adapter maps them onto SQL. A new filter adds a field and a contract test case, so every adapter must support it.
- `Maintainer` and `Migrate` are not use-case ports. They are lifecycle hooks that exist because each adapter owns its physical layout.
- Retention settings are passed to the adapter's constructor from the config file. They are not a port method.

### Adapters

| Adapter | Package | Status | Purpose |
|---|---|---|---|
| Postgres | `internal/store/postgres` | **v1** | The production store. Postgres 16 or later |
| In-memory | `internal/store/memory` | v1, tests only | Maps and slices under a mutex. Used by unit tests of `writer`, `query`, `adminapi`, `live` and `alerts`, and as the reference behaviour in the contract suite. Never built into the release binary |
| SQLite | `internal/store/sqlite` | Possible later; **not v1** | Single-machine installs without a Postgres server |

**`store/postgres`** is the adapter this document set describes everywhere it talks about tables, partitions and SQL:

- **SQL and codegen.** Queries live in the adapter as `sqlc` input. Generated code stays in `internal/store/postgres`.
- **Migrations.** goose SQL files in `internal/store/postgres/migrations`, embedded with `go:embed`. `Migrate` applies them. The schema is the one in [Data model changes](#data-model-changes).
- **Write path.** `CopyFrom` into staging tables, then `INSERT … ON CONFLICT DO NOTHING`, then tool-call upserts and session and agent updates, in one transaction ([Batch writer](#batch-writer)).
- **Partitions and retention.** Daily range partitions on `ts` for the fact tables, created 7 days ahead and dropped past retention by a `Maintainer` job. Queries are always time-bounded so the planner prunes partitions.
- **Rollups.** A `Maintainer` job recomputes hour and day buckets. `StatsReader.Usage` reads them.
- **Live feed.** `NOTIFY llm_proxy_calls` after commit; `LiveFeed.Subscribe` runs `LISTEN` on a dedicated connection and reconnects on loss.
- **Config changes.** `NOTIFY llm_proxy_config`; `ConfigStore.Changes` listens for it.
- **Locks.** A Postgres advisory lock per job keeps jobs single-writer across replicas.
- **Percentiles.** `percentile_cont` over raw rows or rollup sketches.

**`store/memory`** implements every port with no persistence. Its notifications are channel sends and its percentiles are computed exactly in Go. It exists for fast tests and as an executable statement of the port semantics. It makes no performance promise.

**Possible later `store/sqlite` adapter.** A single-machine install could use SQLite instead of running Postgres. This is not v1 scope and has no design beyond this note. Adding it means writing a new adapter and running the contract suite against it; no consumer package changes. It would have to emulate these port features:

| Port feature | Postgres mechanism | What SQLite would need |
|---|---|---|
| `LiveFeed` and `ConfigStore.Changes` notifications | `LISTEN/NOTIFY` | In-process fan-out is enough for one process. Several processes on one file would need polling a change counter |
| Percentiles in `StatsReader` | `percentile_cont` | Compute from ordered rows or in Go; the contract tolerance must allow it |
| Partitions and retention | Daily partitions, `DROP PARTITION` | `DELETE` by time range in small batches, or one database file per month that is dropped whole |
| Concurrent writers | MVCC | One writer connection; the bounded queue already serialises writes |
| Idempotent batch insert | `ON CONFLICT DO NOTHING` | `INSERT OR IGNORE`, supported |
| Advisory locks for jobs | `pg_advisory_lock` | Not needed with one process |
| `jsonb`, `timestamptz`, `uuid` | Native | Stored as text and integer; the adapter converts |

The multi-replica tier needs a shared server, so it stays Postgres-only.

### Contract test suite

One suite, `internal/store/storetest`, is the definition of correct behaviour. Every adapter, in-memory and Postgres in v1, must pass it unchanged. It exposes `storetest.Run(t, newStore func(t *testing.T) store.Store)`; each adapter's test file calls it with its own constructor. The Postgres run uses a throwaway database (`LLM_PROXY_TEST_DATABASE_URL`, or a container started by the test). CI runs both adapters.

The suite covers:

- **Write.** A batch inserts exactly once; replaying a batch inserts nothing; a failed batch leaves nothing visible.
- **Tool-call join.** A use then its result resolves the row; a result before its use is held as `result_only` and merges later; a use with no result becomes `use_only` after the idle-close job; resolving twice changes nothing.
- **Sessions and agents.** A derived session key is stable within the idle timeout and changes after it; sub-agent links are recorded with confidence.
- **Pagination.** Ordering by `(ts, id)` is total, including calls with equal `ts`; forward and backward cursors return no duplicates and no gaps while rows are inserted; the time bound is honoured.
- **Filters.** Each `CallFilter` field, alone and combined.
- **Detail and tree.** `GetCall` returns attempts, segments and tool calls; `SessionTree` returns the same parent and child structure from every adapter.
- **Stats.** `Usage` sums disjoint token columns and cost correctly for each `GroupBy` and `Grain`, and absorbs rows that arrive late. Percentiles agree with the exact value within the documented tolerance.
- **Live feed.** A subscriber receives the ids of committed calls and never the ids of a rolled-back batch.
- **Config.** Keys store only the HMAC; revocation and alias edits raise a change signal; `Snapshot` after the signal shows the edit; budget counters add up under concurrent `AddSpend`.
- **Alerts.** Dedup inside the window; ids increase; `Since` returns only newer alerts.
- **Maintenance.** With an injected clock, a retention job removes data past retention and nothing else; running a job twice, or on two store instances at once, is safe.
- **No bodies.** The suite writes records, then scans every stored value it can reach (via a store-specific hook) and fails if any text from the test's fake prompt, response or tool arguments is present. This holds the no-bodies model at the storage layer.

A new filter, port method or behaviour starts as a suite case. An adapter is not accepted until it passes.

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
    participant X as Extractor
    participant Q as Call queue
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
        P->>P: scan for facts, reset idle watchdog
        P-->>C: same bytes, flushed immediately
    end
    P->>X: Close: exchange finished
    X->>X: build call record: usage, stop reason, tool halves, segment hashes
    X->>X: discard buffers
    X->>X: add cost to key budget counter
    X->>L: call.finished
    X->>Q: non-blocking send (drop and count if full)
```

A client abort follows the same path from the abort onward: the context cancels upstream, `Close` fires, and the call is logged with the facts extracted so far and `error_class = client_abort`.

## Sequence: extraction and write

```mermaid
sequenceDiagram
    autonumber
    participant X as Extractor
    participant Q as Call queue
    participant W as Batch writer
    participant CW as CallWriter port
    participant PG as store/postgres and Postgres
    participant N as Admin planes (LiveFeed)

    X->>X: facts from request and response, in memory
    X->>Q: call record (non-blocking; drop and count if full)
    loop batch full or interval elapsed
        Q->>W: up to batch_rows call records
        W->>CW: WriteBatch(calls)
        CW->>PG: BEGIN, COPY into staging tables
        PG->>PG: INSERT ... ON CONFLICT DO NOTHING into requests, request_attempts, request_segments
        PG->>PG: upsert tool_calls: insert pending, resolve earlier ones by (session_key, tool_call_id)
        PG->>PG: derive sessions when no header, upsert sessions and agents, COMMIT
        PG->>N: NOTIFY llm_proxy_calls (multi-replica live tail)
        PG-->>W: WriteResult
    end
```

The steps inside `store/postgres` are that adapter's implementation of `WriteBatch`; another adapter would do the same work its own way. If the store is down, `WriteBatch` fails, the writer retries with backoff and the queue fills. Records that arrive when it is full are dropped and counted. Nothing on the data plane notices. A process crash loses the records still in the queue.

## Data model changes

This section describes the schema of the `store/postgres` adapter. It is the adapter's private detail: nothing outside `internal/store/postgres` knows these table names, and another adapter may lay data out differently as long as it passes the [contract suite](#contract-test-suite). The design doc's section 4 is the starting point. This architecture needs the following changes. Column lists are indicative; the migrations are the source of truth once they exist.

| Change | Reason |
|---|---|
| `requests` primary key becomes `(id, ts)`, ids are UUIDv7 | The design doc's `id` primary key fails on a table partitioned by `ts` (gap review B1) |
| `request_attempts` and `request_segments` gain `ts` and are partitioned like `requests` | Retention by `DROP PARTITION` (B1) |
| Rollup dimensions use a sentinel `'unknown'` instead of NULL | NULL breaks the rollup primary key (B1) |
| `requests` gains `session_key text`, `agent_id`, `parent_agent_id`, `agent_type`, `prompt_id`, `request_class`, `compaction`, `client_raw`, `client_request_id`, `stop_reason`, `error_type`, `usage_estimated`, `extraction_partial` | Correlation filters (G1) and the outcome facts the extractor reads |
| Usage columns become disjoint: `input_uncached`, `cache_read`, `cache_write_5m`, `cache_write_1h`, `output` (including reasoning), `reasoning` (informational), plus `usage_raw jsonb` | One meaning per column; cost is the sum of column times price; raw kept for recomputation (B6) |
| `request_attempts` gains `upstream_request_id` and usage columns | Vendor support tickets (R17); billed tokens on failed attempts (B6) |
| New `providers` view of the config file, `admin_tokens`, `alerts` | B8, R8, R9 |
| New `agents(id, session_key, parent_agent_id, agent_type, spawned_by_tool_call_id, link_confidence, first_ts, last_ts)` | Session tree (G2) |
| New `tool_calls` (below) | Tool-call explorer and filters (G6) |
| New indexes: `requests (ts DESC, id DESC)`; `requests (ts DESC) WHERE error_class IS NOT NULL`; `requests (agent_id, ts) WHERE agent_id IS NOT NULL`; `requests (prompt_id)` | Keyset pagination and the measured filters (G8, G9, UI report 6.4) |
| `usage_rollups` gains `session_key`, `agent_type`, `alias`, `provider`, `fallbacks`, `reasoning` dimensions or a second rollup by session | Cost views and the call-log summary strip (G7, R11) |
| `request_bodies` and `virtual_keys.log_bodies` are removed | Bodies are never stored. There is no body table, no pointer and no searchable text |

Sketch of the new fact table, partitioned by day. It holds metadata only:

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
  args_bytes            int,                    -- size of the arguments; the text is not kept
  result_bytes          int,                    -- size of the result; the text is not kept
  is_error              boolean,
  duration_ms           int,
  duration_source       text,                   -- 'header' | 'estimated'
  status                text NOT NULL,          -- 'pending' | 'resolved' | 'use_only' | 'result_only'
  PRIMARY KEY (id, ts)
) PARTITION BY RANGE (ts);
CREATE INDEX ON tool_calls (tool_name, ts DESC);
CREATE INDEX ON tool_calls (session_key, tool_call_id);
```

Retention defaults: `requests` and `request_attempts` 400 days, `request_segments` 14 days, `tool_calls` 400 days. Deployment sizing is in [deployment.md](deployment.md#storage-retention-and-sizing).

## Package layout

```
llm-proxy/
├── cmd/
│   └── llm-proxy/              main: subcommand dispatch (serve, migrate, keys)
├── internal/
│   ├── config/                 YAML + env loading, secret references, validation
│   ├── server/                 listeners, mux, middleware chain, graceful shutdown
│   ├── domain/                 call record, filters, pages, cursors, config and alert types; pure Go,
│   │                           no I/O, no driver imports
│   ├── auth/                   virtual keys, admin tokens, HMAC, limits, key lookup
│   ├── configcache/            in-memory snapshot of keys, aliases, routes, pricing; fed by ConfigStore
│   ├── budget/                 per-key counters (in-process; shared counter via ConfigStore in team tier)
│   ├── correlate/              harness header extraction, client normalization
│   ├── route/                  aliases, routes, provider registry, model splice
│   ├── proxy/                  ReverseProxy setup, Rewrite, retrying RoundTripper, commit point,
│   │                           include_usage inject and strip, protocol-correct errors
│   ├── tap/                    stream tap, idle watchdog, exchange assembly
│   ├── extract/                call-record extraction (the only package that decodes provider formats)
│   │   ├── anthropic/          Messages request and SSE scanning, usage
│   │   ├── openai/             chat completions and Responses scanning, usage
│   │   └── segments/           canonical segment hashing
│   ├── writer/                 bounded queue, batching, retry, drop counting, shutdown drain
│   ├── store/                  the ports only: CallWriter, CallQuery, StatsReader, ConfigStore,
│   │   │                       LiveFeed, AlertLog, Maintainer, Store; filter and page types
│   │   ├── postgres/           v1 adapter: pgx pool, sqlc queries, goose migrations, partitions,
│   │   │   └── migrations/     rollups, LISTEN/NOTIFY; the only package that knows the schema
│   │   ├── memory/             in-memory adapter for tests
│   │   └── storetest/          contract suite run against every adapter
│   ├── query/                  read-side logic over CallQuery and StatsReader: filter parsing,
│   │                           cursor encoding, approx counts
│   ├── adminapi/               HTTP handlers, OpenAPI document, browser session cookies
│   ├── live/                   in-process broadcaster, SSE writer, relay of LiveFeed events
│   ├── alerts/                 evaluator and rules (StatsReader in, AlertLog out)
│   ├── otelexport/             GenAI span mapping, pinned semantic-convention version
│   ├── metrics/                Prometheus collectors for proxy internals
│   └── ui/                     //go:embed all:dist, SPA handler (see frontend.md)
├── api/
│   └── openapi.yaml            admin API contract, source for the UI's TypeScript types
├── web/                        React + Vite source (see frontend.md)
├── testdata/
│   └── fixtures/               recorded SSE exchanges per provider, golden call-record outputs
└── docs/
```

Dependency rules:

- `domain` imports nothing from this module and no driver.
- The ports in `store` import only `domain` and the standard library.
- `proxy`, `tap`, `auth`, `route` and `correlate` (the hot path) import no `store` package, ports included, and never import `query` or `writer`. They see configuration only through small lookup interfaces they define themselves, which `configcache` satisfies and `cmd` wires in. They never import `configcache` directly.
- `writer`, `query`, `adminapi`, `live`, `alerts` and `configcache` depend only on the `store` ports and `domain`, each on the ports it needs. None imports `store/postgres`, `store/memory` or any database driver.
- **Only `cmd/llm-proxy` (the wiring) imports `store/postgres`.** It builds the adapter from config, passes it to each consumer as the port it needs, and runs `Maintainer` jobs. Tests wire `store/memory` the same way.
- `store/postgres` and `store/memory` import `domain` and `store`. They never import each other and never import consumer packages.
- `extract` is the only package that decodes provider body formats. It returns call records and keeps no bytes.
- `writer` is the only caller of `CallWriter`. Nothing else writes call or tool-call records. `adminapi` writes only through `ConfigStore` and `AlertLog`.
- An import check in CI (for example `go list -deps` or a `depguard` rule) enforces the rules above.

Later executables from design doc 9 (the firstmate alert adapter, the ledger reporter) go under `cmd/` in the same module (gap review N9).

## Libraries

| Concern | Choice |
|---|---|
| HTTP, proxying, SSE | `net/http`, `net/http/httputil` (no fasthttp: no HTTP/2 client, different streaming model) |
| Hot-path JSON | `github.com/tidwall/gjson` to read, `github.com/tidwall/sjson` to splice |
| Postgres (inside the `store/postgres` adapter only) | `github.com/jackc/pgx/v5`, `sqlc`, goose migrations |
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
| B5 queue | In-process bounded channel feeding a batch writer; idempotent insert on the call id; drops counted; no durable buffer, so a crash loses the queued records (decision D2, resolved) |
| B6 usage | Disjoint columns plus `usage_raw`; inclusive values derived at export; `include_usage` injected for OpenAI streams |
| B7 budgets | Soft enforcement from an in-process counter; Postgres counter row in the team tier; UTC calendar periods |
| B8 providers | Config-file providers with credential references (D7) |
| B10 Go baseline | As in [Libraries](#libraries); one static binary with subcommands; distroless image |
| R1 keys | `lp_` keys, HMAC-SHA256 with pepper, change notification (Postgres NOTIFY) plus 30 s TTL cache |
| R8 admin auth | Hashed admin tokens with `admin` and `read` scopes; browser session cookie |
| R10 retention | See [Data model changes](#data-model-changes) and [deployment.md](deployment.md#storage-retention-and-sizing) |
| R14 multi-replica | v1 targets one replica; team tier described in [deployment.md](deployment.md#topology-b-multi-replica-team-or-production); drain timeout 10 minutes |
| R15 targets | Added p99 overhead under 5 ms before the first byte; 200 concurrent streams on one small VM; 32 MiB maximum body |
| R16 self-observability | `/healthz`, `/readyz`, `/metrics`, OTLP spans with content off, `traceparent` propagated |
