# llm-proxy Architecture Decision Records

This folder breaks the v1 work of llm-proxy into architecture decision records (ADRs). Each ADR is one reviewable implementation unit: a decision that is already taken, or an open question that needs a decision before the related work is built. Together, the "Work required" lists of the ADRs cover all of v1.

The architecture documents stay canonical: [llm-proxy-design.md](../llm-proxy-design.md), [backend.md](../architecture/backend.md), [frontend.md](../architecture/frontend.md), [upstream-connection.md](../architecture/upstream-connection.md) and [deployment.md](../architecture/deployment.md), with the decision list in [architecture/README.md](../architecture/README.md). An ADR summarizes a decision, lists the work it needs, and points to the document sections for detail. If an ADR and a document disagree, the document wins, and the ADR needs a fix.

Format: Michael Nygard style, one Markdown file per ADR, named `NNNN-kebab-title.md`. Numbers follow a logical build order: repository and core, configuration, the data plane (auth, correlation, passthrough, tap, extractor, recorder), storage, the admin API, the UI, deployment and tests, then firstmate integration, then the open decisions.

## Template

```markdown
# ADR NNNN: Title

Status: Accepted | Proposed | Completed | Superseded by ADR NNNN

Date: YYYY-MM-DD

## Context
Why the decision is needed. Forces and constraints.

## Decision
What we do. For a Proposed ADR: the recommended option if the documents give one, and the alternatives.

## Consequences
What gets easier, what gets harder, what stays open.

## Work required
Small work items that can become tasks.

## Depends on
ADRs that must land first.

## References
Document sections (file and section name), not restated detail.
```

## Status meanings

| Status | Meaning |
|---|---|
| Accepted | The documents state this as decided. |
| Completed | The ADR's implementation work has landed on main. |
| Proposed | An open question or gap. The ADR lists the recommended option where the documents give one, and the alternatives. A gap with no recommendation stays Proposed with its options. |
| Superseded | Replaced by a later ADR (none yet). |

An ADR changes status when a decision is made. Record the outcome in the ADR and in the canonical document. An ADR moves to Completed in the same change that lands its implementation work.

## Index

"Depends on" lists the ADRs that must land first.

