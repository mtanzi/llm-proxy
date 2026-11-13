package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"

	"github.com/mtanzi/spyglass/internal/toon"
)

type kind int

const (
	kString  kind = iota // one value, the last wins
	kStrings             // repeatable
	kBool                // true or false; a bare flag means true
	kInt                 // one non-negative integer
	kInts                // repeatable non-negative integers
)

// filterSpec is one API filter parameter and the flag that sets it. The flag
// name is the parameter name with _ written as - (ADR 0055).
type filterSpec struct {
	param string
	kind  kind
	help  string
}

func (s filterSpec) flagName() string { return strings.ReplaceAll(s.param, "_", "-") }

// queryFlag is a flag.Value that writes into the query of a request.
type queryFlag struct {
	q     url.Values
	param string
	kind  kind
}

func (f *queryFlag) String() string {
	if f == nil || f.q == nil {
		return ""
	}
	return strings.Join(f.q[f.param], ",")
}

func (f *queryFlag) IsBoolFlag() bool { return f != nil && f.kind == kBool }

func (f *queryFlag) Set(v string) error {
	switch f.kind {
	case kBool:
		b, err := strconv.ParseBool(v)
		if err != nil {
			return fmt.Errorf("%q is not true or false", v)
		}
		v = strconv.FormatBool(b)
	case kInt, kInts:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return fmt.Errorf("%q is not a non-negative integer", v)
		}
		v = strconv.FormatInt(n, 10)
	}
	if f.kind == kStrings || f.kind == kInts {
		f.q.Add(f.param, v)
	} else {
		f.q.Set(f.param, v)
	}
	return nil
}

// registerFilters adds one flag per filter spec, all writing into q.
func registerFilters(fs *flag.FlagSet, q url.Values, specs []filterSpec) {
	for _, s := range specs {
		fs.Var(&queryFlag{q: q, param: s.param, kind: s.kind}, s.flagName(), s.help)
	}
}

// filtersObject echoes the filters in effect, in spec order, one string per
// parameter (repeated values joined by a comma).
func filtersObject(q url.Values, specs []filterSpec) toon.Object {
	var o toon.Object
	for _, s := range specs {
		vs := q[s.param]
		switch {
		case len(vs) == 0:
		case s.kind == kBool:
			o = append(o, toon.Pair{Key: s.param, Value: vs[0] == "true"})
		case s.kind == kInt:
			n, _ := strconv.ParseInt(vs[0], 10, 64)
			o = append(o, toon.Pair{Key: s.param, Value: n})
		default:
			o = append(o, toon.Pair{Key: s.param, Value: strings.Join(vs, ",")})
		}
	}
	return o
}

// filtersText is the filters in a sentence of an empty state.
func filtersText(q url.Values, specs []filterSpec) string {
	var parts []string
	for _, s := range specs {
		if vs := q[s.param]; len(vs) > 0 {
			parts = append(parts, s.param+"="+strings.Join(vs, "|"))
		}
	}
	if len(parts) == 0 {
		return "no filters"
	}
	return "filters " + strings.Join(parts, ", ")
}

// newFlagSet makes a flag set that prints nothing itself: the CLI formats
// usage errors and --help.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	return fs
}

// parseInterleaved parses flags that may come before or after positional
// arguments and returns the positionals.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

// helpRequested marks a --help request: the text goes to stdout, exit 0.
type helpRequested struct{ text string }

func (h helpRequested) Error() string { return "help requested" }

// helpText renders --help of one command: usage, a line of purpose, at least
// one example, and the flags.
func helpText(fs *flag.FlagSet, usage, purpose string, examples ...string) string {
	var b strings.Builder
	b.WriteString("usage: " + usage + "\n\n" + purpose + "\n\nexamples:\n")
	for _, e := range examples {
		b.WriteString("  " + e + "\n")
	}
	flags := false
	fs.VisitAll(func(*flag.Flag) { flags = true })
	if flags {
		b.WriteString("\nflags:\n")
		fs.SetOutput(&b)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	return b.String()
}

// parseFlags parses args and maps flag errors: --help to helpRequested and
// anything else to a usage error with exit code 2.
func parseFlags(fs *flag.FlagSet, cmd string, args []string, help func() string) ([]string, error) {
	pos, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		return nil, helpRequested{text: help()}
	}
	if err != nil {
		return nil, usageError(cmd, "%s", err.Error())
	}
	return pos, nil
}

// limitFlag registers --limit with the default 20 and checks 1..max.
func limitFlag(fs *flag.FlagSet, def int) *int {
	return fs.Int("limit", def, "rows to show; the CLI default is 20, below the API default of 50")
}

func checkLimit(cmd string, n, max int) error {
	if n < 1 || n > max {
		return usageError(cmd, "--limit %d is outside 1..%d", n, max)
	}
	return nil
}
