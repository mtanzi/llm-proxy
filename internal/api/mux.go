package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mtanzi/llm-proxy/internal/config"
	"github.com/mtanzi/llm-proxy/internal/core"
	"github.com/mtanzi/llm-proxy/internal/httpmw"
	"github.com/mtanzi/llm-proxy/internal/ui"
)

// Options configures NewMux.
type Options struct {
	// Metrics is served at /metrics. It must not be nil.
	Metrics prometheus.Gatherer
	// Ping is the admin readiness check: the store answers (Store.Ping). Nil
	// reports not-ready, so a wiring that forgets it fails loudly.
	Ping func(context.Context) error
	// Drain is the shutdown flag shared with the data plane. Nil never drains.
	Drain *httpmw.Drain
	// Calls serves the call-log, session, tool-call and usage endpoints.
	Calls core.CallReader
	// Config returns the redacted config view of the running snapshot for
	// GET /admin/config.
	Config func() config.View
	// Now is the clock of the default time range. Nil uses time.Now.
	Now func() time.Time
	// A nil Calls or Config makes its endpoints answer 503, so a wiring that
	// forgets one fails loudly (the composition root is ADR 0028).
}

// NewRegistry returns the Prometheus registry of the binary: the Go runtime
// and process collectors plus cs. Each package declares the collectors it
// updates; the composition root passes them here. It panics on a duplicate.
func NewRegistry(cs ...prometheus.Collector) *prometheus.Registry {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	reg.MustRegister(cs...)
	return reg
}

// NewMux returns the admin-plane mux: the routes of ADR 0006. It sets no CORS
// headers and checks no credentials; the loopback bind is the protection.
// /healthz says the process is alive, /readyz says the store answers and drain
// has not started, and /metrics serves the registry (ADR 0026). Under /admin/
// it serves the call-log endpoints, the config view and the OpenAPI document
// (ADR 0023), and the session, tool-call and usage endpoints (ADR 0024). Under
// /ui/ it serves the embedded web UI (ADR 0029).
func NewMux(o Options) *http.ServeMux {
	now := o.Now
	if now == nil {
		now = time.Now
	}
	h := &handlers{calls: o.Calls, config: o.Config, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/", notFound)
	mux.HandleFunc("GET /admin/calls", h.listCalls)
	mux.HandleFunc("GET /admin/calls/{id}", h.getCall)
	mux.HandleFunc("GET /admin/sessions", h.listSessions)
	mux.HandleFunc("GET /admin/sessions/{key}", h.getSession)
	mux.HandleFunc("GET /admin/tool-calls", h.listToolCalls)
	mux.HandleFunc("GET /admin/tool-calls/summary", h.toolSummary)
	mux.HandleFunc("GET /admin/usage", h.getUsage)
	mux.HandleFunc("GET /admin/config", h.getConfig)
	mux.HandleFunc("GET /admin/openapi.json", h.getOpenAPI)
	ui.Mount(mux)
	mux.Handle("/metrics", promhttp.HandlerFor(o.Metrics, promhttp.HandlerOpts{}))
	mux.Handle("/healthz", httpmw.Healthz())
	ping := o.Ping
	if ping == nil {
		ping = func(context.Context) error { return errors.New("api: no store to ping") }
	}
	mux.Handle("/readyz", httpmw.Readyz(o.Drain, ping))
	return mux
}
