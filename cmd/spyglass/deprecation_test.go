package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestEnvWarningsOncePerNameAndBufferedUntilLogger(t *testing.T) {
	var e envWarnings
	e.note("LLM_PROXY_CONFIG", "SPYGLASS_CONFIG")
	e.note("LLM_PROXY_CONFIG", "SPYGLASS_CONFIG")

	var buf bytes.Buffer
	e.setLogger(slog.New(slog.NewTextHandler(&buf, nil)))
	e.note("LLM_PROXY_LOG_LEVEL", "SPYGLASS_LOG_LEVEL")
	e.note("LLM_PROXY_CONFIG", "SPYGLASS_CONFIG") // a reload reads it again
	e.note("LLM_PROXY_LOG_LEVEL", "SPYGLASS_LOG_LEVEL")

	out := buf.String()
	if n := strings.Count(out, "deprecated environment variable"); n != 2 {
		t.Fatalf("warnings = %d, want 2:\n%s", n, out)
	}
	for _, want := range []string{"variable=LLM_PROXY_CONFIG use=SPYGLASS_CONFIG", "variable=LLM_PROXY_LOG_LEVEL use=SPYGLASS_LOG_LEVEL"} {
		if !strings.Contains(out, want) {
			t.Errorf("logs lack %q:\n%s", want, out)
		}
	}
}
