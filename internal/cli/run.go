// Package cli is the agent-facing, read-only client of the Spyglass admin API
// (ADR 0055, docs/architecture/cli.md). Commands print TOON on stdout, made
// for agents and equally usable by people, scripts and CI.
//
// The package is a pure API client. It owns small decode types checked
// against api/openapi.yaml, and it imports no server package: not
// internal/api, internal/config, core or a store (see the depguard rule).
// A command maps flags to request parameters, calls the admin client and
// shapes the result with its column table. There is no service layer and no
// renderer interface.
package cli

import (
	"context"
	"errors"
	"flag"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/mtanzi/spyglass/internal/toon"
)

const topHelp = `usage: spyglass [command] [flags]

With no command, spyglass shows today's cost, calls and errors and the most
expensive sessions. Output is TOON on stdout. Every command is read-only.

commands:
  describe        the data model: every field with its unit, the default window
  config          providers, pricing per model, virtual key names
  sessions list   sessions in a window, newest first
  sessions show   summary, agents, prompts and calls of one session
  calls list      calls in a window, newest first
  calls show      every recorded field of one call
  serve           run the proxy
  keygen          generate a virtual key and its config entry
  version         print the build version

The admin API is read from SPYGLASS_ADMIN_URL (default http://127.0.0.1:9090).
Run "spyglass <command> --help" for flags. Exit codes: 0 success, 1 error,
2 usage error.

examples:
  spyglass
  spyglass calls list --since 1h --model claude-opus-4-8
  spyglass sessions show team/a:1
`

// app is one invocation: the environment, the clock and the admin client.
type app struct {
	env    func(string) string
	now    func() time.Time
	client *adminClient
}

// Run executes a client command and returns the process exit code: 0 for
// success, 1 for an error (API error, unreachable server, bad response) and 2
// for a usage error. No arguments run the home view. Output and errors are TOON
// objects on stdout; stderr is not used. env reads an environment variable.
func Run(args []string, stdout, stderr io.Writer, env func(string) string) int {
	return run(args, stdout, env, time.Now)
}

func run(args []string, stdout io.Writer, env func(string) string, now func() time.Time) int {
	a := &app{env: env, now: now}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out, err := a.dispatch(ctx, args)
	var h helpRequested
	if errors.As(err, &h) {
		_, _ = io.WriteString(stdout, h.text)
		return 0
	}
	code := 0
	if err != nil {
		ce, ok := err.(*cmdError)
		if !ok {
			ce = &cmdError{code: 1, typ: "internal", msg: err.Error()}
		}
		out, code = ce.object(), ce.code
	}
	s, encErr := toon.Encode(out)
	if encErr != nil {
		s, code = "error: internal\nmessage: "+encErr.Error(), 1
	}
	_, _ = io.WriteString(stdout, s+"\n")
	return code
}

func (a *app) dispatch(ctx context.Context, args []string) (toon.Object, error) {
	if len(args) == 0 {
		return a.home(ctx)
	}
	rest := args[1:]
	switch args[0] {
	case "-h", "--help", "help":
		return nil, helpRequested{text: topHelp}
	case "describe":
		return a.describe(ctx, rest)
	case "config":
		return a.config(ctx, rest)
	case "sessions":
		return a.sub(ctx, "sessions", rest, map[string]func(context.Context, []string) (toon.Object, error){
			"list": a.sessionsList, "show": a.sessionsShow})
	case "calls":
		return a.sub(ctx, "calls", rest, map[string]func(context.Context, []string) (toon.Object, error){
			"list": a.callsList, "show": a.callsShow})
	}
	return nil, &cmdError{code: 2, typ: "usage", msg: "unknown command " + quoteArg(args[0]),
		help: []string{"spyglass --help"}}
}

func quoteArg(s string) string { return shellQuote(cutText(s, 60)) }

// sub dispatches `<group> <verb>`.
func (a *app) sub(ctx context.Context, group string, args []string, verbs map[string]func(context.Context, []string) (toon.Object, error)) (toon.Object, error) {
	if len(args) > 0 {
		if f, ok := verbs[args[0]]; ok {
			return f(ctx, args[1:])
		}
	}
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		return nil, helpRequested{text: "usage: spyglass " + group + " list|show [flags]\n\nexamples:\n  spyglass " + group + " list --limit 5\n  spyglass " + group + " show <id>\n"}
	}
	return nil, &cmdError{code: 2, typ: "usage", msg: "spyglass " + group + " needs list or show",
		help: []string{"spyglass " + group + " list", "spyglass " + group + " --help"}}
}

// admin returns the client, built from SPYGLASS_ADMIN_URL on first use.
func (a *app) admin() (*adminClient, error) {
	if a.client != nil {
		return a.client, nil
	}
	c, err := newAdminClient(a.env)
	if err != nil {
		return nil, err
	}
	a.client = c
	return c, nil
}

func (a *app) get(ctx context.Context, path string, q url.Values, out any) error {
	c, err := a.admin()
	if err != nil {
		return err
	}
	return c.get(ctx, path, q, out)
}

// noArgs rejects a positional argument of a command that takes none.
func noArgs(cmd string, pos []string) error {
	if len(pos) > 0 {
		return usageError(cmd, "unexpected argument %s", quoteArg(pos[0]))
	}
	return nil
}

// nextCommand rebuilds the command line of the flags in effect, with the
// window pinned and a cursor set, for the help line of the next page.
func nextCommand(fs *flag.FlagSet, base string, w window, cursor string) string {
	var b strings.Builder
	b.WriteString(base)
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "cursor", "from", "to", "since":
			return
		}
		if qf, ok := f.Value.(*queryFlag); ok {
			for _, v := range qf.q[qf.param] {
				b.WriteString(" --" + f.Name + "=" + shellQuote(v))
			}
			return
		}
		b.WriteString(" --" + f.Name + "=" + shellQuote(f.Value.String()))
	})
	b.WriteString(" --from " + fmtTime(w.From) + " --to " + fmtTime(w.To))
	if cursor != "" {
		b.WriteString(" --cursor " + shellQuote(cursor))
	}
	return b.String()
}
