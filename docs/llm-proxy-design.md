# Self-Hosted LLM Proxy: Requirements and firstmate Integration

Status: draft
Based on: an integration analysis for [firstmate](https://github.com/kunchenguid/firstmate).
Scope: v1 is an observe-only logging proxy for the Anthropic family (architecture decisions C1 to C4). This document holds only the requirements, the firstmate integration and the sources. The design is in [architecture/](architecture/README.md): the components, the data model and the admin API in [backend.md](architecture/backend.md), the UI in [frontend.md](architecture/frontend.md), upstream credentials in [upstream-connection.md](architecture/upstream-connection.md), and the deployment in [deployment.md](architecture/deployment.md). The v1 work is broken into [ADRs](adrs/README.md). When two sources disagree, the precedence rule in the [architecture README](architecture/README.md#source-precedence) decides.

> The baseline requirements are few and general. Section 1 separates the baseline requirements from what this design adds. Everything beyond the baseline list is extrapolation.

---

## 1. Goals and requirements

### 1.1 Baseline requirements

| # | Requirement |
|---|---|
| R1 | One endpoint you control that fronts multiple providers and models |
| R2 | Logs of which key called which model, from which client, at what time, and with what status |
| R3 | Virtual API keys, so the real provider key is never spread across work, agents and harnesses |
| R4 | Infrastructure you control (self-hosted) |
| R5 | Fast pivot of model or provider without reconfiguring every client (later version) |
| R6 | Engineer-grade usage data: token time series, not a vendor "proof of work" panel |
| R7 | Cache-miss diagnosis in seconds, with an API so it can be automated (later version) |
| R8 | Extensibility: custom MCP server endpoints and extra features |

### 1.2 Added by this design

In v1:

- Cost computation with versioned pricing (in the config file)
- No payload storage: prompts, responses, tool arguments and tool results are never stored. Retention controls apply to metadata only

Later versions, each with a trigger in [architecture/backend.md](architecture/backend.md#later-versions):

- Cache-miss diagnosis ([architecture/backend.md, Cache-miss diagnosis](architecture/backend.md#cache-miss-diagnosis))
- Budgets and rate limits per virtual key
- Model aliases and request rewriting
- Fallback, retry and circuit-breaker routing
- Alerts
- Admin sign-in, needed before the admin plane leaves localhost
- OTel export as one output among several
- The OpenAI family (v2)

### 1.3 Non-goals

- Being a general-purpose API gateway for non-LLM traffic
- Prompt management, evals or a playground UI
- Hosting models (local backends are just another provider)

---

## 2. Integration with firstmate

### 2.1 What firstmate is

Firstmate is an "agent distro": a repository of instructions, skills and bash helper scripts that turns a coding agent into a supervisor of parallel crewmate agents, each in its own git worktree and terminal session. It supports several harnesses (Claude Code, Codex, Pi, OpenCode, Cursor Agent CLI and others).

### 2.2 Why this should not be a plugin

Firstmate's extension mechanism is deliberately narrow. Per its own docs, the first binding is "one complete vertical capability, not a general plugin system". It supports a single capability, `process-event-adapter/1`, and explicitly excludes lifecycle hooks, worker-launch grants, instruction injection and delivery providers. New capabilities need a separately reviewed contract. A long-running HTTP service with a database does not fit that model, so the proxy runs as an independent service.

### 2.3 Where the two fit together

Firstmate runs many agents in parallel, possibly on different models and harnesses. That is the situation where per-key attribution pays off.

**(a) Per-task virtual keys.** Issue one key per crewmate, named with the firstmate task id (one config entry and a `SIGHUP` each), so proxy logs answer which task spent what on which model.
- In v1 the operator issues these keys by hand. Crewmates use `proxy_key` mode with a Console API key held by the proxy. `client_passthrough` stays for one person's own interactive Claude Code.
- Injecting a key into each crewmate at launch is not in v1. Worker-launch grants are out of scope for firstmate's extension system, so it would need a dispatch-profile setting or a core change. The ADR and its trigger are in [architecture/backend.md, Later versions](architecture/backend.md#later-versions).

**(b) A process-event adapter for alerts (later version; v1 has no alert feed).** This is the one thing the plugin boundary supports.
- Depends on the alerts feature, which is deferred ([architecture/backend.md](architecture/backend.md#gateway-features-decision-c1)); the design below stays valid for when it is added.
- Package: a small executable plus `firstmate-extension.json`, with capability `process-event-adapter` version 1 and required consent `network`.
- `source.poll` calls `GET /admin/alerts?since=<cursor>` on the proxy (once alerts exist) and returns `{status: "result", output: "<alert text>"}` or `{status: "no-result", output: ""}` before the host timeout.
- Candidate alerts: budget cap hit on a key, cache hit ratio drop, repeated fallback, error-rate spike, spend anomaly.
- Constraints from the extension protocol: JSON envelopes up to 64 KiB, raw result up to 32 KiB, stderr up to 8 KiB, one UTF-8 JSON document in and out per invocation, no shell, minimal environment. Responses are untrusted evidence and can never authorize a merge, decision or destination.
- The adapter should be read-only against the proxy. A bound package is trusted same-user code, not sandboxed code.

Manifest sketch:

```json
{
  "schema": "firstmate.extension-manifest.v1",
  "id": "local.llm-proxy-alerts",
  "version": "0.1.0",
  "host_protocols": [1],
  "entrypoint": "bin/firstmate-extension",
  "capabilities": [
    {
      "name": "process-event-adapter",
      "versions": [1],
      "adapter_names": ["llm-proxy-alerts"]
    }
  ],
  "required_consents": ["network"]
}
```

**(c) Ledger join (no firstmate changes).** Firstmate has an opt-in, append-only `state/fleet-ledger.jsonl` with events `task.dispatched` (kind, project, harness, requested model), `task.status`, `task.pr_ready`, `task.merged` and `task.cleaned_up`. It records no tokens or cost. A standalone reporter joins the ledger with the proxy tables on task id (via the virtual key name) and time window to produce cost per task and cost per merged PR. Ledger readers should tolerate duplicate records, since delivery is at-least-once.

### 2.4 Caveats

- **Harness routing.** The proxy only sees traffic from harnesses that can be pointed at a custom base URL with an API key. Harnesses that authenticate through a vendor subscription login may bypass it. Verify each harness before committing. This is the largest risk.
- **Request-to-task mapping** is clean with per-task keys and guesswork without them.
- **Maintainer fit.** If contributing upstream rather than running locally, read firstmate's VISION.md and CONTRIBUTING.md first. The project favors flat, inspectable, deterministic mechanics with explicit consent.

---

## Sources

- [firstmate README](https://github.com/kunchenguid/firstmate)
- [firstmate extension bindings](https://github.com/kunchenguid/firstmate/blob/main/docs/extension-bindings.md)
- [firstmate fleet ledger](https://github.com/kunchenguid/firstmate/blob/main/docs/fleet-ledger.md)
