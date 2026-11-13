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
- The admin plane has no sign-in in v1: it binds to a loopback address and `serve` refuses to start otherwise (backend.md, "Shape"; ADR 0023). So there is no admin key to send today.
- The project keeps few packages, enforces import rules with `depguard`, and treats `api/openapi.yaml` as the contract that clients are checked against (backend.md, "Layers and the dependency rule"; frontend.md, "typed client").
- One ADR is one reviewable implementation unit, and becomes Completed when its work lands (docs/adrs/README.md).

This ADR holds the decisions shared by every CLI command and the work of phase 1. Later phases are listed under "Later", each with the trigger for its own ADR.

## Decision

### Purpose and shape

- **A CLI, not an MCP server, for now.** A CLI costs no agent context until used, and it also serves humans, scripts and CI. An MCP server (`spyglass mcp`, stdio) is a possible later addition, only if a client without a shell needs it. It is out of scope here.
- **Same repository and binary.** The commands are subcommands of `spyglass`, next to `serve`, `keygen` and `version` (ADR 0009). They act as a read-only client of the existing `/admin` HTTP API, the way `docker` and `kubectl` are both server and client. There is no separate `spyglass-axi` project. One release and one install. The API and the CLI change together against `api/openapi.yaml`.
- **Read-only.** The commands only issue `GET` requests. They never write, never start or stop anything, and never prompt.
- **An agent-specific surface over the general API.** The CLI is not a mirror of the API. It has its own small field set, names and units (for example `cost_usd`), chosen for an agent reader. The API stays the general contract.
- **AXI output only in the first version.** Output is TOON on stdout. JSON output (`--json`) is deferred. The admin API already serves JSON for programs.

### Where the client gets its types

The CLI owns small decode types and is checked against `api/openapi.yaml`. It does not import `internal/api` or `internal/config`.

- The wire types are unexported structs of the server's admin adapter (`internal/api/wire.go`), and `/admin/config` returns `config.View`. A client that imported them would depend on the adapter, the embedded UI, the metrics client and the config package. That works against the dependency rule and against the import rule below.
- This mirrors the web UI, which generates its types from the spec and has no private path to the server code (frontend.md).
- Each decode type holds only the fields the CLI uses. Decoding ignores fields it does not know, so a CLI that talks through a tunnel to a server of another version decodes what is there.
- A CLI test validates every fake-server fixture against the embedded `api.OpenAPIYAML`, with the same JSON-Schema approach as `internal/api/conformance_test.go`. A second check confirms that every API field the CLI reads exists in the spec. Drift in shape then fails a test, not a user.
- The cost is a dozen small duplicated structs. The benefit is that the server does not change, and no server JSON struct becomes a public Go API.

### The TOON encoder

- **A Go TOON encoder is part of the work.** Go TOON libraries exist, but they are community-maintained and split across spec versions, and no Go AXI SDK exists. Under the project's rule of few, deliberate dependencies (backend.md, "Libraries"), an encode-only subset of a few hundred lines is smaller than a dependency that tracks a moving spec.
- **Spec version pinned: TOON 3.3** (the `@toon-format/spec` 3.3.0 release, a working draft at the time of writing). The encoder's package comment names this version, and a spec change is a change to the encoder, not to the commands. The golden cases for the subset are taken from the spec's conformance fixtures where they exist, and are written by hand otherwise.
- **The encoder takes an explicit, ordered input model**, not reflection over arbitrary structs. An object is an ordered list of key-value pairs, and a table is a field list plus rows. Go maps have no order, and the output depends on field order. The encoder has no domain knowledge and no dependency beyond the standard library.
- **Subset:** objects, primitive values, arrays of scalars, and tabular arrays of objects with a shared field list.

### AXI rules applied

