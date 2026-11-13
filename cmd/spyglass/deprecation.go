package main

import (
	"log/slog"
	"sync"
)

// envWarnings warns once per deprecated LLM_PROXY_ variable that the config
// reads in place of its unset SPYGLASS_ name (ADR 0054). Names read before the
// logger exists are kept and logged by setLogger. It never sees a value.
type envWarnings struct {
	mu      sync.Mutex
	log     *slog.Logger
	seen    map[string]bool
	pending [][2]string
}

// note is config.Options.Deprecated. Each legacy name is warned about once.
func (e *envWarnings) note(legacy, name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seen[legacy] {
		return
	}
	if e.seen == nil {
		e.seen = map[string]bool{}
	}
	e.seen[legacy] = true
	if e.log == nil {
		e.pending = append(e.pending, [2]string{legacy, name})
		return
	}
	warnEnv(e.log, legacy, name)
}

// setLogger logs the names noted so far, then logs later ones at once.
func (e *envWarnings) setLogger(log *slog.Logger) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.log = log
	for _, p := range e.pending {
		warnEnv(log, p[0], p[1])
	}
	e.pending = nil
}

func warnEnv(log *slog.Logger, legacy, name string) {
	log.Warn("deprecated environment variable; rename it", "variable", legacy, "use", name)
}
