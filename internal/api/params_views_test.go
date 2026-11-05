package api

import (
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

func rangeOf(from, to time.Time) (time.Time, time.Time) { return from, to }

func mustQuery(t *testing.T, s string) url.Values {
	t.Helper()
	q, err := url.ParseQuery(s)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestParseSessionParams(t *testing.T) {
	cur := core.Cursor{TS: testNow.Add(-time.Hour), ID: core.NewID()}
	from, to := rangeOf(testNow.Add(-24*time.Hour), testNow)
	tests := []struct {
		name  string
		query string
		want  core.SessionFilter
		limit int
		after *core.Cursor
	}{
		{"defaults", "", core.SessionFilter{From: from, To: to}, DefaultLimit, nil},
		{"repeated and scalars", "key=a&key=b&client=c&provider=p&model=m1&model=m2&error_class=e&session=s%2F1%3A2&cost_micro_usd_gte=500&limit=7",
			core.SessionFilter{From: from, To: to, Keys: []string{"a", "b"}, Clients: []string{"c"}, Providers: []string{"p"},
				Models: []string{"m1", "m2"}, ErrorClasses: []string{"e"}, SessionKey: "s/1:2", MinCost: 500}, 7, nil},
		{"cursor", "cursor=" + encodeCursor(cur), core.SessionFilter{From: from, To: to}, DefaultLimit, &cur},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parseSessionParams(mustQuery(t, tc.query), testNow)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Filter, tc.want) || p.Page.Limit != tc.limit || !reflect.DeepEqual(p.Page.After, tc.after) {
				t.Errorf("got %+v %+v, want %+v limit %d after %v", p.Filter, p.Page, tc.want, tc.limit, tc.after)
			}
		})
	}
}

func TestParseToolCallParams(t *testing.T) {
	cur := core.Cursor{TS: testNow.Add(-time.Hour), ID: core.NewID(), Idx: 2}
	from, to := rangeOf(testNow.Add(-24*time.Hour), testNow)
	tests := []struct {
		name  string
		query string
		want  core.ToolCallFilter
		after *core.Cursor
	}{
		{"defaults", "", core.ToolCallFilter{From: from, To: to}, nil},
		{"every filter", "session=s&agent=a&tool=Bash&tool=Read&source=client&source=server&status=paired&status=use_only&status=result_only&status=pending&is_error=false&duration_ms_gte=1500&arg_bytes_gte=10&result_bytes_gte=20",
			core.ToolCallFilter{From: from, To: to, SessionKey: "s", AgentID: "a", Tools: []string{"Bash", "Read"},
				Sources:  []core.ToolSource{core.ToolSourceClient, core.ToolSourceServer},
				Statuses: []core.ToolCallStatus{core.ToolCallPaired, core.ToolCallUseOnly, core.ToolCallResultOnly, core.ToolCallPending},
				IsError:  ptr(false), MinDuration: 1500 * time.Millisecond, MinArgBytes: 10, MinResultBytes: 20}, nil},
		{"cursor keeps its index", "cursor=" + encodeCursor(cur), core.ToolCallFilter{From: from, To: to}, &cur},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := parseToolCallParams(mustQuery(t, tc.query), testNow)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Filter, tc.want) || p.Page.Limit != DefaultLimit || !reflect.DeepEqual(p.Page.After, tc.after) {
				t.Errorf("got %+v %+v, want %+v after %v", p.Filter, p.Page, tc.want, tc.after)
			}
		})
	}
}

func TestParseUsageParams(t *testing.T) {
	from, to := rangeOf(testNow.Add(-24*time.Hour), testNow)
	tests := []struct {
		name  string
		query string
		want  core.UsageQuery
	}{
		{"defaults to hourly and ungrouped", "", core.UsageQuery{Filter: core.CallFilter{From: from, To: to}, Grain: core.GrainHour}},
		{"every group", "group_by=error_class&grain=minute", core.UsageQuery{Filter: core.CallFilter{From: from, To: to}, GroupBy: core.GroupErrorClass, Grain: core.GrainMinute}},
		{"call-log filters", "group_by=session&grain=day&model=m&status=529&tool=Bash&tool_error=true&latency_ms_gte=1000",
			core.UsageQuery{Filter: core.CallFilter{From: from, To: to, Models: []string{"m"}, Statuses: []int{529}, Tools: []string{"Bash"},
				ToolError: ptr(true), MinLatency: time.Second}, GroupBy: core.GroupSession, Grain: core.GrainDay}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q, err := parseUsageParams(mustQuery(t, tc.query), testNow)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(q, tc.want) {
				t.Errorf("got %+v, want %+v", q, tc.want)
			}
			if err := q.Validate(); err != nil {
				t.Errorf("a parsed query must pass core validation: %v", err)
			}
		})
	}
	for _, g := range usageGroups {
		q, err := parseUsageParams(mustQuery(t, "group_by="+string(g)), testNow)
		if err != nil || q.GroupBy != g {
			t.Errorf("group_by=%s: %+v %v", g, q, err)
		}
	}
}