- **No arguments shows live content.** `spyglass` with no arguments prints the home view, not help. This changes today's behaviour (see "No-argument behaviour" below).
- **Minimal default fields.** A list item has 3 to 4 default fields. `--fields a,b,c` selects others from the command's declared columns.
- **Totals and window, always.** Every list and summary reports the total count and the time window it used (`from`, `to`, and whether they are the defaults). When the API gives no count, the output says so and does not invent one (see gaps).
- **Truncation with size hints.** A long list is cut at `--limit` (default 20 in the CLI, below the API default of 50). The output says `showing 20 of 134` and carries a `help[]` line with the next page or a narrower filter. Text fields are cut with a stated length.
- **Explicit empty states.** An empty result prints a sentence that names the window and the filters, not a blank.
- **`help[]` next-step lines.** Each response ends with concrete commands, for example `spyglass sessions show team/a:1`.
- **Concise `--help`.** Every subcommand has a short `--help` with at least one example. Standard library `flag`, no CLI framework, as in ADR 0009.
- **Structured errors on stdout, with exit codes.** An error is a TOON object (`error`, `message`, `help[]`) on stdout. Exit 0 means success, 1 an error (API error, unreachable server, bad response), 2 an unknown flag or a usage error. An API error carries the API error type from `ErrorType` in the OpenAPI document (`invalid_parameter`, `not_found` and so on). The stdout rule covers the client subcommands only. `serve` and `keygen` keep writing errors and usage to stderr (ADR 0009). Client commands set the `flag.FlagSet` output to `io.Discard` and format usage errors themselves, so the `flag` package prints no text of its own.
- **Never prompt.** A missing value is an error with a hint, not a question.

### No-argument behaviour

Today `spyglass` with no arguments prints usage to stderr and exits 2, and `cmd/spyglass/keygen_test.go` asserts it. This ADR changes it: no arguments runs the home view, which makes a network call. An unknown first word is still a usage error with exit 2. The container is not affected, because the image runs `serve` explicitly (Dockerfile, docker-compose.yml). The "Shape" table in backend.md lists exactly three subcommands. Its update, and the changed test, are part of Work required.

### Connection

| Variable | Meaning |
|---|---|
| `SPYGLASS_ADMIN_URL` | Base URL of the admin listener. Default `http://127.0.0.1:9090`, the default `listen.admin` address (deployment.md). |

- **The admin API needs no key in v1**, so the CLI sends no `Authorization` header. Admin sign-in is deferred (backend.md, "Later versions"). The CLI adds its key variable in the ADR that adds sign-in, which has to change the server anyway and decides the header format. Adding one environment variable read to the client is an additive change. This ADR reserves no name for it.
- **The server stays loopback-only.** The CLI reaches a remote server only through something the operator provides, for example an SSH tunnel to the admin port. This ADR does not change the admin listener.
- **No flag overrides the connection.** A connection error prints the URL used and the variable that sets it.

### Time window

Every command that takes a window uses `--from` and `--to` (RFC 3339), plus `--since <duration>` as a shorthand for `from = now - duration`. A duration is an integer and one unit: `m` (minutes), `h` (hours) or `d` (days, 24 hours), for example `30m`, `24h`, `7d`. `time.ParseDuration` has no `d` unit, so the CLI parses this form itself. Any other form is a usage error. The default is the API default: the last 24 hours. The command always prints the resolved window.

The home view uses a fixed window: the current UTC day, from 00:00 UTC to now. Buckets are aligned in UTC, so with `grain=day` each group has exactly one bucket.

### Phase 1 commands: orient and look up

Each command lists the endpoint it uses and the existing parameters it maps to.

| Command | What it shows | Endpoint and parameters |
|---|---|---|
| `spyglass` (home view) | Today's cost, calls and errors, with the headline count of unpriced calls; the most expensive sessions; `help[]` | `GET /admin/usage` (ungrouped, `grain=day`, window = current UTC day); `GET /admin/usage?group_by=session` (`grain=day`, same window), ranked by cost in the client. |
| `spyglass describe` | The data model, every field with its unit, the default window, what is never stored | The column tables of the commands (see "Output"), plus a call to `GET /admin/config` for the providers and key names. |
| `spyglass config` | Providers (`name`, `kind`, `auth_mode`, `base_url`), pricing per model and start day, key names with the `disabled` flag | `GET /admin/config`. |
| `spyglass sessions list` | Sessions in the window, newest first | `GET /admin/sessions`: `from`, `to`, `key`, `client`, `provider`, `model`, `error_class`, `session`, `cost_micro_usd_gte`, `limit`, `cursor`. |
| `spyglass sessions show <key>` | Summary, agent tree, prompts and calls of one session | `GET /admin/sessions/{key}`, the key percent-encoded in the path. |
| `spyglass calls list` | Calls in the window, newest first | `GET /admin/calls`: `from`, `to`, `key`, `client`, `provider`, `model`, `endpoint`, `streamed`, `status`, `error_class`, `session`, `agent`, `agent_type`, `prompt`, `request_class`, `tool`, `tool_error`, `latency_ms_gte`, `cost_micro_usd_gte`, `limit`, `cursor`. |
| `spyglass calls show <id>` | Every recorded field of one call, with its tool uses and results | `GET /admin/calls/{id}`. |

