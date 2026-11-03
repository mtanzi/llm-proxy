package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/mtanzi/llm-proxy/internal/httpmw"
)

// Options configures NewMux.
type Options struct {
	// Metrics is served at /metrics. Nil leaves the route unimplemented.
	Metrics prometheus.Gatherer
	// Ping is the admin readiness check: the store answers (Store.Ping). Nil
	// reports not-ready, so a wiring that forgets it fails loudly.
	Ping func(context.Context) error
	// Drain is the shutdown flag shared with the data plane. Nil never drains.
	Drain *httpmw.Drain
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
// has not started, and /metrics serves the registry (ADR 0026). The admin API
// (ADR 0023) and the UI (ADR 0029) are placeholders until those ADRs land.
func NewMux(o Options) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/", notImplemented)
	mux.HandleFunc("/ui/", notImplemented)
	if o.Metrics != nil {
		mux.Handle("/metrics", promhttp.HandlerFor(o.Metrics, promhttp.HandlerOpts{}))
	} else {
		mux.HandleFunc("/metrics", notImplemented)
	}
	mux.Handle("/healthz", httpmw.Healthz())
	ping := o.Ping
	if ping == nil {
		ping = func(context.Context) error { return errors.New("api: no store to ping") }
	}
	mux.Handle("/readyz", httpmw.Readyz(o.Drain, ping))
	return mux
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
