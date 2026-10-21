# ADR 0006: HTTP listeners, timeouts and request lifecycle

Status: Accepted

Date: 2026-10-21

## Context

The binary has two listeners so the data plane and the admin plane can be exposed separately. The data plane carries long SSE streams, and a global write timeout would cut them. The admin plane has no sign-in in v1 (decision C7), so its safety rests on binding to loopback.

## Decision

- Data plane on `:8080`: `/v1/*`, `HEAD /api/hello`, `/healthz`, `/readyz`. Admin plane on `127.0.0.1:9090`: `/admin/*`, `/ui/*`, `/metrics`, `/healthz`, `/readyz`.
- `net/http` with the standard `ServeMux` and method patterns (Go 1.22 and later). No framework.
- Server timeouts: set `ReadHeaderTimeout` and `IdleTimeout`. Never set a global `WriteTimeout`. A streaming request clears its own deadline with `http.NewResponseController(w).SetWriteDeadline(time.Time{})`.
- One request-size limit: `server.max_request_bytes`, default 32 MiB, enforced with `http.MaxBytesReader` before the tap sees the body.
- Every response carries `x-request-id`, a UUIDv7 that is also the call id. A client-supplied `x-request-id` is kept as `client_request_id`, never reused.
- The handler chain is plain function calls: recover, request id, access log, auth, correlation. The access log uses `log/slog` with header redaction in one place.
- `serve` refuses to start if `listen.admin` is not a loopback address (`127.0.0.1`, `::1`, `localhost`). The admin plane has no auth and no CORS headers.
- Optional `listen.tls` serves TLS directly; usually TLS ends in front of the binary.

## Consequences

- Anyone who can reach the loopback interface can read the admin API and UI, so the host must be trusted. Remote access uses a tunnel such as `ssh -L`.
- A body past the limit is rejected before extraction, so there is no second extraction-side limit.
- Logs never contain bodies or credentials.

## Work required

- Build the two `http.Server` instances with the timeouts above and the per-listener muxes in `cmd` wiring helpers (the mux construction lives with each plane's package).
- Implement the request-id middleware (UUIDv7, client id kept separately) and the recover middleware that returns a protocol-correct error (ADR 0007).
- Implement the redacting access logger: one `slog` line per request with the call id, never a body, never `Authorization`, `x-api-key` or `x-llm-proxy-key` values.
- Implement `MaxBytesReader` handling with a protocol-correct `413` body.
- Implement the loopback check for `listen.admin` as a config validation step (ADR 0004) and a startup assertion in `serve`.
- Implement optional TLS from `listen.tls`.
- Tests: a streaming response longer than any server timeout completes; an oversize body is rejected; a non-loopback admin address aborts startup; redaction golden test.

## Depends on

- ADR 0004, ADR 0003.

## References

- architecture/backend.md, "Shape", "Listener and router".
- architecture/deployment.md, "TLS and network exposure".
- architecture/README.md, C7.
