package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"

	"github.com/mtanzi/spyglass/internal/toon"
)

// callFilters are the filter parameters of GET /admin/calls (ADR 0055).
var callFilters = []filterSpec{
	{"key", kStrings, "virtual key name (repeatable)"},
	{"client", kStrings, "client name (repeatable)"},
	{"provider", kStrings, "provider name (repeatable)"},
	{"model", kStrings, "model as the client asked for it (repeatable)"},
	{"endpoint", kStrings, "request path, for example /v1/messages (repeatable)"},
	{"streamed", kBool, "only streamed (true) or non-streamed (false) calls"},
	{"status", kInts, "HTTP status returned to the client (repeatable)"},
	{"error_class", kStrings, "error class (repeatable)"},
	{"session", kString, "session key"},
	{"agent", kString, "agent id"},
	{"agent_type", kStrings, "agent type (repeatable)"},
	{"prompt", kString, "prompt id"},
	{"request_class", kStrings, "request class (repeatable)"},
	{"tool", kStrings, "calls that made a tool call with this tool name (repeatable)"},
	{"tool_error", kBool, "calls whose paired tool result is an error (true) or not (false)"},
	{"latency_ms_gte", kInt, "calls whose total duration is at least this many ms"},
	{"cost_micro_usd_gte", kInt, "calls with a known cost of at least this many micro-USD"},
}

func (a *app) callsList(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "calls list"
	fs := newFlagSet(cmd)
	var wf windowFlags
	wf.register(fs)
	q := url.Values{}
	registerFilters(fs, q, callFilters)
	limit := limitFlag(fs, 20)
	cursor := fs.String("cursor", "", "the next_cursor of the previous page (see the help line)")
	fields := fs.String("fields", "", "comma-separated columns to show; default id,model,status,cost_usd; `spyglass describe` lists them all")
	help := func() string {
		return helpText(fs, "spyglass calls list [flags]",
			"Calls in the window, newest first, with the total count. Default window: the last 24 hours.",
			"spyglass calls list --since 1h --limit 10",
			"spyglass calls list --model claude-opus-4-8 --status 429 --status 529",
			"spyglass calls list --cost-micro-usd-gte 500000 --fields id,ts,model,cost_usd,duration_ms")
	}
	pos, err := parseFlags(fs, cmd, args, help)
	if err != nil {
		return nil, err
	}
	if err := noArgs(cmd, pos); err != nil {
		return nil, err
	}
	if err := checkLimit(cmd, *limit, 500); err != nil {
		return nil, err
	}
	w, err := wf.resolve(a.now())
	if err != nil {
		return nil, usageError(cmd, "%s", err.Error())
	}
	cols, err := callCols.pick(cmd, *fields)
	if err != nil {
		return nil, err
	}

	q.Set("from", fmtTime(w.From))
	q.Set("to", fmtTime(w.To))
	q.Set("limit", strconv.Itoa(*limit))
	if *cursor != "" {
		q.Set("cursor", *cursor)
	}
	var list callList
	if err := a.get(ctx, "/admin/calls", q, &list); err != nil {
		return nil, err
	}
	rows := make([]callRow, len(list.Calls))
	anyCost := false
	for i, c := range list.Calls {
		rows[i].callSummary = c
		anyCost = anyCost || c.CostMicroUSD != nil
	}
	var notes []string
	if anyCost {
		modes, err := a.authModes(ctx)
		if err != nil {
			return nil, err
		}
		anyEquiv := false
		for i := range rows {
			if rows[i].CostMicroUSD != nil && modes[rows[i].Provider] == authPassthrough {
				rows[i].apiEquivalent, anyEquiv = true, true
			}
		}
		if anyEquiv {
			if *fields == "" {
				cols = append(cols, callCols.cols[colIndex(callCols, "api_equivalent")])
			}
			notes = append(notes, "cost_usd of a row with api_equivalent true is an API-equivalent price, not a bill (client_passthrough provider, ADR 0052)")
		}
	}
	unpricedRows := 0
	for _, c := range list.Calls {
		if c.CostMicroUSD == nil {
			unpricedRows++
		}
	}
	if unpricedRows > 0 {
		notes = append(notes, fmt.Sprintf("%d of the shown calls: cost_usd is unpriced because no price matched, and unknown is not zero", unpricedRows))
	}

	count := any(list.Count)
	if list.CountCapped {
		count = strconv.FormatInt(list.Count, 10) + "+"
	}
	out := toon.Object{{Key: "window", Value: w.object()}}
	if f := filtersObject(q, callFilters); len(f) > 0 {
		out = append(out, toon.Pair{Key: "filters", Value: f})
	}
	out = append(out,
		toon.Pair{Key: "count", Value: count},
		toon.Pair{Key: "showing", Value: len(rows)},
		toon.Pair{Key: "next_page", Value: list.NextCursor != nil},
		toon.Pair{Key: "calls", Value: tableOf(cols, rows)},
	)
	var helps []string
	if len(rows) == 0 {
		out = append(out, toon.Pair{Key: "empty", Value: fmt.Sprintf("No calls from %s with %s.", w.describe(), filtersText(q, callFilters))})
		helps = append(helps, "spyglass calls list --since 7d")
	} else {
		helps = append(helps, "spyglass calls show "+rows[0].ID)
		if list.NextCursor != nil {
			helps = append(helps, nextCommand(fs, "spyglass calls list", w, *list.NextCursor))
		}
	}
	if p, ok := notesPair(notes); ok {
		out = append(out, p)
	}
	helps = append(helps, "spyglass describe")
	return append(out, helpPair(helps)), nil
}