- **Filter flags use the API parameter names**, with `_` written as `-` (`--error-class`, `--latency-ms-gte`). A repeatable parameter (`key`, `model`, `status`, `error_class` and so on) is a repeatable flag.
- **`calls list` shows the API `count`** (exact up to 10000) and marks it `10000+` when `count_capped` is true. `sessions list` has no count in the API, so it reports only the rows returned and whether a next page exists.
- **The home view is exact in phase 1.** Both usage requests use the current UTC day and `grain=day`, so each group is one bucket. Totals and p95 come from that one bucket, and the session ranking is a sort of one row per session. There is no `approx` path and no "too large to rank" note. The home view makes two usage requests and no `/admin/config` request.
- **Unknown is not zero.** A call with `cost_micro_usd` null prints `cost_usd: unpriced`, not `0`. A session cost that sums only the priced calls says so, with its `unpriced_calls` count. That count comes from the `unpriced_calls` field of the session summary, added by ADR 0056. The CLI does no join. A call or a total cost through a `client_passthrough` provider is marked API-equivalent, as in ADR 0052, using `auth_mode` from `GET /admin/config`. Session rows carry no API-equivalent label, because a session summary does not say which providers served it (ADR 0052).
- **The home view shows the headline `unpriced_calls`** from the ungrouped bucket, and a `help[]` line that points to `spyglass calls list` with the filters that find them. It has no per-model table of models with no price. That table needs the API additions of gap 2, and comes with the later `top model` command.

Illustrative output of the home view (field names and units are the contract; values are made up):

