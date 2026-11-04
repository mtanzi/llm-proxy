package api

import (
	"encoding/json"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// The wire types below are the JSON of api/openapi.yaml. Durations are whole
// milliseconds, money is micro-USD, and an unknown cost is null.

type usageJSON struct {
	InputUncached int `json:"input_uncached"`
	CacheRead     int `json:"cache_read"`
	CacheWrite5m  int `json:"cache_write_5m"`
	CacheWrite1h  int `json:"cache_write_1h"`
	Output        int `json:"output"`
	Reasoning     int `json:"reasoning"`
}

func toUsage(u core.Usage) usageJSON {
	return usageJSON{u.InputUncached, u.CacheRead, u.CacheWrite5m, u.CacheWrite1h, u.Output, u.Reasoning}
}

type callSummaryJSON struct {
	ID               string         `json:"id"`
	TS               time.Time      `json:"ts"`
	Key              string         `json:"key"`
	Client           string         `json:"client"`
	SessionKey       string         `json:"session_key"`
	AgentID          string         `json:"agent_id"`
	Provider         string         `json:"provider"`
	Model            string         `json:"model"`
	Status           int            `json:"status"`
	ErrorClass       string         `json:"error_class"`
	StopReason       string         `json:"stop_reason"`
	Streamed         bool           `json:"streamed"`
	TTFTMs           int64          `json:"ttft_ms"`
	DurationMs       int64          `json:"duration_ms"`
	Usage            usageJSON      `json:"usage"`
	CostMicroUSD     *core.MicroUSD `json:"cost_micro_usd"`
	ToolCount        int            `json:"tool_count"`
	ContextCompacted bool           `json:"context_compacted"`
}

func toCallSummary(c core.CallSummary) callSummaryJSON {
	return callSummaryJSON{
		ID: c.ID.String(), TS: c.TS.UTC(), Key: c.KeyName, Client: c.Client, SessionKey: c.SessionKey,
		AgentID: c.AgentID, Provider: c.Provider, Model: c.Model, Status: c.Status, ErrorClass: c.ErrorClass,
		StopReason: c.StopReason, Streamed: c.Streamed, TTFTMs: c.TTFT.Milliseconds(), DurationMs: c.Duration.Milliseconds(),
		Usage: toUsage(c.Usage), CostMicroUSD: c.Cost, ToolCount: c.ToolCount, ContextCompacted: c.ContextCompacted,
	}
}

type callListJSON struct {
	Calls       []callSummaryJSON `json:"calls"`
	NextCursor  *string           `json:"next_cursor"`
	Count       int               `json:"count"`
	CountCapped bool              `json:"count_capped"`
}

type toolUseJSON struct {
	Source     core.ToolSource `json:"source"`
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	ArgBytes   int             `json:"arg_bytes"`
}

type toolResultJSON struct {
	Source      core.ToolSource `json:"source"`
	ToolCallID  string          `json:"tool_call_id"`
	IsError     bool            `json:"is_error"`
	ResultBytes int             `json:"result_bytes"`
}

type callDetailJSON struct {
	ID                string           `json:"id"`
	TS                time.Time        `json:"ts"`
	Key               string           `json:"key"`
	Client            string           `json:"client"`
	ClientRaw         string           `json:"client_raw"`
	ClientRequestID   string           `json:"client_request_id"`
	SessionKey        string           `json:"session_key"`
	AgentID           string           `json:"agent_id"`
	ParentAgentID     string           `json:"parent_agent_id"`
	AgentType         string           `json:"agent_type"`
	PromptID          string           `json:"prompt_id"`
	RequestClass      string           `json:"request_class"`
	Compaction        bool             `json:"compaction"`
	ContextCompacted  bool             `json:"context_compacted"`
	TraceID           string           `json:"trace_id"`
	Family            string           `json:"family"`
	Endpoint          string           `json:"endpoint"`
	AuthMode          string           `json:"auth_mode"`
	Provider          string           `json:"provider"`
	Model             string           `json:"model"`
	UpstreamRequestID string           `json:"upstream_request_id"`
	Status            int              `json:"status"`
	ErrorClass        string           `json:"error_class"`
	ErrorType         string           `json:"error_type"`
	StopReason        string           `json:"stop_reason"`
	Streamed          bool             `json:"streamed"`
	TTFBMs            int64            `json:"ttfb_ms"`
	TTFTMs            int64            `json:"ttft_ms"`
	DurationMs        int64            `json:"duration_ms"`
	EndedAt           *time.Time       `json:"ended_at"`
	Usage             usageJSON        `json:"usage"`
	UsageRaw          json.RawMessage  `json:"usage_raw"`
	UsageEstimated    bool             `json:"usage_estimated"`
	ExtractionPartial bool             `json:"extraction_partial"`
	CostMicroUSD      *core.MicroUSD   `json:"cost_micro_usd"`
	PricingVersion    string           `json:"pricing_version"`
	PrevToolDurations string           `json:"prev_tool_durations"`
	ToolUses          []toolUseJSON    `json:"tool_uses"`
	ToolResults       []toolResultJSON `json:"tool_results"`
}

func toCallDetail(d core.CallDetail) callDetailJSON {
	out := callDetailJSON{
		ID: d.ID.String(), TS: d.TS.UTC(), Key: d.KeyName, Client: d.Client, ClientRaw: d.ClientRaw,
		ClientRequestID: d.ClientRequestID, SessionKey: d.SessionKey, AgentID: d.AgentID,
		ParentAgentID: d.ParentAgentID, AgentType: d.AgentType, PromptID: d.PromptID,
		RequestClass: d.RequestClass, Compaction: d.Compaction, ContextCompacted: d.ContextCompacted,
		TraceID: d.TraceID, Family: d.Family, Endpoint: d.Endpoint, AuthMode: d.AuthMode,
		Provider: d.Provider, Model: d.Model, UpstreamRequestID: d.UpstreamRequestID,
		Status: d.Status, ErrorClass: d.ErrorClass, ErrorType: d.ErrorType, StopReason: d.StopReason,
		Streamed: d.Streamed, TTFBMs: d.TTFB.Milliseconds(), TTFTMs: d.TTFT.Milliseconds(),
		DurationMs: d.Duration.Milliseconds(), Usage: toUsage(d.Usage), UsageRaw: usageRaw(d.UsageRaw),
		UsageEstimated: d.UsageEstimated, ExtractionPartial: d.ExtractionPartial,
		CostMicroUSD: d.Cost, PricingVersion: d.PricingVersion, PrevToolDurations: d.PrevToolDurations,
		ToolUses:    make([]toolUseJSON, 0, len(d.ToolUses)),
		ToolResults: make([]toolResultJSON, 0, len(d.ToolResults)),
	}
	if !d.EndedAt.IsZero() {
		t := d.EndedAt.UTC()
		out.EndedAt = &t
	}
	for _, u := range d.ToolUses {
		out.ToolUses = append(out.ToolUses, toolUseJSON{u.Source, u.ToolCallID, u.Name, u.ArgBytes})
	}
	for _, r := range d.ToolResults {
		out.ToolResults = append(out.ToolResults, toolResultJSON{r.Source, r.ToolCallID, r.IsError, r.ResultBytes})
	}
	return out
}

// usageRaw returns the provider usage object, or null when none was kept or the
// stored text is not a JSON object.
func usageRaw(raw json.RawMessage) json.RawMessage {
	var m map[string]json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || m == nil {
		return json.RawMessage("null")
	}
	return raw
}