func TestViewParamErrors(t *testing.T) {
	type parse func(url.Values) *apiError
	sessions := func(q url.Values) *apiError { _, e := parseSessionParams(q, testNow); return e }
	tools := func(q url.Values) *apiError { _, e := parseToolCallParams(q, testNow); return e }
	summary := func(q url.Values) *apiError {
		_, e := parseToolCallFilter(q, testNow, knownToolSummaryParams)
		return e
	}
	usage := func(q url.Values) *apiError { _, e := parseUsageParams(q, testNow); return e }
	tests := []struct {
		name  string
		parse parse
		query string
		typ   ErrorType
		param string
	}{
		{"sessions: unknown", sessions, "nope=1", ErrInvalidParameter, "nope"},
		{"sessions: call-only parameter", sessions, "status=500", ErrInvalidParameter, "status"},
		{"sessions: repeated session", sessions, "session=a&session=b", ErrInvalidParameter, "session"},
		{"sessions: empty key", sessions, "key=", ErrInvalidParameter, "key"},
		{"sessions: bad cost", sessions, "cost_micro_usd_gte=-1", ErrInvalidParameter, "cost_micro_usd_gte"},
		{"sessions: inverted range", sessions, "from=2026-10-02T00:00:00Z&to=2026-10-01T00:00:00Z", ErrInvalidParameter, "from"},
		{"sessions: bad cursor", sessions, "cursor=zzz", ErrInvalidCursor, "cursor"},
		{"sessions: limit too big", sessions, "limit=501", ErrInvalidParameter, "limit"},
		{"tools: unknown", tools, "model=x", ErrInvalidParameter, "model"},
		{"tools: bad source", tools, "source=both", ErrInvalidParameter, "source"},
		{"tools: bad status", tools, "status=open", ErrInvalidParameter, "status"},
		{"tools: bad bool", tools, "is_error=yes", ErrInvalidParameter, "is_error"},
		{"tools: empty tool", tools, "tool=", ErrInvalidParameter, "tool"},
		{"tools: bad duration", tools, "duration_ms_gte=x", ErrInvalidParameter, "duration_ms_gte"},
		{"tools: bytes overflow", tools, "arg_bytes_gte=2147483648", ErrInvalidParameter, "arg_bytes_gte"},
		{"tools: bad cursor", tools, "cursor=" + "MTIz", ErrInvalidCursor, "cursor"},
		{"summary: no limit", summary, "limit=5", ErrInvalidParameter, "limit"},
		{"summary: no cursor", summary, "cursor=x", ErrInvalidParameter, "cursor"},
		{"usage: no limit", usage, "limit=5", ErrInvalidParameter, "limit"},
		{"usage: no cursor", usage, "cursor=x", ErrInvalidParameter, "cursor"},
		{"usage: group_by outside the list", usage, "group_by=prompt", ErrInvalidParameter, "group_by"},
		{"usage: empty group_by", usage, "group_by=", ErrInvalidParameter, "group_by"},
		{"usage: group_by in upper case", usage, "group_by=Model", ErrInvalidParameter, "group_by"},
		{"usage: repeated group_by", usage, "group_by=key&group_by=model", ErrInvalidParameter, "group_by"},
		{"usage: grain outside the list", usage, "grain=week", ErrInvalidParameter, "grain"},
		{"usage: empty grain", usage, "grain=", ErrInvalidParameter, "grain"},
		{"usage: repeated grain", usage, "grain=hour&grain=day", ErrInvalidParameter, "grain"},
		{"usage: bad filter", usage, "status=99", ErrInvalidParameter, "status"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.parse(mustQuery(t, tc.query))
			if e == nil {
				t.Fatal("no error")
			}
			if e.typ != tc.typ || e.parameter != tc.param || e.status != 400 {
				t.Errorf("error = %d %s %q, want 400 %s %q", e.status, e.typ, e.parameter, tc.typ, tc.param)
			}
		})
	}
}
