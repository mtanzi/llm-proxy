package httpmw

import (
	"net/http"

	"github.com/mtanzi/spyglass/internal/family/anthropic"
)

// WriteError writes a proxy-generated error as is.
func WriteError(w http.ResponseWriter, e anthropic.ErrorResponse) {
	h := w.Header()
	for k, v := range e.Header {
		h[k] = v
	}
	w.WriteHeader(e.Status)
	_, _ = w.Write(e.Body)
}
