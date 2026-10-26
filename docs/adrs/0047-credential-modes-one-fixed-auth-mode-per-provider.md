# ADR 0047: Credential modes: one fixed auth mode per provider

Status: Accepted

Date: 2026-10-26

## Context

A coherence review of the design document, the architecture documents and the ADRs found two ideas with several names and one rule with two statements.

- The credential mode has the config names `proxy_key`, `client_passthrough` and `both`, and the call record names it `oauth_passthrough`. A `billing_mode` field repeated what the mode already says.
- Completed ADR 0004 validates `auth_mode` and the code accepts `both`. Completed ADR 0008 says where the key is read from per mode, but it does not say what happens to a credential in the wrong place. A virtual key in the wrong header could reach the upstream.
- The `core` comment on `CallRecord.AuthMode` names `oauth_passthrough`.

This ADR amends Completed ADR 0004 (the `auth_mode` validation) and Completed ADR 0008 (where the key is read from). Neither is edited. ADR 0012 (Accepted) is edited in place to match.

## Decision

- Each provider has one fixed `auth_mode`: `proxy_key` or `client_passthrough`. There is no `both` mode, and no per-request mode selection by the presence of `x-llm-proxy-key`.
- One name per idea. The call record stores the mode actually used for the request, with the config names `proxy_key` or `client_passthrough`. `oauth_passthrough` is gone everywhere: docs, schema and the `core` comment.
- `billing_mode` is removed. The UI shows the "API-equivalent" cost label when `auth_mode = client_passthrough`.
- One header check. A presented `lp_` virtual key is never forwarded upstream. A request that carries a credential in the wrong place for the provider's mode is rejected with `401`:
  - for a `client_passthrough` provider: an `lp_` value in `Authorization` or `x-api-key`;
  - for a `proxy_key` provider: an `x-llm-proxy-key` header.
- The v1 firstmate route is `proxy_key` with a Console API key for crewmates. `client_passthrough` stays for one person's own interactive Claude Code.

## Consequences

- A virtual key cannot leak to the provider through a misplaced header.
- A deployment that needs both modes runs two providers, once a second provider exists. v1 has one provider.
- A config that sets `auth_mode: both` stops loading. No released config uses it.
- The cost step needs no label. The UI derives the label from `auth_mode`.

## Work required

- Remove `config.AuthBoth` and its case in `internal/config/validate.go`. Validation accepts `proxy_key` and `client_passthrough` only, and its error message lists those two.
- Change the `auth_mode` value in `internal/config/testdata/valid.view.json` and the `AuthBoth` assertion in `internal/config/config_test.go`. Add a test that `both` is rejected.
- Change the `core.CallRecord.AuthMode` comment in `internal/core/call.go` to "proxy_key or client_passthrough".
- Add the wrong-place `401` check to the `proxy` auth step (ADRs 0008, 0011, 0012), with tests for each of the two rejected cases and for a passthrough request that carries a valid `x-llm-proxy-key`.

## Depends on

- ADR 0004, ADR 0008, ADR 0012.

## References

- architecture/upstream-connection.md, "Credential modes".
- architecture/backend.md, "Auth (virtual keys)".
- architecture/README.md, D6 and C8.
