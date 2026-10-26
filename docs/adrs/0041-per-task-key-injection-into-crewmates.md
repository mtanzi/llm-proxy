# ADR 0041: Per-task key injection into firstmate crewmates

Status: Deferred

Date: 2026-10-21

## Context

Per-task virtual keys (ADR 0040) need the crewmate's environment (`ANTHROPIC_BASE_URL`, the key) set at launch. Firstmate's extension system excludes worker-launch grants, so a plugin cannot do it. Keys also have to exist in the proxy's config before the crewmate starts, and adding a key needs a config edit plus `SIGHUP`.

The design plan listed this as phase 7. The captain took it out of v1: v1 ends at ADR 0040, with keys issued by hand.

## Decision

Deferred. This ADR stays in the record, outside v1, and no v1 work depends on it. The trigger for picking it up is in [backend.md, Later versions](../architecture/backend.md#storage-and-scale): **manual key issuing becomes the bottleneck.**

Options to evaluate when the trigger fires:

1. **Static dispatch profile.** Use firstmate's existing dispatch-profile configuration to set the environment, with a shared per-harness key instead of one per task. Simple; loses per-task attribution.
2. **Pre-issued key pool.** Issue a pool of named keys (for example `fm:slot-1` to `fm:slot-N`) in the config, and map task ids to slots in the ledger. No firstmate change; attribution needs a join table.
3. **Per-task key via a core change.** Propose a firstmate change that lets a dispatch step add environment and call a small helper that generates a key, appends the config entry and sends `SIGHUP`. Best attribution; needs a reviewed contract in firstmate and automation that edits the proxy config.

Start with option 2 or 3 only after reading firstmate's VISION.md and CONTRIBUTING.md; keep option 1 as the fallback. The choice changes only the operator procedure and the reporter's join, not the proxy code.

## Consequences

- v1 has no key injection. The operator issues each `fm:<task-id>` key by hand (ADR 0040, ADR 0037 runbook).
- The proxy needs no change for any option. Key names must stay stable for history.
- A helper that edits the config file touches a sensitive file and needs the same care as any deploy change.

## Work required

None in v1. When the trigger fires:

- Inspect firstmate's dispatch configuration and record what it can set per task.
- Choose an option and update this ADR to Accepted.
- If option 3: write the helper (generate, append, `SIGHUP`, idempotent) and a firstmate change proposal.
- Update the runbook and the reporter's task-id extraction if keys are not named `fm:<task-id>`.

## Depends on

- ADR 0040.

## References

- llm-proxy-design.md, sections 2.2, 2.3 (a), 2.4.
- architecture/backend.md, "Later versions".