| # | Title | Status | Depends on |
|---|---|---|---|
| [0001](0001-go-module-and-repository-skeleton.md) | Go module and repository skeleton | Completed | - |
| [0002](0002-v1-scope-and-metadata-only-storage-model.md) | v1 scope and the metadata-only storage model | Completed | 0001 |
| [0003](0003-core-domain-and-storage-ports.md) | Core domain types and storage ports | Completed | 0001, 0002 |
| [0004](0004-configuration-file-and-secret-references.md) | Configuration file and secret references | Completed | 0001, 0003 |
| [0005](0005-config-snapshot-and-sighup-reload.md) | Config snapshot and `SIGHUP` reload | Completed | 0004 |
| [0006](0006-http-listeners-timeouts-and-request-lifecycle.md) | HTTP listeners, timeouts and request lifecycle | Accepted | 0004, 0003 |
| [0007](0007-anthropic-error-classes-and-error-bodies.md) | Error classes and protocol-correct error bodies | Completed | 0003 |
| [0008](0008-virtual-key-auth-and-keyring.md) | Virtual-key auth and the keyring | Completed | 0003, 0005, 0007 |
| [0009](0009-cli-keygen-and-version.md) | CLI subcommands: `keygen` and `version` | Accepted | 0001, 0004, 0008 |
| [0010](0010-correlation-headers-and-session-derivation.md) | Correlation headers and session derivation | Accepted | 0003, 0006 |
| [0011](0011-proxy-passthrough-and-streaming.md) | Proxy passthrough and streaming | Accepted | 0005, 0006, 0007, 0008, 0010 |
| [0012](0012-client-passthrough-and-subscription-setup.md) | Client passthrough mode and subscription setup | Accepted | 0004, 0008, 0011 |
| [0013](0013-tap-incremental-response-scanner.md) | Tap: incremental response scanner | Accepted | 0011 |
| [0014](0014-extractor-and-call-record.md) | Extractor and the call record | Accepted | 0003, 0007, 0010, 0013 |
| [0015](0015-pricing-and-cost-computation.md) | Pricing and cost computation | Accepted | 0003, 0004 |
| [0016](0016-recorder-queue-and-batch-writer.md) | Recorder: live fan-out, bounded queue and batch writer | Accepted | 0003, 0014 |
| [0017](0017-postgres-store-schema-and-write-path.md) | Postgres store: schema, migrations and write path | Accepted | 0002, 0003, 0004 |
| [0018](0018-contract-test-suite-and-postgres-test-harness.md) | Contract test suite and Postgres test harness | Accepted | 0003, 0017 |
| [0019](0019-postgres-callreader-lists-filters-and-pagination.md) | Postgres `CallReader`: lists, filters, pagination and call detail | Accepted | 0017, 0018 |
| [0020](0020-postgres-read-time-derivation-tools-sessions-agents.md) | Postgres read-time derivation: tool pairing, sessions and agents | Accepted | 0019 |
| [0021](0021-postgres-usage-aggregation.md) | Postgres usage aggregation | Accepted | 0019 |
| [0022](0022-retention-job.md) | Retention job | Accepted | 0004, 0017, 0018 |
| [0023](0023-admin-plane-skeleton-openapi-and-call-log-endpoints.md) | Admin plane skeleton, OpenAPI contract and call-log endpoints | Accepted | 0003, 0004, 0006, 0015, 0019 |
| [0024](0024-session-tool-call-and-usage-endpoints.md) | Session, tool-call and usage endpoints | Accepted | 0020, 0021, 0023 |
| [0025](0025-live-tail-sse-stream.md) | Live tail (`GET /admin/stream`) | Accepted | 0003, 0016, 0023 |
| [0026](0026-observability-health-readiness-and-metrics.md) | Observability: health, readiness and Prometheus metrics | Accepted | 0006, 0016, 0017 |
| [0027](0027-graceful-shutdown-and-drain.md) | Graceful shutdown and drain | Accepted | 0006, 0011, 0016, 0026 |
| [0028](0028-composition-root-and-serve-startup.md) | Composition root and `serve` startup | Accepted | 0005, 0008, 0011, 0016, 0017, 0022, 0023, 0026, 0027 |
| [0029](0029-ui-scaffold-embedding-and-serving.md) | UI scaffold, embedding and serving | Accepted | 0001, 0006, 0023 |
| [0030](0030-ui-foundation-routing-api-client-and-tables.md) | UI foundation: routing, typed API client, query state and tables | Accepted | 0023, 0029 |
| [0031](0031-ui-v1-call-log-and-live-toggle.md) | UI view V1: call log and live toggle | Accepted | 0024, 0025, 0030 |
| [0032](0032-ui-v3-call-detail.md) | UI view V3: call detail | Accepted | 0023, 0030 |
| [0033](0033-ui-v2-session-list-and-session-view.md) | UI view V2: session list and session view | Accepted | 0024, 0030 |
| [0034](0034-ui-v4-tool-call-explorer.md) | UI view V4: tool-call explorer | Accepted | 0024, 0030 |
| [0035](0035-ui-v5-cost-and-cache-and-config-page.md) | UI view V5: cost and cache, and the read-only config page | Accepted | 0024, 0030 |
| [0036](0036-container-image-and-release-build.md) | Container image and release build | Accepted | 0028, 0029 |
| [0037](0037-deployment-guide-compose-runbook-and-personal-setup.md) | Deployment guide: compose file, runbook and personal local setup | Accepted | 0009, 0012, 0036 |
| [0038](0038-test-fixtures-fake-upstream-and-end-to-end-tests.md) | Test fixtures, fake upstream and end-to-end tests | Accepted | 0011, 0013, 0014, 0016, 0017, 0028 |
| [0039](0039-performance-targets-and-load-verification.md) | Performance targets and load verification | Accepted | 0011, 0013, 0038 |
| [0040](0040-firstmate-integration-claude-code-routing-and-ledger-reporter.md) | Firstmate integration: Claude Code routing and the ledger-join reporter | Accepted | 0008, 0009, 0037, 0024 |
| [0041](0041-per-task-key-injection-into-crewmates.md) | Per-task key injection into firstmate crewmates | Proposed | 0040 |
| [0042](0042-claude-code-hint-headers-for-crewmates.md) | Claude Code hint headers for firstmate crewmates (D5) | Proposed | 0010, 0040 |
| [0043](0043-short-tool-snippets.md) | Short tool snippets (D11) | Proposed | 0014, 0034 |
| [0044](0044-provider-error-message-text.md) | Provider error message text (D12) | Proposed | 0014, 0032 |
| [0045](0045-opt-in-per-key-body-capture.md) | Opt-in per-key body capture (D13) | Proposed | 0002, 0014 |
| [0046](0046-adopt-gap-review-defaults.md) | Adopt the gap-review defaults (D9 and D10) | Proposed | 0002 |

