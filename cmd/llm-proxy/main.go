// Command llm-proxy is the LLM proxy server binary.
package main

import (
	"fmt"
	"os"
)

// version is set at build time: go build -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	fmt.Fprintln(os.Stderr, "llm-proxy", version)
}
