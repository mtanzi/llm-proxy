package api

import (
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// Page size of the call list: the default, and the most a client may ask for.
const (
	DefaultLimit = 50
	MaxLimit     = 500
)

// DefaultRange is the time range of a list query that names none.
const DefaultRange = 24 * time.Hour

// CountCap is the most calls CountCalls counts. A larger match is reported as
// count 10000 with count_capped true, which the UI shows as "10,000+".
const CountCap = 10_000

// callParams is a parsed GET /admin/calls query.
type callParams struct {
	Filter core.CallFilter
	Page   core.Page
}

var knownCallParams = map[string]bool{
	"from": true, "to": true, "key": true, "client": true, "provider": true, "model": true,
	"endpoint": true, "streamed": true, "status": true, "error_class": true, "session": true,
	"agent": true, "agent_type": true, "prompt": true, "request_class": true, "tool": true,
	"tool_error": true, "latency_ms_gte": true, "cost_micro_usd_gte": true, "limit": true, "cursor": true,
}

// parseCallParams maps a query string onto a core.CallFilter and a page. It
// refuses an unknown parameter name, a repeated single-value parameter and any
// malformed value, naming the parameter at fault. now supplies the default
// range: the last 24 hours up to now.
func parseCallParams(q url.Values, now time.Time) (callParams, *apiError) {
	var p callParams
	for name := range q {
		if !knownCallParams[name] {
			return p, badParam(name, "unknown parameter")
		}
	}
	f := &p.Filter
	var err *apiError

	if f.From, err = timeParam(q, "from"); err != nil {
		return p, err
	}
	if f.To, err = timeParam(q, "to"); err != nil {
		return p, err
	}
	if f.To.IsZero() {
		f.To = now
	}
	if f.From.IsZero() {
		f.From = f.To.Add(-DefaultRange)
	}
	if !f.From.Before(f.To) {
		return p, badParam("from", "from must be before to")
	}

	f.Keys = q["key"]
	f.Clients = q["client"]
	f.Providers = q["provider"]
	f.Models = q["model"]
	f.Endpoints = q["endpoint"]
	f.ErrorClasses = q["error_class"]
	f.AgentTypes = q["agent_type"]
	f.RequestClasses = q["request_class"]
	f.Tools = q["tool"]
	for _, name := range []string{"key", "client", "provider", "model", "endpoint", "error_class", "agent_type", "request_class", "tool"} {
		for _, v := range q[name] {
			if v == "" {
				return p, badParam(name, "empty value")
			}
		}
	}
	for _, v := range q["status"] {
		n, e := strconv.Atoi(v)
		if e != nil || n < 100 || n > 599 {
			return p, badParam("status", fmt.Sprintf("%q is not an HTTP status", v))
		}
		f.Statuses = append(f.Statuses, n)
	}
	if f.Streamed, err = boolParam(q, "streamed"); err != nil {
		return p, err
	}
	if f.ToolError, err = boolParam(q, "tool_error"); err != nil {
		return p, err
	}
	if f.SessionKey, err = stringParam(q, "session"); err != nil {
		return p, err
	}
	if f.AgentID, err = stringParam(q, "agent"); err != nil {
		return p, err
	}
	if f.PromptID, err = stringParam(q, "prompt"); err != nil {
		return p, err
	}
	ms, err := intParam(q, "latency_ms_gte", 0, 1<<40)
	if err != nil {
		return p, err
	}
	f.MinLatency = time.Duration(ms) * time.Millisecond
	cost, err := intParam(q, "cost_micro_usd_gte", 0, 1<<62)
	if err != nil {
		return p, err
	}
	f.MinCost = core.MicroUSD(cost)

	limit, err := intParam(q, "limit", 1, MaxLimit)
	if err != nil {
		return p, err
	}
	p.Page.Limit = int(limit)
	if _, set := q["limit"]; !set {
		p.Page.Limit = DefaultLimit
	}
	if s, err := stringParam(q, "cursor"); err != nil {
		return p, err
	} else if s != "" {
		c, e := decodeCursor(s)
		if e != nil {
			return p, &apiError{status: 400, typ: ErrInvalidCursor, message: "cursor is not valid", parameter: "cursor"}
		}
		p.Page.After = &c
	}
	return p, nil
}

// single returns the one value of name, whether it is present, or an error when
// the parameter is repeated.
func single(q url.Values, name string) (string, bool, *apiError) {
	vs, ok := q[name]
	switch {
	case !ok:
		return "", false, nil
	case len(vs) > 1:
		return "", true, badParam(name, "parameter must not be repeated")
	}
	return vs[0], true, nil
}

func stringParam(q url.Values, name string) (string, *apiError) {
	v, _, err := single(q, name)
	return v, err
}

func timeParam(q url.Values, name string) (time.Time, *apiError) {
	v, ok, err := single(q, name)
	if err != nil || !ok {
		return time.Time{}, err
	}
	t, e := time.Parse(time.RFC3339Nano, v)
	if e != nil {
		return time.Time{}, badParam(name, fmt.Sprintf("%q is not an RFC 3339 time", v))
	}
	return t.UTC(), nil
}

func boolParam(q url.Values, name string) (*bool, *apiError) {
	v, ok, err := single(q, name)
	if err != nil || !ok {
		return nil, err
	}
	switch v {
	case "true":
		b := true
		return &b, nil
	case "false":
		b := false
		return &b, nil
	}
	return nil, badParam(name, fmt.Sprintf("%q is not true or false", v))
}

func intParam(q url.Values, name string, lo, hi int64) (int64, *apiError) {
	v, ok, err := single(q, name)
	if err != nil || !ok {
		return 0, err
	}
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n < lo || n > hi {
		return 0, badParam(name, fmt.Sprintf("%q is not a whole number from %d to %d", v, lo, hi))
	}
	return n, nil
}
