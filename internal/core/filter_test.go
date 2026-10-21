package core

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// baseCall is a call every single-field case below matches or misses on exactly one field.
func baseCall() CallRecord {
	return CallRecord{
		ID:           NewIDAt(t0),
		TS:           t0,
		KeyName:      "marco-laptop",
		Client:       "claude-cli",
		Provider:     "anthropic",
		Model:        "claude-opus-4-8",
		Status:       429,
		ErrorClass:   "rate_limit",
		SessionKey:   "s1",
		AgentID:      "a1",
		AgentType:    "subagent",
		PromptID:     "p1",
		RequestClass: "main",
		Duration:     90 * time.Second,
		Cost:         ptr(MicroUSD(2_000_000)),
		ToolUses:     []ToolUse{{ToolCallID: "toolu_1", Name: "Bash"}, {ToolCallID: "toolu_2", Name: "Read"}},
		ToolResults:  []ToolResult{{ToolCallID: "toolu_0", IsError: true}},
	}
}

func TestCallFilterMatchSingleField(t *testing.T) {
	tests := []struct {
		name string
		hit  CallFilter
		miss CallFilter
	}{
		{"from", CallFilter{From: t0}, CallFilter{From: t0.Add(time.Nanosecond)}},
		{"to", CallFilter{To: t0.Add(time.Nanosecond)}, CallFilter{To: t0}},
		{"keys", CallFilter{Keys: []string{"x", "marco-laptop"}}, CallFilter{Keys: []string{"x"}}},
		{"clients", CallFilter{Clients: []string{"claude-cli"}}, CallFilter{Clients: []string{"curl"}}},
		{"providers", CallFilter{Providers: []string{"anthropic"}}, CallFilter{Providers: []string{"openai"}}},
		{"models", CallFilter{Models: []string{"claude-opus-4-8"}}, CallFilter{Models: []string{"claude-haiku-5-5"}}},
		{"statuses", CallFilter{Statuses: []int{500, 429}}, CallFilter{Statuses: []int{200}}},
		{"error classes", CallFilter{ErrorClasses: []string{"rate_limit"}}, CallFilter{ErrorClasses: []string{"overloaded"}}},
		{"session key", CallFilter{SessionKey: "s1"}, CallFilter{SessionKey: "s2"}},
		{"agent id", CallFilter{AgentID: "a1"}, CallFilter{AgentID: "a2"}},
		{"agent types", CallFilter{AgentTypes: []string{"subagent"}}, CallFilter{AgentTypes: []string{"main"}}},
		{"prompt id", CallFilter{PromptID: "p1"}, CallFilter{PromptID: "p2"}},
		{"request classes", CallFilter{RequestClasses: []string{"main"}}, CallFilter{RequestClasses: []string{"compaction"}}},
		{"tools", CallFilter{Tools: []string{"Read"}}, CallFilter{Tools: []string{"Edit"}}},
		{"tool error true", CallFilter{ToolError: ptr(true)}, CallFilter{ToolError: ptr(false)}},
		{"min latency", CallFilter{MinLatency: 90 * time.Second}, CallFilter{MinLatency: 91 * time.Second}},
		{"min cost", CallFilter{MinCost: 2_000_000}, CallFilter{MinCost: 2_000_001}},
	}
	c := baseCall()
	for _, tt := range tests {
		if !tt.hit.Match(c) {
			t.Errorf("%s: filter should match", tt.name)
		}
		if tt.miss.Match(c) {
			t.Errorf("%s: filter should not match", tt.name)
		}
	}
}

func TestCallFilterMatchEmptyMatchesAll(t *testing.T) {
	if !(CallFilter{}).Match(CallRecord{}) || !(CallFilter{}).Match(baseCall()) {
		t.Error("zero filter should match every call")
	}
}

func TestCallFilterMatchTimeRangeHalfOpen(t *testing.T) {
	f := CallFilter{From: t0, To: t0.Add(time.Hour)}
	for _, tt := range []struct {
		ts   time.Time
		want bool
	}{
		{t0.Add(-time.Nanosecond), false},
		{t0, true},
		{t0.Add(time.Hour - time.Nanosecond), true},
		{t0.Add(time.Hour), false},
	} {
		if got := f.Match(CallRecord{TS: tt.ts}); got != tt.want {
			t.Errorf("Match at %v = %v, want %v", tt.ts, got, tt.want)
		}
	}
}

