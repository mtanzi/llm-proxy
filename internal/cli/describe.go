package cli

import (
	"context"
	"strings"

	"github.com/mtanzi/spyglass/internal/toon"
)

// neverStored lists what Spyglass does not keep (ADR 0002).
var neverStored = []string{
	"request and response bodies",
	"prompt and message text",
	"tool arguments and tool results (only their sizes)",
	"provider credentials and the virtual key itself (only its SHA-256 and its name)",
}

var commandDocs = [][]any{
	{"spyglass", "today's cost, calls and errors, and the most expensive sessions", "GET /admin/usage (twice, grain=day, the current UTC day)"},
	{"spyglass describe", "this data model", "the column tables and GET /admin/config"},
	{"spyglass config", "providers, pricing per model, key names", "GET /admin/config"},
	{"spyglass sessions list", "sessions in a window, newest first", "GET /admin/sessions"},
	{"spyglass sessions show <key>", "one session: summary, agents, prompts, calls", "GET /admin/sessions/{key}"},
	{"spyglass calls list", "calls in a window, newest first", "GET /admin/calls"},
	{"spyglass calls show <id>", "every recorded field of one call", "GET /admin/calls/{id}"},
}

func (a *app) describe(ctx context.Context, args []string) (toon.Object, error) {
	const cmd = "describe"
	fs := newFlagSet(cmd)
	help := func() string {
		return helpText(fs, "spyglass describe",
			"The data model: every field with its unit, the default window, what is never stored, and the providers and key names of the running server.",
			"spyglass describe")
	}
	pos, err := parseFlags(fs, cmd, args, help)
	if err != nil {
		return nil, err
	}
	if err := noArgs(cmd, pos); err != nil {
		return nil, err
	}
	var cfg configView
	if err := a.get(ctx, "/admin/config", nil, &cfg); err != nil {
		return nil, err
	}

	never := make(toon.List, len(neverStored))
	for i, s := range neverStored {
		never[i] = s
	}
	out := toon.Object{
		{Key: "about", Value: "Spyglass records the metadata of every LLM call: tokens, cost, latency, errors, sessions, agents and tool calls."},
		{Key: "windows", Value: toon.Object{
			{Key: "default", Value: "the last 24 hours, the API default; set with --from, --to or --since (30m, 24h, 7d)"},
			{Key: "home", Value: "the current UTC day, from 00:00 UTC to now"},
		}},
		{Key: "conventions", Value: toon.List{
			"cost_usd is USD rounded to four decimals; unpriced means no price matched, and unknown is not zero",
			"a session cost sums the priced calls only; unpriced_calls counts the calls left out",
			"api_equivalent true means the cost is an API-equivalent price, not a bill (client_passthrough provider)",
			"latency_ms, duration_ms and ttft_ms are milliseconds; token fields are counts",
			"times are RFC 3339 UTC",
		}},
		{Key: "never_stored", Value: never},
		{Key: "commands", Value: toon.Table{Fields: []string{"command", "shows", "reads"}, Rows: commandDocs}},
	}
	for _, t := range allTables() {
		rows := make([][]any, len(t.Cols))
		for i, c := range t.Cols {
			rows[i] = []any{c.Name, c.Unit, c.Deflt, strings.Join(c.API, "+"), c.Desc}
		}
		out = append(out, toon.Pair{Key: strings.ReplaceAll(t.ID, ".", "_") + "_fields",
			Value: toon.Table{Fields: []string{"name", "unit", "default", "reads", "description"}, Rows: rows}})
	}
	provs := make([][]any, len(cfg.Providers))
	for i := range cfg.Providers {
		provs[i] = []any{cfg.Providers[i].Name, cfg.Providers[i].Kind, cfg.Providers[i].AuthMode}
	}
	keys := make([][]any, len(cfg.Keys))
	for i := range cfg.Keys {
		keys[i] = []any{cfg.Keys[i].Name, cfg.Keys[i].Disabled}
	}
	out = append(out,
		toon.Pair{Key: "providers", Value: toon.Table{Fields: []string{"name", "kind", "auth_mode"}, Rows: provs}},
		toon.Pair{Key: "keys", Value: toon.Table{Fields: []string{"name", "disabled"}, Rows: keys}},
		helpPair([]string{"spyglass", "spyglass calls list --help", "spyglass sessions list --help"}),
	)
	return out, nil
}
