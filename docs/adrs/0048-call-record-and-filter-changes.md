# ADR 0048: Call record and filter changes

Status: Accepted

Date: 2026-10-26

## Context

The coherence review found three gaps between the documents and `core`, and one feature that v1 does not need.

- The UI draws a compaction marker where the cache prefix resets. The call record stores `compaction`, but the marker needs `context_compacted`, which Claude Code sends in `x-claude-code-context-compacted`. No column holds it.
- The v1 call-log filter menu lists endpoint and streamed. `core.CallFilter` has no field for either. The menu also listed TTFT, tokens and cache hit, which no filter field serves.
- Completed ADR 0003 puts a `SessionKey` derivation in `core`: a hash of (key, client, hash of tools and system prompt, hash of the first user message) for a request without a session header. Claude Code always sends `x-claude-code-session-id`, so v1 does not need it. Merging header-less clients into one session was an accepted cost of a feature nobody uses.

This ADR amends Completed ADR 0003. It is not edited. ADR 0010 and ADR 0014 (Accepted) are edited in place.

## Decision

- The call record and the `calls` table gain `context_compacted boolean`, read from `x-claude-code-context-compacted`. The UI compaction marker uses it.
- `core.CallFilter` gains `Endpoints []string` and `Streamed *bool`, with `Match` cases, filter-builder branches and contract cases. The v1 call-log filter menu drops TTFT, tokens and cache hit.
- There is no session derivation in v1. A call without `x-claude-code-session-id` has no session: `session_key` is null. The extractor hashes no tools, system prompt or first user message, and `core.SessionKey` and its `Digest` helpers go. The call shows in the call log and in call detail, and in no session.
- The admin call-log endpoints are `/admin/calls` and `/admin/calls/{id}`. `/admin/requests` is gone. "Call" is the domain noun everywhere.
- The live tail shows finished calls only. The recorder takes `Finished` events only, the stream has one event type (`call.finished`), and the UI has no in-flight rows.
- `pricing_version` is the `from` date of the matched pricing entry (for example `2026-10-01`). There is no separate pricing id.
- Tool pairing uses two constants: a use with no result after 1 hour reads as `use_only`, and the lookup for results reaches up to 24 hours past the query's `To` bound.

## Consequences

- A harness that sends no session header gets no session view. The OpenAI family (v2) brings back a derived key or its own headers; the trigger is in backend.md, "Later versions".
- Removing `Started` makes a running call invisible until it finishes. The trigger to bring it back is in backend.md, "Later versions".
- The migration that adds `context_compacted` is expand-only (ADR 0017).

## Work required

- Add `ContextCompacted bool` to `core.CallRecord`, and parse `x-claude-code-context-compacted` in the correlation step (ADR 0010).
- Add the column to the `calls` migration and the write path (ADR 0017), the `CallSummary` and `CallDetail` results, and the OpenAPI document.
- Add `Endpoints` and `Streamed` to `core.CallFilter`, `Match`, the Postgres filter builder (ADR 0019), the API parameter mapper (ADR 0023: `endpoint`, `streamed`) and the contract suite.
- Remove `core.SessionKey`, `Digest` and `DigestOf` from `internal/core/session.go`, with their tests, and remove the tools, system and first-message hashing from the extractor (ADR 0014). `session_key` stays null without the header.
- Remove the `Started` method and event from `recorder` and `proxy`, and the `call.started` event from `api` and the UI (ADRs 0016, 0025, 0031).
- Rename the call-log routes to `/admin/calls` and `/admin/calls/{id}` in the OpenAPI document, the handlers and the UI client.
- Compute `pricing_version` as the matched entry's `from` date in the cost step (ADR 0015).
- Implement the 1 hour and 24 hour constants in the pairing query (ADR 0020).

## Depends on

- ADR 0003, ADR 0010, ADR 0014, ADR 0017.

## References

- architecture/backend.md, "Correlation", "Tool calls, sessions and agents at read time", "Data model", "Ports".
- architecture/frontend.md, "V1 Call log", "Live tail".
- architecture/README.md, C8.
