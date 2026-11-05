// Command fixturecap refreshes the golden fixtures of testdata/fixtures from
// real traffic (ADR 0038). It has two steps:
//
//	fixturecap record -out DIR        a pass-through recorder: point a client at it
//	                                  and every exchange lands in DIR/NNNN
//	fixturecap scrub  -in DIR/NNNN -out testdata/fixtures/NAME
//	                                  turn one raw exchange into a fixture
//
// A raw capture holds real prompts and responses, so it stays outside the
// repository. Only the output of scrub is committed. The procedure is in
// testdata/fixtures/README.md.
package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "record":
		return runRecord(args[1:], stderr)
	case "scrub":
		return runScrub(args[1:], stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	}
	_, _ = fmt.Fprintf(stderr, "fixturecap: unknown command %q\n", args[0])
	usage(stderr)
	return 2
}

func usage(w io.Writer) {
	_, _ = fmt.Fprint(w, `usage:
  fixturecap record -out DIR [-listen 127.0.0.1:8081] [-upstream https://api.anthropic.com]
  fixturecap scrub  -in DIR/NNNN -out testdata/fixtures/NAME

record is a pass-through proxy: set ANTHROPIC_BASE_URL of the client to its
address. Raw captures hold real prompts; keep DIR outside the repository.
scrub removes credentials and replaces all prompt, response and tool text with
placeholder text of the same length, then checks the result for leaks.
`)
}
