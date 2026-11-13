package cli

import (
	"fmt"
	"math"
	"strings"

	"github.com/mtanzi/spyglass/internal/toon"
)

// cmdError is a failure a command reports as a structured TOON object on
// stdout. Exit code 1 is an error (API error, unreachable server, bad
// response); 2 is a usage error.
type cmdError struct {
	code  int
	typ   string
	msg   string
	param string
	help  []string
}

func (e *cmdError) Error() string { return e.typ + ": " + e.msg }

func (e *cmdError) object() toon.Object {
	o := toon.Object{{Key: "error", Value: e.typ}, {Key: "message", Value: e.msg}}
	if e.param != "" {
		o = append(o, toon.Pair{Key: "parameter", Value: e.param})
	}
	if len(e.help) > 0 {
		o = append(o, helpPair(e.help))
	}
	return o
}

func usageError(cmd, format string, args ...any) *cmdError {
	return &cmdError{code: 2, typ: "usage", msg: fmt.Sprintf(format, args...),
		help: []string{"spyglass " + cmd + " --help"}}
}

func helpPair(lines []string) toon.Pair {
	a := make(toon.List, len(lines))
	for i, l := range lines {
		a[i] = l
	}
	return toon.Pair{Key: "help", Value: a}
}

func notesPair(notes []string) (toon.Pair, bool) {
	if len(notes) == 0 {
		return toon.Pair{}, false
	}
	a := make(toon.List, len(notes))
	for i, n := range notes {
		a[i] = n
	}
	return toon.Pair{Key: "notes", Value: a}, true
}

// cutText cuts s to n characters and says so, so a reader knows text is gone.
func cutText(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return fmt.Sprintf("%s... (cut at %d characters)", string(r[:n]), n)
}

// unpriced is what a cost prints when no price matched. Unknown is not zero
// (ADR 0052).
const unpriced = "unpriced"

// usdFromMicro converts micro-USD to USD, rounded to four decimals.
func usdFromMicro(micro int64) float64 {
	neg := micro < 0
	if neg {
		micro = -micro
	}
	r := (micro + 50) / 100 // units of 0.0001 USD
	v := float64(r) / 10000
	if neg {
		v = -v
	}
	return v
}

// costUSD is the cost_usd cell of a row whose cost may be null.
func costUSD(micro *int64) any {
	if micro == nil {
		return unpriced
	}
	return usdFromMicro(*micro)
}

// ratio rounds a ratio to four decimals.
func ratio(num, den int64) float64 {
	if den == 0 {
		return 0
	}
	return math.Round(float64(num)/float64(den)*10000) / 10000
}

// perMTok converts micro-USD per million tokens to USD per million tokens.
func perMTok(micro int64) float64 { return float64(micro) / 1e6 }

var shellSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+=:,./-"

// shellQuote quotes s for a POSIX shell when it needs it, so a help line can
// be pasted.
func shellQuote(s string) string {
	if s != "" && strings.Trim(s, shellSafe) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
