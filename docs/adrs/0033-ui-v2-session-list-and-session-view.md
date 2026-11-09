# ADR 0033: UI view V2: session list and session view

Status: Completed

Date: 2026-10-21

## Context

Sessions are the natural unit for agent work. The hierarchy is session, prompt (user turn), agent (main or sub-agent), LLM call, tool call. No prompt text is stored, so sessions are labelled by key, client and start time.

## Decision

- **Session list** at `/ui/sessions`: one row per session with start, duration, client, key, prompts, calls, tool calls, cost, cache hit and errors.
- **Session view** at `/ui/sessions/:key`: **Tree mode** nests prompt, agent, call and tool call; a sub-agent nests under its parent agent from the parent-agent header. **Time mode** is a waterfall sorted by start time with one lane per agent.
- **Idle-gap compression** in both modes: gaps wider than 5% of the session collapse to a fixed marker.
- Each tree row shows duration, tokens, cost and an error badge. Compaction points (the `context_compacted` flag) are drawn as markers where the cache prefix resets. Collapsed by default past the latest prompts; virtualised beyond a few hundred rows.
- The tree follows agent-prism's model (a flattened, virtualised list with waterfall bars on a shared compressed axis). Use its components if they fit, or reimplement the pattern.
- Nesting under the exact `Task` call is a later item.
- **Design direction.** The look and feel follows Linear: dark, dense, keyboard-first, monochrome greys with one accent colour. Error red is a status colour, not a second accent. The list and the tree are driven from the keyboard: arrow keys or `j` and `k` move between rows, `Enter` opens, and in the tree the left and right arrows (or `h` and `l`) collapse and expand.
- **Session view model.** The session view follows Langfuse's session and trace view: a nested tree (prompt, agent and sub-agent, call, tool call) where every level shows duration, tokens, cost and an error badge, beside a trace waterfall on a shared time axis. A call or tool-call row opens the call detail view (V3).
- The colour tokens live in `web/src/styles.css` and are shared, so the call log and call detail views pick up the dark palette. Restyling those views to the same density is follow-up work.

## Consequences

- The agent tree is computed from the session's call rows when the page loads.
- A call without a session header has no session, so it appears in no session row or tree.
- `SessionSummary` in `GET /admin/sessions` has no prompt count and no tool-call count, so those two list columns show unknown. The list duration is the first to the last call start, because the summary has no end time for the last call. The session view has the exact numbers, because it reads the calls and `GET /admin/tool-calls?session=`.

## Work required

- Implement the list route and columns with the shared table.
- Implement the tree flattening, the compressed time axis and the waterfall bars, with virtualisation.
- Implement tree and time modes with the mode in the URL, and the compaction markers.
- Tests: flatten function with parallel sub-agents, idle-gap compression boundaries, 1,000-call session performance, deep links.

## Depends on

- ADR 0024, ADR 0030.

## References

- architecture/frontend.md, "Views" (V2), "V2 Session view", "Table virtualization".
