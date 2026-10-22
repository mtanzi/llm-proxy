package httpmw

import (
	"context"
	"net/http"

	"github.com/mtanzi/llm-proxy/internal/core"
)

// HeaderRequestID carries the call id on every response and, when a client
// sends it, the client's own request id on the request.
const HeaderRequestID = "X-Request-Id"

type ctxKey int

const (
	callIDKey ctxKey = iota
	clientRequestIDKey
)

// RequestID assigns a new UUIDv7 call id to every request and sets it as the
// x-request-id response header. A client-supplied x-request-id is not reused;
// it is kept apart and read with ClientRequestID.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := core.NewID()
		w.Header().Set(HeaderRequestID, id.String())
		ctx := context.WithValue(r.Context(), callIDKey, id)
		ctx = context.WithValue(ctx, clientRequestIDKey, r.Header.Get(HeaderRequestID))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// CallID returns the call id RequestID assigned to the request, or the zero
// value when the request did not pass through RequestID.
func CallID(ctx context.Context) core.ID {
	id, _ := ctx.Value(callIDKey).(core.ID)
	return id
}

// ClientRequestID returns the x-request-id the client sent, or "" if none.
func ClientRequestID(ctx context.Context) string {
	s, _ := ctx.Value(clientRequestIDKey).(string)
	return s
}
