package core

import "time"

// DurationSource says where a tool call duration came from.
type DurationSource string

const (
	DurationHeader    DurationSource = "header"    // prev_tool_durations on the resolving call
	DurationEstimated DurationSource = "estimated" // gap between the use and the result
)

// CallSummary is one row of the call log.
type CallSummary struct {
	ID         ID
	TS         time.Time
	KeyName    string
	Client     string
	SessionKey string
	AgentID    string
	Provider   string
	Model      string
	Status     int
	ErrorClass string
	StopReason string
	Streamed   bool
	TTFT       time.Duration
	Duration   time.Duration
	Usage      Usage
	Cost       *MicroUSD
	ToolCount  int // tool uses carried by the call
}

// CallDetail is one call with all its recorded metadata and its tool halves.
type CallDetail struct {
	CallRecord
}

// SessionSummary is one row of the session list, grouped from call rows.
type SessionSummary struct {
	SessionKey string
	KeyName    string
	Client     string
	FirstTS    time.Time
	LastTS     time.Time
	Calls      int
	Errors     int
	AgentCount int
	Models     []string
	Usage      Usage
	Cost       *MicroUSD // nil when no call in the session has a known cost
}

// AgentNode is one distinct (agent_id, parent_agent_id, agent_type) of a session.
type AgentNode struct {
	AgentID       string
	ParentAgentID string
	AgentType     string
	Calls         int
}

// PromptSummary groups the calls of one prompt id inside a session.
type PromptSummary struct {
	PromptID string
	FirstTS  time.Time
	Calls    int
	Usage    Usage
	Cost     *MicroUSD
}

// SessionDetail is a session with its agent tree, prompts and calls (metadata only).
type SessionDetail struct {
	SessionSummary
	Agents  []AgentNode
	Prompts []PromptSummary
	Calls   []CallSummary
}

// ToolCall is a tool use paired with its result at read time. Either half can be missing.
type ToolCall struct {
	ToolCallID     string
	Name           string // from the use; empty for result_only
	Source         ToolSource
	Status         ToolCallStatus
	SessionKey     string
	AgentID        string
	UseCallID      ID
	ResultCallID   ID
	UseTS          time.Time
	ResultTS       time.Time
	Duration       time.Duration
	DurationSource DurationSource
	IsError        *bool // nil without a result
	ArgBytes       int
	ResultBytes    int
}

// ToolStat is per-tool statistics over a filter.
type ToolStat struct {
	Name        string
	Count       int
	Errors      int
	DurationP50 time.Duration
	DurationP95 time.Duration
}

// UsageBucket is one point of the usage time series.
type UsageBucket struct {
	Start      time.Time // UTC, aligned to the grain
	Group      string    // value of the group-by dimension; empty when ungrouped
	Calls      int
	Errors     int
	Usage      Usage
	Cost       MicroUSD // sum over calls with a known cost
	LatencyP50 time.Duration
	LatencyP95 time.Duration
	LatencyP99 time.Duration
}