func colIndex[T any](t colTable[T], name string) int {
	for i, c := range t.cols {
		if c.name == name {
			return i
		}
	}
	return 0
}

const authPassthrough = "client_passthrough"

// authModes reads provider -> auth_mode from /admin/config, which a call row
// does not carry (ADR 0052).
func (a *app) authModes(ctx context.Context) (map[string]string, error) {
	var cfg configView
	if err := a.get(ctx, "/admin/config", nil, &cfg); err != nil {
		return nil, err
	}
	m := make(map[string]string, len(cfg.Providers))
	for _, p := range cfg.Providers {
		m[p.Name] = p.AuthMode
	}
	return m, nil
}

func (a *app) callsShow(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "calls show"
	fs := newFlagSet(cmd)
	fields := fs.String("fields", "", "comma-separated fields to show; default every recorded field")
	help := func() string {
		return helpText(fs, "spyglass calls show <id> [flags]",
			"Every recorded field of one call, with its tool uses and tool results.",
			"spyglass calls show 0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b",
			"spyglass calls show 0190a1b2-c3d4-7e5f-8a9b-0c1d2e3f4a5b --fields id,model,status,cost_usd")
	}
	pos, err := parseFlags(fs, cmd, args, help)
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, usageError(cmd, "give exactly one call id, for example: spyglass calls show <id>")
	}
	cols, err := callDetailCols.pick(cmd, *fields)
	if err != nil {
		return nil, err
	}
	var d callDetail
	if err := a.get(ctx, "/admin/calls/"+escapeSegment(pos[0]), nil, &d); err != nil {
		return nil, err
	}
	row := callDetailRow{callDetail: d, apiEquivalent: d.AuthMode == authPassthrough && d.CostMicroUSD != nil}
	out := objectOf(cols, &row)
	uses, results := toolUseCols.cols, toolResultCols.cols
	out = append(out,
		toon.Pair{Key: "tool_uses", Value: tableOf(uses, d.ToolUses)},
		toon.Pair{Key: "tool_results", Value: tableOf(results, d.ToolResults)},
	)
	var notes []string
	if row.apiEquivalent {
		notes = append(notes, "cost_usd is an API-equivalent price, not a bill: the call went through a client_passthrough provider (ADR 0052)")
	}
	if d.CostMicroUSD == nil {
		notes = append(notes, "cost_usd is unpriced: no price matched, and unknown is not zero")
	}
	if p, ok := notesPair(notes); ok {
		out = append(out, p)
	}
	var helps []string
	if d.SessionKey != "" {
		helps = append(helps, "spyglass sessions show "+shellQuote(d.SessionKey))
	}
	helps = append(helps, "spyglass calls list --since 24h")
	return append(out, helpPair(helps)), nil
}
