package httpmw

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"
)

// readyTimeout bounds one readiness check, so a hung dependency answers
// not-ready instead of holding the probe open.
const readyTimeout = 2 * time.Second

// Drain is the shutdown flag of ADR 0026. Both listeners share one Drain.
// Once Start is called, every /readyz it guards answers not-ready, so a load
// balancer stops sending new requests while streams drain. A nil *Drain is
// never draining.
type Drain struct{ on atomic.Bool }

// Start marks the process as shutting down. It is idempotent.
func (d *Drain) Start() { d.on.Store(true) }

// Active reports whether shutdown has started.
func (d *Drain) Active() bool { return d != nil && d.on.Load() }

// Healthz answers 200 while the process runs. It checks nothing else.
func Healthz() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeProbe(w, http.StatusOK, "ok")
	})
}

// Readyz answers 200 when the process is not draining and check returns nil,
// and 503 otherwise. check must not be nil. The body names the cause
// without the error text, which can carry connection details.
func Readyz(d *Drain, check func(context.Context) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d.Active() {
			writeProbe(w, http.StatusServiceUnavailable, "shutting down")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), readyTimeout)
		defer cancel()
		if err := check(ctx); err != nil {
			writeProbe(w, http.StatusServiceUnavailable, "not ready")
			return
		}
		writeProbe(w, http.StatusOK, "ok")
	})
}

func writeProbe(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body + "\n"))
}
