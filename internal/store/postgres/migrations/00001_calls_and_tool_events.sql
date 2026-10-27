-- Two plain tables, unpartitioned, written by insert only. There are no
-- foreign keys: tool halves arrive in any order, and a removed key leaves its
-- history readable.
--
-- Nullability follows core.CallRecord: a Go field whose zero value means
-- "not known" (strings, durations, times, Status, Cost, UsageRaw) is a nullable
-- column and is written as NULL when zero; the other fields are NOT NULL.

-- +goose Up
CREATE TABLE calls (
  id                  uuid PRIMARY KEY,        -- UUIDv7, also x-request-id
  ts                  timestamptz NOT NULL,    -- request start
  key_name            text NOT NULL,           -- name from the config keys section
  client              text,
  client_raw          text,
  client_request_id   text,
  session_key         text,                    -- x-claude-code-session-id; null without it
  agent_id            text,
  parent_agent_id     text,
  agent_type          text,
  prompt_id           text,
  request_class       text,
  compaction          boolean NOT NULL,
  context_compacted   boolean NOT NULL,        -- x-claude-code-context-compacted
  trace_id            text,
  family              text NOT NULL,           -- 'anthropic' in v1
  endpoint            text NOT NULL,
  auth_mode           text NOT NULL,           -- 'proxy_key' | 'client_passthrough'
  provider            text NOT NULL,
  model               text,
  upstream_request_id text,
  status              smallint,
  error_class         text,
  error_type          text,
  stop_reason         text,
  streamed            boolean NOT NULL,
  ttfb_ms             integer,
  ttft_ms             integer,
  duration_ms         integer,
  ended_at            timestamptz,
  input_uncached      integer NOT NULL,        -- usage columns are disjoint
  cache_read          integer NOT NULL,
  cache_write_5m      integer NOT NULL,
  cache_write_1h      integer NOT NULL,
  output              integer NOT NULL,        -- includes reasoning
  reasoning           integer NOT NULL,        -- informational
  usage_raw           jsonb,                   -- provider usage object
  usage_estimated     boolean NOT NULL,
  extraction_partial  boolean NOT NULL,
  cost_usd            numeric(12,6),           -- exact core.MicroUSD; null when no pricing matched
  pricing_version     text,                    -- 'from' date of the matched pricing entry
  prev_tool_durations text                     -- raw header value
);
CREATE INDEX calls_ts_id_idx ON calls (ts DESC, id DESC);
CREATE INDEX calls_session_key_ts_idx ON calls (session_key, ts);
CREATE INDEX calls_key_name_ts_idx ON calls (key_name, ts DESC);
CREATE INDEX calls_errors_ts_idx ON calls (ts DESC) WHERE error_class IS NOT NULL;

-- One row per tool-call half. Pairing happens at read time.
CREATE TABLE tool_events (
  call_id      uuid NOT NULL,                  -- the call that carried this half
  idx          integer NOT NULL,               -- position within that call
  ts           timestamptz NOT NULL,           -- use: response end; result: request start
  kind         text NOT NULL,                  -- 'use' | 'result'
  source       text NOT NULL,                  -- 'client' | 'server'
  tool_call_id text NOT NULL,                  -- toolu_...
  tool_name    text,                           -- uses only
  bytes        integer NOT NULL,               -- argument or result size; text never kept
  is_error     boolean,                        -- results only
  session_key  text,
  agent_id     text,
  PRIMARY KEY (call_id, idx)
);
CREATE INDEX tool_events_tool_call_id_idx ON tool_events (tool_call_id);
CREATE INDEX tool_events_ts_idx ON tool_events (ts DESC);
CREATE INDEX tool_events_uses_idx ON tool_events (tool_name, ts DESC) WHERE kind = 'use';

-- +goose Down
DROP TABLE tool_events;
DROP TABLE calls;
