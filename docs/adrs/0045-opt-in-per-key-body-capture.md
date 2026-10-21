# ADR 0045: Opt-in per-key body capture (D13)

Status: Proposed

Date: 2026-10-21

## Context

Decision D1 removed body storage. Some operators may want bodies for chosen keys during debugging. That would bring back a body store, retention, encryption at rest, search and the journal-like machinery that D1 and D2 removed.

## Decision

Open (D13). The documents use the recommended option.

- **Recommended (strict default):** no capture switch exists, off or on.
- **Alternative:** a per-key switch, off by default, that writes bodies for chosen keys. It needs a separate store, its own retention, encryption, access control on the admin plane (which has no sign-in in v1), and UI views for conversations. It would be a new design, not an option flag.

## Consequences

- Strict: the product stays metadata-only and the admin plane stays safe on loopback.
- Capture: admin sign-in becomes a prerequisite and the privacy model changes.

## Work required

- Decide and update this ADR with the outcome.
- If capture is wanted: write a separate ADR set for the body store, retention, encryption and admin sign-in before any code.
- If strict is confirmed: mark Accepted and remove the pending note from the architecture documents.

## Depends on

- ADR 0002, ADR 0014.

## References

- architecture/README.md, "Open decisions" (D13), "Resolved decisions" (D1, D2).
- architecture/deployment.md, "Storage, retention and sizing".
