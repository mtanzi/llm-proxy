package cli

import (
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/mtanzi/spyglass/internal/toon"
)

// window is the time range a command reads. Default is true when the user gave
// no window flag, so the range is the API default of the last 24 hours.
type window struct {
	From, To time.Time
	Default  bool
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// ceilSecond rounds t up to a whole second in UTC. A window that ends at now
// uses it: to is exclusive and printed in whole seconds, so rounding down would
// drop a call recorded earlier in the current second.
func ceilSecond(t time.Time) time.Time {
	t = t.UTC()
	if c := t.Truncate(time.Second); c.Before(t) {
		return c.Add(time.Second)
	}
	return t
}

func (w window) object() toon.Object {
	return toon.Object{{Key: "from", Value: fmtTime(w.From)}, {Key: "to", Value: fmtTime(w.To)}, {Key: "default", Value: w.Default}}
}

func (w window) describe() string {
	return fmt.Sprintf("%s to %s UTC", fmtTime(w.From), fmtTime(w.To))
}

// windowFlags holds --from, --to and --since of a command.
type windowFlags struct{ from, to, since string }

func (w *windowFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&w.from, "from", "", "start of the window, RFC 3339 (default: 24 hours before --to)")
	fs.StringVar(&w.to, "to", "", "end of the window, RFC 3339 (default: now)")
	fs.StringVar(&w.since, "since", "", "window start as a duration before now: 30m, 24h or 7d (not with --from)")
}

var sinceRE = regexp.MustCompile(`^([0-9]+)([mhd])$`)

// parseSince parses an integer and one unit: m, h or d (24 hours).
// time.ParseDuration has no d unit.
func parseSince(s string) (time.Duration, error) {
	m := sinceRE.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("--since %q is not a duration: use an integer and m, h or d, for example 30m, 24h or 7d", s)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 || n > 100000 {
		return 0, fmt.Errorf("--since %q: the number must be between 1 and 100000", s)
	}
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, nil
}

// resolve turns the flags into a window. A flag given sets that side; --since
// sets from; the other side takes the API default.
func (w *windowFlags) resolve(now time.Time) (window, error) {
	now = ceilSecond(now)
	out := window{To: now, Default: w.from == "" && w.to == "" && w.since == ""}
	if w.since != "" && w.from != "" {
		return out, fmt.Errorf("--since and --from both set the start of the window: use one")
	}
	if w.to != "" {
		t, err := time.Parse(time.RFC3339, w.to)
		if err != nil {
			return out, fmt.Errorf("--to %q is not an RFC 3339 time, for example 2026-11-13T14:20:00Z", w.to)
		}
		out.To = t.UTC()
	}
	switch {
	case w.since != "":
		d, err := parseSince(w.since)
		if err != nil {
			return out, err
		}
		out.From = now.Add(-d)
	case w.from != "":
		t, err := time.Parse(time.RFC3339, w.from)
		if err != nil {
			return out, fmt.Errorf("--from %q is not an RFC 3339 time, for example 2026-11-13T00:00:00Z", w.from)
		}
		out.From = t.UTC()
	default:
		out.From = out.To.Add(-24 * time.Hour)
	}
	if !out.From.Before(out.To) {
		return out, fmt.Errorf("the window is empty: from %s is not before to %s", fmtTime(out.From), fmtTime(out.To))
	}
	return out, nil
}
