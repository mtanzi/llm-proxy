package httpmw

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/mtanzi/spyglass/internal/family/anthropic"
)

// Recover turns a panic in a later step into a logged 500 with the protocol
// error body, and keeps the connection serving. It reads the call id from the
// response header RequestID set, so it can run outside RequestID. If the
// response already started, it logs the panic and aborts the connection with
// http.ErrAbortHandler, so the client sees a cut response, not a complete one.
// http.ErrAbortHandler is passed on: it is how a handler aborts a response.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			id := w.Header().Get(HeaderRequestID)
			log.Error("panic in handler", "call_id", id, "panic", v, "stack", string(debug.Stack()))
			if sw.status != 0 {
				panic(http.ErrAbortHandler)
			}
			WriteError(w, anthropic.InternalError(id))
		}()
		next.ServeHTTP(sw, r)
	})
}
