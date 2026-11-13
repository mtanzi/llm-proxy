package cli

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"time"

	"github.com/mtanzi/spyglass/internal/toon"
)

// topSessions is how many sessions the home view ranks.
const topSessions = 5

// home is the view of `spyglass` with no arguments (ADR 0055). Its window is
// fixed: the current UTC day, from 00:00 UTC to now. Both usage requests use
// grain=day, so each group is exactly one bucket: the totals, the percentile
// and the session ranking are exact, with no merge of buckets.
func (a *app) home(ctx context.Context) (toon.Object, error) {
	now := a.now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := ceilSecond(now)
	if !to.After(from) { // at 00:00:00 exactly, keep the window non-empty
		to = from.Add(time.Second)
	}
	w := window{From: from, To: to}
	q := url.Values{"from": {fmtTime(from)}, "to": {fmtTime(to)}, "grain": {"day"}}
	var total usageSeries
	if err := a.get(ctx, "/admin/usage", q, &total); err != nil {
		return nil, err
	}
	qs := url.Values{"from": q["from"], "to": q["to"], "grain": q["grain"], "group_by": {"session"}}
	var bySession usageSeries
	if err := a.get(ctx, "/admin/usage", qs, &bySession); err != nil {
		return nil, err
	}
	if len(total.Buckets) > 1 {
		return nil, &cmdError{code: 1, typ: "bad_response",
			msg:  fmt.Sprintf("the admin API returned %d day buckets for one UTC day", len(total.Buckets)),
			help: []string{"check that the server and the CLI are the same version: spyglass version"}}
	}

	out := toon.Object{{Key: "window", Value: w.object()}}
	var notes []string
	var t usageBucket
	if len(total.Buckets) == 1 {
		t = total.Buckets[0]
		cols, _ := todayCols.pick("home", "")
		out = append(out, toon.Pair{Key: "today", Value: objectOf(cols, &t)})
	} else {
		out = append(out, toon.Pair{Key: "today", Value: toon.Object{{Key: "calls", Value: 0}}})
		notes = append(notes, fmt.Sprintf("No calls between %s. A new UTC day starts empty; use spyglass calls list --since 24h for a longer window", w.describe()))
	}

	ranked, noSession := rankSessions(bySession.Buckets)
	if len(ranked) > topSessions {
		ranked = ranked[:topSessions]
	}
	cols, _ := topSessionCols.pick("home", "")
	out = append(out, toon.Pair{Key: "top_sessions", Value: tableOf(cols, ranked)})
	if noSession > 0 {
		out = append(out, toon.Pair{Key: "calls_without_session", Value: noSession})
	}

	if t.UnpricedCalls > 0 {
		notes = append(notes, fmt.Sprintf("%d %s no price: cost_usd leaves %s out, and unknown is not zero",
			t.UnpricedCalls, plural(t.UnpricedCalls, "call has", "calls have"), plural(t.UnpricedCalls, "it", "them")))
	}
	if p, ok := notesPair(notes); ok {
		out = append(out, p)
	}

	var helps []string
	if len(ranked) > 0 {
		helps = append(helps, "spyglass sessions show "+shellQuote(ranked[0].Group))
		for _, r := range ranked[1:] { // the first other session with unpriced calls
			if r.UnpricedCalls > 0 && ranked[0].UnpricedCalls == 0 {
				helps = append(helps, "spyglass sessions show "+shellQuote(r.Group))
				break
			}
		}
	}
	window := " --from " + fmtTime(from) + " --to " + fmtTime(to)
	if t.UnpricedCalls > 0 {
		helps = append(helps, "spyglass calls list"+window+" --fields id,model,provider,cost_usd   # unpriced calls show cost_usd: unpriced")
	}
	if t.Errors > 0 {
		helps = append(helps, "spyglass calls list"+window+" --fields id,model,status,error_class")
	}
	helps = append(helps, "spyglass config")
	return append(out, helpPair(helps)), nil
}

// rankSessions sorts the one-bucket-per-session series by cost, most expensive
// first, and sets aside the calls that have no session.
func rankSessions(buckets []usageBucket) (ranked []usageBucket, noSession int64) {
	for _, b := range buckets {
		if b.Group == "" {
			noSession += b.Calls
			continue
		}
		ranked = append(ranked, b)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		x, y := ranked[i], ranked[j]
		if x.CostMicroUSD != y.CostMicroUSD {
			return x.CostMicroUSD > y.CostMicroUSD
		}
		if x.Calls != y.Calls {
			return x.Calls > y.Calls
		}
		return x.Group < y.Group
	})
	return ranked, noSession
}
