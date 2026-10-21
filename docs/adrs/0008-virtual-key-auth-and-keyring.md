# ADR 0008: Virtual-key auth and the keyring

Status: Accepted

Date: 2026-10-21

## Context

Virtual keys keep the real provider key off clients and make calls attributable (R3). Decision D15 moved them from the store into the config file as names and SHA-256 hashes. Keys are 256-bit random values, so a plain hash is enough and no pepper is needed. The hot path must not read the store.

## Decision

- Key format: `lp_<base62 of 32 random bytes>`. The proxy treats the presented key as an opaque string and hashes it.
- A config entry has a unique `name`, the lowercase hex `sha256` and an optional `disabled` flag. The plaintext is never stored. Names stay stable because call records store the name.
- Package `keyring` builds an immutable map from SHA-256 to key from the keys of a config snapshot. The map travels with the snapshot.
- Where the key is read from: in `proxy_key` mode, `Authorization: Bearer lp_…` or `x-api-key: lp_…`. In `client_passthrough` mode the key is in `x-llm-proxy-key` (ADR 0012). The proxy consumes `x-llm-proxy-key` and never forwards it.
- Checks: the key exists and is not disabled. A failure returns a protocol-correct 401 (ADR 0007).
- Expiry, request-rate and concurrency limits and budgets are not in v1.
- The admin plane has no auth in v1 (ADR 0006).

## Consequences

- No self-service key creation in the UI. Per-key usage comes from the call log grouped by key name.
- Changing keys is a config edit plus `SIGHUP`. Rotate: add the new entry, switch the client, disable the old entry.
- A leaked key can spend without limit until it is disabled and the proxy reloaded. Mitigation is the provider console's spend limit.

## Work required

- Implement `keyring.New(keys []core.Key)` with duplicate-name, duplicate-hash and malformed-hash rejection, returning an immutable lookup.
- Implement the credential extraction step in `proxy` for the `Authorization` and `x-api-key` forms, with constant-time-agnostic hash lookup (the hash is of a high-entropy key, so a map lookup is fine).
- Define the small `KeyLookup` interface in `proxy` and a fake for its tests.
- Return the key name to the correlation step so it lands on the call record as `key_name`.
- Tests: lookup of a known key, an unknown key, a disabled key, a missing credential, both header forms, and config-load rejection cases.
- Wire the keyring into the config snapshot so `SIGHUP` swaps it atomically (ADR 0005).

## Depends on

- ADR 0003, ADR 0005, ADR 0007.

## References

- architecture/backend.md, "Auth (virtual keys)".
- architecture/deployment.md, "Secrets".
- architecture/README.md, D15.
