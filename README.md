# llm-proxy

An observe-only logging proxy for LLM APIs. See [CONTRIBUTING.md](CONTRIBUTING.md) for the layout and checks, and [docs/](docs/) for design, architecture and ADRs.

## v1 scope

In v1:

- Forwards, records and shows calls for the Anthropic family, with Claude Code as the day-one harness.
- Stores facts about each call: who, provider, tokens, cost, timings, outcome and tool-call metadata (name, ids, sizes, duration, success or error).
- Inserts call records only. Tool-call pairing, sessions, agent trees and statistics are derived when read.
- Virtual-key auth on the data plane. The admin plane and UI are loopback-only.

Out of v1:

- Request and response bodies are never stored, by default or by option. No search over prompt or response text.
- No retry, fallback, request rewriting or model aliases.
- No budgets, rate limits, alerts, OTel export or admin sign-in.
- No partitioning, rollups or multi-replica tier.
- No other provider family.

Each deferred item and the trigger for adding it back is in [Later versions](docs/architecture/backend.md#later-versions). The decision is in [ADR 0002](docs/adrs/0002-v1-scope-and-metadata-only-storage-model.md).
