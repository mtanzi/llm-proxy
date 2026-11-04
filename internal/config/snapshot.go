package config

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// Reload results, the values of the result label of
// llm_proxy_config_reloads_total (exported by ADR 0026).
const (
	ResultSuccess = "success"
	ResultFailure = "failure"
)

// Snapshot is one immutable, fully resolved configuration: providers, resolved
// credentials, pricing and the key entries. A request reads the current
// snapshot once at its start and keeps it for its whole life, so a reload never
// changes a request in flight. Nothing may modify the Config a snapshot returns.
type Snapshot struct {
	cfg  *Config
	keys KeyIndex
}

// KeyIndex finds a virtual key by the lowercase hex SHA-256 of its plaintext.
// keyring.Keyring implements it.
type KeyIndex interface {
	Lookup(sha256 string) (core.Key, bool)
}

// Keys returns the key index built from this snapshot's keys, or nil when the
// holder has no Options.BuildKeys. It is immutable and shares the snapshot's
// life, so a reload never changes the index a request already holds.
func (s *Snapshot) Keys() KeyIndex { return s.keys }

// Config returns the validated configuration. Treat it as read-only.
func (s *Snapshot) Config() *Config { return s.cfg }

// Holder owns the current Snapshot behind an atomic pointer. Current is safe
// for any number of concurrent readers while Reload swaps the snapshot.
type Holder struct {
	path string
	opts Options
	log  *slog.Logger

	cur atomic.Pointer[Snapshot]
	mu  sync.Mutex // serialises Load and Reload

	succeeded atomic.Uint64
	failed    atomic.Uint64
}

// NewHolder returns a holder for the config file at path. It reads nothing
// until Load. A nil logger discards log output.
func NewHolder(path string, o Options, log *slog.Logger) *Holder {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Holder{path: path, opts: o, log: log}
}

// Load reads the config file and the secrets it references, and installs the
// first snapshot. A failure leaves the holder empty. Start-up counts nothing.
func (h *Holder) Load() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cur.Load() != nil {
		return errors.New("config already loaded; use Reload")
	}
	snap, err := h.build()
	if err != nil {
		return err
	}
	h.cur.Store(snap)
	return nil
}

// build loads the config and builds the key index, so a snapshot is whole or
// not made.
func (h *Holder) build() (*Snapshot, error) {
	cfg, err := Load(h.path, h.opts)
	if err != nil {
		return nil, err
	}
	snap := &Snapshot{cfg: cfg}
	if h.opts.BuildKeys != nil {
		if snap.keys, err = h.opts.BuildKeys(cfg.Keys); err != nil {
			return nil, fmt.Errorf("build keys: %w", err)
		}
	}
	return snap, nil
}

// Current returns the current snapshot, or nil before a successful Load.
func (h *Holder) Current() *Snapshot { return h.cur.Load() }

// Reload re-reads the file and the referenced secrets, validates the result and
// swaps it in atomically. A failure rejects the new config whole: the previous
// snapshot stays, the error is logged and counted, and Reload returns it.
// Restart-only settings keep their running values; a change to one logs a
// warning and is ignored. Reload logs the outcome, so callers need not.
func (h *Holder) Reload() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	prev := h.cur.Load()
	if prev == nil {
		return errors.New("config not loaded; call Load first")
	}
	snap, err := h.build()
	if err != nil {
		h.failed.Add(1)
		h.log.Error("config reload rejected; keeping the previous config",
			"path", h.path, "error", err)
		return err
	}
	cfg := snap.cfg
	if changed := keepRestartOnly(cfg, prev.cfg); len(changed) > 0 {
		h.log.Warn("config reload changes settings that need a restart; the running values stay",
			"settings", changed)
	}
	h.cur.Store(snap)
	h.succeeded.Add(1)
	h.log.Info("config reloaded", "path", h.path,
		"providers", len(cfg.Providers), "keys", len(cfg.Keys), "pricing", len(cfg.Pricing))
	return nil
}

// Reloads returns how many reloads ended with the given result, ResultSuccess
// or ResultFailure. It is the source of llm_proxy_config_reloads_total{result}.
func (h *Holder) Reloads(result string) uint64 {
	switch result {
	case ResultSuccess:
		return h.succeeded.Load()
	case ResultFailure:
		return h.failed.Load()
	}
	return 0
}

// WatchReload calls Reload for each value received on sigs until ctx ends or
// sigs closes. Reload logs the outcome with its cause; a failure never stops
// the loop.
func (h *Holder) WatchReload(ctx context.Context, sigs <-chan os.Signal) {
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-sigs:
			if !ok {
				return
			}
			_ = h.Reload()
		}
	}
}

// ReloadOnSIGHUP reloads the config on each SIGHUP until ctx ends.
func (h *Holder) ReloadOnSIGHUP(ctx context.Context) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	defer signal.Stop(sigs)
	h.WatchReload(ctx, sigs)
}

// keepRestartOnly copies the restart-only settings of the running config into
// next, and returns the names of those that differ. These settings go into
// constructors or listeners once, at start: listener addresses, TLS files,
// database URL and pool size, queue capacity, call retention, log level and
// format, and the drain timeout. Everything else reloads, among it
// server.max_request_bytes, which each request reads from its snapshot. The
// names are safe to log; the database URL is compared by source and value but
// never printed.
func keepRestartOnly(next, running *Config) []string {
	var changed []string
	note := func(name string, differs bool) {
		if differs {
			changed = append(changed, name)
		}
	}
	note("listen.data", next.Listen.Data != running.Listen.Data)
	note("listen.admin", next.Listen.Admin != running.Listen.Admin)
	note("listen.tls.cert_file", next.Listen.TLS.CertFile != running.Listen.TLS.CertFile)
	note("listen.tls.key_file", next.Listen.TLS.KeyFile != running.Listen.TLS.KeyFile)
	note("database.url", next.Database.URL.String() != running.Database.URL.String() ||
		next.Database.URL.Secret().Reveal() != running.Database.URL.Secret().Reveal())
	note("database.max_conns", next.Database.MaxConns != running.Database.MaxConns)
	note("queue_capacity", next.QueueCapacity != running.QueueCapacity)
	note("retention.calls", next.Retention.Calls != running.Retention.Calls)
	note("server.drain_timeout", next.Server.DrainTimeout != running.Server.DrainTimeout)
	note("log.level", next.Log.Level != running.Log.Level)
	note("log.format", next.Log.Format != running.Log.Format)

	next.Listen = running.Listen
	next.Database = running.Database
	next.QueueCapacity = running.QueueCapacity
	next.Retention = running.Retention
	next.Server.DrainTimeout = running.Server.DrainTimeout
	next.Log = running.Log
	return changed
}
