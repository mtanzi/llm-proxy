package proxy

import "net/http"

// NewMux returns the data-plane mux: the routes of ADR 0006. Forwarding is
// ADR 0011; health is a placeholder until ADR 0026. Every other path gets the
// protocol 404.
func NewMux(h *Handler) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/v1/", h)
	mux.HandleFunc("HEAD /api/hello", Hello)
	mux.HandleFunc("/healthz", notImplemented)
	mux.HandleFunc("/readyz", notImplemented)
	mux.HandleFunc("/", NotFound)
	return mux
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
