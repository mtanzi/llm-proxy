package cli

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/mtanzi/spyglass/internal/toon"
)

// sessionFilters are the filter parameters of GET /admin/sessions (ADR 0055).
var sessionFilters = []filterSpec{
	{"key", kStrings, "virtual key name (repeatable)"},
	{"client", kStrings, "client name (repeatable)"},
	{"provider", kStrings, "provider name (repeatable)"},
	{"model", kStrings, "model as the client asked for it (repeatable)"},
	{"error_class", kStrings, "error class (repeatable)"},
	{"session", kString, "session key"},
	{"cost_micro_usd_gte", kInt, "sessions with a call of known cost of at least this many micro-USD"},
}

func (a *app) sessionsList(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "sessions list"
	fs := newFlagSet(cmd)
	var wf windowFlags
	wf.register(fs)
	q := url.Values{}
	registerFilters(fs, q, sessionFilters)
	limit := limitFlag(fs, 20)
	cursor := fs.String("cursor", "", "the next_cursor of the previous page (see the help line)")
	fields := fs.String("fields", "", "comma-separated columns to show; default session_key,calls,errors,cost_usd; `spyglass describe` lists them all")
	help := func() string {
		return helpText(fs, "spyglass sessions list [flags]",
			"Sessions in the window, newest first. The API gives no session count, so the output reports the rows shown and whether a next page exists. Default window: the last 24 hours.",
			"spyglass sessions list --limit 5",
			"spyglass sessions list --since 7d --error-class rate_limited",
			"spyglass sessions list --fields session_key,calls,cost_usd,unpriced_calls")
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
	cols, err := sessionCols.pick(cmd, *fields)
	if err != nil {
		return nil, err
	}
	q.Set("from", fmtTime(w.From))
	q.Set("to", fmtTime(w.To))
	q.Set("limit", strconv.Itoa(*limit))
	if *cursor != "" {
		q.Set("cursor", *cursor)
	}
	var list sessionList
	if err := a.get(ctx, "/admin/sessions", q, &list); err != nil {
		return nil, err
	}
	var notes []string
	partial, allUnpriced := 0, 0
	for _, s := range list.Sessions {
		switch {
		case s.CostMicroUSD == nil && s.UnpricedCalls > 0:
			allUnpriced++
		case s.UnpricedCalls > 0:
			partial++
		}
	}
	if partial > 0 {
		notes = append(notes, fmt.Sprintf("%d of the shown sessions: cost_usd sums the priced calls only; add unpriced_calls to --fields for the count left out", partial))
	}
	if allUnpriced > 0 {
		notes = append(notes, fmt.Sprintf("%d of the shown sessions: cost_usd is unpriced because no call has a price, and unknown is not zero", allUnpriced))
	}
	out := toon.Object{{Key: "window", Value: w.object()}}
	if f := filtersObject(q, sessionFilters); len(f) > 0 {
		out = append(out, toon.Pair{Key: "filters", Value: f})
	}
	out = append(out,
		toon.Pair{Key: "showing", Value: len(list.Sessions)},
		toon.Pair{Key: "next_page", Value: list.NextCursor != nil},
		toon.Pair{Key: "sessions", Value: tableOf(cols, list.Sessions)},
	)
	var helps []string
	if len(list.Sessions) == 0 {
		out = append(out, toon.Pair{Key: "empty", Value: fmt.Sprintf("No sessions from %s with %s.", w.describe(), filtersText(q, sessionFilters))})
		helps = append(helps, "spyglass sessions list --since 7d")
	} else {
		if list.NextCursor != nil {
			helps = append(helps, nextCommand(fs, "spyglass sessions list", w, *list.NextCursor))
		}
		helps = append(helps, "spyglass sessions show "+shellQuote(list.Sessions[0].SessionKey))
	}
	if p, ok := notesPair(notes); ok {
		out = append(out, p)
	}
	return append(out, helpPair(helps)), nil
}

func (a *app) sessionsShow(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "sessions show"
	fs := newFlagSet(cmd)
	limit := limitFlag(fs, 20)
	fields := fs.String("fields", "", "comma-separated call columns to show; default id,model,status,cost_usd")
	help := func() string {
		return helpText(fs, "spyglass sessions show <key> [flags]",
			"Summary, agent tree, prompts and calls of one session. Prompts and calls are cut at --limit; the totals say how many there are. The key is the session key exactly as listed.",
			"spyglass sessions show team/a:1",
			"spyglass sessions show fm:7f3a --limit 50 --fields id,model,status,duration_ms,cost_usd")
	}
	pos, err := parseFlags(fs, cmd, args, help)
	if err != nil {
		return nil, err
	}
	if len(pos) != 1 {
		return nil, &cmdError{code: 2, typ: "usage", msg: "give exactly one session key, for example: spyglass sessions show team/a:1",
			help: []string{"spyglass sessions list"}}
	}
	if err := checkLimit(cmd, *limit, 100000); err != nil {
		return nil, err
	}
	cols, err := callCols.pick(cmd, *fields)
	if err != nil {
		return nil, err
	}
	var d sessionDetail
	if err := a.get(ctx, "/admin/sessions/"+escapeSegment(pos[0]), nil, &d); err != nil {
		return nil, err
	}
	rows := make([]callRow, 0, min(len(d.Calls), *limit))
	anyCost := false
	for _, c := range d.Calls {
		anyCost = anyCost || c.CostMicroUSD != nil
	}
	var modes map[string]string
	if anyCost {
		if modes, err = a.authModes(ctx); err != nil {
			return nil, err
		}
	}
	anyEquiv := false
	for i, c := range d.Calls {
		if i >= *limit {
			break
		}
		r := callRow{callSummary: c}
		if c.CostMicroUSD != nil && modes[c.Provider] == authPassthrough {
			r.apiEquivalent, anyEquiv = true, true
		}
		rows = append(rows, r)
	}
	if anyEquiv && *fields == "" {
		cols = append(cols, callCols.cols[colIndex(callCols, "api_equivalent")])
	}
	prompts := d.Prompts[:min(len(d.Prompts), *limit)]

	sumCols, err := sessionCols.pick(cmd, strings.Join(sessionCols.names(), ","))
	if err != nil {
		return nil, err
	}
	out := toon.Object{
		{Key: "session", Value: objectOf(sumCols, &d.Summary)},
		{Key: "agents", Value: tableOf(agentCols.cols, d.Agents)},
		{Key: "prompts", Value: tableOf(promptCols.cols, prompts)},
		{Key: "calls", Value: tableOf(cols, rows)},
		{Key: "totals", Value: toon.Object{
			{Key: "agents", Value: len(d.Agents)},
			{Key: "prompts", Value: len(d.Prompts)},
			{Key: "calls", Value: len(d.Calls)},
			{Key: "showing_prompts", Value: len(prompts)},
			{Key: "showing_calls", Value: len(rows)},
		}},
	}
	var notes []string
	s := d.Summary
	if s.UnpricedCalls > 0 {
		if s.CostMicroUSD == nil {
			notes = append(notes, fmt.Sprintf("cost_usd is unpriced: none of the %d calls has a price, and unknown is not zero", s.UnpricedCalls))
		} else {
			notes = append(notes, fmt.Sprintf("cost_usd sums the priced calls only; %d %s left out (unpriced_calls)", s.UnpricedCalls, plural(s.UnpricedCalls, "call is", "calls are")))
		}
	}
	if anyEquiv {
		notes = append(notes, "cost_usd of a call row with api_equivalent true is an API-equivalent price, not a bill (ADR 0052). The session total does not say which providers served it")
	}
	if len(rows) < len(d.Calls) {
		notes = append(notes, fmt.Sprintf("showing the first %d of %d calls in API order; raise --limit or page with calls list", len(rows), len(d.Calls)))
	}
	if p, ok := notesPair(notes); ok {
		out = append(out, p)
	}
	helps := []string{"spyglass calls list --session " + shellQuote(s.SessionKey) + " --from " + shiftSeconds(s.FirstTS, 0) + " --to " + shiftSeconds(s.LastTS, 1)}
	if len(rows) > 0 {
		helps = append(helps, "spyglass calls show "+rows[0].ID)
	}
	return append(out, helpPair(helps)), nil
}

// shiftSeconds returns ts as an RFC 3339 time with whole seconds, moved by n
// seconds. A window from the first call and to one second past the last call
// holds the whole session, because from is inclusive and to is exclusive.
func shiftSeconds(ts string, n int) string {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	t = t.UTC().Truncate(time.Second)
	if n > 0 {
		t = t.Add(time.Duration(n) * time.Second)
	}
	return fmtTime(t)
}