```
window: {from: "2026-11-13T00:00:00Z", to: "2026-11-13T14:20:00Z", default: false}
today: {calls: 412, errors: 9, error_rate: 0.022, cost_usd: 18.4213, unpriced_calls: 14, tokens_in: 3120000, tokens_out: 210000, cache_hit_ratio: 0.81, latency_p95_ms: 9400}
top_sessions[3]{session_key,calls,cost_usd,unpriced_calls}:
  fm:7f3a,188,9.8120,0
  fm:21c0,95,4.2007,14
  team/a:1,40,1.9005,0
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

### Output

There is no output abstraction beyond `internal/toon`. A command builds the ordered TOON value and calls `toon.Encode`. A later `--json` is `encoding/json` over the same value. The output helpers are plain functions in `internal/cli`: window and totals header, truncation, empty states, `help[]` lines, structured errors, and the cost conversion.

Each command declares a column table: the CLI name, the API field or fields it reads, and a value function. The table drives the default fields, `--fields`, the rename (`cost_micro_usd` to `cost_usd`, the API value divided by 1,000,000, fixed at four decimals), the `unpriced` text, and `describe`. Other CLI names keep the API unit: `latency_ms`, `duration_ms` and `ttft_ms` are milliseconds, and token fields are counts. `--fields` accepts any declared column, and the declared columns can include every field the API returns for that row. Because the tables name their API fields, the spec check of the tests also covers `describe`, and `describe` takes its field descriptions from the tables, not from separate static text that could drift from the spec.

### The admin client

The admin client is a concrete struct that holds a base URL and an `*http.Client`. There is no client interface and no mock. Tests run against an `httptest.Server` (backend.md names the same rule for the upstream client). The read side has no service layer either: a command maps flags to parameters, calls the client, and shapes the result.

### Packages and the import rule

Two new packages:

- **`internal/toon`**: the pure, stdlib-only encoder, with golden tests. It has its own strict `depguard` allow-list, like `core`.
- **`internal/cli`**: the client, the commands and the output helpers, one file per concern (for example `client.go`, `window.go`, `output.go`, `home.go`, `sessions.go`, `calls.go`, `config.go`, `describe.go`), exposed as one entry point, `cli.Run(args, stdout, stderr, env)`. Its strict `depguard` allow-list permits `$gostd`, `github.com/mtanzi/spyglass/api` (the embedded spec, used by tests) and `internal/toon`, and nothing else. It cannot import `store/postgres`, a database driver, `internal/api`, `internal/config` or `core`. This is the same pattern as `keyring`, `recorder` and `ui`.

`cmd/spyglass` only dispatches: `main.go` gains one dispatch line per client command, or a single fallback case, that calls `cli.Run`. No client logic lives there, because `cmd/spyglass` is the wiring and may import everything, so no import rule could be enforced in it.

Splitting the client into its own package is a mechanical move, if an MCP server ever needs to reuse it. It is not done now.

### Gaps in the API

Phase 1 uses the existing API, plus the field added by ADR 0056. Where the API still falls short, the gap and the smallest addition it implies are below. Gaps 2, 4 and 6 matter only to the later phases. Gaps 1 and 3 are avoided in phase 1 by the fixed one-day window.

1. **Ranked list.** `/admin/usage?group_by=session` returns one bucket per session and time, so a client must fetch every bucket and sort. In phase 1 the one-day window leaves one bucket per session. For a longer window (`top`, `compare`) the smallest addition is an `order_by` and `limit` on `/admin/usage` when grouped, or a grouped single-bucket mode.
2. **Models with no price.** The API has no "unpriced" filter, and a bucket grouped by model carries the model name only, not the provider. A `priced=false` filter, or a combined `group_by=provider,model`, would let the CLI list them without one request per provider.
3. **Exact percentiles over a window.** Usage percentiles are per bucket and cannot be summed (ADR 0052). An ungrouped single-bucket mode of `/admin/usage` fixes it. This is the same gap ADR 0052 names. Phase 1 does not need it.
4. **Totals over a window** (`compare`, `top`). Both need one total per window, not a series. The same single-bucket mode serves them. Close gaps 1, 3 and 4 before building these commands. Otherwise the CLI repeats in Go the call-weighted approximation that the UI carries in TypeScript (ADR 0052).
5. **Counts for sessions and tool calls.** `/admin/sessions` and `/admin/tool-calls` return no count. The CLI reports the page only ("showing N, next_page: true") and says so. A `count` there would let it satisfy "always report total counts" fully.
6. **Per-agent cost.** Agents appear in `sessions show` (the agent tree with call counts) and as an `agent` filter, but `/admin/usage` has no `group_by=agent`, so "which agent cost most" needs an extra call per agent. A `group_by=agent` value is the smallest addition.
7. **Unpriced calls per session.** Closed by ADR 0056 (`unpriced_calls` on the session summary). The summary also does not name its providers, so a per-session API-equivalent label would need a `providers` field. That half can wait.

Each remaining gap is a separate API change against `api/openapi.yaml`, with its own ADR, decided only when a command needs it.

## Later

These are not part of this ADR. Each becomes its own ADR when its trigger is met.

| Item | What it adds | Trigger |
|---|---|---|
| Install for an agent | `spyglass setup hooks`: a Claude Code session-start hook that prints a cut-down home view, written to the Claude Code settings file only after naming the file and the change, with `--dry-run`, idempotent. A generated agent skill, produced from the command table, with a test that fails when a command is missing from it. This is the only command that writes, and it writes to the user's agent settings, never to Spyglass data, so it is an explicit exception to "read-only". It is the riskiest command because it writes outside the repository and the database. | Phase 1 has landed and is in use. |
| Analysis commands | `tools summary` and `tools list` (`/admin/tool-calls/summary`, `/admin/tool-calls`), `usage` (`/admin/usage` with `group_by` and `grain`), `top <dimension>`, `compare`, and `tail --for <duration>` (`/admin/stream`, always time-boxed, with a bound on events). Pairing and thresholds of tool calls are the API's (ADR 0024). | Phase 1 has landed, and for `top` and `compare`, gaps 1, 3 and 4 are closed. |
| Per-model unpriced table | A table of models with no price in the home view or `top model`. | A `priced=false` filter or `group_by=provider,model` exists (gap 2). |
| Admin key | One environment variable read by the client, and the `Authorization` header it implies. | The ADR for admin sign-in lands. |
| `--json` output | `encoding/json` over the value each command already builds. | A caller needs JSON from the CLI and cannot use the admin API directly. |
| MCP server | `spyglass mcp`, stdio, over the same client code. | A client without a shell needs it. |

## Consequences

- **An agent can answer cost, token, time and error questions in a few shell calls**, with output that is small and says what to run next. The cost to a session that never uses it is zero.
- **One release and one install.** The `spyglass` binary grows: it now carries client code and a TOON encoder next to the server. The client cannot import the server internals, which a `depguard` rule checks.
- **The types are duplicated on purpose.** The CLI's decode types and the server's wire types can differ. The spec check in the tests is what keeps them honest.
- **The CLI is only as good as the API.** The remaining gaps limit exactness (percentiles, rankings over long windows, counts) for the later commands, not for phase 1.
- **The home view takes two requests, and the window is fixed to the current UTC day.** A person who wants a longer window uses `sessions list` and `calls list`. Just after 00:00 UTC the home view is nearly empty. This is a stated consequence, not a defect.
- **`spyglass` with no arguments changes meaning.** Anyone who relied on usage on stderr and exit 2 now gets a network call. The container is not affected.
- **TOON is new code to maintain.** The encoder is small and covered by golden tests. If the AXI or TOON spec changes, the encoder changes, not the commands.
- **No key exists to protect today**, and this ADR adds no admin auth and no key variable.
- **JSON output is not available from the CLI.** A script that needs JSON calls the admin API directly.

## Work required

- `internal/toon`: the encoder with the explicit ordered input model, a package comment that names TOON 3.3, and golden tests for objects, scalar arrays, tabular arrays, quoting and escaping, and empty values, taking cases from the spec's conformance fixtures where they exist.
- `internal/cli`: the admin client (a concrete struct over `*http.Client`, with `SPYGLASS_ADMIN_URL` resolution and the API error envelope), the window parsing (`--from`, `--to`, `--since` with `m`, `h`, `d`), the output helpers, the per-command column tables, and the commands `home`, `describe`, `config`, `sessions list|show`, `calls list|show`, with `--fields`, `--limit`, the filter flags, `--help` with an example on every subcommand, and exit codes 0, 1 and 2. Client commands set the `flag.FlagSet` output to `io.Discard`.
- `cmd/spyglass/main.go`: dispatch to `cli.Run`, and the no-argument home view. Change the test in `cmd/spyglass/keygen_test.go` that asserts usage and exit 2 for no arguments.
- `.golangci.yml`: a strict `depguard` allow-list for `internal/cli` (`$gostd`, the root `api` package, `internal/toon`) and one for `internal/toon` (`$gostd`).
- Update `docs/architecture/backend.md` ("Shape" table of subcommands, and the package tree) and the layout table in `CONTRIBUTING.md` in the same change.
- Tests: each command against an `httptest` fake admin server; every fixture validated against `api.OpenAPIYAML`; a check that every API field the CLI reads exists in the spec and that the column tables match it; the usage-error exit code; the empty states; the unreachable-server error; the `--since` units; and the home view's two requests with `grain=day` and the UTC-day window.
- One case in `cmd/spyglass/e2e_test.go` that runs a client command (`calls list`) against the real wiring the e2e suite already starts.
- Land ADR 0056 first or with the session commands, so `sessions list` and the home view read `unpriced_calls` from the API.
- Mark this ADR Completed in the change that lands the work above.

## Depends on

- ADR 0009, ADR 0019, ADR 0021, ADR 0023, ADR 0024, ADR 0052.
- ADR 0054 (the `spyglass` binary and `SPYGLASS_*` names).
- ADR 0056 (`unpriced_calls` on the session summary).

## References

- AXI: https://axi.md.
- TOON specification 3.3: https://cdn.jsdelivr.net/npm/@toon-format/spec@3.3.0/SPEC.md.
- `api/openapi.yaml`: the endpoints and parameters named above.
- architecture/backend.md, "Shape", "Layers and the dependency rule", "Admin and query API", "Package layout", "Later versions" (admin sign-in).
- architecture/frontend.md (typed client generated from the spec).
- architecture/deployment.md (admin listener address).
- architecture/cli.md (the architecture picture of this ADR).
- ADR 0009 (CLI subcommands), ADR 0052 (unknown is not zero, API-equivalent label, percentile gap).