func TestCallFilterMatchToolError(t *testing.T) {
	ok := CallRecord{ToolResults: []ToolResult{{IsError: false}}}
	none := CallRecord{ToolUses: []ToolUse{{Name: "Bash"}}}
	if (CallFilter{ToolError: ptr(true)}).Match(ok) {
		t.Error("ToolError=true matched a call whose results all succeeded")
	}
	if !(CallFilter{ToolError: ptr(false)}).Match(ok) {
		t.Error("ToolError=false missed a call with a successful result")
	}
	if (CallFilter{ToolError: ptr(false)}).Match(none) || (CallFilter{ToolError: ptr(true)}).Match(none) {
		t.Error("ToolError matched a call with no tool results")
	}
}

func TestCallFilterMatchUnknownCost(t *testing.T) {
	c := baseCall()
	c.Cost = nil
	if (CallFilter{MinCost: 1}).Match(c) {
		t.Error("MinCost matched a call with unknown cost")
	}
	if !(CallFilter{}).Match(c) {
		t.Error("unfiltered cost should match unknown cost")
	}
}

func TestCallFilterMatchCombined(t *testing.T) {
	full := CallFilter{
		From: t0, To: t0.Add(time.Hour),
		Keys: []string{"marco-laptop"}, Clients: []string{"claude-cli"}, Providers: []string{"anthropic"},
		Models: []string{"claude-opus-4-8"}, Statuses: []int{429}, ErrorClasses: []string{"rate_limit"},
		SessionKey: "s1", AgentID: "a1", AgentTypes: []string{"subagent"}, PromptID: "p1",
		RequestClasses: []string{"main"}, Tools: []string{"Bash"}, ToolError: ptr(true),
		MinLatency: time.Minute, MinCost: 1_000_000,
	}
	c := baseCall()
	if !full.Match(c) {
		t.Fatal("all fields set should match the base call")
	}
	// Breaking any one field of the combined filter must reject the call.
	breakers := map[string]func(*CallFilter){
		"keys":     func(f *CallFilter) { f.Keys = []string{"x"} },
		"clients":  func(f *CallFilter) { f.Clients = []string{"x"} },
		"models":   func(f *CallFilter) { f.Models = []string{"x"} },
		"statuses": func(f *CallFilter) { f.Statuses = []int{200} },
		"session":  func(f *CallFilter) { f.SessionKey = "x" },
		"agent":    func(f *CallFilter) { f.AgentID = "x" },
		"prompt":   func(f *CallFilter) { f.PromptID = "x" },
		"tools":    func(f *CallFilter) { f.Tools = []string{"x"} },
		"toolerr":  func(f *CallFilter) { f.ToolError = ptr(false) },
		"latency":  func(f *CallFilter) { f.MinLatency = time.Hour },
		"cost":     func(f *CallFilter) { f.MinCost = 1 << 40 },
		"range":    func(f *CallFilter) { f.From = t0.Add(time.Minute) },
	}
	for name, brk := range breakers {
		f := full
		brk(&f)
		if f.Match(c) {
			t.Errorf("combined filter with broken %s still matched", name)
		}
	}
}

func TestFilterValidate(t *testing.T) {
	good := CallFilter{From: t0, To: t0.Add(time.Hour)}
	if err := good.Validate(); err != nil {
		t.Errorf("good range: %v", err)
	}
	for name, f := range map[string]CallFilter{
		"zero":     {},
		"no from":  {To: t0},
		"no to":    {From: t0},
		"empty":    {From: t0, To: t0},
		"reversed": {From: t0.Add(time.Hour), To: t0},
	} {
		if f.Validate() != ErrInvalidFilter {
			t.Errorf("%s: Validate did not reject", name)
		}
	}
	if (SessionFilter{}).Validate() != ErrInvalidFilter || (ToolCallFilter{}).Validate() != ErrInvalidFilter {
		t.Error("session and tool-call filters must require a range")
	}
	if (SessionFilter{From: t0, To: t0.Add(1)}).Validate() != nil || (ToolCallFilter{From: t0, To: t0.Add(1)}).Validate() != nil {
		t.Error("valid range rejected")
	}
}

func TestUsageQueryValidate(t *testing.T) {
	r := CallFilter{From: t0, To: t0.Add(time.Hour)}
	if err := (UsageQuery{Filter: r, GroupBy: GroupModel, Grain: GrainHour}).Validate(); err != nil {
		t.Errorf("valid query: %v", err)
	}
	for name, q := range map[string]UsageQuery{
		"no range":  {Grain: GrainDay},
		"bad group": {Filter: r, GroupBy: "tool", Grain: GrainDay},
		"no grain":  {Filter: r},
		"bad grain": {Filter: r, Grain: "week"},
	} {
		if q.Validate() != ErrInvalidFilter {
			t.Errorf("%s: Validate did not reject", name)
		}
	}
}
