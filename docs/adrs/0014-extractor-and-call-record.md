# ADR 0014: Extractor and the call record

Status: Completed

Date: 2026-10-21

## Context

`family/anthropic` is the only package that understands provider formats. After an exchange ends, it turns the request bytes, the facts scanned from the response, the timings and the metadata into one call record. It is pure and golden-tested. A wrong rule cannot be recomputed for past rows, so correctness is checked up front.

## Decision

A call record holds:

- **Who:** key name, client, session key (null without the session header), agent, parent agent, agent type, prompt id, and the `compaction` and `context_compacted` flags (from headers, ADR 0010).
- **What ran:** provider, model as the client sent it, `upstream_request_id`, one attempt per call, and `auth_mode`, the credential mode used for the request (`proxy_key` or `client_passthrough`).
- **Usage:** disjoint columns `input_uncached`, `cache_read`, `cache_write_5m`, `cache_write_1h`, `output` (including reasoning), `reasoning` (informational), plus `usage_raw` as the provider's usage JSON. Cost carries `pricing_version`, the `from` date of the matched pricing entry (ADR 0015).
- **Timings:** TTFB, TTFT, duration, end timestamp.
- **Outcome:** status, `error_class`, provider `error_type`, stop reason, `streamed`, `usage_estimated`, `extraction_partial`. No provider message text.
- **Tool halves:** for each `tool_use` in the response: call id, tool name, argument size in bytes. For each `tool_result` in the trailing run of consecutive user messages of the request: the id it answers, `is_error`, result size in bytes. Results before that run were recorded by the calls that first carried them. For each server tool result block in the response, such as `web_search_tool_result` or `code_execution_tool_result`: the same fields with `source = server`, timed at response end. Tool ids and names are kept only as JSON strings of at most 256 bytes. Text is read to measure it and not kept.
- No snippet, no prompt text, no header value other than the correlation fields; credentials are never read into the record.

After the record is built, the proxy computes cost (ADR 0015), drops the request bytes and sends `Finished(record)` to the recorder.

## Consequences

- A client abort is logged with the facts extracted so far, `error_class = client_abort` and `usage_estimated = true`.
- Prompt caching diagnosis (segment hashing) is not in v1; the extractor hashes nothing, because v1 has no session derivation.
- The strict reading is confirmed by Accepted ADRs 0043 to 0045: no tool snippets, no error message text, no body capture.

## Work required

- Implement request fact extraction with `gjson`: `model`, stream flag, `tool_result` blocks (ids, `is_error`, sizes).
- Implement usage normalisation into the disjoint columns, including 5-minute and 1-hour cache writes, and keep `usage_raw`.
- Implement response-side assembly from the tap facts (ADR 0013), error mapping (ADR 0007), `stop_reason`, `streamed`, and `usage_estimated` for aborts.
- Assemble the `core.CallRecord` and assign tool-half indices (`idx`) and timestamps (`use` and server `result`: response end; client `result`: request start).
- Record server-side tools (web search, code execution) with `source = server`.
- Build the golden fixture set under `testdata/fixtures/`: recorded or hand-written SSE exchanges (short chat, long thinking, parallel tool calls, tool results in the next request, cache reads and writes, 429, 529, mid-stream error, abort) with expected call-record JSON. Provide a small tool to regenerate goldens.
- Tests: a table test per usage shape, a property test that no input string from the fixture bodies appears in any record field (the fake-prompt scan), and a test that a malformed body yields a record with null fields, not a panic.

## Depends on

- ADR 0003, ADR 0007, ADR 0010, ADR 0013.

## References

- architecture/backend.md, "Extractor", "Data model" (Notes on usage columns).
- architecture/backend.md, "Data model".
- ADR 0048, which amends Completed ADR 0003 for the record changes.
