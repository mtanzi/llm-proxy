# ADR 0050: Upstream identity encoding

Status: Completed

Date: 2026-11-02

## Context

`ReverseProxy` forwarded the client's `Accept-Encoding` unchanged (ADR 0011). Anthropic SDK clients send `accept-encoding: gzip, deflate, br`. If the upstream compressed a response, the tap could not scan it: it passed the bytes through and set `extraction_partial` (ADR 0013). Usage, stop reason and tool facts were lost, and cost stayed null.

ADR 0013 also says the idle watchdog cancels the upstream request before the response headers "for every request". The landed code arms the watchdog before headers only for a streaming request. A non-streaming request sends no header until the model has finished, so `Transport.ResponseHeaderTimeout` alone bounds that wait.

This ADR amends Completed ADRs 0011 and 0013. Neither is edited (ADR 0049).

## Decision

- **Amends ADR 0011, `Rewrite` and header forwarding.** `Rewrite` sets `Accept-Encoding: identity` on the outbound request, in both credential modes, whatever the client sent. The client's own `Accept-Encoding` value is not forwarded. All other headers are forwarded unchanged as before.
- The tap scans every response. Its handling of a non-identity `Content-Encoding` stays as a fallback for an upstream that compresses anyway: the body passes through unchanged and the record gets `extraction_partial = true`.
- The proxy does not decompress and adds no dependency.
- **Amends ADR 0013, watchdog wording.** Before the response headers, the idle watchdog watches a streaming request only (`"stream": true`). For that request it cancels the upstream request and the proxy sends its own 504. A non-streaming request is bounded before headers by `Transport.ResponseHeaderTimeout` alone. After the headers the behaviour is as ADR 0013 states.

## Consequences

- Usage, stop reason, tool facts and cost are recorded for every response from an upstream that honours `identity`.
- Proxy-to-client traffic is uncompressed. The cost is local traffic, because the client talks to the proxy on a local or private link. Proxy-to-upstream traffic is also uncompressed.
- A client that decodes the response itself is unaffected: the response has no `Content-Encoding`, so it has nothing to decode.
- The ADR 0013 text about the watchdog stays as written. This ADR is the correction.

## Work required

- Set `Accept-Encoding: identity` in `rewrite` (`internal/proxy/handler.go`).
- Tests: the outbound request carries `Accept-Encoding: identity` whatever the client sends (gzip list, empty, identity); a gzip-encoded upstream response still passes through and is marked partial.
- Update `backend.md` and `upstream-connection.md` where they describe header forwarding.

## Depends on

- ADR 0011, ADR 0013.

## References

- adrs/0011-proxy-passthrough-and-streaming.md, "Decision".
- adrs/0013-tap-incremental-response-scanner.md, "Decision".
- architecture/backend.md, "Proxy passthrough and streaming", "Tap".
- architecture/upstream-connection.md, "Header handling".
