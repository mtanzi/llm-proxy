package httpmw

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

// statusWriter records the status and size of a response. Unwrap lets
// http.NewResponseController reach the underlying writer for flushes and
// deadlines.
type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

func (w *statusWriter) Flush() { _ = http.NewResponseController(w.ResponseWriter).Flush() }

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// AccessLog writes one slog line per request when the response ends: the call
// id, the client's request id, method, path, status, size and duration. It
// never logs a body or the query string. Request headers are added at debug
// level, after RedactHeaders. A panic is logged as status 500 and passed on to
// Recover.
func AccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		defer func() {
			p := recover()
			status := sw.status
			if p != nil {
				status = http.StatusInternalServerError
			} else if status == 0 {
				status = http.StatusOK
			}
			attrs := []any{
				"call_id", CallID(r.Context()).String(),
				"client_request_id", ClientRequestID(r.Context()),
				"method", r.Method,
				"path", r.URL.Path,
				"status", status,
				"bytes", sw.bytes,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote", r.RemoteAddr,
			}
			if log.Enabled(context.Background(), slog.LevelDebug) {
				attrs = append(attrs, "headers", RedactHeaders(r.Header))
			}
			log.Info("request", attrs...)
			if p != nil {
				panic(p)
			}
		}()
		next.ServeHTTP(sw, r)
	})
}
