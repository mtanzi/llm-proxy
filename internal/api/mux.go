package api

import "net/http"

// NewMux returns the admin-plane mux: the routes of ADR 0006. It sets no CORS
// headers and checks no credentials; the loopback bind is the protection. The
// handlers are placeholders until the ADRs that own them land: the admin API
// (ADR 0023), the UI (ADR 0029) and health and metrics (ADR 0026).
func NewMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/admin/", notImplemented)
	mux.HandleFunc("/ui/", notImplemented)
	mux.HandleFunc("/metrics", notImplemented)
	mux.HandleFunc("/healthz", notImplemented)
	mux.HandleFunc("/readyz", notImplemented)
	return mux
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not implemented", http.StatusNotImplemented)
}
