# ADR 0033: UI view V2: session list and session view

Status: Accepted

Date: 2026-10-21

## Context

Sessions are the natural unit for agent work. The hierarchy is session, prompt (user turn), agent (main or sub-agent), LLM call, tool call. No prompt text is stored, so sessions are labelled by key, client and start time.

## Decision

- **Session list** at `/ui/sessions`: one row per session with start, duration, client, key, prompts, calls, tool calls, cost, cache hit and errors.
- **Session view** at `/ui/sessions/:key`: **Tree mode** nests prompt, agent, call and tool call; a sub-agent nests under its parent agent from the parent-agent header. **Time mode** is a waterfall sorted by start time with one lane per agent.
- **Idle-gap compression** in both modes: gaps wider than 5% of the session collapse to a fixed marker.
- Each tree row shows duration, tokens, cost and an error badge. Compaction points are drawn as markers where the cache prefix resets. Collapsed by default past the latest prompts; virtualised beyond a few hundred rows.
- The tree follows agent-prism's model (a flattened, virtualised list with waterfall bars on a shared compressed axis). Use its components if they fit, or reimplement the pattern.
- Nesting under the exact `Task` call is a later item.

## Consequences

- The agent tree is computed from the session's call rows when the page loads.
- Sessions with derived keys from header-less clients can merge.

## Work required

- Implement the list route and columns with the shared table.
- Implement the tree flattening, the compressed time axis and the waterfall bars, with virtualisation.
- Implement tree and time modes with the mode in the URL, and the compaction markers.
- Tests: flatten function with parallel sub-agents, idle-gap compression boundaries, 1,000-call session performance, deep links.

## Depends on

- ADR 0024, ADR 0030.

## References

- architecture/frontend.md, "Views" (V2), "V2 Session view", "Table virtualization".
