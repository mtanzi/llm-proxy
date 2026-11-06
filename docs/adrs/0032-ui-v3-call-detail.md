# ADR 0032: UI view V3: call detail

Status: Completed

Date: 2026-10-21

## Context

Call detail shows what is stored for one call: numbers, names and hashes. It never shows what the prompt or response said, because that is not stored.

## Decision

- Route `/ui/calls/:id` with tabs **Overview**, **Tools** and **Usage and cost**; the active tab is a search parameter.
- **Overview:** who made the call (key, client, session, agent, sub-agent, prompt id), model, provider, upstream request id, status, error type, stop reason, TTFT and duration. No provider error message text.
- **Tools:** each tool requested in this call beside the call that resolved it: name, call id, argument size, result size, duration (header or estimated) and success or error. Each row links to the other call. No argument or result text.
- **Usage and cost:** disjoint token columns, cost with pricing version, and the raw usage JSON in a plain collapsible block.
- No conversation tab, no raw tab, no headers. "Content is not stored" appears once.
- Finished calls never change, so the detail query is cached for the session.

## Consequences

- The cache-miss tab is deferred with cache-miss diagnosis.
- Decisions D11 and D12 (Accepted ADRs 0043 and 0044) keep content fields off this page.

## Work required

- Implement the route and the three tabs with the generated types.
- Link the Tools rows to the resolving call and the session cell to V2.
- Format unknown values as unknown, `usage_estimated` and `extraction_partial` as visible badges.
- Tests: rendering with partial data, tool rows with unmatched halves, tab in URL, links.

## Depends on

- ADR 0023, ADR 0030.

## References

- architecture/frontend.md, "Views" (V3), "V3 Call detail".
