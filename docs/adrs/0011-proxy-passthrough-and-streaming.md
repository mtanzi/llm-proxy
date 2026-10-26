# ADR 0011: Proxy passthrough and streaming

Status: Accepted

Date: 2026-10-21

## Context

The hot path forwards each request to the one configured Anthropic provider and streams the response back without buffering. The approaches report verified that `httputil.ReverseProxy` flushes `text/event-stream` immediately, propagates cancellation, closes bodies on client abort, and adds no measurable latency. The body must be forwarded exactly as sent so prompt caching and the system-prompt attribution strip keep working.

## Decision

- Built on `net/http/httputil.ReverseProxy`. No custom `Upstream` port.
- **One body read:** the handler reads the whole request body once, bounded by `MaxBytesReader`, before forwarding. That slice goes upstream and then to the extractor. There is no tee and no second buffer.
- **`Rewrite`** sets the upstream URL from the configured provider, applies the credential mode (in `proxy_key` mode replace the client credential with `x-api-key: <provider key>`), and removes `x-llm-proxy-key`. A presented `lp_` key is never forwarded. All other headers and body bytes are forwarded unchanged. `anthropic-version`, `anthropic-beta` and every other `anthropic-*` header are open lists.
- **`Transport`** is a plain `http.Transport`, one per provider, with default pool and HTTP/2 settings. Timeouts: a dialer timeout and `ResponseHeaderTimeout`. No retry, no fallback, no body edit.
- Response headers pass through (`content-type`, `retry-after`, `x-should-retry`, `anthropic-ratelimit-unified-*`). The upstream `request-id` is stored as `upstream_request_id`. `x-request-id` is added.
- SSE `ping` events are never stripped.
- **Client abort:** the upstream request uses the client's context, so an abort cancels upstream. The call is still logged (ADR 0013, ADR 0014).
- **Local endpoints:** `HEAD /api/hello` is answered locally with `200` and not logged as a call. `GET /v1/models` and `POST /v1/messages/count_tokens` are forwarded. Paths match without the query string (`/v1/messages?beta=true`).
- Model name is forwarded as sent. No aliases, no rewriting.
- Unsupported paths return a protocol-correct 404.

## Consequences

- Upstream 4xx and 5xx reach the client unchanged; the client applies its own retries.
- Failover, aliases and body edits are later versions and need their own design.
- `client_passthrough` mode adds rules on top of this flow (ADR 0012).

## Work required

- Implement the `proxy` handler: request id and body read, auth call (ADR 0008), correlation (ADR 0010), `ReverseProxy` setup with `Rewrite`, `ModifyResponse` hook installing the tap (ADR 0013), error handler using ADR 0007 builders.
- Implement the per-provider `Transport` factory from the config snapshot, with a transport swap when a reload changes a provider.
- Implement the local `HEAD /api/hello` handler and the path allowlist.
- Define the small interfaces `proxy` needs (`KeyLookup`, `Recorder`) and keep `proxy` free of `store` imports.
- Tests with an `httptest.Server` as upstream: bytes and headers forwarded unchanged, streaming flushes per chunk, client abort cancels upstream, upstream 429 and 5xx pass through unchanged with `x-request-id` added, `x-llm-proxy-key` never reaches the upstream, an `lp_` key never reaches the upstream in any header, `anthropic-beta` preserved.

## Depends on

- ADR 0005, ADR 0006, ADR 0007, ADR 0008, ADR 0010.

## References

- architecture/backend.md, "Proxy handler", "Forwarding and the config snapshot", "Proxy passthrough and streaming", "Sequence: a streaming request".
- architecture/upstream-connection.md, "Protocol family and endpoints", "Header handling".
