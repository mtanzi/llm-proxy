package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/mtanzi/llm-proxy/internal/api"
	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/httpmw"
	"github.com/mtanzi/llm-proxy/internal/proxy"
)

// Server timeouts. There is deliberately no WriteTimeout: it would cut long
// SSE streams (ADR 0006).
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 2 * time.Minute
)

// Observability is what the servers need from the rest of the binary for ADR
// 0026. The composition root (ADR 0028) fills it.
type Observability struct {
	// Ping is the admin readiness check, Store.Ping. Nil reports not-ready.
	Ping func(context.Context) error
	// Collectors are registered next to the proxy's own: the recorder, the
	// config holder and the store's retention job.
	Collectors []prometheus.Collector
}

// Servers are the two listeners of the binary. The caller starts them with
// ServeData and ServeAdmin and stops them with Shutdown.
type Servers struct {
	Data  *http.Server
	Admin *http.Server

	// Drain is the shutdown flag. Drain.Start makes both /readyz endpoints
	// report not-ready; graceful shutdown (ADR 0027) calls it first.
	Drain *httpmw.Drain
	// Inflight is the number of streams being forwarded now, from the proxy
	// handler. Shutdown logs it when the drain deadline passes.
	Inflight func() int64
	// InflightCalls is the number of forwarded calls, streaming or not, whose
	// record has not reached the recorder yet. Shutdown waits for it after
	// cutting streams.
	InflightCalls func() int64
	// Registry is served at /metrics on the admin plane.
	Registry *prometheus.Registry

	tls config.TLS
}

// BuildServers builds the data-plane and admin-plane servers from cfg, each
// behind the request chain. It refuses a non-loopback listen.admin, so the
// unauthenticated admin plane never leaves the host even when validation was
// skipped. The data plane reads its providers and keys from snaps, one
// snapshot per request. The /metrics registry holds the proxy collectors and
// obs.Collectors.
func BuildServers(cfg *config.Config, snaps proxy.Snapshots, log *slog.Logger, obs Observability) (*Servers, error) {
	if !config.IsLoopback(cfg.Listen.Admin) {
		return nil, fmt.Errorf("listen.admin %q is not a loopback address; the admin plane must listen on 127.0.0.1, ::1 or localhost", cfg.Listen.Admin)
	}
	opts := httpmw.Options{Logger: log, MaxRequestBytes: maxRequestBytes(cfg, snaps)}
	pm := proxy.NewMetrics()
	reg := api.NewRegistry(append([]prometheus.Collector{pm}, obs.Collectors...)...)
	drain := &httpmw.Drain{}
	handler := proxy.NewHandler(snaps, proxy.Options{Logger: log, Metrics: pm})
	adminMux := api.NewMux(api.Options{Metrics: reg, Ping: obs.Ping, Drain: drain})
	s := &Servers{
		Data:          newServer(cfg.Listen.Data, httpmw.Wrap(proxy.NewMux(handler, drain), opts), log),
		Admin:         newServer(cfg.Listen.Admin, httpmw.Wrap(adminMux, opts), log),
		Drain:         drain,
		Inflight:      handler.InflightStreams,
		InflightCalls: handler.InflightCalls,
		Registry:      reg,
		tls:           cfg.Listen.TLS,
	}
	if s.tls.Enabled() {
		s.Data.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return s, nil
}

// maxRequestBytes returns the size limit for each request: the current
// snapshot's server.max_request_bytes, so a SIGHUP reload applies to the next
// request. Before the first snapshot it is the value of cfg.
func maxRequestBytes(cfg *config.Config, snaps proxy.Snapshots) func() int64 {
	return func() int64 {
		if snap := snaps.Current(); snap != nil {
			return int64(snap.Config().Server.MaxRequestBytes)
		}
		return int64(cfg.Server.MaxRequestBytes)
	}
}

func newServer(addr string, h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		IdleTimeout:       idleTimeout,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// ServeData serves the data plane on ln, over TLS when listen.tls is set.
func (s *Servers) ServeData(ln net.Listener) error {
	if s.tls.Enabled() {
		return s.Data.ServeTLS(ln, s.tls.CertFile, s.tls.KeyFile)
	}
	return s.Data.Serve(ln)
}

// ServeAdmin serves the admin plane on ln, always as plain HTTP.
func (s *Servers) ServeAdmin(ln net.Listener) error { return s.Admin.Serve(ln) }
