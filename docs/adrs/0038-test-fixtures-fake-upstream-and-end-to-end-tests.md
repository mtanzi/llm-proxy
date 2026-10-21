# ADR 0038: Test fixtures, fake upstream and end-to-end tests

Status: Accepted

Date: 2026-10-21

## Context

The extractor is pure and golden-tested, and the other packages use small fakes. Beyond those, v1 needs tests that show the whole path works with a real client library, real streaming and a real database, and that bodies never reach storage or logs. No live Claude Code traffic has been captured yet, so fixtures also need a capture step.

## Decision

- A **fake Anthropic upstream** (an `httptest.Server`) that replays recorded SSE fixtures with controllable timing: chunk splits, delays, `ping` events, mid-stream errors, 429 and 5xx, aborts.
- **End-to-end tests** run the real `serve` wiring against the fake upstream and a test Postgres, with `anthropic-sdk-go` as a client against the proxy (tests only; never in the data path). They cover: a streamed and a non-streamed call end up as rows, tool halves pair at read, unknown keys get 401, upstream errors pass through unchanged, a client abort is logged, and a Postgres outage does not stop the data plane.
- **No-bodies check:** send a request with a unique marker in the prompt, response and tool arguments; assert the marker is in no table value, no log line, no metric and no API response.
- **Fixture capture:** a documented procedure and small tool to record real Claude Code exchanges through a pass-through recorder, then scrub them, to refresh golden fixtures. Header-set fixtures with and without hint headers are included (ADR 0010).
- Fixtures live under `testdata/fixtures/`.

## Consequences

- E2E tests need Docker for Postgres, like the contract suite.
- Real-traffic fixtures are scrubbed before commit: no credentials, no personal prompts.

## Work required

- Implement the fake upstream and the fixture format (headers, body chunks, delays).
- Implement the e2e test package and its helpers (start proxy, create key, query through the API).
- Implement the marker-based no-bodies test.
- Write the capture and scrub tool and document the procedure; capture a first real set when a Claude Code session can be routed (ADR 0040).
- Add the e2e job to CI.

## Depends on

- ADR 0011, ADR 0013, ADR 0014, ADR 0016, ADR 0017, ADR 0028.

## References

- architecture/backend.md, "Tests and fakes", "Libraries", "Extractor".
- architecture/deployment.md, "Sizing" (no captured traffic yet).
