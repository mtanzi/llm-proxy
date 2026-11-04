package httpmw

import (
	"log/slog"
	"net/http"
)

// Middleware wraps a handler with one step.
type Middleware func(http.Handler) http.Handler

// Options configures Wrap.
type Options struct {
	Logger *slog.Logger
	// MaxRequestBytes returns the size limit, read once per request so a
	// reload applies to the next request. Nil, or a result of zero or less,
	// means no limit.
	MaxRequestBytes func() int64
	// Inner are the steps that run after the size limit, outermost first. They
	// are the seam for auth (ADR 0008) and correlation (ADR 0010).
	Inner []Middleware
}

// Wrap runs h behind the fixed chain: arrival stamp, recover, request id,
// access log, size limit, then the Inner steps in order.
func Wrap(h http.Handler, o Options) http.Handler {
	for i := len(o.Inner) - 1; i >= 0; i-- {
		h = o.Inner[i](h)
	}
	if o.MaxRequestBytes != nil {
		h = MaxBytes(o.MaxRequestBytes, h)
	}
	h = AccessLog(o.Logger, h)
	h = RequestID(h)
	return stampArrival(Recover(o.Logger, h))
}
