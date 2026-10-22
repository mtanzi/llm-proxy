package proxy

import "net/http"

// NewMux returns the data-plane mux: the routes of ADR 0006. The handlers are
// placeholders until the ADRs that own them land: forwarding (ADR 0011) and
// health (ADR 0026).
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/", notImplemented)
	mux.HandleFunc("HEAD /api/hello", notImplemented)
	mux.HandleFunc("/healthz", notImplemented)
	mux.HandleFunc("/readyz", notImplemented)
	return mux
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
