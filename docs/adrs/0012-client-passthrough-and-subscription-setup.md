# ADR 0012: Client passthrough mode and subscription setup

Status: Accepted

Date: 2026-10-21

## Context

Decision D6: Claude Code signed in with a Claude subscription can route through the proxy. Anthropic documents the setup (`ANTHROPIC_BASE_URL` without a gateway credential variable, the gateway forwarding `Authorization` and the OAuth capability in `anthropic-beta`). Anthropic's terms forbid collecting, storing or intermediating claude.ai credentials, and assume ordinary individual use. The captain chose to support it as an opt-in personal local setup, off by default.

## Decision

- `auth_mode` is a per-provider setting: `proxy_key` (default), `client_passthrough`, or `both`.
- In `client_passthrough` mode the client's `Authorization` and the full `anthropic-beta` header are forwarded unchanged. The virtual key travels in `x-llm-proxy-key` and is checked against the `keys` section. Without a valid key the request gets `401`.
- The proxy sends passthrough traffic only to the provider the credential belongs to (`api.anthropic.com`).
- The proxy **must not** store the OAuth token anywhere (not in the store, logs or a hash), hold or inject a subscription token, offer or capture a sign-in, share one login among several people, rewrite the model, or fall back to a proxy-held key.
- The call record carries `auth_mode = oauth_passthrough` and `billing_mode = subscription` and nothing else about the credential. Cost is an API-equivalent figure for analysis.
- Documented as a personal local setup: proxy on `127.0.0.1`, one login per person, never shared. Heavy or shared use goes through `proxy_key` with an API key.
- Settings live in `~/.claude/settings.json` (`ANTHROPIC_BASE_URL`, `ANTHROPIC_CUSTOM_HEADERS` with `x-llm-proxy-key`, optional `CLAUDE_CODE_GATEWAY_HINT_HEADERS`), never in a committed project file. `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY` and `apiKeyHelper` must stay unset.

## Consequences

- The subscription's usage limits apply, and a fleet of sessions on one login uses them quickly.
- Remote Control is disabled behind a non-Anthropic base URL; gateway sessions are not eligible for the HIPAA configuration.
- Passthrough in a shared deployment cannot be verified by the proxy, so the default configuration disables it.

## Work required

- Add `auth_mode` handling to the config schema and validation (ADR 0004), defaulting to `proxy_key`.
- Implement the mode switch in `Rewrite` (ADR 0011): credential replacement versus unchanged forwarding, `x-llm-proxy-key` read and removal, and `both` mode selection by the presence of `x-llm-proxy-key`.
- Ensure the log redactor and the extractor never read or keep the passthrough credential; add a test that scans logs, stored rows and metrics for a marker token.
- Record `auth_mode` and `billing_mode` on the call record and apply the subscription label in the cost step (ADR 0015).
- Forward `anthropic-ratelimit-unified-*` response headers and verify with a test.
- Write the personal local setup guide (config, `settings.json`, `/login`, rules and limits) as part of ADR 0037.
- Tests: a request with a valid key and a client credential is forwarded with the credential unchanged; a request without `x-llm-proxy-key` is rejected; the key header never reaches the upstream.

## Depends on

- ADR 0004, ADR 0008, ADR 0011.

## References

- architecture/upstream-connection.md, "Credential modes", "Claude Code with a Claude subscription".
- architecture/deployment.md, "Personal local setup (subscription passthrough)".
- architecture/README.md, D6.