## Suggested implementation order

Phases follow the design document's plan (llm-proxy-design.md, section 9). Items inside a phase can run in parallel where "Depends on" allows.

1. **Foundations:** 0001, 0002, 0003, then 0004 and 0005 (config and reload), 0006 (listeners).
2. **Proxy core (design phase 1):** 0007 (error bodies), 0008 (auth), 0009 (keygen), 0010 (correlation), 0011 (passthrough and streaming). In parallel, the storage track: 0017 (schema and write path) and 0018 (contract suite and harness).
3. **Recording (phases 1 and 2):** 0013 (tap), 0014 (extractor), 0015 (pricing), 0016 (recorder). Then 0012 (client passthrough, needed for the personal setup), 0026 (health and metrics), 0027 (graceful shutdown), 0028 (composition root). The first end-to-end run is possible here.
4. **Read side (phase 3):** 0019, 0020, 0021 (readers), 0022 (retention), then 0023, 0024, 0025 (admin API and live tail).
5. **UI (phase 3):** 0029, 0030, then 0031 to 0035 (views, mostly in parallel).
6. **Ship and verify:** 0036 (image), 0037 (deployment guide), 0038 (e2e tests and fixtures), 0039 (performance).
7. **Firstmate (phases 5 and 7):** 0040 (routing and ledger reporter), 0041 (key injection, Proposed).
8. **Open decisions:** 0042 to 0046 are Proposed. Decide 0042 (D5) when 0040 first routes Claude Code through the proxy. 0043 to 0045 (D11 to D13) must be decided before 0014 and the UI views are finalised; the recommended answer in each is the strict default already assumed by those ADRs. 0046 (D9, D10) is a review of defaults already in use.

The test harness of 0018 and the fixtures of 0038 are used by most other ADRs, so start them early.

## Out of v1 scope

These items are deferred and have no ADR. Each has a trigger for adding it back in [backend.md, Later versions](../architecture/backend.md#later-versions); the v1 design leaves room for all of them.

- Gateway features: fallback, retries and circuit breakers with `request_attempts`; model aliases and request rewriting (C6); budgets, rate limits, concurrency limits and key expiry; alerts, `/admin/alerts` and the firstmate alert adapter.
- Admin sign-in, admin scopes and OIDC (C7). The admin plane stays loopback-only.
- OTel span export.
- Cache-miss diagnosis: segment hashing, `call_segments`, `CacheDiff`, the cache-analysis endpoint and the UI cache view (C5).
- Storage and scale: rollup tables, daily partitioning, `CopyFrom` staging, a materialised tool-call pairing table, `sync.Pool` buffers in the tap, the multi-replica tier with a `LiveFeed` port (D8), `Last-Event-ID` catch-up, backward paging and exact counts above 10,000, the sub-agent link to the spawning `Task` call.
- `store/sqlite`, the MCP gateway (`source = mcp`) and OIDC.
- The OpenAI family (v2): `family/openai`, `include_usage` handling, Codex CLI and OpenCode setup and their session headers (C4, D3, D4).
- Saved views, annotations and scores, editing pricing in the UI, static HTML export of a session.
