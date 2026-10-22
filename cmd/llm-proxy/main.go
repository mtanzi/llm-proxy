// Command llm-proxy is the LLM proxy server binary.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is set at build time: go build -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `usage: llm-proxy <command> [flags]

commands:
  serve    run the proxy
  keygen   generate a virtual key and its config entry
  version  print the build version
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run dispatches a subcommand and returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = io.WriteString(stderr, usage)
		return 2
	}
	switch args[0] {
	case "keygen":
		return runKeygen(args[1:], stdout, stderr)
	case "version":
		_, _ = fmt.Fprintln(stdout, version)
		return 0
	case "serve":
		// Startup wiring belongs to ADR 0028.
		_, _ = fmt.Fprintln(stderr, "llm-proxy: serve is not implemented yet")
		return 1
	case "-h", "-help", "--help", "help":
		_, _ = io.WriteString(stdout, usage)
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "llm-proxy: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
