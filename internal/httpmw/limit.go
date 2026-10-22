package httpmw

import (
	"errors"
	"net/http"
	"time"

	"github.com/mtanzi/llm-proxy/internal/family/anthropic"
)

// MaxBytes enforces the one request-size limit (server.max_request_bytes). A
// declared Content-Length above the limit gets the protocol 413 at once; any
// other body is wrapped in http.MaxBytesReader, so the reader fails once the
// limit is crossed. Handlers report that failure with WriteBodyError.
func MaxBytes(limit int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > limit {
			WriteError(w, anthropic.OversizeBodyError(w.Header().Get(HeaderRequestID)))
			return
		}
		if r.Body != nil && r.Body != http.NoBody {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// WriteBodyError answers a failed body read. It writes the protocol 413 for a
// body past the limit and reports true; for any other error it writes nothing
// and reports false, so the caller picks the response.
func WriteBodyError(w http.ResponseWriter, err error) bool {
	if _, ok := errors.AsType[*http.MaxBytesError](err); !ok {
		return false
	}
	WriteError(w, anthropic.OversizeBodyError(w.Header().Get(HeaderRequestID)))
	return true
}

// ClearWriteDeadline removes the write deadline of the connection, for a
// streaming response that outlasts any fixed timeout. A writer that does not
// support deadlines is left as it is.
func ClearWriteDeadline(w http.ResponseWriter) error {
	err := http.NewResponseController(w).SetWriteDeadline(time.Time{})
	if errors.Is(err, http.ErrNotSupported) {
		return nil
	}
	return err
}
