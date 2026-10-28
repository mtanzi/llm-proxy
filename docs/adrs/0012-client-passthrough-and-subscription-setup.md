# ADR 0012: Client passthrough mode and subscription setup

Status: Completed

Date: 2026-10-21

## Context

Decision D6: Claude Code signed in with a Claude subscription can route through the proxy. Anthropic documents the setup (`ANTHROPIC_BASE_URL` without a gateway credential variable, the gateway forwarding `Authorization` and the OAuth capability in `anthropic-beta`). Anthropic's terms forbid collecting, storing or intermediating claude.ai credentials, and assume ordinary individual use. The captain chose to support it as an opt-in personal local setup, off by default.

## Decision

- `auth_mode` is a per-provider setting with one fixed value: `proxy_key` (default) or `client_passthrough`. There is no `both` mode.
- In `client_passthrough` mode the client's `Authorization` and the full `anthropic-beta` header are forwarded unchanged. The virtual key travels in `x-llm-proxy-key` and is checked against the `keys` section. Without a valid key the request gets `401`.
- A presented `lp_` key is never forwarded upstream. A request that carries a credential in the wrong place for the provider's mode gets `401`: for a `client_passthrough` provider, an `lp_` value in `Authorization` or `x-api-key`; for a `proxy_key` provider, an `x-llm-proxy-key` header.
- The proxy sends passthrough traffic only to the provider the credential belongs to (`api.anthropic.com`).
- The proxy **must not** store the OAuth token anywhere (not in the store, logs or a hash), hold or inject a subscription token, offer or capture a sign-in, share one login among several people, rewrite the model, or fall back to a proxy-held key.
- The call record carries `auth_mode`, the mode actually used for the request, as `proxy_key` or `client_passthrough`, and nothing else about the credential. In passthrough mode the cost is an API-equivalent figure for analysis, and the UI shows the "API-equivalent" label when `auth_mode = client_passthrough`.
- Documented as a personal local setup: proxy on `127.0.0.1`, one login per person, never shared. Heavy or shared use goes through `proxy_key` with an API key.
- Settings live in `~/.claude/settings.json` (`ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` with `x-llm-proxy-key`, optional `CLAUDE_CODE_GATEWAY_HINT_HEADERS`), never in a committed project file. `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` and `apiKeyHelper` must stay unset.

## Consequences

- The subscription's usage limits apply, and a fleet of sessions on one login uses them quickly.
- Remote Control is disabled behind a non-Anthropic base URL; gateway sessions are not eligible for the HIPAA configuration.
- Passthrough in a shared deployment cannot be verified by the proxy, so the default configuration disables it.

## Work required

- Add `auth_mode` handling to the config schema and validation (ADR 0004), defaulting to `proxy_key`. The two values are the only valid ones. This amends Completed ADRs 0004 and 0008; the code change is listed in ADR 0047.
- Implement the mode switch in `Rewrite` (ADR 0011): credential replacement versus unchanged forwarding, `x-llm-proxy-key` read and removal, and the wrong-place `401` check.
- Ensure the log redactor and the extractor never read or keep the passthrough credential; add a test that scans logs, stored rows and metrics for a marker token.
- Record `auth_mode` on the call record. The cost step (ADR 0015) needs no label; the UI derives the "API-equivalent" label from `auth_mode`.
- Forward `anthropic-ratelimit-unified-*` response headers and verify with a test.
- Write the personal local setup guide (config, `settings.json`, `/login`, rules and limits) as part of ADR 0037.
- Tests: a request with a valid key and a client credential is forwarded with the credential unchanged; a request without `x-llm-proxy-key` is rejected; the key header never reaches the upstream; an `lp_` value in `Authorization` or `x-api-key` on a passthrough provider is rejected with `401`; an `x-llm-proxy-key` header on a `proxy_key` provider is rejected with `401`.

## Depends on

- ADR 0004, ADR 0008, ADR 0011.

## References

- architecture/upstream-connection.md, "Credential modes", "Claude Code with a Claude subscription".
- architecture/deployment.md, "Personal local setup (subscription passthrough)".
- architecture/README.md, D6 and C8.
- ADR 0047, which records the credential-mode amendments to Completed ADRs 0004 and 0008.
