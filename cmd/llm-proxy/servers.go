package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

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

// Servers are the two listeners of the binary. The caller starts them with
// ServeData and ServeAdmin and stops them with Shutdown.
type Servers struct {
	Data  *http.Server
	Admin *http.Server

	tls config.TLS
}

// BuildServers builds the data-plane and admin-plane servers from cfg, each
// behind the request chain. It refuses a non-loopback listen.admin, so the
// unauthenticated admin plane never leaves the host even when validation was
// skipped.
func BuildServers(cfg *config.Config, log *slog.Logger) (*Servers, error) {
	if !config.IsLoopback(cfg.Listen.Admin) {
		return nil, fmt.Errorf("listen.admin %q is not a loopback address; the admin plane must listen on 127.0.0.1, ::1 or localhost", cfg.Listen.Admin)
	}
	if cfg.Listen.TLS.Enabled() && (cfg.Listen.TLS.CertFile == "" || cfg.Listen.TLS.KeyFile == "") {
		return nil, errors.New("listen.tls: cert_file and key_file must be set together")
	}
	opts := httpmw.Options{Logger: log, MaxRequestBytes: int64(cfg.Server.MaxRequestBytes)}
	s := &Servers{
		Data:  newServer(cfg.Listen.Data, httpmw.Wrap(proxy.NewMux(), opts), log),
		Admin: newServer(cfg.Listen.Admin, httpmw.Wrap(api.NewMux(), opts), log),
		tls:   cfg.Listen.TLS,
	}
	if s.tls.Enabled() {
		s.Data.TLSConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return s, nil
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
