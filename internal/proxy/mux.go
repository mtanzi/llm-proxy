package proxy

import (
	"context"
	"errors"
	"net/http"

	"github.com/mtanzi/spyglass/internal/httpmw"
)

// NewMux returns the data-plane mux: the routes of ADR 0006. /healthz says the
// process is alive. /readyz says the config is loaded and drain has not
// started (ADR 0026); it is not tied to Postgres, because the data plane keeps
// working when the store is down. drain may be nil. Every other path gets the
// protocol 404.
func NewMux(h *Handler, drain *httpmw.Drain) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/v1/", h)
	mux.HandleFunc("HEAD /api/hello", Hello)
	mux.Handle("/healthz", httpmw.Healthz())
	mux.Handle("/readyz", httpmw.Readyz(drain, h.ConfigLoaded))
	mux.HandleFunc("/", NotFound)
	return mux
}

// ConfigLoaded is the data-plane readiness check: it fails until a config
// snapshot is in place.
func (h *Handler) ConfigLoaded(context.Context) error {
	if h.snaps.Current() == nil {
		return errors.New("proxy: config not loaded")
	}
	return nil
}
