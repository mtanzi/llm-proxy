# ADR 0045: Opt-in per-key body capture (D13)

Status: Accepted

Date: 2026-10-21

## Context

Decision D1 removed body storage. Some operators may want bodies for chosen keys during debugging. That would bring back a body store, retention, encryption at rest, search and the journal-like machinery that D1 and D2 removed.

## Decision

Accepted (D13), with the strict default: no capture switch exists, off or on.

Rejected option: a per-key switch, off by default, that writes bodies for chosen keys. It needs a separate store, its own retention, encryption, access control on the admin plane (which has no sign-in in v1), and UI views for conversations. It would be a new design, not an option flag.

## Consequences

- The product stays metadata-only and the admin plane stays safe on loopback.
- Body capture would need admin sign-in first and would change the privacy model, so it is a new design if it is ever wanted.

## Work required

- Add no capture switch, config key or body table.
- If capture is ever wanted: write a separate ADR set for the body store, retention, encryption and admin sign-in before any code.

## Depends on

- ADR 0002, ADR 0014.

## References

- architecture/README.md, "Resolved decisions" (D1, D2, D13).
- architecture/deployment.md, "Storage, retention and sizing".
