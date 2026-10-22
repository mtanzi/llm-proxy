package httpmw

import (
	"log/slog"
	"net/http"
)

// Middleware wraps a handler with one step.
type Middleware func(http.Handler) http.Handler

// Options configures Wrap.
type Options struct {
	Logger          *slog.Logger
	MaxRequestBytes int64 // 0 means no limit
	// Inner are the steps that run after the size limit, outermost first. They
	// are the seam for auth (ADR 0008) and correlation (ADR 0010).
	Inner []Middleware
}

// Wrap runs h behind the fixed chain: recover, request id, access log, size
// limit, then the Inner steps in order.
func Wrap(h http.Handler, o Options) http.Handler {
	for i := len(o.Inner) - 1; i >= 0; i-- {
		h = o.Inner[i](h)
	}
	if o.MaxRequestBytes > 0 {
		h = MaxBytes(o.MaxRequestBytes, h)
	}
	h = AccessLog(o.Logger, h)
	h = RequestID(h)
	return Recover(o.Logger, h)
}
