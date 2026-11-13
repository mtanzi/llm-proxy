# ADR 0055: Agent-facing CLI with AXI (TOON) output

Status: Proposed

Date: 2026-11-13

## Context

Spyglass (named in ADR 0054) records every call as metadata: tokens, cost, latency, errors, sessions, agents and tool calls. The admin API and the UI expose all of it, but both are made for a person who looks at a screen. An agent such as Claude Code that wants to answer "where did the cost, tokens, time and errors go, per session, call, model, agent and tool?" has no good way in. Raw JSON from `curl` is large, carries every field of every row, and gives no hint what to ask next.

Forces:

- Agents already run shell commands. Anything that costs context before it is used (an MCP server with a tool list, a long skill) is paid on every session, used or not.
- The same output should serve humans, scripts and CI.
- The admin API is the single read path. A second path into the database would bypass its filters, its time-range bounds and its OpenAPI contract.
- [AXI](https://axi.md) is a set of rules for agent-facing CLIs, with TOON as its compact output format. No Go AXI SDK exists.
- The admin plane has no sign-in in v1: it binds to a loopback address and `serve` refuses to start otherwise (backend.md, "Shape"; ADR 0023). So there is no admin key to send today. See "Connection and credentials" below.

## Decision

### Purpose and shape

- **A CLI, not an MCP server, for now.** A CLI costs no agent context until used, and it also serves humans, scripts and CI. An MCP server (`spyglass mcp`, stdio, reusing the same client code) is a possible later addition, only if a client without a shell needs it. It is out of scope here.
- **Same repository and binary.** The commands are subcommands of `spyglass`, next to `serve`, `keygen` and `version` (ADR 0009). They act as a read-only client of the existing `/admin` HTTP API, the way `docker` and `kubectl` are both server and client. There is no separate `spyglass-axi` project. One release, one install, shared Go types, and the API and the CLI change together against `api/openapi.yaml`.
- **Read-only.** The commands only issue `GET` requests. They never write, never start or stop anything, and never prompt.
- **AXI output only in the first version.** Output is TOON on stdout. JSON output (`--json`) is deferred. The admin API already serves JSON for programs.
- **A Go TOON encoder is needed**, because no Go AXI SDK exists. It lives in a small internal package, with no dependency beyond the standard library, and encodes the subset the commands use: objects, primitive values, arrays of scalars, and tabular arrays of objects with a shared field list.

### AXI rules applied

- **No arguments shows live content.** `spyglass` with no arguments prints the home view, not help.
- **Minimal default fields.** A list item has 3 to 4 default fields. `--fields a,b,c` selects others, including fields the default omits.
- **Totals and window, always.** Every list and summary reports the total count and the time window it used (`from`, `to`, and whether they are the defaults). When the API gives no count, the output says so and does not invent one (see gaps).
- **Truncation with size hints.** A long list is cut at `--limit` (default 20 in the CLI, below the API default of 50). The output says `showing 20 of 134` and carries a `help[]` line with the next page or a narrower filter. Text fields are cut with a stated length.
- **Explicit empty states.** An empty result prints a sentence that names the window and the filters, not a blank.
- **`help[]` next-step lines.** Each response ends with concrete commands, for example `spyglass sessions show team/a:1`.
- **Concise `--help`.** Every subcommand has a short `--help` with at least one example. Standard library `flag`, no CLI framework, as in ADR 0009.
- **Structured errors on stdout, with exit codes.** An error is a TOON object (`error`, `message`, `help[]`) on stdout. Exit 0 means success, 1 an error (API error, unreachable server, bad response), 2 an unknown flag or a usage error. An API error carries the API error type from `ErrorType` in the OpenAPI document (`invalid_parameter`, `not_found` and so on).
- **Never prompt.** A missing value is an error with a hint, not a question.
- **Never print key values.** See below.

### Connection and credentials

| Variable | Meaning |
|---|---|
| `SPYGLASS_ADMIN_URL` | Base URL of the admin listener. Default `http://127.0.0.1:9090`, the default `listen.admin` address (deployment.md). |
| `SPYGLASS_ADMIN_KEY` | Optional admin key. When set, the CLI sends it as `Authorization: Bearer <key>`. |

- **The admin API needs no key in v1.** The key variable exists so the CLI does not change when admin sign-in lands (backend.md, "Later versions", which names `Authorization: Bearer <admin token>` for scripts). Until then an unset key is normal and the CLI sends no `Authorization` header.
- **The key is never printed.** The CLI shows only that a key is `set` or `unset`, in `describe` and in connection errors. It never echoes the key, the header, or a URL that carries one. The key is not accepted as a flag, because flags show in process lists and shell history.
- **The server stays loopback-only.** The CLI reaches a remote server only through something the operator provides, for example an SSH tunnel to the admin port. This ADR does not change the admin listener.
- **No flag overrides the connection.** A connection error prints the URL used and the variable that sets it.

### Time window

Every command that takes a window uses `--from` and `--to` (RFC 3339, plus `--since <duration>` such as `24h` or `7d` as a shorthand for `from = now - duration`). The default is the API default: the last 24 hours. The command always prints the resolved window.

### Commands, by phase

Each command lists the endpoint it uses and the existing parameters it maps to.

#### Phase 1: orient and look up

| Command | What it shows | Endpoint and parameters |
|---|---|---|
| `spyglass` (home view) | Today's cost, calls and errors; the most expensive sessions; models with no price; `help[]` | `GET /admin/usage` (ungrouped, `grain=day`, window = today); `GET /admin/usage?group_by=session` (window = today), ranked by cost in the client; `GET /admin/config` and `GET /admin/usage?group_by=model&provider=<name>`, one per provider, for unpriced models. See gaps. |
| `spyglass describe` | The data model, every field with its unit, the default window, what is never stored | Static text built into the binary, plus a call to `GET /admin/config` for the providers and key names. |
| `spyglass config` | Providers (`name`, `kind`, `auth_mode`, `base_url`), pricing per model and start day, key names with the `disabled` flag | `GET /admin/config`. |
| `spyglass sessions list` | Sessions in the window, newest first | `GET /admin/sessions`: `from`, `to`, `key`, `client`, `provider`, `model`, `error_class`, `session`, `cost_micro_usd_gte`, `limit`, `cursor`. |
| `spyglass sessions show <key>` | Summary, agent tree, prompts and calls of one session | `GET /admin/sessions/{key}`, the key percent-encoded in the path. |
| `spyglass calls list` | Calls in the window, newest first | `GET /admin/calls`: `from`, `to`, `key`, `client`, `provider`, `model`, `endpoint`, `streamed`, `status`, `error_class`, `session`, `agent`, `agent_type`, `prompt`, `request_class`, `tool`, `tool_error`, `latency_ms_gte`, `cost_micro_usd_gte`, `limit`, `cursor`. |
| `spyglass calls show <id>` | Every recorded field of one call, with its tool uses and results | `GET /admin/calls/{id}`. |

- **Filter flags use the API parameter names**, with `_` written as `-` (`--error-class`, `--latency-ms-gte`). A repeatable parameter (`key`, `model`, `status`, `error_class` and so on) is a repeatable flag.
- **`calls list` shows the API `count`** (exact up to 10000) and marks it `10000+` when `count_capped` is true. `sessions list` has no count in the API, so it reports only the rows returned and whether a next page exists.
- **Unknown is not zero.** A call with `cost_micro_usd` null prints `cost_usd: unpriced`, not `0`. A session cost that sums only the priced calls says so, with its `unpriced_calls` count. `SessionSummary` has no such count, so the CLI takes it from `GET /admin/usage?group_by=session` with the same window and a `session` filter for the listed session keys, and no other call filter, because a session's totals cover all of its calls in the window, not only the calls that match the filters (see gaps). A call or a total cost through a `client_passthrough` provider is marked API-equivalent, as in ADR 0052, using `auth_mode` from `GET /admin/config`. Session rows carry no API-equivalent label, because a session summary does not say which providers served it (ADR 0052).

Illustrative output of the home view (field names and units are the contract; values are made up):

```
window: {from: "2026-11-13T00:00:00Z", to: "2026-11-13T14:20:00Z", default: false}
today: {calls: 412, errors: 9, error_rate: 0.022, cost_usd: 18.4213, unpriced_calls: 14, tokens_in: 3120000, tokens_out: 210000, cache_hit_ratio: 0.81, latency_p95_ms: 9400}
top_sessions[3]{session_key,calls,cost_usd,unpriced_calls}:
  fm:7f3a,188,9.8120,0
  fm:21c0,95,4.2007,14
  team/a:1,40,1.9005,0
unpriced_models[1]{provider,model,calls}:
  anthropic,claude-new-model,14
help[3]:
  spyglass sessions show fm:7f3a
  spyglass calls list --error-class rate_limited --since 24h
  spyglass config
```

Illustrative output of `spyglass sessions list --limit 3`:

```
window: {from: "2026-11-12T14:20:00Z", to: "2026-11-13T14:20:00Z", default: true}
count: showing 3, next_page: true
sessions[3]{session_key,calls,errors,cost_usd}:
  fm:7f3a,188,2,9.8120
  fm:21c0,95,0,4.2007
  team/a:1,40,3,1.9005
help[2]:
  spyglass sessions list --limit 3 --cursor eyJ0cyI6...
  spyglass sessions show fm:7f3a
```

Field naming in the CLI: `cost_usd` is the API `cost_micro_usd` divided by 1,000,000, with a fixed four decimals; `latency_ms`, `duration_ms` and `ttft_ms` keep the API unit; token fields are counts. `--fields` accepts any field of the API row, in the CLI name.

#### Phase 2: install for an agent

- `spyglass setup hooks` installs a Claude Code session-start hook that prints a compact cost and usage summary (the home view, cut down), so the agent starts each session with that context. It writes to the Claude Code settings file only after naming the file and the change, supports `--dry-run`, and is idempotent. This is the only command that writes, and it writes to the user's agent settings, never to Spyglass data. It is not a `GET` command, so it is an explicit exception to "read-only" and prints what it changed.
- A generated agent skill, a short text file written by the same command, that tells an agent when to call `spyglass` and which commands to start with. It is generated from the command table, so it cannot drift from the commands.

#### Phase 3: analysis

| Command | What it shows | Endpoint and parameters |
|---|---|---|
| `spyglass tools summary` | Per tool and agent type: count, errors, error rate, duration p50 and p95, total duration | `GET /admin/tool-calls/summary`: `from`, `to`, `session`, `agent`, `tool`, `source`, `status`, `is_error`, `duration_ms_gte`, `arg_bytes_gte`, `result_bytes_gte`. |
| `spyglass tools list` | Tool calls, a use paired with its result | `GET /admin/tool-calls`: the same filters plus `limit` and `cursor`. No count in the API. |
| `spyglass usage` | Time series of calls, errors, tokens, cost, latency | `GET /admin/usage`: the call-log filters, `group_by` (`key`, `model`, `client`, `provider`, `session`, `error_class`), `grain` (`minute`, `hour`, `day`). |
| `spyglass top <dimension>` | The biggest items of a dimension by cost, calls, errors or time | `GET /admin/usage?group_by=<dimension>` summed over the window. See gaps. |
| `spyglass compare` | The window against the previous equal window, with the change | Two `GET /admin/usage` calls (ungrouped or grouped). See gaps. |
| `spyglass tail --for <duration>` | A time-boxed live stream of finished calls | `GET /admin/stream` with the call-log filters. The CLI stops after the duration or at `--limit` events, and prints the calls as one TOON table. |

- **`tail` always has a bound.** `--for` is required. An agent must not wait on an open stream. The stream has no replay, so the CLI reports only calls that finish during the wait, and an empty tail says "no calls finished in 30s".
- **Pairing and thresholds of tool calls are the API's**: `pending` for one hour, then `use_only`; a result is looked up to 24 hours past `to` (ADR 0024).

### Gaps in the API

The commands use the existing API as it is. Where it falls short, the gap and the smallest addition it implies are:

1. **Home view and `top`: a ranked list.** `/admin/usage?group_by=session` returns one bucket per session and time, so a client must fetch every bucket and sort. The smallest addition is an `order_by` and `limit` on `/admin/usage` when grouped, or a grouped single-bucket mode. Until then the CLI sorts client-side, and the output says when the window was too large to rank exactly.
2. **Home view: models with no price.** The API has no "unpriced" filter, and a bucket grouped by model carries the model name only, not the provider. `group_by` takes one dimension, and the same model name can appear under two providers. The CLI makes one `/admin/usage?group_by=model&provider=<name>` request per provider in `/admin/config` and keeps the models with a non-zero `unpriced_calls`. A `priced=false` filter, or a combined `group_by=provider,model`, would remove the extra requests.
3. **Exact percentiles over a window.** Usage percentiles are per bucket and cannot be summed (ADR 0052). `home` and `compare` use a single bucket when the window fits one `grain`, and otherwise print the call-weighted mean marked `approx`. An ungrouped single-bucket mode of `/admin/usage` fixes it. This is the same gap ADR 0052 names.
4. **`compare` and `top`: totals over a window.** Both need one total per window, not a series. The same single-bucket mode serves them.
5. **Counts for sessions and tool calls.** `/admin/sessions` and `/admin/tool-calls` return no count. The CLI reports the page only and says so. A `count` there would let it satisfy "always report total counts" fully.
6. **Per-agent cost.** Agents appear in `sessions show` (the agent tree with call counts) and as an `agent` filter, but `/admin/usage` has no `group_by=agent`, so "which agent cost most" needs an extra call per agent. A `group_by=agent` value is the smallest addition.
7. **Unpriced calls per session.** `SessionSummary` has a `cost_micro_usd` that is null only when no call is priced, and no count of unpriced calls, so a partial cost looks complete. The CLI joins `/admin/usage?group_by=session` and its per-bucket `unpriced_calls` to the session rows, with the same window and only the listed session keys as filter. An `unpriced_calls` field on `SessionSummary` removes the join. The summary also does not name its providers, so a per-session API-equivalent label needs a `providers` field there too.

These additions are not designed in this ADR. Each is a separate API change against `api/openapi.yaml`, with its own ADR or an amendment, and each phase works without them, with the client-side workaround named above.

## Consequences

- **An agent can answer cost, token, time and error questions in a few shell calls**, with output that is small and says what to run next. The cost to a session that never uses it is zero.
- **One release and one install.** The CLI and the API move together and the Go types are shared. The cost is that the `spyglass` binary grows: it now carries client code and a TOON encoder next to the server. The client code must not import the server internals that open a database or a listener.
- **The CLI is only as good as the API.** The seven gaps above limit exactness (percentiles, rankings, counts) until the API gains the named additions. The workarounds fetch more data than a single-bucket mode would, so the home view takes several requests, and a long window takes seconds (ADR 0021).
- **TOON is new code to maintain.** The encoder is small and covered by golden tests. If the AXI or TOON spec changes, the encoder changes, not the commands.
- **No key exists to protect today**, and this ADR does not add admin auth. If the admin listener is later exposed beyond loopback, `SPYGLASS_ADMIN_KEY` takes effect without a CLI change. Until then a remote use needs a tunnel.
- **JSON output is not available from the CLI.** A script that needs JSON calls the admin API directly. Adding `--json` later is a change to one output layer.
- **`setup hooks` writes outside the repository and the database**, so it is the riskiest command. It stays out of Phase 1, prints its change, and supports `--dry-run`.
- **An MCP server stays possible.** It would reuse the same client code and call the same endpoints, so it adds a transport, not new queries.

## Work required

Phase 1:

- Internal TOON encoder package, with golden tests for objects, scalar arrays, tabular arrays, quoting and escaping, and empty values.
- Client package: base URL and key resolution from `SPYGLASS_ADMIN_URL` and `SPYGLASS_ADMIN_KEY`, typed requests that reuse the `api` types, the API error envelope, and a test that no key value reaches output or errors.
- Subcommand dispatch in `cmd/spyglass`: no-argument home view, `describe`, `config`, `sessions list|show`, `calls list|show`, with `--from`, `--to`, `--since`, `--fields`, `--limit`, the filter flags, `--help` with an example on every subcommand, and exit codes 0, 1 and 2.
- Output layer: window and totals header, truncation with size hints, empty-state messages, `help[]` lines, structured errors on stdout, unpriced and API-equivalent cost handling, and `cost_micro_usd` to `cost_usd` conversion.
- Tests: each command against a fake admin server, the usage-error exit code, the empty states, the unreachable-server error, and a check that the field set in `describe` matches `api/openapi.yaml`.

Phase 2:

- `setup hooks` with `--dry-run`, idempotent edit of the Claude Code settings, and a test on a temporary settings file.
- Generated agent skill, produced from the command table, with a test that fails when a command is missing from it.

Phase 3:

- `tools summary|list`, `usage`, `top`, `compare` and `tail --for`, with tests including a fake SSE stream and the bounded wait.
- Open a separate ADR or amendment for each API gap that is worth closing, before the command that needs it is called exact.

## Depends on

- ADR 0009, ADR 0019, ADR 0021, ADR 0023, ADR 0024, ADR 0025, ADR 0052.
- ADR 0054 (the `spyglass` binary and `SPYGLASS_*` names).

## References

- AXI: https://axi.md.
- `api/openapi.yaml`: the endpoints and parameters named above.
- architecture/backend.md, "Shape", "Admin API", "Later versions" (admin sign-in).
- architecture/deployment.md (admin listener address).
- ADR 0009 (CLI subcommands), ADR 0052 (unknown is not zero, API-equivalent label, percentile gap).
