# ADR 0002: v1 scope and the metadata-only storage model

Status: Completed

Date: 2026-10-21

## Context

The captain narrowed v1 in several steps (decisions D1, D2, C1 to C7). The result is an observe-only logging proxy for the Anthropic family with Claude Code as the day-one harness. Request and response bodies are never stored. Call records go straight into the store, with no journal and no projector. Many later ADRs rely on these limits, so they need one place that states them and says how the code enforces them.

## Decision

- v1 forwards, records and shows calls. It does not retry, fall back, rewrite, budget, rate-limit, alert, export OTel, partition, roll up, or translate between families.
- Bodies are never stored, by default or by option. The tap reads each exchange in memory, the extractor keeps a small set of facts, and the bytes are discarded. Tool calls are kept as records of name, ids, sizes, duration and success or error.
- Call records pass through a bounded in-memory queue and a batch writer into the store. A full queue drops the record and counts it. A crash loses at most the queued records.
- The database receives inserts only. Tool-call pairing, sessions, agent trees and statistics are computed when read ("write facts, derive views").
- Only the Anthropic family is served. There are no aliases, no `request_attempts`, no `sessions` or `agents` tables, no `call_segments`, no admin sign-in.
- Deferred items and their triggers stay in backend.md, "Later versions". They get no ADR in v1.

## Consequences

- No search over prompt or response text. Call detail shows numbers and tool names, not content.
- A wrong parsing rule cannot be recomputed for past rows. `usage_raw` is kept so a usage normalization fix can still be applied to old rows.
- The write path and schema stay small. The hot path never waits on storage.
- Open follow-ups that would relax the strict reading are separate Proposed ADRs: 0043 (tool snippets), 0044 (provider error text), 0045 (per-key body capture).

## Work required

- Add a short "v1 scope" section to the repository README that lists what is in and out, linking to backend.md, "Later versions".
- Add a review checklist item for pull requests: no new field may carry prompt, response, tool argument, tool result or provider error text, and no new endpoint may return such content.
- Make the "no bodies" rule testable at two layers: the storage contract suite scan (ADR 0018) and an end-to-end check that sends a request with a marker string and verifies the marker is in no stored value and no log line (ADR 0038).
- Record in the code (a `doc.go` in `core`) that every v1 write is an insert and every relational view is derived in `CallReader`.

The two enforcement checks in the third item are delivered by ADR 0018 (storage contract suite scan) and ADR 0038 (end-to-end marker check), not by this ADR.

## Depends on

- ADR 0001.

## References

- architecture/README.md, "Resolved decisions" (D1, D2, C1 to C7), "Open decisions".
- architecture/backend.md, "Shape", "Tool calls, sessions and agents at read time", "Later versions".
- llm-proxy-design.md, sections 1.2, 4.3, 7.2.
